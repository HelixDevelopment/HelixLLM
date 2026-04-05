package messaging_test

import (
	"sync"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/shared/messaging"
)

func TestInMemoryBroker_ConcurrentPublishSubscribe(t *testing.T) {
	broker := messaging.NewInMemoryBroker()
	defer broker.Close()

	const goroutines = 50
	const messagesPerGoroutine = 100

	var totalReceived int64
	var mu sync.Mutex

	if err := broker.Subscribe("stress", func(data []byte) {
		mu.Lock()
		totalReceived++
		mu.Unlock()
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < messagesPerGoroutine; i++ {
				_ = broker.Publish("stress", []byte("msg"))
			}
		}()
	}

	wg.Wait()

	mu.Lock()
	got := totalReceived
	mu.Unlock()

	want := int64(goroutines * messagesPerGoroutine)
	if got != want {
		t.Errorf("received %d messages, want %d", got, want)
	}
}

func TestInMemoryBroker_ConcurrentCloseWhilePublishing(t *testing.T) {
	broker := messaging.NewInMemoryBroker()

	if err := broker.Subscribe("topic", func(data []byte) {}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = broker.Publish("topic", []byte("data"))
			}
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = broker.Close()
	}()

	wg.Wait()
}
