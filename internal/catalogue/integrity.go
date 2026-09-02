package catalogue

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"math"
	"net/url"
	"os"
	"strings"
)

// Errors reported when obtaining or verifying a weight file.
//
// They are distinct because their remedies are distinct: a source off the
// allowlist is a provenance decision for an operator, a digest mismatch is a
// corrupted or substituted download to be re-obtained, and an algorithm this
// build cannot compute is a build gap. Collapsing them into one "could not load"
// loses the remedy and, worse, invites treating the third as harmless.
var (
	ErrSourceNotAllowlisted = errors.New("catalogue: model source is not on the allowlist")
	// ErrNoAllowlistConfigured is reported instead of ErrSourceNotAllowlisted's
	// bare form when the allowlist is empty, because "you allowlisted nothing" and
	// "this particular source is not allowlisted" have different fixes.
	ErrNoAllowlistConfigured      = errors.New("catalogue: no model source is allowlisted, so no source may be obtained from")
	ErrMalformedSource            = errors.New("catalogue: source is not an absolute location with a scheme and host")
	ErrSizeMismatch               = errors.New("catalogue: weight file length does not match the recorded expectation")
	ErrDigestMismatch             = errors.New("catalogue: weight file digest does not match the recorded expectation")
	ErrUnsupportedDigestAlgorithm = errors.New("catalogue: digest algorithm cannot be computed by this build")
	ErrMalformedDigest            = errors.New("catalogue: recorded digest is not a well-formed value for its algorithm")
	ErrUnreadableWeights          = errors.New("catalogue: weight file could not be read for verification")
)

// allowedSource is one allowlist prefix, split so matching happens on whole path
// segments. Matching on the raw string would admit any repository whose name
// merely begins with an allowlisted one.
type allowedSource struct {
	scheme   string
	host     string
	segments []string
	raw      string
}

// SourceAllowlist is the explicit set of locations model files may be obtained
// from (FR-010). It fails closed: an allowlist that was never populated permits
// nothing, so a missing configuration can never become "obtain from anywhere".
type SourceAllowlist struct {
	allowed []allowedSource
}

// NewSourceAllowlist builds an allowlist from location prefixes such as
// "https://huggingface.co/deepseek-ai/DeepSeek-R1". A prefix that is not a
// usable absolute location is rejected here, when it is configured, rather than
// being silently dropped and leaving the operator believing it is in force.
func NewSourceAllowlist(prefixes ...string) (SourceAllowlist, error) {
	list := SourceAllowlist{}
	for _, prefix := range prefixes {
		parsed, err := parseSource(prefix)
		if err != nil {
			return SourceAllowlist{}, fmt.Errorf("allowlist entry %q: %w", prefix, err)
		}
		list.allowed = append(list.allowed, parsed)
	}
	return list, nil
}

// Len reports how many prefixes the allowlist declares.
func (a SourceAllowlist) Len() int { return len(a.allowed) }

// Permits reports whether source lies at or beneath an allowlisted prefix.
func (a SourceAllowlist) Permits(source string) bool { return a.Authorise(source) == nil }

// Authorise reports why source may not be obtained from, naming the source, so
// a refusal can be explained rather than merely returned.
func (a SourceAllowlist) Authorise(source string) error {
	if len(a.allowed) == 0 {
		return fmt.Errorf("%w: %q", ErrNoAllowlistConfigured, source)
	}
	candidate, err := parseSource(source)
	if err != nil {
		return fmt.Errorf("%w: %q: %w", ErrSourceNotAllowlisted, source, err)
	}
	for _, allowed := range a.allowed {
		if allowed.admits(candidate) {
			return nil
		}
	}
	return fmt.Errorf("%w: %q", ErrSourceNotAllowlisted, source)
}

// admits reports whether candidate lies at or beneath this prefix. Scheme and
// host must match exactly, and the prefix's path segments must match whole
// segments of the candidate's — so "…/DeepSeek-R1" does not admit
// "…/DeepSeek-R1-evil".
func (a allowedSource) admits(candidate allowedSource) bool {
	if a.scheme != candidate.scheme || a.host != candidate.host {
		return false
	}
	if len(candidate.segments) < len(a.segments) {
		return false
	}
	for i, segment := range a.segments {
		if candidate.segments[i] != segment {
			return false
		}
	}
	return true
}

// parseSource splits a location into the parts matching is performed on.
//
// It refuses anything that could make the host ambiguous to a reader: a missing
// scheme or host, embedded credentials (which put a trusted-looking name to the
// left of an untrusted host), and any "." or ".." segment (which would let a
// path climb out of an allowlisted prefix).
func parseSource(raw string) (allowedSource, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return allowedSource{}, fmt.Errorf("%w: %v", ErrMalformedSource, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return allowedSource{}, fmt.Errorf("%w: needs both a scheme and a host", ErrMalformedSource)
	}
	if parsed.User != nil {
		return allowedSource{}, fmt.Errorf("%w: embedded credentials disguise the host", ErrMalformedSource)
	}
	var segments []string
	for _, segment := range strings.Split(parsed.Path, "/") {
		switch segment {
		case "", ".":
			continue
		case "..":
			return allowedSource{}, fmt.Errorf("%w: path traverses upwards", ErrMalformedSource)
		default:
			segments = append(segments, segment)
		}
	}
	return allowedSource{
		scheme:   strings.ToLower(parsed.Scheme),
		host:     strings.ToLower(parsed.Host),
		segments: segments,
		raw:      raw,
	}, nil
}

