//go:build stress

package stress_test

import (
	"crypto/tls"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var baseURL = "https://localhost:8443"

func init() {
	http.DefaultTransport = &http.Transport{
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		MaxIdleConns:        1000,
		MaxIdleConnsPerHost: 1000,
		IdleConnTimeout:     30 * time.Second,
	}
}

func TestStress_ConcurrentChatCompletions(t *testing.T) {
	const concurrency = 100
	const requestsPerWorker = 10

	var success, failure atomic.Int64
	var wg sync.WaitGroup

	body := `{"model":"auto","messages":[{"role":"user","content":"hi"}]}`

	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < requestsPerWorker; i++ {
				resp, err := http.Post(
					baseURL+"/v1/chat/completions",
					"application/json",
					strings.NewReader(body),
				)
				if err != nil {
					failure.Add(1)
					continue
				}
				io.Copy(io.Discard, resp.Body) //nolint:errcheck
				resp.Body.Close()

				if resp.StatusCode == 200 || resp.StatusCode == 429 {
					success.Add(1)
				} else {
					failure.Add(1)
				}
			}
		}()
	}

	wg.Wait()

	total := success.Load() + failure.Load()
	t.Logf("Total: %d, Success: %d, Failure: %d", total, success.Load(), failure.Load())

	if total != int64(concurrency*requestsPerWorker) {
		t.Errorf("expected %d total responses, got %d", concurrency*requestsPerWorker, total)
	}
}

func TestStress_HealthEndpointUnderLoad(t *testing.T) {
	const concurrency = 500
	const duration = 5 * time.Second

	var success, failure atomic.Int64
	var wg sync.WaitGroup

	done := make(chan struct{})
	go func() {
		time.Sleep(duration)
		close(done)
	}()

	for w := 0; w < concurrency; w++ {
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
				if err != nil {
					failure.Add(1)
					continue
				}
				io.Copy(io.Discard, resp.Body) //nolint:errcheck
				resp.Body.Close()
				if resp.StatusCode == 200 {
					success.Add(1)
				} else {
					failure.Add(1)
				}
			}
		}()
	}

	wg.Wait()
	t.Logf("Health check: Success=%d, Failure=%d over %v", success.Load(), failure.Load(), duration)

	if success.Load() == 0 {
		t.Error("zero successful health checks under load")
	}
}
