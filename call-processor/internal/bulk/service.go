package bulk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/namanag97/call_in_go/call-processor/internal/domain"
	"github.com/namanag97/call_in_go/call-processor/internal/event"
	"github.com/namanag97/call_in_go/call-processor/internal/ingestion"
	"github.com/namanag97/call_in_go/call-processor/internal/repository"
	"github.com/namanag97/call_in_go/call-processor/internal/transcription"
	"github.com/namanag97/call_in_go/call-processor/internal/worker"
)

// Service errors
var (
	ErrInvalidBulkRequest = errors.New("invalid bulk request")
	ErrBulkProcessFailed  = errors.New("bulk processing failed")
)

// Config contains configuration for the bulk operations service
type Config struct {
	MaxItemsPerBulkRequest int
	BulkWorkers            int
	BatchSize              int
	DefaultPriority        int
}

// Service provides operations for bulk processing
type Service struct {
	config                 Config
	jobRepo                repository.JobRepository
	recordingRepo          repository.RecordingRepository
	ingestionService       *ingestion.Service
	transcriptionService   *transcription.Service
	eventPublisher         event.Publisher
	workerManager          worker.Manager
}

// NewService creates a new bulk operations service
func NewService(
	config Config,
	jobRepo repository.JobRepository,
	recordingRepo repository.RecordingRepository,
	ingestionService *ingestion.Service,
	transcriptionService *transcription.Service,
	eventPublisher event.Publisher,
	workerManager worker.Manager,
) *Service {
	s := &Service{
		config:                config,
		jobRepo:               jobRepo,
		recordingRepo:         recordingRepo,
		ingestionService:      ingestionService,
		transcriptionService:  transcriptionService,
		eventPublisher:        eventPublisher,
		workerManager:         workerManager,
	}

	// Register worker handlers
	s.workerManager.RegisterHandler("bulk.upload", s.processBulkUploadJob)
	s.workerManager.RegisterHandler("bulk.transcribe", s.processBulkTranscribeJob)

	return s
}

// BulkUploadRequest represents a request for bulk upload operations
type BulkUploadRequest struct {
	Files       []BulkUploadFile      `json:"files"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	Source      string                 `json:"source,omitempty"`
	Tags        []string               `json:"tags,omitempty"`
	CallbackURL string                 `json:"callbackUrl,omitempty"`
	UserID      uuid.UUID              `json:"userId"`
}

// BulkUploadFile represents a file in a bulk upload request
type BulkUploadFile struct {
	FilePath   string                 `json:"filePath"`
	FileName   string                 `json:"fileName"`
	FileMetadata map[string]interface{} `json:"fileMetadata,omitempty"`
}

// BulkTranscribeRequest represents a request for bulk transcription operations
type BulkTranscribeRequest struct {
	RecordingIDs []uuid.UUID           `json:"recordingIds"`
	Language     string                 `json:"language,omitempty"`
	Engine       string                 `json:"engine,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
	UserID       uuid.UUID              `json:"userId"`
}

// BulkJobResponse represents the response from a bulk operation request
type BulkJobResponse struct {
	JobID      uuid.UUID `json:"jobId"`
	JobType    string    `json:"jobType"`
	TotalItems int       `json:"totalItems"`
	Status     string    `json:"status"`
}

// StartBulkUpload starts a bulk upload operation
func (s *Service) StartBulkUpload(ctx context.Context, req BulkUploadRequest) (*BulkJobResponse, error) {
	// Validate request
	if len(req.Files) == 0 {
		return nil, ErrInvalidBulkRequest
	}

	if s.config.MaxItemsPerBulkRequest > 0 && len(req.Files) > s.config.MaxItemsPerBulkRequest {
		return nil, fmt.Errorf("number of files (%d) exceeds maximum allowed (%d)", 
			len(req.Files), s.config.MaxItemsPerBulkRequest)
	}

	// Create job payload
	jobPayload := map[string]interface{}{
		"files":       req.Files,
		"metadata":    req.Metadata,
		"source":      req.Source,
		"tags":        req.Tags,
		"callbackUrl": req.CallbackURL,
		"userId":      req.UserID.String(),
	}

	jobPayloadJSON, err := json.Marshal(jobPayload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal job payload: %w", err)
	}

	// Create job
	jobID := uuid.New()
	job := &domain.Job{
		ID:           jobID,
		JobType:      "bulk.upload",
		Status:       domain.JobStatusPending,
		Priority:     s.config.DefaultPriority,
		Payload:      jobPayloadJSON,
		MaxAttempts:  3,
		ScheduledFor: time.Now(),
	}

	// Enqueue job
	err = s.workerManager.EnqueueJob(ctx, job)
	if err != nil {
		return nil, fmt.Errorf("failed to enqueue job: %w", err)
	}

	// Return response
	return &BulkJobResponse{
		JobID:      jobID,
		JobType:    "bulk.upload",
		TotalItems: len(req.Files),
		Status:     string(domain.JobStatusPending),
	}, nil
}

