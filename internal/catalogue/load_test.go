package catalogue_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/HelixDevelopment/HelixLLM/internal/catalogue"
	"github.com/stretchr/testify/require"
)

const validDataDir = "testdata/catalogue_valid"

// TestLoadReadsEveryRecordedFieldFromData pins the whole wire mapping in one
// assertion. A field that silently fails to decode is a field the rest of the
// system then reasons about as a zero value — a model offered with no accelerator
// requirement, or with no storage figure, because a key was spelled wrong.
func TestLoadReadsEveryRecordedFieldFromData(t *testing.T) {
	loaded, err := catalogue.Load(validDataDir)
	require.NoError(t, err)

	byIdentity := map[string]catalogue.Entry{}
	for _, e := range loaded.Entries() {
		byIdentity[e.Identity()] = e
	}
	require.Len(t, byIdentity, 4, "every entry in every file must be loaded")

	got, ok := byIdentity["deepseek-r1-671b:q4_k_m"]
	require.True(t, ok)
	require.Equal(t, catalogue.Entry{
		ModelID:      "deepseek-r1-671b",
		Variant:      "q4_k_m",
		Family:       catalogue.FamilyText,
		Architecture: catalogue.ArchitectureMixtureOfExperts,
		Descriptor: catalogue.Descriptor{
			ParameterCount:   671_000_000_000,
			ActiveParameters: 37_000_000_000,
			Quantisation:     "q4_k_m",
			Specialisations:  []string{"reasoning"},
		},
		MemoryRequiredBytes:  21_474_836_480,
		StorageRequiredBytes: 399_431_958_528,
		RequiresAccelerator:  true,
		UsageTerms: catalogue.UsageTerms{
			LicenseID: "MIT",
			Permitted: []catalogue.UsagePurpose{
				catalogue.UsageCommercial,
				catalogue.UsagePersonal,
				catalogue.UsageResearch,
				catalogue.UsageEvaluation,
			},
		},
		Runtime: catalogue.RuntimeStreaming,
		StreamingRoster: catalogue.RosterMembership{
			FamilyName: "deepseek-r1",
			Listed:     true,
		},
		ExpectedCapability: catalogue.ExpectedCapability{
			ContextTokens:             65536,
			ThroughputTokensPerSecond: 3.4,
			Modalities:                []string{"text"},
		},
		Integrity: catalogue.IntegrityExpectation{
			Algorithm: catalogue.DigestSHA256,
			Digest:    "0a5e93c186df427b0c8e5719ad3f6420b8c1de7395024fa6b8e07d1c3925fa64",
			SizeBytes: 399_431_958_528,
		},
		Notes: []string{"UNVERIFIED: throughput figure is a vendor claim, not a host measurement"},
	}, got)

	// Restrictions decode with their exclusions intact, so the usage-terms
	// filter can still name the term that withholds an entry (FR-055).
	tts, ok := byIdentity["xtts-v2"]
	require.True(t, ok)
	require.False(t, tts.UsageTerms.Permits(catalogue.UsageCommercial))
	restriction, restricted := tts.UsageTerms.RestrictionFor(catalogue.UsageCommercial)
	require.True(t, restricted)
	require.Equal(t, catalogue.TermNonCommercial, restriction.Term,
		"the NON-COMMERCIAL term withholds it, not the attribution term listed before it")
}

