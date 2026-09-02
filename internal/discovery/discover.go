// Package discovery finds HelixLLM serving instances — on this host, elsewhere
// on the local network, and at explicitly configured remote endpoints — and
// decides which of them may be trusted as a source of models (FR-021..FR-025).
//
// Two properties shape every design decision in the package.
//
// A disabled mode must be SILENT. Not "filtered out of the results" — silent,
// with nothing on the wire (FR-022, SC-007). So candidate endpoints for a mode
// are resolved inside the branch that has already checked the mode is enabled;
// there is no path that builds a candidate list first and filters it after,
// because such a path passes a configuration-flag test while still probing.
//
// An unauthenticated instance must receive NOTHING. Trust is established by
// challenge-response before a single served model is read out of its answer
// (FR-024), and [Discoverer.Send] refuses on the trust flag before it marshals
// a request body (FR-025). Anything a discovered instance reports — its host
// name, its model list — is untrusted input until the proof checks out, and is
// bounded and validated even then.
package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/naming"
)

// Wire contract with a serving instance. These paths and parameter names are
// part of the protocol two HelixLLM processes speak; changing one is a
// compatibility break, not a rename.
const (
	// AttestPath answers a discovery challenge with the instance's identity,
	// its served models, and a proof it holds the pre-shared secret.
	AttestPath = "/helixllm/discovery/v1/attest"
	// RequestPath carries request content to an instance already trusted.
	RequestPath = "/helixllm/discovery/v1/request"
	// NonceParam carries the hex challenge on an attestation probe.
	NonceParam = "nonce"
	// NonceHeader and ProofHeader authenticate US to the instance on a request.
	// They carry a proof over a fresh nonce — never the secret (FR-025).
	NonceHeader = "X-Helixllm-Discovery-Nonce"
	ProofHeader = "X-Helixllm-Discovery-Proof"
)

// Bounds applied to everything a remote instance sends. A discovered host is
// untrusted input until it has authenticated, and even afterwards it is a
// separate machine that can be wrong: an unbounded read from one is a
// memory-exhaustion vector reachable by anything that can open a socket.
const (
	// MaxAttestationBytes caps an attestation response.
	MaxAttestationBytes = 1 << 20
	// MaxAdvertisedModels caps how many models one instance may advertise.
	MaxAdvertisedModels = 512
	// DefaultProbeTimeout bounds a single probe.
	DefaultProbeTimeout = 5 * time.Second
	// DefaultMaxSweepAddresses bounds an address-range sweep. A range wider
	// than this is refused rather than truncated: truncating would silently
	// search part of what the operator asked for and report success.
	DefaultMaxSweepAddresses = 256
	// maxConcurrentProbes bounds fan-out so a sweep cannot exhaust file
	// descriptors on the host running it.
	maxConcurrentProbes = 16
)

// Errors reported by discovery itself. Trust errors live in trust.go.
var (
	// ErrNoCandidates means an enabled mode has nothing configured to probe.
	ErrNoCandidates = errors.New("discovery: mode has no configured candidates")
	// ErrSweepTooLarge means a configured range exceeds the sweep bound.
	ErrSweepTooLarge = errors.New("discovery: address range exceeds the configured sweep bound")
	// ErrBadEndpoint means an endpoint could not be parsed.
	ErrBadEndpoint = errors.New("discovery: malformed endpoint")
)

// ServedModel is one model an instance serves, labelled with the host serving
// it (FR-023).
//
// ServingHost duplicates Identity.Host on purpose: Identity is a value with its
// own escaping rules, and a consumer that wants to group a listing by host
// should read a plain field rather than parse a rendered identity back apart.
type ServedModel struct {
	// Identity is the human-readable identity, `helixllm/<host>/<model>[:<variant>]`.
	Identity naming.Identity
	// ServingHost is the host serving this model.
	ServingHost string
	// Family is the capability family the instance reported, as a machine key.
	Family string
}

// Instance is a reachable provider of models (data-model.md § Serving Instance).
type Instance struct {
	// Endpoint is how to reach it.
	Endpoint string
	// Reachability is the class the mode that found it belongs to.
	Reachability Reachability
	// Trusted records whether it proved it holds the pre-shared secret. An
	// instance that did not is never a model source and never receives request
	// content (FR-024, FR-025).
	Trusted bool
	// ServedOptions is what it serves. It is EMPTY for an untrusted instance:
	// what such a host advertises is not withheld at the presentation layer, it
	// is never read out of the response at all.
	ServedOptions []ServedModel
	// Health is liveness, so a dead instance is not exported as available.
	Health Health
}

