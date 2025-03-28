package transcription

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/namanag97/call_in_go/call-processor/internal/domain"
	"github.com/namanag97/call_in_go/call-processor/internal/event"
	"github.com/namanag97/call_in_go/call-processor/internal/repository"
	"github.com/namanag97/call_in_go/call-processor/internal/stt"
	"github.com/namanag97/call_in_go/call-processor/internal/storage"
	"github.com/namanag97/call_in_go/call-processor/internal/worker"
)

// Service errors
var (
	ErrTranscriptionNotFound = domain.ErrNotFound
	ErrInvalidInput          = domain.ErrInvalidInput
	ErrProcessingFailed      = domain.ErrProcessingFailed
)

// Config contains configuration for the transcription service
type Config struct {
	BucketName          string
	DefaultLanguage     string
	DefaultEngine       string
	MaxConcurrentJobs   int
	JobMaxRetries       int
	PollInterval        time.Duration
	TranscriptionTTL    time.Duration
}

// Service provides operations for transcribing audio recordings
type Service struct {
	config                Config
	recordingRepository   repository.RecordingRepository
	transcriptionRepository repository.TranscriptionRepository
	storageClient         storage.Client
	sttClient             stt.Client
	eventPublisher        event.Publisher
	workerManager         worker.Manager
	mu                    sync.Mutex
}

// NewService creates a new transcription service
func NewService(
	config Config,
	recordingRepository repository.RecordingRepository,
	transcriptionRepository repository.TranscriptionRepository,
	storageClient storage.Client,
	sttClient stt.Client,
	eventPublisher event.Publisher,
	workerManager worker.Manager,
) *Service {
	s := &Service{
		config:                config,
		recordingRepository:   recordingRepository,
		transcriptionRepository: transcriptionRepository,
		storageClient:         storageClient,
		sttClient:             sttClient,
		eventPublisher:        eventPublisher,
		workerManager:         workerManager,
	}

	// Register worker handlers
	s.workerManager.RegisterHandler("transcription", s.processTranscriptionJob)

	return s
}

// TranscriptionRequest represents a request to transcribe a recording
type TranscriptionRequest struct {
	RecordingID uuid.UUID
	Language    string
	Engine      string
	UserID      uuid.UUID
	Metadata    map[string]interface{}
	Priority    int
}

// TranscriptionResponse represents the response from a transcription request
type TranscriptionResponse struct {
	TranscriptionID uuid.UUID              `json:"transcriptionId"`
	RecordingID     uuid.UUID              `json:"recordingId"`
	Status          domain.TranscriptionStatus `json:"status"`
	JobID           uuid.UUID              `json:"jobId"`
}

