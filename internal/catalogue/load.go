package catalogue

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// The catalogue data format.
//
// A data file is a YAML document with two optional top-level keys:
//
//	streaming_roster:            # families the streaming runtime declares support for
//	  - deepseek-r1
//	entries:
//	  - model_id: deepseek-r1-671b
//	    variant: q4_k_m          # optional
//	    family: text             # one of the recorded capability families
//	    architecture: mixture-of-experts   # descriptive only; optional
//	    descriptor:
//	      parameter_count: 671000000000
//	      active_parameters: 37000000000  # omit for dense models
//	      quantisation: q4_k_m
//	      specialisations: [reasoning]
//	    memory_required_bytes: 21474836480
//	    storage_required_bytes: 399431958528   # never derived from memory (D2)
//	    requires_accelerator: true
//	    source: https://huggingface.co/…      # where the weights come from (FR-012)
//	    usage_terms:
//	      license_id: MIT
//	      permitted: [commercial, personal, research, evaluation]
//	      restrictions:
//	        - term: non-commercial
//	          excludes: [commercial]           # omit where the term withholds nothing
//	          threshold: {value: 1000000, currency: USD, period: annual}
//	          reference: "licence §2(d)"
//	    runtime: streaming                     # in-memory | streaming
//	    streaming_family: deepseek-r1          # resolved against streaming_roster
//	    expected_capability:
//	      context_tokens: 65536
//	      throughput_tokens_per_second: 3.4
//	      modalities: [text]
//	      resolution: 832x480                  # video entries only
//	      num_frames: 49
//	      fps: 16
//	    integrity:
//	      algorithm: sha256
//	      digest: 0a5e…
//	      size_bytes: 399431958528
//	    notes: ["UNVERIFIED: …"]               # provenance, carried forward verbatim
//	    annotations: {frame_count_rule: 4n+1}  # free-form; never read for a decision
//
// The roster is DATA. Every family name lives in these files and none appears in
// Go source, so a runtime release that adds a supported family is a data change
// and never a code change (research.md D1). Rosters from every file are unioned
// before any entry is resolved, so which file declares a family — and in which
// order the files are read — cannot change the answer.
//
// Unknown keys are an error, not a silence. A misspelled `storage_requried_bytes`
// would otherwise decode as zero, and the entry would be refused for a missing
// storage figure it plainly states, or worse, pass a check it should not. Free-form
// material belongs under `annotations`, which is the one place arbitrary shape is
// accepted.

// Errors reported by Load. As elsewhere in this package they are separate
// because their remedies are separate: a misspelled key is fixed in the data
// file, an entry that fails validation may be a genuine gap in research, and a
// duplicate identity means two files disagree about which model is which.
var (
	ErrNoDataFiles              = errors.New("catalogue: no data files found")
	ErrNoEntries                = errors.New("catalogue: data files declare no entries")
	ErrMalformedDocument        = errors.New("catalogue: data file is not a well-formed catalogue document")
	ErrUnknownField             = errors.New("catalogue: data file uses a field the catalogue does not record")
	ErrUnknownArchitecture      = errors.New("catalogue: entry architecture is not a recorded architecture")
	ErrUnknownUsagePurpose      = errors.New("catalogue: entry names a usage purpose that is not recorded")
	ErrUnknownRestrictionTerm   = errors.New("catalogue: entry names a restriction term that is not recorded")
	ErrUnknownDigestAlgorithm   = errors.New("catalogue: entry names a digest algorithm that is not recorded")
	ErrDuplicateEntry           = errors.New("catalogue: two entries share one identity")
	ErrMalformedResolution      = errors.New("catalogue: resolution is not of the form <width>x<height>")
	ErrIncompleteVideoOutput    = errors.New("catalogue: video output is partly recorded")
	ErrContradictoryVideoOutput = errors.New("catalogue: recorded video duration disagrees with the frame count and rate")
)

// Catalogue is the loaded set of entries together with the streaming runtime's
// declared roster, which the entries were resolved against.
type Catalogue struct {
	entries []Entry
	roster  Roster
}

// Entries returns the loaded entries in a stable order — file order, then the
// order within each file — so two loads of the same data agree.
func (c Catalogue) Entries() []Entry { return c.entries }

// Roster returns the streaming runtime's declared supported set, as loaded.
func (c Catalogue) Roster() Roster { return c.roster }

