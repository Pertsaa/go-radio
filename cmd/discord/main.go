package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/bwmarrin/discordgo"
	"layeh.com/gopus"
)

var (
	radioBaseURL = "http://localhost:8080"
	token        string
	vcs          = make(map[string]*discordgo.VoiceConnection)
	vcMu         sync.Mutex
	ffmpegs      = make(map[string]context.CancelFunc)
	ffmpegMu     sync.Mutex
)

var commands = []*discordgo.ApplicationCommand{
	{Name: "radio", Description: "Start streaming the default radio channel"},
	{Name: "stop", Description: "Stop streaming"},
	{Name: "channels", Description: "List available radio channels"},
	{
		Name:        "channel",
		Description: "Stream a specific radio channel",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "name",
				Description: "Name of the radio channel",
				Required:    true,
			},
		},
	},
}

func init() {
	flag.StringVar(&token, "t", "", "Bot Token")
	flag.Parse()
}

func main() {
	if token == "" {
		fmt.Println("No token provided. Please run: bot -t <bot token>")
		return
	}

	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		fmt.Println("Error creating Discord session: ", err)
		return
	}

	dg.AddHandler(ready)
	dg.AddHandler(interactionCreate)
	dg.AddHandler(guildCreate)
	dg.AddHandler(voiceStateUpdate)

	dg.Identify.Intents = discordgo.IntentsGuilds | discordgo.IntentsGuildVoiceStates

	if err := dg.Open(); err != nil {
		fmt.Println("Error opening Discord session: ", err)
		return
	}

	for _, v := range commands {
		_, err := dg.ApplicationCommandCreate(dg.State.User.ID, "", v)
		if err != nil {
			log.Println("Cannot create slash command:", err)
		}
	}

	fmt.Println("Radio bot is running. Press CTRL-C to exit.")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc

	cleanup()
	dg.Close()
}

func ready(s *discordgo.Session, event *discordgo.Ready) {
	s.UpdateGameStatus(0, "/radio")
}

func interactionCreate(s *discordgo.Session, i *discordgo.InteractionCreate) {
	cmd := i.ApplicationCommandData().Name

	if cmd == "radio" {
		radioInteraction(s, i)
		return
	}

	if cmd == "channels" {
		channelListInteraction(s, i)
		return
	}

	if cmd == "channel" {
		channelInteraction(s, i)
		return
	}

	if cmd == "stop" {
		stopInteraction(s, i)
		return
	}
}

// Join user vc and start streaming first radio channel
func radioInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	channels, err := fetchChannels(radioBaseURL)

	if err != nil || len(channels) == 0 {
		respondMessage(s, i, "No radio channels available")
		return
	}

	joinUserVoiceChannel(s, i)

	go streamChannel(i.GuildID, channels[0])

	respondMessage(s, i, fmt.Sprintf("Streaming %s...", channels[0].Name))
}

// Switch stream to different radio channel
func channelInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	channelName := i.ApplicationCommandData().Options[0].StringValue()
	channels, err := fetchChannels(radioBaseURL)
	if err != nil || len(channels) == 0 {
		respondMessage(s, i, "No radio channels available")
		return
	}

	var channel Channel
	found := false
	for _, ch := range channels {
		if strings.EqualFold(ch.Name, channelName) {
			channel = ch
			found = true
			break
		}
	}

	if !found {
		respondMessage(s, i, "Channel not found")
		return
	}

	// get existing voice connection
	_, ok := vcs[i.GuildID]
	if !ok {
		respondMessage(s, i, "Not in a voice channel. Remember to run /radio first.")
		return
	}

	go streamChannel(i.GuildID, channel)

	respondMessage(s, i, fmt.Sprintf("Streaming %s...", channelName))
}

// Fetch and list available radio channels
func channelListInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	channels, err := fetchChannels(radioBaseURL)
	if err != nil || len(channels) == 0 {
		respondMessage(s, i, "No radio channels available.")
		return
	}

	var names []string
	for _, ch := range channels {
		names = append(names, ch.Name)
	}

	respondMessage(s, i, "Available channels: "+strings.Join(names, ", "))
}

// Stop streaming and disconnect vc
func stopInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	ffmpegMu.Lock()
	if cancel, ok := ffmpegs[i.GuildID]; ok {
		cancel()
		delete(ffmpegs, i.GuildID)
	}
	ffmpegMu.Unlock()

	vcMu.Lock()
	defer vcMu.Unlock()
	if vc, ok := vcs[i.GuildID]; ok {
		vc.Disconnect()
		delete(vcs, i.GuildID)
	}

	respondMessage(s, i, "Stopped streaming.")
}

