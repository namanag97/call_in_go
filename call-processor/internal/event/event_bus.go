package event

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/namanag97/call_in_go/call-processor/domain"
	"github.com/namanag97/call_in_go/call-processor/repository"

	"github.com/segmentio/kafka-go"
)

// System errors
var (
	ErrPublishFailed   = errors.New("failed to publish event")
	ErrSubscribeFailed = errors.New("failed to subscribe to events")
	ErrInvalidTopic    = errors.New("invalid topic")
)

// Publisher defines the interface for publishing events
type Publisher interface {
	Publish(ctx context.Context, event *domain.Event) error
}

// Subscriber defines the interface for subscribing to events
type Subscriber interface {
	Subscribe(ctx context.Context, eventTypes []string, handler Handler) error
	Unsubscribe(handler Handler)
}

// EventBus combines Publisher and Subscriber interfaces
type EventBus interface {
	Publisher
	Subscriber
}

// Handler defines the interface for handling events
type Handler interface {
	Handle(ctx context.Context, event *domain.Event) error
	ID() string
}

// Config contains configuration for the event system
type Config struct {
	KafkaBrokers      []string
	KafkaTopic        string
	KafkaConsumerGroup string
	RetryCount        int
	RetryDelay        time.Duration
	EventBufferSize   int
}

// KafkaEventBus implements EventBus using Kafka
type KafkaEventBus struct {
	config          Config
	writer          *kafka.Writer
	reader          *kafka.Reader
	eventRepository repository.EventRepository
	handlers        map[string][]Handler
	mu              sync.RWMutex
	ctx             context.Context
	cancel          context.CancelFunc
}

// NewKafkaEventBus creates a new KafkaEventBus
func NewKafkaEventBus(config Config, eventRepository repository.EventRepository) (*KafkaEventBus, error) {
	// Validate config
	if len(config.KafkaBrokers) == 0 {
		return nil, errors.New("no Kafka brokers specified")
	}
	if config.KafkaTopic == "" {
		return nil, errors.New("Kafka topic is required")
	}
	if config.KafkaConsumerGroup == "" {
		return nil, errors.New("Kafka consumer group is required")
	}

	// Create Kafka writer
	writer := &kafka.Writer{
		Addr:     kafka.TCP(config.KafkaBrokers...),
		Topic:    config.KafkaTopic,
		Balancer: &kafka.LeastBytes{},
		// Enable idempotent writes to prevent duplicates in case of retries
		RequiredAcks: kafka.RequireAll,
	}

	// Create Kafka reader
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:     config.KafkaBrokers,
		Topic:       config.KafkaTopic,
		GroupID:     config.KafkaConsumerGroup,
		MinBytes:    10e3,    // 10KB
		MaxBytes:    10e6,    // 10MB
		StartOffset: kafka.FirstOffset,
	})

	// Create context for reader
	ctx, cancel := context.WithCancel(context.Background())

	return &KafkaEventBus{
		config:          config,
		writer:          writer,
		reader:          reader,
		eventRepository: eventRepository,
		handlers:        make(map[string][]Handler),
		ctx:             ctx,
		cancel:          cancel,
	}, nil
}

// Start starts the event bus
func (b *KafkaEventBus) Start() error {
	go b.consumeEvents()
	return nil
}

// Stop stops the event bus
func (b *KafkaEventBus) Stop() error {
	b.cancel()
	b.writer.Close()
	b.reader.Close()
	return nil
}

// Publish publishes an event to the Kafka topic
func (b *KafkaEventBus) Publish(ctx context.Context, event *domain.Event) error {
	// Store event in database first for audit and recovery
	err := b.eventRepository.Create(ctx, event)
	if err != nil {
		return fmt.Errorf("failed to store event: %w", err)
	}

	// Marshal event to JSON
	eventJSON, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	// Publish to Kafka with retries
	for i := 0; i <= b.config.RetryCount; i++ {
		err = b.writer.WriteMessages(ctx, kafka.Message{
			Key:   []byte(event.ID.String()),
			Value: eventJSON,
			Headers: []kafka.Header{
				{Key: "event_type", Value: []byte(event.EventType)},
				{Key: "entity_type", Value: []byte(event.EntityType)},
				{Key: "entity_id", Value: []byte(event.EntityID.String())},
				{Key: "created_at", Value: []byte(event.CreatedAt.Format(time.RFC3339))},
			},
		})

		if err == nil {
			break
		}

		if i < b.config.RetryCount {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(b.config.RetryDelay):
				// Retry after delay
			}
		}
	}

	if err != nil {
		return fmt.Errorf("%w: %v", ErrPublishFailed, err)
	}

	return nil
}

