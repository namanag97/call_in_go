package domain

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// Common errors
var (
	ErrNotFound          = errors.New("entity not found")
	ErrInvalidInput      = errors.New("invalid input")
	ErrUnauthorized      = errors.New("unauthorized access")
	ErrDuplicateEntity   = errors.New("entity already exists")
	ErrProcessingFailed  = errors.New("processing failed")
	ErrInvalidFileFormat = errors.New("invalid file format")
	ErrInvalidStatus     = errors.New("invalid status transition")
)

// Status types
type RecordingStatus string
type TranscriptionStatus string
type AnalysisStatus string
type JobStatus string

// Status constants
const (
	// Recording statuses
	RecordingStatusPending     RecordingStatus = "pending"
	RecordingStatusUploaded    RecordingStatus = "uploaded"
	RecordingStatusProcessing  RecordingStatus = "processing"
	RecordingStatusTranscribed RecordingStatus = "transcribed"
	RecordingStatusAnalyzed    RecordingStatus = "analyzed"
	RecordingStatusCompleted   RecordingStatus = "completed"
	RecordingStatusError       RecordingStatus = "error"

	// Transcription statuses
	TranscriptionStatusPending    TranscriptionStatus = "pending"
	TranscriptionStatusProcessing TranscriptionStatus = "processing"
	TranscriptionStatusCompleted  TranscriptionStatus = "completed"
	TranscriptionStatusError      TranscriptionStatus = "error"

	// Analysis statuses
	AnalysisStatusPending    AnalysisStatus = "pending"
	AnalysisStatusProcessing AnalysisStatus = "processing"
	AnalysisStatusCompleted  AnalysisStatus = "completed"
	AnalysisStatusError      AnalysisStatus = "error"

	// Job statuses
	JobStatusPending   JobStatus = "pending"
	JobStatusProcessing JobStatus = "processing"
	JobStatusCompleted  JobStatus = "completed"
	JobStatusFailed     JobStatus = "failed"
	JobStatusRetrying   JobStatus = "retrying"
)

