package common

import (
	"github.com/google/uuid"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/namanag97/call_in_go/call-processor/internal/event"
	"github.com/namanag97/call_in_go/call-processor/internal/repository"
	"github.com/namanag97/call_in_go/call-processor/internal/storage"
	"github.com/namanag97/call_in_go/call-processor/internal/transcription"
	"github.com/namanag97/call_in_go/call-processor/internal/worker"
)

// Shared components accessible by both main and batch
var (
	DbPool        *pgxpool.Pool
	Repos         repository.Repositories
	StorageClient storage.Client
	EventBus      event.EventBus
	WorkerManager worker.Manager
	S3BucketName  string
	AppServices   Services
)

// Services holds initialized application services
type Services struct {
	TranscriptionService *transcription.Service
}

// Repository initialization
type Repositories struct {
	RecordingRepo     repository.RecordingRepository
	TranscriptionRepo repository.TranscriptionRepository
	// Other repositories
}