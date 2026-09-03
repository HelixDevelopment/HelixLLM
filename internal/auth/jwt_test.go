package auth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// testSecret is 34 bytes — over MinSecretBytes, and never a deployed value.
const testSecret = "unit-test-hs256-signing-key-32byt!"

func newTestVerifier(t *testing.T) *Verifier {
	t.Helper()
	v, err := New(testSecret, time.Hour)
	if err != nil {
		t.Fatalf("New(valid secret): unexpected error: %v", err)
	}
	if !v.Enabled() {
		t.Fatal("New(valid secret): verifier reports disabled")
	}
	return v
}

// sign mints a token with arbitrary claims and algorithm, bypassing Issue, so
// the verifier can be attacked with inputs Issue would never produce.
func sign(t *testing.T, method jwt.SigningMethod, key interface{}, claims jwt.Claims) string {
	t.Helper()
	s, err := jwt.NewWithClaims(method, claims).SignedString(key)
	if err != nil {
		t.Fatalf("signing test token: %v", err)
	}
	return s
}

func goodClaims() jwt.RegisteredClaims {
	now := time.Now()
	return jwt.RegisteredClaims{
		Issuer:    Issuer,
		Subject:   "test-principal",
		Audience:  jwt.ClaimStrings{Audience},
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
	}
}

// TestIssueVerifyRoundTrip is the baseline: the credential this package exists
// to provide actually works. Everything else in this file is an attempt to
// break it.
func TestIssueVerifyRoundTrip(t *testing.T) {
	v := newTestVerifier(t)

	token, err := v.Issue("client-alpha")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if token == "" {
		t.Fatal("Issue returned an empty token")
	}
	if strings.Count(token, ".") != 2 {
		t.Fatalf("Issue returned something that is not a JWS compact token: %q", token)
	}

	subject, err := v.Verify(token)
	if err != nil {
		t.Fatalf("Verify(own token): %v", err)
	}
	if subject != "client-alpha" {
		t.Errorf("Verify subject = %q, want %q", subject, "client-alpha")
	}
}

// TestVerifyRejects is the security core of this package.
//
// A JWT implementation tested only on its happy path is a bluff: the happy
// path is what the library does for you, and every defect that matters is a
// forged, stale, or misdirected token being ACCEPTED. Each case below is a
// token a real attacker can construct, and each MUST be refused.
func TestVerifyRejects(t *testing.T) {
	v := newTestVerifier(t)
	hs256 := jwt.SigningMethodHS256
	key := []byte(testSecret)

	futureClaims := func(mutate func(*jwt.RegisteredClaims)) jwt.RegisteredClaims {
		c := goodClaims()
		mutate(&c)
		return c
	}

	cases := []struct {
		name    string
		token   string
		wantErr error
		why     string
	}{
		{
			name:    "wrong signing key",
			token:   sign(t, hs256, []byte("a-different-32-byte-signing-key!!!"), goodClaims()),
			wantErr: ErrTokenInvalid,
			why:     "anyone could mint tokens without knowing the secret",
		},
		{
			name: "expired",
			token: sign(t, hs256, key, futureClaims(func(c *jwt.RegisteredClaims) {
				c.IssuedAt = jwt.NewNumericDate(time.Now().Add(-2 * time.Hour))
				c.NotBefore = c.IssuedAt
				c.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Hour))
			})),
			wantErr: ErrTokenExpired,
			why:     "a leaked token would be valid forever",
		},
		{
			name:    "alg=none (signature stripped)",
			token:   sign(t, jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, goodClaims()),
			wantErr: ErrTokenInvalid,
			why:     "the classic JWT forgery: declare no algorithm and send no signature",
		},
		{
			name:    "different HMAC algorithm than the one allowed",
			token:   sign(t, jwt.SigningMethodHS512, key, goodClaims()),
			wantErr: ErrTokenInvalid,
			why:     "the algorithm allowlist must be an allowlist, not a suggestion",
		},
		{
			name: "audience is another service",
			token: sign(t, hs256, key, futureClaims(func(c *jwt.RegisteredClaims) {
				c.Audience = jwt.ClaimStrings{"some-other-service"}
			})),
			wantErr: ErrTokenInvalid,
			why:     "a token minted for a sibling service that shares this secret would be accepted here",
		},
		{
			name: "issuer is another service",
			token: sign(t, hs256, key, futureClaims(func(c *jwt.RegisteredClaims) {
				c.Issuer = "some-other-service"
			})),
			wantErr: ErrTokenInvalid,
			why:     "same as audience, from the minting side",
		},
		{
			name: "no exp claim at all",
			token: sign(t, hs256, key, futureClaims(func(c *jwt.RegisteredClaims) {
				c.ExpiresAt = nil
			})),
			wantErr: ErrTokenInvalid,
			why:     "an absent exp is not 'no opinion about expiry', it is a token valid forever",
		},
		{
			name: "no sub claim",
			token: sign(t, hs256, key, futureClaims(func(c *jwt.RegisteredClaims) {
				c.Subject = ""
			})),
			wantErr: ErrTokenInvalid,
			why:     "a token naming no principal cannot authenticate one",
		},
		{
			name: "not valid yet (nbf in the future)",
			token: sign(t, hs256, key, futureClaims(func(c *jwt.RegisteredClaims) {
				c.NotBefore = jwt.NewNumericDate(time.Now().Add(time.Hour))
			})),
			wantErr: ErrTokenInvalid,
			why:     "nbf must be honoured or pre-dated tokens activate early",
		},
		{
			name: "issued in the future",
			token: sign(t, hs256, key, futureClaims(func(c *jwt.RegisteredClaims) {
				c.IssuedAt = jwt.NewNumericDate(time.Now().Add(2 * time.Hour))
			})),
			wantErr: ErrTokenInvalid,
			why:     "a future iat signals a forged or clock-broken minter",
		},
		{
			name:    "empty string",
			token:   "",
			wantErr: ErrTokenInvalid,
			why:     "an absent credential must not parse as a valid one",
		},
		{
			name:    "not a token at all",
			token:   "this-is-not-a-jwt",
			wantErr: ErrTokenInvalid,
			why:     "malformed input must be refused, not panic",
		},
		{
			name:    "header and payload only, no signature segment",
			token:   "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJhZG1pbiJ9",
			wantErr: ErrTokenInvalid,
			why:     "a truncated token must not be treated as unsigned-but-fine",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			subject, err := v.Verify(tc.token)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Verify accepted or misclassified a token it must refuse.\n"+
					"  got err  = %v\n  want err = %v\n  subject  = %q\n"+
					"  why this matters: %s", err, tc.wantErr, subject, tc.why)
			}
			if subject != "" {
				t.Errorf("Verify returned subject %q alongside an error; a rejected token must name nobody", subject)
			}
		})
	}
}