// FileContent is one file's contents accompanying a request. It is request
// CONTENT for FR-025 purposes: it must not reach an unauthenticated instance.
type FileContent struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// RequestContent is everything of the user's that a request carries. The three
// fields are exactly the three FR-025 names, kept in one struct so the guard in
// [Discoverer.Send] cannot be applied to some of them and forgotten for others.
type RequestContent struct {
	Prompt      string            `json:"prompt,omitempty"`
	Files       []FileContent     `json:"files,omitempty"`
	Credentials map[string]string `json:"credentials,omitempty"`
}

// Options configures a Discoverer.
type Options struct {
	// Modes is which discovery modes may run. The zero ModeSet runs none.
	Modes ModeSet
	// Secret is the pre-shared secret. Required by any mode beyond local-host.
	Secret Secret

	// LocalHostEndpoints are explicit loopback endpoints.
	LocalHostEndpoints []string
	// LocalHostPorts are loopback ports to probe, in addition to the above.
	LocalHostPorts []int

	// LocalNetworkEndpoints are explicit peers on a local network.
	LocalNetworkEndpoints []string
	// LocalNetworkCIDRs are ranges to sweep, bounded by MaxSweepAddresses.
	LocalNetworkCIDRs []string
	// LocalNetworkPorts are the ports to try on each swept address. Required
	// when LocalNetworkCIDRs is set: a range without ports is not a narrower
	// search, it is an unanswerable one, and guessing a port here would be a
	// guess about which machines get probed.
	LocalNetworkPorts []int

	// RemoteEndpoints are explicitly configured remote endpoints.
	RemoteEndpoints []string

	// RequireSecretForLocalHost extends FR-024 to loopback. It is off by
	// default because FR-024 governs instances "beyond the current host", and
	// on by choice for a shared machine, where loopback is not a trust boundary.
	RequireSecretForLocalHost bool

	// HTTPClient, Logger, Now, ProbeTimeout, HealthTTL and MaxSweepAddresses
	// all have working defaults.
	HTTPClient        *http.Client
	Logger            *slog.Logger
	Now               func() time.Time
	ProbeTimeout      time.Duration
	HealthTTL         time.Duration
	MaxSweepAddresses int
}

// Discoverer finds and authenticates serving instances.
type Discoverer struct {
	modes    ModeSet
	secret   Secret
	opts     Options
	client   *http.Client
	log      *slog.Logger
	now      func() time.Time
	tracker  *Tracker
	timeout  time.Duration
	maxSweep int
}

// New validates the options and builds a Discoverer.
func New(opts Options) (*Discoverer, error) {
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: DefaultProbeTimeout}
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	timeout := opts.ProbeTimeout
	if timeout <= 0 {
		timeout = DefaultProbeTimeout
	}
	maxSweep := opts.MaxSweepAddresses
	if maxSweep <= 0 {
		maxSweep = DefaultMaxSweepAddresses
	}

	d := &Discoverer{
		modes:    opts.Modes,
		secret:   opts.Secret,
		opts:     opts,
		client:   client,
		log:      logger,
		now:      now,
		tracker:  NewTracker(opts.HealthTTL, now),
		timeout:  timeout,
		maxSweep: maxSweep,
	}
	return d, nil
}

// Tracker exposes the liveness tracker, so a caller can consult health between
// discovery rounds without re-probing.
func (d *Discoverer) Tracker() *Tracker { return d.tracker }

// Available keeps only instances that may be exported to users: trusted, and
// freshly observed as reachable.
func (d *Discoverer) Available(instances []Instance) []Instance {
	return d.tracker.Filter(instances)
}

