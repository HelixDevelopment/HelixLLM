package fallback

import (
	"testing"
	"time"
)

func TestCircuitBreaker_StartsClosedAllowsRequests(t *testing.T) {
	cb := NewCircuitBreaker(3, 30*time.Second)

	if cb.State() != StateClosed {
		t.Fatalf("expected StateClosed, got %v", cb.State())
	}
	if !cb.Allow() {
		t.Fatal("expected Allow() == true in closed state")
	}
}

func TestCircuitBreaker_OpensAfterMaxFailures(t *testing.T) {
	cb := NewCircuitBreaker(3, 30*time.Second)

	cb.RecordFailure()
	cb.RecordFailure()

	if cb.State() != StateClosed {
		t.Fatalf("expected StateClosed after 2 failures (threshold=3), got %v", cb.State())
	}

	cb.RecordFailure()

	if cb.State() != StateOpen {
		t.Fatalf("expected StateOpen after 3 failures, got %v", cb.State())
	}
	if cb.Allow() {
		t.Fatal("expected Allow() == false in open state")
	}
}

func TestCircuitBreaker_SuccessResetsFailureCount(t *testing.T) {
	cb := NewCircuitBreaker(3, 30*time.Second)

	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordSuccess()

	// After a success, failures should be reset; two more should not open
	cb.RecordFailure()
	cb.RecordFailure()

	if cb.State() != StateClosed {
		t.Fatalf("expected StateClosed after reset+2 failures, got %v", cb.State())
	}
}

func TestCircuitBreaker_TransitionsToHalfOpenAfterTimeout(t *testing.T) {
	cb := NewCircuitBreaker(1, 50*time.Millisecond)

	cb.RecordFailure() // opens circuit

	if cb.State() != StateOpen {
		t.Fatalf("expected StateOpen, got %v", cb.State())
	}

	// Before timeout: still open
	if cb.Allow() {
		t.Fatal("expected Allow() == false immediately after open")
	}

	time.Sleep(60 * time.Millisecond)

	// After timeout: should transition to HalfOpen on State() call
	if cb.State() != StateHalfOpen {
		t.Fatalf("expected StateHalfOpen after timeout, got %v", cb.State())
	}
	if !cb.Allow() {
		t.Fatal("expected Allow() == true in half-open state")
	}
}

func TestCircuitBreaker_HalfOpenSuccessCloses(t *testing.T) {
	cb := NewCircuitBreaker(1, 50*time.Millisecond)

	cb.RecordFailure()
	time.Sleep(60 * time.Millisecond)

	// Trigger HalfOpen transition
	_ = cb.State()

	cb.RecordSuccess()

	if cb.State() != StateClosed {
		t.Fatalf("expected StateClosed after HalfOpen+success, got %v", cb.State())
	}
}

func TestCircuitBreaker_HalfOpenFailureReopens(t *testing.T) {
	cb := NewCircuitBreaker(1, 50*time.Millisecond)

	cb.RecordFailure()
	time.Sleep(60 * time.Millisecond)

	// Trigger HalfOpen transition
	_ = cb.State()

	cb.RecordFailure()

	if cb.State() != StateOpen {
		t.Fatalf("expected StateOpen after HalfOpen+failure, got %v", cb.State())
	}
	if cb.Allow() {
		t.Fatal("expected Allow() == false after HalfOpen failure re-opens circuit")
	}
}

func TestCircuitBreaker_AllowFalseWhenOpen(t *testing.T) {
	cb := NewCircuitBreaker(2, 10*time.Second)

	cb.RecordFailure()
	cb.RecordFailure()

	if cb.State() != StateOpen {
		t.Fatalf("expected StateOpen, got %v", cb.State())
	}
	for i := 0; i < 5; i++ {
		if cb.Allow() {
			t.Fatalf("Allow() should be false when circuit is open (iteration %d)", i)
		}
	}
}
