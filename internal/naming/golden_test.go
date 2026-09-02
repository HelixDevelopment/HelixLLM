package naming_test

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"

	"github.com/HelixDevelopment/HelixLLM/internal/naming"
)

// FR-015: the naming scheme is stable across releases.
//
// That requirement exists for one concrete reason: these identifiers are
// written into users' tool configurations — a Claude Toolkit alias, an OpenCode
// provider entry — and a configuration refers to a model BY THIS NAME. Change
// how a name is derived and every existing entry stops resolving. Nothing
// errors; the model simply is not found any more, in a file the user wrote
// weeks ago and has no reason to revisit.
//
// Nothing enforced that. derive_test.go proves determinism WITHIN one run
// (derive twice, compare), which is a different and much weaker property: an
// edit to the sanitiser, the separator, the digest length, the prefix, or to
// Identity.String — whose output is what gets hashed — keeps every existing
// test green while silently re-minting every name in the field.
//
// So the pinned values live in a file, checked in, diffed by review.
//
// Regenerate deliberately with:
//
//	go test ./internal/naming -run TestNamingSchemeIsStableAcrossReleases -update
//
// A diff in that file is never a formatting nit. It is a breaking change to a
// published interface, and it needs the migration path FR-015 implies.

var update = flag.Bool("update", false,
	"rewrite the naming-scheme golden file; only with a deliberate, migrated scheme change")

const goldenPath = "testdata/naming_scheme.golden"

// digestHexLen mirrors the unexported naming.digestHexLen.
//
// Duplicating it here is the point rather than a smell: it is part of the
// published scheme, so a change to it must break something. This copy is what
// breaks.
const digestHexLen = 12

// goldenCase is one pinned identity. Each exists because it exercises a
// distinct part of the derivation; a corpus of six ordinary names would pin
// nothing but the happy path.
type goldenCase struct {
	name string
	id   naming.Identity
	why  string
}

// The corpus. Fields are set literally rather than through NewIdentity so the
// exact bytes being hashed are visible here, and so the normalisation NewIdentity
// applies cannot quietly change what is pinned.
var goldenCases = []goldenCase{
	{
		name: "plain",
		id:   naming.Identity{Host: "gpu-01", Model: "llama3"},
		why:  "the ordinary shape: host and model, nothing escaped, nothing sanitised",
	},
	{
		name: "variant",
		id:   naming.Identity{Host: "gpu-01", Model: "llama3", Variant: "8b"},
		why:  "a variant segment appears in the identity and in the readable part",
	},
	{
		name: "near-miss-flat",
		id:   naming.Identity{Host: "gpu-01", Model: "llama3-8b"},
		why: "pairs with near-miss-variant: DIFFERENT identities whose readable parts " +
			"sanitise identically, so only the digest keeps them apart",
	},
	{
		name: "forbidden-charset",
		id: naming.Identity{
			Host:    "gpu-01.local",
			Model:   "library/qwen2.5-coder",
			Variant: "7b-instruct-q4_K_M",
		},
		why: "'.', '/' and upper case are forbidden or folded by the toolkit charset; " +
			"pins how runs of forbidden characters collapse",
	},
	{
		name: "length-cap",
		id: naming.Identity{
			Host:  "gpu-01",
			Model: "meta-llama-3-1-70b-instruct-abliterated-uncensored-experimental-build",
		},
		why: "exceeds the toolkit's 64-byte cap, so the readable part is trimmed — and " +
			"ONLY the readable part; see TestNamingSchemeCapTrimsOnlyTheReadablePart",
	},
	{
		name: "non-ascii-model",
		id:   naming.Identity{Host: "gpu-01", Model: "日本語モデル"},
		why:  "no character of the model survives the charset; the host still does",
	},
	{
		name: "readable-part-vanishes",
		id:   naming.Identity{Host: "日本", Model: "モデル"},
		why:  "nothing at all survives sanitising: pins the degenerate prefix+digest form",
	},
}

// nearMissVariant is the other half of the near-miss pair. It is also the
// "variant" case above; naming it here makes the pairing explicit.
var nearMissVariant = naming.Identity{Host: "gpu-01", Model: "llama3", Variant: "8b"}

// goldenRulesets are the rulesets whose output reaches a user's configuration.
// Both are pinned, because both are written to disk on the user's machine.
var goldenRulesets = []naming.Ruleset{naming.ClaudeToolkit, naming.OpenCode}

