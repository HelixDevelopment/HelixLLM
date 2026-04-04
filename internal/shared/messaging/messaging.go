// Package messaging provides a distributed EventBus adapter that bridges
// in-process pub/sub with a pluggable message broker (Kafka in production,
// in-memory for tests).
//
// Architecture
//
//	DistributedBus
//	  ├── local  *events.Bus   — in-process delivery (zero latency)
//	  └── broker MessageBroker — cross-host delivery (Kafka / in-memory)
//
// Events published via DistributedBus are delivered locally AND forwarded to
// the broker for any remote subscriber on the same topic.
package messaging

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/HelixDevelopment/HelixLLM/internal/shared/events"
)

// MessageBroker is the abstraction layer for cross-host message transport.
// An in-memory implementation is provided for testing; a Kafka implementation
// is used in production.
type MessageBroker interface {
	// Publish serialises data and sends it to the named topic.
	Publish(topic string, data []byte) error

	// Subscribe registers handler to be called for every message
	// delivered to topic. Implementations must be safe to call
	// concurrently.
	Subscribe(topic string, handler func([]byte)) error

	// Close releases all broker resources.
	Close() error
}

// DistributedBus wraps an in-process events.Bus with a MessageBroker so that
// events can be propagated across hosts.
type DistributedBus struct {
	local  *events.Bus
	broker MessageBroker
	topics map[string]bool
	mu     sync.RWMutex
}

// NewDistributedBus creates a DistributedBus backed by localBus for in-process
// delivery and broker for cross-host delivery.
func NewDistributedBus(localBus *events.Bus, broker MessageBroker) *DistributedBus {
	return &DistributedBus{
		local:  localBus,
		broker: broker,
		topics: make(map[string]bool),
	}
}

// brokerMessage is the wire format for events forwarded through the broker.
type brokerMessage struct {
	Topic   string      `json:"topic"`
	Source  string      `json:"source"`
	Payload interface{} `json:"payload"`
}

// Publish delivers the event locally and forwards it to the broker so remote
// subscribers receive it too.
func (d *DistributedBus) Publish(topic events.Topic, source string, payload interface{}) {
	// Always deliver in-process.
	d.local.Publish(topic, source, payload)

	// Forward via broker so other hosts receive it.
	msg := brokerMessage{
		Topic:   string(topic),
		Source:  source,
		Payload: payload,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		// Serialisation failure is non-fatal — local delivery already succeeded.
		return
	}
	_ = d.broker.Publish(string(topic), data)
}

// Subscribe registers interest in topic on both the local bus and the broker.
// Remote messages received from the broker are re-published locally so all
// in-process subscribers are notified.
func (d *DistributedBus) Subscribe(topic events.Topic) {
	d.mu.Lock()
	alreadySubscribed := d.topics[string(topic)]
	d.topics[string(topic)] = true
	d.mu.Unlock()

	if alreadySubscribed {
		return
	}

	// Subscribe on the broker; re-publish incoming remote messages locally.
	_ = d.broker.Subscribe(string(topic), func(data []byte) {
		var msg brokerMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return
		}
		d.local.Publish(events.Topic(msg.Topic), msg.Source, msg.Payload)
	})
}

// Close shuts down the broker connection. The local bus must be closed
// separately by its owner.
func (d *DistributedBus) Close() error {
	return d.broker.Close()
}

// ---------------------------------------------------------------------------
// InMemoryBroker — synchronous in-memory broker for testing.
// ---------------------------------------------------------------------------

// InMemoryBroker implements MessageBroker with a simple in-process map.
// All Publish calls invoke registered handlers synchronously.
type InMemoryBroker struct {
	mu       sync.RWMutex
	handlers map[string][]func([]byte)
	closed   bool
}

// NewInMemoryBroker returns a ready-to-use InMemoryBroker.
func NewInMemoryBroker() *InMemoryBroker {
	return &InMemoryBroker{
		handlers: make(map[string][]func([]byte)),
	}
}

// Publish delivers data to all handlers registered for topic.
func (b *InMemoryBroker) Publish(topic string, data []byte) error {
	b.mu.RLock()
	if b.closed {
		b.mu.RUnlock()
		return fmt.Errorf("broker closed")
	}
	handlers := make([]func([]byte), len(b.handlers[topic]))
	copy(handlers, b.handlers[topic])
	b.mu.RUnlock()

	for _, h := range handlers {
		h(data)
	}
	return nil
}

// Subscribe registers handler for all messages published to topic.
func (b *InMemoryBroker) Subscribe(topic string, handler func([]byte)) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return fmt.Errorf("broker closed")
	}
	b.handlers[topic] = append(b.handlers[topic], handler)
	return nil
}

// Close marks the broker as closed and clears all handlers.
func (b *InMemoryBroker) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	b.handlers = make(map[string][]func([]byte))
	return nil
}

// ---------------------------------------------------------------------------
// KafkaBroker — production broker backed by a Kafka cluster.
// ---------------------------------------------------------------------------

// KafkaBrokerConfig holds connection parameters for a Kafka broker.
type KafkaBrokerConfig struct {
	// Brokers is a comma-separated list of Kafka bootstrap servers,
	// e.g. "kafka1:9092,kafka2:9092".
	Brokers string

	// GroupID is the consumer group identifier used for subscriptions.
	GroupID string
}

// KafkaBroker is the production MessageBroker implementation.
// The actual Kafka client is injected via the sender/receiver interfaces so
// that the package does not import a specific Kafka library at this layer —
// callers provide concrete implementations (e.g. via Sarama or franz-go).
type KafkaBroker struct {
	config KafkaBrokerConfig
	sender KafkaSender
	mu     sync.Mutex
	closed bool
}

// KafkaSender abstracts the Kafka producer operation.
type KafkaSender interface {
	Send(topic string, data []byte) error
	Close() error
}

// NewKafkaBroker creates a KafkaBroker using the provided sender.
// Subscription support requires the caller to also manage consumer setup
// outside this type, or extend via a dedicated consumer interface.
func NewKafkaBroker(cfg KafkaBrokerConfig, sender KafkaSender) *KafkaBroker {
	return &KafkaBroker{
		config: cfg,
		sender: sender,
	}
}

// Publish sends data to the Kafka topic via the injected sender.
func (k *KafkaBroker) Publish(topic string, data []byte) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.closed {
		return fmt.Errorf("kafka broker closed")
	}
	return k.sender.Send(topic, data)
}

// Subscribe is not implemented at this layer — Kafka consumer group management
// requires a longer-lived goroutine which callers set up via their chosen
// Kafka client library. This method returns an error to signal that.
func (k *KafkaBroker) Subscribe(_ string, _ func([]byte)) error {
	return fmt.Errorf("KafkaBroker: Subscribe not implemented; manage consumer groups via your Kafka client")
}

// Close shuts down the Kafka producer.
func (k *KafkaBroker) Close() error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.closed {
		return nil
	}
	k.closed = true
	return k.sender.Close()
}