// StartBulkTranscribe starts a bulk transcription operation
func (s *Service) StartBulkTranscribe(ctx context.Context, req BulkTranscribeRequest) (*BulkJobResponse, error) {
	// Validate request
	if len(req.RecordingIDs) == 0 {
		return nil, ErrInvalidBulkRequest
	}

	if s.config.MaxItemsPerBulkRequest > 0 && len(req.RecordingIDs) > s.config.MaxItemsPerBulkRequest {
		return nil, fmt.Errorf("number of recordings (%d) exceeds maximum allowed (%d)", 
			len(req.RecordingIDs), s.config.MaxItemsPerBulkRequest)
	}

	// Validate that all recordings exist
	for _, recordingID := range req.RecordingIDs {
		_, err := s.recordingRepo.Get(ctx, recordingID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return nil, fmt.Errorf("recording %s not found", recordingID)
			}
			return nil, fmt.Errorf("error validating recording %s: %w", recordingID, err)
		}
	}

	// Create job payload
	recordingIDStrings := make([]string, len(req.RecordingIDs))
	for i, id := range req.RecordingIDs {
		recordingIDStrings[i] = id.String()
	}

	jobPayload := map[string]interface{}{
		"recordingIds": recordingIDStrings,
		"language":     req.Language,
		"engine":       req.Engine,
		"metadata":     req.Metadata,
		"userId":       req.UserID.String(),
	}

	jobPayloadJSON, err := json.Marshal(jobPayload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal job payload: %w", err)
	}

	// Create job
	jobID := uuid.New()
	job := &domain.Job{
		ID:           jobID,
		JobType:      "bulk.transcribe",
		Status:       domain.JobStatusPending,
		Priority:     s.config.DefaultPriority,
		Payload:      jobPayloadJSON,
		MaxAttempts:  3,
		ScheduledFor: time.Now(),
	}

	// Enqueue job
	err = s.workerManager.EnqueueJob(ctx, job)
	if err != nil {
		return nil, fmt.Errorf("failed to enqueue job: %w", err)
	}

	// Return response
	return &BulkJobResponse{
		JobID:      jobID,
		JobType:    "bulk.transcribe",
		TotalItems: len(req.RecordingIDs),
		Status:     string(domain.JobStatusPending),
	}, nil
}

// GetBulkJobStatus gets the status of a bulk job
func (s *Service) GetBulkJobStatus(ctx context.Context, jobID uuid.UUID) (*domain.Job, error) {
	return s.jobRepo.Get(ctx, jobID)
}

