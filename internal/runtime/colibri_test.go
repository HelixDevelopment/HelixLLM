package runtime_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/runtime"
	"github.com/HelixDevelopment/HelixLLM/internal/vrambroker"
	"github.com/stretchr/testify/require"
)

// These exercise the two REAL adopted-runtime pieces against real things: a
// real operating-system process, and a real HTTP server. Nothing here stands in
// for a process or a response — the point of the adoption is that the lifecycle
// drives something that actually runs, and a test that proved it against a
// double would prove only that the double behaves.
//
// What they do NOT do is run Colibri itself. Colibri is a C runtime built from
// source with `make -C colibri/c`; there is no such build on this host and
// fetching and building one is not something a unit test may do. The binary is
// configuration precisely so this seam is exercisable without it, and the gap —
// no run against a real Colibri build — is stated in the report rather than
// papered over with a test that implies otherwise.

// shell returns a shell that exists on this host, skipping if none does rather
// than failing for a reason that has nothing to do with the code under test.
func shell(t *testing.T) string {
	t.Helper()
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skipf("SKIP-OK: no POSIX shell on this host, so no real process to drive: %v", err)
	}
	return sh
}

// TestExecProcessStartsAndStopsARealProcess.
//
// The process writes a file and then sleeps. The file proves it genuinely ran —
// a Start that returned nil without executing anything would leave no trace —
// and the sleep means it is still alive when Stop is called, so Stop is
// exercised against a running process rather than one that had already exited.
func TestExecProcessStartsAndStopsARealProcess(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")

	p := &runtime.ExecProcess{
		Binary:    shell(t),
		Args:      []string{"-c", "touch " + marker + "; sleep 60"},
		StopGrace: 2 * time.Second,
	}

	require.NoError(t, p.Start(context.Background()))

	require.Eventually(t, func() bool {
		_, err := os.Stat(marker)
		return err == nil
	}, 5*time.Second, 10*time.Millisecond, "the process must actually have run and written its marker")

	require.NoError(t, p.Stop(context.Background()),
		"a process ended by the signal this teardown sent is an ordinary shutdown, not a failure")
}

// TestExecProcessStopIsSafeOnAProcessThatAlreadyExited.
//
// This is the teardown of a launch that failed because the runtime died on its
// own. Reporting "it is not running" here would replace the real diagnosis with
// a complaint about the cleanup.
func TestExecProcessStopIsSafeOnAProcessThatAlreadyExited(t *testing.T) {
	p := &runtime.ExecProcess{Binary: shell(t), Args: []string{"-c", "exit 0"}, StopGrace: time.Second}

	require.NoError(t, p.Start(context.Background()))
	time.Sleep(50 * time.Millisecond) // let it exit on its own

	require.NoError(t, p.Stop(context.Background()))
	require.NoError(t, p.Stop(context.Background()), "a second stop is a no-op, not an error")
}

// TestExecProcessStopIsSafeOnAProcessThatWasNeverStarted.
//
// Session.Close and the failure paths inside Launch both tear down without
// always knowing how far the start got.
func TestExecProcessStopIsSafeOnAProcessThatWasNeverStarted(t *testing.T) {
	require.NoError(t, (&runtime.ExecProcess{Binary: "/nonexistent"}).Stop(context.Background()))
}

// TestExecProcessRefusesToStartWithNoBinaryNamed.
//
// There is no binary this package could pick on the caller's behalf without
// guessing where their build of the runtime lives, so it refuses instead.
func TestExecProcessRefusesToStartWithNoBinaryNamed(t *testing.T) {
	err := (&runtime.ExecProcess{}).Start(context.Background())
	require.ErrorIs(t, err, runtime.ErrNoBinary)
}

// TestExecProcessReportsAFailureToStart.
//
// A binary that does not exist must fail at Start, so the launcher tears down
// and releases the lease rather than waiting out a health budget for a process
// that was never there.
func TestExecProcessReportsAFailureToStart(t *testing.T) {
	err := (&runtime.ExecProcess{Binary: filepath.Join(t.TempDir(), "not-a-binary")}).Start(context.Background())
	require.Error(t, err)
}

// TestExecProcessRefusesASecondStart.
//
// The second call would abandon the first process, leaving it running with
// nothing holding a handle able to stop it.
func TestExecProcessRefusesASecondStart(t *testing.T) {
	p := &runtime.ExecProcess{Binary: shell(t), Args: []string{"-c", "sleep 30"}, StopGrace: time.Second}
	require.NoError(t, p.Start(context.Background()))
	defer func() { _ = p.Stop(context.Background()) }()

	require.ErrorIs(t, p.Start(context.Background()), runtime.ErrAlreadyStarted)
}

