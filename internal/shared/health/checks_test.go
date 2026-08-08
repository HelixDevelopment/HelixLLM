package health_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/shared/health"
)

func TestHTTPGetCheck_2xxIsHealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"scores":{"openrouter":{"total":90}}}`))
	}))
	defer srv.Close()

	if err := health.HTTPGetCheck(srv.Client(), srv.URL)(context.Background()); err != nil {
		t.Fatalf("200 response should be healthy, got %v", err)
	}
}

func TestHTTPGetCheck_ReallyIssuesTheRequest(t *testing.T) {
	// Guards against a future "optimisation" that returns nil without touching
	// the network — the check must do REAL work.
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	check := health.HTTPGetCheck(srv.Client(), srv.URL)
	for i := 0; i < 3; i++ {
		if err := check(context.Background()); err != nil {
			t.Fatalf("probe %d: %v", i, err)
		}
	}
	if hits != 3 {
		t.Errorf("server saw %d requests, want 3 — the check is not actually probing", hits)
	}
}

func TestHTTPGetCheck_Non2xxIsUnhealthy(t *testing.T) {
	// A dependency that answers 500 to every request is DOWN. "It responded"
	// is not health.
	for _, status := range []int{http.StatusInternalServerError, http.StatusNotFound, http.StatusBadGateway} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte("backend exploded"))
		}))

		err := health.HTTPGetCheck(srv.Client(), srv.URL)(context.Background())
		srv.Close()

		if err == nil {
			t.Errorf("status %d should be unhealthy", status)
			continue
		}
		if !strings.Contains(err.Error(), "backend exploded") {
			t.Errorf("status %d: message should carry the response body for diagnosis, got %q", status, err)
		}
	}
}

func TestHTTPGetCheck_UnreachableIsUnhealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	client := srv.Client()
	srv.Close() // nothing is listening any more

	if err := health.HTTPGetCheck(client, url)(context.Background()); err == nil {
		t.Fatal("an unreachable dependency MUST be unhealthy")
	}
}

func TestHTTPGetCheck_HonoursContextDeadline(t *testing.T) {
	// A hung dependency must not hang the health endpoint: the caller's
	// deadline has to cancel the in-flight request, not merely be ignored.
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-release
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	defer close(release)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := health.HTTPGetCheck(srv.Client(), srv.URL)(ctx)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("a request cancelled by its deadline MUST NOT report healthy")
	}
	if elapsed > 2*time.Second {
		t.Errorf("check took %s — the context deadline was not honoured", elapsed)
	}
}

func TestHTTPGetCheck_BadURLIsUnhealthy(t *testing.T) {
	if err := health.HTTPGetCheck(http.DefaultClient, "://not a url")(context.Background()); err == nil {
		t.Fatal("an unbuildable request MUST be unhealthy, not silently healthy")
	}
}