// renderGolden builds the file's exact contents.
func renderGolden(t *testing.T) string {
	t.Helper()

	var b strings.Builder
	b.WriteString("# HelixLLM naming scheme — PINNED (FR-015).\n")
	b.WriteString("#\n")
	b.WriteString("# These identifiers live in users' tool configurations. A change to any line\n")
	b.WriteString("# below breaks every existing configuration that refers to that model: the\n")
	b.WriteString("# entry stops resolving, silently. Treat a diff here as a breaking change to\n")
	b.WriteString("# a published interface, needing the migration path FR-015 implies — never as\n")
	b.WriteString("# a value to refresh so the test goes green.\n")
	b.WriteString("#\n")
	b.WriteString("# Columns, tab-separated:\n")
	b.WriteString("#   consumer  case  canonical-identity  derived-identifier\n")
	b.WriteString("#\n")
	b.WriteString("# The canonical identity is pinned alongside the identifier because it is what\n")
	b.WriteString("# gets hashed: a change to Identity.String re-mints every identifier too.\n")

	for _, rs := range goldenRulesets {
		b.WriteString("#\n")
		fmt.Fprintf(&b, "# consumer %q: prefix %q, separator %q, max length %d\n",
			rs.Name, rs.Prefix, string(rs.Separator), rs.MaxLength)
		for _, c := range goldenCases {
			identifier, err := naming.Derive(c.id, rs)
			if err != nil {
				t.Fatalf("Derive(%s, %s): %v", c.name, rs.Name, err)
			}
			fmt.Fprintf(&b, "%s\t%s\t%s\t%s\n", rs.Name, c.name, c.id.String(), identifier)
		}
	}
	return b.String()
}

// TestNamingSchemeIsStableAcrossReleases is the pin itself.
func TestNamingSchemeIsStableAcrossReleases(t *testing.T) {
	got := renderGolden(t)

	if *update {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("create testdata: %v", err)
		}
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("rewrote %s — commit it only with a deliberate, migrated scheme change", goldenPath)
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read %s: %v\n\nRegenerate with: go test ./internal/naming -run %s -update",
			goldenPath, err, t.Name())
	}

	if got == string(want) {
		return
	}

	// Say what a mismatch MEANS, and which entries moved. "golden mismatch" tells
	// the next reader to run -update; this tells them what running -update does
	// to the people already using these names.
	t.Errorf(`THE NAMING SCHEME CHANGED.

Every existing user configuration that refers to one of the models below will
stop resolving. The name in an alias, a provider entry, a saved model selection
no longer matches anything HelixLLM publishes; nothing errors, the model just
disappears from the tool.

This is a breaking change to a published interface (FR-015), not a test that
needs refreshing. If the change is not intended, revert it — the likely causes
are an edit to the sanitiser, the separator, the prefix, the digest length, or
to Identity.String, whose output is what gets hashed. If it IS intended, it
needs a migration path for the names already in the field, and only then a
deliberate: go test ./internal/naming -run %s -update

%s`, t.Name(), diffLines(string(want), got))
}

// diffLines reports which pinned entries changed, in the terms the reader cares
// about: which consumer, which case, old name, new name.
func diffLines(want, got string) string {
	index := func(s string) map[string]string {
		m := map[string]string{}
		for _, line := range strings.Split(s, "\n") {
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			f := strings.Split(line, "\t")
			if len(f) != 4 {
				m["malformed: "+line] = line
				continue
			}
			m[f[0]+"/"+f[1]] = f[2] + "\t" + f[3]
		}
		return m
	}
	w, g := index(want), index(got)

	var b strings.Builder
	for _, rs := range goldenRulesets {
		for _, c := range goldenCases {
			key := rs.Name + "/" + c.name
			was, had := w[key]
			now, has := g[key]
			switch {
			case had && has && was != now:
				wasF, nowF := strings.Split(was, "\t"), strings.Split(now, "\t")
				fmt.Fprintf(&b, "  %s\n      identity   was %s\n                 now %s\n"+
					"      identifier was %s\n                 now %s\n",
					key, wasF[0], nowF[0], wasF[1], nowF[1])
			case had && !has:
				fmt.Fprintf(&b, "  %s\n      REMOVED from the scheme (was %s)\n", key, was)
			case !had && has:
				fmt.Fprintf(&b, "  %s\n      NEW in the scheme (%s)\n", key, now)
			}
		}
	}
	if b.Len() == 0 {
		return "  (the entries match; the file's header or ordering changed)"
	}
	return "Changed entries:\n" + b.String()
}

