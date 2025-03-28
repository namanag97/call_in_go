package ingestion

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
	"github.com/your-org/call-processing/domain"
	"github.com/your-org/call-processing/event"
	"github.com/your-org/call-processing/repository"
	"github.com/your-org/call-processing/storage"
)

// Service errors
var (
	ErrInvalidFile      = errors.New("invalid file")
	ErrFileTooBig       = errors.New("file too large")
	ErrUnsupportedType  = errors.New("unsupported file type")
	ErrUploadFailed     = errors.New("upload failed")
	ErrDuplicateFile    = errors.New("duplicate file")
)

// Config contains configuration for the ingestion service
type Config struct {
	MaxFileSize        int64           // Maximum file size in bytes
	AllowedMimeTypes   []string        // List of allowed MIME types
	BucketName         string          // S3 bucket name
	DuplicateCheck     bool            // Whether to check for duplicate files
	DefaultCallType    string          // Default call type for metadata
	DefaultSource      string          // Default source for recordings
}

// Service provides operations for ingesting audio recordings
type Service struct {
	config              Config
	recordingRepository repository.RecordingRepository
	storageClient       storage.Client
	eventPublisher      event.Publisher
}

// NewService creates a new ingestion service
func NewService(
	config Config,
	recordingRepository repository.RecordingRepository,
	storageClient storage.Client,
	eventPublisher event.Publisher,
) *Service {
	return &Service{
		config:              config,
		recordingRepository: recordingRepository,
		storageClient:       storageClient,
		eventPublisher:      eventPublisher,
	}
}

// UploadRequest represents a request to upload a recording
type UploadRequest struct {
	File         multipart.File
	FileHeader   *multipart.FileHeader
	UserID       uuid.UUID
	Metadata     map[string]interface{}
	Source       string
	Tags         []string
	CallbackURL  string // Optional callback URL for notification
}

