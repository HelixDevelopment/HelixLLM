package discovery_test

// Relay attacks against the attestation challenge, and the loopback exemption.
//
// The existing T056 case proves that an instance which answers a challenge
// WRONGLY is refused. That is a weaker claim than it looks: a host holding no
// secret at all does not have to invent a proof, it can obtain a real one from
// a genuine instance and hand it back as its own. Everything in this file
// therefore relays a REAL proof, computed by a genuine holder of the secret
// over the very nonce we issued. A test whose hostile server returns garbage
// cannot distinguish an implementation that binds the proof to the endpoint it
// dialled from one that does not.
//
// The assertions are on the bytes a hostile host received, for the same reason
// T056's are: an implementation can set Trusted correctly and still have posted
// the prompt.

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/discovery"
)

// relayServer is a hostile host: it holds NO secret, records every byte it
// receives, and answers the attestation challenge by getting a genuine instance
// to answer it instead.
type relayServer struct {
	mu       sync.Mutex
	requests []recordedRequest

	// attest is how this host answers a challenge. It is a field rather than a
	// switch inside the handler so the two relay variants — redirect and active
	// proxy — differ only in this one function.
	attest func(w http.ResponseWriter, r *http.Request)

	server *httptest.Server
}

func newRelayServer(t *testing.T, attest func(w http.ResponseWriter, r *http.Request)) *relayServer {
	t.Helper()
	s := &relayServer{attest: attest}
	s.server = httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(s.server.Close)
	return s
}

func (s *relayServer) handle(w http.ResponseWriter, r *http.Request) {
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

	if r.URL.Path == discovery.AttestPath {
		s.attest(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

func (s *relayServer) traffic() []recordedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]recordedRequest, len(s.requests))
	copy(out, s.requests)
	return out
}

func (s *relayServer) url() string { return s.server.URL }

// assertRelayWireClean is assertWireClean for a relayServer: it reads the bytes
// the hostile host actually received, never a flag the implementation set.
func assertRelayWireClean(t *testing.T, srv *relayServer, forbidden map[string]string) {
	t.Helper()
	traffic := srv.traffic()
	for i, req := range traffic {
		flat := req.flatten()
		for marker, what := range forbidden {
			if strings.Contains(flat, marker) {
				t.Fatalf("request %d (%s %s) carried %s to the hostile host:\n%s",
					i, req.Method, req.Path, what, flat)
			}
		}
	}
	t.Logf("wire clean: %d request(s) inspected on the hostile host", len(traffic))
}

// sendCanaries posts everything FR-025 names to inst and returns the error, so
// the wire assertion can run before the error is judged.
func sendCanaries(t *testing.T, d *discovery.Discoverer, inst discovery.Instance) error {
	t.Helper()
	_, err := d.Send(context.Background(), inst, discovery.RequestContent{
		Prompt:      promptCanary,
		Files:       []discovery.FileContent{{Path: "main.go", Content: fileCanary}},
		Credentials: map[string]string{"upstream": credCanary},
	})
	return err
}

// ---------------------------------------------------------------------------
// SECURITY-2: proof relay. A host holding NO secret answers our challenge by
// having a genuine instance answer it.
// ---------------------------------------------------------------------------