func voiceStateUpdate(s *discordgo.Session, vs *discordgo.VoiceStateUpdate) {
	guildID := vs.GuildID
	vc, ok := vcs[guildID]
	if !ok {
		return
	}

	guild, err := s.State.Guild(guildID)
	if err != nil {
		return
	}

	userCount := 0
	for _, vs := range guild.VoiceStates {
		if vs.ChannelID == vc.ChannelID && vs.UserID != s.State.User.ID {
			userCount++
		}
	}

	if userCount == 0 {
		ffmpegMu.Lock()
		if cancel, ok := ffmpegs[guildID]; ok {
			cancel()
			delete(ffmpegs, guildID)
		}
		ffmpegMu.Unlock()

		vcMu.Lock()
		defer vcMu.Unlock()
		if vc, ok := vcs[guildID]; ok {
			vc.Disconnect()
			delete(vcs, guildID)
		}
		log.Println("Disconnected from empty voice channel in guild:", guildID)
	}
}

func streamChannel(guildID string, channel Channel) error {
	vc, ok := vcs[guildID]
	if !ok {
		return fmt.Errorf("vc not found for guild %s", guildID)
	}
	vc.Speaking(true)
	defer vc.Speaking(false)

	cancel, ok := ffmpegs[guildID]
	if ok {
		cancel()
	}

	ctx, cancel := context.WithCancel(context.Background())
	ffmpegMu.Lock()
	ffmpegs[guildID] = cancel
	ffmpegMu.Unlock()

	streamURL := fmt.Sprintf("%s/radio/channels/%s/stream", radioBaseURL, channel.ID)
	cmd := exec.Command("ffmpeg",
		"-i", streamURL,
		"-map", "0:a",
		"-ac", "2",
		"-ar", "48000",
		"-f", "s16le",
		"pipe:1",
	)
	cmd.Stderr = os.Stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	go func() {
		<-ctx.Done()
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
	}()

	encoder, err := gopus.NewEncoder(48000, 2, gopus.Audio)
	if err != nil {
		return err
	}

	const frameSize = 960
	pcm := make([]int16, frameSize*2)
	buf := make([]byte, frameSize*4)

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		n, err := io.ReadFull(stdout, buf)
		if err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return nil
			}
			log.Println("Error reading PCM from FFmpeg:", err)
			return err
		}

		select {
		case <-ctx.Done():
			ffmpegMu.Lock()
			delete(ffmpegs, guildID)
			ffmpegMu.Unlock()
			return nil
		default:
		}

		for i := 0; i < n/2 && i < len(pcm); i++ {
			pcm[i] = int16(binary.LittleEndian.Uint16(buf[2*i:]))
		}

		opusFrame, err := encoder.Encode(pcm, frameSize, 4000)
		if err != nil {
			log.Println("Opus encode error:", err)
			continue
		}
		vc.OpusSend <- opusFrame
	}
}

func joinUserVoiceChannel(s *discordgo.Session, i *discordgo.InteractionCreate) {
	g, err := s.State.Guild(i.GuildID)
	if err != nil {
		return
	}

	var userChannelID string
	for _, vs := range g.VoiceStates {
		if vs.UserID == i.Member.User.ID {
			userChannelID = vs.ChannelID
			break
		}
	}

	if userChannelID == "" {
		respondMessage(s, i, "Join a voice channel first!")
		return
	}

	vc, err := s.ChannelVoiceJoin(i.GuildID, userChannelID, false, true)
	if err != nil {
		respondMessage(s, i, "Failed to join voice channel.")
		return
	}

	vcMu.Lock()
	vcs[i.GuildID] = vc
	vcMu.Unlock()
}

func respondMessage(s *discordgo.Session, i *discordgo.InteractionCreate, message string) {
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: message},
	})
}

func guildCreate(s *discordgo.Session, event *discordgo.GuildCreate) {
	if event.Guild.Unavailable {
		return
	}
	for _, channel := range event.Guild.Channels {
		if channel.ID == event.Guild.ID {
			s.ChannelMessageSend(channel.ID, "Radio is ready! Use /radio while in a voice channel to play a stream.")
			return
		}
	}
}

func cleanup() {
	for _, cancel := range ffmpegs {
		cancel()
	}

	for _, vc := range vcs {
		vc.Disconnect()
	}

	ffmpegMu.Lock()
	ffmpegs = make(map[string]context.CancelFunc)
	ffmpegMu.Unlock()

	vcMu.Lock()
	vcs = make(map[string]*discordgo.VoiceConnection)
	vcMu.Unlock()
}

type Channel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func fetchChannels(baseURL string) ([]Channel, error) {
	resp, err := http.Get(baseURL + "/radio/channels")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var channels []Channel
	if err := json.NewDecoder(resp.Body).Decode(&channels); err != nil {
		return nil, err
	}
	return channels, nil
}