// Len reports how many entries were loaded.
func (c Catalogue) Len() int { return len(c.entries) }

// Load reads every catalogue data file in dir.
func Load(dir string) (Catalogue, error) {
	loaded, err := LoadFS(os.DirFS(dir), ".")
	if err != nil {
		return Catalogue{}, fmt.Errorf("catalogue in %s: %w", dir, err)
	}
	return loaded, nil
}

// LoadFS reads every catalogue data file under root in fsys.
//
// Loading is all-or-nothing. A file with one defective entry yields no entries
// at all, rather than the rest of them: a partially-loaded catalogue is a
// catalogue that silently omits models, and a model omitted without explanation
// is indistinguishable to the user from a model that was never researched.
func LoadFS(fsys fs.FS, root string) (Catalogue, error) {
	files, err := dataFiles(fsys, root)
	if err != nil {
		return Catalogue{}, err
	}
	if len(files) == 0 {
		return Catalogue{}, fmt.Errorf("%w under %q", ErrNoDataFiles, root)
	}

	// First pass: read every document, so the roster is complete before any
	// entry is resolved against it.
	type located struct {
		file  string
		index int
		doc   entryDoc
	}
	var (
		rosterNames []string
		pending     []located
	)
	for _, file := range files {
		doc, err := readDocument(fsys, file)
		if err != nil {
			return Catalogue{}, err
		}
		rosterNames = append(rosterNames, doc.StreamingRoster...)
		for i, entry := range doc.Entries {
			pending = append(pending, located{file: file, index: i, doc: entry})
		}
	}
	if len(pending) == 0 {
		return Catalogue{}, fmt.Errorf("%w under %q", ErrNoEntries, root)
	}

	// Second pass: resolve and validate every entry against the complete roster.
	roster := NewRoster(rosterNames...)
	entries := make([]Entry, 0, len(pending))
	seen := make(map[string]string, len(pending))
	for _, item := range pending {
		entry, err := item.doc.entry(roster)
		if err != nil {
			return Catalogue{}, fmt.Errorf("%s: entry %d (%q): %w",
				item.file, item.index, item.doc.ModelID, err)
		}
		if err := entry.Validate(); err != nil {
			return Catalogue{}, fmt.Errorf("%s: entry %d: %w", item.file, item.index, err)
		}
		if first, duplicate := seen[entry.Identity()]; duplicate {
			return Catalogue{}, fmt.Errorf("%s: entry %d: %w: %q also declared in %s",
				item.file, item.index, ErrDuplicateEntry, entry.Identity(), first)
		}
		seen[entry.Identity()] = item.file
		entries = append(entries, entry)
	}
	return Catalogue{entries: entries, roster: roster}, nil
}

// dataFiles lists the YAML files under root, in lexical order.
func dataFiles(fsys fs.FS, root string) ([]string, error) {
	var files []string
	err := fs.WalkDir(fsys, root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		base := path.Base(name)
		if entry.IsDir() {
			if name != root && strings.HasPrefix(base, ".") {
				return fs.SkipDir
			}
			return nil
		}
		switch path.Ext(base) {
		case ".yaml", ".yml":
			files = append(files, name)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNoDataFiles, err)
	}
	return files, nil
}

// readDocument decodes one data file, refusing any key the catalogue does not
// record rather than letting it decode as a zero value.
func readDocument(fsys fs.FS, name string) (document, error) {
	content, err := fs.ReadFile(fsys, name)
	if err != nil {
		return document{}, fmt.Errorf("%s: %w: %v", name, ErrMalformedDocument, err)
	}
	decoder := yaml.NewDecoder(strings.NewReader(string(content)))
	decoder.KnownFields(true)

	var doc document
	if err := decoder.Decode(&doc); err != nil {
		var typeErr *yaml.TypeError
		if errors.As(err, &typeErr) {
			for _, message := range typeErr.Errors {
				if strings.Contains(message, "not found in type") {
					return document{}, fmt.Errorf("%s: %w: %s", name, ErrUnknownField,
						summarise(typeErr.Errors))
				}
			}
		}
		return document{}, fmt.Errorf("%s: %w: %v", name, ErrMalformedDocument, err)
	}
	return doc, nil
}

