package messaging_test

import (
	"fmt"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/shared/messaging"
)

// mockKafkaSender implements messaging.KafkaSender for testing.
type mockKafkaSender struct {
	sent    []mockSendCall
	sendErr error
	closed  bool
}

type mockSendCall struct {
	Topic string
	Data  []byte
}

func (m *mockKafkaSender) Send(topic string, data []byte) error {
	if m.sendErr != nil {
		return m.sendErr
	}
	m.sent = append(m.sent, mockSendCall{Topic: topic, Data: data})
	return nil
}

func (m *mockKafkaSender) Close() error {
	m.closed = true
	return nil
}

// ---------------------------------------------------------------------------
// KafkaBroker tests
// ---------------------------------------------------------------------------

func TestKafkaBroker_Publish_Success(t *testing.T) {
	sender := &mockKafkaSender{}
	broker := messaging.NewKafkaBroker(messaging.KafkaBrokerConfig{
		Brokers: "localhost:9092",
		GroupID: "test-group",
	}, sender)

	err := broker.Publish("events.topic", []byte("hello kafka"))
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("expected 1 send call, got %d", len(sender.sent))
	}
	if sender.sent[0].Topic != "events.topic" {
		t.Errorf("topic = %q, want %q", sender.sent[0].Topic, "events.topic")
	}
	if string(sender.sent[0].Data) != "hello kafka" {
		t.Errorf("data = %q, want %q", sender.sent[0].Data, "hello kafka")
	}

	if err := broker.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestKafkaBroker_Publish_SenderError(t *testing.T) {
	sender := &mockKafkaSender{sendErr: fmt.Errorf("connection lost")}
	broker := messaging.NewKafkaBroker(messaging.KafkaBrokerConfig{
		Brokers: "localhost:9092",
	}, sender)
	defer broker.Close()

	err := broker.Publish("topic", []byte("data"))
	if err == nil {
		t.Fatal("expected error from sender, got nil")
	}
}

func TestKafkaBroker_Publish_AfterClose(t *testing.T) {
	sender := &mockKafkaSender{}
	broker := messaging.NewKafkaBroker(messaging.KafkaBrokerConfig{}, sender)

	if err := broker.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	err := broker.Publish("topic", []byte("data"))
	if err == nil {
		t.Fatal("expected error publishing to closed broker, got nil")
	}
}

func TestKafkaBroker_Subscribe_ReturnsError(t *testing.T) {
	sender := &mockKafkaSender{}
	broker := messaging.NewKafkaBroker(messaging.KafkaBrokerConfig{}, sender)
	defer broker.Close()

	err := broker.Subscribe("topic", func(_ []byte) {})
	if err == nil {
		t.Fatal("expected error from Subscribe (not implemented), got nil")
	}
}

func TestKafkaBroker_Close_Idempotent(t *testing.T) {
	sender := &mockKafkaSender{}
	broker := messaging.NewKafkaBroker(messaging.KafkaBrokerConfig{}, sender)

	if err := broker.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	// Second close should be a no-op and not error.
	if err := broker.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if !sender.closed {
		t.Error("expected sender.Close to have been called")
	}
}

func TestKafkaBroker_Close_CallsSenderClose(t *testing.T) {
	sender := &mockKafkaSender{}
	broker := messaging.NewKafkaBroker(messaging.KafkaBrokerConfig{}, sender)

	if err := broker.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !sender.closed {
		t.Error("sender.Close was not called")
	}
}

func TestKafkaBroker_MultiplePublishes(t *testing.T) {
	sender := &mockKafkaSender{}
	broker := messaging.NewKafkaBroker(messaging.KafkaBrokerConfig{}, sender)
	defer broker.Close()

	for i := 0; i < 5; i++ {
		if err := broker.Publish("t", []byte(fmt.Sprintf("msg-%d", i))); err != nil {
			t.Fatalf("Publish %d: %v", i, err)
		}
	}
	if len(sender.sent) != 5 {
		t.Errorf("expected 5 sends, got %d", len(sender.sent))
	}
}

func TestKafkaBrokerConfig_Fields(t *testing.T) {
	cfg := messaging.KafkaBrokerConfig{
		Brokers: "kafka1:9092,kafka2:9092",
		GroupID: "helix-consumers",
	}
	if cfg.Brokers != "kafka1:9092,kafka2:9092" {
		t.Errorf("Brokers = %q", cfg.Brokers)
	}
	if cfg.GroupID != "helix-consumers" {
		t.Errorf("GroupID = %q", cfg.GroupID)
	}
}