// TestExecProcessKillsAProcessThatIgnoresTheSignal.
//
// A runtime that traps the interrupt and keeps going must still be stopped, or
// the lease released after it would be handing back capacity the process is
// still using.
func TestExecProcessKillsAProcessThatIgnoresTheSignal(t *testing.T) {
	p := &runtime.ExecProcess{
		Binary:    shell(t),
		Args:      []string{"-c", "trap '' INT TERM; sleep 60"},
		StopGrace: 250 * time.Millisecond,
	}
	require.NoError(t, p.Start(context.Background()))

	start := time.Now()
	require.NoError(t, p.Stop(context.Background()))
	require.Less(t, time.Since(start), 10*time.Second,
		"the grace period is bounded, so an unresponsive runtime cannot hold the teardown open")
}

// TestHTTPHealthAnswersFromARealServer.
func TestHTTPHealthAnswersFromARealServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	h := runtime.HTTPHealth{URL: srv.URL, PerProbeTimeout: time.Second}
	require.True(t, h.Healthy(context.Background()))
}

// TestHTTPHealthIsUnhealthyWhileTheRuntimeIsStillLoading.
//
// A runtime that is up but not ready answers non-2xx. That is "not yet", which
// is the expected answer for most of a streaming load — the launcher keeps
// polling rather than treating it as a fault.
func TestHTTPHealthIsUnhealthyWhileTheRuntimeIsStillLoading(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	require.False(t, runtime.HTTPHealth{URL: srv.URL, PerProbeTimeout: time.Second}.Healthy(context.Background()))
}

// TestHTTPHealthIsUnhealthyWhenNothingIsListening.
//
// Connection refused is the normal answer in the seconds before the runtime
// binds its port, and is the same "not yet" as a 503.
func TestHTTPHealthIsUnhealthyWhenNothingIsListening(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // nothing is listening now

	require.False(t, runtime.HTTPHealth{URL: url, PerProbeTimeout: time.Second}.Healthy(context.Background()))
}

// TestHTTPHealthBoundsASingleProbe.
//
// One hung connection must not consume the whole health budget in a single
// attempt, or a runtime that accepts connections and never answers would be
// probed exactly once before the budget expired.
func TestHTTPHealthBoundsASingleProbe(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-block }))
	defer func() { close(block); srv.Close() }()

	h := runtime.HTTPHealth{URL: srv.URL, PerProbeTimeout: 100 * time.Millisecond}

	start := time.Now()
	require.False(t, h.Healthy(context.Background()))
	require.Less(t, time.Since(start), 5*time.Second, "the probe is bounded by its own timeout")
}

// TestHTTPHealthWithNoURLIsUnhealthyRatherThanPanicking.
func TestHTTPHealthWithNoURLIsUnhealthyRatherThanPanicking(t *testing.T) {
	require.False(t, runtime.HTTPHealth{}.Healthy(context.Background()))
}

// TestLaunchDrivesARealProcessToHealthAndTearsItDown.
//
// The lifecycle end to end on real components: a real process is started, a
// real HTTP endpoint is probed until it answers, and Close stops the process
// and releases the reservation. Only the admission gate is a double, because
// the real one reads a GPU this test host need not have.
func TestLaunchDrivesARealProcessToHealthAndTearsItDown(t *testing.T) {
	ready := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		select {
		case <-ready:
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusServiceUnavailable) // still loading weights
		}
	}))
	defer srv.Close()

	rec := &recorder{}
	admitter := &fakeAdmitter{rec: rec}
	process := &runtime.ExecProcess{Binary: shell(t), Args: []string{"-c", "sleep 60"}, StopGrace: 2 * time.Second}

	choice, entry := streamingChoice(t)
	plan, err := runtime.NewChooser().PlanLaunch(choice, entry, vrambroker.ClassCoder)
	require.NoError(t, err)

	// The runtime becomes ready shortly after launch begins, as a real one does.
	go func() { time.Sleep(30 * time.Millisecond); close(ready) }()

	l := runtime.Launcher{
		Admit:          admitter,
		Health:         runtime.HTTPHealth{URL: srv.URL, PerProbeTimeout: time.Second},
		HealthBudget:   20 * time.Second,
		HealthInterval: 10 * time.Millisecond,
	}
	session, err := l.Launch(context.Background(), plan, process)

	require.NoError(t, err, "a real process probed over real HTTP must reach a healthy session")
	require.NotNil(t, session)
	require.Zero(t, admitter.lease.releases, "a healthy session still holds its reservation")

	require.NoError(t, session.Close(context.Background()))
	require.Equal(t, 1, admitter.lease.releases, "closing returns the reservation")
}
