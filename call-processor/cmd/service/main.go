package main

import (
	"context"
	"fmt"
	"log"
	"net/http" // Add this import
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/joho/godotenv"
	"github.com/spf13/cobra" // Make sure Cobra is imported

	// Your internal packages...
	"github.com/namanag97/call_in_go/call-processor/internal/api"
	"github.com/namanag97/call_in_go/call-processor/internal/analysis"
	"github.com/namanag97/call_in_go/call-processor/internal/domain"
	"github.com/namanag97/call_in_go/call-processor/internal/event"
	"github.com/namanag97/call_in_go/call-processor/internal/bulk"
	"github.com/namanag97/call_in_go/call-processor/internal/ingestion"
	"github.com/namanag97/call_in_go/call-processor/internal/repository"
	"github.com/namanag97/call_in_go/call-processor/internal/storage"
	"github.com/namanag97/call_in_go/call-processor/internal/transcription"
	"github.com/namanag97/call_in_go/call-processor/internal/worker"
	"github.com/namanag97/call_in_go/call-processor/internal/stt"
)

// Repository initialization
type repositories struct {
	RecordingRepo     repository.RecordingRepository     // Export fields
	TranscriptionRepo repository.TranscriptionRepository
	AnalysisRepo      repository.AnalysisRepository
	JobRepo           repository.JobRepository
	EventRepo         repository.EventRepository
}

// Service initialization
type services struct {
	IngestionService     *ingestion.Service     // Export fields
	TranscriptionService *transcription.Service
	AnalysisService      *analysis.Service
	BulkService          *bulk.Service
}

// --- Main Application Structure - Exported for batch command access ---
var (
	DbPool        *pgxpool.Pool
	Repos         repositories
	StorageClient storage.Client
	EventBus      event.EventBus
	WorkerManager worker.Manager
	SttClient     stt.Client
	AppServices   services
	S3BucketName  string // Store bucket name for batch command
)

// Environment variable helpers
func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

func getEnvInt(key string, defaultValue int) int {
	valueStr := os.Getenv(key)
	if valueStr == "" {
		return defaultValue
	}
	
	value, err := strconv.Atoi(valueStr)
	if err != nil {
		log.Printf("Warning: Invalid integer value for %s: %s, using default: %d", key, valueStr, defaultValue)
		return defaultValue
	}
	
	return value
}

func getEnvBool(key string, defaultValue bool) bool {
	valueStr := os.Getenv(key)
	if valueStr == "" {
		return defaultValue
	}
	
	value, err := strconv.ParseBool(valueStr)
	if err != nil {
		log.Printf("Warning: Invalid boolean value for %s: %s, using default: %v", key, valueStr, defaultValue)
		return defaultValue
	}
	
	return value
}

// Controller initialization
func initControllers(
	services services,
	repos repositories,
	workerManager worker.Manager,
) []api.Controller {
	return []api.Controller{
		api.NewRecordingController(services.IngestionService, repos.RecordingRepo),
		api.NewTranscriptionController(services.TranscriptionService, repos.TranscriptionRepo),
		api.NewAnalysisController(services.AnalysisService, repos.AnalysisRepo),
		api.NewJobController(workerManager),
		api.NewBulkOperationsController(services.BulkService), // Add this line
	}
}

// API server initialization
func initAPIServer(controllers []api.Controller) *api.Server {
	config := api.Config{
		Port:               getEnv("API_PORT", "8080"),
		BasePath:           getEnv("API_BASE_PATH", ""),
		AllowedOrigins:     strings.Split(getEnv("CORS_ALLOWED_ORIGINS", "*"), ","),
		RequestSizeLimit:   int64(getEnvInt("MAX_REQUEST_SIZE_MB", 60) * 1024 * 1024),
		EnableSwagger:      getEnvBool("ENABLE_SWAGGER", true),
		EnableMetrics:      getEnvBool("ENABLE_METRICS", true),
		EnableTracing:      getEnvBool("ENABLE_TRACING", false),
		TracingServiceName: getEnv("TRACING_SERVICE_NAME", "call-processing-api"),
	}
	
	return api.NewServer(config, controllers...)
}

