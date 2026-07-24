package util

import (
	"testing"
)

func TestParseMentions(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []Mention
	}{
		{
			name:    "simple agent mention",
			content: "[@Agent](mention://agent/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa) please fix",
			want:    []Mention{{Type: "agent", ID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"}},
		},
		{
			name:    "agent name with square brackets",
			content: "[@David[TF]](mention://agent/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa) please fix",
			want:    []Mention{{Type: "agent", ID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"}},
		},
		{
			name:    "agent name with nested brackets",
			content: "[@Bot[v2][beta]](mention://agent/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa) help",
			want:    []Mention{{Type: "agent", ID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"}},
		},
		{
			name:    "multiple mentions with brackets",
			content: "[@A[1]](mention://agent/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa) and [@B[2]](mention://agent/bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb)",
			want: []Mention{
				{Type: "agent", ID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"},
				{Type: "agent", ID: "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"},
			},
		},
		{
			name:    "issue mention without @",
			content: "[MUL-123](mention://issue/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa) is related",
			want:    []Mention{{Type: "issue", ID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"}},
		},
		{
			name:    "member mention",
			content: "[@Bob](mention://member/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa) look",
			want:    []Mention{{Type: "member", ID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"}},
		},
		{
			name:    "all mention",
			content: "[@All](mention://all/all) heads up",
			want:    []Mention{{Type: "all", ID: "all"}},
		},
		{
			name:    "deduplicate same mention",
			content: "[@A](mention://agent/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa) and again [@A](mention://agent/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa)",
			want:    []Mention{{Type: "agent", ID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"}},
		},
		{
			name:    "no mentions",
			content: "just a plain comment",
			want:    nil,
		},
		{
			name:    "escaped brackets in label",
			content: `[@David\[TF\]](mention://agent/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa) hi`,
			want:    []Mention{{Type: "agent", ID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseMentions(tt.content)
			if len(got) != len(tt.want) {
				t.Fatalf("ParseMentions() returned %d mentions, want %d\ngot:  %+v\nwant: %+v", len(got), len(tt.want), got, tt.want)
			}
			for i := range got {
				if got[i].Type != tt.want[i].Type || got[i].ID != tt.want[i].ID {
					t.Errorf("mention[%d] = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestIssueReferences(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []string
	}{
		{
			name:    "single issue reference",
			content: "blocked by [MUL-123](mention://issue/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa)",
			want:    []string{"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"},
		},
		{
			name:    "multiple issue references",
			content: "see [MUL-1](mention://issue/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa) and [MUL-2](mention://issue/bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb)",
			want:    []string{"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"},
		},
		{
			name:    "deduplicates repeated reference",
			content: "[MUL-1](mention://issue/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa) then [MUL-1](mention://issue/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa)",
			want:    []string{"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"},
		},
		{
			name:    "ignores member/agent/squad/all mentions",
			content: "[@Bob](mention://member/11111111-1111-1111-1111-111111111111) [@Bot](mention://agent/22222222-2222-2222-2222-222222222222) [@Team](mention://squad/33333333-3333-3333-3333-333333333333) [@All](mention://all/all)",
			want:    nil,
		},
		{
			name:    "extracts only the issue ref from mixed content",
			content: "[@Bob](mention://member/11111111-1111-1111-1111-111111111111) please look at [MUL-9](mention://issue/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa)",
			want:    []string{"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"},
		},
		{
			name:    "no references",
			content: "just a plain comment",
			want:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IssueReferences(tt.content)
			if len(got) != len(tt.want) {
				t.Fatalf("IssueReferences() returned %d ids, want %d\ngot:  %v\nwant: %v", len(got), len(tt.want), got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("id[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// TestIssueReferencesNeverRoute is the invariant guard for ITT-237: content
// that contains only issue references must yield no member/agent/squad/all
// mentions, so persisting a relation from such content can never notify a
// member or trigger an agent.
func TestIssueReferencesNeverRoute(t *testing.T) {
	content := "relates to [MUL-1](mention://issue/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa) and [MUL-2](mention://issue/bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb)"

	if got := IssueReferences(content); len(got) != 2 {
		t.Fatalf("expected 2 issue references, got %d: %v", len(got), got)
	}

	for _, m := range ParseMentions(content) {
		switch m.Type {
		case "member", "agent", "squad", "all":
			t.Errorf("issue-only content produced a routing mention %+v — would notify/trigger", m)
		}
	}
	if HasMentionAll(ParseMentions(content)) {
		t.Error("issue-only content must not be treated as @all")
	}
}

func TestHasMentionAll(t *testing.T) {
	tests := []struct {
		name     string
		mentions []Mention
		want     bool
	}{
		{"empty", nil, false},
		{"no all", []Mention{{Type: "agent", ID: "x"}}, false},
		{"has all", []Mention{{Type: "all", ID: "all"}}, true},
		{"mixed", []Mention{{Type: "agent", ID: "x"}, {Type: "all", ID: "all"}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasMentionAll(tt.mentions); got != tt.want {
				t.Errorf("HasMentionAll() = %v, want %v", got, tt.want)
			}
		})
	}
}
