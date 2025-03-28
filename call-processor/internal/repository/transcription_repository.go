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

// PostgresTranscriptionRepository implements TranscriptionRepository
type PostgresTranscriptionRepository struct {
	db *pgxpool.Pool
}

// NewPostgresTranscriptionRepository creates a new PostgresTranscriptionRepository
func NewPostgresTranscriptionRepository(db *pgxpool.Pool) *PostgresTranscriptionRepository {
	return &PostgresTranscriptionRepository{db: db}
}

// Create creates a new transcription
func (r *PostgresTranscriptionRepository) Create(ctx context.Context, transcription *domain.Transcription) error {
	if transcription.ID == uuid.Nil {
		transcription.ID = uuid.New()
	}

	now := time.Now()
	
	// Update fields using pointers as needed
	if transcription.StartedAt == nil {
		transcription.StartedAt = &now
	}
	
	// CompletedAt might already be set for imported transcriptions

	query := `
		INSERT INTO transcriptions (
			id, recording_id, full_text, language, confidence,
			status, engine, started_at, completed_at, error, metadata
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`

	_, err := r.db.Exec(ctx, query,
		transcription.ID, transcription.RecordingID, transcription.FullText,
		transcription.Language, transcription.Confidence, transcription.Status,
		transcription.Engine, transcription.StartedAt, transcription.CompletedAt,
		transcription.Error, transcription.Metadata,
	)

	return err
}

