package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/namanag97/call_in_go/call-processor/internal/domain"
)

// Repository errors
var (
	ErrNotFound    = errors.New("entity not found")
	ErrInvalidID   = errors.New("invalid ID")
	ErrTransaction = errors.New("transaction error")
)

// IsNotFound checks if the error is an ErrNotFound error
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}

// RecordingRepository defines the interface for recording data access
type RecordingRepository interface {
	Create(ctx context.Context, recording *domain.Recording) error
	Get(ctx context.Context, id uuid.UUID) (*domain.Recording, error)
	Update(ctx context.Context, recording *domain.Recording) error
	Delete(ctx context.Context, id uuid.UUID) error
	List(ctx context.Context, filter domain.RecordingFilter, pagination domain.Pagination) ([]*domain.Recording, int, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status domain.RecordingStatus) error
	FindByHash(ctx context.Context, hash string) (*domain.Recording, error)
}

// TranscriptionRepository defines the interface for transcription data access
type TranscriptionRepository interface {
	Create(ctx context.Context, transcription *domain.Transcription) error
	Get(ctx context.Context, id uuid.UUID) (*domain.Transcription, error)
	GetByRecordingID(ctx context.Context, recordingID uuid.UUID) (*domain.Transcription, error)
	Update(ctx context.Context, transcription *domain.Transcription) error
	Delete(ctx context.Context, id uuid.UUID) error
	List(ctx context.Context, filter domain.TranscriptionFilter, pagination domain.Pagination) ([]*domain.Transcription, int, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status domain.TranscriptionStatus) error
	AddSegment(ctx context.Context, segment *domain.TranscriptionSegment) error
	GetSegments(ctx context.Context, transcriptionID uuid.UUID) ([]domain.TranscriptionSegment, error)
	GetRecordingByFilename(ctx context.Context, filename string) ([]*domain.Recording, int, error)
	CreateRecording(ctx context.Context, recording *domain.Recording) error
}

// AnalysisRepository defines the interface for analysis data access
type AnalysisRepository interface {
	Create(ctx context.Context, analysis *domain.Analysis) error
	Get(ctx context.Context, id uuid.UUID) (*domain.Analysis, error)
	GetByRecordingIDAndType(ctx context.Context, recordingID uuid.UUID, analysisType string) (*domain.Analysis, error)
	Update(ctx context.Context, analysis *domain.Analysis) error
	Delete(ctx context.Context, id uuid.UUID) error
	List(ctx context.Context, filter domain.AnalysisFilter, pagination domain.Pagination) ([]*domain.Analysis, int, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status domain.AnalysisStatus) error
	ListByRecordingID(ctx context.Context, recordingID uuid.UUID) ([]*domain.Analysis, error)
}

// UserRepository defines the interface for user data access
type UserRepository interface {
	Create(ctx context.Context, user *domain.User) error
	Get(ctx context.Context, id uuid.UUID) (*domain.User, error)
	GetByEmail(ctx context.Context, email string) (*domain.User, error)
	Update(ctx context.Context, user *domain.User) error
	Delete(ctx context.Context, id uuid.UUID) error
	List(ctx context.Context, pagination domain.Pagination) ([]*domain.User, int, error)
	UpdateLastLogin(ctx context.Context, id uuid.UUID) error
}

// JobRepository defines the interface for job data access
type JobRepository interface {
	Create(ctx context.Context, job *domain.Job) error
	Get(ctx context.Context, id uuid.UUID) (*domain.Job, error)
	Update(ctx context.Context, job *domain.Job) error
	Delete(ctx context.Context, id uuid.UUID) error
	List(ctx context.Context, jobType string, status domain.JobStatus, limit int) ([]*domain.Job, error)
	AcquireJobs(ctx context.Context, workerID string, jobTypes []string, limit int) ([]*domain.Job, error)
	MarkComplete(ctx context.Context, id uuid.UUID) error
	MarkFailed(ctx context.Context, id uuid.UUID, err error) error
	ReleaseJob(ctx context.Context, id uuid.UUID) error
	CountByStatus(ctx context.Context, status domain.JobStatus) (int, error)
	FindStuckJobs(ctx context.Context, status domain.JobStatus, olderThan time.Time) ([]*domain.Job, error)
}

