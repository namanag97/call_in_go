package main

// Add these imports at the top of your main.go file
// Make sure these imports are included along with your existing imports

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
	
	"github.com/google/uuid"  // Make sure this is imported
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/joho/godotenv"
	
	"github.com/namanag97/call_in_go/call-processor/internal/api"
	"github.com/namanag97/call_in_go/call-processor/internal/analysis"
	"github.com/namanag97/call_in_go/call-processor/internal/domain"  // Make sure this is imported
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
	recordingRepo     repository.RecordingRepository
	transcriptionRepo repository.TranscriptionRepository
	analysisRepo      repository.AnalysisRepository
	jobRepo           repository.JobRepository
	eventRepo         repository.EventRepository
}

// Service initialization
type services struct {
	ingestionService     *ingestion.Service
	transcriptionService *transcription.Service
	analysisService      *analysis.Service
	bulkService          *bulk.Service
}

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
		api.NewRecordingController(services.ingestionService, repos.recordingRepo),
		api.NewTranscriptionController(services.transcriptionService, repos.transcriptionRepo),
		api.NewAnalysisController(services.analysisService, repos.analysisRepo),
		api.NewJobController(workerManager),
		api.NewBulkOperationsController(services.bulkService), // Add this line
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

// Fix for the repository implementations in main.go

func initRepositories(dbPool *pgxpool.Pool) repositories {
    return repositories{
        recordingRepo:     repository.NewPostgresRecordingRepository(dbPool),
        // Use empty implementations or implement these as needed
        transcriptionRepo: &dummyTranscriptionRepository{},
        analysisRepo:      &dummyAnalysisRepository{},
        jobRepo:           &dummyJobRepository{},
        eventRepo:         &dummyEventRepository{},
    }
}

// Dummy repository implementations
type dummyTranscriptionRepository struct{}
func (d *dummyTranscriptionRepository) Create(ctx context.Context, transcription *domain.Transcription) error { return nil }
func (d *dummyTranscriptionRepository) Get(ctx context.Context, id uuid.UUID) (*domain.Transcription, error) { return nil, nil }
func (d *dummyTranscriptionRepository) GetByRecordingID(ctx context.Context, recordingID uuid.UUID) (*domain.Transcription, error) { return nil, nil }
func (d *dummyTranscriptionRepository) Update(ctx context.Context, transcription *domain.Transcription) error { return nil }
func (d *dummyTranscriptionRepository) Delete(ctx context.Context, id uuid.UUID) error { return nil }
func (d *dummyTranscriptionRepository) List(ctx context.Context, filter domain.TranscriptionFilter, pagination domain.Pagination) ([]*domain.Transcription, int, error) { return nil, 0, nil }
func (d *dummyTranscriptionRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status domain.TranscriptionStatus) error { return nil }
func (d *dummyTranscriptionRepository) AddSegment(ctx context.Context, segment *domain.TranscriptionSegment) error { return nil }
func (d *dummyTranscriptionRepository) GetSegments(ctx context.Context, transcriptionID uuid.UUID) ([]domain.TranscriptionSegment, error) { return nil, nil }

type dummyAnalysisRepository struct{}
func (d *dummyAnalysisRepository) Create(ctx context.Context, analysis *domain.Analysis) error { return nil }
func (d *dummyAnalysisRepository) Get(ctx context.Context, id uuid.UUID) (*domain.Analysis, error) { return nil, nil }
func (d *dummyAnalysisRepository) GetByRecordingIDAndType(ctx context.Context, recordingID uuid.UUID, analysisType string) (*domain.Analysis, error) { return nil, nil }
func (d *dummyAnalysisRepository) Update(ctx context.Context, analysis *domain.Analysis) error { return nil }
func (d *dummyAnalysisRepository) Delete(ctx context.Context, id uuid.UUID) error { return nil }
func (d *dummyAnalysisRepository) List(ctx context.Context, filter domain.AnalysisFilter, pagination domain.Pagination) ([]*domain.Analysis, int, error) { return nil, 0, nil }
func (d *dummyAnalysisRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status domain.AnalysisStatus) error { return nil }
func (d *dummyAnalysisRepository) ListByRecordingID(ctx context.Context, recordingID uuid.UUID) ([]*domain.Analysis, error) { return nil, nil }

type dummyJobRepository struct{}
func (d *dummyJobRepository) Create(ctx context.Context, job *domain.Job) error { return nil }
func (d *dummyJobRepository) Get(ctx context.Context, id uuid.UUID) (*domain.Job, error) { return nil, nil }
func (d *dummyJobRepository) Update(ctx context.Context, job *domain.Job) error { return nil }
func (d *dummyJobRepository) Delete(ctx context.Context, id uuid.UUID) error { return nil }
func (d *dummyJobRepository) List(ctx context.Context, jobType string, status domain.JobStatus, limit int) ([]*domain.Job, error) { return nil, nil }
func (d *dummyJobRepository) AcquireJobs(ctx context.Context, workerID string, jobTypes []string, limit int) ([]*domain.Job, error) { return nil, nil }
func (d *dummyJobRepository) MarkComplete(ctx context.Context, id uuid.UUID) error { return nil }
func (d *dummyJobRepository) MarkFailed(ctx context.Context, id uuid.UUID, err error) error { return nil }
func (d *dummyJobRepository) ReleaseJob(ctx context.Context, id uuid.UUID) error { return nil }
func (d *dummyJobRepository) CountByStatus(ctx context.Context, status domain.JobStatus) (int, error) { return 0, nil }