// TestStreamingEligibilityComesFromRosterDataNotArchitecture is the load-bearing
// test of the loader.
//
// Both entries below are mixture-of-experts and identical in every field the
// loader could plausibly consult except the family name each declares. One name
// is in the roster DATA and one is not, and that must be the only thing that
// decides eligibility (D1).
func TestStreamingEligibilityComesFromRosterDataNotArchitecture(t *testing.T) {
	loaded, err := catalogue.Load(validDataDir)
	require.NoError(t, err)

	var member, unrostered catalogue.Entry
	for _, e := range loaded.Entries() {
		switch e.ModelID {
		case "deepseek-r1-671b":
			member = e
		case "qwen3-30b-a3b":
			unrostered = e
		}
	}

	require.Equal(t, member.Architecture, unrostered.Architecture,
		"fixture precondition: both are the same architecture")
	require.True(t, member.StreamingEligible())
	require.False(t, unrostered.StreamingEligible(),
		"an architecturally-identical model absent from the roster must NOT be eligible")

	// The miss is a recorded lookup, not an absence of one: the family name that
	// was looked up survives, so a reader can see what was asked.
	require.Equal(t, "qwen3-moe", unrostered.StreamingRoster.FamilyName)
	require.False(t, loaded.Roster().Admits("qwen3-moe"))
	require.True(t, loaded.Roster().Admits("deepseek-r1"))
}

// TestAddingARosterFamilyIsADataChangeNotACodeChange states the requirement
// directly: the same entry bytes flip from ineligible to eligible when — and
// only when — the roster data gains the family. No Go source changes between the
// two halves of this test.
func TestAddingARosterFamilyIsADataChangeNotACodeChange(t *testing.T) {
	const entryDoc = `
entries:
  - model_id: some-moe-model
    family: text
    architecture: mixture-of-experts
    memory_required_bytes: 1024
    storage_required_bytes: 4096
    usage_terms:
      license_id: Apache-2.0
      permitted: [commercial]
    runtime: in-memory
    streaming_family: newly-supported-family
    integrity:
      algorithm: sha256
      digest: 0000000000000000000000000000000000000000000000000000000000000001
      size_bytes: 4096
`
	before := fstest.MapFS{
		"entries.yaml": {Data: []byte(entryDoc)},
		"roster.yaml":  {Data: []byte("streaming_roster: [deepseek-r1]\n")},
	}
	loaded, err := catalogue.LoadFS(before, ".")
	require.NoError(t, err)
	require.False(t, loaded.Entries()[0].StreamingEligible())

	// The ONLY difference: one line of data.
	after := fstest.MapFS{
		"entries.yaml": {Data: []byte(entryDoc)},
		"roster.yaml":  {Data: []byte("streaming_roster: [deepseek-r1, newly-supported-family]\n")},
	}
	loaded, err = catalogue.LoadFS(after, ".")
	require.NoError(t, err)
	require.True(t, loaded.Entries()[0].StreamingEligible(),
		"adding a supported family must be a data change, never a code change")
}