// StartTranscription begins the transcription process for a recording
func (s *Service) StartTranscription(ctx context.Context, req TranscriptionRequest) (*TranscriptionResponse, error) {
	// Validate input
	if req.RecordingID == uuid.Nil {
		return nil, ErrInvalidInput
	}

	// Check if recording exists
	recording, err := s.recordingRepository.Get(ctx, req.RecordingID)
	if err != nil {
		return nil, fmt.Errorf("failed to get recording: %w", err)
	}

	// Check if transcription already exists
	existingTranscription, err := s.transcriptionRepository.GetByRecordingID(ctx, req.RecordingID)
	if err == nil {
		// Transcription already exists
		return &TranscriptionResponse{
			TranscriptionID: existingTranscription.ID,
			RecordingID:     existingTranscription.RecordingID,
			Status:          existingTranscription.Status,
		}, nil
	} else if !errors.Is(err, repository.ErrNotFound) {
		// Unexpected error
		return nil, fmt.Errorf("failed to check existing transcription: %w", err)
	}

	// Set defaults if not provided
	language := req.Language
	if language == "" {
		language = s.config.DefaultLanguage
	}

	engine := req.Engine
	if engine == "" {
		engine = s.config.DefaultEngine
	}

	// Prepare metadata
	metadataJSON := []byte("{}")
	if req.Metadata != nil {
		metadata := req.Metadata
		metadata["requested_at"] = time.Now().Format(time.RFC3339)
		metadata["engine"] = engine
		metadata["language"] = language
		
		var err error
		metadataJSON, err = json.Marshal(metadata)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal metadata: %w", err)
		}
	}

	// Create transcription entity
	transcriptionID := uuid.New()
	transcription := &domain.Transcription{
		ID:          transcriptionID,
		RecordingID: req.RecordingID,
		Language:    language,
		Engine:      engine,
		Status:      domain.TranscriptionStatusPending,
		Metadata:    metadataJSON,
	}

	// Save to database
	err = s.transcriptionRepository.Create(ctx, transcription)
	if err != nil {
		return nil, fmt.Errorf("failed to create transcription: %w", err)
	}

	// Update recording status
	err = s.recordingRepository.UpdateStatus(ctx, req.RecordingID, domain.RecordingStatusProcessing)
	if err != nil {
		// Log but continue
		fmt.Printf("Failed to update recording status: %v\n", err)
	}

	// Create job payload
	jobPayload := map[string]interface{}{
		"transcriptionId": transcriptionID.String(),
		"recordingId":     req.RecordingID.String(),
		"language":        language,
		"engine":          engine,
	}

	jobPayloadJSON, err := json.Marshal(jobPayload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal job payload: %w", err)
	}

	// Create job
	job := &domain.Job{
		ID:           uuid.New(),
		JobType:      "transcription",
		Status:       domain.JobStatusPending,
		Priority:     req.Priority,
		Payload:      jobPayloadJSON,
		MaxAttempts:  s.config.JobMaxRetries,
		ScheduledFor: time.Now(),
	}

	// Enqueue job
	err = s.workerManager.EnqueueJob(ctx, job)
	if err != nil {
		return nil, fmt.Errorf("failed to enqueue job: %w", err)
	}

	// Publish event
	event := &domain.Event{
		ID:         uuid.New(),
		EventType:  "transcription.started",
		EntityType: "transcription",
		EntityID:   transcriptionID,
		UserID:     &req.UserID,
		CreatedAt:  time.Now(),
		Data:       jobPayloadJSON,
	}
	
	err = s.eventPublisher.Publish(ctx, event)
	if err != nil {
		// Log the error but don't fail the request
		fmt.Printf("Failed to publish event: %v\n", err)
	}

	return &TranscriptionResponse{
		TranscriptionID: transcriptionID,
		RecordingID:     req.RecordingID,
		Status:          domain.TranscriptionStatusPending,
		JobID:           job.ID,
	}, nil
}

// GetTranscription retrieves a transcription by ID
func (s *Service) GetTranscription(ctx context.Context, id uuid.UUID) (*domain.Transcription, error) {
	transcription, err := s.transcriptionRepository.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	// Load segments if available
	segments, err := s.transcriptionRepository.GetSegments(ctx, id)
	if err == nil {
		transcription.Segments = segments
	}

	return transcription, nil
}

// GetTranscriptionByRecordingID retrieves a transcription by recording ID
func (s *Service) GetTranscriptionByRecordingID(ctx context.Context, recordingID uuid.UUID) (*domain.Transcription, error) {
	transcription, err := s.transcriptionRepository.GetByRecordingID(ctx, recordingID)
	if err != nil {
		return nil, err
	}

	// Load segments if available
	segments, err := s.transcriptionRepository.GetSegments(ctx, transcription.ID)
	if err == nil {
		transcription.Segments = segments
	}

	return transcription, nil
}