// TestRedirectedAttestationIsNotAProofOfTheEndpointsIdentity is the redirect
// variant. The hostile host answers the probe with a 302 to a genuine instance;
// the HTTP client follows it, and the honest instance's proof comes back as the
// hostile endpoint's response.
//
// Nothing in the exchange ties the proof to the endpoint that was DIALLED, so
// without a fix the hostile endpoint is trusted, and the prompt, the file and
// the credential are posted to it.
func TestRedirectedAttestationIsNotAProofOfTheEndpointsIdentity(t *testing.T) {
	honest := newInstanceServer(t, sharedSecret)

	hostile := newRelayServer(t, func(w http.ResponseWriter, r *http.Request) {
		// The nonce is carried through unchanged: the honest instance answers
		// the very challenge we issued to the hostile endpoint.
		http.Redirect(w, r, honest.url()+discovery.AttestPath+"?"+r.URL.RawQuery, http.StatusFound)
	})

	d, err := discovery.New(remoteOptions(t, hostile.url()))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	instances, err := d.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(instances) != 1 {
		t.Fatalf("expected the endpoint to be reported once, got %d", len(instances))
	}
	inst := instances[0]

	sendErr := sendCanaries(t, d, inst)

	assertRelayWireClean(t, hostile, map[string]string{
		promptCanary: "the user's prompt",
		fileCanary:   "file content",
		credCanary:   "a credential",
		sharedSecret: "the pre-shared secret itself",
	})

	if inst.Trusted {
		t.Errorf("an endpoint that answered with a REDIRECT to a genuine instance was trusted: " +
			"the proof is not bound to the endpoint that was dialled (FR-024)")
	}
	if len(inst.ServedOptions) != 0 {
		t.Errorf("relaying endpoint had %d models read out of its answer: %v (FR-024)",
			len(inst.ServedOptions), inst.ServedOptions)
	}
	if sendErr == nil {
		t.Errorf("Send to a relaying endpoint succeeded; it must be refused (FR-025)")
	}
	for _, req := range hostile.traffic() {
		if req.Path != discovery.AttestPath {
			t.Errorf("relaying endpoint received a %s %s request; only %s is permitted",
				req.Method, req.Path, discovery.AttestPath)
		}
	}
}

// TestProxiedAttestationIsNotAProofOfTheEndpointsIdentity is the active-proxy
// variant, and the one a redirect refusal alone does not answer: the hostile
// host fetches the genuine attestation ITSELF, rewrites the host label and the
// model list to values of its choosing, and returns the honest proof as its
// own. From the client's side this is an ordinary 200 response.
//
// The rewritten host label is the second half of the harm: it collides with the
// real serving host, so an attacker-chosen model appears in a listing under a
// name the user recognises.
func TestProxiedAttestationIsNotAProofOfTheEndpointsIdentity(t *testing.T) {
	honest := newInstanceServer(t, sharedSecret)

	const attackerModel = "ATTACKER-CHOSEN-MODEL"

	hostile := newRelayServer(t, func(w http.ResponseWriter, r *http.Request) {
		upstream, err := http.Get(honest.url() + discovery.AttestPath + "?" + r.URL.RawQuery)
		if err != nil {
			http.Error(w, "upstream", http.StatusBadGateway)
			return
		}
		defer func() { _ = upstream.Body.Close() }()

		var att map[string]any
		if err := json.NewDecoder(io.LimitReader(upstream.Body, 1<<20)).Decode(&att); err != nil {
			http.Error(w, "decode", http.StatusBadGateway)
			return
		}
		// Keep the honest proof; replace everything the proof does not cover.
		att["host"] = servingHost
		att["models"] = []map[string]string{
			{"name": attackerModel, "variant": "", "family": "text"},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(att)
	})

	d, err := discovery.New(remoteOptions(t, hostile.url()))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	instances, err := d.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(instances) != 1 {
		t.Fatalf("expected the endpoint to be reported once, got %d", len(instances))
	}
	inst := instances[0]

	sendErr := sendCanaries(t, d, inst)

	assertRelayWireClean(t, hostile, map[string]string{
		promptCanary: "the user's prompt",
		fileCanary:   "file content",
		credCanary:   "a credential",
		sharedSecret: "the pre-shared secret itself",
	})

	if inst.Trusted {
		t.Errorf("an endpoint that PROXIED a genuine instance's proof was trusted: " +
			"the proof is not bound to the endpoint that was dialled (FR-024)")
	}
	for _, m := range inst.ServedOptions {
		if m.Identity.Model == attackerModel {
			t.Errorf("an attacker-chosen model %q was read out of a relayed attestation, "+
				"labelled with host %q (FR-023, FR-024)", attackerModel, m.ServingHost)
		}
	}
	if len(inst.ServedOptions) != 0 {
		t.Errorf("relaying endpoint had %d models read out of its answer (FR-024)", len(inst.ServedOptions))
	}
	if sendErr == nil {
		t.Errorf("Send to a relaying endpoint succeeded; it must be refused (FR-025)")
	}
}

// TestHonestInstanceStillVerifiesUnderChannelBinding is the counterpart that
// stops the two tests above from being satisfiable by refusing everything: an
// instance reached directly, computing its proof over the endpoint clients are
// configured to reach it at, must still authenticate.
func TestHonestInstanceStillVerifiesUnderChannelBinding(t *testing.T) {
	honest := newInstanceServer(t, sharedSecret)

	d, err := discovery.New(remoteOptions(t, honest.url()))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	instances, err := d.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(instances) != 1 || !instances[0].Trusted {
		t.Fatalf("a directly reached honest instance was not trusted: %+v", instances)
	}
	if len(instances[0].ServedOptions) != 1 {
		t.Fatalf("honest instance advertised %d usable models, want 1", len(instances[0].ServedOptions))
	}
}

// ---------------------------------------------------------------------------
// SECURITY-3: the local-host exemption must be an exemption for LOOPBACK, not
// for whatever an operator happened to write in LocalHostEndpoints.
// ---------------------------------------------------------------------------

// nonLoopbackAddress finds an address on this machine that is genuinely not
// loopback, so a "local-host" endpoint can be pointed at something off the
// trust boundary the exemption assumes.
func nonLoopbackAddress(t *testing.T) string {
	t.Helper()
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		t.Skipf("SKIP-OK: cannot enumerate interfaces on this host: %v", err)
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok || ipnet.IP.IsLoopback() || ipnet.IP.To4() == nil {
			continue
		}
		return ipnet.IP.String()
	}
	t.Skip("SKIP-OK: this host has no non-loopback IPv4 address, so the local-host " +
		"exemption cannot be pointed off the loopback boundary here")
	return ""
}

