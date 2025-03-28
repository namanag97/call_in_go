package worker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/namanag97/call_in_go/call-processor/internal/domain"
	"github.com/namanag97/call_in_go/call-processor/internal/repository"
)

// Job handler function type
type JobHandler func(ctx context.Context, job *domain.Job) error

// Manager defines the interface for worker management
type Manager interface {
	Start() error
	Stop() error
	RegisterHandler(jobType string, handler JobHandler)
	EnqueueJob(ctx context.Context, job *domain.Job) error
	GetJobStatus(ctx context.Context, jobID uuid.UUID) (*domain.Job, error)
// RegisterHandler registers a handler for a job type
func (m *WorkerManager) RegisterHandler(jobType string, handler JobHandler) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	
	m.handlers[jobType] = handler
	fmt.Printf("Registered handler for job type: %s\n", jobType)
}

// EnqueueJob adds a job to the queue
func (m *WorkerManager) EnqueueJob(ctx context.Context, job *domain.Job) error {
	if job.ID == uuid.Nil {
		job.ID = uuid.New()
	}
	
	// Set defaults if not provided
	if job.Status == "" {
		job.Status = domain.JobStatusPending
	}
	if job.MaxAttempts <= 0 {
		job.MaxAttempts = m.config.MaxRetries
	}
	
	now := time.Now()
	if job.CreatedAt.IsZero() {
		job.CreatedAt = now
	}
	if job.UpdatedAt.IsZero() {
		job.UpdatedAt = now
	}
	if job.ScheduledFor.IsZero() {
		job.ScheduledFor = now
	}
	
	return m.jobRepo.Create(ctx, job)
}

// GetJobStatus gets the status of a job
func (m *WorkerManager) GetJobStatus(ctx context.Context, jobID uuid.UUID) (*domain.Job, error) {
	return m.jobRepo.Get(ctx, jobID)
}

// pollForJobs continuously polls for available jobs
func (m *WorkerManager) pollForJobs() {
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-time.After(m.config.PollingInterval):
			m.acquireAndProcessJobs()
		}
	}
}

// acquireAndProcessJobs acquires and processes available jobs
func (m *WorkerManager) acquireAndProcessJobs() {
	// Get list of job types we can handle
	m.mutex.RLock()
	jobTypes := make([]string, 0, len(m.handlers))
	for jobType := range m.handlers {
		jobTypes = append(jobTypes, jobType)
	}
	m.mutex.RUnlock()
	
	if len(jobTypes) == 0 {
		return
	}
	
	// Determine how many workers are available
	availableWorkers := m.config.WorkerCount - len(m.workerPool)
	if availableWorkers <= 0 {
		return
	}
	
	// Acquire jobs
	ctx, cancel := context.WithTimeout(m.ctx, m.config.PollingInterval)
	defer cancel()
	
	jobs, err := m.jobRepo.AcquireJobs(ctx, m.hostname, jobTypes, availableWorkers)
	if err != nil {
		fmt.Printf("Error acquiring jobs: %v\n", err)
		return
	}
	
	// Process each job in its own goroutine
	for _, job := range jobs {
		m.workerWg.Add(1)
		m.workerPool <- struct{}{}
		
		go func(j *domain.Job) {
			defer m.workerWg.Done()
			defer func() { <-m.workerPool }()
			
			m.processJob(j)
		}(job)
	}
}

// processJob processes a single job
func (m *WorkerManager) processJob(job *domain.Job) {
	// Create a context with timeout for the job
	ctx, cancel := context.WithTimeout(m.ctx, m.config.JobTimeout)
	defer cancel()
	
	// Get the handler
	m.mutex.RLock()
	handler, exists := m.handlers[job.JobType]
	m.mutex.RUnlock()
	
	if !exists {
		fmt.Printf("No handler found for job type: %s\n", job.JobType)
		m.jobRepo.MarkFailed(ctx, job.ID, errors.New("no handler registered for job type"))
		return
	}
	
	// Execute the handler
	fmt.Printf("Processing job %s of type %s (attempt %d/%d)\n", 
		job.ID, job.JobType, job.Attempts+1, job.MaxAttempts)
	
	err := handler(ctx, job)
	
	// Handle result
	if err != nil {
		fmt.Printf("Job %s failed: %v\n", job.ID, err)
		
		// Check if we should retry
		if job.Attempts < job.MaxAttempts-1 {
			// Increment attempts and release the job for retry
			job.Attempts++
			job.LastError = err.Error()
			job.Status = domain.JobStatusRetrying
			job.ScheduledFor = time.Now().Add(m.config.RetryDelay)
			
			retryErr := m.jobRepo.Update(ctx, job)
			if retryErr != nil {
				fmt.Printf("Error updating job for retry: %v\n", retryErr)
			}
		} else {
			// Mark job as failed
			failErr := m.jobRepo.MarkFailed(ctx, job.ID, err)
			if failErr != nil {
				fmt.Printf("Error marking job as failed: %v\n", failErr)
			}
		}
	} else {
		// Mark job as completed
		fmt.Printf("Job %s completed successfully\n", job.ID)
		completeErr := m.jobRepo.MarkComplete(ctx, job.ID)
		if completeErr != nil {
			fmt.Printf("Error marking job as complete: %v\n", completeErr)
		}
	}
}