// Database initialization
func initDatabase() (*pgxpool.Pool, error) {
	// Get database connection string from environment
	dbURL := getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/call_processing")
	
	// Create connection pool
	config, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		return nil, fmt.Errorf("unable to parse database URL: %w", err)
	}
	
	// Set pool configuration
	config.MaxConns = 20
	config.MinConns = 5
	config.MaxConnLifetime = 30 * time.Minute
	config.MaxConnIdleTime = 5 * time.Minute
	
	// Connect to database
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	pool, err := pgxpool.ConnectConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("unable to connect to database: %w", err)
	}
	
	// Test connection
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("unable to ping database: %w", err)
	}
	
	log.Println("Database connection established")
	return pool, nil
}

// Repository initialization
func initRepositories(dbPool *pgxpool.Pool) repositories {
    // Use PostgreSQL implementations for repositories
    recordingRepo := repository.NewPostgresRecordingRepository(dbPool)
    
    // Initialize other repository implementations
    // These would need to be implemented similar to the RecordingRepository
    // For now, let's use the PostgresRecordingRepository as a reference
    
    return repositories{
        RecordingRepo:     recordingRepo,
        // Other repositories would be initialized here with their PostgreSQL implementations
        // For example:
        // TranscriptionRepo: repository.NewPostgresTranscriptionRepository(dbPool),
        // AnalysisRepo:      repository.NewPostgresAnalysisRepository(dbPool),
        // JobRepo:           repository.NewPostgresJobRepository(dbPool),
        // EventRepo:         repository.NewPostgresEventRepository(dbPool),
        
        // Temporarily use stub implementations until proper implementations are available
        TranscriptionRepo: NewStubTranscriptionRepository(dbPool),
        AnalysisRepo:      NewStubAnalysisRepository(dbPool),
        JobRepo:           NewStubJobRepository(dbPool),
        EventRepo:         NewStubEventRepository(dbPool),
    }
}

// Stub repository implementations - to be replaced with proper PostgreSQL implementations
// These stubs provide minimal implementations that don't panic but don't do much either

// StubTranscriptionRepository - minimal implementation for TranscriptionRepository
type StubTranscriptionRepository struct {
    db *pgxpool.Pool
}

func NewStubTranscriptionRepository(db *pgxpool.Pool) *StubTranscriptionRepository {
    return &StubTranscriptionRepository{db: db}
}

func (r *StubTranscriptionRepository) Create(ctx context.Context, transcription *domain.Transcription) error {
    log.Println("StubTranscriptionRepository.Create called - not fully implemented")
    return nil
}

func (r *StubTranscriptionRepository) Get(ctx context.Context, id uuid.UUID) (*domain.Transcription, error) {
    log.Println("StubTranscriptionRepository.Get called - not fully implemented")
    return nil, repository.ErrNotFound
}

func (r *StubTranscriptionRepository) GetByRecordingID(ctx context.Context, recordingID uuid.UUID) (*domain.Transcription, error) {
    log.Println("StubTranscriptionRepository.GetByRecordingID called - not fully implemented")
    return nil, repository.ErrNotFound
}

func (r *StubTranscriptionRepository) Update(ctx context.Context, transcription *domain.Transcription) error {
    log.Println("StubTranscriptionRepository.Update called - not fully implemented")
    return nil
}

func (r *StubTranscriptionRepository) Delete(ctx context.Context, id uuid.UUID) error {
    log.Println("StubTranscriptionRepository.Delete called - not fully implemented")
    return nil
}

func (r *StubTranscriptionRepository) List(ctx context.Context, filter domain.TranscriptionFilter, pagination domain.Pagination) ([]*domain.Transcription, int, error) {
    log.Println("StubTranscriptionRepository.List called - not fully implemented")
    return []*domain.Transcription{}, 0, nil
}

func (r *StubTranscriptionRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status domain.TranscriptionStatus) error {
    log.Println("StubTranscriptionRepository.UpdateStatus called - not fully implemented")
    return nil
}

