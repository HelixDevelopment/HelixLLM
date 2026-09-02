package discovery_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/discovery"
)

// Canaries. Every one of these strings is chosen so that a single substring
// search over everything a foreign host received answers a security question
// exactly, with no interpretation: if the byte sequence is on the wire, the
// thing it stands for was transmitted.
const (
	sharedSecret   = "SECRET-CANARY-2f8c41d9e77b0a6534ee"
	promptCanary   = "PROMPT-CANARY-what-is-in-my-private-repository"
	fileCanary     = "FILE-CANARY-contents-of-a-source-file-the-user-opened"
	credCanary     = "CREDENTIAL-CANARY-users-upstream-api-key"
	servingHost    = "gpu-01"
	servedModel    = "llama3"
	servedVariant  = "8b"
	attestNonceLen = 32
)

// recordedRequest is one HTTP request a test instance received, captured whole:
// line, query, every header value, and the complete body. Wire-cleanliness
// assertions read this and nothing else, so they describe what was actually
// transmitted rather than what the caller intended to transmit.
type recordedRequest struct {
	Method   string
	Path     string
	RawQuery string
	Header   http.Header
	Body     []byte
}

// flatten renders the whole request as one searchable string.
func (r recordedRequest) flatten() string {
	var b strings.Builder
	b.WriteString(r.Method)
	b.WriteByte(' ')
	b.WriteString(r.Path)
	b.WriteByte('?')
	b.WriteString(r.RawQuery)
	b.WriteByte('\n')
	for name, values := range r.Header {
		for _, v := range values {
			b.WriteString(name)
			b.WriteString(": ")
			b.WriteString(v)
			b.WriteByte('\n')
		}
	}
	b.Write(r.Body)
	return b.String()
}

// instanceServer is a real HTTP server standing in for a serving instance on
// the network. secret is what it can present: when empty it cannot answer the
// challenge, which is precisely the untrusted case FR-024/FR-025 govern.
type instanceServer struct {
	mu       sync.Mutex
	requests []recordedRequest

	secret string
	server *httptest.Server
}

func newInstanceServer(t *testing.T, secret string) *instanceServer {
	t.Helper()
	s := &instanceServer{secret: secret}
	s.server = httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(s.server.Close)
	return s
}

func (s *instanceServer) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	s.mu.Lock()
	s.requests = append(s.requests, recordedRequest{
		Method:   r.Method,
		Path:     r.URL.Path,
		RawQuery: r.URL.RawQuery,
		Header:   r.Header.Clone(),
		Body:     body,
	})
	s.mu.Unlock()

	if r.URL.Path != discovery.AttestPath {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
		return
	}

	nonce, err := hex.DecodeString(r.URL.Query().Get(discovery.NonceParam))
	if err != nil {
		http.Error(w, "bad nonce", http.StatusBadRequest)
		return
	}

	proof := "00000000000000000000000000000000000000000000000000000000deadbeef"
	if s.secret != "" {
		mac := hmac.New(sha256.New, []byte(s.secret))
		mac.Write(nonce)
		proof = hex.EncodeToString(mac.Sum(nil))
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"host": servingHost,
		"models": []map[string]string{
			{"name": servedModel, "variant": servedVariant, "family": "text"},
		},
		"proof": proof,
	})
}

func (s *instanceServer) traffic() []recordedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]recordedRequest, len(s.requests))
	copy(out, s.requests)
	return out
}

func (s *instanceServer) url() string { return s.server.URL }

// assertWireClean fails if any byte a foreign host received contains one of the
// forbidden markers. This is the assertion the whole security surface rests on:
// it inspects transmitted bytes, not a boolean the implementation set itself.
func assertWireClean(t *testing.T, srv *instanceServer, forbidden map[string]string) {
	t.Helper()
	traffic := srv.traffic()
	for i, req := range traffic {
		flat := req.flatten()
		for marker, what := range forbidden {
			if strings.Contains(flat, marker) {
				t.Fatalf("request %d (%s %s) carried %s to the instance:\n%s",
					i, req.Method, req.Path, what, flat)
			}
		}
	}
	t.Logf("wire clean: %d request(s) inspected, none carried prompt, file, credential or secret bytes", len(traffic))
}

func mustSecret(t *testing.T) discovery.Secret {
	t.Helper()
	s, err := discovery.NewSecret(sharedSecret)
	if err != nil {
		t.Fatalf("NewSecret: %v", err)
	}
	return s
}

