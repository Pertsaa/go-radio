package main

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

var (
	broadcasterMap = make(map[string]chan AudioChunk)
	broadcasterMux sync.Mutex
	bufferMap      = make(map[string]*RingBuffer)
	bufferMux      sync.Mutex
)

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

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: ./go-radio <data_dir>")
		os.Exit(1)
	}

	dataDir := os.Args[1]

	channels, err := loadChannels(dataDir)
	if err != nil {
		log.Fatalf("Server failed to load channels: %v", err)
	}

	for _, channel := range channels {
		go broadcastAudio(dataDir, channel)
	}

	http.Handle("/", http.FileServer(http.Dir(".")))
	http.HandleFunc("/{channelID}/stream", streamHandler)

	fmt.Println("Server is running on http://localhost:8080")
	fmt.Println("Press Ctrl+C to stop.")

	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}

func loadChannels(dataDir string) ([]Channel, error) {
	channels := []Channel{}

	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return channels, err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			channels = append(channels, Channel{ID: uuid.NewString(), Name: entry.Name()})
		}
	}

	return channels, nil
}

func streamHandler(w http.ResponseWriter, r *http.Request) {
	channelID := r.PathValue("channelID")

	broadcasterMux.Lock()
	broadcastChan, ok := broadcasterMap[channelID]
	broadcasterMux.Unlock()
	if !ok {
		http.Error(w, "Channel not found.", http.StatusNotFound)
		return
	}

	bufferMux.Lock()
	audioBuffer, ok := bufferMap[channelID]
	bufferMux.Unlock()
	if !ok {
		http.Error(w, "Channel not found.", http.StatusNotFound)
		return
	}

	// Send the data from the ring buffer first to fill the client's buffer
	initialChunks := audioBuffer.ReadAll()
	for _, chunk := range initialChunks {
		if _, err := w.Write(chunk); err != nil {
			log.Printf("Failed to write to client on channel %s: %v", channelID, err)
			return
		}
	}

	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}

	// set http headers
	w.Header().Set("Connection", "Keep-Alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Transfer-Encoding", "chunked")
	w.Header().Set("Content-Type", "audio/mpeg")

	for chunk := range broadcastChan {
		if _, err := w.Write(chunk.Data); err != nil {
			log.Printf("Failed to write to client on channel %s: %v", channelID, err)
			return
		}
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}
}

func broadcastAudio(dataDir string, channel Channel) {
	entries, err := os.ReadDir(fmt.Sprintf("%s/%s", dataDir, channel.Name))
	if err != nil {
		log.Fatalf("Failed to load audio files: %v", err)
	}

	audioSources := []AudioSource{}

	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".mp3") {
			audioSources = append(audioSources, AudioSource{ID: uuid.NewString(), Name: entry.Name()})
		}
	}

	bufferMux.Lock()
	audioBuffer := NewRingBuffer(28) // e.g., 6 seconds of audio at 192kbps
	bufferMap[channel.Name] = audioBuffer
	bufferMux.Unlock()

	broadcasterMux.Lock()
	broadcastChan := make(chan AudioChunk)
	broadcasterMap[channel.Name] = broadcastChan
	broadcasterMux.Unlock()

	const chunkSize = 1024 * 4                                                                 // 4 KB                                                            // 10 KB                                                             // 10KB
	file, err := os.Open(fmt.Sprintf("%s/%s/%s", dataDir, channel.Name, audioSources[0].Name)) // Replace with your file name
	if err != nil {
		log.Fatalf("Failed to open audio file: %v", err)
	}
	defer file.Close()

	fmt.Printf("streaming file: %s\n", fmt.Sprintf("%s/%s/%s", dataDir, channel.Name, audioSources[0].Name))

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

type RingBuffer struct {
	data  [][]byte
	head  int
	size  int
	mutex sync.Mutex
}

func NewRingBuffer(size int) *RingBuffer {
	return &RingBuffer{
		data: make([][]byte, size),
		size: size,
	}
}

func (b *RingBuffer) Write(chunk []byte) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	b.data[b.head] = chunk
	b.head = (b.head + 1) % b.size
}

func (b *RingBuffer) ReadAll() [][]byte {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	result := make([][]byte, 0, b.size)
	for i := b.head; i < b.size; i++ {
		if b.data[i] != nil {
			result = append(result, b.data[i])
		}
	}
	for i := 0; i < b.head; i++ {
		if b.data[i] != nil {
			result = append(result, b.data[i])
		}
	}
	return result
}
