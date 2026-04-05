//go:build performance

package performance_test

import (
	"crypto/tls"
	"io"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var baseURL = "https://localhost:8443"

func init() {
	http.DefaultTransport = &http.Transport{
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		MaxIdleConns:        200,
		MaxIdleConnsPerHost: 200,
	}
}

func measureLatency(t *testing.T, path string, count int) []time.Duration {
	t.Helper()
	durations := make([]time.Duration, 0, count)
	for i := 0; i < count; i++ {
		start := time.Now()
		resp, err := http.Get(baseURL + path)
		if err != nil {
			continue
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		durations = append(durations, time.Since(start))
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	return durations
}

func percentile(durations []time.Duration, p float64) time.Duration {
	if len(durations) == 0 {
		return 0
	}
	idx := int(float64(len(durations)) * p)
	if idx >= len(durations) {
		idx = len(durations) - 1
	}
	return durations[idx]
}

func TestPerformance_HealthLatency(t *testing.T) {
	d := measureLatency(t, "/internal/health", 100)
	p99 := percentile(d, 0.99)
	t.Logf("Health: p50=%v p95=%v p99=%v (n=%d)", percentile(d, 0.50), percentile(d, 0.95), p99, len(d))
	if p99 > 100*time.Millisecond {
		t.Errorf("health p99 = %v, want < 100ms", p99)
	}
}

func TestPerformance_ModelsLatency(t *testing.T) {
	d := measureLatency(t, "/v1/models", 100)
	p99 := percentile(d, 0.99)
	t.Logf("Models: p50=%v p95=%v p99=%v (n=%d)", percentile(d, 0.50), percentile(d, 0.95), p99, len(d))
	if p99 > 200*time.Millisecond {
		t.Errorf("models p99 = %v, want < 200ms", p99)
	}
}

func TestPerformance_HealthThroughput(t *testing.T) {
	const workers = 50
	const duration = 5 * time.Second
	var count atomic.Int64
	done := make(chan struct{})
	go func() { time.Sleep(duration); close(done) }()

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
				}
				resp, err := http.Get(baseURL + "/internal/health")
				if err == nil {
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
					count.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	rps := float64(count.Load()) / duration.Seconds()
	t.Logf("Health throughput: %.0f req/s", rps)
	if rps < 100 {
		t.Errorf("health throughput = %.0f req/s, want >= 100", rps)
	}
}