// TestLocalHostModeDoesNotExemptANonLoopbackEndpoint.
//
// FR-024 exempts local-host discovery by construction: an instance ON THIS
// MACHINE is not "beyond the current host". The exemption is therefore a claim
// about loopback, and an endpoint that is not loopback is not covered by it —
// whatever mode listed it. Without the check, writing a LAN address into
// LocalHostEndpoints turns FR-024 off for that host: it is trusted with no
// secret configured and no proof presented, and it receives the prompt.
func TestLocalHostModeDoesNotExemptANonLoopbackEndpoint(t *testing.T) {
	ip := nonLoopbackAddress(t)

	// A host on the network that presents NO secret, bound to a non-loopback
	// address so the endpoint really is off the machine's own boundary.
	hostile := newRelayServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"host":   servingHost,
			"models": []map[string]string{{"name": servedModel, "variant": servedVariant, "family": "text"}},
			// No proof field at all: this host holds nothing.
		})
	})
	hostile.server.Close()
	ln, err := net.Listen("tcp", net.JoinHostPort(ip, "0"))
	if err != nil {
		t.Skipf("SKIP-OK: cannot bind %s: %v", ip, err)
	}
	hostile.server = httptest.NewUnstartedServer(http.HandlerFunc(hostile.handle))
	hostile.server.Listener = ln
	hostile.server.Start()
	t.Cleanup(hostile.server.Close)

	modes := discovery.NoModes()
	modes.Enable(discovery.LocalHost)
	d, err := discovery.New(discovery.Options{
		Modes: modes,
		// No secret, and the local-host exemption left at its default.
		LocalHostEndpoints: []string{hostile.url()},
		ProbeTimeout:       5 * time.Second,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	instances, _ := d.Discover(context.Background())
	if len(instances) != 1 {
		t.Fatalf("expected the endpoint to be reported once, got %d", len(instances))
	}
	inst := instances[0]

	sendErr := sendCanaries(t, d, inst)

	assertRelayWireClean(t, hostile, map[string]string{
		promptCanary: "the user's prompt",
		fileCanary:   "file content",
		credCanary:   "a credential",
	})

	if inst.Trusted {
		t.Errorf("a NON-LOOPBACK endpoint listed under local-host discovery was trusted "+
			"with no secret configured and no proof presented (FR-024): %s", inst.Endpoint)
	}
	if len(inst.ServedOptions) != 0 {
		t.Errorf("unauthenticated non-loopback host had %d models read out of its answer", len(inst.ServedOptions))
	}
	if sendErr == nil {
		t.Errorf("Send to an unauthenticated non-loopback host succeeded (FR-025)")
	}
	// A host we cannot authenticate should not even learn that we are looking:
	// the refusal belongs before the probe, exactly as it does for the modes
	// that are refused wholesale for want of a secret.
	if got := len(hostile.traffic()); got != 0 {
		t.Errorf("%d request(s) reached a non-loopback endpoint we could never have authenticated", got)
	}
}

// TestLocalHostModeStillExemptsLoopback keeps the check above from being
// satisfiable by requiring a secret everywhere: a genuine loopback instance,
// with no secret configured, is still trusted. That is the whole point of the
// exemption, and removing it would be a different defect.
func TestLocalHostModeStillExemptsLoopback(t *testing.T) {
	local := newInstanceServer(t, "") // presents no proof — and needs none
	modes := discovery.NoModes()
	modes.Enable(discovery.LocalHost)
	d, err := discovery.New(discovery.Options{
		Modes:              modes,
		LocalHostEndpoints: []string{local.url()}, // httptest binds 127.0.0.1
		ProbeTimeout:       5 * time.Second,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	instances, err := d.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(instances) != 1 || !instances[0].Trusted {
		t.Fatalf("a loopback instance must remain exempt from FR-024: %+v", instances)
	}
	if len(instances[0].ServedOptions) != 1 {
		t.Errorf("loopback instance advertised %d usable models, want 1", len(instances[0].ServedOptions))
	}
}

// ---------------------------------------------------------------------------
// SECURITY-4: DNS rebinding between the probe and the send.
//
// The channel binding added for SECURITY-2 is established on the connection the
// PROBE opened. Send opens a NEW connection, so if the endpoint is a NAME the
// name is resolved a second time — and the answer to the second lookup is the
// attacker's to choose. The probe's binding says nothing about it.
// ---------------------------------------------------------------------------

// rebindingDialer is a test-controlled name service. Every dial for any name is
// routed to whichever backend the test currently points it at, so a name can
// answer honestly for the probe and hostilely for the send — which is exactly
// what an attacker who controls one DNS answer between two requests has.
//
// It replaces resolution at the transport, not at the resolver, because the
// process resolver is global state and this package's tests run in parallel.
type rebindingDialer struct {
	mu     sync.Mutex
	target string
	dialed []string
}

func (r *rebindingDialer) point(addr string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.target = addr
}

func (r *rebindingDialer) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	r.mu.Lock()
	target := r.target
	r.dialed = append(r.dialed, addr+" -> "+target)
	r.mu.Unlock()
	var d net.Dialer
	return d.DialContext(ctx, network, target)
}

func (r *rebindingDialer) log() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.dialed))
	copy(out, r.dialed)
	return out
}

