package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/namanag97/call_in_go/call-processor/internal/domain"
	"github.com/namanag97/call_in_go/call-processor/internal/repository"
	"github.com/namanag97/call_in_go/call-processor/internal/storage"
	"github.com/namanag97/call_in_go/call-processor/internal/transcription"
	"github.com/spf13/cobra"

	// Import the main package to access global variables
	main "github.com/namanag97/call_in_go/call-processor/cmd/service"
)

func NewBatchCmd() *cobra.Command {
	var (
		inputDir  string
		language  string
		sourceTag string // Optional: Tag to identify batch source
	)

	cmd := &cobra.Command{
		Use:   "batch",
		Short: "Batch process local audio files using application services",
		Long:  `Scans a directory, uploads audio files to storage, creates recording records, and queues transcription jobs.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Input validation
			if inputDir == "" {
				return fmt.Errorf("--input-dir must be specified")
			}

			// Create context for this command's execution
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Hour) // 1 hour timeout
			defer cancel()

			// Access globally initialized components
			if main.DbPool == nil || main.AppServices.TranscriptionService == nil || main.StorageClient == nil {
				return fmt.Errorf("application components not initialized. Ensure PersistentPreRun ran correctly")
			}

			// Start Workers Temporarily for this Command
			log.Println("Starting worker manager for batch command...")
			if err := main.WorkerManager.Start(); err != nil {
				log.Printf("Warning: Failed to start worker manager: %v. Jobs will queue but may not process immediately.", err)
			}
			// Ensure workers are stopped when this command finishes
			defer func() {
				log.Println("Stopping worker manager for batch command...")
				main.WorkerManager.Stop()
			}()

			fmt.Printf("Scanning audio files in %s...\n", inputDir)
			audioFiles, err := ScanAudioFiles(inputDir)
			if err != nil {
				return fmt.Errorf("failed to scan audio files: %w", err)
			}

			if len(audioFiles) == 0 {
				fmt.Println("No audio files found.")
				return nil
			}
			fmt.Printf("Found %d audio files. Starting processing...\n", len(audioFiles))

			processedCount := 0
			errorCount := 0
			skippedCount := 0

			// Define default source tag if not provided
			if sourceTag == "" {
				sourceTag = "batch_cli"
			}

			for _, filePath := range audioFiles {
				fileName := filepath.Base(filePath)
				fmt.Printf("Processing %s...\n", fileName)

				// 1. Read Local File
				fileData, err := os.ReadFile(filePath)
				if err != nil {
					log.Printf("  Error reading file %s: %v", fileName, err)
					errorCount++
					continue
				}

				// 2. Upload to S3
				objectKey := fmt.Sprintf("batch-uploads/%s/%s", time.Now().Format("20060102"), fileName)
				contentType := GetMimeTypeFromExtension(filePath)

				_, err = main.StorageClient.UploadObject(ctx, &storage.UploadObjectInput{
					Bucket:      main.S3BucketName,
					Key:         objectKey,
					Body:        bytes.NewReader(fileData),
					ContentType: contentType,
				})
				if err != nil {
					log.Printf("  Error uploading file %s to S3: %v", fileName, err)
					errorCount++
					continue
				}

				// 3. Create Recording record in DB
				fileInfo, _ := os.Stat(filePath)
				fileSize := int64(0)
				if fileInfo != nil {
					fileSize = fileInfo.Size()
				}

				var createdByUserID *uuid.UUID // Set to nil or a system user ID

				recording := &domain.Recording{
					ID:        uuid.New(),
					FileName:  fileName,
					FilePath:  objectKey, // S3 Key
					FileSize:  fileSize,
					MimeType:  contentType,
					CreatedBy: createdByUserID,
					CreatedAt: time.Now(),
					UpdatedAt: time.Now(),
					Source:    sourceTag,
					Status:    domain.RecordingStatusUploaded,
				}
				err = main.Repos.RecordingRepo.Create(ctx, recording)
				if err != nil {
					log.Printf("  Error creating recording record for %s: %v", fileName, err)
					errorCount++
					continue
				}

				// 4. Start Transcription via Service
				transcriptionReq := transcription.TranscriptionRequest{
					RecordingID: recording.ID,
					Language:    language,
					Engine:      "elevenlabs", // Specify the engine explicitly
					UserID:      uuid.Nil,     // Or the system user ID used above
					// We can add more settings here if needed:
					// SpeakerDiarization: true,  // Enable if needed
					// AddTimestamps: true,       // Enable if needed
				}
				_, err = main.AppServices.TranscriptionService.StartTranscription(ctx, transcriptionReq)
				if err != nil {
					// Check if it failed because transcription already exists
					if errors.Is(err, transcription.ErrInvalidInput) || strings.Contains(err.Error(), "already exists") {
						log.Printf("  Skipping transcription for %s: Already exists or requested.", fileName)
						skippedCount++
					} else {
						log.Printf("  Error starting transcription for %s (Rec ID %s): %v", fileName, recording.ID, err)
						// Mark recording as error since transcription failed to queue
						main.Repos.RecordingRepo.UpdateStatus(ctx, recording.ID, domain.RecordingStatusError)
						errorCount++
						continue
					}
				} else {
					log.Printf("  Queued transcription job for %s (Rec ID %s)\n", fileName, recording.ID)
					processedCount++
				}
			}

			fmt.Printf("\nBatch processing finished.\n")
			fmt.Printf("  Successfully Queued: %d\n", processedCount)
			fmt.Printf("  Skipped (e.g., exists): %d\n", skippedCount)
			fmt.Printf("  Errors: %d\n", errorCount)
			return nil
		},
	}

	// Add flags for the integrated command
	cmd.Flags().StringVarP(&inputDir, "input-dir", "i", "", "Directory containing audio files to process (required)")
	cmd.Flags().StringVarP(&language, "language", "l", "eng", "Language code for transcription (e.g., eng, hin)")
	cmd.Flags().StringVarP(&sourceTag, "source-tag", "s", "batch_cli", "Source tag to apply to recordings")
	cmd.MarkFlagRequired("input-dir")

	return cmd
}

// ScanAudioFiles scans a directory for supported audio files
func ScanAudioFiles(dir string) ([]string, error) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil, fmt.Errorf("directory does not exist: %s", dir)
	}

	supportedExts := map[string]bool{
		".mp3": true, ".wav": true, ".m4a": true, ".aac": true, ".ogg": true, ".flac": true,
	}
	var audioFiles []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			ext := strings.ToLower(filepath.Ext(path))
			if supportedExts[ext] {
				audioFiles = append(audioFiles, path)
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("error scanning directory: %w", err)
	}
	return audioFiles, nil
}

// GetMimeTypeFromExtension determines MIME type from file extension
func GetMimeTypeFromExtension(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".m4a":
		return "audio/m4a"
	case ".aac":
		return "audio/aac"
	case ".ogg":
		return "audio/ogg"
	case ".flac":
		return "audio/flac"
	default:
		return "application/octet-stream"
	}
}