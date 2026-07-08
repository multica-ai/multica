package engine

import "testing"

func TestParseIssueCommand(t *testing.T) {
	cases := []struct {
		name      string
		body      string
		wantOK    bool
		wantTitle string
		wantDesc  string
	}{
		{"title only", "/issue Fix the login bug", true, "Fix the login bug", ""},
		{"title + description", "/issue Fix login\nIt 500s on submit\nsince Tuesday", true, "Fix login", "It 500s on submit\nsince Tuesday"},
		{"bare command", "/issue", true, "", ""},
		{"bare command with trailing space", "/issue   ", true, "", ""},
		{"leading blank lines", "\n\n/issue Title", true, "Title", ""},
		{"tab separator", "/issue\tTabbed", true, "Tabbed", ""},
		{"not a token", "/issuetracker do thing", false, "", ""},
		{"prefix mid-sentence", "hey /issue not a command", false, "", ""},
		{"case sensitive", "/Issue Title", false, "", ""},
		{"empty", "", false, "", ""},
		{"only whitespace", "   \n  ", false, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd, ok := ParseIssueCommand(tc.body)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !tc.wantOK {
				if cmd != nil {
					t.Errorf("cmd = %+v, want nil", cmd)
				}
				return
			}
			if cmd.Title != tc.wantTitle || cmd.Description != tc.wantDesc {
				t.Errorf("cmd = {%q, %q}, want {%q, %q}", cmd.Title, cmd.Description, tc.wantTitle, tc.wantDesc)
			}
		})
	}
}

func TestTitleFromPreviousMessage(t *testing.T) {
	if got := titleFromPreviousMessage("/issue Real title"); got != "Real title" {
		t.Errorf("prev /issue should strip prefix: %q", got)
	}
	if got := titleFromPreviousMessage("\n  first line\nsecond"); got != "first line" {
		t.Errorf("prev plain should take first non-empty line: %q", got)
	}
	if got := titleFromPreviousMessage("   \n  "); got != "" {
		t.Errorf("blank prev → empty: %q", got)
	}
}
