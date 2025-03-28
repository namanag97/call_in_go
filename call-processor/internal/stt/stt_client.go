package stt

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

// Client interface for speech-to-text operations
type Client interface {
	Transcribe(ctx context.Context, input *TranscribeInput) (*TranscribeOutput, error)
}

// TranscribeInput represents input for transcribing audio
type TranscribeInput struct {
	AudioData         []byte
	MimeType          string
	Language          string
	IncludeTimestamps bool
	SpeakerDiarization bool
}

// TranscribeOutput represents the output from transcribing audio
type TranscribeOutput struct {
	Text             string
	DurationSeconds  float64
	Confidence       float64
	Segments         []Segment
	WordCount        int
	ProcessingTimeMs int64
}

// Segment represents a time-aligned segment of the transcript
type Segment struct {
	StartTime  float64
	EndTime    float64
	Text       string
	Speaker    string
	Confidence float64
}

// Config contains configuration for the ElevenLabs STT client
type Config struct {
	APIKey         string
	Endpoint       string
	TimeoutSeconds int
}

// ElevenLabsClient implements Client using ElevenLabs Speech-to-Text API
type ElevenLabsClient struct {
	config Config
	client *http.Client
}

// NewElevenLabsClient creates a new ElevenLabsClient
func NewElevenLabsClient(config Config) *ElevenLabsClient {
	// Set default endpoint if not provided
	if config.Endpoint == "" {
		config.Endpoint = "https://api.elevenlabs.io/v1/speech-to-text/transcribe"
	}
	
	// Set default timeout if not provided
	if config.TimeoutSeconds <= 0 {
		config.TimeoutSeconds = 300 // 5 minutes default
	}
	
	return &ElevenLabsClient{
		config: config,
		client: &http.Client{
			Timeout: time.Duration(config.TimeoutSeconds) * time.Second,
		},
	}
}

// Transcribe transcribes audio using ElevenLabs API
func (c *ElevenLabsClient) Transcribe(ctx context.Context, input *TranscribeInput) (*TranscribeOutput, error) {
	
	
	// Determine file extension based on mime type
	fileExt := ".mp3" // Default
	switch input.MimeType {
	case "audio/wav", "audio/x-wav":
		fileExt = ".wav"
	case "audio/aac":
		fileExt = ".aac"
	case "audio/m4a":
		fileExt = ".m4a"
	case "audio/ogg":
		fileExt = ".ogg"
	case "audio/flac":
		fileExt = ".flac"
	}
	
	// Create multipart form
	var formBuf bytes.Buffer
	formWriter := multipart.NewWriter(&formBuf)
	
	// Add file field
	fileWriter, err := formWriter.CreateFormFile("file", "audio"+fileExt)
	if err != nil {
		return nil, fmt.Errorf("failed to create form file: %w", err)
	}
	
	if _, err := fileWriter.Write(input.AudioData); err != nil {
		return nil, fmt.Errorf("failed to write audio data: %w", err)
	}
	
	// Add other form fields
	if err := formWriter.WriteField("model_id", "scribe_v1"); err != nil {
		return nil, fmt.Errorf("failed to add model_id field: %w", err)
	}
	
	if err := formWriter.WriteField("tag_audio_events", "true"); err != nil {
		return nil, fmt.Errorf("failed to add tag_audio_events field: %w", err)
	}
	
	// Add language code if provided
	if input.Language != "" {
		if err := formWriter.WriteField("language_code", input.Language); err != nil {
			return nil, fmt.Errorf("failed to add language_code field: %w", err)
		}
	}
	
	// Add diarization if requested
	if input.SpeakerDiarization {
		if err := formWriter.WriteField("diarize", "true"); err != nil {
			return nil, fmt.Errorf("failed to add diarize field: %w", err)
		}
	}
	
	formWriter.Close()
	
	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", c.config.Endpoint, &formBuf)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	
	req.Header.Set("Content-Type", formWriter.FormDataContentType())
	req.Header.Set("xi-api-key", c.config.APIKey)
	
	// Execute request
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	
	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	
	// Check for error response
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error: %s - %s", resp.Status, string(body))
	}
	
	// Parse response
	var response struct {
		Text      string  `json:"text"`
		Segments  []struct {
			Text      string  `json:"text"`
			StartTime float64 `json:"start_time"`
			EndTime   float64 `json:"end_time"`
			Speaker   string  `json:"speaker"`
			Confidence float64 `json:"confidence"`
		} `json:"segments"`
		Duration       float64 `json:"duration"`
		WordCount      int     `json:"word_count"`
		Confidence     float64 `json:"confidence"`
		ProcessingTime int64   `json:"processing_time_ms"`
	}
	
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	
	// Convert to our output format
	output := &TranscribeOutput{
		Text:            response.Text,
		DurationSeconds: response.Duration,
		Confidence:      response.Confidence,
		WordCount:       response.WordCount,
		ProcessingTimeMs: response.ProcessingTime,
		Segments:        make([]Segment, len(response.Segments)),
	}
	
	// Convert segments
	for i, seg := range response.Segments {
		output.Segments[i] = Segment{
			StartTime:  seg.StartTime,
			EndTime:    seg.EndTime,
			Text:       seg.Text,
			Speaker:    seg.Speaker,
			Confidence: seg.Confidence,
		}
	}
	
	return output, nil
}

// Utility function to encode binary data to base64 string
func encodeBase64(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}