// UploadResponse represents the response from an upload operation
type UploadResponse struct {
	RecordingID  uuid.UUID              `json:"recordingId"`
	FileName     string                 `json:"fileName"`
	FileSize     int64                  `json:"fileSize"`
	MimeType     string                 `json:"mimeType"`
	MD5Hash      string                 `json:"md5Hash"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
	Status       domain.RecordingStatus `json:"status"`
	DuplicateOf  *uuid.UUID             `json:"duplicateOf,omitempty"`
}

// ProcessUpload processes a file upload
func (s *Service) ProcessUpload(ctx context.Context, req UploadRequest) (*UploadResponse, error) {
	// Validate the file
	if req.File == nil || req.FileHeader == nil {
		return nil, ErrInvalidFile
	}

	// Check file size
	if req.FileHeader.Size > s.config.MaxFileSize {
		return nil, ErrFileTooBig
	}

	// Check MIME type
	mimeType := req.FileHeader.Header.Get("Content-Type")
	if !s.isAllowedMimeType(mimeType) {
		return nil, ErrUnsupportedType
	}

	// Read the file for hash calculation
	fileBytes, err := io.ReadAll(req.File)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// Calculate MD5 hash
	hash := md5.Sum(fileBytes)
	md5Hash := hex.EncodeToString(hash[:])

	// Check for duplicates if enabled
	var duplicateID *uuid.UUID
	if s.config.DuplicateCheck {
		duplicate, err := s.recordingRepository.FindByHash(ctx, md5Hash)
		if err == nil {
			// Found a duplicate
			duplicateID = &duplicate.ID
			return &UploadResponse{
				RecordingID: duplicate.ID,
				FileName:    duplicate.FileName,
				FileSize:    duplicate.FileSize,
				MimeType:    duplicate.MimeType,
				MD5Hash:     duplicate.MD5Hash,
				Status:      duplicate.Status,
				DuplicateOf: duplicateID,
			}, ErrDuplicateFile
		} else if !errors.Is(err, repository.ErrNotFound) {
			// Unexpected error occurred
			return nil, fmt.Errorf("failed to check for duplicates: %w", err)
		}
	}

	// Generate a unique filename
	fileExt := filepath.Ext(req.FileHeader.Filename)
	uniqueFileName := fmt.Sprintf("%s%s", uuid.New().String(), fileExt)
	
	// Define the S3 key (path)
	now := time.Now()
	s3Key := fmt.Sprintf("recordings/%d/%02d/%02d/%s", 
		now.Year(), now.Month(), now.Day(), uniqueFileName)

	// Upload to S3
	_, err = s.storageClient.UploadObject(ctx, &storage.UploadObjectInput{
		Bucket: s.config.BucketName,
		Key:    s3Key,
		Body:   bytes.NewReader(fileBytes),
		Size:   req.FileHeader.Size,
		Metadata: map[string]string{
			"Content-Type": mimeType,
			"Original-Filename": req.FileHeader.Filename,
			"Upload-Date": now.Format(time.RFC3339),
			"User-ID": req.UserID.String(),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to upload to storage: %w", err)
	}

	// Prepare metadata for storage
	metadataJSON, err := prepareMetadata(req.Metadata, s.config.DefaultCallType)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare metadata: %w", err)
	}

	// Create recording record
	recordingID := uuid.New()
	recording := &domain.Recording{
		ID:             recordingID,
		FileName:       req.FileHeader.Filename,
		FilePath:       s3Key,
		FileSize:       req.FileHeader.Size,
		MimeType:       mimeType,
		MD5Hash:        md5Hash,
		CreatedBy:      &req.UserID,
		CreatedAt:      now,
		UpdatedAt:      now,
		Source:         getSource(req.Source, s.config.DefaultSource),
		Status:         domain.RecordingStatusUploaded,
		Metadata:       metadataJSON,
		Tags:           req.Tags,
	}

	// Save to database
	err = s.recordingRepository.Create(ctx, recording)
	if err != nil {
		return nil, fmt.Errorf("failed to save recording: %w", err)
	}

	// Publish event
	event := &domain.Event{
		ID:         uuid.New(),
		EventType:  "recording.uploaded",
		EntityType: "recording",
		EntityID:   recordingID,
		UserID:     &req.UserID,
		CreatedAt:  now,
		Data:       metadataJSON,
	}
	
	err = s.eventPublisher.Publish(ctx, event)
	if err != nil {
		// Log the error but don't fail the upload
		fmt.Printf("Failed to publish event: %v\n", err)
	}

	// Return the response
	return &UploadResponse{
		RecordingID:  recordingID,
		FileName:     req.FileHeader.Filename,
		FileSize:     req.FileHeader.Size,
		MimeType:     mimeType,
		MD5Hash:      md5Hash,
		Metadata:     req.Metadata,
		Status:       domain.RecordingStatusUploaded,
	}, nil
}

// GetRecording retrieves a recording by ID
func (s *Service) GetRecording(ctx context.Context, id uuid.UUID) (*domain.Recording, error) {
	return s.recordingRepository.Get(ctx, id)
}

// GetPresignedURL generates a pre-signed URL for downloading a recording
func (s *Service) GetPresignedURL(ctx context.Context, id uuid.UUID, expiresIn time.Duration) (string, error) {
	recording, err := s.recordingRepository.Get(ctx, id)
	if err != nil {
		return "", err
	}

	presignedURL, err := s.storageClient.GetPresignedURL(ctx, &storage.GetPresignedURLInput{
		Bucket:  s.config.BucketName,
		Key:     recording.FilePath,
		Expires: expiresIn,
	})
	if err != nil {
		return "", fmt.Errorf("failed to generate presigned URL: %w", err)
	}

	return presignedURL, nil
}

// isAllowedMimeType checks if the provided MIME type is allowed
func (s *Service) isAllowedMimeType(mimeType string) bool {
	for _, allowed := range s.config.AllowedMimeTypes {
		if allowed == mimeType {
			return true
		}
	}
	return false
}

// prepareMetadata prepares the metadata for storage
func prepareMetadata(metadata map[string]interface{}, defaultCallType string) ([]byte, error) {
	// Ensure metadata has call_type
	if metadata == nil {
		metadata = make(map[string]interface{})
	}
	
	if _, ok := metadata["call_type"]; !ok {
		metadata["call_type"] = defaultCallType
	}
	
	// Add timestamp
	metadata["ingested_at"] = time.Now().Format(time.RFC3339)
	
	// Marshal to JSON
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return nil, err
	}
	
	return metadataJSON, nil
}

// getSource returns the source to use
func getSource(requestSource, defaultSource string) string {
	if requestSource != "" {
		return requestSource
	}
	return defaultSource
}