// TestVerifyRejectsTamperedPayload proves the signature actually covers the
// claims: re-encoding the payload of a legitimately-signed token, keeping its
// original signature, must fail.
func TestVerifyRejectsTamperedPayload(t *testing.T) {
	v := newTestVerifier(t)

	original, err := v.Issue("low-privilege-client")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	parts := strings.Split(original, ".")
	if len(parts) != 3 {
		t.Fatalf("expected 3 token segments, got %d", len(parts))
	}

	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decoding payload: %v", err)
	}
	var claims map[string]interface{}
	if err := json.Unmarshal(raw, &claims); err != nil {
		t.Fatalf("unmarshalling payload: %v", err)
	}
	if claims["sub"] != "low-privilege-client" {
		t.Fatalf("sub = %v, want the subject Issue was given", claims["sub"])
	}
	claims["sub"] = "administrator"
	edited, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshalling edited payload: %v", err)
	}

	tampered := fmt.Sprintf("%s.%s.%s",
		parts[0], base64.RawURLEncoding.EncodeToString(edited), parts[2])

	subject, err := v.Verify(tampered)
	if !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("a token whose payload was edited to sub=administrator was accepted: "+
			"err=%v subject=%q", err, subject)
	}
}

// TestDisabledVerifier: nil is the "JWT auth is off" value and must be inert
// AND safe. Inert because a disabled verifier must not authenticate anybody —
// including with a token it would otherwise have accepted. Safe because
// callers ask Enabled() on a possibly-nil pointer without a nil check, so a
// method on nil must not panic.
func TestDisabledVerifier(t *testing.T) {
	enabled := newTestVerifier(t)
	validToken, err := enabled.Issue("client")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	off, err := New("", time.Hour)
	if err != nil {
		t.Fatalf("New(\"\"): unexpected error: %v", err)
	}
	if off != nil {
		t.Fatalf("New(\"\") returned a non-nil verifier; the off-switch must produce nil")
	}

	for _, v := range []*Verifier{nil, off} {
		if v.Enabled() {
			t.Error("a nil verifier reports Enabled()")
		}
		if got := v.TTL(); got != 0 {
			t.Errorf("nil verifier TTL = %v, want 0", got)
		}
		if _, err := v.Issue("client"); !errors.Is(err, ErrTokenInvalid) {
			t.Errorf("nil verifier Issue err = %v, want ErrTokenInvalid", err)
		}
		if subject, err := v.Verify(validToken); err == nil {
			t.Errorf("a DISABLED verifier accepted a validly-signed token as %q; "+
				"turning JWT off must refuse every token, not fall back to trusting them", subject)
		}
	}
}

