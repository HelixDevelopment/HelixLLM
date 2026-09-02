package catalogue_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/catalogue"
	"github.com/stretchr/testify/require"
)

// weightFile writes a stand-in weight file and returns its path with the
// expectation that genuinely describes it.
func weightFile(t *testing.T, content []byte) (string, catalogue.IntegrityExpectation) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "weights.gguf")
	require.NoError(t, os.WriteFile(path, content, 0o600))
	sum := sha256.Sum256(content)
	return path, catalogue.IntegrityExpectation{
		Algorithm: catalogue.DigestSHA256,
		Digest:    hex.EncodeToString(sum[:]),
		SizeBytes: uint64(len(content)),
	}
}

const allowedPrefix = "https://huggingface.co/deepseek-ai/DeepSeek-R1"

func allowlist(t *testing.T) catalogue.SourceAllowlist {
	t.Helper()
	list, err := catalogue.NewSourceAllowlist(allowedPrefix)
	require.NoError(t, err)
	return list
}

// TestVerifyAcceptsAFileMatchingItsRecordedExpectation — the positive control.
// Without it, a verifier that refuses everything would pass every refusal test
// below while making the product unusable.
func TestVerifyAcceptsAFileMatchingItsRecordedExpectation(t *testing.T) {
	path, want := weightFile(t, []byte("intact weights"))
	require.NoError(t, catalogue.VerifyFile(path, want))
}

// TestVerifyRefusesACorruptedFile — same length, one bit different. This is the
// substitution case SC-011 exists for: a file that passes every cheap check and
// is not the file the catalogue recorded.
func TestVerifyRefusesACorruptedFile(t *testing.T) {
	path, want := weightFile(t, []byte("intact weights"))
	require.NoError(t, os.WriteFile(path, []byte("intact weightt"), 0o600))

	err := catalogue.VerifyFile(path, want)
	require.ErrorIs(t, err, catalogue.ErrDigestMismatch)
	require.Contains(t, err.Error(), want.Digest, "the error must name the value that was expected")
}

// TestVerifyRefusesAFileOfTheWrongLength — checked before the digest so a
// truncated download fails without reading gigabytes, and reports the cheap
// reason rather than a digest mismatch that hides it.
func TestVerifyRefusesAFileOfTheWrongLength(t *testing.T) {
	for name, content := range map[string][]byte{
		"truncated": []byte("intact weigh"),
		"extended":  []byte("intact weights and more"),
	} {
		t.Run(name, func(t *testing.T) {
			path, want := weightFile(t, []byte("intact weights"))
			require.NoError(t, os.WriteFile(path, content, 0o600))
			require.ErrorIs(t, catalogue.VerifyFile(path, want), catalogue.ErrSizeMismatch)
		})
	}

	t.Run("stream longer than expected is not accepted on a prefix match", func(t *testing.T) {
		_, want := weightFile(t, []byte("intact weights"))
		err := catalogue.Verify(bytes.NewReader([]byte("intact weights"+strings.Repeat("x", 4096))), want)
		require.ErrorIs(t, err, catalogue.ErrSizeMismatch)
	})
}

// TestVerifyRefusesAnUnverifiableExpectation — an entry with no recorded value
// cannot be verified, and "cannot verify" must mean refuse, never proceed.
func TestVerifyRefusesAnUnverifiableExpectation(t *testing.T) {
	path, want := weightFile(t, []byte("intact weights"))

	incomplete := want
	incomplete.Digest = ""
	require.ErrorIs(t, catalogue.VerifyFile(path, incomplete), catalogue.ErrIncompleteIntegrity)

	unsupported := want
	unsupported.Algorithm = catalogue.DigestBLAKE3
	err := catalogue.VerifyFile(path, unsupported)
	require.ErrorIs(t, err, catalogue.ErrUnsupportedDigestAlgorithm,
		"an algorithm this build cannot compute must refuse, never silently skip verification")
	require.Contains(t, err.Error(), string(catalogue.DigestBLAKE3))

	malformed := want
	malformed.Digest = strings.Repeat("z", 64)
	require.ErrorIs(t, catalogue.VerifyFile(path, malformed), catalogue.ErrMalformedDigest)

	wrongLength := want
	wrongLength.Digest = "0a5e93"
	require.ErrorIs(t, catalogue.VerifyFile(path, wrongLength), catalogue.ErrMalformedDigest)
}

// TestAllowlistPermitsOnlyListedSources. The sibling-prefix row is the one that
// matters: a naive strings.HasPrefix admits an attacker-controlled repository
// whose name merely starts with an allowlisted one.
func TestAllowlistPermitsOnlyListedSources(t *testing.T) {
	list := allowlist(t)

	permitted := []string{
		allowedPrefix,
		allowedPrefix + "/resolve/main/model-00001-of-00163.safetensors",
		allowedPrefix + "/tree/main",
	}
	for _, source := range permitted {
		require.True(t, list.Permits(source), "must permit %q", source)
	}

	refused := map[string]string{
		"sibling repository sharing the prefix": "https://huggingface.co/deepseek-ai/DeepSeek-R1-evil/weights.gguf",
		"different organisation":                "https://huggingface.co/attacker/DeepSeek-R1/weights.gguf",
		"different host":                        "https://huggingface.co.attacker.test/deepseek-ai/DeepSeek-R1/w.gguf",
		"downgraded scheme":                     "http://huggingface.co/deepseek-ai/DeepSeek-R1/weights.gguf",
		"userinfo disguising the host":          "https://huggingface.co@attacker.test/deepseek-ai/DeepSeek-R1/w.gguf",
		"traversal escaping the prefix":         "https://huggingface.co/deepseek-ai/DeepSeek-R1/../../attacker/w.gguf",
		"unrelated source":                      "https://attacker.test/weights.gguf",
		"not a source at all":                   "weights.gguf",
	}
	for name, source := range refused {
		t.Run(name, func(t *testing.T) {
			require.False(t, list.Permits(source), "must refuse %q", source)
			require.ErrorIs(t, list.Authorise(source), catalogue.ErrSourceNotAllowlisted)
		})
	}
}