func remoteOptions(t *testing.T, endpoint string) discovery.Options {
	t.Helper()
	modes := discovery.NoModes()
	modes.Enable(discovery.Remote)
	return discovery.Options{
		Modes:           modes,
		Secret:          mustSecret(t),
		RemoteEndpoints: []string{endpoint},
		ProbeTimeout:    5 * time.Second,
	}
}

// ---------------------------------------------------------------------------
// T056 negative case — the most important test in this package (FR-024/FR-025).
// ---------------------------------------------------------------------------

// TestUnauthenticatedInstanceIsNeverTrustedAndNeverReceivesContent proves three
// separate things, because the first alone is not enough: an implementation
// could correctly report trusted == false and still have posted the prompt.
//
//  1. an instance that cannot present the shared secret is not trusted,
//  2. its advertised models are not offered to users, and
//  3. NOTHING of the user's — prompt, file content or credentials — and not the
//     secret itself ever reached it, verified against the bytes it received.
func TestUnauthenticatedInstanceIsNeverTrustedAndNeverReceivesContent(t *testing.T) {
	// A host that merely appears on the network advertising models, but cannot
	// present the pre-shared secret.
	hostile := newInstanceServer(t, "")

	d, err := discovery.New(remoteOptions(t, hostile.url()))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	instances, err := d.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(instances) != 1 {
		t.Fatalf("expected the endpoint to be reported once, got %d instances", len(instances))
	}
	inst := instances[0]

	if inst.Trusted {
		t.Errorf("instance that cannot present the secret was trusted (FR-024)")
	}
	if len(inst.ServedOptions) != 0 {
		t.Errorf("untrusted instance had %d models offered: %v (FR-024)", len(inst.ServedOptions), inst.ServedOptions)
	}
	if len(d.Available(instances)) != 0 {
		t.Errorf("untrusted instance was exported as available")
	}

	// Attempting to use it must be refused before anything is transmitted. The
	// error is captured rather than asserted here so that the wire assertion
	// below runs FIRST: if a defect ever lets content out, the failure this
	// test reports should name the bytes that escaped, not the return value.
	_, sendErr := d.Send(context.Background(), inst, discovery.RequestContent{
		Prompt:      promptCanary,
		Files:       []discovery.FileContent{{Path: "main.go", Content: fileCanary}},
		Credentials: map[string]string{"upstream": credCanary},
	})

	// The load-bearing assertion: inspect what the hostile host actually got.
	assertWireClean(t, hostile, map[string]string{
		promptCanary: "the user's prompt",
		fileCanary:   "file content",
		credCanary:   "a credential",
		sharedSecret: "the pre-shared secret itself",
	})

	if !errors.Is(sendErr, discovery.ErrUntrusted) {
		t.Fatalf("Send to an untrusted instance: got %v, want ErrUntrusted", sendErr)
	}

	// And it saw nothing but the challenge probe — no request endpoint at all.
	for _, req := range hostile.traffic() {
		if req.Path != discovery.AttestPath {
			t.Errorf("untrusted instance received a %s %s request; only %s is permitted",
				req.Method, req.Path, discovery.AttestPath)
		}
	}
}

// TestAuthenticatedInstanceReceivesContentButNeverTheSecret is the positive
// counterpart: trust is reachable, and even then the raw secret stays home —
// only a proof over a fresh nonce goes out.
func TestAuthenticatedInstanceReceivesContentButNeverTheSecret(t *testing.T) {
	honest := newInstanceServer(t, sharedSecret)

	d, err := discovery.New(remoteOptions(t, honest.url()))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	instances, err := d.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	inst := instances[0]
	if !inst.Trusted {
		t.Fatalf("instance presenting the correct secret was not trusted: %+v", inst.Health)
	}
	if len(inst.ServedOptions) != 1 {
		t.Fatalf("expected 1 served model, got %d", len(inst.ServedOptions))
	}

	if _, err := d.Send(context.Background(), inst, discovery.RequestContent{Prompt: promptCanary}); err != nil {
		t.Fatalf("Send to a trusted instance: %v", err)
	}

	// The prompt is expected to have been transmitted here — the secret is not.
	assertWireClean(t, honest, map[string]string{sharedSecret: "the pre-shared secret itself"})

	var sawRequest bool
	for _, req := range honest.traffic() {
		if req.Path == discovery.RequestPath {
			sawRequest = true
			if !strings.Contains(string(req.Body), promptCanary) {
				t.Errorf("trusted instance did not receive the prompt it was sent")
			}
		}
	}
	if !sawRequest {
		t.Errorf("no request reached the trusted instance")
	}
}

