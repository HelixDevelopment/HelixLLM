package discovery

// Trust is the security core of discovery. Two rules govern everything here,
// and they are separate rules:
//
//   - FR-024: an instance beyond this host is trusted as a model source ONLY
//     after it proves it holds the pre-shared secret. Failing that, its
//     advertised models are not offered to anyone.
//   - FR-025: prompt content, file content and credentials are never
//     transmitted to an instance that has not passed FR-024 — so a machine that
//     merely appears on the network and advertises models cannot collect user
//     data by doing so.
//
// The second does not follow from the first. An implementation can mark an
// instance untrusted and still have posted the user's prompt to it, which is
// why [Discoverer.Send] refuses BEFORE it builds a request body, and why the
// package's tests assert on the bytes a hostile host received rather than on
// the flag this package set.
//
// The proof is a challenge-response rather than a bearer token, and that choice
// is load-bearing: sending the secret to an unverified endpoint to "log in"
// would hand the credential to precisely the hostile advertiser FR-025 exists
// to defend against. We send a fresh random nonce; the instance returns
// HMAC-SHA256(secret, nonce); we compare in constant time. The secret itself
// never leaves this process, in either direction.

import (
	"bufio"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
)

// Errors reported by the trust layer. None of them ever carries the secret, or
// any part of it: an error is a string that gets logged, wrapped, printed and
// shipped to a bug tracker, so a credential in one is a credential published.
var (
	// ErrUntrusted means the instance did not prove it holds the shared secret.
	ErrUntrusted = errors.New("discovery: instance is not authenticated")
	// ErrNoSecret means no pre-shared secret is configured.
	ErrNoSecret = errors.New("discovery: no pre-shared secret configured")
	// ErrSecretTooShort means the configured secret is not long enough to be
	// worth calling a secret.
	ErrSecretTooShort = errors.New("discovery: pre-shared secret is too short")
	// ErrSecretNotSerialisable is returned by every serialisation entry point on
	// Secret, so a secret cannot be written into an exported configuration by a
	// caller that simply marshalled the struct it happened to sit in.
	ErrSecretNotSerialisable = errors.New("discovery: a pre-shared secret must never be serialised")
)

// Redacted is what a Secret renders as, everywhere, always.
const Redacted = "[REDACTED]"

// SecretEnvVar names the environment variable, or .env key, carrying the
// pre-shared secret (FR-020, FR-024).
const SecretEnvVar = "HELIXLLM_DISCOVERY_SECRET"

// MinSecretLength is the shortest accepted secret, in bytes. A pre-shared
// secret guards which machines may receive a user's prompts; a value short
// enough to be guessed is not a smaller version of that protection, it is the
// absence of it.
const MinSecretLength = 16

// Secret is the pre-shared secret, held so that the ordinary ways a value
// escapes a process are closed by construction rather than by discipline.
//
// String, GoString, MarshalText and MarshalJSON are all overridden: fmt verbs,
// %#v dumps, encoding/json and anything built on TextMarshaler see the
// redaction or an error, never the bytes. slog.LogValuer covers structured
// logging. This is why the "never logged" rule can be a property of the type
// instead of a rule every call site has to remember.
type Secret struct {
	value []byte
}

// NewSecret validates and wraps a secret. The rejected value is never echoed.
func NewSecret(s string) (Secret, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return Secret{}, ErrNoSecret
	}
	if len(trimmed) < MinSecretLength {
		return Secret{}, fmt.Errorf("%w: need at least %d characters, got %d",
			ErrSecretTooShort, MinSecretLength, len(trimmed))
	}
	return Secret{value: []byte(trimmed)}, nil
}

// Empty reports whether no secret is held.
func (s Secret) Empty() bool { return len(s.value) == 0 }

// Equal compares two secrets in constant time.
func (s Secret) Equal(other Secret) bool {
	return subtle.ConstantTimeCompare(s.value, other.value) == 1
}

// String renders the redaction. It exists so %v and %s cannot leak.
func (s Secret) String() string { return Redacted }

// GoString renders the redaction under %#v, which is the verb a debugging
// print reaches for and the one a plain String method does not cover.
func (s Secret) GoString() string { return Redacted }

// LogValue renders the redaction for log/slog.
func (s Secret) LogValue() slog.Value { return slog.StringValue(Redacted) }