// Discover probes every ENABLED mode and returns what it found.
//
// It reports partial results with a joined error rather than failing whole: one
// misconfigured mode should not hide the instances another mode legitimately
// found, and an operator needs to see both facts at once.
func (d *Discoverer) Discover(ctx context.Context) ([]Instance, error) {
	var (
		instances []Instance
		problems  []error
	)

	for _, mode := range Modes() {
		// The enablement check guards candidate RESOLUTION, not just probing.
		// A sweep resolves addresses by parsing a CIDR — harmless — but a
		// future resolver that asks the network what is out there would emit
		// traffic here, and this ordering is what keeps that safe (SC-007).
		if !d.modes.Enabled(mode) {
			continue
		}

		if err := d.checkSecretFor(mode); err != nil {
			// Refused before any candidate is resolved: a host we could not
			// authenticate does not even learn that we are looking.
			problems = append(problems, err)
			d.log.Warn("discovery mode refused", "mode", string(mode), "reason", ReasonRefused)
			continue
		}

		candidates, err := d.candidates(mode)
		if err != nil {
			problems = append(problems, err)
			continue
		}
		if len(candidates) == 0 {
			continue
		}

		instances = append(instances, d.probeAll(ctx, mode, candidates)...)
	}

	sort.SliceStable(instances, func(i, j int) bool {
		return instances[i].Endpoint < instances[j].Endpoint
	})
	return instances, errors.Join(problems...)
}

// checkSecretFor refuses a mode that requires authentication when no secret is
// configured. Probing anyway would produce results that could never be trusted
// while announcing this host to every endpoint in the range.
func (d *Discoverer) checkSecretFor(mode Reachability) error {
	if !d.requiresSecret(mode) || !d.secret.Empty() {
		return nil
	}
	return fmt.Errorf("%w: mode %s requires it", ErrNoSecret, mode)
}

// requiresSecret reports whether instances found by this mode must authenticate
// (FR-024: everything beyond the current host).
func (d *Discoverer) requiresSecret(mode Reachability) bool {
	if mode == LocalHost {
		return d.opts.RequireSecretForLocalHost
	}
	return true
}

// candidates resolves the endpoints for one mode. It is only ever called for an
// enabled mode.
func (d *Discoverer) candidates(mode Reachability) ([]string, error) {
	switch mode {
	case LocalHost:
		out := normaliseAll(d.opts.LocalHostEndpoints)
		for _, port := range d.opts.LocalHostPorts {
			out = append(out, "http://127.0.0.1:"+strconv.Itoa(port))
		}
		return dedupe(out), nil

	case LocalNetwork:
		out := normaliseAll(d.opts.LocalNetworkEndpoints)
		swept, err := d.sweep()
		if err != nil {
			return nil, err
		}
		return dedupe(append(out, swept...)), nil

	case Remote:
		return dedupe(normaliseAll(d.opts.RemoteEndpoints)), nil

	default:
		return nil, fmt.Errorf("discovery: unknown mode %q", mode)
	}
}

// sweep expands the configured ranges into endpoints.
//
// The whole expansion is validated BEFORE any probe runs, so an over-wide range
// is refused without having contacted the part of it that fit. Truncating
// instead would search a prefix of what was asked for and report success.
func (d *Discoverer) sweep() ([]string, error) {
	if len(d.opts.LocalNetworkCIDRs) == 0 {
		return nil, nil
	}
	if len(d.opts.LocalNetworkPorts) == 0 {
		return nil, fmt.Errorf("%w: LocalNetworkCIDRs is set but LocalNetworkPorts is empty",
			ErrNoCandidates)
	}

	var out []string
	for _, cidr := range d.opts.LocalNetworkCIDRs {
		addrs, err := expandCIDR(cidr, d.maxSweep)
		if err != nil {
			return nil, err
		}
		for _, addr := range addrs {
			for _, port := range d.opts.LocalNetworkPorts {
				out = append(out, "http://"+net.JoinHostPort(addr, strconv.Itoa(port)))
			}
		}
		if len(out) > d.maxSweep {
			return nil, fmt.Errorf("%w: %s expands to more than %d endpoints",
				ErrSweepTooLarge, cidr, d.maxSweep)
		}
	}
	return out, nil
}

// expandCIDR lists the host addresses in a range, refusing anything wider than
// max before enumerating it.
func expandCIDR(cidr string, max int) ([]string, error) {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("discovery: %w: %q is not a valid address range", ErrBadEndpoint, cidr)
	}
	ones, bits := network.Mask.Size()
	if bits == 0 {
		return nil, fmt.Errorf("discovery: %w: %q has no usable mask", ErrBadEndpoint, cidr)
	}
	hostBits := bits - ones
	if hostBits > 20 || 1<<uint(hostBits) > max {
		return nil, fmt.Errorf("%w: %s covers 2^%d addresses, bound is %d",
			ErrSweepTooLarge, cidr, hostBits, max)
	}

	var addrs []string
	for ip := network.IP.Mask(network.Mask); network.Contains(ip); ip = nextIP(ip) {
		addrs = append(addrs, ip.String())
		if len(addrs) > max {
			return nil, fmt.Errorf("%w: %s exceeds the bound of %d", ErrSweepTooLarge, cidr, max)
		}
	}
	return addrs, nil
}

