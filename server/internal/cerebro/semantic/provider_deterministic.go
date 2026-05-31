package semantic

import (
	"context"
	"hash/fnv"
	"math"
	"strings"
	"unicode"
)

// DeterministicProvider is a no-network, no-dependency Provider used by
// tests and by self-host installs that do not (yet) wire a real model. It
// produces a stable bag-of-tokens projection: every word hashes to a fixed
// bucket and contributes one unit of weight, then the vector is L2-
// normalised. Two strings that share words land near each other under
// cosine — enough to exercise the hybrid ranking path without making the
// test suite hit the network.
//
// It is NOT a semantic model. Production runs DeterministicProvider only
// when the operator explicitly opts in via env. Tests that want true
// "synonym recall" use NewLookupProvider with hand-authored vectors.
type DeterministicProvider struct {
	dim int
}

// NewDeterministicProvider returns a deterministic provider at EmbeddingDim.
func NewDeterministicProvider() *DeterministicProvider {
	return &DeterministicProvider{dim: EmbeddingDim}
}

func (p *DeterministicProvider) Model() string { return "deterministic-bow:v1" }

func (p *DeterministicProvider) Embed(_ context.Context, text string) ([]float32, error) {
	vec := make([]float32, p.dim)
	tokens := tokenize(text)
	if len(tokens) == 0 {
		// Cosine on a zero vector is undefined. Seed a single deterministic
		// bucket so empty strings still have a valid unit vector.
		vec[0] = 1
		return vec, nil
	}
	for _, tok := range tokens {
		h := fnv.New32a()
		_, _ = h.Write([]byte(tok))
		bucket := int(h.Sum32() % uint32(p.dim))
		vec[bucket] += 1
	}
	return normalise(vec), nil
}

// tokenize splits on non-letter/non-digit runes and lowercases each token.
// Matches the simple FTS tokeniser well enough for the bag-of-words model.
func tokenize(text string) []string {
	if text == "" {
		return nil
	}
	fields := strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for i, f := range fields {
		fields[i] = strings.ToLower(f)
	}
	return fields
}

// normalise scales the vector to unit L2 length. Returns the same slice for
// callers that want to chain.
func normalise(vec []float32) []float32 {
	var sumSq float64
	for _, v := range vec {
		sumSq += float64(v) * float64(v)
	}
	if sumSq == 0 {
		return vec
	}
	norm := float32(math.Sqrt(sumSq))
	for i := range vec {
		vec[i] /= norm
	}
	return vec
}

// LookupProvider returns hand-authored vectors for a fixed set of phrases and
// falls back to a deterministic bag-of-words for anything else. Tests use it
// to express "rollout SHOULD be near deployment" by mapping both phrases to
// the same vector — proving the SQL retrieval picks them up — without
// shipping a real model.
type LookupProvider struct {
	fallback *DeterministicProvider
	vectors  map[string][]float32
	model    string
}

// NewLookupProvider creates a lookup provider. Phrases are lower-cased on
// lookup so callers do not need to worry about casing in their fixtures.
func NewLookupProvider(model string, vectors map[string][]float32) *LookupProvider {
	low := make(map[string][]float32, len(vectors))
	for k, v := range vectors {
		low[strings.ToLower(strings.TrimSpace(k))] = v
	}
	return &LookupProvider{
		fallback: NewDeterministicProvider(),
		vectors:  low,
		model:    model,
	}
}

func (p *LookupProvider) Model() string { return p.model }

func (p *LookupProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	key := strings.ToLower(strings.TrimSpace(text))
	if v, ok := p.vectors[key]; ok {
		out := make([]float32, len(v))
		copy(out, v)
		return normalise(out), nil
	}
	return p.fallback.Embed(ctx, text)
}
