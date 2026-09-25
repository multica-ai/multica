package util

import (
	"encoding/json"
	"reflect"
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
	t.Run("preserves distinct keys that collide after sanitization", func(t *testing.T) {
		in := map[string]any{
			"tool":     "unchanged",
			"tool\x00": "contained a NUL",
		}

		out, ok := SanitizeJSONForPostgres(in).(map[string]any)
		if !ok {
			t.Fatalf("SanitizeJSONForPostgres returned %T, want map[string]any", SanitizeJSONForPostgres(in))
		}
		if len(out) != 2 {
			t.Fatalf("sanitized key collision dropped an entry: got %v", out)
		}
		if got := out["tool"]; got != "unchanged" {
			t.Fatalf("out[tool] = %v, want unchanged value", got)
		}
		if got := out["tool#2"]; got != "contained a NUL" {
			t.Fatalf("out[tool#2] = %v, want sanitized collision value", got)
		}
	})

	t.Run("does not overwrite an existing collision suffix", func(t *testing.T) {
		in := map[string]any{
			"tool":     "unchanged",
			"tool#2":   "existing suffix",
			"tool\x00": "contained a NUL",
		}

		out := SanitizeJSONForPostgres(in).(map[string]any)
		if len(out) != 3 {
			t.Fatalf("sanitized key collision dropped an entry: got %v", out)
		}
		if got := out["tool#2"]; got != "existing suffix" {
			t.Fatalf("out[tool#2] = %v, want existing suffix value", got)
		}
		if got := out["tool#3"]; got != "contained a NUL" {
			t.Fatalf("out[tool#3] = %v, want sanitized collision value", got)
		}
	})

	t.Run("assigns multiple collisions deterministically after reserved suffixes", func(t *testing.T) {
		entries := []struct {
			key   string
			value string
		}{
			{key: "tool", value: "unchanged base"},
			{key: "tool#2", value: "unchanged suffix two"},
			{key: "tool#3", value: "unchanged suffix three"},
			{key: "\x00tool", value: "leading NUL"},
			{key: "to\x00ol", value: "middle NUL"},
			{key: "tool\x00", value: "trailing NUL"},
			{key: "tool\x00\x00", value: "two trailing NULs"},
		}
		want := map[string]any{
			"tool":   "unchanged base",
			"tool#2": "unchanged suffix two",
			"tool#3": "unchanged suffix three",
			"tool#4": "leading NUL",
			"tool#5": "middle NUL",
			"tool#6": "trailing NUL",
			"tool#7": "two trailing NULs",
		}

		var first map[string]any
		for _, reverse := range []bool{false, true} {
			in := make(map[string]any, len(entries))
			for i := range entries {
				index := i
				if reverse {
					index = len(entries) - 1 - i
				}
				in[entries[index].key] = entries[index].value
			}

			out := SanitizeJSONForPostgres(in).(map[string]any)
			if !reflect.DeepEqual(out, want) {
				t.Fatalf("reverse=%v: output = %#v, want %#v", reverse, out, want)
			}
			if first == nil {
				first = out
			} else if !reflect.DeepEqual(out, first) {
				t.Fatalf("output changed with insertion order: first=%#v, reversed=%#v", first, out)
			}

			encoded, err := json.Marshal(out)
			if err != nil {
				t.Fatalf("marshal sanitized value: %v", err)
			}
			if strings.Contains(string(encoded), "\\u0000") {
				t.Fatalf("sanitized JSON still carries a NUL escape: %s", encoded)
			}
		}
	})

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

func TestSanitizePostgresJSONMapKeysCollisionAllocationScalesLinearly(t *testing.T) {
	makeCollisions := func(size int) map[string]any {
		const base = "abcdefghijklmnop"
		values := make(map[string]any, size)
		for mask := 0; mask < size; mask++ {
			var key strings.Builder
			key.Grow(len(base) * 2)
			for index := range len(base) {
				if mask&(1<<index) != 0 {
					key.WriteByte(0)
				}
				key.WriteByte(base[index])
			}
			values[key.String()] = mask
		}
		return values
	}

	small := makeCollisions(128)
	large := makeCollisions(256)
	smallAllocs := testing.AllocsPerRun(3, func() {
		sanitizePostgresJSONMapKeys(small)
	})
	largeAllocs := testing.AllocsPerRun(3, func() {
		sanitizePostgresJSONMapKeys(large)
	})

	// Doubling a collision group may roughly double allocation work. A
	// quadratic suffix scan instead grows close to fourfold because it rebuilds
	// every already-rejected candidate for every later key.
	if largeAllocs > smallAllocs*3 {
		t.Fatalf("allocations grew superlinearly: size 128=%0.f, size 256=%0.f", smallAllocs, largeAllocs)
	}
}