// TestUnconfiguredAllowlistPermitsNothing — the allowlist fails closed. An
// allowlist that was never populated must refuse every source, so a missing
// configuration cannot silently become "obtain from anywhere".
func TestUnconfiguredAllowlistPermitsNothing(t *testing.T) {
	empty, err := catalogue.NewSourceAllowlist()
	require.NoError(t, err)
	require.False(t, empty.Permits(allowedPrefix))
	require.ErrorIs(t, empty.Authorise(allowedPrefix), catalogue.ErrNoAllowlistConfigured)

	_, err = catalogue.NewSourceAllowlist("not a url at all")
	require.ErrorIs(t, err, catalogue.ErrMalformedSource,
		"a malformed allowlist entry must be rejected when configured, not silently dropped")
}

// TestOpenWeightsRefusesASourceOutsideTheAllowlist is one half of SC-011's
// "verifiable by attempting both and observing refusal".
func TestOpenWeightsRefusesASourceOutsideTheAllowlist(t *testing.T) {
	path, want := weightFile(t, []byte("intact weights"))
	gate := catalogue.NewWeightGate(allowlist(t))

	reader, err := gate.OpenWeights("https://attacker.test/weights.gguf", path, want)
	require.Nil(t, reader, "no readable handle may be returned for a refused source")
	require.ErrorIs(t, err, catalogue.ErrSourceNotAllowlisted)
	require.Contains(t, err.Error(), "attacker.test", "the refusal must name the source")
}

// TestOpenWeightsRefusesAFileFailingVerification is the other half: a file from
// a perfectly allowlisted source that is not the file the catalogue recorded.
func TestOpenWeightsRefusesAFileFailingVerification(t *testing.T) {
	path, want := weightFile(t, []byte("intact weights"))
	require.NoError(t, os.WriteFile(path, []byte("substituted!!!"), 0o600))

	gate := catalogue.NewWeightGate(allowlist(t))
	reader, err := gate.OpenWeights(allowedPrefix+"/resolve/main/w.gguf", path, want)
	require.Nil(t, reader)
	require.ErrorIs(t, err, catalogue.ErrDigestMismatch)
}

// TestOpenWeightsReturnsAHandleOnlyWhenBothChecksPass. The handle is the only
// way to read weights, so passing both checks is structurally unavoidable rather
// than a convention a caller is trusted to follow.
func TestOpenWeightsReturnsAHandleOnlyWhenBothChecksPass(t *testing.T) {
	content := []byte("intact weights")
	path, want := weightFile(t, content)

	gate := catalogue.NewWeightGate(allowlist(t))
	reader, err := gate.OpenWeights(allowedPrefix+"/resolve/main/w.gguf", path, want)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reader.Close()) })

	got, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.Equal(t, content, got, "the verified handle must read back the file that was verified")
}

// TestOpenWeightsRefusesEveryCorruptionOfEveryCataloguedEntry sweeps the loaded
// catalogue: SC-011 is a property of every entry, not of one hand-picked one.
func TestOpenWeightsRefusesEveryCorruptionOfEveryCataloguedEntry(t *testing.T) {
	loaded, err := catalogue.Load(validDataDir)
	require.NoError(t, err)
	gate := catalogue.NewWeightGate(allowlist(t))

	for _, entry := range loaded.Entries() {
		t.Run(entry.Identity(), func(t *testing.T) {
			// A file that is emphatically not the recorded weights.
			path := filepath.Join(t.TempDir(), "weights.gguf")
			require.NoError(t, os.WriteFile(path, []byte("not the recorded weights"), 0o600))

			reader, err := gate.OpenWeights(allowedPrefix+"/resolve/main/w.gguf", path, entry.Integrity)
			require.Nil(t, reader)
			require.Error(t, err, "no weight file may load unverified")
		})
	}
}

// TestWrongLengthIsRefusedWithoutHashingTheFile pins the cheap-first ordering.
//
// Weight files run to hundreds of gigabytes. A truncated download must be
// refused from its length alone, not after hashing every byte of it, and the
// only way to observe which of the two checks fired is the refusal it produced —
// so this asserts the length-derived wording, not merely ErrSizeMismatch, which
// both paths return.
func TestWrongLengthIsRefusedWithoutHashingTheFile(t *testing.T) {
	path, want := weightFile(t, []byte("intact weights"))
	require.NoError(t, os.WriteFile(path, []byte("truncated"), 0o600))

	err := catalogue.VerifyFile(path, want)
	require.ErrorIs(t, err, catalogue.ErrSizeMismatch)
	require.Contains(t, err.Error(), "file is 9 bytes",
		"the length must be refused from the file's size, before any of it is read")

	gate := catalogue.NewWeightGate(allowlist(t))
	_, err = gate.OpenWeights(allowedPrefix+"/resolve/main/w.gguf", path, want)
	require.ErrorIs(t, err, catalogue.ErrSizeMismatch)
	require.Contains(t, err.Error(), "file is 9 bytes")
}
