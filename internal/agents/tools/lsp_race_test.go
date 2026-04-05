package tools_test

import (
	"sync"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/agents"
)

func TestLSPRegistry_ConcurrentAccess(t *testing.T) {
	registry := agents.NewToolRegistry()

	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = registry.List()
		}()
	}

	wg.Wait()
}
