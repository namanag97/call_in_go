# Go Project - Call Processing System

## TL;DR
A Go-based API for processing call recordings, providing automated transcription and analysis. Upload call recordings via REST API, process them with a background worker system, and retrieve transcriptions and analytical insights.

## Quick Setup
```bash
# Clone the repository
git clone git@github.com:namanag97/call_in_go.git
cd call_in_go

# Set up the database
./reinit_postgres.sh

# Install dependencies
go mod download

# Build the application
go build -o ./bin/call-processor ./call-processor

# Run the application
./bin/call-processor
```

## Detailed Setup Instructions

### Prerequisites
- Go 1.18 or higher
- PostgreSQL 13 or higher
- S3-compatible storage (like MinIO or AWS S3)
- Google Cloud account (for Speech-to-Text API)

### Environment Setup
1. Create a `.env` file in the project root with the following variables:
```
DB_HOST=localhost
DB_PORT=5432
DB_USER=postgres
DB_PASSWORD=postgres
DB_NAME=go_project
STORAGE_ENDPOINT=localhost:9000
STORAGE_ACCESS_KEY=minioadmin
STORAGE_SECRET_KEY=minioadmin
STORAGE_BUCKET=call-recordings
GOOGLE_APPLICATION_CREDENTIALS=/path/to/google-credentials.json
```

### Database Setup
To initialize the PostgreSQL database for this project, run:

```bash
./reinit_postgres.sh
```

This script will:
1. Stop any running PostgreSQL service
2. Remove existing data directory
3. Initialize a new PostgreSQL database
4. Start the PostgreSQL service
5. Create a database called `go_project`

### Running the Application
```bash
# Development mode
go run ./call-processor

# Production mode
go build -o ./bin/call-processor ./call-processor
./bin/call-processor
```

### Running Tests
```bash
go test ./...
```

## Project Structure

- `call-processor/` - Contains the call processing application 

## Contributing

We welcome contributions to this project! Here's how you can help:

1. **Fork the repository** - Create your own fork of the project
2. **Create a feature branch** - `git checkout -b feature/your-feature-name`
3. **Make your changes** - Implement your feature or bug fix
4. **Run tests** - Ensure your changes don't break existing functionality
5. **Commit your changes** - Use descriptive commit messages
6. **Push to your branch** - `git push origin feature/your-feature-name`
7. **Open a pull request** - Submit your changes for review

Please follow the code style guidelines and include appropriate tests with your changes.

### Development Workflow

1. Pick an issue from the issue tracker
2. Create a branch for your work
3. Implement the feature or fix
4. Write tests for your changes
5. Update documentation if necessary
6. Submit a pull request

# Call Processing API Documentation

## Overview
This Go-based API provides call recording processing capabilities, including ingestion, transcription, and analysis of audio recordings.

## API Endpoints

### Recordings
- `POST /recordings`: Upload a new call recording
- `GET /recordings/:id`: Get recording by ID
- `GET /recordings`: List recordings with filtering options
- `DELETE /recordings/:id`: Delete a recording

### Transcriptions
- `POST /recordings/:id/transcribe`: Start transcription of a recording
- `GET /transcriptions/:id`: Get transcription by ID
- `GET /recordings/:id/transcription`: Get transcription by recording ID

### Analysis
- `POST /recordings/:id/analyze`: Analyze a recording's transcription
- `GET /analysis/:id`: Get analysis results by ID
- `GET /recordings/:id/analysis`: Get analysis results for a recording

### Jobs
- `GET /jobs/:id`: Get job status by ID
- `GET /jobs/stats`: Get worker statistics
- `GET /jobs/health`: Get worker health
- `POST /jobs/maintenance/clear-stuck`: Clear stuck jobs older than specified duration

### Bulk Operations
- Bulk endpoints for processing multiple recordings

## Architecture

The system uses a worker-based processing model:
1. **Job Queue**: Tasks are queued as jobs with specific types
2. **Worker Manager**: Coordinates job processing with handlers for each job type
3. **Repository Layer**: Interface for data storage (PostgreSQL implementation)
4. **Service Layer**: Business logic, separate from API controllers

## Key Components

- **Worker System**: Background processing with retry logic, stuck job handling
- **Storage**: S3-compatible storage for recordings
- **STT**: Speech-to-text processing using Google Speech API
- **Event Bus**: In-memory or Kafka-based event handling

## Usage Example

```bash
# Upload a recording
curl -X POST -F "file=@recording.mp3" \
  -F "metadata={\"callType\":\"customer_service\"}" \
  http://localhost:8080/recordings

# Start transcription
curl -X POST http://localhost:8080/recordings/123e4567-e89b-12d3-a456-426614174000/transcribe

# Get transcription
curl http://localhost:8080/transcriptions/123e4567-e89b-12d3-a456-426614174000

# Check job status
curl http://localhost:8080/jobs/123e4567-e89b-12d3-a456-426614174000
```

## Configuration
Configuration is handled via environment variables with reasonable defaults, loaded from `.env` file when available.
