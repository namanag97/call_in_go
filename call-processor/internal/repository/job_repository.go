package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/namanag97/call_in_go/call-processor/internal/domain"
)

// PostgresJobRepository implements JobRepository using PostgreSQL
type PostgresJobRepository struct {
	db *pgxpool.Pool
}

// NewPostgresJobRepository creates a new PostgresJobRepository
func NewPostgresJobRepository(db *pgxpool.Pool) *PostgresJobRepository {
	return &PostgresJobRepository{db: db}
}

// Create creates a new job
func (r *PostgresJobRepository) Create(ctx context.Context, job *domain.Job) error {
	if job.ID == uuid.Nil {
		job.ID = uuid.New()
	}
	
	now := time.Now()
	job.CreatedAt = now
	job.UpdatedAt = now
	
	query := `
		INSERT INTO jobs (
			id, job_type, status, priority, payload, max_attempts, attempts,
			last_error, created_at, updated_at, scheduled_for
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`
	
	_, err := r.db.Exec(ctx, query,
		job.ID, job.JobType, job.Status, job.Priority, job.Payload,
		job.MaxAttempts, job.Attempts, job.LastError,
		job.CreatedAt, job.UpdatedAt, job.ScheduledFor,
	)
	
	return err
}

// Get gets a job by ID
func (r *PostgresJobRepository) Get(ctx context.Context, id uuid.UUID) (*domain.Job, error) {
	query := `
		SELECT 
			id, job_type, status, priority, payload, max_attempts, attempts,
			last_error, locked_by, locked_at, created_at, updated_at, scheduled_for
		FROM jobs
		WHERE id = $1
	`
	
	var job domain.Job
	err := r.db.QueryRow(ctx, query, id).Scan(
		&job.ID, &job.JobType, &job.Status, &job.Priority, &job.Payload,
		&job.MaxAttempts, &job.Attempts, &job.LastError, &job.LockedBy, &job.LockedAt,
		&job.CreatedAt, &job.UpdatedAt, &job.ScheduledFor,
	)
	
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	
	return &job, nil
}

// Update updates a job
func (r *PostgresJobRepository) Update(ctx context.Context, job *domain.Job) error {
	job.UpdatedAt = time.Now()
	
	query := `
		UPDATE jobs SET
			status = $1,
			priority = $2,
			payload = $3,
			max_attempts = $4,
			attempts = $5,
			last_error = $6,
			locked_by = $7,
			locked_at = $8,
			updated_at = $9,
			scheduled_for = $10
		WHERE id = $11
	`
	
	result, err := r.db.Exec(ctx, query,
		job.Status, job.Priority, job.Payload, job.MaxAttempts, job.Attempts,
		job.LastError, job.LockedBy, job.LockedAt, job.UpdatedAt, job.ScheduledFor,
		job.ID,
	)
	
	if err != nil {
		return err
	}
	
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	
	return nil
}

