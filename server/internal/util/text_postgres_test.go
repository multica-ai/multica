package util

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSanitizeTextForPostgres(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "ordinary text is returned unchanged",
			in:   "worker failed: connection refused",
			want: "worker failed: connection refused",
		},
		{
			name: "multi-byte text is returned unchanged",
			in:   "任务失败：诊断详情",
			want: "任务失败：诊断详情",
		},
		{
			name: "embedded NUL is removed and surrounding text kept",
			in:   "worker failed\x00 diagnostic details",
			want: "worker failed diagnostic details",
		},
		{
			name: "UTF-16-shaped run of NULs is removed",
			in:   "w\x00o\x00r\x00k\x00e\x00r\x00",
			want: "worker",
		},
		{
			name: "a string of only NULs collapses to empty",
			in:   "\x00\x00\x00",
			want: "",
		},
		{
			name: "invalid UTF-8 becomes U+FFFD rather than vanishing",
			in:   string([]byte{'b', 'a', 'd', 0xff, 'b', 'y', 't', 'e'}),
			want: "bad\uFFFDbyte",
		},
		{
			name: "NUL removal and UTF-8 repair compose",
			in:   string([]byte{'a', 0x00, 'b', 0xff, 'c'}),
			want: "ab\uFFFDc",
		},
		{
			name: "empty stays empty",
			in:   "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SanitizeTextForPostgres(tt.in); got != tt.want {
				t.Fatalf("SanitizeTextForPostgres(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestSanitizeTextForPostgresToValidUTF8IsNotEnough pins the single fact this
// whole guard rests on: NUL is VALID UTF-8, so the strings.ToValidUTF8 call on
// its own lets it straight through to the database. If someone ever
// "simplifies" SanitizeTextForPostgres down to just ToValidUTF8, this test is
// what tells them they have reopened GH #7098.
func TestSanitizeTextForPostgresToValidUTF8IsNotEnough(t *testing.T) {
	poisoned := "worker failed\x00 diagnostic"

	if !strings.ContainsRune(strings.ToValidUTF8(poisoned, "\uFFFD"), 0) {
		t.Fatal("premise broken: strings.ToValidUTF8 now strips NUL on its own")
	}
	if strings.ContainsRune(SanitizeTextForPostgres(poisoned), 0) {
		t.Fatal("SanitizeTextForPostgres left a NUL behind")
	}
}

func TestSanitizeJSONForPostgres(t *testing.T) {
	t.Run("cleans strings at every depth, keys included", func(t *testing.T) {
		in := map[string]any{
			"cmd\x00": "cat\x00 binary",
			"args":    []any{"-n\x00", "1"},
			"nested": map[string]any{
				"deep": []any{map[string]any{"k": "v\x00"}},
			},
			"count": float64(3),
			"ok":    true,
			"null":  nil,
		}

		out, ok := SanitizeJSONForPostgres(in).(map[string]any)
		if !ok {
			t.Fatalf("SanitizeJSONForPostgres returned %T, want map[string]any", SanitizeJSONForPostgres(in))
		}

		encoded, err := json.Marshal(out)
		if err != nil {
			t.Fatalf("marshal sanitized value: %v", err)
		}
		// The marshalled form is what actually reaches the JSONB column, so
		// assert on that: encoding/json renders a surviving NUL as an escape,
		// which is precisely what PostgreSQL rejects with SQLSTATE 22P05.
		if strings.Contains(string(encoded), "\\u0000") {
			t.Fatalf("sanitized JSON still carries a NUL escape: %s", encoded)
		}

		if _, exists := out["cmd"]; !exists {
			t.Fatalf("poisoned key was not cleaned: %v", out)
		}
		if got := out["cmd"]; got != "cat binary" {
			t.Fatalf("out[cmd] = %v, want %q", got, "cat binary")
		}
		if got := out["count"]; got != float64(3) {
			t.Fatalf("non-string value was mutated: %v", got)
		}
		if got := out["ok"]; got != true {
			t.Fatalf("bool value was mutated: %v", got)
		}
		if got, exists := out["null"]; !exists || got != nil {
			t.Fatalf("null value was mutated: %v", got)
		}
	})

	t.Run("stops at the depth limit instead of recursing forever", func(t *testing.T) {
		// Build a chain deeper than the cap. The point is that this returns at
		// all rather than blowing the stack on a hostile payload.
		var deep any = "leaf\x00"
		for i := 0; i < sanitizeJSONMaxDepth+10; i++ {
			deep = map[string]any{"next": deep}
		}

		out := SanitizeJSONForPostgres(deep)

		encoded, err := json.Marshal(out)
		if err != nil {
			t.Fatalf("marshal deep value: %v", err)
		}
		if strings.Contains(string(encoded), "\\u0000") {
			t.Fatalf("deep payload still carries a NUL escape: %s", encoded)
		}
	})

	t.Run("scalars pass through", func(t *testing.T) {
		if got := SanitizeJSONForPostgres("plain"); got != "plain" {
			t.Fatalf("got %v, want plain", got)
		}
		if got := SanitizeJSONForPostgres(nil); got != nil {
			t.Fatalf("got %v, want nil", got)
		}
	})
}
