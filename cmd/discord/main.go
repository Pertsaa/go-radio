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
	token       string
	activeVCs   = make(map[string]*discordgo.VoiceConnection)
	ffmpegProcs = make(map[string]context.CancelFunc)
	mu          sync.Mutex
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
	guildID := i.GuildID
	radioBaseURL := "http://localhost:8080"

	switch i.ApplicationCommandData().Name {
	case "radio":
		channels, err := fetchChannels(radioBaseURL)
		if err != nil || len(channels) == 0 {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{Content: "No radio channels available"},
			})
			return
		}
		startStreamingInteraction(s, i, guildID, radioBaseURL, channels[0].ID, channels[0].Name)

	case "stop":
		stopStreaming(guildID)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{Content: "Stopped streaming."},
		})

	case "channels":
		channels, err := fetchChannels(radioBaseURL)
		if err != nil || len(channels) == 0 {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{Content: "No radio channels available."},
			})
			return
		}
		var names []string
		for _, ch := range channels {
			names = append(names, ch.Name)
		}
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{Content: "Available channels: " + strings.Join(names, ", ")},
		})

	case "channel":
		channelName := i.ApplicationCommandData().Options[0].StringValue()
		channels, err := fetchChannels(radioBaseURL)
		if err != nil || len(channels) == 0 {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{Content: "No radio channels available."},
			})
			return
		}

		var selectedChannel *Channel
		for _, ch := range channels {
			if strings.EqualFold(ch.Name, channelName) {
				selectedChannel = &ch
				break
			}
		}
		if selectedChannel == nil {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{Content: "Channel not found."},
			})
			return
		}
		startStreamingInteraction(s, i, guildID, radioBaseURL, selectedChannel.ID, selectedChannel.Name)
	}
}

func startStreamingInteraction(s *discordgo.Session, i *discordgo.InteractionCreate, guildID, radioBaseURL, channelID, channelName string) {
	stopStreaming(guildID)

	g, err := s.State.Guild(guildID)
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
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{Content: "Join a voice channel first!"},
		})
		return
	}

	vc, err := s.ChannelVoiceJoin(guildID, userChannelID, false, true)
	if err != nil {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{Content: "Failed to join voice channel."},
		})
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	mu.Lock()
	ffmpegProcs[guildID] = cancel
	activeVCs[guildID] = vc
	mu.Unlock()

	go func() {
		if err := streamChannel(ctx, guildID, radioBaseURL, channelID); err != nil {
			log.Println("Stream error:", err)
		}
		mu.Lock()
		delete(activeVCs, guildID)
		delete(ffmpegProcs, guildID)
		mu.Unlock()
	}()

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: fmt.Sprintf("Streaming %s...", channelName)},
	})
}

func stopStreaming(guildID string) {
	mu.Lock()
	if cancel, ok := ffmpegProcs[guildID]; ok {
		cancel()
		delete(ffmpegProcs, guildID)
	}
	if vc, ok := activeVCs[guildID]; ok {
		vc.Disconnect()
		delete(activeVCs, guildID)
	}
	mu.Unlock()
}

func voiceStateUpdate(s *discordgo.Session, vs *discordgo.VoiceStateUpdate) {
	fmt.Println("voice state update")

	mu.Lock()
	defer mu.Unlock()

	guildID := vs.GuildID
	vc, ok := activeVCs[guildID]
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

	fmt.Printf("user count %d\n", userCount)

	if userCount == 0 {
		stopStreaming(guildID)
		log.Println("Disconnected from empty voice channel in guild:", guildID)
	}
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

func streamChannel(ctx context.Context, guildID, radioBaseURL, radioChannelID string) error {
	vc := activeVCs[guildID]
	vc.Speaking(true)
	defer vc.Speaking(false)

	streamURL := fmt.Sprintf("%s/radio/channels/%s/stream", radioBaseURL, radioChannelID)
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

func cleanup() {
	mu.Lock()
	defer mu.Unlock()
	for _, cancel := range ffmpegProcs {
		cancel()
	}
	for _, vc := range activeVCs {
		vc.Disconnect()
	}
	ffmpegProcs = make(map[string]context.CancelFunc)
	activeVCs = make(map[string]*discordgo.VoiceConnection)
}