// Delete deletes a job
func (r *PostgresJobRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM jobs WHERE id = $1`
	
	result, err := r.db.Exec(ctx, query, id)
	if err != nil {
		return err
	}
	
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	
	return nil
}

// List lists jobs by type and status
func (r *PostgresJobRepository) List(ctx context.Context, jobType string, status domain.JobStatus, limit int) ([]*domain.Job, error) {
	query := `
		SELECT 
			id, job_type, status, priority, payload, max_attempts, attempts,
			last_error, locked_by, locked_at, created_at, updated_at, scheduled_for
		FROM jobs
		WHERE 1=1
	`
	
	args := []interface{}{}
	argPos := 1
	
	if jobType != "" {
		query += ` AND job_type = $` + string(argPos)
		args = append(args, jobType)
		argPos++
	}
	
	if status != "" {
		query += ` AND status = $` + string(argPos)
		args = append(args, status)
		argPos++
	}
	
	query += ` ORDER BY priority DESC, scheduled_for ASC`
	
	if limit > 0 {
		query += ` LIMIT $` + string(argPos)
		args = append(args, limit)
	}
	
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	
	jobs := []*domain.Job{}
	for rows.Next() {
		var job domain.Job
		err := rows.Scan(
			&job.ID, &job.JobType, &job.Status, &job.Priority, &job.Payload,
			&job.MaxAttempts, &job.Attempts, &job.LastError, &job.LockedBy, &job.LockedAt,
			&job.CreatedAt, &job.UpdatedAt, &job.ScheduledFor,
		)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, &job)
	}
	
	if err = rows.Err(); err != nil {
		return nil, err
	}
	
	return jobs, nil
}

// AcquireJobs acquires pending jobs for processing
func (r *PostgresJobRepository) AcquireJobs(ctx context.Context, workerID string, jobTypes []string, limit int) ([]*domain.Job, error) {
	// This operation needs to be atomic to prevent multiple workers from acquiring the same jobs
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	
	// Build query for acquiring jobs
	// In PostgreSQL, we can use FOR UPDATE SKIP LOCKED to acquire jobs atomically
	query := `
		SELECT 
			id, job_type, status, priority, payload, max_attempts, attempts,
			last_error, locked_by, locked_at, created_at, updated_at, scheduled_for
		FROM jobs
		WHERE status = $1
		AND scheduled_for <= NOW()
		AND job_type = ANY($2)
		ORDER BY priority DESC, scheduled_for ASC
		LIMIT $3
		FOR UPDATE SKIP LOCKED
	`
	
	rows, err := tx.Query(ctx, query, domain.JobStatusPending, jobTypes, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	
	// Read jobs
	jobIDs := []uuid.UUID{}
	jobs := []*domain.Job{}
	
	for rows.Next() {
		var job domain.Job
		err := rows.Scan(
			&job.ID, &job.JobType, &job.Status, &job.Priority, &job.Payload,
			&job.MaxAttempts, &job.Attempts, &job.LastError, &job.LockedBy, &job.LockedAt,
			&job.CreatedAt, &job.UpdatedAt, &job.ScheduledFor,
		)
		if err != nil {
			return nil, err
		}
		
		jobs = append(jobs, &job)
		jobIDs = append(jobIDs, job.ID)
	}
	
	if err = rows.Err(); err != nil {
		return nil, err
	}
	
	// If no jobs found, return empty slice
	if len(jobIDs) == 0 {
		return []*domain.Job{}, nil
	}
	
	// Update jobs to mark them as processing
	now := time.Now()
	updateQuery := `
		UPDATE jobs
		SET status = $1, locked_by = $2, locked_at = $3, updated_at = $4
		WHERE id = ANY($5)
	`
	
	_, err = tx.Exec(ctx, updateQuery, domain.JobStatusProcessing, workerID, now, now, jobIDs)
	if err != nil {
		return nil, err
	}
	
	// Commit transaction
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	
	// Update job status in memory
	for _, job := range jobs {
		job.Status = domain.JobStatusProcessing
		job.LockedBy = workerID
		job.LockedAt = &now
		job.UpdatedAt = now
	}
	
	return jobs, nil
}

// MarkComplete marks a job as completed
func (r *PostgresJobRepository) MarkComplete(ctx context.Context, id uuid.UUID) error {
	now := time.Now()
	
	query := `
		UPDATE jobs
		SET status = $1, locked_by = NULL, locked_at = NULL, updated_at = $2
		WHERE id = $3
	`
	
	result, err := r.db.Exec(ctx, query, domain.JobStatusCompleted, now, id)
	if err != nil {
		return err
	}
	
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	
	return nil
}

// MarkFailed marks a job as failed
func (r *PostgresJobRepository) MarkFailed(ctx context.Context, id uuid.UUID, jobErr error) error {
	now := time.Now()
	
	query := `
		UPDATE jobs
		SET status = $1, last_error = $2, locked_by = NULL, locked_at = NULL, updated_at = $3
		WHERE id = $4
	`
	
	result, err := r.db.Exec(ctx, query, domain.JobStatusFailed, jobErr.Error(), now, id)
	if err != nil {
		return err
	}
	
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	
	return nil
}

// ReleaseJob releases a job that was being processed
func (r *PostgresJobRepository) ReleaseJob(ctx context.Context, id uuid.UUID) error {
	now := time.Now()
	
	query := `
		UPDATE jobs
		SET status = $1, locked_by = NULL, locked_at = NULL, updated_at = $2
		WHERE id = $3
	`
	
	result, err := r.db.Exec(ctx, query, domain.JobStatusPending, now, id)
	if err != nil {
		return err
	}
	
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	
	return nil
}

// CountByStatus counts jobs by status
func (r *PostgresJobRepository) CountByStatus(ctx context.Context, status domain.JobStatus) (int, error) {
	var count int
	
	query := `SELECT COUNT(*) FROM jobs WHERE status = $1`
	
	err := r.db.QueryRow(ctx, query, status).Scan(&count)
	if err != nil {
		return 0, err
	}
	
	return count, nil
}

// CountByType counts jobs by type
func (r *PostgresJobRepository) CountByType(ctx context.Context, jobType string) (int, error) {
	var count int
	
	query := `SELECT COUNT(*) FROM jobs WHERE job_type = $1`
	
	err := r.db.QueryRow(ctx, query, jobType).Scan(&count)
	if err != nil {
		return 0, err
	}
	
	return count, nil
}

// GetJobMetrics retrieves job metrics since a given time
func (r *PostgresJobRepository) GetJobMetrics(ctx context.Context, since time.Time) (map[string]int, error) {
	query := `
		SELECT status, COUNT(*) 
		FROM jobs 
		WHERE created_at >= $1 
		GROUP BY status
	`
	
	rows, err := r.db.Query(ctx, query, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	
	metrics := make(map[string]int)
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		metrics[status] = count
	}
	
	return metrics, rows.Err()
}

// ClearStuckJobs identifies and resets jobs that have been stuck in processing state
func (r *PostgresJobRepository) ClearStuckJobs(ctx context.Context, olderThan time.Duration) (int, error) {
	cutoffTime := time.Now().Add(-olderThan)
	
	query := `
		UPDATE jobs 
		SET status = $1, 
			last_error = 'Job was stuck in processing state', 
			locked_by = NULL,
			locked_at = NULL,
			updated_at = NOW() 
		WHERE status = $2 
		AND locked_at < $3
	`
	
	result, err := r.db.Exec(ctx, query, domain.JobStatusFailed, domain.JobStatusProcessing, cutoffTime)
	if err != nil {
		return 0, err
	}
	
	count := int(result.RowsAffected())
	return count, nil
}