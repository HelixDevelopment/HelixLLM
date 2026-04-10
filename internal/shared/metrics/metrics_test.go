package metrics

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// freshRegistry replaces the default Prometheus registry so that each
// test starts with a clean slate (avoids "duplicate registration" panics
// across parallel test runs in the same process).
func freshRegistry(t *testing.T) {
	t.Helper()
	reg := prometheus.NewRegistry()
	prometheus.DefaultRegisterer = reg
	prometheus.DefaultGatherer = reg
}

func TestRegister(t *testing.T) {
	freshRegistry(t)
	// Must not panic on first call.
	assert.NotPanics(t, func() { Register() })
}

func TestRegister_Idempotent(t *testing.T) {
	freshRegistry(t)
	Register()
	// Second call panics because MustRegister rejects duplicates.
	assert.Panics(t, func() { Register() })
}

func TestTrackInference(t *testing.T) {
	freshRegistry(t)
	Register()

	TrackInference("qwen-1.5b", 200*time.Millisecond, 50)

	val := counterValue(t, ModelInferenceTotal, "qwen-1.5b")
	assert.Equal(t, float64(1), val)

	tokVal := counterValue(t, ModelTokensGenerated, "qwen-1.5b")
	assert.Equal(t, float64(50), tokVal)

	tps := gaugeValue(t, ModelTokensPerSecond, "qwen-1.5b")
	assert.InDelta(t, 250.0, tps, 1.0) // 50 / 0.2s = 250
}

func TestTrackInference_ZeroDuration(t *testing.T) {
	freshRegistry(t)
	Register()

	// Zero duration must not divide by zero.
	assert.NotPanics(t, func() {
		TrackInference("zero-model", 0, 10)
	})
}

func TestTrackToolExecution_Success(t *testing.T) {
	freshRegistry(t)
	Register()

	TrackToolExecution("read_file", 15*time.Millisecond, true)

	val := counterValueVec(t, ToolExecutionTotal, "read_file", "success")
	assert.Equal(t, float64(1), val)
}

func TestTrackToolExecution_Error(t *testing.T) {
	freshRegistry(t)
	Register()

	TrackToolExecution("exec_python", 100*time.Millisecond, false)

	val := counterValueVec(t, ToolExecutionTotal, "exec_python", "error")
	assert.Equal(t, float64(1), val)
}

func TestTrackAPIRequest(t *testing.T) {
	freshRegistry(t)
	Register()

	TrackAPIRequest("/v1/chat/completions", "POST", 200, 50*time.Millisecond)

	val := counterValueVec3(t, APIRequestsTotal, "/v1/chat/completions", "POST", "2xx")
	assert.Equal(t, float64(1), val)
}

func TestStatusBucket(t *testing.T) {
	tests := []struct {
		code int
		want string
	}{
		{100, "1xx"},
		{200, "2xx"},
		{301, "3xx"},
		{404, "4xx"},
		{500, "5xx"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, statusBucket(tt.code))
	}
}

func TestGinMiddleware(t *testing.T) {
	freshRegistry(t)
	Register()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(GinMiddleware())
	r.GET("/v1/models", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Verify the request was tracked.
	val := counterValueVec3(t, APIRequestsTotal, "/v1/models", "GET", "2xx")
	assert.Equal(t, float64(1), val)
}

func TestGinMiddleware_UnknownRoute(t *testing.T) {
	freshRegistry(t)
	Register()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(GinMiddleware())
	// No routes registered — FullPath() returns "".

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/not-registered", nil)
	r.ServeHTTP(w, req)

	// 404 with endpoint="unknown" must not panic.
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// ── helpers ────────────────────────────────────────────────────────────

func counterValue(t *testing.T, cv *prometheus.CounterVec, label string) float64 {
	t.Helper()
	m := &dto.Metric{}
	err := cv.WithLabelValues(label).Write(m)
	require.NoError(t, err)
	return m.GetCounter().GetValue()
}

func counterValueVec(t *testing.T, cv *prometheus.CounterVec, l1, l2 string) float64 {
	t.Helper()
	m := &dto.Metric{}
	err := cv.WithLabelValues(l1, l2).Write(m)
	require.NoError(t, err)
	return m.GetCounter().GetValue()
}

func counterValueVec3(t *testing.T, cv *prometheus.CounterVec, l1, l2, l3 string) float64 {
	t.Helper()
	m := &dto.Metric{}
	err := cv.WithLabelValues(l1, l2, l3).Write(m)
	require.NoError(t, err)
	return m.GetCounter().GetValue()
}

func gaugeValue(t *testing.T, gv *prometheus.GaugeVec, label string) float64 {
	t.Helper()
	m := &dto.Metric{}
	err := gv.WithLabelValues(label).Write(m)
	require.NoError(t, err)
	return m.GetGauge().GetValue()
}
