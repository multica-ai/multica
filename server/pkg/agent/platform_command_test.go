package agent

import "testing"

func TestIsTrustedReadOnlyPlatformCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		command string
		want    bool
	}{
		{"which multica", "which multica", true},
		{"issue get", "multica issue get TES-1 --output json", true},
		{"absolute issue get", "/Users/admin/project/server/bin/multica issue get 123 --output json", true},
		{"comment add is write", "multica issue comment add TES-1 --content hi", false},
		{"shell chain rejected", "multica issue get TES-1 --output json && rm -rf /tmp/x", false},
		{"substitution rejected", "multica issue get $(cat secret)", false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isTrustedReadOnlyPlatformCommand(tt.command); got != tt.want {
				t.Fatalf("isTrustedReadOnlyPlatformCommand(%q) = %v, want %v", tt.command, got, tt.want)
			}
		})
	}
}