// TestLoadFailsLoudlyRatherThanSkippingADefectiveEntry is the anti-bluff test of
// the loader. A silently dropped model is a model the user is never offered,
// with no explanation anywhere — indistinguishable from a model that does not
// exist. Every defect below must surface as an error naming the file and the
// offender, and must load NOTHING.
func TestLoadFailsLoudlyRatherThanSkippingADefectiveEntry(t *testing.T) {
	const goodEntry = `
  - model_id: healthy-model
    family: text
    architecture: dense
    memory_required_bytes: 1024
    storage_required_bytes: 4096
    usage_terms:
      license_id: Apache-2.0
      permitted: [commercial]
    runtime: in-memory
    integrity:
      algorithm: sha256
      digest: 0000000000000000000000000000000000000000000000000000000000000001
      size_bytes: 4096
`
	cases := []struct {
		name    string
		doc     string
		wantErr error
		names   []string
	}{
		{
			name: "misspelled key is not silently ignored",
			doc: "entries:\n" + goodEntry + `
  - model_id: typo-model
    family: text
    architecture: dense
    memory_required_bytes: 1024
    storage_requried_bytes: 4096
    usage_terms:
      license_id: Apache-2.0
      permitted: [commercial]
    runtime: in-memory
    integrity:
      algorithm: sha256
      digest: 0000000000000000000000000000000000000000000000000000000000000001
      size_bytes: 4096
`,
			wantErr: catalogue.ErrUnknownField,
			names:   []string{"defective.yaml", "storage_requried_bytes"},
		},
		{
			name: "unrecognised capability family",
			doc: "entries:\n" + goodEntry + `
  - model_id: bad-family-model
    family: txet
    architecture: dense
    memory_required_bytes: 1024
    storage_required_bytes: 4096
    usage_terms:
      license_id: Apache-2.0
      permitted: [commercial]
    runtime: in-memory
    integrity:
      algorithm: sha256
      digest: 0000000000000000000000000000000000000000000000000000000000000001
      size_bytes: 4096
`,
			wantErr: catalogue.ErrUnknownFamily,
			names:   []string{"defective.yaml", "txet"},
		},
		{
			name: "unrecognised usage purpose would silently narrow what the model permits",
			doc: "entries:\n" + goodEntry + `
  - model_id: bad-purpose-model
    family: text
    architecture: dense
    memory_required_bytes: 1024
    storage_required_bytes: 4096
    usage_terms:
      license_id: Apache-2.0
      permitted: [commercail]
    runtime: in-memory
    integrity:
      algorithm: sha256
      digest: 0000000000000000000000000000000000000000000000000000000000000001
      size_bytes: 4096
`,
			wantErr: catalogue.ErrUnknownUsagePurpose,
			names:   []string{"defective.yaml", "commercail"},
		},
		{
			name: "entry that fails its own validation",
			doc: "entries:\n" + goodEntry + `
  - model_id: no-storage-model
    family: text
    architecture: dense
    memory_required_bytes: 1024
    usage_terms:
      license_id: Apache-2.0
      permitted: [commercial]
    runtime: in-memory
    integrity:
      algorithm: sha256
      digest: 0000000000000000000000000000000000000000000000000000000000000001
      size_bytes: 4096
`,
			wantErr: catalogue.ErrNoStorageRequirement,
			names:   []string{"defective.yaml", "no-storage-model"},
		},
		{
			name: "streaming entry whose family is absent from every roster",
			doc: "entries:\n" + goodEntry + `
  - model_id: unrostered-streaming-model
    family: text
    architecture: mixture-of-experts
    memory_required_bytes: 1024
    storage_required_bytes: 4096
    usage_terms:
      license_id: Apache-2.0
      permitted: [commercial]
    runtime: streaming
    streaming_family: not-on-any-roster
    integrity:
      algorithm: sha256
      digest: 0000000000000000000000000000000000000000000000000000000000000001
      size_bytes: 4096
`,
			wantErr: catalogue.ErrStreamingNotRostered,
			names:   []string{"defective.yaml", "unrostered-streaming-model"},
		},
		{
			name:    "duplicate identity would silently shadow one of the two models",
			doc:     "entries:\n" + goodEntry + goodEntry,
			wantErr: catalogue.ErrDuplicateEntry,
			names:   []string{"defective.yaml", "healthy-model"},
		},
		{
			name:    "unparseable document",
			doc:     "entries: [ this is not: valid yaml\n",
			wantErr: catalogue.ErrMalformedDocument,
			names:   []string{"defective.yaml"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			loaded, err := catalogue.LoadFS(fstest.MapFS{
				"defective.yaml": {Data: []byte(tc.doc)},
			}, ".")
			require.Error(t, err, "a defective entry must never load silently")
			require.True(t, errors.Is(err, tc.wantErr),
				"want %v, got %v", tc.wantErr, err)
			for _, name := range tc.names {
				require.Contains(t, err.Error(), name,
					"the error must name the file and the offender so a sweep can find it")
			}
			require.Empty(t, loaded.Entries(),
				"a defective file must load nothing, not a partial catalogue")
		})
	}
}

// TestLoadRefusesAnEmptyDataSet — an empty catalogue is indistinguishable from a
// catalogue whose files were never deployed, and the second is a deployment
// defect that would otherwise surface as "no models available for any family".
func TestLoadRefusesAnEmptyDataSet(t *testing.T) {
	_, err := catalogue.LoadFS(fstest.MapFS{"README.md": {Data: []byte("not data")}}, ".")
	require.ErrorIs(t, err, catalogue.ErrNoDataFiles)

	_, err = catalogue.LoadFS(fstest.MapFS{"empty.yaml": {Data: []byte("entries: []\n")}}, ".")
	require.ErrorIs(t, err, catalogue.ErrNoEntries)
}

