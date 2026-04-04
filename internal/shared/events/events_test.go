package events_test

import (
	"context"
	"testing"
	"time"

	"digital.vasic.eventbus/pkg/event"

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

func TestPublishAsync(t *testing.T) {
	bus := events.NewBus()
	defer bus.Close()

	sub := bus.Subscribe(events.TopicServerStarted)

	bus.PublishAsync(events.TopicServerStarted, "async-source", "started")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	select {
	case evt := <-sub.Channel:
		if evt.Source != "async-source" {
			t.Errorf("Source = %q, want %q", evt.Source, "async-source")
		}
		if evt.Payload != "started" {
			t.Errorf("Payload = %v, want %q", evt.Payload, "started")
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for async event")
	}
}

func TestSubscribeAll(t *testing.T) {
	bus := events.NewBus()
	defer bus.Close()

	sub := bus.SubscribeAll()

	bus.Publish(events.TopicServerStarted, "src1", "payload1")
	bus.Publish(events.TopicHealthChanged, "src2", "payload2")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	received := 0
	for received < 2 {
		select {
		case <-sub.Channel:
			received++
		case <-ctx.Done():
			t.Fatalf("timed out: received %d events, want 2", received)
		}
	}
}

func TestClose(t *testing.T) {
	bus := events.NewBus()
	err := bus.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

func TestMultipleSubscribers(t *testing.T) {
	bus := events.NewBus()
	defer bus.Close()

	sub1 := bus.Subscribe(events.TopicModeChanged)
	sub2 := bus.Subscribe(events.TopicModeChanged)

	bus.Publish(events.TopicModeChanged, "test", "gateway")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	for _, sub := range []*event.Subscription{sub1, sub2} {
		select {
		case evt := <-sub.Channel:
			if evt.Payload != "gateway" {
				t.Errorf("Payload = %v, want %q", evt.Payload, "gateway")
			}
		case <-ctx.Done():
			t.Fatal("timed out waiting for event")
		}
	}
}