func (r *StubTranscriptionRepository) AddSegment(ctx context.Context, segment *domain.TranscriptionSegment) error {
    log.Println("StubTranscriptionRepository.AddSegment called - not fully implemented")
    return nil
}

func (r *StubTranscriptionRepository) GetSegments(ctx context.Context, transcriptionID uuid.UUID) ([]domain.TranscriptionSegment, error) {
    log.Println("StubTranscriptionRepository.GetSegments called - not fully implemented")
    return []domain.TranscriptionSegment{}, nil
}

func (r *StubTranscriptionRepository) GetRecordingByFilename(ctx context.Context, filename string) ([]*domain.Recording, int, error) {
    log.Println("StubTranscriptionRepository.GetRecordingByFilename called - returning empty result")
    
    // Since this is a stub, we'll just return an empty result
    // In production code, we'd implement the actual database query
    return []*domain.Recording{}, 0, nil
}

func (r *StubTranscriptionRepository) CreateRecording(ctx context.Context, recording *domain.Recording) error {
    log.Println("StubTranscriptionRepository.CreateRecording called - not fully implemented")
    
    // In a stub implementation, we'll just pretend it succeeded
    return nil
}

// StubAnalysisRepository - minimal implementation for AnalysisRepository
type StubAnalysisRepository struct {
    db *pgxpool.Pool
}

func NewStubAnalysisRepository(db *pgxpool.Pool) *StubAnalysisRepository {
    return &StubAnalysisRepository{db: db}
}

func (r *StubAnalysisRepository) Create(ctx context.Context, analysis *domain.Analysis) error {
    log.Println("StubAnalysisRepository.Create called - not fully implemented")
    return nil
}

func (r *StubAnalysisRepository) Get(ctx context.Context, id uuid.UUID) (*domain.Analysis, error) {
    log.Println("StubAnalysisRepository.Get called - not fully implemented")
    return nil, repository.ErrNotFound
}

func (r *StubAnalysisRepository) GetByRecordingIDAndType(ctx context.Context, recordingID uuid.UUID, analysisType string) (*domain.Analysis, error) {
    log.Println("StubAnalysisRepository.GetByRecordingIDAndType called - not fully implemented")
    return nil, repository.ErrNotFound
}

func (r *StubAnalysisRepository) Update(ctx context.Context, analysis *domain.Analysis) error {
    log.Println("StubAnalysisRepository.Update called - not fully implemented")
    return nil
}

func (r *StubAnalysisRepository) Delete(ctx context.Context, id uuid.UUID) error {
    log.Println("StubAnalysisRepository.Delete called - not fully implemented")
    return nil
}

func (r *StubAnalysisRepository) List(ctx context.Context, filter domain.AnalysisFilter, pagination domain.Pagination) ([]*domain.Analysis, int, error) {
    log.Println("StubAnalysisRepository.List called - not fully implemented")
    return []*domain.Analysis{}, 0, nil
}

func (r *StubAnalysisRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status domain.AnalysisStatus) error {
    log.Println("StubAnalysisRepository.UpdateStatus called - not fully implemented")
    return nil
}

func (r *StubAnalysisRepository) ListByRecordingID(ctx context.Context, recordingID uuid.UUID) ([]*domain.Analysis, error) {
    log.Println("StubAnalysisRepository.ListByRecordingID called - not fully implemented")
    return []*domain.Analysis{}, nil
}

// StubJobRepository - minimal implementation for JobRepository
type StubJobRepository struct {
    db *pgxpool.Pool
}

func NewStubJobRepository(db *pgxpool.Pool) *StubJobRepository {
    return &StubJobRepository{db: db}
}

func (r *StubJobRepository) Create(ctx context.Context, job *domain.Job) error {
    log.Println("StubJobRepository.Create called - not fully implemented")
    return nil
}

func (r *StubJobRepository) Get(ctx context.Context, id uuid.UUID) (*domain.Job, error) {
    log.Println("StubJobRepository.Get called - not fully implemented")
    return nil, repository.ErrNotFound
}

