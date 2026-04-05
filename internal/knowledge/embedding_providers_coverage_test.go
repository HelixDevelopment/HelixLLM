package knowledge

import (
	"testing"
)

// ---------------------------------------------------------------------------
// float32SliceToFloat64
// ---------------------------------------------------------------------------

func TestFloat32SliceToFloat64_Empty(t *testing.T) {
	result := float32SliceToFloat64(nil)
	if len(result) != 0 {
		t.Errorf("expected empty slice, got %d elements", len(result))
	}
}

func TestFloat32SliceToFloat64_Values(t *testing.T) {
	input := []float32{1.0, 2.5, 3.75}
	result := float32SliceToFloat64(input)
	if len(result) != 3 {
		t.Fatalf("expected 3 elements, got %d", len(result))
	}
	expected := []float64{1.0, 2.5, 3.75}
	for i, v := range result {
		if v != expected[i] {
			t.Errorf("result[%d] = %f, want %f", i, v, expected[i])
		}
	}
}

func TestFloat32SliceToFloat64_SingleElement(t *testing.T) {
	result := float32SliceToFloat64([]float32{0.42})
	if len(result) != 1 {
		t.Fatalf("expected 1 element, got %d", len(result))
	}
	// float32 to float64 conversion should preserve approximate value.
	if result[0] < 0.41 || result[0] > 0.43 {
		t.Errorf("result[0] = %f, expected approximately 0.42", result[0])
	}
}

// ---------------------------------------------------------------------------
// NewOpenAIEmbedder validation
// ---------------------------------------------------------------------------

func TestNewOpenAIEmbedder_EmptyAPIKey(t *testing.T) {
	_, err := NewOpenAIEmbedder("", "text-embedding-3-small")
	if err == nil {
		t.Fatal("expected error for empty API key, got nil")
	}
}

func TestNewOpenAIEmbedder_ValidKey(t *testing.T) {
	e, err := NewOpenAIEmbedder("test-key", "text-embedding-3-small")
	if err != nil {
		t.Fatalf("NewOpenAIEmbedder: %v", err)
	}
	if e == nil {
		t.Fatal("expected non-nil embedder")
	}
	if e.Dimension() == 0 {
		t.Error("expected non-zero dimension")
	}
}