// TestDiscoveredModelIsLabelledWithItsServingHost covers FR-023.
func TestDiscoveredModelIsLabelledWithItsServingHost(t *testing.T) {
	honest := newInstanceServer(t, sharedSecret)
	d, err := discovery.New(remoteOptions(t, honest.url()))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	instances, err := d.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	got := instances[0].ServedOptions[0]
	if got.Identity.Host != servingHost {
		t.Errorf("model host label = %q, want %q (FR-023)", got.Identity.Host, servingHost)
	}
	if got.Identity.Model != servedModel || got.Identity.Variant != servedVariant {
		t.Errorf("model identity = %+v, want model %q variant %q", got.Identity, servedModel, servedVariant)
	}
	want := "helixllm/" + servingHost + "/" + servedModel + ":" + servedVariant
	if got.Identity.String() != want {
		t.Errorf("identity string = %q, want %q", got.Identity.String(), want)
	}
	if got.ServingHost != servingHost {
		t.Errorf("ServedModel.ServingHost = %q, want %q (FR-023)", got.ServingHost, servingHost)
	}
}

// ---------------------------------------------------------------------------
// T065 / SC-007 — a disabled mode emits NO discovery traffic.
// ---------------------------------------------------------------------------

// countingListener accepts real TCP connections and counts them. A configuration
// flag cannot satisfy this test: only the absence of a connection can.
type countingListener struct {
	mu    sync.Mutex
	conns int
	ln    net.Listener
	done  chan struct{}
}

func newCountingListener(t *testing.T) *countingListener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	c := &countingListener{ln: ln, done: make(chan struct{})}
	go func() {
		defer close(c.done)
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			c.mu.Lock()
			c.conns++
			c.mu.Unlock()
			_ = conn.Close()
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		<-c.done
	})
	return c
}

func (c *countingListener) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conns
}

func (c *countingListener) endpoint() string { return "http://" + c.ln.Addr().String() }

func TestDisabledModeEmitsNoDiscoveryTraffic(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode discovery.Reachability
		opts func(endpoint string) discovery.Options
	}{
		{
			name: "local-network",
			mode: discovery.LocalNetwork,
			opts: func(e string) discovery.Options {
				return discovery.Options{LocalNetworkEndpoints: []string{e}}
			},
		},
		{
			name: "remote",
			mode: discovery.Remote,
			opts: func(e string) discovery.Options {
				return discovery.Options{RemoteEndpoints: []string{e}}
			},
		},
		{
			name: "local-host",
			mode: discovery.LocalHost,
			opts: func(e string) discovery.Options {
				return discovery.Options{LocalHostEndpoints: []string{e}}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			listener := newCountingListener(t)

			// Mode DISABLED: run discovery, then observe the wire.
			off := tc.opts(listener.endpoint())
			off.Modes = discovery.NoModes()
			off.Secret = mustSecret(t)
			off.ProbeTimeout = 5 * time.Second
			d, err := discovery.New(off)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if _, err := d.Discover(context.Background()); err != nil {
				t.Fatalf("Discover with every mode disabled: %v", err)
			}
			if got := listener.count(); got != 0 {
				t.Fatalf("%s disabled but %d connection(s) reached the endpoint (SC-007)", tc.name, got)
			}
			t.Logf("%s disabled: 0 connections observed on the listener", tc.name)

			// Mode ENABLED: the same call must now actually reach the wire,
			// otherwise the silence above would prove nothing.
			on := tc.opts(listener.endpoint())
			on.Modes = discovery.NoModes()
			on.Modes.Enable(tc.mode)
			on.Secret = mustSecret(t)
			on.ProbeTimeout = 5 * time.Second
			d2, err := discovery.New(on)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if _, err := d2.Discover(context.Background()); err != nil {
				t.Fatalf("Discover with %s enabled: %v", tc.name, err)
			}
			if got := listener.count(); got == 0 {
				t.Fatalf("%s enabled but no connection reached the endpoint — the silence test would be vacuous", tc.name)
			}
			t.Logf("%s enabled: %d connection(s) observed — silence above is meaningful", tc.name, listener.count())
		})
	}
}

func TestModesAreIndependentlyDisableable(t *testing.T) {
	all := discovery.AllModes()
	for _, m := range []discovery.Reachability{discovery.LocalHost, discovery.LocalNetwork, discovery.Remote} {
		modes := all
		modes.Disable(m)
		if modes.Enabled(m) {
			t.Errorf("%s stayed enabled after Disable", m)
		}
		for _, other := range []discovery.Reachability{discovery.LocalHost, discovery.LocalNetwork, discovery.Remote} {
			if other == m {
				continue
			}
			if !modes.Enabled(other) {
				t.Errorf("disabling %s also disabled %s — modes are not independent (FR-022)", m, other)
			}
		}
	}
}