type dummyEventRepository struct{}
func (d *dummyEventRepository) Create(ctx context.Context, event *domain.Event) error { return nil }
func (d *dummyEventRepository) Get(ctx context.Context, id uuid.UUID) (*domain.Event, error) { return nil, nil }
func (d *dummyEventRepository) List(ctx context.Context, entityType string, entityID uuid.UUID, pagination domain.Pagination) ([]*domain.Event, int, error) { return nil, 0, nil }
func (d *dummyEventRepository) ListByType(ctx context.Context, eventType string, pagination domain.Pagination) ([]*domain.Event, int, error) { return nil, 0, nil }



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
    // Comment out or remove the unused provider variable
    // provider := stt.Provider(getEnv("STT_PROVIDER", "google"))
    
    // Create provider-specific clients
    googleConfig := stt.Config{
        Provider:       stt.ProviderGoogle,
        APIKey:         getEnv("GOOGLE_STT_API_KEY", ""),
        Region:         getEnv("GOOGLE_STT_REGION", "global"),
        Endpoint:       getEnv("GOOGLE_STT_ENDPOINT", "https://speech.googleapis.com/v1/speech:recognize"),
        TimeoutSeconds: getEnvInt("STT_TIMEOUT_SEC", 60),
    }
    googleClient := stt.NewGoogleSpeechClient(googleConfig)
    
    // For now, just use Google as primary with no fallbacks
    return stt.NewMultiProviderClient(googleClient)
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
		repos.recordingRepo,
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
		repos.recordingRepo,
		repos.transcriptionRepo,
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
		repos.recordingRepo,
		repos.transcriptionRepo,
		repos.analysisRepo,
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
		repos.jobRepo,
		repos.recordingRepo,
		ingestionService,
		transcriptionService,
		eventBus,
		workerManager,
	)
	
	// Register analysis processors
	analysisService.RegisterProcessor(analysis.NewSentimentProcessor())
	// TODO: Register additional processors as needed
	
	return services{
		ingestionService:     ingestionService,
		transcriptionService: transcriptionService,
		analysisService:      analysisService,
		bulkService:          bulkService,
	}
}

func main() {
	// Load environment variables
	err := godotenv.Load()
	if err != nil {
		log.Println("Warning: Error loading .env file:", err)
	}
	
	// Create context that listens for termination signals
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	// Set up signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		log.Printf("Received signal %v, initiating shutdown...", sig)
		cancel()
	}()
	
	// Initialize database connection
	dbPool, err := initDatabase()
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer dbPool.Close()
	
	// Initialize repositories
	repos := initRepositories(dbPool)
	
	// Initialize storage client
	storageClient, err := initStorageClient()
	if err != nil {
		log.Fatalf("Failed to initialize storage client: %v", err)
	}
	
	// Initialize event system
	eventBus, err := initEventSystem(repos.eventRepo)
	if err != nil {
		log.Fatalf("Failed to initialize event system: %v", err)
	}
	
	// Start event bus
	if kafkaEventBus, ok := eventBus.(*event.KafkaEventBus); ok {
		err = kafkaEventBus.Start()
		if err != nil {
			log.Fatalf("Failed to start event bus: %v", err)
		}
		defer kafkaEventBus.Stop()
	}
	
	// Initialize worker manager
	workerManager := initWorkerManager(repos.jobRepo)
	
	// Initialize STT client
	sttClient := initSTTClient()
	
	// Initialize services
	services := initServices(repos, storageClient, eventBus, workerManager, sttClient)
	
	// Start worker manager
	err = workerManager.Start()
	if err != nil {
		log.Fatalf("Failed to start worker manager: %v", err)
	}
	defer workerManager.Stop()
	
	// Initialize API controllers
	controllers := initControllers(services, repos, workerManager)
	
	// Initialize API server
	apiServer := initAPIServer(controllers)
	
	// Start API server in a goroutine
	go func() {
		log.Printf("Starting API server on port %s...", getEnv("API_PORT", "8080"))
		if err := apiServer.Start(); err != nil {
			log.Fatalf("Failed to start API server: %v", err)
		}
	}()
	
	// Wait for context cancellation (from signal handler)
	<-ctx.Done()
	log.Println("Shutting down gracefully...")
	
	// Perform cleanup
	time.Sleep(2 * time.Second) // Give ongoing requests time to complete
	
	log.Println("Shutdown complete")
}