// EventRepository defines the interface for event data access
type EventRepository interface {
	Create(ctx context.Context, event *domain.Event) error
	Get(ctx context.Context, id uuid.UUID) (*domain.Event, error)
	List(ctx context.Context, entityType string, entityID uuid.UUID, pagination domain.Pagination) ([]*domain.Event, int, error)
	ListByType(ctx context.Context, eventType string, pagination domain.Pagination) ([]*domain.Event, int, error)
}

// PostgresRecordingRepository implements RecordingRepository using PostgreSQL
type PostgresRecordingRepository struct {
	db *pgxpool.Pool
}

// NewPostgresRecordingRepository creates a new PostgresRecordingRepository
func NewPostgresRecordingRepository(db *pgxpool.Pool) *PostgresRecordingRepository {
	return &PostgresRecordingRepository{db: db}
}

// Create creates a new recording in the database
func (r *PostgresRecordingRepository) Create(ctx context.Context, recording *domain.Recording) error {
	if recording.ID == uuid.Nil {
		recording.ID = uuid.New()
	}
	
	now := time.Now()
	recording.CreatedAt = now
	recording.UpdatedAt = now
	
	query := `
		INSERT INTO recordings (
			id, file_name, file_path, file_size, duration_seconds, mime_type, md5_hash, 
			created_by, created_at, updated_at, source, status, metadata, tags
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`
	
	_, err := r.db.Exec(ctx, query,
		recording.ID, recording.FileName, recording.FilePath, recording.FileSize,
		recording.DurationSeconds, recording.MimeType, recording.MD5Hash,
		recording.CreatedBy, recording.CreatedAt, recording.UpdatedAt,
		recording.Source, recording.Status, recording.Metadata, recording.Tags,
	)
	
	return err
}

// Get retrieves a recording by ID
func (r *PostgresRecordingRepository) Get(ctx context.Context, id uuid.UUID) (*domain.Recording, error) {
	if id == uuid.Nil {
		return nil, ErrInvalidID
	}
	
	query := `
		SELECT 
			id, file_name, file_path, file_size, duration_seconds, mime_type, md5_hash, 
			created_by, created_at, updated_at, source, status, metadata, tags
		FROM recordings
		WHERE id = $1
	`
	
	var recording domain.Recording
	err := r.db.QueryRow(ctx, query, id).Scan(
		&recording.ID, &recording.FileName, &recording.FilePath, &recording.FileSize,
		&recording.DurationSeconds, &recording.MimeType, &recording.MD5Hash,
		&recording.CreatedBy, &recording.CreatedAt, &recording.UpdatedAt,
		&recording.Source, &recording.Status, &recording.Metadata, &recording.Tags,
	)
	
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	
	return &recording, nil
}

// Update updates a recording in the database
func (r *PostgresRecordingRepository) Update(ctx context.Context, recording *domain.Recording) error {
	recording.UpdatedAt = time.Now()
	
	query := `
		UPDATE recordings SET
			file_name = $1,
			file_path = $2,
			file_size = $3,
			duration_seconds = $4,
			mime_type = $5,
			md5_hash = $6,
			updated_at = $7,
			source = $8,
			status = $9,
			metadata = $10,
			tags = $11
		WHERE id = $12
	`
	
	result, err := r.db.Exec(ctx, query,
		recording.FileName, recording.FilePath, recording.FileSize,
		recording.DurationSeconds, recording.MimeType, recording.MD5Hash,
		recording.UpdatedAt, recording.Source, recording.Status,
		recording.Metadata, recording.Tags, recording.ID,
	)
	
	if err != nil {
		return err
	}
	
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	
	return nil
}

// Delete deletes a recording from the database
func (r *PostgresRecordingRepository) Delete(ctx context.Context, id uuid.UUID) error {
	if id == uuid.Nil {
		return ErrInvalidID
	}
	
	query := `DELETE FROM recordings WHERE id = $1`
	
	result, err := r.db.Exec(ctx, query, id)
	if err != nil {
		return err
	}
	
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	
	return nil
}

