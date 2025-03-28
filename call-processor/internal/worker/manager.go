package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/namanag97/call_in_go/call-processor/internal/domain"
	"github.com/namanag97/call_in_go/call-processor/internal/event"
	"github.com/namanag97/call_in_go/call-processor/internal/repository"
	"github.com/namanag97/call_in_go/call-processor/internal/worker"
)

// Service errors
var (
	ErrAnalysisNotFound  = domain.ErrNotFound
	ErrInvalidInput      = domain.ErrInvalidInput
	ErrProcessingFailed  = domain.ErrProcessingFailed
	ErrUnsupportedType   = errors.New("unsupported analysis type")
	ErrNoTranscription   = errors.New("no transcription available")
)

// AnalysisType represents the type of analysis to perform
type AnalysisType string

const (
	// AnalysisTypeSentiment represents sentiment analysis
	AnalysisTypeSentiment AnalysisType = "sentiment"
	// AnalysisTypeEntities represents entity extraction
	AnalysisTypeEntities AnalysisType = "entities"
	// AnalysisTypeTopics represents topic modeling
	AnalysisTypeTopics AnalysisType = "topics"
	// AnalysisTypeKeywords represents keyword extraction
	AnalysisTypeKeywords AnalysisType = "keywords"
	// AnalysisTypeIntents represents intent detection
	AnalysisTypeIntents AnalysisType = "intents"
	// AnalysisTypeSummary represents text summarization
	AnalysisTypeSummary AnalysisType = "summary"
)

// Config contains configuration for the analysis service
type Config struct {
	MaxConcurrentJobs int
	JobMaxRetries     int
	PollInterval      time.Duration
}

// Service provides operations for analyzing transcriptions
type Service struct {
	config                 Config
	recordingRepository    repository.RecordingRepository
	transcriptionRepository repository.TranscriptionRepository
	analysisRepository     repository.AnalysisRepository
	eventPublisher         event.Publisher
	workerManager          worker.Manager
	processors             map[AnalysisType]Processor
	mu                     sync.Mutex
}

// Processor defines the interface for analysis processors
type Processor interface {
	Process(ctx context.Context, transcription *domain.Transcription) (json.RawMessage, error)
	Type() AnalysisType
}

// NewService creates a new analysis service
func NewService(
	config Config,
	recordingRepository repository.RecordingRepository,
	transcriptionRepository repository.TranscriptionRepository,
	analysisRepository repository.AnalysisRepository,
	eventPublisher event.Publisher,
	workerManager worker.Manager,
) *Service {
	s := &Service{
		config:                 config,
		recordingRepository:    recordingRepository,
		transcriptionRepository: transcriptionRepository,
		analysisRepository:     analysisRepository,
		eventPublisher:         eventPublisher,
		workerManager:          workerManager,
		processors:             make(map[AnalysisType]Processor),
	}

	// Register worker handlers
	s.workerManager.RegisterHandler("analysis", s.processAnalysisJob)

	return s
}

// RegisterProcessor registers an analysis processor
func (s *Service) RegisterProcessor(processor Processor) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.processors[processor.Type()] = processor
}

// AnalysisRequest represents a request to analyze a transcription
type AnalysisRequest struct {
	RecordingID  uuid.UUID
	AnalysisType AnalysisType
	UserID       uuid.UUID
	Priority     int
	Config       map[string]interface{}
}

// AnalysisResponse represents the response from an analysis request
type AnalysisResponse struct {
	AnalysisID   uuid.UUID          `json:"analysisId"`
	RecordingID  uuid.UUID          `json:"recordingId"`
	AnalysisType string             `json:"analysisType"`
	Status       domain.AnalysisStatus `json:"status"`
	JobID        uuid.UUID          `json:"jobId"`
}

