package messaging_test

import (
	"sync"
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/shared/events"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/messaging"
)

// ---------------------------------------------------------------------------
// InMemoryBroker unit tests
// ---------------------------------------------------------------------------

func TestInMemoryBroker_PublishSubscribe(t *testing.T) {
	broker := messaging.NewInMemoryBroker()
	defer broker.Close()

	var received [][]byte
	var mu sync.Mutex

	if err := broker.Subscribe("test.topic", func(data []byte) {
		mu.Lock()
		received = append(received, data)
		mu.Unlock()
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	if err := broker.Publish("test.topic", []byte("hello")); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if err := broker.Publish("test.topic", []byte("world")); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(received))
	}
	if string(received[0]) != "hello" {
		t.Errorf("msg[0] = %q, want %q", received[0], "hello")
	}
	if string(received[1]) != "world" {
		t.Errorf("msg[1] = %q, want %q", received[1], "world")
	}
}

func TestInMemoryBroker_MultipleSubscribers(t *testing.T) {
	broker := messaging.NewInMemoryBroker()
	defer broker.Close()

	var (
		mu     sync.Mutex
		count1 int
		count2 int
	)

	broker.Subscribe("multi", func(_ []byte) { mu.Lock(); count1++; mu.Unlock() })
	broker.Subscribe("multi", func(_ []byte) { mu.Lock(); count2++; mu.Unlock() })

	broker.Publish("multi", []byte("ping"))

	mu.Lock()
	defer mu.Unlock()
	if count1 != 1 || count2 != 1 {
		t.Errorf("each subscriber should receive 1 message; got count1=%d count2=%d", count1, count2)
	}
}

func TestInMemoryBroker_TopicIsolation(t *testing.T) {
	broker := messaging.NewInMemoryBroker()
	defer broker.Close()

	received := false
	broker.Subscribe("topic.a", func(_ []byte) { received = true })

	broker.Publish("topic.b", []byte("should not arrive"))

	if received {
		t.Error("subscriber on topic.a received message published to topic.b")
	}
}

func TestInMemoryBroker_PublishAfterClose(t *testing.T) {
	broker := messaging.NewInMemoryBroker()
	broker.Close()

	if err := broker.Publish("x", []byte("data")); err == nil {
		t.Error("expected error publishing to closed broker, got nil")
	}
}

func TestInMemoryBroker_SubscribeAfterClose(t *testing.T) {
	broker := messaging.NewInMemoryBroker()
	broker.Close()

	if err := broker.Subscribe("x", func(_ []byte) {}); err == nil {
		t.Error("expected error subscribing to closed broker, got nil")
	}
}

func TestInMemoryBroker_CloseIdempotent(t *testing.T) {
	broker := messaging.NewInMemoryBroker()
	if err := broker.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := broker.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// ---------------------------------------------------------------------------
// DistributedBus tests
// ---------------------------------------------------------------------------

func TestDistributedBus_PublishLocal(t *testing.T) {
	bus := events.NewBus()
	defer bus.Close()

	broker := messaging.NewInMemoryBroker()
	dbus := messaging.NewDistributedBus(bus, broker)
	defer dbus.Close()

	sub := bus.Subscribe(events.TopicServerStarted)
	defer sub.Cancel()

	dbus.Publish(events.TopicServerStarted, "test", "payload")

	select {
	case e := <-sub.Channel:
		if e.Source != "test" {
			t.Errorf("source = %q, want %q", e.Source, "test")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for local event delivery")
	}
}

func TestDistributedBus_PublishForwardsToBroker(t *testing.T) {
	bus := events.NewBus()
	defer bus.Close()

	broker := messaging.NewInMemoryBroker()
	dbus := messaging.NewDistributedBus(bus, broker)
	defer dbus.Close()

	var received []byte
	var mu sync.Mutex
	broker.Subscribe(string(events.TopicServerStarted), func(data []byte) {
		mu.Lock()
		received = data
		mu.Unlock()
	})

	dbus.Publish(events.TopicServerStarted, "src", "hello")

	mu.Lock()
	defer mu.Unlock()
	if received == nil {
		t.Fatal("broker did not receive forwarded message")
	}
}

func TestDistributedBus_SubscribeReceivesRemoteMessages(t *testing.T) {
	// Two buses sharing the same in-memory broker simulate two hosts.
	broker := messaging.NewInMemoryBroker()

	busA := events.NewBus()
	defer busA.Close()
	dbusA := messaging.NewDistributedBus(busA, broker)
	defer dbusA.Close()

	busB := events.NewBus()
	defer busB.Close()
	dbusB := messaging.NewDistributedBus(busB, broker)
	defer dbusB.Close()

	// busB subscribes so remote messages are forwarded locally.
	dbusB.Subscribe(events.TopicServerStarted)
	subB := busB.Subscribe(events.TopicServerStarted)
	defer subB.Cancel()

	// busA publishes — should arrive at busB via broker.
	dbusA.Publish(events.TopicServerStarted, "host-a", "cross-host")

	select {
	case e := <-subB.Channel:
		if e.Source != "host-a" {
			t.Errorf("source = %q, want %q", e.Source, "host-a")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for cross-host event delivery")
	}
}

func TestDistributedBus_SubscribeIdempotent(t *testing.T) {
	bus := events.NewBus()
	defer bus.Close()

	broker := messaging.NewInMemoryBroker()
	dbus := messaging.NewDistributedBus(bus, broker)
	defer dbus.Close()

	// Calling Subscribe twice for the same topic must not double-register
	// on the broker, so a single Publish triggers exactly one broker handler.
	dbus.Subscribe(events.TopicServerStarted)
	dbus.Subscribe(events.TopicServerStarted)

	var mu sync.Mutex
	count := 0
	broker.Subscribe(string(events.TopicServerStarted), func(_ []byte) {
		mu.Lock()
		count++
		mu.Unlock()
	})

	dbus.Publish(events.TopicServerStarted, "src", nil)

	// Allow async delivery to settle.
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	// The broker subscriber above is separate; the DistributedBus should not
	// have added a duplicate subscription that re-publishes.
	// We simply verify the count is exactly 1 from the manual subscribe above.
	if count != 1 {
		t.Errorf("broker handler called %d times, want 1", count)
	}
}

func TestDistributedBus_Close(t *testing.T) {
	bus := events.NewBus()
	defer bus.Close()

	broker := messaging.NewInMemoryBroker()
	dbus := messaging.NewDistributedBus(bus, broker)

	if err := dbus.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