// reportedProblems is how many individual complaints a refusal spells out.
//
// A file written against a different shape yields one complaint per key per
// entry — hundreds of them. Joined into a single line that is not a diagnosis, it
// is a wall a reader skips, and the loud failure this loader exists to produce
// stops being read. The first few, with their line numbers, are enough to place
// the problem; the rest are counted so nothing is silently dropped.
const reportedProblems = 8

func summarise(problems []string) string {
	if len(problems) <= reportedProblems {
		return strings.Join(problems, "; ")
	}
	return fmt.Sprintf("%s; (+%d more)",
		strings.Join(problems[:reportedProblems], "; "), len(problems)-reportedProblems)
}

// document is one data file.
type document struct {
	StreamingRoster []string   `yaml:"streaming_roster"`
	Entries         []entryDoc `yaml:"entries"`
}

type entryDoc struct {
	ModelID              string                `yaml:"model_id"`
	Variant              string                `yaml:"variant"`
	Family               string                `yaml:"family"`
	Architecture         string                `yaml:"architecture"`
	Descriptor           descriptorDoc         `yaml:"descriptor"`
	MemoryRequiredBytes  uint64                `yaml:"memory_required_bytes"`
	StorageRequiredBytes uint64                `yaml:"storage_required_bytes"`
	RequiresAccelerator  bool                  `yaml:"requires_accelerator"`
	Source               string                `yaml:"source"`
	UsageTerms           usageTermsDoc         `yaml:"usage_terms"`
	Runtime              string                `yaml:"runtime"`
	StreamingFamily      string                `yaml:"streaming_family"`
	ExpectedCapability   expectedCapabilityDoc `yaml:"expected_capability"`
	Integrity            integrityDoc          `yaml:"integrity"`
	Notes                []string              `yaml:"notes"`
	Annotations          map[string]any        `yaml:"annotations"`
}

type descriptorDoc struct {
	ParameterCount   uint64   `yaml:"parameter_count"`
	ActiveParameters uint64   `yaml:"active_parameters"`
	Quantisation     string   `yaml:"quantisation"`
	Specialisations  []string `yaml:"specialisations"`
}

type usageTermsDoc struct {
	LicenseID    string           `yaml:"license_id"`
	Permitted    []string         `yaml:"permitted"`
	Restrictions []restrictionDoc `yaml:"restrictions"`
}

type restrictionDoc struct {
	Term      string    `yaml:"term"`
	Excludes  []string  `yaml:"excludes"`
	Threshold amountDoc `yaml:"threshold"`
	Reference string    `yaml:"reference"`
}

type amountDoc struct {
	Value    uint64 `yaml:"value"`
	Currency string `yaml:"currency"`
	Period   string `yaml:"period"`
}

type expectedCapabilityDoc struct {
	ContextTokens             int      `yaml:"context_tokens"`
	ThroughputTokensPerSecond float64  `yaml:"throughput_tokens_per_second"`
	Modalities                []string `yaml:"modalities"`

	// Video entries record what they produce in video's own terms. Duration is
	// accepted but not stored: it is checked against the frame count and rate and
	// then discarded, so it cannot become a second, disagreeing source of truth.
	Resolution      string  `yaml:"resolution"`
	FrameWidth      int     `yaml:"frame_width"`
	FrameHeight     int     `yaml:"frame_height"`
	FrameCount      int     `yaml:"num_frames"`
	FramesPerSecond float64 `yaml:"fps"`
	DurationSeconds float64 `yaml:"duration_seconds"`
}

type integrityDoc struct {
	Algorithm string `yaml:"algorithm"`
	Digest    string `yaml:"digest"`
	SizeBytes uint64 `yaml:"size_bytes"`
}

