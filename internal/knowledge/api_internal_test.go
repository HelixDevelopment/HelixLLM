package knowledge

import (
	"fmt"
	"testing"
)

// TestIsValidationError exercises all branches of the unexported
// isValidationError function.
func TestIsValidationError_Nil(t *testing.T) {
	// nil error → must return false (the early-return branch).
	if isValidationError(nil) {
		t.Error("isValidationError(nil) = true, want false")
	}
}

func TestIsValidationError_ValidationMessage(t *testing.T) {
	// An error containing "must not be empty" → must return true.
	err := fmt.Errorf("ingest: content must not be empty")
	if !isValidationError(err) {
		t.Error("isValidationError(validation err) = false, want true")
	}
}

func TestIsValidationError_OtherError(t *testing.T) {
	// A non-validation error → must return false.
	err := fmt.Errorf("some store error")
	if isValidationError(err) {
		t.Error("isValidationError(other err) = true, want false")
	}
}