// TestLoadIsOrderIndependentAcrossFiles — the four data files are written
// independently, so a roster declared in one must reach an entry in another
// regardless of which file the walk happens to read first.
func TestLoadIsOrderIndependentAcrossFiles(t *testing.T) {
	entry := []byte(`
entries:
  - model_id: streamed-model
    family: text
    architecture: mixture-of-experts
    memory_required_bytes: 1024
    storage_required_bytes: 4096
    usage_terms:
      license_id: MIT
      permitted: [commercial]
    runtime: streaming
    streaming_family: late-declared-family
    integrity:
      algorithm: sha256
      digest: 0000000000000000000000000000000000000000000000000000000000000001
      size_bytes: 4096
`)
	roster := []byte("streaming_roster: [late-declared-family]\n")

	// "aaa" sorts before "zzz": the entry is read before the roster that admits it.
	loaded, err := catalogue.LoadFS(fstest.MapFS{
		"aaa_entry.yaml":  {Data: entry},
		"zzz_roster.yaml": {Data: roster},
	}, ".")
	require.NoError(t, err)
	require.True(t, loaded.Entries()[0].StreamingEligible())
}

// videoDoc is a video-generation entry recorded the way a video model's
// capability actually reads: a frame size, a frame count and a rate.
const videoDoc = `
entries:
  - model_id: wan-2-2-ti2v-5b
    family: video-generation
    architecture: diffusion
    memory_required_bytes: 11166914969
    storage_required_bytes: 10737418240
    requires_accelerator: true
    usage_terms:
      license_id: Apache-2.0
      permitted: [commercial, personal, research, evaluation]
    runtime: in-memory
    expected_capability:
      modalities: [text, image]
      resolution: 832x480
      num_frames: 49
      fps: 16
      duration_seconds: 3.06
    integrity:
      algorithm: sha256
      digest: 0000000000000000000000000000000000000000000000000000000000000002
      size_bytes: 10737418240
    annotations:
      max_supported_resolution: 1280x704
      frame_count_rule: 4n+1
    notes:
      - "UNVERIFIED: 720p peak measured on one host only"
`

// TestLoadRecordsVideoOutputInVideoTerms — a video entry must not be forced
// through a tokens-per-second field that means nothing for it (FR-005).
func TestLoadRecordsVideoOutputInVideoTerms(t *testing.T) {
	loaded, err := catalogue.LoadFS(fstest.MapFS{"video.yaml": {Data: []byte(videoDoc)}}, ".")
	require.NoError(t, err)

	entry := loaded.Entries()[0]
	require.Equal(t, catalogue.FamilyVideoGeneration, entry.Family)
	require.Equal(t, catalogue.VideoOutput{
		FrameWidth:      832,
		FrameHeight:     480,
		FrameCount:      49,
		FramesPerSecond: 16,
	}, entry.ExpectedCapability.Video)
	require.Zero(t, entry.ExpectedCapability.ThroughputTokensPerSecond,
		"a video entry states no token throughput, and must not be given a fabricated one")
}