func (r *StubJobRepository) Update(ctx context.Context, job *domain.Job) error {
    log.Println("StubJobRepository.Update called - not fully implemented")
    return nil
}

func (r *StubJobRepository) Delete(ctx context.Context, id uuid.UUID) error {
    log.Println("StubJobRepository.Delete called - not fully implemented")
    return nil
}

func (r *StubJobRepository) List(ctx context.Context, jobType string, status domain.JobStatus, limit int) ([]*domain.Job, error) {
    log.Println("StubJobRepository.List called - not fully implemented")
    return []*domain.Job{}, nil
}

func (r *StubJobRepository) AcquireJobs(ctx context.Context, workerID string, jobTypes []string, limit int) ([]*domain.Job, error) {
    log.Println("StubJobRepository.AcquireJobs called - not fully implemented")
    return []*domain.Job{}, nil
}

func (r *StubJobRepository) MarkComplete(ctx context.Context, id uuid.UUID) error {
    log.Println("StubJobRepository.MarkComplete called - not fully implemented")
    return nil
}

func (r *StubJobRepository) MarkFailed(ctx context.Context, id uuid.UUID, err error) error {
    log.Println("StubJobRepository.MarkFailed called - not fully implemented")
    return nil
}

func (r *StubJobRepository) ReleaseJob(ctx context.Context, id uuid.UUID) error {
    log.Println("StubJobRepository.ReleaseJob called - not fully implemented")
    return nil
}

func (r *StubJobRepository) CountByStatus(ctx context.Context, status domain.JobStatus) (int, error) {
    log.Println("StubJobRepository.CountByStatus called - not fully implemented")
    return 0, nil
}

func (r *StubJobRepository) FindStuckJobs(ctx context.Context, status domain.JobStatus, olderThan time.Time) ([]*domain.Job, error) {
    log.Println("StubJobRepository.FindStuckJobs called - not fully implemented")
    return []*domain.Job{}, nil
}

// StubEventRepository - minimal implementation for EventRepository
type StubEventRepository struct {
    db *pgxpool.Pool
}

func NewStubEventRepository(db *pgxpool.Pool) *StubEventRepository {
    return &StubEventRepository{db: db}
}

func (r *StubEventRepository) Create(ctx context.Context, event *domain.Event) error {
    log.Println("StubEventRepository.Create called - not fully implemented")
    return nil
}

func (r *StubEventRepository) Get(ctx context.Context, id uuid.UUID) (*domain.Event, error) {
    log.Println("StubEventRepository.Get called - not fully implemented")
    return nil, repository.ErrNotFound
}

func (r *StubEventRepository) List(ctx context.Context, entityType string, entityID uuid.UUID, pagination domain.Pagination) ([]*domain.Event, int, error) {
    log.Println("StubEventRepository.List called - not fully implemented")
    return []*domain.Event{}, 0, nil
}

func (r *StubEventRepository) ListByType(ctx context.Context, eventType string, pagination domain.Pagination) ([]*domain.Event, int, error) {
    log.Println("StubEventRepository.ListByType called - not fully implemented")
    return []*domain.Event{}, 0, nil
}

// Storage client initialization
func initStorageClient() (storage.Client, error) {
	config := storage.Config{
		Endpoint:        getEnv("S3_ENDPOINT", ""),
		Region:          getEnv("S3_REGION", "us-east-1"),
		AccessKeyID:     getEnv("S3_ACCESS_KEY", ""),
		SecretAccessKey: getEnv("S3_SECRET_KEY", ""),
		UsePathStyle:    getEnvBool("S3_USE_PATH_STYLE", false),
		UseTLS:          getEnvBool("S3_USE_TLS", true),
	}
	
	return storage.NewS3Client(config)
}