// nextIP returns a fresh copy of ip incremented by one.
func nextIP(ip net.IP) net.IP {
	next := make(net.IP, len(ip))
	copy(next, ip)
	for i := len(next) - 1; i >= 0; i-- {
		next[i]++
		if next[i] != 0 {
			break
		}
	}
	return next
}

// probeAll probes candidates concurrently, bounded.
func (d *Discoverer) probeAll(ctx context.Context, mode Reachability, candidates []string) []Instance {
	var (
		mu      sync.Mutex
		results []Instance
		wg      sync.WaitGroup
	)
	sem := make(chan struct{}, maxConcurrentProbes)

	for _, endpoint := range candidates {
		wg.Add(1)
		go func(endpoint string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			inst := d.probe(ctx, mode, endpoint)
			mu.Lock()
			results = append(results, inst)
			mu.Unlock()
		}(endpoint)
	}
	wg.Wait()
	return results
}

// attestation is what an instance returns to a challenge. Every field is
// untrusted input until Proof has been verified.
type attestation struct {
	Host   string            `json:"host"`
	Models []advertisedModel `json:"models"`
	Proof  string            `json:"proof"`
}

type advertisedModel struct {
	Name    string `json:"name"`
	Variant string `json:"variant"`
	Family  string `json:"family"`
}

// probe challenges one endpoint and builds the resulting Instance.
//
// The order of operations is the security property: read a bounded body, verify
// the proof, and only then read the model list out of the response. An instance
// that fails the check gets an Instance with Trusted false and NO served
// options — its advertisement is not collected and filtered later, it is never
// collected.
func (d *Discoverer) probe(ctx context.Context, mode Reachability, endpoint string) Instance {
	inst := Instance{Endpoint: endpoint, Reachability: mode}

	fail := func(reason string, err error) Instance {
		d.tracker.Failure(endpoint, reason)
		inst.Health = d.tracker.Health(endpoint)
		d.log.Debug("discovery probe failed",
			"endpoint", endpoint, "mode", string(mode), "reason", reason, "error", errText(err))
		return inst
	}

	nonce, err := newNonce()
	if err != nil {
		return fail(ReasonRefused, err)
	}

	probeCtx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()

	target, err := url.Parse(endpoint)
	if err != nil {
		return fail(ReasonUnreachable, err)
	}
	target.Path = AttestPath
	target.RawQuery = url.Values{NonceParam: {hexString(nonce)}}.Encode()

	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, target.String(), nil)
	if err != nil {
		return fail(ReasonUnreachable, err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return fail(ReasonUnreachable, err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, MaxAttestationBytes))
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return fail(ReasonUnreachable, fmt.Errorf("HTTP %d", resp.StatusCode))
	}

	var att attestation
	if err := json.NewDecoder(io.LimitReader(resp.Body, MaxAttestationBytes)).Decode(&att); err != nil {
		return fail(ReasonMalformedResponse, err)
	}

	if d.requiresSecret(mode) {
		if err := Verify(d.secret, nonce, att.Proof); err != nil {
			// FR-024. Nothing the instance advertised is read from here on.
			return fail(ReasonAuthenticationFailed, err)
		}
	}

	inst.Trusted = true
	inst.ServedOptions = d.servedModels(endpoint, att)
	d.tracker.Success(endpoint)
	inst.Health = d.tracker.Health(endpoint)
	d.log.Debug("discovery probe succeeded",
		"endpoint", endpoint, "mode", string(mode), "models", len(inst.ServedOptions))
	return inst
}