// Subscribe registers a handler for specific event types
func (b *KafkaEventBus) Subscribe(ctx context.Context, eventTypes []string, handler Handler) error {
	if handler == nil {
		return errors.New("handler cannot be nil")
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	for _, eventType := range eventTypes {
		b.handlers[eventType] = append(b.handlers[eventType], handler)
	}

	return nil
}

// Unsubscribe removes a handler
func (b *KafkaEventBus) Unsubscribe(handler Handler) {
	if handler == nil {
		return
	}

	handlerID := handler.ID()

	b.mu.Lock()
	defer b.mu.Unlock()

	for eventType, handlers := range b.handlers {
		newHandlers := make([]Handler, 0, len(handlers))
		for _, h := range handlers {
			if h.ID() != handlerID {
				newHandlers = append(newHandlers, h)
			}
		}
		b.handlers[eventType] = newHandlers
	}
}

// consumeEvents listens for events from Kafka and dispatches them to handlers
func (b *KafkaEventBus) consumeEvents() {
	for {
		select {
		case <-b.ctx.Done():
			return
		default:
			// Continue processing
		}

		// Read message from Kafka
		msg, err := b.reader.ReadMessage(b.ctx)
		if err != nil {
			// Check if context was canceled
			if b.ctx.Err() != nil {
				return
			}

			fmt.Printf("Error reading Kafka message: %v\n", err)
			continue
		}

		// Parse event
		var event domain.Event
		err = json.Unmarshal(msg.Value, &event)
		if err != nil {
			fmt.Printf("Error parsing event: %v\n", err)
			continue
		}

		// Dispatch event to handlers
		b.dispatchEvent(&event)
	}
}

// dispatchEvent dispatches an event to all registered handlers for its type
func (b *KafkaEventBus) dispatchEvent(event *domain.Event) {
	b.mu.RLock()
	handlers := b.handlers[event.EventType]
	b.mu.RUnlock()

	// Create a context with timeout for handlers
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create a WaitGroup to wait for all handlers to complete
	var wg sync.WaitGroup
	for _, handler := range handlers {
		wg.Add(1)
		go func(h Handler) {
			defer wg.Done()
			err := h.Handle(ctx, event)
			if err != nil {
				fmt.Printf("Error handling event %s by %s: %v\n", event.ID, h.ID(), err)
			}
		}(handler)
	}

	wg.Wait()
}

// InMemoryEventBus implements EventBus in memory (useful for testing or simple deployments)
type InMemoryEventBus struct {
	handlers map[string][]Handler
	mu       sync.RWMutex
}

// NewInMemoryEventBus creates a new InMemoryEventBus
func NewInMemoryEventBus() *InMemoryEventBus {
	return &InMemoryEventBus{
		handlers: make(map[string][]Handler),
	}
}

// Publish publishes an event in memory
func (b *InMemoryEventBus) Publish(ctx context.Context, event *domain.Event) error {
	b.mu.RLock()
	defer b.mu.RUnlock()

	handlers := b.handlers[event.EventType]
	for _, handler := range handlers {
		// Execute handlers in goroutines to simulate async behavior
		go func(h Handler, e *domain.Event) {
			err := h.Handle(context.Background(), e)
			if err != nil {
				fmt.Printf("Error handling event %s by %s: %v\n", e.ID, h.ID(), err)
			}
		}(handler, event)
	}

	return nil
}

// Subscribe registers a handler for specific event types
func (b *InMemoryEventBus) Subscribe(ctx context.Context, eventTypes []string, handler Handler) error {
	if handler == nil {
		return errors.New("handler cannot be nil")
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	for _, eventType := range eventTypes {
		b.handlers[eventType] = append(b.handlers[eventType], handler)
	}

	return nil
}

// Unsubscribe removes a handler
func (b *InMemoryEventBus) Unsubscribe(handler Handler) {
	if handler == nil {
		return
	}

	handlerID := handler.ID()

	b.mu.Lock()
	defer b.mu.Unlock()

	for eventType, handlers := range b.handlers {
		newHandlers := make([]Handler, 0, len(handlers))
		for _, h := range handlers {
			if h.ID() != handlerID {
				newHandlers = append(newHandlers, h)
			}
		}
		b.handlers[eventType] = newHandlers
	}
}

// EventHandler is a base type for implementing Handler
type EventHandler struct {
	id       string
	callback func(ctx context.Context, event *domain.Event) error
}

// NewEventHandler creates a new EventHandler
func NewEventHandler(id string, callback func(ctx context.Context, event *domain.Event) error) *EventHandler {
	return &EventHandler{
		id:       id,
		callback: callback,
	}
}

// Handle handles an event by calling the callback
func (h *EventHandler) Handle(ctx context.Context, event *domain.Event) error {
	return h.callback(ctx, event)
}

// ID returns the handler ID
func (h *EventHandler) ID() string {
	return h.id
}

// DeadLetterHandler logs events that failed processing
type DeadLetterHandler struct {
	id string
}

// NewDeadLetterHandler creates a new DeadLetterHandler
func NewDeadLetterHandler() *DeadLetterHandler {
	return &DeadLetterHandler{
		id: "dead_letter_handler",
	}
}

// Handle logs the dead letter event
func (h *DeadLetterHandler) Handle(ctx context.Context, event *domain.Event) error {
	// In a real system, this would store the event in a dead letter queue
	// or dedicated database table for later analysis
	fmt.Printf("DEAD LETTER: Failed to process event %s of type %s for entity %s\n",
		event.ID, event.EventType, event.EntityID)
	return nil
}

// ID returns the handler ID
func (h *DeadLetterHandler) ID() string {
	return h.id
}