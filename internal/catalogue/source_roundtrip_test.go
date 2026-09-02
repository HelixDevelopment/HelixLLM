package catalogue_test

import (
	"testing"
	"testing/fstest"

	"github.com/HelixDevelopment/HelixLLM/internal/catalogue"
	"github.com/stretchr/testify/require"
)

// A field with a yaml tag is not a field that survives loading. Source was added
// to Entry and given a tag, but the decoder's entry() mapping dropped it — so
// every loaded entry had an empty Source, and the FR-012/SC-011 source allowlist
// was silently disarmed: the gate still ran, but nothing supplied the value it
// checks. A gate that always sees "" is not a gate.
//
// This asserts the ROUND TRIP, not the struct field. The struct field test
// passed the whole time.
func TestSourceSurvivesLoading(t *testing.T) {
	const doc = `
streaming_roster: []
entries:
  - model_id: qwen2.5-coder-7b-instruct
    variant: q4_k_m
    family: text
    architecture: dense
    descriptor:
      parameter_count: 7000000000
      quantisation: q4_k_m
    memory_required_bytes: 6442450944
    storage_required_bytes: 5046586572
    requires_accelerator: false
    source: https://huggingface.co/Qwen/Qwen2.5-Coder-7B-Instruct-GGUF
    usage_terms:
      license_id: Apache-2.0
      permitted: [commercial, personal, research, evaluation]
    runtime: in-memory
    expected_capability:
      context_tokens: 32768
`
	fsys := fstest.MapFS{"data/text.yaml": &fstest.MapFile{Data: []byte(doc)}}

	cat, err := catalogue.LoadFS(fsys, "data")
	require.NoError(t, err)
	entries := cat.Entries()
	require.Len(t, entries, 1)

	require.Equal(t,
		"https://huggingface.co/Qwen/Qwen2.5-Coder-7B-Instruct-GGUF",
		entries[0].Source,
		"a source recorded in the data file must survive loading — otherwise the "+
			"allowlist gate checks an empty string on every entry and admits everything")
}
