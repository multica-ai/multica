package skillbundle

import "testing"

func TestNormalizeName(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"Runner Health":   "runner-health",
		" runner_health ": "runner-health",
		"---":             "skill",
	}
	for input, want := range tests {
		input, want := input, want
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			if got := NormalizeName(input); got != want {
				t.Fatalf("NormalizeName(%q) = %q, want %q", input, got, want)
			}
		})
	}
}