// processBulkUploadJob processes a bulk upload job
func (s *Service) processBulkUploadJob(ctx context.Context, job *domain.Job) error {
	// Parse job payload
	var payload struct {
		Files       []BulkUploadFile      `json:"files"`
		Metadata    map[string]interface{} `json:"metadata"`
		Source      string                 `json:"source"`
		Tags        []string               `json:"tags"`
		CallbackURL string                 `json:"callbackUrl"`
		UserID      string                 `json:"userId"`
	}

	err := json.Unmarshal(job.Payload, &payload)
	if err != nil {
		return fmt.Errorf("failed to unmarshal job payload: %w", err)
	}

	userID, err := uuid.Parse(payload.UserID)
	if err != nil {
		return fmt.Errorf("invalid user ID: %w", err)
	}

	// Update job status and track progress
	job.Status = domain.JobStatusProcessing
	job.Attempts++
	err = s.jobRepo.Update(ctx, job)
	if err != nil {
		return fmt.Errorf("failed to update job status: %w", err)
	}

	// Process files in batches
	batchSize := s.config.BatchSize
	if batchSize <= 0 {
		batchSize = 10 // Default batch size
	}

	totalFiles := len(payload.Files)
	processedCount := 0
	errorCount := 0
	results := make([]map[string]interface{}, totalFiles)

	for i := 0; i < totalFiles; i += batchSize {
		end := i + batchSize
		if end > totalFiles {
			end = totalFiles
		}

		batch := payload.Files[i:end]
		batchResults := s.processBatch(ctx, batch, payload.Metadata, payload.Source, payload.Tags, userID)

		// Store results
		for j, result := range batchResults {
			results[i+j] = result
			if result["error"] != nil {
				errorCount++
			} else {
				processedCount++
			}
		}

		// Update job progress
		progress := float64(i+len(batch)) / float64(totalFiles)
		s.updateJobProgress(ctx, job.ID, progress, processedCount, errorCount)
	}

	// Update job status
	if errorCount == totalFiles {
		job.Status = domain.JobStatusFailed
	} else if errorCount > 0 {
		job.Status = domain.JobStatusCompleted // Partial success
	} else {
		job.Status = domain.JobStatusCompleted
	}

	// Update job with results
	resultsJSON, err := json.Marshal(results)
	if err != nil {
		return fmt.Errorf("failed to marshal results: %w", err)
	}

	job.Payload = resultsJSON
	job.UpdatedAt = time.Now()
	err = s.jobRepo.Update(ctx, job)
	if err != nil {
		return fmt.Errorf("failed to update job with results: %w", err)
	}

	// Publish event
	event := &domain.Event{
		ID:         uuid.New(),
		EventType:  "bulk.upload.completed",
		EntityType: "job",
		EntityID:   job.ID,
		UserID:     &userID,
		CreatedAt:  time.Now(),
		Data:       resultsJSON,
	}

	err = s.eventPublisher.Publish(ctx, event)
	if err != nil {
		// Log but continue
		fmt.Printf("Failed to publish event: %v\n", err)
	}

	return nil
}

// processBatch processes a batch of files
func (s *Service) processBatch(
	ctx context.Context,
	files []BulkUploadFile,
	globalMetadata map[string]interface{},
	source string,
	tags []string,
	userID uuid.UUID,
) []map[string]interface{} {
	results := make([]map[string]interface{}, len(files))
	var wg sync.WaitGroup
	resultsMu := sync.Mutex{}

	for i, file := range files {
		wg.Add(1)
		go func(index int, fileInfo BulkUploadFile) {
			defer wg.Done()

			result := make(map[string]interface{})
			result["fileName"] = fileInfo.FileName
			result["filePath"] = fileInfo.FilePath

			// Merge metadata
			metadata := make(map[string]interface{})
			for k, v := range globalMetadata {
				metadata[k] = v
			}
			for k, v := range fileInfo.FileMetadata {
				metadata[k] = v
			}

			// Process file (simplified for now)
			// In a real implementation, this would fetch the file from the source location
			// For now, we'll just create a recording entry in the database
			
			recording := &domain.Recording{
				ID:        uuid.New(),
				FileName:  fileInfo.FileName,
				FilePath:  fileInfo.FilePath, // In a real implementation, this would be the storage path
				FileSize:  0,                 // Would be set after actual file processing
				MimeType:  "audio/wav",       // Would be determined from actual file
				MD5Hash:   "",                // Would be calculated from actual file
				CreatedBy: &userID,
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
				Source:    source,
				Status:    domain.RecordingStatusUploaded,
				Metadata:  []byte("{}"), // Would be set from merged metadata
				Tags:      tags,
			}

			err := s.recordingRepo.Create(ctx, recording)
			if err != nil {
				result["error"] = err.Error()
				result["status"] = "failed"
			} else {
				result["recordingId"] = recording.ID.String()
				result["status"] = "success"
			}

			// Store result
			resultsMu.Lock()
			results[index] = result
			resultsMu.Unlock()
		}(i, file)
	}

	wg.Wait()
	return results
}

// updateJobProgress updates the progress of a job
func (s *Service) updateJobProgress(ctx context.Context, jobID uuid.UUID, progress float64, processed, errors int) {
	// This would update a progress field in the job entity
	// For now, we'll just log the progress
	fmt.Printf("Job %s progress: %.2f%% (processed: %d, errors: %d)\n",
		jobID, progress*100, processed, errors)
}

