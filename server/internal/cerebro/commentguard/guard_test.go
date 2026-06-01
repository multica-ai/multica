package commentguard

import "testing"

func TestRejectComment(t *testing.T) {
	g := New()

	cases := []struct {
		name       string
		authorType string
		content    string
		wantOK     bool
	}{
		{"member with no target passes", "member", "just a note", true},
		{"agent with no target rejected", "agent", "work is done", false},
		{"agent with empty content rejected", "agent", "", false},
		{"agent with member mention passes", "agent", "done [@Jesper](mention://member/b0edd870-4ea2-4638-a193-5c20f55170e6)", true},
		{"agent with issue link passes", "agent", "see [MUL-123](mention://issue/10fb2c2c-1ec4-449d-a081-b690ef70eb17)", true},
		{"agent with agent mention passes", "agent", "[@Tine](mention://agent/8bfccab3-89ea-40cc-b57d-c7eabaa30f50) please test", true},
		{"agent with squad mention passes", "agent", "[@Squad](mention://squad/8bfccab3-89ea-40cc-b57d-c7eabaa30f50)", true},
		{"agent with all mention passes", "agent", "[@all](mention://all/all)", true},
		{"agent text that merely looks like a mention is rejected", "agent", "talk to @Jesper soon", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg, ok := g.RejectComment(tc.authorType, tc.content)
			if ok != tc.wantOK {
				t.Fatalf("RejectComment(%q, %q) ok=%v, want %v", tc.authorType, tc.content, ok, tc.wantOK)
			}
			if !ok && msg == "" {
				t.Fatalf("rejected comment must carry a message")
			}
			if ok && msg != "" {
				t.Fatalf("passing comment must not carry a message, got %q", msg)
			}
		})
	}
}

// A nil Service must be a safe no-op so a disabled guard never blocks.
func TestRejectCommentNilService(t *testing.T) {
	var g *Service
	if msg, ok := g.RejectComment("agent", "no target here"); !ok || msg != "" {
		t.Fatalf("nil guard must pass everything, got ok=%v msg=%q", ok, msg)
	}
}