// Event system initialization
func initEventSystem(eventRepo repository.EventRepository) (event.EventBus, error) {
	// Check if Kafka is enabled
	if getEnvBool("KAFKA_ENABLED", false) {
		config := event.Config{
			KafkaBrokers:       []string{getEnv("KAFKA_BROKER", "localhost:9092")},
			KafkaTopic:         getEnv("KAFKA_TOPIC", "call-processing-events"),
			KafkaConsumerGroup: getEnv("KAFKA_CONSUMER_GROUP", "call-processing-service"),
			RetryCount:         getEnvInt("KAFKA_RETRY_COUNT", 3),
			RetryDelay:         time.Duration(getEnvInt("KAFKA_RETRY_DELAY_MS", 500)) * time.Millisecond,
			EventBufferSize:    getEnvInt("KAFKA_EVENT_BUFFER_SIZE", 1000),
		}
		
		return event.NewKafkaEventBus(config, eventRepo)
	}
	
	// Fall back to in-memory event bus
	log.Println("Kafka disabled, using in-memory event bus")
	return event.NewInMemoryEventBus(), nil
}

// Initialize worker manager
func initWorkerManager(config worker.Config, jobRepo repository.JobRepository) worker.Manager {
    return worker.NewWorkerManager(config, jobRepo)
}

// STT client initialization
func initSTTClient() stt.Client {
	// Use ElevenLabs Client directly
	apiKey := getEnv("ELEVENLABS_API_KEY", "")
	if apiKey == "" {
		log.Println("Warning: ELEVENLABS_API_KEY not set in environment or .env file, transcription might fail.")
		log.Println("Set ELEVENLABS_API_KEY environment variable or use --api-key flag with the batch command.")
	} else {
		log.Println("ElevenLabs API key configured.")
	}
	
	config := stt.Config{
		APIKey:         apiKey,
		TimeoutSeconds: getEnvInt("STT_TIMEOUT_SEC", 300), // 5 minutes default
		// Endpoint can be added if needed: Endpoint: getEnv("ELEVENLABS_ENDPOINT", "https://api.elevenlabs.io/v1/speech-to-text/transcribe"),
	}
	return stt.NewElevenLabsClient(config) // Use the ElevenLabs constructor
}

