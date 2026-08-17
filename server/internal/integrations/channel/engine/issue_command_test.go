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

func TestIssueDescriptionFromCommandBodyPreservesInlineLayout(t *testing.T) {
	body := "/issue explain below questions\nWhat is this?\n[Image]\nAnd what is this?\n[Image]"
	want := "What is this?\n[Image]\nAnd what is this?\n[Image]"
	if got := issueDescriptionFromCommandBody(body, "/issue explain below questions\nWhat is this?And what is this?", "flattened fallback"); got != want {
		t.Fatalf("description = %q, want %q", got, want)
	}
}

func TestIssueDescriptionFromCommandBodyExcludesEnrichedPrefix(t *testing.T) {
	body := "> quoted context\n/issue Real intent\nrepro steps"
	if got := issueDescriptionFromCommandBody(body, "/issue Real intent\nrepro steps", "fallback"); got != "repro steps" {
		t.Fatalf("description = %q, want %q", got, "repro steps")
	}
}

func TestIssueDescriptionFromCommandBodyIgnoresIssueLinesInEnrichedPrefix(t *testing.T) {
	body := "<quoted_message>\n/issue Old intent\n</quoted_message>\n/issue Real intent\nrepro steps"
	if got := issueDescriptionFromCommandBody(body, "/issue Real intent\nrepro steps", "fallback"); got != "repro steps" {
		t.Fatalf("description = %q, want %q", got, "repro steps")
	}
}

func TestIssueDescriptionFromCommandBodyHandlesRepeatedDirectiveLine(t *testing.T) {
	body := "<quoted_message>\n/issue Same\n</quoted_message>\n/issue Same\nDetails\n/issue Same"
	commandText := "/issue Same\nDetails\n/issue Same"
	if got := issueDescriptionFromCommandBody(body, commandText, "fallback"); got != "Details\n/issue Same" {
		t.Fatalf("description = %q, want %q", got, "Details\\n/issue Same")
	}
}

func TestIssueDescriptionFromCommandBodyFallsBackWithoutDirective(t *testing.T) {
	if got := issueDescriptionFromCommandBody("rewritten body", "/issue Missing", "parsed description"); got != "parsed description" {
		t.Fatalf("description = %q, want parsed fallback", got)
	}
}