// addrOf reduces a server URL to the host:port a dialler would be handed.
func addrOf(t *testing.T, rawURL string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parsing %q: %v", rawURL, err)
	}
	return u.Host
}

// rebindingClient is a client whose every connection goes through dialer.
// Keep-alives are off so each request dials again: a pooled connection would
// hide the second lookup and make the test prove nothing about rebinding.
func rebindingClient(dialer *rebindingDialer) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext:       dialer.dial,
			DisableKeepAlives: true,
		},
		Timeout: 5 * time.Second,
	}
}

// TestSendDialsTheAttestedAddressNotARebindableName.
//
// The endpoint is a NAME. It resolves to a genuine instance while the probe
// runs, so the instance attests correctly and is trusted — everything about the
// attestation is honest. The name then resolves to a hostile host, and the
// user's prompt, open file and upstream credential are sent.
//
// Without pinning, Send re-resolves the name and posts all three to whoever
// answers the second lookup. The endpoint that passed attestation and the
// endpoint that receives the content are different machines, and nothing in the
// exchange notices.
func TestSendDialsTheAttestedAddressNotARebindableName(t *testing.T) {
	honest := newInstanceServer(t, sharedSecret)
	hostile := newRelayServer(t, func(w http.ResponseWriter, r *http.Request) {
		// Never reached when the address is pinned. If it ever is, the answer
		// carries no proof, so the failure is loud rather than silent.
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"host":"gpu-01","models":[]}`))
	})

	dialer := &rebindingDialer{target: addrOf(t, honest.url())}

	// A name, not an address: the endpoint a rebinding attack needs. Its port
	// is never dialled — the dialler above stands in for resolution — but it is
	// written realistically so the endpoint is a normal one.
	const namedEndpoint = "http://instance.discovery.test:8080"

	opts := remoteOptions(t, namedEndpoint)
	opts.HTTPClient = rebindingClient(dialer)

	d, err := discovery.New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	instances, err := d.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(instances) != 1 {
		t.Fatalf("expected the endpoint to be reported once, got %d", len(instances))
	}
	inst := instances[0]
	if !inst.Trusted {
		t.Fatalf("the genuine instance did not attest during the probe, so this test would "+
			"prove nothing about what happens afterwards: %+v", inst)
	}

	// The DNS answer flips. Nothing about the Instance we hold has changed.
	dialer.point(addrOf(t, hostile.url()))

	sendErr := sendCanaries(t, d, inst)

	assertRelayWireClean(t, hostile, map[string]string{
		promptCanary: "the user's prompt",
		fileCanary:   "file content",
		credCanary:   "a credential",
		sharedSecret: "the pre-shared secret itself",
	})

	if got := len(hostile.traffic()); got != 0 {
		t.Errorf("%d request(s) reached a host that never attested, after the name it shares "+
			"with the attested instance was re-pointed at it; dial log: %v", got, dialer.log())
	}

	// Pinning is not a refusal: the content must still reach the instance that
	// actually passed attestation. A Send that merely failed would satisfy the
	// assertions above while breaking the feature.
	if sendErr != nil {
		t.Fatalf("Send to the attested instance failed: %v", sendErr)
	}
	delivered := false
	for _, req := range honest.traffic() {
		if req.Path == discovery.RequestPath && strings.Contains(string(req.Body), promptCanary) {
			delivered = true
		}
	}
	if !delivered {
		t.Errorf("the prompt reached neither the attested instance nor the hostile one; " +
			"Send must deliver to the address that passed attestation")
	}
}

// TestSendRefusesAnInstanceWithNoVerifiedAddress is the fail-closed half of the
// pin, and it is the half that keeps the guard from decaying. An Instance that
// carries no verified address is one whose provenance this package cannot
// account for: it did not come from a probe. Re-resolving its endpoint "just
// this once" is precisely the behaviour the test above forbids, so it is
// refused instead — loudly, at the FR-025 boundary, before a body is built.
func TestSendRefusesAnInstanceWithNoVerifiedAddress(t *testing.T) {
	hostile := newRelayServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"host":"gpu-01","models":[]}`))
	})

	d, err := discovery.New(remoteOptions(t, hostile.url()))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Trusted set by hand, as a caller outside this package could: the flag says
	// authenticated, but no attestation ever happened, so there is no address to
	// dial that this package verified.
	inst := discovery.Instance{
		Endpoint:     hostile.url(),
		Reachability: discovery.Remote,
		Trusted:      true,
		Health:       discovery.Health{Reachable: true, LastSeen: time.Now()},
	}

	sendErr := sendCanaries(t, d, inst)

	assertRelayWireClean(t, hostile, map[string]string{
		promptCanary: "the user's prompt",
		fileCanary:   "file content",
		credCanary:   "a credential",
		sharedSecret: "the pre-shared secret itself",
	})
	if got := len(hostile.traffic()); got != 0 {
		t.Errorf("%d request(s) reached an endpoint for an Instance that never attested", got)
	}
	if sendErr == nil {
		t.Errorf("Send accepted an Instance with no verified address; it must refuse rather " +
			"than fall back to re-resolving the endpoint (FR-025)")
	}
}