// TestNewRejectsWeakSecret pins the RFC 7518 §3.2 key-size floor. A secret one
// byte short is refused; one exactly at the floor is accepted.
func TestNewRejectsWeakSecret(t *testing.T) {
	if MinSecretBytes != 32 {
		t.Fatalf("MinSecretBytes = %d, want 32 (RFC 7518 §3.2, 256-bit hash output)", MinSecretBytes)
	}

	tooShort := strings.Repeat("k", MinSecretBytes-1)
	v, err := New(tooShort, time.Hour)
	if !errors.Is(err, ErrWeakSecret) {
		t.Fatalf("New(%d-byte secret) err = %v, want ErrWeakSecret", len(tooShort), err)
	}
	if v != nil {
		t.Error("New returned a usable verifier alongside ErrWeakSecret")
	}
	if strings.Contains(err.Error(), tooShort) {
		t.Error("the weak-secret error quotes the secret; error strings reach logs")
	}

	atFloor := strings.Repeat("k", MinSecretBytes)
	if _, err := New(atFloor, time.Hour); err != nil {
		t.Fatalf("New(%d-byte secret) = %v, want acceptance at exactly the floor", MinSecretBytes, err)
	}
}

// TestNonPositiveTTLFallsBack: a misconfigured TTL must not mint tokens that
// are born expired.
func TestNonPositiveTTLFallsBack(t *testing.T) {
	for _, ttl := range []time.Duration{0, -time.Hour} {
		v, err := New(testSecret, ttl)
		if err != nil {
			t.Fatalf("New(ttl=%v): %v", ttl, err)
		}
		if v.TTL() != DefaultTTL {
			t.Errorf("New(ttl=%v).TTL() = %v, want the %v default", ttl, v.TTL(), DefaultTTL)
		}
		token, err := v.Issue("client")
		if err != nil {
			t.Fatalf("Issue with ttl=%v: %v", ttl, err)
		}
		if _, err := v.Verify(token); err != nil {
			t.Errorf("a token minted with ttl=%v did not verify: %v", ttl, err)
		}
	}
}

// TestIssueRejectsEmptySubject: an unnamed principal is a programming error,
// not a token to mint. Verify already rejects a subject-less token, so minting
// one would produce a credential guaranteed to fail.
func TestIssueRejectsEmptySubject(t *testing.T) {
	v := newTestVerifier(t)
	if _, err := v.Issue(""); err == nil {
		t.Fatal("Issue(\"\") succeeded; a token naming no principal must not be minted")
	}
}

// TestSubjectForAPIKeyDoesNotLeakTheKey is the point of that function. The
// subject is readable in the token payload by anything that handles the token,
// so it must not be derivable back to the long-lived API key.
func TestSubjectForAPIKeyDoesNotLeakTheKey(t *testing.T) {
	const key = "sk-a-real-looking-secret-api-key"
	subject := SubjectForAPIKey(key)

	if strings.Contains(subject, key) {
		t.Fatalf("subject %q contains the API key verbatim", subject)
	}
	// Also catch a partial leak: no run of 6+ key characters may appear.
	for i := 0; i+6 <= len(key); i++ {
		if strings.Contains(subject, key[i:i+6]) {
			t.Fatalf("subject %q contains the key fragment %q", subject, key[i:i+6])
		}
	}
	if !strings.HasPrefix(subject, "apikey:") {
		t.Errorf("subject %q lacks the apikey: prefix that marks its provenance", subject)
	}
	if subject != SubjectForAPIKey(key) {
		t.Error("SubjectForAPIKey is not stable across calls; audit logs would not correlate")
	}
	if subject == SubjectForAPIKey(key+"x") {
		t.Error("two different keys map to one subject; callers would be indistinguishable")
	}
}

// TestVerifyErrorsNeverEchoTheToken guards the property that makes it safe to
// put these errors in a 401 body and in logs.
func TestVerifyErrorsNeverEchoTheToken(t *testing.T) {
	v := newTestVerifier(t)
	forged := sign(t, jwt.SigningMethodHS256, []byte("a-different-32-byte-signing-key!!!"), goodClaims())

	_, err := v.Verify(forged)
	if err == nil {
		t.Fatal("forged token accepted")
	}
	if strings.Contains(err.Error(), forged) {
		t.Error("the error echoes the whole rejected token")
	}
	for _, seg := range strings.Split(forged, ".") {
		if len(seg) > 8 && strings.Contains(err.Error(), seg) {
			t.Errorf("the error echoes a token segment: %q", seg)
		}
	}
	if strings.Contains(err.Error(), testSecret) {
		t.Error("the error echoes the signing secret")
	}
}