// servedModels turns an authenticated attestation into labelled models (FR-023).
//
// The host label comes from what the instance reported, falling back to the
// endpoint's own host when that is unusable — an instance that reports nothing
// intelligible still gets labelled with somewhere a user can find it, rather
// than with an empty string that would collapse every such instance into one.
// Each entry goes through naming.NewIdentity, which rejects control characters:
// a model name containing a newline would corrupt every line-oriented listing
// the identity is later written into.
func (d *Discoverer) servedModels(endpoint string, att attestation) []ServedModel {
	host := strings.TrimSpace(att.Host)
	if host == "" {
		host = hostFromEndpoint(endpoint)
	}

	models := att.Models
	if len(models) > MaxAdvertisedModels {
		d.log.Warn("instance advertised more models than permitted",
			"endpoint", endpoint, "advertised", len(models), "limit", MaxAdvertisedModels)
		models = models[:MaxAdvertisedModels]
	}

	out := make([]ServedModel, 0, len(models))
	for _, m := range models {
		identity, err := naming.NewIdentity(host, m.Name, m.Variant)
		if err != nil {
			d.log.Warn("discarding an unusable advertised model",
				"endpoint", endpoint, "error", errText(err))
			continue
		}
		out = append(out, ServedModel{
			Identity:    identity,
			ServingHost: identity.Host,
			Family:      strings.TrimSpace(m.Family),
		})
	}
	return out
}

// Send transmits request content to an instance.
//
// This is the FR-025 boundary. The trust check comes FIRST — before the body is
// marshalled, before a URL is built, before a connection is opened — because a
// guard placed after any of those has already leaked something, and because a
// test can only prove the guard holds by observing that nothing arrived.
//
// The instance is also authenticated TO by proof over a fresh nonce rather than
// by presenting the secret, so even a trusted peer never receives the credential
// itself.
func (d *Discoverer) Send(ctx context.Context, inst Instance, content RequestContent) ([]byte, error) {
	if !inst.Trusted {
		return nil, fmt.Errorf("%w: %s is not authenticated, so no request content may be sent to it (FR-025)",
			ErrUntrusted, inst.Endpoint)
	}
	if !d.tracker.Available(inst) {
		return nil, fmt.Errorf("discovery: %s is not currently available (%s)",
			inst.Endpoint, reasonOr(inst.Health.Reason, ReasonUnreachable))
	}
	if d.requiresSecret(inst.Reachability) && d.secret.Empty() {
		return nil, fmt.Errorf("%w: cannot authenticate to %s", ErrNoSecret, inst.Endpoint)
	}

	body, err := json.Marshal(content)
	if err != nil {
		return nil, fmt.Errorf("discovery: encoding request content: %w", err)
	}

	target, err := url.Parse(inst.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("discovery: %w: %s", ErrBadEndpoint, inst.Endpoint)
	}
	target.Path = RequestPath

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("discovery: building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	if !d.secret.Empty() {
		nonce, err := newNonce()
		if err != nil {
			return nil, err
		}
		req.Header.Set(NonceHeader, hexString(nonce))
		req.Header.Set(ProofHeader, Proof(d.secret, nonce))
	}

	resp, err := d.client.Do(req)
	if err != nil {
		d.tracker.Failure(inst.Endpoint, ReasonUnreachable)
		return nil, fmt.Errorf("discovery: sending to %s: %w", inst.Endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()

	answer, err := io.ReadAll(io.LimitReader(resp.Body, MaxAttestationBytes))
	if err != nil {
		return nil, fmt.Errorf("discovery: reading the answer from %s: %w", inst.Endpoint, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("discovery: %s answered HTTP %d", inst.Endpoint, resp.StatusCode)
	}
	d.tracker.Success(inst.Endpoint)
	return answer, nil
}

// normaliseAll trims and defaults the scheme on each endpoint, dropping blanks.
func normaliseAll(endpoints []string) []string {
	out := make([]string, 0, len(endpoints))
	for _, e := range endpoints {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if !strings.Contains(e, "://") {
			e = "http://" + e
		}
		out = append(out, strings.TrimSuffix(e, "/"))
	}
	return out
}

// dedupe removes repeats while preserving order, so an endpoint listed twice is
// probed once.
func dedupe(endpoints []string) []string {
	seen := make(map[string]struct{}, len(endpoints))
	out := make([]string, 0, len(endpoints))
	for _, e := range endpoints {
		if _, ok := seen[e]; ok {
			continue
		}
		seen[e] = struct{}{}
		out = append(out, e)
	}
	return out
}

// hostFromEndpoint extracts a host label from an endpoint URL.
func hostFromEndpoint(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil {
		return endpoint
	}
	if h := u.Hostname(); h != "" {
		return h
	}
	return endpoint
}

// errText renders an error for a log field, or the empty string for nil. It
// exists so a nil error never becomes the literal "<nil>" in a log line.
func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// reasonOr defaults an empty reason key.
func reasonOr(reason, fallback string) string {
	if reason == "" {
		return fallback
	}
	return reason
}