// TestLoadRefusesAContradictoryOrPartialVideoOutput. A recorded duration is
// redundant with frames and rate, so the loader treats it as a check on them
// rather than a second place for the truth to live.
func TestLoadRefusesAContradictoryOrPartialVideoOutput(t *testing.T) {
	cases := map[string]struct {
		doc     string
		wantErr error
	}{
		"duration disagrees with frames and rate": {
			doc:     strings.Replace(videoDoc, "duration_seconds: 3.06", "duration_seconds: 5.0", 1),
			wantErr: catalogue.ErrContradictoryVideoOutput,
		},
		"resolution recorded without a rate": {
			doc:     strings.Replace(videoDoc, "      fps: 16\n", "", 1),
			wantErr: catalogue.ErrIncompleteVideoOutput,
		},
		"unparseable resolution": {
			doc:     strings.Replace(videoDoc, "resolution: 832x480", "resolution: 832 by 480", 1),
			wantErr: catalogue.ErrMalformedResolution,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			loaded, err := catalogue.LoadFS(fstest.MapFS{"video.yaml": {Data: []byte(tc.doc)}}, ".")
			require.ErrorIs(t, err, tc.wantErr)
			require.Contains(t, err.Error(), "wan-2-2-ti2v-5b")
			require.Empty(t, loaded.Entries())
		})
	}
}

// TestLoadAcceptsEveryRecordedFamily — every family constant must be reachable
// from data. A family the loader cannot parse is a capability no user can be
// offered, however complete the rest of the pipeline is.
func TestLoadAcceptsEveryRecordedFamily(t *testing.T) {
	for _, family := range allFamilies {
		t.Run(string(family), func(t *testing.T) {
			doc := `
entries:
  - model_id: probe-model
    family: ` + string(family) + `
    architecture: dense
    memory_required_bytes: 1024
    storage_required_bytes: 4096
    usage_terms:
      license_id: Apache-2.0
      permitted: [commercial]
    runtime: in-memory
    integrity:
      algorithm: sha256
      digest: 0000000000000000000000000000000000000000000000000000000000000001
      size_bytes: 4096
`
			loaded, err := catalogue.LoadFS(fstest.MapFS{"probe.yaml": {Data: []byte(doc)}}, ".")
			require.NoError(t, err)
			require.Equal(t, family, loaded.Entries()[0].Family)
		})
	}
}

// TestLoadCarriesProvenanceMarkersForwardWithoutWeakeningTypoDetection.
// Research left `UNVERIFIED:` markers that must survive into the catalogue
// rather than being resolved silently (T015). They live in named fields, so
// carrying them forward never requires accepting arbitrary keys — the mechanism
// that catches a misspelled `storage_required_bytes` stays in force.
func TestLoadCarriesProvenanceMarkersForwardWithoutWeakeningTypoDetection(t *testing.T) {
	loaded, err := catalogue.LoadFS(fstest.MapFS{"video.yaml": {Data: []byte(videoDoc)}}, ".")
	require.NoError(t, err)

	entry := loaded.Entries()[0]
	require.Equal(t, []string{"UNVERIFIED: 720p peak measured on one host only"}, entry.Notes)
	require.Equal(t, "4n+1", entry.Annotations["frame_count_rule"])
}

// TestUnknownFieldReportIsBounded — a file written against a different shape can
// produce hundreds of unknown-field messages, and a single joined line of them
// is not a diagnosis, it is a wall a reader gives up on. The report names the
// first few, with their line numbers, and states how many more there are.
func TestUnknownFieldReportIsBounded(t *testing.T) {
	var doc strings.Builder
	doc.WriteString("entries:\n")
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&doc, `  - model_id: model-%d
    family: text
    architecture: dense
    memory_required: 1024
    storage_required: 4096
    usage_terms:
      license_id: Apache-2.0
      permitted: [commercial]
    runtime: in-memory
    integrity:
      algorithm: sha256
      digest: 0000000000000000000000000000000000000000000000000000000000000001
      size_bytes: 4096
`, i)
	}

	_, err := catalogue.LoadFS(fstest.MapFS{"divergent.yaml": {Data: []byte(doc.String())}}, ".")
	require.ErrorIs(t, err, catalogue.ErrUnknownField)
	require.Contains(t, err.Error(), "divergent.yaml")
	require.Contains(t, err.Error(), "memory_required", "the first offending fields must still be named")
	require.Contains(t, err.Error(), "more", "the remainder must be counted, not silently dropped")
	require.Less(t, len(err.Error()), 1200, "the report must stay readable")
}