// hasherFor returns the hasher for algorithm.
//
// An algorithm the catalogue records but this build cannot compute is an error,
// never a skipped check: "we could not verify" must never resolve to "we loaded
// it anyway".
func hasherFor(algorithm DigestAlgorithm) (hash.Hash, error) {
	switch algorithm {
	case DigestSHA256:
		return sha256.New(), nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedDigestAlgorithm, string(algorithm))
	}
}

// expectedDigest decodes the recorded digest and checks it is the right width
// for its algorithm. A digest of the wrong width can never match, so accepting
// one would turn every verification into a guaranteed failure that looks like
// corruption rather than the data defect it is.
func expectedDigest(want IntegrityExpectation, hasher hash.Hash) ([]byte, error) {
	digest, err := hex.DecodeString(want.Digest)
	if err != nil {
		return nil, fmt.Errorf("%w: %q is not hexadecimal", ErrMalformedDigest, want.Digest)
	}
	if len(digest) != hasher.Size() {
		return nil, fmt.Errorf("%w: %q is %d bytes, %s digests are %d",
			ErrMalformedDigest, want.Digest, len(digest), want.Algorithm, hasher.Size())
	}
	return digest, nil
}

// Verify reads r to its end and reports whether it matches want.
//
// Length is checked as the stream is consumed, and a stream longer than the
// recorded length fails: verifying only the recorded prefix would accept a file
// with anything appended to it.
func Verify(r io.Reader, want IntegrityExpectation) error {
	if !want.Complete() {
		return fmt.Errorf("%w: algorithm %q, digest %q, size %d",
			ErrIncompleteIntegrity, want.Algorithm, want.Digest, want.SizeBytes)
	}
	hasher, err := hasherFor(want.Algorithm)
	if err != nil {
		return err
	}
	digest, err := expectedDigest(want, hasher)
	if err != nil {
		return err
	}
	if want.SizeBytes > uint64(math.MaxInt64)-1 {
		return fmt.Errorf("%w: recorded size %d is not a readable length", ErrIncompleteIntegrity, want.SizeBytes)
	}

	// One byte beyond the expectation, so an over-long stream is detected rather
	// than silently truncated to a matching prefix.
	read, err := io.Copy(hasher, io.LimitReader(r, int64(want.SizeBytes)+1))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnreadableWeights, err)
	}
	if uint64(read) != want.SizeBytes {
		return fmt.Errorf("%w: read %d bytes, expected %d", ErrSizeMismatch, read, want.SizeBytes)
	}
	if got := hasher.Sum(nil); !bytes.Equal(got, digest) {
		return fmt.Errorf("%w: computed %s, expected %s",
			ErrDigestMismatch, hex.EncodeToString(got), want.Digest)
	}
	return nil
}

// VerifyFile reports whether the file at path matches want.
func VerifyFile(path string, want IntegrityExpectation) error {
	if !want.Complete() {
		return fmt.Errorf("%w: algorithm %q, digest %q, size %d",
			ErrIncompleteIntegrity, want.Algorithm, want.Digest, want.SizeBytes)
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnreadableWeights, err)
	}
	defer file.Close()
	return verifyOpenFile(file, want)
}

// verifyOpenFile checks the cheap length first — a truncated multi-gigabyte
// download is then refused without hashing any of it — and reports the length as
// the reason, rather than the digest mismatch that would hide it.
//
// The wording of this refusal is deliberately distinct from the one Verify emits
// while streaming ("file is N bytes" here, "read N bytes" there). Both mean the
// same thing to a reader, and both are ErrSizeMismatch; the difference is what
// lets a test observe that the cheap check fired and the file was never hashed.
// Without that, removing this check breaks no test and it becomes decoration
// whose absence nobody notices until a user waits ten minutes to be told a
// download was truncated.
func verifyOpenFile(file *os.File, want IntegrityExpectation) error {
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnreadableWeights, err)
	}
	if info.Mode().IsRegular() && uint64(info.Size()) != want.SizeBytes {
		return fmt.Errorf("%w: file is %d bytes, expected %d", ErrSizeMismatch, info.Size(), want.SizeBytes)
	}
	return Verify(file, want)
}

// WeightGate is the only way to obtain a readable handle on a weight file.
//
// It enforces both halves of SC-011 structurally rather than by convention: a
// caller cannot reach the bytes without having passed an allowlisted source and
// a verification against the catalogue's recorded expectation. A gate that
// merely offered a Verify method callers were trusted to call would be one
// forgotten call away from loading an unverified file.
type WeightGate struct {
	allowlist SourceAllowlist
}

// NewWeightGate builds a gate admitting only sources on allowlist.
func NewWeightGate(allowlist SourceAllowlist) WeightGate {
	return WeightGate{allowlist: allowlist}
}

// Authorise reports whether source may be obtained from at all, for callers that
// must decide before a download starts rather than after.
func (g WeightGate) Authorise(source string) error { return g.allowlist.Authorise(source) }

// OpenWeights returns a handle on the verified weight file at path, obtained
// from source, or an error naming which of the two guarantees was not met.
//
// Verification reads the same open handle that is returned, so the file cannot
// be replaced between being verified and being read.
func (g WeightGate) OpenWeights(source, path string, want IntegrityExpectation) (io.ReadCloser, error) {
	if err := g.allowlist.Authorise(source); err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreadableWeights, err)
	}
	if err := verifyOpenFile(file, want); err != nil {
		file.Close()
		return nil, fmt.Errorf("weights from %q: %w", source, err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		file.Close()
		return nil, fmt.Errorf("%w: %v", ErrUnreadableWeights, err)
	}
	return file, nil
}