// MarshalText refuses. Returning an error rather than the redaction is
// deliberate: a redacted secret written into a configuration file is a
// configuration that silently does not work, whereas a refusal is a loud
// failure at the moment someone tries to export it.
func (s Secret) MarshalText() ([]byte, error) { return nil, ErrSecretNotSerialisable }

// MarshalJSON refuses, for the same reason, and makes any struct that embeds a
// Secret unmarshallable into an exported configuration.
func (s Secret) MarshalJSON() ([]byte, error) { return nil, ErrSecretNotSerialisable }

// LoadSecret reads the secret from the environment, falling back to the given
// environment files in order (FR-020).
//
// lookup is injected rather than calling os.LookupEnv directly so the loader is
// testable without mutating the process environment — which, in a package whose
// tests run in parallel with everything else in the build, would be a race as
// well as a leak.
func LoadSecret(lookup func(string) (string, bool), envFiles ...string) (Secret, error) {
	if lookup != nil {
		if raw, ok := lookup(SecretEnvVar); ok && strings.TrimSpace(raw) != "" {
			return NewSecret(raw)
		}
	}
	for _, path := range envFiles {
		values, err := parseEnvFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return Secret{}, err
		}
		if raw, ok := values[SecretEnvVar]; ok && strings.TrimSpace(raw) != "" {
			return NewSecret(raw)
		}
	}
	return Secret{}, fmt.Errorf("%w: set %s in the environment or an environment file",
		ErrNoSecret, SecretEnvVar)
}

// parseEnvFile reads KEY=value lines, tolerating comments, blank lines, a
// leading `export`, and single or double quotes.
//
// Parse errors name the file and the line NUMBER, never the line: the whole
// point of the file is that it holds credentials, so echoing its content into
// an error would defeat it.
func parseEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path) // #nosec G304 -- path is operator-configured, by design
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	values := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for line := 1; scanner.Scan(); line++ {
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		text = strings.TrimPrefix(text, "export ")
		key, raw, found := strings.Cut(text, "=")
		if !found {
			return nil, fmt.Errorf("discovery: %s line %d: expected KEY=value", path, line)
		}
		values[strings.TrimSpace(key)] = unquote(strings.TrimSpace(raw))
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("discovery: reading %s: %w", path, err)
	}
	return values, nil
}

func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// NonceLength is the size of the challenge, in bytes. 32 bytes from a CSPRNG
// makes replay of a captured proof infeasible.
const NonceLength = 32

// newNonce draws a fresh challenge.
func newNonce() ([]byte, error) {
	nonce := make([]byte, NonceLength)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("discovery: generating a challenge: %w", err)
	}
	return nonce, nil
}

// Proof is HMAC-SHA256(secret, nonce), hex-encoded — what a holder of the
// secret returns to answer a challenge. It is exported because a serving
// instance in this project computes the same value; it reveals nothing about
// the secret to anyone who does not already hold it.
func Proof(secret Secret, nonce []byte) string {
	mac := hmac.New(sha256.New, secret.value)
	mac.Write(nonce)
	return hex.EncodeToString(mac.Sum(nil))
}

// Verify checks a presented proof against the expected one for this nonce.
//
// The comparison is subtle.ConstantTimeCompare, not ==, because == returns at
// the first differing byte and so leaks, through timing, how much of a guess
// was right — which is enough to reconstruct a valid proof one byte at a time.
// A malformed or wrong-length proof is compared against the expectation anyway
// so that a caller cannot distinguish "not hex" from "wrong" by how long the
// answer took.
func Verify(secret Secret, nonce []byte, presented string) error {
	if secret.Empty() {
		return ErrNoSecret
	}
	expected := Proof(secret, nonce)
	presentedBytes, err := hex.DecodeString(presented)
	if err != nil {
		presentedBytes = nil
	}
	expectedBytes, _ := hex.DecodeString(expected)
	if subtle.ConstantTimeCompare(presentedBytes, expectedBytes) != 1 {
		return fmt.Errorf("%w: the instance did not answer the challenge correctly", ErrUntrusted)
	}
	return nil
}

// hexString renders bytes for the wire.
func hexString(b []byte) string { return hex.EncodeToString(b) }
