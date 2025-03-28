package worker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/namanag97/call_in_go/call-processor/internal/domain"
	"github.com/namanag97/call_in_go/call-processor/internal/repository"
)

// Enhanced worker system to improve reliability and monitoring

// WorkerStats represents statistics about worker operations
type WorkerStats struct {
	ActiveWorkers    int                     `json:"activeWorkers"`
	QueuedJobs       int                     `json:"queuedJobs"`
	ProcessingJobs   int                     `json:"processingJobs"`
	CompletedJobs    int                     `json:"completedJobs"`
	FailedJobs       int                     `json:"failedJobs"`
	RetryingJobs     int                     `json:"retryingJobs"`
	JobsByType       map[string]int          `json:"jobsByType"`
	AverageJobTime   map[string]time.Duration `json:"averageJobTime"`
	WorkerUtilization float64                `json:"workerUtilization"`
	StartedAt        time.Time               `json:"startedAt"`
	LastStatsRefresh time.Time               `json:"lastStatsRefresh"`
}

// EnhancedWorkerManager extends the WorkerManager with additional features
type EnhancedWorkerManager struct {
	*WorkerManager
	stats            WorkerStats
	healthCheckFn    HealthCheckFunc
	statsRefreshChan chan struct{}
	processingTimers map[uuid.UUID]time.Time
	jobHistory       map[string][]time.Duration
	statsMu          sync.RWMutex
}

// HealthCheckFunc defines a function that checks worker manager health
type HealthCheckFunc func() (bool, error)

// Config options specific to the enhanced worker manager
type EnhancedConfig struct {
	StatsRefreshInterval time.Duration
	JobHistorySize       int
	HealthCheckInterval  time.Duration
	EnableRetryBackoff   bool
	MaxBackoffDelay      time.Duration
}

// NewEnhancedWorkerManager creates a new enhanced worker manager
func NewEnhancedWorkerManager(
	config Config,
	jobRepo repository.JobRepository,
	enhancedConfig EnhancedConfig,
) *EnhancedWorkerManager {
	baseManager := NewWorkerManager(config, jobRepo)
	
	if enhancedConfig.StatsRefreshInterval == 0 {
		enhancedConfig.StatsRefreshInterval = 30 * time.Second
	}
	
	if enhancedConfig.JobHistorySize == 0 {
		enhancedConfig.JobHistorySize = 100
	}
	
	manager := &EnhancedWorkerManager{
		WorkerManager:    baseManager,
		statsRefreshChan: make(chan struct{}),
		processingTimers: make(map[uuid.UUID]time.Time),
		jobHistory:       make(map[string][]time.Duration),
		stats: WorkerStats{
			JobsByType:     make(map[string]int),
			AverageJobTime: make(map[string]time.Duration),
			StartedAt:      time.Now(),
		},
		healthCheckFn: defaultHealthCheck,
	}
	
	// Override the job processing function to collect metrics
	originalProcessJob := baseManager.processJob
	baseManager.processJob = func(job *domain.Job) {
		manager.trackJobStart(job)
		originalProcessJob(job)
		manager.trackJobEnd(job)
	}
	
	return manager
}

// Start starts the worker manager with enhanced features
func (m *EnhancedWorkerManager) Start() error {
	// Refresh stats initially
	m.refreshStats()
	
	// Start stats collector goroutine
	go m.statsCollector()
	
	// Start health check goroutine if interval > 0
	if m.config.HealthCheckInterval > 0 {
		go m.healthChecker()
	}
	
	// Start the base worker manager
	return m.WorkerManager.Start()
}

// Stop stops the enhanced worker manager
func (m *EnhancedWorkerManager) Stop() error {
	// Signal stats collector to stop
	close(m.statsRefreshChan)
	
	// Stop the base worker manager
	return m.WorkerManager.Stop()
}

// GetStats returns the current worker stats
func (m *EnhancedWorkerManager) GetStats() WorkerStats {
	m.statsMu.RLock()
	defer m.statsMu.RUnlock()
	
	// Return a copy to avoid race conditions
	statsCopy := m.stats
	statsCopy.JobsByType = make(map[string]int)
	statsCopy.AverageJobTime = make(map[string]time.Duration)
	
	for k, v := range m.stats.JobsByType {
		statsCopy.JobsByType[k] = v
	}
	
	for k, v := range m.stats.AverageJobTime {
		statsCopy.AverageJobTime[k] = v
	}
	
	return statsCopy
}

// CheckHealth performs a health check on the worker manager
func (m *EnhancedWorkerManager) CheckHealth() (bool, error) {
	if m.healthCheckFn == nil {
		return true, nil
	}
	return m.healthCheckFn()
}

// SetHealthCheckFunc sets a custom health check function
func (m *EnhancedWorkerManager) SetHealthCheckFunc(fn HealthCheckFunc) {
	m.healthCheckFn = fn
}

// trackJobStart tracks when a job starts processing
func (m *EnhancedWorkerManager) trackJobStart(job *domain.Job) {
	m.statsMu.Lock()
	defer m.statsMu.Unlock()
	
	m.processingTimers[job.ID] = time.Now()
	m.stats.ProcessingJobs++
}

