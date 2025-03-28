package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
	
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/joho/godotenv"
	
	"github.com/namanag97/call_in_go/call-processor/internal/api"
	"github.com/namanag97/call_in_go/call-processor/internal/analysis"
	"github.com/namanag97/call_in_go/call-processor/internal/event"
	"github.com/namanag97/call_in_go/call-processor/internal/ingestion"
	"github.com/namanag97/call_in_go/call-processor/internal/repository"
	"github.com/namanag97/call_in_go/call-processor/internal/storage"
	"github.com/namanag97/call_in_go/call-processor/internal/transcription"
	"github.com/namanag97/call_in_go/call-processor/internal/worker"
)

func main() {
	// Load environment variables
	err := godotenv.Load()
	if err != nil {
		log.Println("Warning: Error loading .env file:", err)
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
type repositories struct {
	recordingRepo     repository.RecordingRepository
	transcriptionRepo repository.TranscriptionRepository
	analysisRepo      repository.AnalysisRepository
	jobRepo           repository.JobRepository
	eventRepo         repository.EventRepository
}

func initRepositories(dbPool *pgxpool.Pool) repositories {
	return repositories{
		recordingRepo:     repository.NewPostgresRecordingRepository(dbPool),
		transcriptionRepo: repository.NewPostgresTranscriptionRepository(dbPool),
		analysisRepo:      repository.NewPostgresAnalysisRepository(dbPool),
		jobRepo:           repository.NewPostgresJobRepository(dbPool),
		eventRepo:         repository.NewPostgresEventRepository(dbPool),
	}
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

// Worker manager initialization
func initWorkerManager(jobRepo repository.JobRepository) worker.Manager {
	config := worker.Config{
		WorkerCount:     getEnvInt("WORKER_COUNT", 5),
		PollingInterval: time.Duration(getEnvInt("WORKER_POLLING_INTERVAL_MS", 5000)) * time.Millisecond,
		LockDuration:    time.Duration(getEnvInt("WORKER_LOCK_DURATION_SEC", 900)) * time.Second,
		MaxRetries:      getEnvInt("WORKER_MAX_RETRIES", 3),
		RetryDelay:      time.Duration(getEnvInt("WORKER_RETRY_DELAY_SEC", 30)) * time.Second,
		JobTimeout:      time.Duration(getEnvInt("WORKER_JOB_TIMEOUT_SEC", 600)) * time.Second,
		ShutdownTimeout: time.Duration(getEnvInt("WORKER_SHUTDOWN_TIMEOUT_SEC", 30)) * time.Second,
	}
	
	return worker.NewWorkerManager(config, jobRepo)
}

// STT client initialization
func initSTTClient() stt.Client {
	// Determine which provider to use
	provider := stt.Provider(getEnv("STT_PROVIDER", "google"))
	
	// Create provider-specific clients
	googleConfig := stt.Config{
		Provider:       stt.ProviderGoogle,
		APIKey:         getEnv("GOOGLE_STT_API_KEY", ""),
		Region:         getEnv("GOOGLE_STT_REGION", "global"),
		Endpoint:       getEnv("GOOGLE_STT_ENDPOINT", "https://speech.googleapis.com/v1/speech:recognize"),
		TimeoutSeconds: getEnvInt("STT_TIMEOUT_SEC", 60),
	}
	googleClient := stt.NewGoogleSpeechClient(googleConfig)
	
	// Note: For a real system, you would implement and add other providers here
	// such as AWS, Azure, Whisper, etc.
	
	// For now, just use Google as primary with no fallbacks
	return stt.NewMultiProviderClient(googleClient)
}

// Service initialization
type services struct {
	ingestionService     *ingestion.Service
	transcriptionService *transcription.Service
	analysisService      *analysis.Service
}

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
	
	// Register analysis processors
	analysisService.RegisterProcessor(analysis.NewSentimentProcessor())
	// TODO: Register additional processors as needed
	
	return services{
		ingestionService:     ingestionService,
		transcriptionService: transcriptionService,
		analysisService:      analysisService,
	}
}