// StartAnalysis begins the analysis process for a transcription
func (s *Service) StartAnalysis(ctx context.Context, req AnalysisRequest) (*AnalysisResponse, error) {
	// Validate input
	if req.RecordingID == uuid.Nil {
		return nil, ErrInvalidInput
	}

	if req.AnalysisType == "" {
		return nil, ErrInvalidInput
	}

	// Check if processor exists
	s.mu.Lock()
	_, exists := s.processors[req.AnalysisType]
	s.mu.Unlock()
	if !exists {
		return nil, ErrUnsupportedType
	}

	// Check if recording exists
	recording, err := s.recordingRepository.Get(ctx, req.RecordingID)
	if err != nil {
		return nil, fmt.Errorf("failed to get recording: %w", err)
	}

	// Check if transcription exists and is completed
	transcription, err := s.transcriptionRepository.GetByRecordingID(ctx, req.RecordingID)
	if err != nil {
		return nil, ErrNoTranscription
	}

	if transcription.Status != domain.TranscriptionStatusCompleted {
		return nil, ErrNoTranscription
	}

	// Check if analysis already exists
	existingAnalysis, err := s.analysisRepository.GetByRecordingIDAndType(ctx, req.RecordingID, string(req.AnalysisType))
	if err == nil {
		// Analysis already exists
		return &AnalysisResponse{
			AnalysisID:   existingAnalysis.ID,
			RecordingID:  existingAnalysis.RecordingID,
			AnalysisType: existingAnalysis.AnalysisType,
			Status:       existingAnalysis.Status,
		}, nil
	} else if !errors.Is(err, repository.ErrNotFound) {
		// Unexpected error
		return nil, fmt.Errorf("failed to check existing analysis: %w", err)
	}

	// Create analysis entity
	analysisID := uuid.New()
	
	// Marshal config to JSON
	configJSON := []byte("{}")
	if req.Config != nil {
		var err error
		configJSON, err = json.Marshal(req.Config)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal config: %w", err)
		}
	}
	
	analysis := &domain.Analysis{
		ID:           analysisID,
		RecordingID:  req.RecordingID,
		AnalysisType: string(req.AnalysisType),
		Status:       domain.AnalysisStatusPending,
		Results:      nil,
	}

	// Save to database
	err = s.analysisRepository.Create(ctx, analysis)
	if err != nil {
		return nil, fmt.Errorf("failed to create analysis: %w", err)
	}

	// Create job payload
	jobPayload := map[string]interface{}{
		"analysisId":   analysisID.String(),
		"recordingId":  req.RecordingID.String(),
		"analysisType": string(req.AnalysisType),
		"config":       req.Config,
	}

	jobPayloadJSON, err := json.Marshal(jobPayload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal job payload: %w", err)
	}

	// Create job
	job := &domain.Job{
		ID:           uuid.New(),
		JobType:      "analysis",
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
		EventType:  "analysis.started",
		EntityType: "analysis",
		EntityID:   analysisID,
		UserID:     &req.UserID,
		CreatedAt:  time.Now(),
		Data:       configJSON,
	}

	err = s.eventPublisher.Publish(ctx, event)
	if err != nil {
		// Log but continue
		fmt.Printf("Failed to publish start event: %v\n", err)
	}

	return &AnalysisResponse{
		AnalysisID:   analysisID,
		RecordingID:  req.RecordingID,
		AnalysisType: string(req.AnalysisType),
		Status:       domain.AnalysisStatusPending,
		JobID:        job.ID,
	}, nil
}

// GetAnalysis retrieves an analysis by ID
func (s *Service) GetAnalysis(ctx context.Context, id uuid.UUID) (*domain.Analysis, error) {
	return s.analysisRepository.Get(ctx, id)
}

// GetAnalysisByRecordingIDAndType retrieves an analysis by recording ID and type
func (s *Service) GetAnalysisByRecordingIDAndType(ctx context.Context, recordingID uuid.UUID, analysisType string) (*domain.Analysis, error) {
	return s.analysisRepository.GetByRecordingIDAndType(ctx, recordingID, analysisType)
}

