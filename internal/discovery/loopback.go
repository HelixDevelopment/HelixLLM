package discovery

import (
	"net"
	"net/url"
)

// isLoopbackPeer reports whether an address we ACTUALLY CONNECTED TO is on this
// machine's loopback boundary.
//
// The input is a remote address taken from a live connection (net.Conn's
// RemoteAddr), not a configured string and not a pre-dial DNS lookup. That
// distinction is the whole point of this function, and it is worth stating
// plainly because the defect it fixes was subtle:
//
// The local-host exemption in FR-024 is a claim about LOOPBACK — an instance ON
// THIS MACHINE is not "beyond the current host", so it need not prove it holds
// the pre-shared secret. The code used to grant that exemption based on which
// CONFIG LIST an endpoint appeared in. But LocalHostEndpoints is a list of
// arbitrary strings from a user or an environment variable. Writing a LAN or
// internet address into it turned FR-024 off for that host: no secret required,
// no proof presented, and the user's prompt, open files, and upstream
// credentials posted straight to it.
//
// Checking the string, or resolving it before dialling, would both be weaker
// than this. A name can resolve to 127.0.0.1 when checked and to something else
// a moment later when dialled — the attacker controls the DNS answer and the
// gap between the two lookups is the attack. Reading the address off the
// established connection closes that gap: whatever the config claimed and
// whatever DNS said, this is the peer we are actually talking to.
//
// An empty or unparseable address is NOT loopback. A missing reading is not a
// pass — it means we do not know who we connected to, which is exactly when the
// exemption must not apply.
func isLoopbackPeer(remoteAddr string) bool {
	if remoteAddr == "" {
		return false
	}
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		// Some address kinds (Unix sockets) have no port. A Unix socket is on
		// this machine by construction, but we do not guess: only an address we
		// can parse and confirm counts.
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}

// endpointLooksLoopback is the PRE-DIAL estimate: it resolves the endpoint's
// host and reports whether every address it resolves to is loopback.
//
// This is deliberately a weaker instrument than isLoopbackPeer, and it is used
// for a different job. As a security GATE a pre-dial lookup is defeatable: the
// name can resolve to 127.0.0.1 here and to an attacker's address a moment
// later when the connection is actually made, and nothing in between would
// notice. isLoopbackPeer remains the gate for exactly that reason.
//
// As a DISCLOSURE FILTER it is perfectly adequate, and that is what it does
// here: when we hold no secret and this estimate says the endpoint is off-box,
// we already know we could never authenticate it, so dialling it would only
// tell a host we cannot trust that we exist and hand it a fresh nonce. Being
// occasionally wrong in the attacker's favour costs nothing, because the peer
// check still refuses the connection afterwards. Being wrong in the other
// direction -- refusing to dial something that was really loopback -- would
// only happen for a name whose own resolution says otherwise.
//
// Unresolvable or unparseable means "not known to be loopback": we let the
// probe proceed and leave the real decision to the peer check, rather than
// silently dropping an endpoint over a transient DNS failure.
func endpointLooksLoopback(endpoint string) bool {
	host := hostOnly(endpoint)
	if host == "" {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	addrs, err := net.LookupIP(host)
	if err != nil || len(addrs) == 0 {
		return false
	}
	for _, ip := range addrs {
		if !ip.IsLoopback() {
			return false
		}
	}
	return true
}

// hostOnly extracts the hostname from an endpoint URL, without its port.
func hostOnly(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil {
		return ""
	}
	return u.Hostname()
}
