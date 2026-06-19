package lark

import "testing"

func TestParseIssueCommand(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantMatch   bool
		wantTitle   string
		wantDescrip string
	}{
		{
			name:      "non command body",
			body:      "hello world",
			wantMatch: false,
		},
		{
			name:      "empty body",
			body:      "",
			wantMatch: false,
		},
		{
			name:      "issue mentioned mid-sentence does not match",
			body:      "please use /issue to file a ticket",
			wantMatch: false,
		},
		{
			name:      "wrong case rejected",
			body:      "/Issue something",
			wantMatch: false,
		},
		{
			name:      "prefix-of-token rejected",
			body:      "/issuetracker my title",
			wantMatch: false,
		},
		{
			name:      "title only",
			body:      "/issue fix login redirect",
			wantMatch: true,
			wantTitle: "fix login redirect",
		},
		{
			name:        "title and description",
			body:        "/issue fix login redirect\nuser is bounced back to landing",
			wantMatch:   true,
			wantTitle:   "fix login redirect",
			wantDescrip: "user is bounced back to landing",
		},
		{
			name:        "multi-line description preserved",
			body:        "/issue title\nline one\nline two",
			wantMatch:   true,
			wantTitle:   "title",
			wantDescrip: "line one\nline two",
		},
		{
			name:      "leading blank lines tolerated",
			body:      "\n\n/issue lazy formatter",
			wantMatch: true,
			wantTitle: "lazy formatter",
		},
		{
			name:      "command alone has no title",
			body:      "/issue",
			wantMatch: true,
			wantTitle: "",
		},
		{
			name:      "command with trailing whitespace alone has no title",
			body:      "/issue   ",
			wantMatch: true,
			wantTitle: "",
		},
		{
			name:      "tab separator accepted",
			body:      "/issue\ttabby",
			wantMatch: true,
			wantTitle: "tabby",
		},
		{
			name:        "trailing blank lines trimmed from description",
			body:        "/issue title\nbody\n\n\n",
			wantMatch:   true,
			wantTitle:   "title",
			wantDescrip: "body",
		},
		{
			name:      "lines after a non-command first line are not parsed",
			body:      "hello\n/issue not here",
			wantMatch: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd, ok := parseIssueCommand(tc.body)
			if ok != tc.wantMatch {
				t.Fatalf("match=%v want %v (cmd=%+v)", ok, tc.wantMatch, cmd)
			}
			if !tc.wantMatch {
				if cmd != nil {
					t.Fatalf("expected nil command, got %+v", cmd)
				}
				return
			}
			if cmd.Title != tc.wantTitle {
				t.Errorf("title=%q want %q", cmd.Title, tc.wantTitle)
			}
			if cmd.Description != tc.wantDescrip {
				t.Errorf("description=%q want %q", cmd.Description, tc.wantDescrip)
			}
		})
	}
}

func TestTitleFromPreviousMessage(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "plain message", body: "fix the bug", want: "fix the bug"},
		{name: "previous was /issue command, use its title", body: "/issue prior title", want: "prior title"},
		{name: "multi-line plain takes first line", body: "first line\nsecond line", want: "first line"},
		{name: "blank lines skipped", body: "\n\nactual line", want: "actual line"},
		{name: "all whitespace returns empty", body: "   \n\t\n", want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := titleFromPreviousMessage(tc.body); got != tc.want {
				t.Errorf("titleFromPreviousMessage(%q) = %q, want %q", tc.body, got, tc.want)
			}
		})
	}
}
