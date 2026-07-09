package service

import "testing"

func TestParseTaskPromptDirectivesGoal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		input      string
		wantMode   string
		wantPrompt string
	}{
		{
			name:       "goal on first line",
			input:      "/goal\nImplement login validation.",
			wantMode:   "goal",
			wantPrompt: "Implement login validation.",
		},
		{
			name:       "goal after leading blank lines",
			input:      "\n  \n   /goal\n\nRun all tests.",
			wantMode:   "goal",
			wantPrompt: "Run all tests.",
		},
		{
			name:       "goal with crlf",
			input:      "\r\n/goal\r\n\r\nFix Windows path handling.",
			wantMode:   "goal",
			wantPrompt: "Fix Windows path handling.",
		},
		{
			name:       "directive only leaves empty prompt",
			input:      "/goal\n\n",
			wantMode:   "goal",
			wantPrompt: "",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ParseTaskPromptDirectives(tt.input)
			if got.ExecutionMode != tt.wantMode {
				t.Fatalf("ExecutionMode = %q, want %q", got.ExecutionMode, tt.wantMode)
			}
			if got.Prompt != tt.wantPrompt {
				t.Fatalf("Prompt = %q, want %q", got.Prompt, tt.wantPrompt)
			}
		})
	}
}

func TestParseTaskPromptDirectivesNormal(t *testing.T) {
	t.Parallel()

	tests := []string{
		"Implement /goal support in docs.",
		"Implement this.\n/goal",
		"/Goal\nImplement this.",
		"/goal please\nImplement this.",
		"goal\nImplement this.",
		"",
	}

	for _, input := range tests {
		input := input
		t.Run(input, func(t *testing.T) {
			t.Parallel()

			got := ParseTaskPromptDirectives(input)
			if got.ExecutionMode != "normal" {
				t.Fatalf("ExecutionMode = %q, want normal", got.ExecutionMode)
			}
			if got.Prompt != input {
				t.Fatalf("Prompt = %q, want original %q", got.Prompt, input)
			}
		})
	}
}
