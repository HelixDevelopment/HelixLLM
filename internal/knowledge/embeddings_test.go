package knowledge_test

import (
	"math"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/knowledge"
)

func l2Norm(v []float64) float64 {
	var sum float64
	for _, x := range v {
		sum += x * x
	}
	return math.Sqrt(sum)
}

func TestHashEmbedder_Dimension(t *testing.T) {
	for _, dim := range []int{8, 16, 64, 128, 256} {
		e := knowledge.NewHashEmbedder(dim)
		if e.Dimension() != dim {
			t.Errorf("Dimension() = %d, want %d", e.Dimension(), dim)
		}
		vec, err := e.Embed("hello")
		if err != nil {
			t.Fatalf("Embed error: %v", err)
		}
		if len(vec) != dim {
			t.Errorf("len(vec) = %d, want %d", len(vec), dim)
		}
	}
}

func TestHashEmbedder_Deterministic(t *testing.T) {
	e := knowledge.NewHashEmbedder(64)
	v1, _ := e.Embed("deterministic test")
	v2, _ := e.Embed("deterministic test")
	for i := range v1 {
		if v1[i] != v2[i] {
			t.Errorf("dimension %d: got %f and %f, want identical", i, v1[i], v2[i])
		}
	}
}

func TestHashEmbedder_DifferentTexts(t *testing.T) {
	e := knowledge.NewHashEmbedder(64)
	v1, _ := e.Embed("the quick brown fox")
	v2, _ := e.Embed("jumped over the lazy dog")
	same := true
	for i := range v1 {
		if v1[i] != v2[i] {
			same = false
			break
		}
	}
	if same {
		t.Error("different texts produced identical vectors")
	}
}

func TestHashEmbedder_Normalized(t *testing.T) {
	e := knowledge.NewHashEmbedder(64)
	texts := []string{"hello world", "foo bar baz", "unit length check", ""}
	for _, text := range texts {
		vec, err := e.Embed(text)
		if err != nil {
			t.Fatalf("Embed(%q) error: %v", text, err)
		}
		norm := l2Norm(vec)
		// Allow a small tolerance for floating-point arithmetic.
		if math.Abs(norm-1.0) > 1e-9 {
			t.Errorf("Embed(%q): L2 norm = %f, want ~1.0", text, norm)
		}
	}
}

func TestHashEmbedder_EmbedBatch(t *testing.T) {
	e := knowledge.NewHashEmbedder(32)
	texts := []string{"alpha", "beta", "gamma"}
	batch, err := e.EmbedBatch(texts)
	if err != nil {
		t.Fatalf("EmbedBatch error: %v", err)
	}
	if len(batch) != len(texts) {
		t.Fatalf("EmbedBatch returned %d results, want %d", len(batch), len(texts))
	}
	for i, text := range texts {
		single, _ := e.Embed(text)
		for j := range single {
			if batch[i][j] != single[j] {
				t.Errorf("batch[%d][%d] = %f, Embed = %f", i, j, batch[i][j], single[j])
			}
		}
	}
}

func TestHashEmbedder_DimensionClampedToOne(t *testing.T) {
	// dimension < 1 is clamped to 1.
	e := knowledge.NewHashEmbedder(0)
	if e.Dimension() != 1 {
		t.Errorf("Dimension() = %d, want 1 (clamped from 0)", e.Dimension())
	}
	e2 := knowledge.NewHashEmbedder(-5)
	if e2.Dimension() != 1 {
		t.Errorf("Dimension() = %d, want 1 (clamped from -5)", e2.Dimension())
	}
}

func TestHashEmbedder_EmbedBatch_Empty(t *testing.T) {
	e := knowledge.NewHashEmbedder(16)
	batch, err := e.EmbedBatch([]string{})
	if err != nil {
		t.Fatalf("EmbedBatch(empty) error: %v", err)
	}
	if len(batch) != 0 {
		t.Errorf("EmbedBatch(empty) len = %d, want 0", len(batch))
	}
}

func TestHashEmbedder_Embed_ZeroNorm(t *testing.T) {
	// All-zero bytes could theoretically produce a zero-norm vector.
	// The Embed function guards against norm=0 and returns the vector as-is.
	// We call Embed with dimension=1 on a very short input to verify no panic.
	e := knowledge.NewHashEmbedder(1)
	vec, err := e.Embed("x")
	if err != nil {
		t.Fatalf("Embed error: %v", err)
	}
	if len(vec) != 1 {
		t.Errorf("len(vec) = %d, want 1", len(vec))
	}
}

func TestHashEmbedder_LargeDimension(t *testing.T) {
	// Dimension > 32 requires multiple SHA-256 rounds.
	e := knowledge.NewHashEmbedder(256)
	vec, err := e.Embed("large dimension test")
	if err != nil {
		t.Fatalf("Embed error: %v", err)
	}
	if len(vec) != 256 {
		t.Errorf("len(vec) = %d, want 256", len(vec))
	}
	norm := l2Norm(vec)
	if math.Abs(norm-1.0) > 1e-9 {
		t.Errorf("L2 norm = %f, want ~1.0", norm)
	}
}
