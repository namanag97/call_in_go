package stt

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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
	MaxSpeakers       int
	Vocabulary        []string
}

// TranscribeOutput represents the output from transcribing audio
type TranscribeOutput struct {
	Text             string
	Language         string
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

// Provider represents a speech-to-text provider
type Provider string

const (
	// ProviderGoogle represents Google Cloud Speech-to-Text
	ProviderGoogle Provider = "google"
	// ProviderAWS represents Amazon Transcribe
	ProviderAWS Provider = "aws"
	// ProviderAzure represents Azure Speech Services
	ProviderAzure Provider = "azure"
	// ProviderWhisper represents OpenAI Whisper
	ProviderWhisper Provider = "whisper"
)

// Config contains configuration for the STT client
type Config struct {
	Provider       Provider
	APIKey         string
	Region         string
	Endpoint       string
	TimeoutSeconds int
}

// MultiProviderClient implements Client using multiple providers with fallback
type MultiProviderClient struct {
	primaryClient   Client
	fallbackClients []Client
}

// NewMultiProviderClient creates a new MultiProviderClient
func NewMultiProviderClient(primary Client, fallbacks ...Client) *MultiProviderClient {
	return &MultiProviderClient{
		primaryClient:   primary,
		fallbackClients: fallbacks,
	}
}

// Transcribe transcribes audio using the primary provider with fallback
func (c *MultiProviderClient) Transcribe(ctx context.Context, input *TranscribeInput) (*TranscribeOutput, error) {
	// Try primary client first
	output, err := c.primaryClient.Transcribe(ctx, input)
	if err == nil {
		return output, nil
	}

	// Log primary failure
	fmt.Printf("Primary STT provider failed: %v. Trying fallbacks.\n", err)

	// Try fallbacks in order
	for i, fallback := range c.fallbackClients {
		output, err := fallback.Transcribe(ctx, input)
		if err == nil {
			return output, nil
		}
		fmt.Printf("Fallback STT provider %d failed: %v\n", i+1, err)
	}

	return nil, fmt.Errorf("all STT providers failed")
}

// GoogleSpeechClient implements Client using Google Cloud Speech-to-Text
type GoogleSpeechClient struct {
	config Config
	client *http.Client
}

// NewGoogleSpeechClient creates a new GoogleSpeechClient
func NewGoogleSpeechClient(config Config) *GoogleSpeechClient {
	return &GoogleSpeechClient{
		config: config,
		client: &http.Client{
			Timeout: time.Duration(config.TimeoutSeconds) * time.Second,
		},
	}
}

// Transcribe transcribes audio using Google Cloud Speech-to-Text
func (c *GoogleSpeechClient) Transcribe(ctx context.Context, input *TranscribeInput) (*TranscribeOutput, error) {
	startTime := time.Now()

	// Prepare request payload
	requestBody := map[string]interface{}{
		"config": map[string]interface{}{
			"encoding":        getGoogleEncoding(input.MimeType),
			"sampleRateHertz": 16000, // Assuming 16kHz, adjust as needed
			"languageCode":    input.Language,
			"enableWordTimeOffsets": input.IncludeTimestamps,
			"enableAutomaticPunctuation": true,
			"model":           "latest_long", // Use appropriate model
		},
		"audio": map[string]interface{}{
			"content": encodeBase64(input.AudioData),
		},
	}

	if input.SpeakerDiarization {
		requestBody["config"].(map[string]interface{})["diarizationConfig"] = map[string]interface{}{
			"enableSpeakerDiarization": true,
			"maxSpeakerCount":          input.MaxSpeakers,
		}
	}

	// Add vocabulary if provided
	if len(input.Vocabulary) > 0 {
		requestBody["config"].(map[string]interface{})["speechContexts"] = []map[string]interface{}{
			{
				"phrases": input.Vocabulary,
			},
		}
	}

	requestJSON, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(
		ctx,
		"POST",
		c.config.Endpoint,
		strings.NewReader(string(requestJSON)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.config.APIKey))

	// Send request
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

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error: %s - %s", resp.Status, string(body))
	}

	// Parse response
	var response struct {
		Results []struct {
			Alternatives []struct {
				Transcript string  `json:"transcript"`
				Confidence float64 `json:"confidence"`
				Words      []struct {
					StartTime struct {
						Seconds int64 `json:"seconds"`
						Nanos   int64 `json:"nanos"`
					} `json:"startTime"`
					EndTime struct {
						Seconds int64 `json:"seconds"`
						Nanos   int64 `json:"nanos"`
					} `json:"endTime"`
					Word     string  `json:"word"`
					Speaker  string  `json:"speakerTag,omitempty"`
				} `json:"words"`
			} `json:"alternatives"`
		} `json:"results"`
	}

	err = json.Unmarshal(body, &response)
	if err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Process results
	output := &TranscribeOutput{
		Language:         input.Language,
		ProcessingTimeMs: time.Since(startTime).Milliseconds(),
		Segments:         []Segment{},
	}

	var fullText strings.Builder
	var wordCount int
	var confidence float64
	var segments []Segment

	for _, result := range response.Results {
		for _, alt := range result.Alternatives {
			if alt.Transcript != "" {
				fullText.WriteString(alt.Transcript)
				confidence = alt.Confidence
				break
			}
		}
		if fullText.Len() > 0 {
			break
		}
	}

	// Process words and build segments
	var currentSegment Segment
	var wordBuffer strings.Builder

	for i, word := range response.Results[0].Alternatives[0].Words {
		wordCount++
		
		// Convert time to seconds
		startTime := float64(word.StartTime.Seconds) + float64(word.StartTime.Nanos)/1e9
		endTime := float64(word.EndTime.Seconds) + float64(word.EndTime.Nanos)/1e9
		
		// First word or new speaker
		if i == 0 || word.Speaker != currentSegment.Speaker {
			// Save previous segment if it exists
			if i > 0 {
				currentSegment.Text = wordBuffer.String()
				segments = append(segments, currentSegment)
				wordBuffer.Reset()
			}
			
			// Start new segment
			currentSegment = Segment{
				StartTime: startTime,
				EndTime:   endTime,
				Speaker:   word.Speaker,
				Confidence: confidence, // Use overall confidence as default
			}
			
			wordBuffer.WriteString(word.Word)
		} else {
			// Continue current segment
			currentSegment.EndTime = endTime
			wordBuffer.WriteString(" " + word.Word)
		}
		
		// Last word
		if i == len(response.Results[0].Alternatives[0].Words)-1 {
			currentSegment.Text = wordBuffer.String()
			segments = append(segments, currentSegment)
		}
	}

	output.Text = fullText.String()
	output.WordCount = wordCount
	output.Confidence = confidence
	output.Segments = segments
	
	// If no segments were created but we have text, create a single segment
	if len(segments) == 0 && output.Text != "" {
		segments = append(segments, Segment{
			StartTime:  0,
			EndTime:    0, // We don't know the duration
			Text:       output.Text,
			Confidence: output.Confidence,
		})
	}

	return output, nil
}

// getGoogleEncoding converts MIME type to Google encoding format
func getGoogleEncoding(mimeType string) string {
	switch mimeType {
	case "audio/wav", "audio/x-wav", "audio/wave":
		return "LINEAR16"
	case "audio/ogg", "application/ogg":
		return "OGG_OPUS"
	case "audio/mpeg", "audio/mp3":
		return "MP3"
	case "audio/flac":
		return "FLAC"
	default:
		return "ENCODING_UNSPECIFIED"
	}
}

// encodeBase64 encodes binary data to base64 string
func encodeBase64(data []byte) string {
	// Implementation omitted for brevity - use standard library base64 encoding
	return ""
}

// Additional STT client implementations (AWS, Azure, Whisper) would follow a similar pattern
// but with provider-specific API calls and response parsing