// processBulkTranscribeJob processes a bulk transcription job
func (s *Service) processBulkTranscribeJob(ctx context.Context, job *domain.Job) error {
	// Parse job payload
	var payload struct {
		RecordingIDs []string               `json:"recordingIds"`
		Language     string                 `json:"language"`
		Engine       string                 `json:"engine"`
		Metadata     map[string]interface{} `json:"metadata"`
		UserID       string                 `json:"userId"`
	}

	err := json.Unmarshal(job.Payload, &payload)
	if err != nil {
		return fmt.Errorf("failed to unmarshal job payload: %w", err)
	}

	userID, err := uuid.Parse(payload.UserID)
	if err != nil {
		return fmt.Errorf("invalid user ID: %w", err)
	}

	// Update job status and track progress
	job.Status = domain.JobStatusProcessing
	job.Attempts++
	err = s.jobRepo.Update(ctx, job)
	if err != nil {
		return fmt.Errorf("failed to update job status: %w", err)
	}

	// Process recordings in batches
	recordingIDs := make([]uuid.UUID, len(payload.RecordingIDs))
	for i, idStr := range payload.RecordingIDs {
		id, err := uuid.Parse(idStr)
		if err != nil {
			return fmt.Errorf("invalid recording ID at index %d: %w", i, err)
		}
		recordingIDs[i] = id
	}

	batchSize := s.config.BatchSize
	if batchSize <= 0 {
		batchSize = 10 // Default batch size
	}

	totalRecordings := len(recordingIDs)
	processedCount := 0
	errorCount := 0
	results := make([]map[string]interface{}, totalRecordings)

	for i := 0; i < totalRecordings; i += batchSize {
		end := i + batchSize
		if end > totalRecordings {
			end = totalRecordings
		}

		batch := recordingIDs[i:end]
		batchResults := s.processTranscriptionBatch(
			ctx, batch, payload.Language, payload.Engine, payload.Metadata, userID)

		// Store results
		for j, result := range batchResults {
			results[i+j] = result
			if result["error"] != nil {
				errorCount++
			} else {
				processedCount++
			}
		}

		// Update job progress
		progress := float64(i+len(batch)) / float64(totalRecordings)
		s.updateJobProgress(ctx, job.ID, progress, processedCount, errorCount)
	}

	// Update job status
	if errorCount == totalRecordings {
		job.Status = domain.JobStatusFailed
	} else if errorCount > 0 {
		job.Status = domain.JobStatusCompleted // Partial success
	} else {
		job.Status = domain.JobStatusCompleted
	}

	// Update job with results
	resultsJSON, err := json.Marshal(results)
	if err != nil {
		return fmt.Errorf("failed to marshal results: %w", err)
	}

	job.Payload = resultsJSON
	job.UpdatedAt = time.Now()
	err = s.jobRepo.Update(ctx, job)
	if err != nil {
		return fmt.Errorf("failed to update job with results: %w", err)
	}

	// Publish event
	event := &domain.Event{
		ID:         uuid.New(),
		EventType:  "bulk.transcribe.completed",
		EntityType: "job",
		EntityID:   job.ID,
		UserID:     &userID,
		CreatedAt:  time.Now(),
		Data:       resultsJSON,
	}

	err = s.eventPublisher.Publish(ctx, event)
	if err != nil {
		// Log but continue
		fmt.Printf("Failed to publish event: %v\n", err)
	}

	return nil
}

// processTranscriptionBatch processes a batch of transcription requests
func (s *Service) processTranscriptionBatch(
	ctx context.Context,
	recordingIDs []uuid.UUID,
	language string,
	engine string,
	metadata map[string]interface{},
	userID uuid.UUID,
) []map[string]interface{} {
	results := make([]map[string]interface{}, len(recordingIDs))
	var wg sync.WaitGroup
	resultsMu := sync.Mutex{}

	for i, recordingID := range recordingIDs {
		wg.Add(1)
		go func(index int, id uuid.UUID) {
			defer wg.Done()

			result := make(map[string]interface{})
			result["recordingId"] = id.String()

			// Create transcription request
			req := transcription.TranscriptionRequest{
				RecordingID: id,
				Language:    language,
				Engine:      engine,
				UserID:      userID,
				Metadata:    metadata,
				Priority:    0, // Use default priority
			}

			// Start transcription
			response, err := s.transcriptionService.StartTranscription(ctx, req)
			if err != nil {
				result["error"] = err.Error()
				result["status"] = "failed"
			} else {
				result["transcriptionId"] = response.TranscriptionID.String()
				result["status"] = response.Status
				result["jobId"] = response.JobID.String()
			}

			// Store result
			resultsMu.Lock()
			results[index] = result
			resultsMu.Unlock()
		}(i, recordingID)
	}

	wg.Wait()
	return results
}