// List retrieves a list of recordings based on filter and pagination
func (r *PostgresRecordingRepository) List(ctx context.Context, filter domain.RecordingFilter, pagination domain.Pagination) ([]*domain.Recording, int, error) {
	// Build query
	baseQuery := `
		SELECT 
			id, file_name, file_path, file_size, duration_seconds, mime_type, md5_hash, 
			created_by, created_at, updated_at, source, status, metadata, tags
		FROM recordings
		WHERE 1=1
	`
	
	countQuery := `SELECT COUNT(*) FROM recordings WHERE 1=1`
	
	// Add filter conditions
	args := []interface{}{}
	argPos := 1
	
	if len(filter.Status) > 0 {
		baseQuery += fmt.Sprintf(" AND status = ANY($%d)", argPos)
		countQuery += fmt.Sprintf(" AND status = ANY($%d)", argPos)
		args = append(args, filter.Status)
		argPos++
	}
	
	if filter.Source != "" {
		baseQuery += fmt.Sprintf(" AND source = $%d", argPos)
		countQuery += fmt.Sprintf(" AND source = $%d", argPos)
		args = append(args, filter.Source)
		argPos++
	}
	
	if !filter.From.IsZero() {
		baseQuery += fmt.Sprintf(" AND created_at >= $%d", argPos)
		countQuery += fmt.Sprintf(" AND created_at >= $%d", argPos)
		args = append(args, filter.From)
		argPos++
	}
	
	if !filter.To.IsZero() {
		baseQuery += fmt.Sprintf(" AND created_at <= $%d", argPos)
		countQuery += fmt.Sprintf(" AND created_at <= $%d", argPos)
		args = append(args, filter.To)
		argPos++
	}
	
	// Add sorting
	baseQuery += " ORDER BY created_at DESC"
	
	// Add pagination
	if pagination.PageSize > 0 {
		baseQuery += fmt.Sprintf(" LIMIT $%d", argPos)
		args = append(args, pagination.PageSize)
		argPos++
		
		if pagination.Page > 1 {
			offset := (pagination.Page - 1) * pagination.PageSize
			baseQuery += fmt.Sprintf(" OFFSET $%d", argPos)
			args = append(args, offset)
		}
	}
	
	// Get total count
	var totalCount int
	err := r.db.QueryRow(ctx, countQuery, args...).Scan(&totalCount)
	if err != nil {
		return nil, 0, err
	}
	
	// Execute main query
	rows, err := r.db.Query(ctx, baseQuery, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	
	// Parse results
	recordings := []*domain.Recording{}
	for rows.Next() {
		var recording domain.Recording
		err := rows.Scan(
			&recording.ID, &recording.FileName, &recording.FilePath, &recording.FileSize,
			&recording.DurationSeconds, &recording.MimeType, &recording.MD5Hash,
			&recording.CreatedBy, &recording.CreatedAt, &recording.UpdatedAt,
			&recording.Source, &recording.Status, &recording.Metadata, &recording.Tags,
		)
		if err != nil {
			return nil, 0, err
		}
		recordings = append(recordings, &recording)
	}
	
	if err = rows.Err(); err != nil {
		return nil, 0, err
	}
	
	return recordings, totalCount, nil
}

// UpdateStatus updates the status of a recording
func (r *PostgresRecordingRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status domain.RecordingStatus) error {
	if id == uuid.Nil {
		return ErrInvalidID
	}
	
	query := `
		UPDATE recordings SET
			status = $1,
			updated_at = $2
		WHERE id = $3
	`
	
	result, err := r.db.Exec(ctx, query, status, time.Now(), id)
	if err != nil {
		return err
	}
	
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	
	return nil
}

// FindByHash finds a recording by its MD5 hash
func (r *PostgresRecordingRepository) FindByHash(ctx context.Context, hash string) (*domain.Recording, error) {
	query := `
		SELECT 
			id, file_name, file_path, file_size, duration_seconds, mime_type, md5_hash, 
			created_by, created_at, updated_at, source, status, metadata, tags
		FROM recordings
		WHERE md5_hash = $1
	`
	
	var recording domain.Recording
	err := r.db.QueryRow(ctx, query, hash).Scan(
		&recording.ID, &recording.FileName, &recording.FilePath, &recording.FileSize,
		&recording.DurationSeconds, &recording.MimeType, &recording.MD5Hash,
		&recording.CreatedBy, &recording.CreatedAt, &recording.UpdatedAt,
		&recording.Source, &recording.Status, &recording.Metadata, &recording.Tags,
	)
	
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	
	return &recording, nil
}