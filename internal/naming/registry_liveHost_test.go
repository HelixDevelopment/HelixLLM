package naming

import "testing"

// A live host noted before anything is registered must survive the first
// Register for that consumer.
//
// The three per-consumer maps used to be created together under a single
// `byIdentifier` guard. NoteLiveHost populates liveHostPrefixes WITHOUT
// registering anything, so byIdentifier stayed absent, and the first Register
// then fired that guard and replaced the noted hosts with an empty map.
//
// This is the ordering RegisterNamesFor actually produces. It walks a MAP of
// providers, noting each host and registering its models in one pass — so Go's
// randomised iteration order decided whether a host was noted before or after
// the first Register wiped the set. A provider reporting a host but not yet
// listing a model is precisely the case NoteLiveHost exists to cover, and it
// was the one being dropped.
//
// The user-visible consequence was a permanent 404: an identifier for a live
// host was judged retired, told to re-fetch, and the same request succeeded on
// the next start. Measured before the fix: 23 failures in 30 runs of the route
// test; 43 in 300 of the direct check.
func TestNoteLiveHostSurvivesTheFirstRegister(t *testing.T) {
	rs := ClaudeToolkit
	const starting = "localhost.lan" // reports a host, lists no model yet
	const serving = "gpu-01"         // reports a host and a model

	r := NewRegistry()

	// The order that used to lose the note: note first, register second.
	r.NoteLiveHost(rs, starting)

	id, err := NewIdentity(serving, "llama3", "8b")
	if err != nil {
		t.Fatalf("build identity: %v", err)
	}
	if _, err := r.Register(id, rs); err != nil {
		t.Fatalf("register: %v", err)
	}

	startingID, err := NewIdentity(starting, "qwen2.5", "7b")
	if err != nil {
		t.Fatalf("build identity on the starting host: %v", err)
	}
	identifier, err := Derive(startingID, rs)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}

	if r.IsRetiredIdentifier(rs, identifier) {
		t.Errorf("%q was judged RETIRED, but its host %q was noted live before "+
			"the first Register. A user holding this identifier is told it is "+
			"permanently gone and to re-fetch, when the host is simply still "+
			"starting up.", identifier, starting)
	}

	// The registered host must also still be live — proving the fix did not
	// simply stop the map being created.
	servedIdentifier, err := Derive(id, rs)
	if err != nil {
		t.Fatalf("derive the served identifier: %v", err)
	}
	if r.IsRetiredIdentifier(rs, servedIdentifier) {
		t.Errorf("%q was judged retired, though it was registered", servedIdentifier)
	}
}

// The opposite order must work too, so the fix is not order-sensitive in the
// other direction.
func TestNoteLiveHostAfterRegisterAlsoSurvives(t *testing.T) {
	rs := ClaudeToolkit
	r := NewRegistry()

	id, _ := NewIdentity("gpu-01", "llama3", "8b")
	if _, err := r.Register(id, rs); err != nil {
		t.Fatalf("register: %v", err)
	}
	r.NoteLiveHost(rs, "localhost.lan")

	startingID, _ := NewIdentity("localhost.lan", "qwen2.5", "7b")
	identifier, _ := Derive(startingID, rs)
	if r.IsRetiredIdentifier(rs, identifier) {
		t.Errorf("%q judged retired when its host was noted after a Register", identifier)
	}
}

// The negative control: a host that was never noted and never registered must
// still be judged retired, or the two tests above would pass against a check
// that has simply stopped refusing anything.
func TestAnUnknownRetiredHostIsStillRetired(t *testing.T) {
	rs := ClaudeToolkit
	r := NewRegistry()

	id, _ := NewIdentity("gpu-01", "llama3", "8b")
	if _, err := r.Register(id, rs); err != nil {
		t.Fatalf("register: %v", err)
	}

	gone, _ := NewIdentity("127.0.0.1", "qwen2.5", "7b")
	identifier, _ := Derive(gone, rs)
	if !r.IsRetiredIdentifier(rs, identifier) {
		t.Errorf("%q was NOT judged retired, though nothing live accounts for "+
			"its host. If this passes, the retired check no longer refuses "+
			"anything and the two tests above prove nothing.", identifier)
	}
}