// processTranscriptionJob processes a transcription job
func (s *Service) processTranscriptionJob(ctx context.Context, job *domain.Job) error {
	// Parse job payload
	var payload struct {
		TranscriptionID string `json:"transcriptionId"`
		RecordingID     string `json:"recordingId"`
		Language        string `json:"language"`
		Engine          string `json:"engine"`
	}

	err := json.Unmarshal(job.Payload, &payload)
	if err != nil {
		return fmt.Errorf("failed to unmarshal job payload: %w", err)
	}

	transcriptionID, err := uuid.Parse(payload.TranscriptionID)
	if err != nil {
		return fmt.Errorf("invalid transcription ID: %w", err)
	}

	recordingID, err := uuid.Parse(payload.RecordingID)
	if err != nil {
		return fmt.Errorf("invalid recording ID: %w", err)
	}

	// Update transcription status to processing
	transcription, err := s.transcriptionRepository.Get(ctx, transcriptionID)
	if err != nil {
		return fmt.Errorf("failed to get transcription: %w", err)
	}

	startedAt := time.Now()
	transcription.Status = domain.TranscriptionStatusProcessing
	transcription.StartedAt = &startedAt

	err = s.transcriptionRepository.Update(ctx, transcription)
	if err != nil {
		return fmt.Errorf("failed to update transcription status: %w", err)
	}

	// Get recording
	recording, err := s.recordingRepository.Get(ctx, recordingID)
	if err != nil {
		return fmt.Errorf("failed to get recording: %w", err)
	}

	// Download recording from storage
	downloadOutput, err := s.storageClient.DownloadObject(ctx, &storage.DownloadObjectInput{
		Bucket: s.config.BucketName,
		Key:    recording.FilePath,
	})
	if err != nil {
		return fmt.Errorf("failed to download recording: %w", err)
	}
	defer downloadOutput.Body.Close()

	// Read the file content
	audioData, err := io.ReadAll(downloadOutput.Body)
	if err != nil {
		return fmt.Errorf("failed to read audio data: %w", err)
	}

	// Call STT service
	transcribeInput := &stt.TranscribeInput{
		AudioData:         audioData,
		MimeType:          recording.MimeType,
		Language:          payload.Language,
		IncludeTimestamps: true,
		SpeakerDiarization: true,
	}

	transcribeOutput, err := s.sttClient.Transcribe(ctx, transcribeInput)
	if err != nil {
		transcription.Status = domain.TranscriptionStatusError
		transcription.Error = err.Error()
		s.transcriptionRepository.Update(ctx, transcription)
		return fmt.Errorf("transcription failed: %w", err)
	}

	// Update transcription with results
	completedAt := time.Now()
	transcription.Status = domain.TranscriptionStatusCompleted
	transcription.FullText = transcribeOutput.Text
	transcription.Confidence = transcribeOutput.Confidence
	transcription.CompletedAt = &completedAt

	err = s.transcriptionRepository.Update(ctx, transcription)
	if err != nil {
		return fmt.Errorf("failed to update transcription with results: %w", err)
	}

	// Save segments
	for _, segment := range transcribeOutput.Segments {
		transcriptionSegment := &domain.TranscriptionSegment{
			ID:             uuid.New(),
			TranscriptionID: transcriptionID,
			StartTime:      segment.StartTime,
			EndTime:        segment.EndTime,
			Text:           segment.Text,
			Speaker:        segment.Speaker,
			Confidence:     segment.Confidence,
		}

		err = s.transcriptionRepository.AddSegment(ctx, transcriptionSegment)
		if err != nil {
			// Log but continue
			fmt.Printf("Failed to save segment: %v\n", err)
		}
	}

	// Update recording status
	err = s.recordingRepository.UpdateStatus(ctx, recordingID, domain.RecordingStatusTranscribed)
	if err != nil {
		// Log but continue
		fmt.Printf("Failed to update recording status: %v\n", err)
	}

	// Publish completion event
	resultData := map[string]interface{}{
		"transcriptionId":  transcriptionID.String(),
		"recordingId":      recordingID.String(),
		"duration":         completedAt.Sub(startedAt).Seconds(),
		"segmentCount":     len(transcribeOutput.Segments),
		"confidence":       transcribeOutput.Confidence,
		"wordCount":        transcribeOutput.WordCount,
		"processingTimeMs": transcribeOutput.ProcessingTimeMs,
	}

	resultDataJSON, err := json.Marshal(resultData)
	if err != nil {
		fmt.Printf("Failed to marshal event data: %v\n", err)
		resultDataJSON = []byte("{}")
	}

	event := &domain.Event{
		ID:         uuid.New(),
		EventType:  "transcription.completed",
		EntityType: "transcription",
		EntityID:   transcriptionID,
		CreatedAt:  time.Now(),
		Data:       resultDataJSON,
	}
	
	err = s.eventPublisher.Publish(ctx, event)
	if err != nil {
		// Log but continue
		fmt.Printf("Failed to publish completion event: %v\n", err)
	}

	return nil
}