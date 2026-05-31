package semantic

import (
	"context"
	"math"
	"testing"
)

func TestDeterministicProvider_StableAcrossCalls(t *testing.T) {
	p := NewDeterministicProvider()
	a, err := p.Embed(context.Background(), "deployment failure on production")
	if err != nil {
		t.Fatalf("embed a: %v", err)
	}
	b, err := p.Embed(context.Background(), "deployment failure on production")
	if err != nil {
		t.Fatalf("embed b: %v", err)
	}
	if !vectorsEqual(a, b) {
		t.Errorf("deterministic provider returned different vectors for the same text")
	}
}

func TestDeterministicProvider_Normalised(t *testing.T) {
	p := NewDeterministicProvider()
	vec, err := p.Embed(context.Background(), "anything")
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(vec) != EmbeddingDim {
		t.Fatalf("dim mismatch: got %d want %d", len(vec), EmbeddingDim)
	}
	if !nearlyOne(l2(vec)) {
		t.Errorf("expected unit-norm vector, got L2=%g", l2(vec))
	}
}

func TestDeterministicProvider_EmptyStringHasSeededBucket(t *testing.T) {
	p := NewDeterministicProvider()
	vec, err := p.Embed(context.Background(), "")
	if err != nil {
		t.Fatalf("embed empty: %v", err)
	}
	if !nearlyOne(l2(vec)) {
		t.Errorf("empty-string vector should still be unit-norm, got L2=%g", l2(vec))
	}
}

func TestLookupProvider_OverridesAndFallsBack(t *testing.T) {
	custom := make([]float32, EmbeddingDim)
	custom[5] = 1
	p := NewLookupProvider("test:v1", map[string][]float32{
		"deployment": custom,
		"rollout":    custom, // same vector as deployment → cosine ≈ 1
	})

	a, _ := p.Embed(context.Background(), "deployment")
	b, _ := p.Embed(context.Background(), "rollout")
	if cosine(a, b) < 0.99 {
		t.Errorf("expected lookup synonyms to be near-identical, got cosine=%g", cosine(a, b))
	}

	// Fallback path: unknown phrase goes through deterministic provider.
	c, _ := p.Embed(context.Background(), "random phrase")
	if !nearlyOne(l2(c)) {
		t.Errorf("fallback vector not unit-norm")
	}
}

func TestFormatVectorLiteral_RoundtripShape(t *testing.T) {
	vec := []float32{0.1, -0.25, 0.5}
	got := FormatVectorLiteral(vec)
	want := "[0.1,-0.25,0.5]"
	if got != want {
		t.Errorf("FormatVectorLiteral = %q, want %q", got, want)
	}
}

func TestContentHash_Stable(t *testing.T) {
	a := ContentHash("hello world")
	b := ContentHash("hello world")
	if a != b {
		t.Errorf("ContentHash should be stable, got %q vs %q", a, b)
	}
	if ContentHash("hello world") == ContentHash("hello world!") {
		t.Errorf("ContentHash collided on different inputs")
	}
}

// --- helpers ---

func vectorsEqual(a, b []float32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if math.Abs(float64(a[i]-b[i])) > 1e-9 {
			return false
		}
	}
	return true
}

func l2(v []float32) float64 {
	var s float64
	for _, x := range v {
		s += float64(x) * float64(x)
	}
	return math.Sqrt(s)
}

func nearlyOne(x float64) bool { return math.Abs(x-1.0) < 1e-6 }

func cosine(a, b []float32) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
