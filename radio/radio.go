package radio

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Radio struct {
	dir            string
	channels       []Channel
	broadcasterMap map[string]chan AudioChunk
	broadcasterMux sync.Mutex
	bufferMap      map[string]*RingBuffer
	bufferMux      sync.Mutex
}

type Channel struct {
	ID   string
	Name string
}

type AudioSource struct {
	ID   string
	Name string
}

type AudioChunk struct {
	Data []byte
}

func New(dataDir string) *Radio {
	return &Radio{dir: dataDir, broadcasterMap: make(map[string]chan AudioChunk), bufferMap: make(map[string]*RingBuffer)}
}

func (r *Radio) LoadChannels() error {
	channels := []Channel{}

	entries, err := os.ReadDir(r.dir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			channels = append(channels, Channel{ID: uuid.NewString(), Name: entry.Name()})
		}
	}

	r.channels = channels

	return nil
}

func (r *Radio) GetChannels() []Channel {
	return r.channels
}

func (r *Radio) Broadcast() {
	for _, channel := range r.channels {
		go r.BroadcastChannel(channel)
	}
}

func (r *Radio) BroadcastChannel(channel Channel) {
	entries, err := os.ReadDir(fmt.Sprintf("%s/%s", r.dir, channel.Name))
	if err != nil {
		log.Fatalf("Failed to load audio files: %v", err)
	}

	audioSources := []AudioSource{}

	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".mp3") {
			audioSources = append(audioSources, AudioSource{ID: uuid.NewString(), Name: entry.Name()})
		}
	}

	r.bufferMux.Lock()
	audioBuffer := NewRingBuffer(28) // e.g., 6 seconds of audio at 192kbps
	r.bufferMap[channel.ID] = audioBuffer
	r.bufferMux.Unlock()

	r.broadcasterMux.Lock()
	broadcastChan := make(chan AudioChunk)
	r.broadcasterMap[channel.ID] = broadcastChan
	r.broadcasterMux.Unlock()

	const chunkSize = 1024 * 4
	file, err := os.Open(fmt.Sprintf("%s/%s/%s", r.dir, channel.Name, audioSources[0].Name))
	if err != nil {
		log.Fatalf("Failed to open audio file: %v", err)
	}
	defer file.Close()

	fmt.Printf("streaming file: %s\n", fmt.Sprintf("%s/%s/%s", r.dir, channel.Name, audioSources[0].Name))

	readBuffer := make([]byte, chunkSize)
	ticker := time.NewTicker(170 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		n, err := file.Read(readBuffer)
		if err == io.EOF {
			log.Println("End of file reached. Looping back to the beginning.")
			// TODO: load different file here
			file.Seek(0, 0) // Seek to the beginning of the file to loop
			continue
		}
		if err != nil {
			log.Fatalf("Error reading audio file: %v", err)
		}

		chunkData := make([]byte, n)
		copy(chunkData, readBuffer[:n])

		audioBuffer.Write(chunkData)

		chunk := AudioChunk{Data: chunkData}

		select {
		case broadcastChan <- chunk:
		default:
		}
	}
}

// Send data from channel ring buffer to writer
func (r *Radio) WriteBuffer(w io.Writer, channelID string) error {
	r.bufferMux.Lock()
	audioBuffer, ok := r.bufferMap[channelID]
	r.bufferMux.Unlock()
	if !ok {
		return fmt.Errorf("channel %s not found", channelID)
	}

	initialChunks := audioBuffer.ReadAll()
	for _, chunk := range initialChunks {
		if _, err := w.Write(chunk); err != nil {
			log.Printf("Failed to write to client on channel %s: %v", channelID, err)
		}
	}

	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}

	return nil
}

func (r *Radio) StreamChunks(w io.Writer, channelID string) error {
	r.broadcasterMux.Lock()
	broadcastChan, ok := r.broadcasterMap[channelID]
	r.broadcasterMux.Unlock()
	if !ok {
		return fmt.Errorf("channel %s not found", channelID)
	}

	for chunk := range broadcastChan {
		if _, err := w.Write(chunk.Data); err != nil {
			log.Printf("Failed to write to client on channel %s: %v", channelID, err)
			return err
		}
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}

	return nil
}
