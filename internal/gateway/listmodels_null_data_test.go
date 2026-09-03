package gateway_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/internal/gateway"
)

// A listing that says `"data": null` is not a listing.
//
// HandleListModels states the rule itself, in the branch taken when no brain is
// configured at all: `"data": []` is a listing that states "none", while
// `"data": null` reads as a malformed body — and the reason must travel WITH
// the empty list, because an unexplained empty list is indistinguishable from a
// broken server.
//
// The neighbouring branch violates both halves of that rule. When a brain IS
// configured but serves nothing, Data comes from modelsFromOptions, which
// declares `var models []api.Model` and only ever appends — so an empty result
// is a nil slice, marshalled as `null`, and no Reason is set either. The client
// gets the worst of both: a body that reads as malformed, with nothing saying
// why it is empty.
//
// This is the configuration a user actually hits: a running gateway whose
// backend is not serving anything yet.
func TestListModels_ConfiguredBrainWithNothingServedIsAnEmptyList(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// A real brain with no provider configured: non-nil, serving nothing.
	b := brain.New(brain.Config{})

	r := gin.New()
	r.GET("/v1/models", gateway.HandleListModels(b))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/models", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()

	// Assert on the wire bytes, not on a decoded value: `null` and `[]` both
	// decode to a nil map entry in some shapes, and it is the bytes the client
	// parses.
	if strings.Contains(body, `"data":null`) {
		t.Errorf("the listing says \"data\": null, which reads as a malformed body "+
			"rather than a listing stating that nothing is served.\nbody: %s", body)
	}

	var decoded struct {
		Object string             `json:"object"`
		Data   *[]json.RawMessage `json:"data"`
		Reason string             `json:"reason"`
	}
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("response is not valid JSON: %v\nbody: %s", err, body)
	}
	if decoded.Data == nil {
		t.Errorf("data is absent or null; it must be present and empty.\nbody: %s", body)
	} else if len(*decoded.Data) != 0 {
		t.Fatalf("this brain serves nothing, so the list must be empty, got %d entries", len(*decoded.Data))
	}
	if strings.TrimSpace(decoded.Reason) == "" {
		t.Errorf("an empty listing carries no reason, so the caller cannot tell "+
			"\"nothing is served here\" from \"the request went wrong\" — which is "+
			"the exact distinction the Reason field exists to carry.\nbody: %s", body)
	}
}
