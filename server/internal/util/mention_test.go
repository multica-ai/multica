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

// TestParseMentions_PopulatesLabel pins that the visible markdown label
// (the text inside the `[ ]`, without the optional leading @) is preserved
// on the returned Mention. Dispatch-time logging compares this against the
// resolved entity's canonical name to flag label/UUID mismatch.
func TestParseMentions_PopulatesLabel(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		wantLabel string
	}{
		{
			name:      "agent mention without @",
			content:   "[Bare](mention://agent/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa)",
			wantLabel: "Bare",
		},
		{
			name:      "agent mention with @",
			content:   "[@Alice](mention://agent/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa)",
			wantLabel: "Alice",
		},
		{
			name:      "issue mention",
			content:   "[MUL-7](mention://issue/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa)",
			wantLabel: "MUL-7",
		},
		{
			name:      "all mention",
			content:   "[@all](mention://all/all)",
			wantLabel: "all",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseMentions(tt.content)
			if len(got) != 1 {
				t.Fatalf("expected 1 mention, got %d: %+v", len(got), got)
			}
			if got[0].Label != tt.wantLabel {
				t.Errorf("Label = %q, want %q", got[0].Label, tt.wantLabel)
			}
		})
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
