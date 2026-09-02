package discovery_test

import (
	"os"
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/discovery"
)

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}

func TestHealthTrackerRecordsReachabilityAndFailures(t *testing.T) {
	base := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	now := base
	tr := discovery.NewTracker(time.Minute, func() time.Time { return now })

	const ep = "http://198.51.100.7:8080"

	if h := tr.Health(ep); h.Reachable {
		t.Errorf("an endpoint never observed must not read as reachable: %+v", h)
	}

	tr.Success(ep)
	h := tr.Health(ep)
	if !h.Reachable || !h.LastSeen.Equal(base) || h.ConsecutiveFailures != 0 {
		t.Fatalf("after a success: %+v", h)
	}
	if !h.Live(now, time.Minute) {
		t.Errorf("a just-seen endpoint must be live")
	}

	// Staleness: past the TTL, liveness lapses even with no observed failure.
	now = base.Add(2 * time.Minute)
	if tr.Health(ep).Live(now, time.Minute) {
		t.Errorf("an endpoint unseen for longer than the TTL must not be live")
	}

	now = base.Add(3 * time.Minute)
	tr.Failure(ep, discovery.ReasonUnreachable)
	h = tr.Health(ep)
	if h.Reachable {
		t.Errorf("after a failure the endpoint must not be reachable: %+v", h)
	}
	if h.ConsecutiveFailures != 1 || h.Reason != discovery.ReasonUnreachable {
		t.Errorf("failure not recorded: %+v", h)
	}
	tr.Failure(ep, discovery.ReasonUnreachable)
	if got := tr.Health(ep).ConsecutiveFailures; got != 2 {
		t.Errorf("consecutive failures = %d, want 2", got)
	}
	tr.Success(ep)
	if got := tr.Health(ep).ConsecutiveFailures; got != 0 {
		t.Errorf("a success must clear the failure streak, got %d", got)
	}
}

// TestUnhealthyOrUntrustedInstanceIsNotExportedAsAvailable covers T060: the two
// independent grounds on which an instance must be withheld.
func TestUnhealthyOrUntrustedInstanceIsNotExportedAsAvailable(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	tr := discovery.NewTracker(time.Minute, func() time.Time { return now })

	live := discovery.Health{Reachable: true, LastSeen: now}
	stale := discovery.Health{Reachable: true, LastSeen: now.Add(-time.Hour)}
	down := discovery.Health{Reachable: false, LastFailure: now, Reason: discovery.ReasonUnreachable}

	cases := []struct {
		name string
		inst discovery.Instance
		want bool
	}{
		{"trusted and live", discovery.Instance{Endpoint: "a", Trusted: true, Health: live}, true},
		{"trusted but unreachable", discovery.Instance{Endpoint: "b", Trusted: true, Health: down}, false},
		{"trusted but stale", discovery.Instance{Endpoint: "c", Trusted: true, Health: stale}, false},
		{"live but untrusted", discovery.Instance{Endpoint: "d", Trusted: false, Health: live}, false},
	}

	var all []discovery.Instance
	for _, c := range cases {
		all = append(all, c.inst)
		if got := tr.Available(c.inst); got != c.want {
			t.Errorf("%s: Available = %v, want %v", c.name, got, c.want)
		}
	}

	got := tr.Filter(all)
	if len(got) != 1 || got[0].Endpoint != "a" {
		t.Fatalf("Filter kept %d instance(s) %v, want only the trusted live one", len(got), got)
	}
}