func TestModesFromEnvironment(t *testing.T) {
	env := map[string]string{
		"HELIXLLM_DISCOVERY_LOCAL_HOST":    "true",
		"HELIXLLM_DISCOVERY_LOCAL_NETWORK": "false",
		"HELIXLLM_DISCOVERY_REMOTE":        "0",
	}
	modes, err := discovery.ModesFromEnv(func(k string) (string, bool) { v, ok := env[k]; return v, ok })
	if err != nil {
		t.Fatalf("ModesFromEnv: %v", err)
	}
	if !modes.Enabled(discovery.LocalHost) {
		t.Errorf("local-host should be enabled")
	}
	if modes.Enabled(discovery.LocalNetwork) || modes.Enabled(discovery.Remote) {
		t.Errorf("local-network and remote should be disabled: %v", modes.List())
	}

	env["HELIXLLM_DISCOVERY_REMOTE"] = "perhaps"
	if _, err := discovery.ModesFromEnv(func(k string) (string, bool) { v, ok := env[k]; return v, ok }); err == nil {
		t.Errorf("an unparseable mode setting must be an error, not a guess")
	}
}

// ---------------------------------------------------------------------------
// T058 — secret handling.
// ---------------------------------------------------------------------------

func TestSecretRedactsAndRefusesSerialisation(t *testing.T) {
	s := mustSecret(t)

	for name, rendered := range map[string]string{
		"%v":     fmt.Sprintf("%v", s),
		"%s":     fmt.Sprintf("%s", s),
		"%#v":    fmt.Sprintf("%#v", s),
		"%+v":    fmt.Sprintf("%+v", s),
		"String": s.String(),
	} {
		if strings.Contains(rendered, sharedSecret) {
			t.Errorf("%s rendering leaked the secret: %s", name, rendered)
		}
		if !strings.Contains(rendered, discovery.Redacted) {
			t.Errorf("%s rendering = %q, want it to say %q", name, rendered, discovery.Redacted)
		}
	}

	// A secret must never be writable into an exported configuration.
	if b, err := json.Marshal(struct{ Secret discovery.Secret }{s}); err == nil {
		t.Errorf("marshalling a struct containing a Secret succeeded and produced %s; it must refuse", b)
	}
	if b, err := s.MarshalText(); err == nil {
		t.Errorf("MarshalText succeeded and produced %q; it must refuse", b)
	}
}

func TestSecretNeverAppearsInErrorsOrLogs(t *testing.T) {
	var logs strings.Builder
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	hostile := newInstanceServer(t, "")
	opts := remoteOptions(t, hostile.url())
	opts.Logger = logger
	// A second endpoint that refuses connections, to exercise the failure paths.
	opts.RemoteEndpoints = append(opts.RemoteEndpoints, "http://127.0.0.1:1")

	d, err := discovery.New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	instances, discoverErr := d.Discover(context.Background())

	collected := []error{discoverErr}
	for _, inst := range instances {
		_, err := d.Send(context.Background(), inst, discovery.RequestContent{Prompt: promptCanary})
		collected = append(collected, err)
	}
	// Also drive the loader's failure path.
	_, err = discovery.LoadSecret(func(string) (string, bool) { return "", false })
	collected = append(collected, err)
	_, err = discovery.NewSecret("short")
	collected = append(collected, err)

	for _, e := range collected {
		if e == nil {
			continue
		}
		if strings.Contains(e.Error(), sharedSecret) {
			t.Errorf("an error message leaked the secret: %v", e)
		}
	}
	if strings.Contains(logs.String(), sharedSecret) {
		t.Errorf("log output leaked the secret:\n%s", logs.String())
	}
	t.Logf("%d error(s) and %d bytes of log output inspected, no secret bytes present", len(collected), logs.Len())
}