// Service initialization
func initServices(
	repos repositories,
	storageClient storage.Client,
	eventBus event.EventBus,
	workerManager worker.Manager,
	sttClient stt.Client,
) services {
	// Initialize ingestion service
	ingestionConfig := ingestion.Config{
		MaxFileSize:      int64(getEnvInt("MAX_FILE_SIZE_MB", 50) * 1024 * 1024),
		AllowedMimeTypes: []string{"audio/wav", "audio/x-wav", "audio/mpeg", "audio/mp3", "audio/ogg", "audio/flac"},
		BucketName:       getEnv("S3_BUCKET_NAME", "call-recordings"),
		DuplicateCheck:   getEnvBool("INGESTION_DUPLICATE_CHECK", true),
		DefaultCallType:  getEnv("DEFAULT_CALL_TYPE", "customer_service"),
		DefaultSource:    getEnv("DEFAULT_SOURCE", "api"),
	}
	
	ingestionService := ingestion.NewService(
		ingestionConfig,
		repos.RecordingRepo,
		storageClient,
		eventBus,
	)
	
	// Initialize transcription service
	transcriptionConfig := transcription.Config{
		BucketName:       getEnv("S3_BUCKET_NAME", "call-recordings"),
		DefaultLanguage:  getEnv("DEFAULT_LANGUAGE", "en-US"),
		DefaultEngine:    getEnv("DEFAULT_STT_ENGINE", "google"),
		MaxConcurrentJobs: getEnvInt("TRANSCRIPTION_MAX_CONCURRENT_JOBS", 10),
		JobMaxRetries:    getEnvInt("TRANSCRIPTION_JOB_MAX_RETRIES", 3),
		PollInterval:     time.Duration(getEnvInt("TRANSCRIPTION_POLL_INTERVAL_SEC", 5)) * time.Second,
		TranscriptionTTL: time.Duration(getEnvInt("TRANSCRIPTION_TTL_DAYS", 90)) * 24 * time.Hour,
	}
	
	transcriptionService := transcription.NewService(
		transcriptionConfig,
		repos.RecordingRepo,
		repos.TranscriptionRepo,
		storageClient,
		sttClient,
		eventBus,
		workerManager,
	)
	
	// Initialize analysis service
	analysisConfig := analysis.Config{
		MaxConcurrentJobs: getEnvInt("ANALYSIS_MAX_CONCURRENT_JOBS", 10),
		JobMaxRetries:    getEnvInt("ANALYSIS_JOB_MAX_RETRIES", 3),
		PollInterval:     time.Duration(getEnvInt("ANALYSIS_POLL_INTERVAL_SEC", 5)) * time.Second,
	}
	
	analysisService := analysis.NewService(
		analysisConfig,
		repos.RecordingRepo,
		repos.TranscriptionRepo,
		repos.AnalysisRepo,
		eventBus,
		workerManager,
	)
	// Initialize bulk operations service
	bulkConfig := bulk.Config{
		MaxItemsPerBulkRequest: getEnvInt("BULK_MAX_ITEMS", 1000),
		BulkWorkers:            getEnvInt("BULK_WORKERS", 5),
		BatchSize:              getEnvInt("BULK_BATCH_SIZE", 50),
		DefaultPriority:        getEnvInt("BULK_DEFAULT_PRIORITY", 5),
	}
	
	bulkService := bulk.NewService(
		bulkConfig,
		repos.JobRepo,
		repos.RecordingRepo,
		ingestionService,
		transcriptionService,
		eventBus,
		workerManager,
	)
	
	// Register analysis processors
	analysisService.RegisterProcessor(analysis.NewSentimentProcessor())
	// TODO: Register additional processors as needed
	
	return services{
		IngestionService:     ingestionService,
		TranscriptionService: transcriptionService,
		AnalysisService:      analysisService,
		BulkService:          bulkService,
	}
}
// checkConfiguration validates that required configuration is in place
func checkConfiguration() []string {
	// Collect warnings about missing or potentially problematic configurations
	var warnings []string

	// Check for essential environment variables
	if getEnv("DATABASE_URL", "") == "" {
		warnings = append(warnings, "DATABASE_URL is not set - using default postgres://postgres:postgres@localhost:5432/call_processing")
	}

	if getEnv("S3_ENDPOINT", "") == "" {
		warnings = append(warnings, "S3_ENDPOINT is not set - S3 storage may not work properly")
	}

	if getEnv("S3_ACCESS_KEY", "") == "" || getEnv("S3_SECRET_KEY", "") == "" {
		warnings = append(warnings, "S3 credentials are not configured - storage operations may fail")
	}

	if getEnv("S3_BUCKET_NAME", "") == "" {
		warnings = append(warnings, "S3_BUCKET_NAME is not set - using default 'call-recordings'")
	}

	if getEnv("ELEVENLABS_API_KEY", "") == "" {
		warnings = append(warnings, "ELEVENLABS_API_KEY is not set - transcription will not work")
	}

	return warnings
}