// Get retrieves a transcription by ID
func (r *PostgresTranscriptionRepository) Get(ctx context.Context, id uuid.UUID) (*domain.Transcription, error) {
	query := `
		SELECT 
			id, recording_id, full_text, language, confidence,
			status, engine, started_at, completed_at, error, metadata
		FROM transcriptions
		WHERE id = $1
	`

	var transcription domain.Transcription
	err := r.db.QueryRow(ctx, query, id).Scan(
		&transcription.ID, &transcription.RecordingID, &transcription.FullText,
		&transcription.Language, &transcription.Confidence, &transcription.Status,
		&transcription.Engine, &transcription.StartedAt, &transcription.CompletedAt,
		&transcription.Error, &transcription.Metadata,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return &transcription, nil
}

// GetByRecordingID retrieves a transcription by recording ID
func (r *PostgresTranscriptionRepository) GetByRecordingID(ctx context.Context, recordingID uuid.UUID) (*domain.Transcription, error) {
	query := `
		SELECT 
			id, recording_id, full_text, language, confidence,
			status, engine, started_at, completed_at, error, metadata
		FROM transcriptions
		WHERE recording_id = $1
	`

	var transcription domain.Transcription
	err := r.db.QueryRow(ctx, query, recordingID).Scan(
		&transcription.ID, &transcription.RecordingID, &transcription.FullText,
		&transcription.Language, &transcription.Confidence, &transcription.Status,
		&transcription.Engine, &transcription.StartedAt, &transcription.CompletedAt,
		&transcription.Error, &transcription.Metadata,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return &transcription, nil
}

// Update updates a transcription
func (r *PostgresTranscriptionRepository) Update(ctx context.Context, transcription *domain.Transcription) error {
	query := `
		UPDATE transcriptions SET
			full_text = $1,
			language = $2,
			confidence = $3,
			status = $4,
			engine = $5,
			started_at = $6,
			completed_at = $7,
			error = $8,
			metadata = $9
		WHERE id = $10
	`

	result, err := r.db.Exec(ctx, query,
		transcription.FullText, transcription.Language, transcription.Confidence,
		transcription.Status, transcription.Engine, transcription.StartedAt,
		transcription.CompletedAt, transcription.Error, transcription.Metadata,
		transcription.ID,
	)

	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// Delete deletes a transcription
func (r *PostgresTranscriptionRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM transcriptions WHERE id = $1`

	result, err := r.db.Exec(ctx, query, id)
	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// List lists transcriptions by filter and pagination
func (r *PostgresTranscriptionRepository) List(ctx context.Context, filter domain.TranscriptionFilter, pagination domain.Pagination) ([]*domain.Transcription, int, error) {
	// Build query
	baseQuery := `
		SELECT 
			id, recording_id, full_text, language, confidence,
			status, engine, started_at, completed_at, error, metadata
		FROM transcriptions
		WHERE 1=1
	`

	countQuery := `SELECT COUNT(*) FROM transcriptions WHERE 1=1`

	args := []interface{}{}
	argPos := 1

	if filter.RecordingID != nil {
		baseQuery += fmt.Sprintf(" AND recording_id = $%d", argPos)
		countQuery += fmt.Sprintf(" AND recording_id = $%d", argPos)
		args = append(args, filter.RecordingID)
		argPos++
	}

	if filter.Status != nil && len(filter.Status) > 0 {
		baseQuery += fmt.Sprintf(" AND status = ANY($%d)", argPos)
		countQuery += fmt.Sprintf(" AND status = ANY($%d)", argPos)
		args = append(args, filter.Status)
		argPos++
	}

	if filter.Language != "" {
		baseQuery += fmt.Sprintf(" AND language = $%d", argPos)
		countQuery += fmt.Sprintf(" AND language = $%d", argPos)
		args = append(args, filter.Language)
		argPos++
	}

	// Apply pagination
	totalCount := 0
	err := r.db.QueryRow(ctx, countQuery, args...).Scan(&totalCount)
	if err != nil {
		return nil, 0, err
	}

	// Add ordering
	baseQuery += " ORDER BY started_at DESC"

	if pagination.PageSize > 0 {
		baseQuery += fmt.Sprintf(" LIMIT $%d", argPos)
		args = append(args, pagination.PageSize)
		argPos++
	}

	if pagination.Page > 1 && pagination.PageSize > 0 {
		offset := (pagination.Page - 1) * pagination.PageSize
		baseQuery += fmt.Sprintf(" OFFSET $%d", argPos)
		args = append(args, offset)
	}

	// Execute query
	rows, err := r.db.Query(ctx, baseQuery, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	transcriptions := []*domain.Transcription{}
	for rows.Next() {
		var transcription domain.Transcription
		err := rows.Scan(
			&transcription.ID, &transcription.RecordingID, &transcription.FullText,
			&transcription.Language, &transcription.Confidence, &transcription.Status,
			&transcription.Engine, &transcription.StartedAt, &transcription.CompletedAt,
			&transcription.Error, &transcription.Metadata,
		)
		if err != nil {
			return nil, 0, err
		}
		transcriptions = append(transcriptions, &transcription)
	}

	if err = rows.Err(); err != nil {
		return nil, 0, err
	}

	return transcriptions, totalCount, nil
}

// UpdateStatus updates a transcription's status
func (r *PostgresTranscriptionRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status domain.TranscriptionStatus) error {
	now := time.Now()
	var completedAt *time.Time

	if status == domain.TranscriptionStatusCompleted {
		completedAt = &now
	}

	query := `
		UPDATE transcriptions SET
			status = $1,
			updated_at = $2,
			completed_at = $3
		WHERE id = $4
	`

	result, err := r.db.Exec(ctx, query, status, now, completedAt, id)
	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// AddSegment adds a segment to a transcription
func (r *PostgresTranscriptionRepository) AddSegment(ctx context.Context, segment *domain.TranscriptionSegment) error {
	if segment.ID == uuid.Nil {
		segment.ID = uuid.New()
	}

	query := `
		INSERT INTO transcription_segments (
			id, transcription_id, start_time, end_time, text, speaker, confidence
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`

	_, err := r.db.Exec(ctx, query,
		segment.ID, segment.TranscriptionID, segment.StartTime, segment.EndTime,
		segment.Text, segment.Speaker, segment.Confidence,
	)

	return err
}

// GetSegments gets segments for a transcription
func (r *PostgresTranscriptionRepository) GetSegments(ctx context.Context, transcriptionID uuid.UUID) ([]domain.TranscriptionSegment, error) {
	query := `
		SELECT 
			id, transcription_id, start_time, end_time, text, speaker, confidence
		FROM transcription_segments
		WHERE transcription_id = $1
		ORDER BY start_time ASC
	`

	rows, err := r.db.Query(ctx, query, transcriptionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	segments := []domain.TranscriptionSegment{}
	for rows.Next() {
		var segment domain.TranscriptionSegment
		err := rows.Scan(
			&segment.ID, &segment.TranscriptionID, &segment.StartTime, &segment.EndTime,
			&segment.Text, &segment.Speaker, &segment.Confidence,
		)
		if err != nil {
			return nil, err
		}
		segments = append(segments, segment)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return segments, nil
}

// GetRecordingByFilename gets a recording by its filename
func (r *PostgresTranscriptionRepository) GetRecordingByFilename(ctx context.Context, filename string) ([]*domain.Recording, int, error) {
	query := `
		SELECT 
			id, file_name, file_path, file_size, duration_seconds, mime_type, md5_hash, 
			created_by, created_at, updated_at, source, status, metadata, tags
		FROM recordings
		WHERE file_name = $1
	`

	countQuery := `SELECT COUNT(*) FROM recordings WHERE file_name = $1`

	var totalCount int
	err := r.db.QueryRow(ctx, countQuery, filename).Scan(&totalCount)
	if err != nil {
		return nil, 0, err
	}

	rows, err := r.db.Query(ctx, query, filename)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

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

// CreateRecording creates a new recording
func (r *PostgresTranscriptionRepository) CreateRecording(ctx context.Context, recording *domain.Recording) error {
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