// User represents a system user
type User struct {
	ID          uuid.UUID  `json:"id"`
	Email       string     `json:"email"`
	PasswordHash string     `json:"-"`
	FullName    string     `json:"fullName"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	LastLoginAt *time.Time `json:"lastLoginAt,omitempty"`
	Roles       []Role     `json:"roles,omitempty"`
}

// Role represents a user role for authorization
type Role struct {
	ID          uuid.UUID    `json:"id"`
	Name        string       `json:"name"`
	Description string       `json:"description,omitempty"`
	Permissions []Permission `json:"permissions,omitempty"`
}

// Permission represents a system permission
type Permission struct {
	ID          uuid.UUID `json:"id"`
	Code        string    `json:"code"`
	Description string    `json:"description,omitempty"`
}

// Recording represents an audio recording file
type Recording struct {
	ID             uuid.UUID        `json:"id"`
	FileName       string           `json:"fileName"`
	FilePath       string           `json:"filePath"`
	FileSize       int64            `json:"fileSize"`
	DurationSeconds *int            `json:"durationSeconds,omitempty"`
	MimeType       string           `json:"mimeType"`
	MD5Hash        string           `json:"md5Hash"`
	CreatedBy      *uuid.UUID       `json:"createdBy,omitempty"`
	CreatedAt      time.Time        `json:"createdAt"`
	UpdatedAt      time.Time        `json:"updatedAt"`
	Source         string           `json:"source,omitempty"`
	Status         RecordingStatus  `json:"status"`
	Metadata       json.RawMessage  `json:"metadata,omitempty"`
	Tags           pq.StringArray   `json:"tags,omitempty"`
	Transcription  *Transcription   `json:"transcription,omitempty"`
	Analyses       []Analysis       `json:"analyses,omitempty"`
}

// Validate validates the recording data
func (r *Recording) Validate() error {
	if r.FileName == "" {
		return errors.New("file name is required")
	}
	if r.FilePath == "" {
		return errors.New("file path is required")
	}
	if r.FileSize <= 0 {
		return errors.New("file size must be greater than 0")
	}
	if r.MimeType == "" {
		return errors.New("mime type is required")
	}
	if r.MD5Hash == "" {
		return errors.New("MD5 hash is required")
	}
	return nil
}

// Transcription represents the text transcript of a recording
type Transcription struct {
	ID            uuid.UUID          `json:"id"`
	RecordingID   uuid.UUID          `json:"recordingId"`
	FullText      string             `json:"fullText,omitempty"`
	Language      string             `json:"language,omitempty"`
	Confidence    float64            `json:"confidence,omitempty"`
	Status        TranscriptionStatus `json:"status"`
	Engine        string             `json:"engine,omitempty"`
	StartedAt     *time.Time         `json:"startedAt,omitempty"`
	CompletedAt   *time.Time         `json:"completedAt,omitempty"`
	Error         string             `json:"error,omitempty"`
	Metadata      json.RawMessage    `json:"metadata,omitempty"`
	Segments      []TranscriptionSegment `json:"segments,omitempty"`
}

// TranscriptionSegment represents a time-aligned segment of the transcript
type TranscriptionSegment struct {
	ID             uuid.UUID `json:"id"`
	TranscriptionID uuid.UUID `json:"transcriptionId"`
	StartTime      float64   `json:"startTime"`
	EndTime        float64   `json:"endTime"`
	Text           string    `json:"text"`
	Speaker        string    `json:"speaker,omitempty"`
	Confidence     float64   `json:"confidence,omitempty"`
}

// Analysis represents an analysis performed on a recording
type Analysis struct {
	ID           uuid.UUID     `json:"id"`
	RecordingID  uuid.UUID     `json:"recordingId"`
	AnalysisType string        `json:"analysisType"`
	Status       AnalysisStatus `json:"status"`
	StartedAt    *time.Time    `json:"startedAt,omitempty"`
	CompletedAt  *time.Time    `json:"completedAt,omitempty"`
	Error        string        `json:"error,omitempty"`
	Results      json.RawMessage `json:"results,omitempty"`
}

// Entity represents a named entity extracted from a transcript
type Entity struct {
	ID             uuid.UUID   `json:"id"`
	TranscriptionID uuid.UUID  `json:"transcriptionId"`
	EntityType     string      `json:"entityType"`
	EntityValue    string      `json:"entityValue"`
	Confidence     float64     `json:"confidence,omitempty"`
	SegmentIDs     []uuid.UUID `json:"segmentIds,omitempty"`
}

// Job represents a background processing job
type Job struct {
	ID           uuid.UUID      `json:"id"`
	JobType      string         `json:"jobType"`
	Status       JobStatus      `json:"status"`
	Priority     int            `json:"priority"`
	Payload      json.RawMessage `json:"payload"`
	MaxAttempts  int            `json:"maxAttempts"`
	Attempts     int            `json:"attempts"`
	LastError    string         `json:"lastError,omitempty"`
	LockedBy     string         `json:"lockedBy,omitempty"`
	LockedAt     *time.Time     `json:"lockedAt,omitempty"`
	CreatedAt    time.Time      `json:"createdAt"`
	UpdatedAt    time.Time      `json:"updatedAt"`
	ScheduledFor time.Time      `json:"scheduledFor"`
}

// Event represents a system event
type Event struct {
	ID         uuid.UUID      `json:"id"`
	EventType  string         `json:"eventType"`
	EntityType string         `json:"entityType"`
	EntityID   uuid.UUID      `json:"entityId"`
	UserID     *uuid.UUID     `json:"userId,omitempty"`
	CreatedAt  time.Time      `json:"createdAt"`
	Data       json.RawMessage `json:"data,omitempty"`
}

// Pagination represents pagination parameters
type Pagination struct {
	Page     int `json:"page"`
	PageSize int `json:"pageSize"`
	Total    int `json:"total,omitempty"`
}

// DefaultPageSize is the default page size for pagination
const DefaultPageSize = 20

// NewPagination creates a new pagination with default values
func NewPagination(page, pageSize int) Pagination {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = DefaultPageSize
	}
	return Pagination{
		Page:     page,
		PageSize: pageSize,
	}
}

// RecordingFilter represents filter criteria for recordings
type RecordingFilter struct {
	Status    []RecordingStatus `json:"status,omitempty"`
	Source    string            `json:"source,omitempty"`
	CreatedBy *uuid.UUID        `json:"createdBy,omitempty"`
	Tags      []string          `json:"tags,omitempty"`
	From      *time.Time        `json:"from,omitempty"`
	To        *time.Time        `json:"to,omitempty"`
	Search    string            `json:"search,omitempty"`
}

// TranscriptionFilter represents filter criteria for transcriptions
type TranscriptionFilter struct {
	Status      []TranscriptionStatus `json:"status,omitempty"`
	RecordingID *uuid.UUID            `json:"recordingId,omitempty"`
	Language    string                `json:"language,omitempty"`
	Engine      string                `json:"engine,omitempty"`
	From        *time.Time            `json:"from,omitempty"`
	To          *time.Time            `json:"to,omitempty"`
}

// AnalysisFilter represents filter criteria for analyses
type AnalysisFilter struct {
	Status       []AnalysisStatus `json:"status,omitempty"`
	RecordingID  *uuid.UUID       `json:"recordingId,omitempty"`
	AnalysisType string           `json:"analysisType,omitempty"`
	From         *time.Time       `json:"from,omitempty"`
	To           *time.Time       `json:"to,omitempty"`
}

// StorageObject represents a storage object (file) in the system
type StorageObject struct {
	Key          string    `json:"key"`
	Size         int64     `json:"size"`
	LastModified time.Time `json:"lastModified"`
	ETag         string    `json:"etag"`
}