// trackJobEnd tracks when a job finishes processing
func (m *EnhancedWorkerManager) trackJobEnd(job *domain.Job) {
	m.statsMu.Lock()
	defer m.statsMu.Unlock()
	
	startTime, exists := m.processingTimers[job.ID]
	if !exists {
		return
	}
	
	duration := time.Since(startTime)
	delete(m.processingTimers, job.ID)
	
	// Update job history
	jobType := job.JobType
	history := m.jobHistory[jobType]
	if len(history) >= m.config.JobHistorySize {
		// Remove oldest entry
		history = history[1:]
	}
	history = append(history, duration)
	m.jobHistory[jobType] = history
	
	// Update average time
	var total time.Duration
	for _, d := range history {
		total += d
	}
	m.stats.AverageJobTime[jobType] = total / time.Duration(len(history))
	
	// Update job counts based on status
	m.stats.ProcessingJobs--
	switch job.Status {
	case domain.JobStatusCompleted:
		m.stats.CompletedJobs++
	case domain.JobStatusFailed:
		m.stats.FailedJobs++
	case domain.JobStatusRetrying:
		m.stats.RetryingJobs++
	}
}

// statsCollector periodically refreshes worker stats
func (m *EnhancedWorkerManager) statsCollector() {
	ticker := time.NewTicker(m.config.StatsRefreshInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.refreshStats()
		case <-m.statsRefreshChan:
			m.refreshStats()
		}
	}
}

// refreshStats refreshes the worker stats
func (m *EnhancedWorkerManager) refreshStats() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	// Get job counts by status
	queuedJobs, _ := m.jobRepo.CountByStatus(ctx, domain.JobStatusPending)
	processingJobs, _ := m.jobRepo.CountByStatus(ctx, domain.JobStatusProcessing)
	completedJobs, _ := m.jobRepo.CountByStatus(ctx, domain.JobStatusCompleted)
	failedJobs, _ := m.jobRepo.CountByStatus(ctx, domain.JobStatusFailed)
	retryingJobs, _ := m.jobRepo.CountByStatus(ctx, domain.JobStatusRetrying)
	
	// Get job counts by type
	jobsByType := make(map[string]int)
	m.mu.RLock()
	for jobType := range m.handlers {
		count, _ := m.jobRepo.CountByType(ctx, jobType)
		jobsByType[jobType] = count
	}
	activeWorkers := len(m.workerPool)
	m.mu.RUnlock()
	
	// Calculate worker utilization
	workerUtilization := float64(activeWorkers) / float64(m.config.WorkerCount)
	
	// Update stats
	m.statsMu.Lock()
	m.stats.QueuedJobs = queuedJobs
	m.stats.ProcessingJobs = processingJobs
	m.stats.CompletedJobs = completedJobs
	m.stats.FailedJobs = failedJobs
	m.stats.RetryingJobs = retryingJobs
	m.stats.JobsByType = jobsByType
	m.stats.ActiveWorkers = activeWorkers
	m.stats.WorkerUtilization = workerUtilization
	m.stats.LastStatsRefresh = time.Now()
	m.statsMu.Unlock()
}

// healthChecker periodically checks worker manager health
func (m *EnhancedWorkerManager) healthChecker() {
	ticker := time.NewTicker(m.config.HealthCheckInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			healthy, err := m.CheckHealth()
			if !healthy {
				fmt.Printf("Worker manager health check failed: %v\n", err)
				// In a real system, this would report to monitoring
			}
		}
	}
}

// defaultHealthCheck performs a basic health check
func defaultHealthCheck() (bool, error) {
	// Check system resources
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	
	// Example: Check if we're using too much memory
	if m.Sys > 1024*1024*1024 { // 1GB
		return false, errors.New("worker using too much memory")
	}
	
	// Check if we can access the job repository
	// This would be implemented in a real system
	
	return true, nil
}

// Enhanced JobRepository interface
type EnhancedJobRepository interface {
	repository.JobRepository
	CountByType(ctx context.Context, jobType string) (int, error)
	GetJobMetrics(ctx context.Context, since time.Time) (map[string]int, error)
	ClearStuckJobs(ctx context.Context, olderThan time.Duration) (int, error)
}

// Implementation for the CountByType method in PostgresJobRepository
func (r *PostgresJobRepository) CountByType(ctx context.Context, jobType string) (int, error) {
	var count int
	err := r.db.QueryRow(ctx, "SELECT COUNT(*) FROM jobs WHERE job_type = $1", jobType).Scan(&count)
	return count, err
}

// Implementation for the GetJobMetrics method in PostgresJobRepository
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

// Implementation for the ClearStuckJobs method in PostgresJobRepository
func (r *PostgresJobRepository) ClearStuckJobs(ctx context.Context, olderThan time.Duration) (int, error) {
	cutoffTime := time.Now().Add(-olderThan)
	
	query := `
		UPDATE jobs 
		SET status = $1, 
			last_error = 'Job was stuck in processing state', 
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