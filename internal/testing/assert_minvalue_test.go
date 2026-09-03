package testing

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/knowledge"
)

// min_count answers "how many elements are in this collection". min_value
// answers "how large is this number". Conflating them is what left one
// challenge unable to state its assertion at all.
//
// challenges/banks/rag/ingestion.yaml wanted "at least one chunk was
// produced". The handler's IngestResult reports Chunks as an int, so the
// response is `{"chunks": 3}` and not `{"chunks": [...]}` -- a perfectly
// reasonable API that min_count rejects with "field body.chunks is not
// countable", because an integer has no length. The vocabulary had no other
// way to say it, so the challenge failed on a shape mismatch that was nobody's
// defect.
func sampleWithBody(body string) []httpSample {
	return []httpSample{{Status: http.StatusOK, Headers: http.Header{}, Body: body}}
}

func TestMinValue(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		field   string
		want    any
		wantErr string // substring; empty means the assertion must pass
	}{
		{
			name:  "integer at the boundary passes",
			body:  `{"chunks": 1}`,
			field: "body.chunks", want: 1,
		},
		{
			name:  "integer above the minimum passes",
			body:  `{"chunks": 7}`,
			field: "body.chunks", want: 1,
		},
		{
			name:  "the real ingest shape, which min_count could not express",
			body:  `{"document_id":"doc-1","chunks":3,"collection":"test-collection"}`,
			field: "body.chunks", want: 1,
		},
		{
			name:  "integer below the minimum fails, and says both numbers",
			body:  `{"chunks": 0}`,
			field: "body.chunks", want: 1,
			wantErr: "want >= 1",
		},
		{
			name:  "a collection is refused, and the message names the right assertion",
			body:  `{"chunks": [1,2,3]}`,
			field: "body.chunks", want: 1,
			wantErr: "use min_count",
		},
		{
			name:  "a non-numeric scalar is refused rather than coerced",
			body:  `{"chunks": "three"}`,
			field: "body.chunks", want: 1,
			wantErr: "is not a number",
		},
		{
			name:  "an absent field is absent, not zero",
			body:  `{"document_id":"doc-1"}`,
			field: "body.chunks", want: 1,
			wantErr: "is absent",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := evalAssertion(
				Assertion{Type: "min_value", Field: tc.field, Expected: tc.want},
				sampleWithBody(tc.body),
			)
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("assertion should have passed, got: %v", err)
			case tc.wantErr != "" && err == nil:
				t.Fatalf("assertion should have failed with %q, but passed", tc.wantErr)
			case tc.wantErr != "" && !strings.Contains(err.Error(), tc.wantErr):
				t.Fatalf("error should mention %q, got: %v", tc.wantErr, err)
			}
		})
	}
}

// The negative control for the whole exercise: min_count must still refuse an
// integer. If a later change made it accept one, min_value would be redundant
// and this file would be passing for the wrong reason.
func TestMinCountStillRefusesAnInteger(t *testing.T) {
	err := evalAssertion(
		Assertion{Type: "min_count", Field: "body.chunks", Expected: 1},
		sampleWithBody(`{"chunks": 3}`),
	)
	if err == nil {
		t.Fatal("min_count accepted an integer; if that is now intended, min_value is redundant and this test should be reconsidered rather than deleted")
	}
	if !strings.Contains(err.Error(), "not countable") {
		t.Fatalf("expected the not-countable refusal, got: %v", err)
	}
}

// The shape this assertion has to satisfy is not a JSON literal I typed; it is
// whatever the production type marshals to. So marshal the production type.
//
// If IngestResult.Chunks ever becomes a slice, min_value will start refusing it
// with "use min_count" and this test will say so -- which is the correct
// failure, because the challenge would then need the other assertion. A
// hand-written fixture would have gone on passing while the real response
// changed underneath it.
func TestMinValueMatchesTheRealIngestResult(t *testing.T) {
	result := knowledge.IngestResult{
		DocumentID: "doc-1",
		Chunks:     3,
		Collection: "test-collection",
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal IngestResult: %v", err)
	}

	// Exactly the assertion challenges/banks/rag/ingestion.yaml now declares.
	if err := evalAssertion(
		Assertion{Type: "min_value", Field: "body.chunks", Expected: 1},
		sampleWithBody(string(encoded)),
	); err != nil {
		t.Fatalf("the shipped challenge's assertion fails against the real response type: %v\nbody: %s", err, encoded)
	}

	// And the assertion it replaced must still be wrong for this shape, or the
	// change was unnecessary.
	if err := evalAssertion(
		Assertion{Type: "min_count", Field: "body.chunks", Expected: 1},
		sampleWithBody(string(encoded)),
	); err == nil {
		t.Fatal("min_count now accepts the real IngestResult; the challenge change would be unnecessary")
	}
}