func TestLoadSecretFromEnvironmentAndFile(t *testing.T) {
	got, err := discovery.LoadSecret(func(k string) (string, bool) {
		if k == discovery.SecretEnvVar {
			return sharedSecret, true
		}
		return "", false
	})
	if err != nil {
		t.Fatalf("LoadSecret from environment: %v", err)
	}
	if !got.Equal(mustSecret(t)) {
		t.Errorf("secret loaded from the environment does not match")
	}

	dir := t.TempDir()
	path := dir + "/.env"
	content := "# comment\nexport OTHER=x\n" + discovery.SecretEnvVar + "=\"" + sharedSecret + "\"\n"
	if err := writeFile(path, content); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	got, err = discovery.LoadSecret(func(string) (string, bool) { return "", false }, path)
	if err != nil {
		t.Fatalf("LoadSecret from file: %v", err)
	}
	if !got.Equal(mustSecret(t)) {
		t.Errorf("secret loaded from .env does not match")
	}

	if _, err := discovery.LoadSecret(func(string) (string, bool) { return "", false }); !errors.Is(err, discovery.ErrNoSecret) {
		t.Errorf("missing secret: got %v, want ErrNoSecret", err)
	}
}

func TestVerifyRejectsAWrongProof(t *testing.T) {
	s := mustSecret(t)
	nonce := make([]byte, attestNonceLen)
	for i := range nonce {
		nonce[i] = byte(i)
	}
	good := discovery.Proof(s, nonce)
	if err := discovery.Verify(s, nonce, good); err != nil {
		t.Fatalf("a correct proof was rejected: %v", err)
	}
	for name, bad := range map[string]string{
		"empty":       "",
		"not hex":     "zzzz",
		"wrong bytes": strings.Repeat("ab", sha256.Size),
		"truncated":   good[:len(good)-2],
		"other nonce": discovery.Proof(s, append(nonce, 0xff)),
	} {
		if err := discovery.Verify(s, nonce, bad); !errors.Is(err, discovery.ErrUntrusted) {
			t.Errorf("%s proof: got %v, want ErrUntrusted", name, err)
		}
	}
}

// TestRemoteModeWithoutASecretEmitsNoTraffic: a mode we could not authenticate
// is not probed at all — a host we cannot verify learns nothing, not even that
// we exist.
func TestRemoteModeWithoutASecretEmitsNoTraffic(t *testing.T) {
	listener := newCountingListener(t)
	modes := discovery.NoModes()
	modes.Enable(discovery.Remote)
	d, err := discovery.New(discovery.Options{
		Modes:           modes,
		RemoteEndpoints: []string{listener.endpoint()},
		ProbeTimeout:    5 * time.Second,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	instances, err := d.Discover(context.Background())
	if !errors.Is(err, discovery.ErrNoSecret) {
		t.Errorf("Discover without a secret: got %v, want ErrNoSecret", err)
	}
	if len(instances) != 0 {
		t.Errorf("expected no instances, got %d", len(instances))
	}
	if got := listener.count(); got != 0 {
		t.Errorf("%d connection(s) were made to an endpoint we cannot authenticate", got)
	}
}

func TestLocalNetworkSweepProbesEveryAddressInTheRange(t *testing.T) {
	honest := newInstanceServer(t, sharedSecret)
	host, port, err := net.SplitHostPort(strings.TrimPrefix(honest.url(), "http://"))
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	var portNum int
	if _, err := fmt.Sscanf(port, "%d", &portNum); err != nil {
		t.Fatalf("parse port: %v", err)
	}

	modes := discovery.NoModes()
	modes.Enable(discovery.LocalNetwork)
	d, err := discovery.New(discovery.Options{
		Modes:             modes,
		Secret:            mustSecret(t),
		LocalNetworkCIDRs: []string{host + "/32"},
		LocalNetworkPorts: []int{portNum},
		ProbeTimeout:      5 * time.Second,
		MaxSweepAddresses: 4,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	instances, err := d.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(instances) != 1 || !instances[0].Trusted {
		t.Fatalf("sweep did not find and authenticate the instance: %+v", instances)
	}
	if instances[0].Reachability != discovery.LocalNetwork {
		t.Errorf("reachability = %q, want %q", instances[0].Reachability, discovery.LocalNetwork)
	}
}

func TestSweepRefusesAnUnboundedRange(t *testing.T) {
	modes := discovery.NoModes()
	modes.Enable(discovery.LocalNetwork)
	listener := newCountingListener(t)
	d, err := discovery.New(discovery.Options{
		Modes:             modes,
		Secret:            mustSecret(t),
		LocalNetworkCIDRs: []string{"10.0.0.0/8"},
		LocalNetworkPorts: []int{80},
		MaxSweepAddresses: 16,
		ProbeTimeout:      time.Second,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := d.Discover(context.Background()); err == nil {
		t.Errorf("a sweep larger than the configured bound must be refused, not attempted")
	}
	if got := listener.count(); got != 0 {
		t.Errorf("refused sweep still made %d connection(s)", got)
	}
}
