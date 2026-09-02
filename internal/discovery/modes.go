package discovery

import (
	"fmt"
	"sort"
	"strings"
)

// Reachability is where a serving instance sits relative to this machine, and —
// because each class is found by a different mechanism and carries a different
// trust weight — it doubles as the identity of a discovery MODE.
//
// One type rather than two is deliberate. A separate Mode enum would have to be
// kept in one-to-one correspondence with this one by hand, and the first time
// the two drifted, a mode would be reported as disabled while its class was
// still being probed.
type Reachability string

const (
	// LocalHost is an instance on this machine, reached over loopback.
	LocalHost Reachability = "local-host"
	// LocalNetwork is an instance elsewhere on a network this host is attached
	// to, found by probing configured peers or sweeping a configured range.
	LocalNetwork Reachability = "local-network"
	// Remote is an instance at an explicitly configured endpoint.
	Remote Reachability = "remote"
)

// Modes lists every discovery mode, in the order results are reported.
func Modes() []Reachability { return []Reachability{LocalHost, LocalNetwork, Remote} }

// Known reports whether r is one of the recorded reachability classes.
func (r Reachability) Known() bool {
	return r == LocalHost || r == LocalNetwork || r == Remote
}

// Environment variables that enable or disable each mode independently
// (FR-020, FR-022). One variable per mode is what makes them independent: a
// single combined setting could not express "network off, remote on" without
// inventing a syntax, and a syntax invites a parse that guesses.
const (
	EnvLocalHost    = "HELIXLLM_DISCOVERY_LOCAL_HOST"
	EnvLocalNetwork = "HELIXLLM_DISCOVERY_LOCAL_NETWORK"
	EnvRemote       = "HELIXLLM_DISCOVERY_REMOTE"
)

// EnvVar is the environment variable governing this mode.
func (r Reachability) EnvVar() string {
	switch r {
	case LocalHost:
		return EnvLocalHost
	case LocalNetwork:
		return EnvLocalNetwork
	case Remote:
		return EnvRemote
	default:
		return ""
	}
}

// ModeSet records which discovery modes may run.
//
// It is a value: copying it yields an independently mutable set, which is what
// lets a caller derive "all modes except this one" without disturbing the
// original. The zero ModeSet has every mode disabled, so a caller that forgets
// to configure modes emits no traffic rather than probing the network by
// default.
type ModeSet struct {
	localHost    bool
	localNetwork bool
	remote       bool
}

// DefaultModes is the posture applied when a caller expresses no preference.
//
// Local-host discovery is on: it reaches nothing but this machine. The other
// two are off, because both leave this host, and both should be a decision
// someone made rather than a default they inherited.
func DefaultModes() ModeSet { return ModeSet{localHost: true} }

// AllModes enables every mode.
func AllModes() ModeSet { return ModeSet{localHost: true, localNetwork: true, remote: true} }

// NoModes disables every mode.
func NoModes() ModeSet { return ModeSet{} }

// Enabled reports whether mode r may run. An unknown class is never enabled.
func (m ModeSet) Enabled(r Reachability) bool {
	switch r {
	case LocalHost:
		return m.localHost
	case LocalNetwork:
		return m.localNetwork
	case Remote:
		return m.remote
	default:
		return false
	}
}

// Enable turns mode r on.
func (m *ModeSet) Enable(r Reachability) { m.set(r, true) }

// Disable turns mode r off. It affects that mode and no other (FR-022).
func (m *ModeSet) Disable(r Reachability) { m.set(r, false) }

func (m *ModeSet) set(r Reachability, on bool) {
	switch r {
	case LocalHost:
		m.localHost = on
	case LocalNetwork:
		m.localNetwork = on
	case Remote:
		m.remote = on
	}
}

// List returns the enabled modes in reporting order.
func (m ModeSet) List() []Reachability {
	var out []Reachability
	for _, r := range Modes() {
		if m.Enabled(r) {
			out = append(out, r)
		}
	}
	return out
}

// Any reports whether at least one mode is enabled.
func (m ModeSet) Any() bool { return m.localHost || m.localNetwork || m.remote }

// String renders the enabled modes, for logs and diagnostics.
func (m ModeSet) String() string {
	list := m.List()
	if len(list) == 0 {
		return "none"
	}
	parts := make([]string, len(list))
	for i, r := range list {
		parts[i] = string(r)
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

// ModesFromEnv reads the per-mode settings through lookup, which is the process
// environment in production and a map in tests.
//
// An absent variable leaves that mode at its DefaultModes value. An unparseable
// one is an error rather than a fallback: an operator who wrote
// HELIXLLM_DISCOVERY_REMOTE=perhaps has expressed an intent, and guessing which
// way they meant it is exactly the kind of silent decision that ends with a
// mode probing the network when someone believed it was off.
func ModesFromEnv(lookup func(string) (string, bool)) (ModeSet, error) {
	modes := DefaultModes()
	for _, r := range Modes() {
		raw, ok := lookup(r.EnvVar())
		if !ok {
			continue
		}
		on, err := parseBool(raw)
		if err != nil {
			return NoModes(), fmt.Errorf("discovery: %s: %w", r.EnvVar(), err)
		}
		modes.set(r, on)
	}
	return modes, nil
}

// parseBool accepts the spellings an operator plausibly writes in a .env file.
// The rejected value is echoed because it is a mode flag, never a credential.
func parseBool(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "t", "true", "y", "yes", "on", "enabled":
		return true, nil
	case "0", "f", "false", "n", "no", "off", "disabled":
		return false, nil
	default:
		return false, fmt.Errorf("%q is not a recognised on/off value", raw)
	}
}
