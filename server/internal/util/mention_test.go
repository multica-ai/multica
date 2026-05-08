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

func TestBroadcastMentions(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		wantMention Mention
		wantTag     string
	}{
		{
			name:        "broadcast all agents",
			content:     "[@@](mention://broadcast/all) everyone",
			wantMention: Mention{Type: "broadcast", ID: "all"},
			wantTag:     "",
		},
		{
			name:        "broadcast tag-scoped",
			content:     "[@@coding](mention://broadcast/coding) please review",
			wantMention: Mention{Type: "broadcast", ID: "coding"},
			wantTag:     "coding",
		},
		{
			name:        "broadcast with hyphenated tag",
			content:     "[@@on-call](mention://broadcast/on-call) incident",
			wantMention: Mention{Type: "broadcast", ID: "on-call"},
			wantTag:     "on-call",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseMentions(tt.content)
			if len(got) != 1 {
				t.Fatalf("ParseMentions() returned %d mentions, want 1\ngot: %+v", len(got), got)
			}
			m := got[0]
			if m.Type != tt.wantMention.Type || m.ID != tt.wantMention.ID {
				t.Errorf("mention = %+v, want %+v", m, tt.wantMention)
			}
			if !m.IsBroadcast() {
				t.Errorf("IsBroadcast() = false, want true")
			}
			if tag := m.BroadcastTag(); tag != tt.wantTag {
				t.Errorf("BroadcastTag() = %q, want %q", tag, tt.wantTag)
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