// ListAnalysesByRecordingID lists all analyses for a recording
func (s *Service) ListAnalysesByRecordingID(ctx context.Context, recordingID uuid.UUID) ([]*domain.Analysis, error) {
	return s.analysisRepository.ListByRecordingID(ctx, recordingID)
}

// processAnalysisJob processes an analysis job
func (s *Service) processAnalysisJob(ctx context.Context, job *domain.Job) error {
	// Parse job payload
	var payload struct {
		AnalysisID   string                 `json:"analysisId"`
		RecordingID  string                 `json:"recordingId"`
		AnalysisType string                 `json:"analysisType"`
		Config       map[string]interface{} `json:"config"`
	}

	err := json.Unmarshal(job.Payload, &payload)
	if err != nil {
		return fmt.Errorf("failed to unmarshal job payload: %w", err)
	}

	analysisID, err := uuid.Parse(payload.AnalysisID)
	if err != nil {
		return fmt.Errorf("invalid analysis ID: %w", err)
	}

	recordingID, err := uuid.Parse(payload.RecordingID)
	if err != nil {
		return fmt.Errorf("invalid recording ID: %w", err)
	}

	// Update analysis status to processing
	analysis, err := s.analysisRepository.Get(ctx, analysisID)
	if err != nil {
		return fmt.Errorf("failed to get analysis: %w", err)
	}

	startedAt := time.Now()
	analysis.Status = domain.AnalysisStatusProcessing
	analysis.StartedAt = &startedAt

	err = s.analysisRepository.Update(ctx, analysis)
	if err != nil {
		return fmt.Errorf("failed to update analysis status: %w", err)
	}

	// Get transcription
	transcription, err := s.transcriptionRepository.GetByRecordingID(ctx, recordingID)
	if err != nil {
		analysis.Status = domain.AnalysisStatusError
		analysis.Error = "Transcription not found"
		s.analysisRepository.Update(ctx, analysis)
		return fmt.Errorf("failed to get transcription: %w", err)
	}

	// Load segments if available
	segments, err := s.transcriptionRepository.GetSegments(ctx, transcription.ID)
	if err == nil {
		transcription.Segments = segments
	}

	// Get processor
	s.mu.Lock()
	processor, exists := s.processors[AnalysisType(payload.AnalysisType)]
	s.mu.Unlock()
	if !exists {
		analysis.Status = domain.AnalysisStatusError
		analysis.Error = "Processor not found"
		s.analysisRepository.Update(ctx, analysis)
		return ErrUnsupportedType
	}

	// Process the transcription
	results, err := processor.Process(ctx, transcription)
	if err != nil {
		analysis.Status = domain.AnalysisStatusError
		analysis.Error = err.Error()
		s.analysisRepository.Update(ctx, analysis)
		return fmt.Errorf("processing failed: %w", err)
	}

	// Update analysis with results
	completedAt := time.Now()
	analysis.Status = domain.AnalysisStatusCompleted
	analysis.Results = results
	analysis.CompletedAt = &completedAt

	err = s.analysisRepository.Update(ctx, analysis)
	if err != nil {
		return fmt.Errorf("failed to update analysis with results: %w", err)
	}

	// Publish completion event
	resultData := map[string]interface{}{
		"analysisId":   analysisID.String(),
		"recordingId":  recordingID.String(),
		"analysisType": payload.AnalysisType,
		"duration":     completedAt.Sub(startedAt).Seconds(),
	}

	resultDataJSON, err := json.Marshal(resultData)
	if err != nil {
		fmt.Printf("Failed to marshal event data: %v\n", err)
		resultDataJSON = []byte("{}")
	}

	event := &domain.Event{
		ID:         uuid.New(),
		EventType:  "analysis.completed",
		EntityType: "analysis",
		EntityID:   analysisID,
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

// Example of a processor implementation - SentimentProcessor
type SentimentProcessor struct {
	// Dependencies would go here, such as NLP client, etc.
}

// NewSentimentProcessor creates a new sentiment processor
func NewSentimentProcessor() *SentimentProcessor {
	return &SentimentProcessor{}
}

// Type returns the processor type
func (p *SentimentProcessor) Type() AnalysisType {
	return AnalysisTypeSentiment
}

// Process analyzes the sentiment of a transcription
func (p *SentimentProcessor) Process(ctx context.Context, transcription *domain.Transcription) (json.RawMessage, error) {
	// This is a simplified implementation - in a real system, this would use an NLP service
	
	// Example result structure
	result := struct {
		OverallSentiment float64 `json:"overallSentiment"`
		PositiveSegments int     `json:"positiveSegments"`
		NeutralSegments  int     `json:"neutralSegments"`
		NegativeSegments int     `json:"negativeSegments"`
		SegmentSentiments []struct {
			SegmentID  string  `json:"segmentId"`
			StartTime  float64 `json:"startTime"`
			EndTime    float64 `json:"endTime"`
			Text       string  `json:"text"`
			Sentiment  float64 `json:"sentiment"`
			Confidence float64 `json:"confidence"`
		} `json:"segmentSentiments"`
	}{
		OverallSentiment: 0.0,
		PositiveSegments: 0,
		NeutralSegments:  0,
		NegativeSegments: 0,
		SegmentSentiments: []struct {
			SegmentID  string  `json:"segmentId"`
			StartTime  float64 `json:"startTime"`
			EndTime    float64 `json:"endTime"`
			Text       string  `json:"text"`
			Sentiment  float64 `json:"sentiment"`
			Confidence float64 `json:"confidence"`
		}{},
	}

	// Process each segment for sentiment
	var totalSentiment float64
	for _, segment := range transcription.Segments {
		// This would call an NLP service for real sentiment analysis
		// Here we're just using a placeholder algorithm
		sentiment := calculateDummySentiment(segment.Text)
		
		segmentResult := struct {
			SegmentID  string  `json:"segmentId"`
			StartTime  float64 `json:"startTime"`
			EndTime    float64 `json:"endTime"`
			Text       string  `json:"text"`
			Sentiment  float64 `json:"sentiment"`
			Confidence float64 `json:"confidence"`
		}{
			SegmentID:  segment.ID.String(),
			StartTime:  segment.StartTime,
			EndTime:    segment.EndTime,
			Text:       segment.Text,
			Sentiment:  sentiment,
			Confidence: 0.8, // Placeholder
		}
		
		result.SegmentSentiments = append(result.SegmentSentiments, segmentResult)
		totalSentiment += sentiment
		
		// Categorize sentiment
		if sentiment > 0.2 {
			result.PositiveSegments++
		} else if sentiment < -0.2 {
			result.NegativeSegments++
		} else {
			result.NeutralSegments++
		}
	}
	
	// Calculate overall sentiment
	if len(transcription.Segments) > 0 {
		result.OverallSentiment = totalSentiment / float64(len(transcription.Segments))
	}

	// Serialize to JSON
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}

	return resultJSON, nil
}

// calculateDummySentiment calculates a dummy sentiment score for demo purposes
// In a real system, this would use a proper NLP service
func calculateDummySentiment(text string) float64 {
	// This is a very naive implementation - just for demonstration
	// Positive words increase score, negative words decrease it
	positiveWords := []string{"good", "great", "excellent", "happy", "pleased", "satisfied", "thanks", "thank"}
	negativeWords := []string{"bad", "poor", "terrible", "unhappy", "disappointed", "problem", "issue", "sorry"}
	
	words := strings.Fields(strings.ToLower(text))
	var score float64
	
	for _, word := range words {
		for _, positive := range positiveWords {
			if word == positive {
				score += 0.1
			}
		}
		for _, negative := range negativeWords {
			if word == negative {
				score -= 0.1
			}
		}
	}
	
	// Normalize to range [-1, 1]
	if score > 1.0 {
		score = 1.0
	} else if score < -1.0 {
		score = -1.0
	}
	
	return score
}

