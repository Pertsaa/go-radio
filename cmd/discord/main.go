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
	activeVCs   = make(map[string]*discordgo.VoiceConnection) // map[guildID]*VoiceConnection
	ffmpegProcs = make(map[string]context.CancelFunc)         // map[guildID]*CancelFunc
	mu          sync.Mutex                                    // protects maps
)

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
	dg.AddHandler(messageCreate)
	dg.AddHandler(guildCreate)
	dg.Identify.Intents = discordgo.IntentsGuilds | discordgo.IntentsGuildMessages | discordgo.IntentsGuildVoiceStates

	if err := dg.Open(); err != nil {
		fmt.Println("Error opening Discord session: ", err)
		return
	}

	fmt.Println("Radio bot is running. Press CTRL-C to exit.")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc

	cleanup()
	dg.Close()
}

// ready sets the bot status
func ready(s *discordgo.Session, event *discordgo.Ready) {
	s.UpdateGameStatus(0, "!radio")
}

func messageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID {
		return
	}

	c, err := s.State.Channel(m.ChannelID)
	if err != nil {
		return
	}
	guildID := c.GuildID
	radioBaseURL := "http://localhost:8080"

	switch {
	case strings.HasPrefix(m.Content, "!radio"):
		channels, err := fetchChannels(radioBaseURL)
		if err != nil || len(channels) == 0 {
			s.ChannelMessageSend(m.ChannelID, "No radio channels available")
			return
		}
		firstChannel := channels[0] // pick first channel
		startStreaming(s, m, guildID, radioBaseURL, firstChannel.ID, firstChannel.Name)

	case strings.HasPrefix(m.Content, "!stop"):
		stopStreaming(guildID)
		// s.ChannelMessageSend(m.ChannelID, "Stopped streaming and left the voice channel.")

	case strings.HasPrefix(m.Content, "!channels"):
		channels, err := fetchChannels(radioBaseURL)
		if err != nil || len(channels) == 0 {
			s.ChannelMessageSend(m.ChannelID, "No radio channels available.")
			return
		}
		var names []string
		for _, ch := range channels {
			names = append(names, ch.Name)
		}
		s.ChannelMessageSend(m.ChannelID, "Available channels: "+strings.Join(names, ", "))

	case strings.HasPrefix(m.Content, "!channel "):
		args := strings.SplitN(m.Content, " ", 2)
		if len(args) < 2 {
			s.ChannelMessageSend(m.ChannelID, "Usage: !channel <channel_name>")
			return
		}
		channelName := strings.TrimSpace(args[1])

		channels, err := fetchChannels(radioBaseURL)
		if err != nil || len(channels) == 0 {
			s.ChannelMessageSend(m.ChannelID, "No radio channels available.")
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
			s.ChannelMessageSend(m.ChannelID, "Channel not found.")
			return
		}

		startStreaming(s, m, guildID, radioBaseURL, selectedChannel.ID, selectedChannel.Name)
	}
}

// startStreaming joins user's VC and starts streaming a channel
func startStreaming(s *discordgo.Session, m *discordgo.MessageCreate, guildID, radioBaseURL, channelID, channelName string) {
	stopStreaming(guildID)

	g, err := s.State.Guild(guildID)
	if err != nil {
		return
	}

	var userChannelID string
	for _, vs := range g.VoiceStates {
		if vs.UserID == m.Author.ID {
			userChannelID = vs.ChannelID
			break
		}
	}
	if userChannelID == "" {
		s.ChannelMessageSend(m.ChannelID, "Join a voice channel first!")
		return
	}

	vc, err := s.ChannelVoiceJoin(guildID, userChannelID, false, true)
	if err != nil {
		s.ChannelMessageSend(m.ChannelID, "Failed to join voice channel")
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	mu.Lock()
	ffmpegProcs[guildID] = cancel
	activeVCs[guildID] = vc
	mu.Unlock()

	go func() {
		err := streamChannel(ctx, guildID, radioBaseURL, channelID)
		if err != nil {
			log.Println("Stream error:", err)
		}
		mu.Lock()
		delete(activeVCs, guildID)
		delete(ffmpegProcs, guildID)
		mu.Unlock()
	}()

	// s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Streaming %s...", channelName))
}

// stopStreaming stops any active stream in a guild
func stopStreaming(guildID string) {
	mu.Lock()
	defer mu.Unlock()
	if cancel, ok := ffmpegProcs[guildID]; ok {
		cancel()
		delete(ffmpegProcs, guildID)
	}
	if vc, ok := activeVCs[guildID]; ok {
		vc.Disconnect()
		delete(activeVCs, guildID)
	}
}

// guildCreate sends a ready message when joining a guild
func guildCreate(s *discordgo.Session, event *discordgo.GuildCreate) {
	if event.Guild.Unavailable {
		return
	}
	for _, channel := range event.Guild.Channels {
		if channel.ID == event.Guild.ID {
			s.ChannelMessageSend(channel.ID, "Radio is ready! Type !radio while in a voice channel to play a stream.")
			return
		}
	}
}

// streamChannel streams a radio channel into a voice channel
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

	// kill ffmpeg if context canceled
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
			n, err := io.ReadFull(stdout, buf)
			if err != nil {
				if err == io.EOF || err == io.ErrUnexpectedEOF {
					return nil
				}
				log.Println("Error reading PCM from FFmpeg:", err)
				return err
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
}

// Channel represents a radio channel
type Channel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// fetchChannels fetches radio channels
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

// cleanup stops all streams and disconnects all voice connections
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
