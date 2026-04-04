package events_test

import (
	"context"
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/shared/events"
)

func TestNewBus(t *testing.T) {
	bus := events.NewBus()
	if bus == nil {
		t.Fatal("NewBus() returned nil")
	}
	defer bus.Close()
}

func TestPublishSubscribe(t *testing.T) {
	bus := events.NewBus()
	defer bus.Close()

	sub := bus.Subscribe(events.TopicHealthChanged)

	bus.Publish(events.TopicHealthChanged, "test-source", "healthy")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	select {
	case evt := <-sub.Channel:
		if evt.Source != "test-source" {
			t.Errorf("Source = %q, want %q", evt.Source, "test-source")
		}
		if evt.Payload != "healthy" {
			t.Errorf("Payload = %v, want %q", evt.Payload, "healthy")
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for event")
	}
}

func TestTopicConstants(t *testing.T) {
	topics := []events.Topic{
		events.TopicServerStarted,
		events.TopicServerStopped,
		events.TopicHealthChanged,
		events.TopicConfigReloaded,
		events.TopicModeChanged,
	}
	for _, topic := range topics {
		if topic == "" {
			t.Error("topic constant is empty")
		}
	}
}
