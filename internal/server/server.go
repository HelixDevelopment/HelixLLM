// Package server provides an HTTP/3 (QUIC) + HTTP/2 (TLS) Gin-based server
// with request-ID and compression middleware and a built-in health endpoint.
package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/quic-go/quic-go/http3"

	obsgin "digital.vasic.observability/pkg/gin"

	"github.com/HelixDevelopment/HelixLLM/internal/server/middleware"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/health"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/observability"
)

// Options configures the Server.
type Options struct {
	// Host is the bind address (e.g. "0.0.0.0" or "127.0.0.1").
	Host string
	// Port is the TCP/UDP port to listen on. 0 is valid for testing (httptest).
	Port int
	// TLSCert is the path to the PEM-encoded certificate file (required for
	// ListenAndServe; not needed for Handler/Router-only usage).
	TLSCert string
	// TLSKey is the path to the PEM-encoded private-key file.
	TLSKey string
	// Checker is used to serve the /internal/health endpoint.
	Checker *health.Checker
	// Obs is the optional observability instance. When non-nil, Prometheus
	// metrics middleware is applied to all requests and /internal/metrics is
	// registered.
	Obs *observability.Observability
}

// Server wraps a Gin engine with HTTP/3 and HTTP/2 transports.
type Server struct {
	opts   Options
	engine *gin.Engine
}

// New creates and configures a Server with all standard middleware and routes.
func New(opts Options) *Server {
	engine := gin.New()
	engine.Use(gin.Recovery())
	engine.Use(middleware.RequestID())
	engine.Use(middleware.Compression())

	// Alt-Svc middleware — advertises the HTTP/3 endpoint on every response.
	engine.Use(altSvcMiddleware(opts.Host, opts.Port))

	// Observability metrics middleware (optional).
	if opts.Obs != nil {
		engine.Use(obsgin.MetricsMiddleware(opts.Obs.Metrics()))
	}

	s := &Server{opts: opts, engine: engine}
	s.registerRoutes()
	return s
}

// Handler returns the underlying http.Handler. Use this in tests with
// httptest.NewRecorder.
func (s *Server) Handler() http.Handler {
	return s.engine
}

// Router returns the Gin engine so callers can register additional routes.
func (s *Server) Router() *gin.Engine {
	return s.engine
}

// ListenAndServe starts both an HTTP/3 server (UDP) and an HTTP/2 server
// (TCP) on the configured address. Both servers are shut down gracefully when
// ctx is cancelled.
//
// TLSCert and TLSKey must be set; the method returns an error immediately if
// either is missing.
func (s *Server) ListenAndServe(ctx context.Context) error {
	if s.opts.TLSCert == "" || s.opts.TLSKey == "" {
		return fmt.Errorf("server: TLSCert and TLSKey are required for ListenAndServe")
	}

	cert, err := tls.LoadX509KeyPair(s.opts.TLSCert, s.opts.TLSKey)
	if err != nil {
		return fmt.Errorf("server: load TLS key pair: %w", err)
	}

	addr := fmt.Sprintf("%s:%d", s.opts.Host, s.opts.Port)

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
		NextProtos:   []string{"h3", "h2", "http/1.1"},
	}

	// HTTP/3 server (QUIC over UDP).
	h3srv := &http3.Server{
		Addr:      addr,
		TLSConfig: tlsCfg,
		Handler:   s.engine,
		Port:      s.opts.Port,
	}

	// HTTP/2 server (TLS over TCP).
	h2srv := &http.Server{
		Addr:      addr,
		Handler:   s.engine,
		TLSConfig: tlsCfg,
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 2)

	// Start HTTP/3.
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := h3srv.ListenAndServeTLS(s.opts.TLSCert, s.opts.TLSKey); err != nil &&
			err != http.ErrServerClosed {
			errCh <- fmt.Errorf("http3: %w", err)
		}
	}()

	// Start HTTP/2.
	wg.Add(1)
	go func() {
		defer wg.Done()
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			errCh <- fmt.Errorf("http2 listen: %w", err)
			return
		}
		tlsLn := tls.NewListener(ln, tlsCfg)
		if err := h2srv.Serve(tlsLn); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("http2: %w", err)
		}
	}()

	// Wait for context cancellation then shut everything down.
	<-ctx.Done()

	h3srv.Close()
	h2srv.Close()

	wg.Wait()
	close(errCh)

	for e := range errCh {
		return e
	}
	return nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func (s *Server) registerRoutes() {
	s.engine.GET("/internal/health", healthHandler(s.opts.Checker))

	// Expose Prometheus metrics. When an Observability instance is provided,
	// serve its isolated registry; otherwise fall back to the default registry.
	if s.opts.Obs != nil {
		h := promhttp.HandlerFor(s.opts.Obs.Gatherer(), promhttp.HandlerOpts{})
		s.engine.GET("/internal/metrics", gin.WrapH(h))
	} else {
		s.engine.GET("/internal/metrics", gin.WrapH(promhttp.Handler()))
	}
}

func healthHandler(checker *health.Checker) gin.HandlerFunc {
	return func(c *gin.Context) {
		report := checker.Check(c.Request.Context())
		status := http.StatusOK
		if report.Status != health.StatusHealthy {
			status = http.StatusServiceUnavailable
		}
		c.JSON(status, report)
	}
}

// altSvcMiddleware injects an Alt-Svc header on every response to advertise
// the HTTP/3 endpoint. When port is 0 (e.g. in tests), no header is set.
func altSvcMiddleware(host string, port int) gin.HandlerFunc {
	if port == 0 {
		return func(c *gin.Context) { c.Next() }
	}
	value := fmt.Sprintf(`h3=":%d"; ma=86400`, port)
	return func(c *gin.Context) {
		c.Header("Alt-Svc", value)
		c.Next()
	}
}