func main() {
	// Load environment variables FIRST
	err := godotenv.Load()
	if err != nil {
		log.Println("Warning: Error loading .env file:", err)
	}
	S3BucketName = getEnv("S3_BUCKET_NAME", "call-recordings") // Load bucket name globally

	// Check configuration and print warnings
	warnings := checkConfiguration()
	if len(warnings) > 0 {
		log.Println("Configuration warnings:")
		for _, warning := range warnings {
			log.Printf("  - %s", warning)
		}
		log.Println("You can specify environment variables in the .env file or use command-line flags.")
	}

	// --- Cobra Root Command Setup ---
	var rootCmd = &cobra.Command{
		Use:   "call-processor",
		Short: "Call Processing Service and CLI",
		Long: `Call-Processor is a tool for processing audio call recordings.
It can run as a server or batch process audio files for transcription and analysis.

Example usage:
  call-processor server             - Run the API server
  call-processor batch -i ./clips   - Process all audio files in ./clips directory
  call-processor batch --help       - Show help for batch processing options`,
		// This PersistentPreRun will initialize shared components *before* any command runs
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			var initErr error
			log.Println("Initializing shared components...")

			// Initialize shared components and store in global vars
			DbPool, initErr = initDatabase()
			if initErr != nil {
				return fmt.Errorf("failed to initialize database: %w", initErr)
			}

			Repos = initRepositories(DbPool)

			StorageClient, initErr = initStorageClient()
			if initErr != nil {
				return fmt.Errorf("failed to initialize storage client: %w", initErr)
			}

			EventBus, initErr = initEventSystem(Repos.EventRepo)
			if initErr != nil {
				return fmt.Errorf("failed to initialize event system: %w", initErr)
			}

			// Use worker config from env or defaults
			workerConfig := worker.Config{
				WorkerCount:     getEnvInt("WORKER_COUNT", 5),
				PollingInterval: time.Duration(getEnvInt("WORKER_POLLING_INTERVAL_MS", 5000)) * time.Millisecond,
				LockDuration:    time.Duration(getEnvInt("WORKER_LOCK_DURATION_SEC", 900)) * time.Second,
				MaxRetries:      getEnvInt("WORKER_MAX_RETRIES", 3),
				RetryDelay:      time.Duration(getEnvInt("WORKER_RETRY_DELAY_SEC", 30)) * time.Second,
				JobTimeout:      time.Duration(getEnvInt("WORKER_JOB_TIMEOUT_SEC", 600)) * time.Second,
				ShutdownTimeout: time.Duration(getEnvInt("WORKER_SHUTDOWN_TIMEOUT_SEC", 30)) * time.Second,
			}
			WorkerManager = initWorkerManager(workerConfig, Repos.JobRepo)

			SttClient = initSTTClient()

			AppServices = initServices(Repos, StorageClient, EventBus, WorkerManager, SttClient)

			log.Println("Shared components initialized.")
			return nil
		},
		// Optional: Cleanup shared resources after any command finishes
		PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
			log.Println("Cleaning up shared resources...")
			if DbPool != nil {
				DbPool.Close()
			}
			// Add other cleanup if needed (e.g., stopping event bus if started in PersistentPreRun)
			log.Println("Cleanup complete.")
			return nil
		},
	}

	// --- Default command (Run API Server) ---
	var serverCmd = &cobra.Command{
		Use:   "server",
		Short: "Run the API server (default)",
		Long: `Run the call processing API server.

This command starts the API server that provides RESTful endpoints for:
- Uploading audio recordings
- Requesting transcriptions
- Analyzing call content
- Managing batch processing jobs

The server configuration is read from the .env file or environment variables.
The server will start workers to process queued jobs in the background.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			log.Println("Starting API server...")
			
			// Start the worker manager
			go func() {
				if err := WorkerManager.Start(); err != nil {
					log.Printf("Error starting worker manager: %v", err)
				}
			}()

			// Initialize API server
			controllers := initControllers(AppServices, Repos, WorkerManager)
			apiServer := initAPIServer(controllers)

			// Start the API server
			go func() {
				if err := apiServer.Start(); err != nil && err != http.ErrServerClosed {
					log.Printf("Error starting API server: %v", err)
				}
			}()

			// Wait for shutdown signal
			c := make(chan os.Signal, 1)
			signal.Notify(c, os.Interrupt, syscall.SIGTERM)

			// Block until we receive the signal
			<-c
			log.Println("Shutdown signal received, stopping API server...")
			// Implement graceful shutdown for the HTTP server if needed
			// e.g., apiServer.Shutdown(shutdownCtx)
			time.Sleep(2 * time.Second) // Give time for cleanup

			log.Println("API server stopped.")
			return nil
		},
	}
	
	// Add commands to root command
	rootCmd.AddCommand(serverCmd)
	rootCmd.RunE = serverCmd.RunE // Make server the default command

	// Add the batch command
	rootCmd.AddCommand(NewBatchCmd())

	// Execute Cobra
	if err := rootCmd.Execute(); err != nil {
		log.Fatalf("Command execution failed: %v", err)
		os.Exit(1)
	}
}