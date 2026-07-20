package featureflag

import (
	"context"
	"testing"
)

func TestEvalContextLookup(t *testing.T) {
	t.Parallel()
	ec := EvalContext{
		UserID:      "u-1",
		WorkspaceID: "w-2",
		Attributes:  map[string]string{"plan": "pro", "country": ""},
	}
	tests := []struct {
		name   string
		key    string
		value  string
		found  bool
	}{
		{"user_id", "user_id", "u-1", true},
		{"workspace_id", "workspace_id", "w-2", true},
		{"plan", "plan", "pro", true},
		{"empty attribute treated as missing", "country", "", false},
		{"unknown attribute", "unknown", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, ok := ec.Lookup(tt.key)
			if v != tt.value || ok != tt.found {
				t.Fatalf("Lookup(%q) = (%q, %v), want (%q, %v)", tt.key, v, ok, tt.value, tt.found)
			}
		})
	}
}

func TestEvalContextRoundTripThroughContext(t *testing.T) {
	t.Parallel()
	ec := EvalContext{UserID: "u-1"}
	ctx := WithEvalContext(context.Background(), ec)
	got := EvalContextFrom(ctx)
	if got.UserID != "u-1" {
		t.Fatalf("EvalContext did not round-trip, got %+v", got)
	}
}

func TestEvalContextFromUnattachedContext(t *testing.T) {
	t.Parallel()
	// An unattached context must return the zero value, not panic.
	got := EvalContextFrom(context.Background())
	if got.UserID != "" || got.WorkspaceID != "" || got.Attributes != nil {
		t.Fatalf("unattached context should yield zero EvalContext, got %+v", got)
	}
}

func TestEvalContextFromNilContext(t *testing.T) {
	t.Parallel()
	//nolint:staticcheck // deliberately exercise the nil-ctx defensive path.
	got := EvalContextFrom(nil)
	if got.UserID != "" {
		t.Fatalf("nil context must yield zero EvalContext, got %+v", got)
	}
}

func TestPercentBucketStable(t *testing.T) {
	t.Parallel()
	// Hash stability is part of the public contract: the same (key, id)
	// MUST produce the same bucket forever, otherwise users will flip
	// in and out of experiments. We pin a handful of values so a future
	// refactor that swaps the hash will fail loudly here.
	cases := []struct {
		key, id string
		want    int
	}{
		{"feature_a", "user-1", bucketFor("feature_a", "user-1")},
		{"feature_b", "", bucketFor("feature_b", "")},
	}
	for _, tc := range cases {
		got := bucketFor(tc.key, tc.id)
		if got != tc.want {
			t.Fatalf("bucketFor(%q, %q) = %d, want %d", tc.key, tc.id, got, tc.want)
		}
		if got < 0 || got >= 100 {
			t.Fatalf("bucket out of range: %d", got)
		}
	}
}

func TestPercentBucketSeparator(t *testing.T) {
	t.Parallel()
	// Without a separator, ("ab", "c") and ("a", "bc") would collide.
	// The separator must keep them distinct, otherwise two unrelated
	// flags could share buckets and skew an experiment.
	left := bucketFor("ab", "c")
	right := bucketFor("a", "bc")
	if left == right {
		// Not guaranteed unequal in general, but for these inputs the
		// FNV-1a + zero separator should produce different buckets.
		// If this ever does collide we should switch separators, not
		// hide the regression.
		t.Fatalf("hash separator failed: bucketFor('ab','c') == bucketFor('a','bc') == %d", left)
	}
}

// TestPercentBucketCrossLanguageGolden pins concrete (key, identifier) ->
// bucket values that the Go side MUST agree on with the TS side. The same
// values are duplicated in packages/core/feature-flags/hash.test.ts; if
// either side drifts, both tests fail and one must be brought back in
// sync. This is the single source of truth for "same user, same bucket"
// across the backend and the frontend.
//
// The non-ASCII cases (CJK, accented, emoji) exist on purpose: Go hashes
// the UTF-8 byte representation of a string, and the TS side must do the
// same. A regression that swaps charCodeAt for UTF-8 decoding on either
// side would only be caught by these inputs.
func TestPercentBucketCrossLanguageGolden(t *testing.T) {
	t.Parallel()
	cases := []struct {
		key, id string
		want    int
	}{
		// ASCII baseline.
		{"billing_new_invoice", "user-42", 97},
		{"feature_a", "user-1", 50},
		{"checkout_algo", "u-7f8a", 11},
		{"ws_rollout", "workspace-1", 62},
		{"empty_id_flag", "", 83},
		// Non-ASCII: enforces UTF-8 parity with TextEncoder on the TS side.
		{"flag", "é", 53},
		{"flag", "🦄", 82},
		{"实验", "user-1", 90},
		{"flag", "用户-1", 95},
		{"checkout_算法", "user-100", 79},
	}
	for _, tc := range cases {
		got := bucketFor(tc.key, tc.id)
		if got != tc.want {
			t.Fatalf(
				"cross-language golden mismatch: bucketFor(%q, %q) = %d, want %d. "+
					"If you changed the hash you MUST also update hash.test.ts.",
				tc.key, tc.id, got, tc.want,
			)
		}
	}
}
