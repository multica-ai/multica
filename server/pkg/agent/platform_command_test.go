package agent

import "testing"

func TestIsTrustedPlatformCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		command string
		want    bool
	}{
		{"which multica", "which multica", true},
		{"issue get", "multica issue get TES-1 --output json", true},
		{"absolute issue get", "/Users/admin/project/server/bin/multica issue get 123 --output json", true},
		{"comment list", "multica issue comment list TES-1 --output json", true},
		{"comment add is trusted", "multica issue comment add TES-1 --content hi", true},
		{"heredoc pipe into comment add", "cat <<'COMMENT' | multica issue comment add TES-1 --content-stdin\n已删除 `image.png`\n路径：`/Users/admin/image.png`\nCOMMENT", true},
		{"echo pipe into comment add", "echo hi | /Users/admin/project/server/bin/multica issue comment add 123 --content-stdin", true},
		{"shell chain rejected", "multica issue get TES-1 --output json && rm -rf /tmp/x", false},
		{"non-multica command rejected", "rm -rf /tmp/x", false},
		{"mixed pipeline rejected", "rm -rf /tmp/x | multica issue comment add TES-1 --content-stdin", false},
		{"substitution rejected", "multica issue get $(cat secret)", false},
		{"backslash n in content is trusted", "multica issue comment add TES-1 --content \"hello \\\\n world\"", true},
		{"backslash underscore in content is trusted", "multica issue comment add TES-1 --content \"test\\_agent\"", true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isTrustedPlatformCommand(tt.command); got != tt.want {
				t.Fatalf("isTrustedPlatformCommand(%q) = %v, want %v", tt.command, got, tt.want)
			}
		})
	}
}