// entry converts a decoded document entry into an Entry, resolving streaming
// eligibility by looking the declared family up in the roster.
func (d entryDoc) entry(roster Roster) (Entry, error) {
	family, err := parseFamily(d.Family)
	if err != nil {
		return Entry{}, err
	}
	architecture, err := parseArchitecture(d.Architecture)
	if err != nil {
		return Entry{}, err
	}
	runtime, err := parseRuntime(d.Runtime)
	if err != nil {
		return Entry{}, err
	}
	terms, err := d.UsageTerms.terms()
	if err != nil {
		return Entry{}, err
	}
	capability, err := d.ExpectedCapability.capability()
	if err != nil {
		return Entry{}, err
	}
	integrity, err := d.Integrity.expectation()
	if err != nil {
		return Entry{}, err
	}

	// Roster membership is a lookup by name, and only that. Architecture is
	// carried above as description and is never consulted here: several
	// mixture-of-experts models have no streaming support path, so inferring
	// eligibility from architecture offers models that cannot run (D1).
	var membership RosterMembership
	if d.StreamingFamily != "" {
		membership = roster.Membership(d.StreamingFamily)
	}

	return Entry{
		ModelID:      d.ModelID,
		Source:       d.Source,
		Variant:      d.Variant,
		Family:       family,
		Architecture: architecture,
		Descriptor: Descriptor{
			ParameterCount:   d.Descriptor.ParameterCount,
			ActiveParameters: d.Descriptor.ActiveParameters,
			Quantisation:     d.Descriptor.Quantisation,
			Specialisations:  d.Descriptor.Specialisations,
		},
		MemoryRequiredBytes:  d.MemoryRequiredBytes,
		StorageRequiredBytes: d.StorageRequiredBytes,
		RequiresAccelerator:  d.RequiresAccelerator,
		UsageTerms:           terms,
		Runtime:              runtime,
		StreamingRoster:      membership,
		ExpectedCapability:   capability,
		Integrity:            integrity,
		Notes:                d.Notes,
		Annotations:          d.Annotations,
	}, nil
}

func (d usageTermsDoc) terms() (UsageTerms, error) {
	terms := UsageTerms{LicenseID: d.LicenseID}
	for _, raw := range d.Permitted {
		purpose, err := parseUsagePurpose(raw)
		if err != nil {
			return UsageTerms{}, err
		}
		terms.Permitted = append(terms.Permitted, purpose)
	}
	for _, restriction := range d.Restrictions {
		term, err := parseRestrictionTerm(restriction.Term)
		if err != nil {
			return UsageTerms{}, err
		}
		converted := Restriction{
			Term:      term,
			Reference: restriction.Reference,
			Threshold: Amount{
				Value:    restriction.Threshold.Value,
				Currency: restriction.Threshold.Currency,
				Period:   restriction.Threshold.Period,
			},
		}
		for _, raw := range restriction.Excludes {
			purpose, err := parseUsagePurpose(raw)
			if err != nil {
				return UsageTerms{}, err
			}
			converted.Excludes = append(converted.Excludes, purpose)
		}
		terms.Restrictions = append(terms.Restrictions, converted)
	}
	return terms, nil
}

func (d expectedCapabilityDoc) capability() (ExpectedCapability, error) {
	video, err := d.video()
	if err != nil {
		return ExpectedCapability{}, err
	}
	return ExpectedCapability{
		ContextTokens:             d.ContextTokens,
		ThroughputTokensPerSecond: d.ThroughputTokensPerSecond,
		Modalities:                d.Modalities,
		Video:                     video,
	}, nil
}

// durationTolerance is how far a recorded duration may sit from the one implied
// by the frame count and rate before it is treated as a contradiction. It is
// wide enough for a figure rounded to two decimal places (49/16 recorded as
// 3.06) and far too narrow to hide a wrong rate or frame count.
const durationTolerance = 0.05

func (d expectedCapabilityDoc) video() (VideoOutput, error) {
	width, height := d.FrameWidth, d.FrameHeight
	if d.Resolution != "" {
		parsedWidth, parsedHeight, err := parseResolution(d.Resolution)
		if err != nil {
			return VideoOutput{}, err
		}
		if (width != 0 && width != parsedWidth) || (height != 0 && height != parsedHeight) {
			return VideoOutput{}, fmt.Errorf("%w: resolution %q disagrees with frame_width %d and frame_height %d",
				ErrContradictoryVideoOutput, d.Resolution, width, height)
		}
		width, height = parsedWidth, parsedHeight
	}

	video := VideoOutput{
		FrameWidth:      width,
		FrameHeight:     height,
		FrameCount:      d.FrameCount,
		FramesPerSecond: d.FramesPerSecond,
	}
	stated := width != 0 || height != 0 || d.FrameCount != 0 || d.FramesPerSecond != 0 || d.DurationSeconds != 0
	if !stated {
		return VideoOutput{}, nil
	}
	// Half a video output is worse than none: a clip with a size but no rate
	// cannot be compared against anything, and reads as though it were complete.
	if !video.Recorded() {
		return VideoOutput{}, fmt.Errorf("%w: width %d, height %d, frames %d, fps %v",
			ErrIncompleteVideoOutput, width, height, d.FrameCount, d.FramesPerSecond)
	}
	if d.DurationSeconds != 0 {
		if math.Abs(d.DurationSeconds-video.DurationSeconds()) > durationTolerance {
			return VideoOutput{}, fmt.Errorf("%w: recorded %vs, but %d frames at %v fps is %.4fs",
				ErrContradictoryVideoOutput, d.DurationSeconds, d.FrameCount, d.FramesPerSecond,
				video.DurationSeconds())
		}
	}
	return video, nil
}