// The cap is the case most likely to be "simplified" into a plain truncation of
// the whole identifier, which would shorten the digest — quietly weakening the
// only thing keeping two distinct identities apart. Pinning the identifier alone
// would not say WHY the pinned value has that shape, so the property is asserted
// directly: the capped identifier ends in the SAME full digest as the uncapped
// one derived for a consumer with no cap.
func TestNamingSchemeCapTrimsOnlyTheReadablePart(t *testing.T) {
	var capped goldenCase
	for _, c := range goldenCases {
		if c.name == "length-cap" {
			capped = c
		}
	}
	if capped.name == "" {
		t.Fatal("the length-cap case is missing from the corpus; the cap is unexercised")
	}

	if naming.ClaudeToolkit.MaxLength == 0 {
		t.Fatal("ClaudeToolkit has no length cap any more; this test proves nothing")
	}
	if naming.OpenCode.MaxLength != 0 {
		t.Fatalf("OpenCode now caps at %d; it is used here as the uncapped reference",
			naming.OpenCode.MaxLength)
	}

	short, err := naming.Derive(capped.id, naming.ClaudeToolkit)
	if err != nil {
		t.Fatalf("Derive(capped): %v", err)
	}
	long, err := naming.Derive(capped.id, naming.OpenCode)
	if err != nil {
		t.Fatalf("Derive(uncapped): %v", err)
	}

	if len(short) > naming.ClaudeToolkit.MaxLength {
		t.Errorf("capped identifier is %d bytes, over the consumer's %d-byte limit: %q",
			len(short), naming.ClaudeToolkit.MaxLength, short)
	}
	if len(long) <= naming.ClaudeToolkit.MaxLength {
		t.Fatalf("the corpus case no longer exceeds the cap (%d bytes uncapped); it stopped "+
			"exercising the trim and must be lengthened: %q", len(long), long)
	}

	wantDigest := long[len(long)-digestHexLen:]
	gotDigest := short[len(short)-digestHexLen:]
	if gotDigest != wantDigest {
		t.Errorf("the length cap truncated the DIGEST: capped ends %q, uncapped ends %q.\n"+
			"The readable part is lossy by design; the digest is the only thing that keeps two "+
			"distinct identities apart, so shortening it silently weakens collision resistance.",
			gotDigest, wantDigest)
	}
	for _, r := range gotDigest {
		if !strings.ContainsRune("0123456789abcdef", r) {
			t.Errorf("identifier %q does not end in a %d-character hex digest", short, digestHexLen)
			break
		}
	}
}

// The pair the corpus exists to hold apart. Their readable parts are identical
// after sanitising — model "llama3" variant "8b" and model "llama3-8b" both
// render "gpu-01-llama3-8b" — so if the pinned identifiers ever became equal,
// one of the two models would vanish from every user's configuration while
// every charset assertion still passed.
func TestNamingSchemeKeepsPinnedNearMissesApart(t *testing.T) {
	var flat naming.Identity
	for _, c := range goldenCases {
		if c.name == "near-miss-flat" {
			flat = c.id
		}
	}
	if flat.Model == "" {
		t.Fatal("the near-miss-flat case is missing from the corpus")
	}
	if flat == nearMissVariant {
		t.Fatal("the near-miss pair collapsed into one identity; it proves nothing")
	}

	for _, rs := range goldenRulesets {
		a, err := naming.Derive(flat, rs)
		if err != nil {
			t.Fatalf("Derive(flat, %s): %v", rs.Name, err)
		}
		b, err := naming.Derive(nearMissVariant, rs)
		if err != nil {
			t.Fatalf("Derive(variant, %s): %v", rs.Name, err)
		}
		if a == b {
			t.Errorf("consumer %q: %q and %q both derive %q; one of these two models is "+
				"unreachable", rs.Name, flat.String(), nearMissVariant.String(), a)
		}
	}
}

// A golden file can pin a wrong value as faithfully as a right one. Every
// identifier it holds must still satisfy the rules of the consumer it is for,
// so a scheme change that made names invalid could not be absorbed by simply
// re-running with -update.
func TestNamingSchemeGoldenValuesAreValidForTheirConsumer(t *testing.T) {
	raw, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read %s: %v", goldenPath, err)
	}

	byName := map[string]naming.Ruleset{}
	for _, rs := range goldenRulesets {
		byName[rs.Name] = rs
	}

	rows := 0
	for _, line := range strings.Split(string(raw), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 4 {
			t.Errorf("malformed golden row: %q", line)
			continue
		}
		rows++
		consumer, caseName, identity, identifier := f[0], f[1], f[2], f[3]

		rs, ok := byName[consumer]
		if !ok {
			t.Errorf("row %q names consumer %q, which is not pinned here", caseName, consumer)
			continue
		}
		if _, err := naming.ParseIdentity(identity); err != nil {
			t.Errorf("%s/%s: pinned identity %q does not parse: %v", consumer, caseName, identity, err)
		}
		if !strings.HasPrefix(identifier, rs.Prefix) {
			t.Errorf("%s/%s: identifier %q does not open with the consumer's prefix %q",
				consumer, caseName, identifier, rs.Prefix)
		}
		if rs.MustStartWithLetter && !unicode.IsLetter([]rune(identifier)[0]) {
			t.Errorf("%s/%s: identifier %q does not start with a letter", consumer, caseName, identifier)
		}
		for _, r := range identifier {
			if !rs.Allow(r) {
				t.Errorf("%s/%s: identifier %q contains %q, which consumer %q forbids",
					consumer, caseName, identifier, r, consumer)
				break
			}
		}
		if rs.MaxLength > 0 && len(identifier) > rs.MaxLength {
			t.Errorf("%s/%s: identifier %q is %d bytes, over consumer %q's %d-byte limit",
				consumer, caseName, identifier, len(identifier), consumer, rs.MaxLength)
		}
	}

	if want := len(goldenCases) * len(goldenRulesets); rows != want {
		t.Errorf("golden holds %d rows, want %d (%d cases x %d consumers); entries were dropped",
			rows, want, len(goldenCases), len(goldenRulesets))
	}
}