// collectMetrics periodically collects metrics about job processing
func (m *WorkerManager) collectMetrics() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			
			// Count jobs by status
			for _, status := range []domain.JobStatus{
				domain.JobStatusPending,
				domain.JobStatusProcessing,
				domain.JobStatusCompleted,
				domain.JobStatusFailed,
				domain.JobStatusRetrying,
			} {
				count, err := m.jobRepo.CountByStatus(ctx, status)
				if err != nil {
					fmt.Printf("Error counting jobs with status %s: %v\n", status, err)
					continue
				}
				
				m.mutex.Lock()
				m.jobsByStatus[status] = count
				m.mutex.Unlock()
			}
			
			// Log metrics
			m.mutex.RLock()
			fmt.Printf("Job metrics - Pending: %d, Processing: %d, Completed: %d, Failed: %d, Retrying: %d\n",
				m.jobsByStatus[domain.JobStatusPending],
				m.jobsByStatus[domain.JobStatusProcessing],
				m.jobsByStatus[domain.JobStatusCompleted],
				m.jobsByStatus[domain.JobStatusFailed],
				m.jobsByStatus[domain.JobStatusRetrying],
			)
			activeWorkers := len(m.workerPool)
			m.mutex.RUnlock()
			
			fmt.Printf("Worker metrics - Active: %d, Available: %d\n", 
				activeWorkers, m.config.WorkerCount-activeWorkers)
			
			cancel()
		}
	}
}

// getHostname returns the hostname of the current machine
func getHostname() (string, error) {
	// In a real implementation, use os.Hostname() or container ID
	return "worker-" + uuid.New().String()[:8], nil
}

// Config contains configuration for the worker manager
type Config struct {
	WorkerCount      int
	PollingInterval  time.Duration
	LockDuration     time.Duration
	MaxRetries       int
	RetryDelay       time.Duration
	JobTimeout       time.Duration
	ShutdownTimeout  time.Duration
}

// DefaultConfig returns a default worker configuration
func DefaultConfig() Config {
	return Config{
		WorkerCount:      5,
		PollingInterval:  5 * time.Second,
		LockDuration:     15 * time.Minute,
		MaxRetries:       3,
		RetryDelay:       30 * time.Second,
		JobTimeout:       10 * time.Minute,
		ShutdownTimeout:  30 * time.Second,
	}
}

// WorkerManager implements Manager interface
type WorkerManager struct {
	config       Config
	jobRepo      repository.JobRepository
	handlers     map[string]JobHandler
	workerPool   chan struct{}
	workerWg     sync.WaitGroup
	jobsByStatus map[domain.JobStatus]int
	mutex        sync.RWMutex
	ctx          context.Context
	cancel       context.CancelFunc
	hostname     string
}

// NewWorkerManager creates a new worker manager
func NewWorkerManager(config Config, jobRepo repository.JobRepository) *WorkerManager {
	// Set defaults for unspecified values
	if config.WorkerCount <= 0 {
		config.WorkerCount = 5
	}
	if config.PollingInterval <= 0 {
		config.PollingInterval = 5 * time.Second
	}
	if config.LockDuration <= 0 {
		config.LockDuration = 15 * time.Minute
	}
	if config.MaxRetries <= 0 {
		config.MaxRetries = 3
	}
	if config.RetryDelay <= 0 {
		config.RetryDelay = 30 * time.Second
	}
	if config.JobTimeout <= 0 {
		config.JobTimeout = 10 * time.Minute
	}
	if config.ShutdownTimeout <= 0 {
		config.ShutdownTimeout = 30 * time.Second
	}

	// Create context with cancel
	ctx, cancel := context.WithCancel(context.Background())

	hostname, err := getHostname()
	if err != nil {
		hostname = fmt.Sprintf("worker-%s", uuid.New().String()[:8])
	}

	return &WorkerManager{
		config:       config,
		jobRepo:      jobRepo,
		handlers:     make(map[string]JobHandler),
		workerPool:   make(chan struct{}, config.WorkerCount),
		jobsByStatus: make(map[domain.JobStatus]int),
		ctx:          ctx,
		cancel:       cancel,
		hostname:     hostname,
	}
}

// Start starts the worker manager
func (m *WorkerManager) Start() error {
	fmt.Printf("Starting worker manager with %d workers...\n", m.config.WorkerCount)
	
	// Start job polling
	go m.pollForJobs()
	
	// Start metrics collection
	go m.collectMetrics()
	
	return nil
}

// Stop stops the worker manager
func (m *WorkerManager) Stop() error {
	fmt.Println("Stopping worker manager...")
	
	// Cancel context to signal shutdown
	m.cancel()
	
	// Wait for all workers to finish with timeout
	done := make(chan struct{})
	go func() {
		m.workerWg.Wait()
		close(done)
	}()
	
	select {
	case <-done:
		fmt.Println("All workers gracefully stopped")
	case <-time.After(m.config.ShutdownTimeout):
		fmt.Println("Shutdown timed out, some jobs may still be running")
	}
	
	return nil
}