// parseResolution reads "<width>x<height>". It is the one place the two numbers
// may be written as one value, and both must be positive.
func parseResolution(raw string) (int, int, error) {
	width, height, found := strings.Cut(strings.TrimSpace(raw), "x")
	if !found {
		return 0, 0, fmt.Errorf("%w: %q", ErrMalformedResolution, raw)
	}
	parsedWidth, widthErr := strconv.Atoi(strings.TrimSpace(width))
	parsedHeight, heightErr := strconv.Atoi(strings.TrimSpace(height))
	if widthErr != nil || heightErr != nil || parsedWidth <= 0 || parsedHeight <= 0 {
		return 0, 0, fmt.Errorf("%w: %q", ErrMalformedResolution, raw)
	}
	return parsedWidth, parsedHeight, nil
}

func (d integrityDoc) expectation() (IntegrityExpectation, error) {
	algorithm, err := parseDigestAlgorithm(d.Algorithm)
	if err != nil {
		return IntegrityExpectation{}, err
	}
	return IntegrityExpectation{
		Algorithm: algorithm,
		Digest:    strings.ToLower(strings.TrimSpace(d.Digest)),
		SizeBytes: d.SizeBytes,
	}, nil
}

// The parsers below reject an unrecognised value rather than storing it.
//
// A misspelled `commercail` in a permitted list would otherwise leave the model
// silently unofferable for commercial use, with the data file plainly stating
// the opposite — a disagreement between what the catalogue says and what it does
// that nothing downstream could detect.
//
// An ABSENT optional value is not the same as a misspelled one and is accepted:
// architecture is descriptive and may be unrecorded, and a missing digest
// algorithm is reported by Entry.Validate as an unverifiable entry, which names
// the real problem better than a parse error would.

func parseFamily(raw string) (CapabilityFamily, error) {
	family := CapabilityFamily(raw)
	if !family.Known() {
		return "", fmt.Errorf("%w: %q", ErrUnknownFamily, raw)
	}
	return family, nil
}

func parseRuntime(raw string) (RuntimeKind, error) {
	runtime := RuntimeKind(raw)
	if !runtime.Known() {
		return "", fmt.Errorf("%w: %q", ErrUnknownRuntime, raw)
	}
	return runtime, nil
}

func parseArchitecture(raw string) (Architecture, error) {
	switch architecture := Architecture(raw); architecture {
	case "":
		return "", nil
	case ArchitectureDense, ArchitectureMixtureOfExperts, ArchitectureDiffusion, ArchitectureEncoderDecoder:
		return architecture, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownArchitecture, raw)
	}
}

func parseUsagePurpose(raw string) (UsagePurpose, error) {
	switch purpose := UsagePurpose(raw); purpose {
	case UsageCommercial, UsagePersonal, UsageResearch, UsageEvaluation:
		return purpose, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownUsagePurpose, raw)
	}
}

func parseRestrictionTerm(raw string) (RestrictionTerm, error) {
	switch term := RestrictionTerm(raw); term {
	case TermNonCommercial, TermRevenueCap, TermAttributionRequired, TermShareAlike, TermResearchOnly:
		return term, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownRestrictionTerm, raw)
	}
}

func parseDigestAlgorithm(raw string) (DigestAlgorithm, error) {
	switch algorithm := DigestAlgorithm(raw); algorithm {
	case "":
		return "", nil
	case DigestSHA256, DigestBLAKE3:
		return algorithm, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownDigestAlgorithm, raw)
	}
}
