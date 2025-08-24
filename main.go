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

	// Serve the static HTML client.
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

	// set http headers
	w.Header().Set("Connection", "Keep-Alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Transfer-Encoding", "chunked")
	w.Header().Set("Content-Type", "audio/mpeg")

	// write channel data to response
	// Loop and read chunks from the broadcaster's channel
	for chunk := range broadcastChan {
		if _, err := w.Write(chunk.Data); err != nil {
			log.Printf("Failed to write to client on channel %s: %v", channelID, err)
			return // Exit on write error
		}
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}
}

// should read files in order from a folder and stream them in order
// read audio file chunks from disk
// broadcast chunks to all client connections
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

	buffer := make([]byte, chunkSize)
	ticker := time.NewTicker(170 * time.Millisecond) // Adjust for desired stream rate
	defer ticker.Stop()

	// Use a for range loop to iterate over the ticker channel
	for range ticker.C {
		n, err := file.Read(buffer)
		if err == io.EOF {
			log.Println("End of file reached. Looping back to the beginning.")
			// TODO: load different file here
			file.Seek(0, 0) // Seek to the beginning of the file to loop
			continue
		}
		if err != nil {
			log.Fatalf("Error reading audio file: %v", err)
		}

		// Create an AudioChunk with the read data
		chunk := AudioChunk{Data: buffer[:n]}

		select {
		case broadcastChan <- chunk:
		default:
			// log.Print("Broadcast channel is blocked, dropping chunk")
		}
	}
}
