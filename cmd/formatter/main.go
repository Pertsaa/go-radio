package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// main is the entry point of the program.
func main() {
	// Check for the correct number of command-line arguments.
	if len(os.Args) != 3 {
		fmt.Printf("Usage: %s <input_directory> <output_directory>\n", os.Args[0])
		os.Exit(1)
	}

	inputDir := os.Args[1]
	outputDir := os.Args[2]

	// Ensure the output directory exists. If not, create it.
	if err := os.MkdirAll(outputDir, os.ModePerm); err != nil {
		log.Fatalf("Error creating output directory: %v", err)
	}

	fmt.Printf("Scanning directory: %s\n", inputDir)
	fmt.Printf("Output directory set to: %s\n", outputDir)
	fmt.Println("----------------------------------------")

	// Read all directory entries from the input directory.
	entries, err := os.ReadDir(inputDir)
	if err != nil {
		log.Fatalf("Error reading input directory: %v", err)
	}

	// Iterate over each entry in the directory.
	for _, entry := range entries {
		// Skip directories and non-MP3 files.
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".mp3") {
			continue
		}

		fileName := entry.Name()
		inputPath := filepath.Join(inputDir, fileName)
		outputPath := filepath.Join(outputDir, fileName)

		fmt.Printf("Processing file: %s -> %s\n", inputPath, outputPath)

		// Create the FFmpeg command.
		cmd := exec.Command("ffmpeg", "-i", inputPath, "-b:a", "192k", "-y", outputPath)

		// Run the command and capture any errors.
		output, err := cmd.CombinedOutput()
		if err != nil {
			log.Printf("Failed to process %s: %v\n", fileName, err)
			log.Println("FFmpeg output:")
			fmt.Println(string(output))
		} else {
			fmt.Printf("Successfully converted %s\n", fileName)
		}
	}

	fmt.Println("----------------------------------------")
	fmt.Println("Processing complete.")
}
