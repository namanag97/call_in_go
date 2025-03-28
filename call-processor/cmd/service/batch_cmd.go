package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// NewBatchCmd creates a new batch command
func NewBatchCmd() *cobra.Command {
	var (
		inputDir  string
		outputDir string
		language  string
		verbosity int
	)

	cmd := &cobra.Command{
		Use:   "batch",
		Short: "Process audio files in batch",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("Starting batch processing with verbosity level %d\n", verbosity)
			fmt.Printf("Input directory: %s\n", inputDir)
			
			// Check if directory exists
			if _, err := os.Stat(inputDir); os.IsNotExist(err) {
				return fmt.Errorf("input directory does not exist: %s", inputDir)
			}
			
			// Scan for audio files
			audioFiles := []string{}
			err := filepath.Walk(inputDir, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if !info.IsDir() {
					ext := strings.ToLower(filepath.Ext(path))
					if ext == ".mp3" || ext == ".wav" || ext == ".aac" || ext == ".m4a" {
						audioFiles = append(audioFiles, path)
					}
				}
				return nil
			})
			
			if err != nil {
				return fmt.Errorf("error scanning directory: %w", err)
			}
			
			fmt.Printf("Found %d audio files\n", len(audioFiles))
			for _, file := range audioFiles {
				fmt.Printf("- %s\n", file)
			}
			
			return nil
		},
	}

	// Add flags
	cmd.Flags().StringVarP(&inputDir, "input-dir", "i", "/Users/namanagarwal/Documents/go_project/call-processor/clips", 
		"Directory containing audio files")
	cmd.Flags().StringVarP(&outputDir, "output-dir", "o", "", 
		"Directory to save output files")
	cmd.Flags().StringVarP(&language, "language", "l", "en-US", 
		"Language code for transcription")
	cmd.Flags().IntVarP(&verbosity, "verbosity", "v", 1, 
		"Verbosity level (0-3)")

	return cmd
}
