package service

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSanitizeSubjectField(t *testing.T) {
	long := strings.Repeat("a", 100)
	longRunes := strings.Repeat("深", 100)

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain ascii", "Acme", "Acme"},
		{"strips newline", "Acme\nEvil", "AcmeEvil"},
		{"strips crlf header-style", "Acme\r\nBcc: evil@example.com", "AcmeBcc: evil@example.com"},
		{"strips tab", "Acme\tTeam", "AcmeTeam"},
		{"strips unicode control", "Acme\x07Beep", "AcmeBeep"},
		{"preserves non-ascii", "深度学习工作区", "深度学习工作区"},
		{"preserves emoji", "Team 🚀", "Team 🚀"},
		{"truncates long ascii", long, strings.Repeat("a", maxSubjectFieldRunes-1) + "…"},
		{"truncates rune-aware", longRunes, strings.Repeat("深", maxSubjectFieldRunes-1) + "…"},
		{"empty stays empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeSubjectField(tt.in)
			if got != tt.want {
				t.Errorf("sanitizeSubjectField(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestInvitationBody_EscapesHTMLInBody(t *testing.T) {
	tests := []struct {
		name          string
		inviter       string
		workspace     string
		wantInBody    []string
		wantNotInBody []string
	}{
		{
			name:      "escapes script tag in inviter",
			inviter:   "<script>alert(1)</script>",
			workspace: "Acme",
			wantInBody: []string{
				"&lt;script&gt;alert(1)&lt;/script&gt;",
			},
			wantNotInBody: []string{
				"<script>alert(1)</script>",
			},
		},
		{
			name:      "escapes attribute-break payload in inviter",
			inviter:   `Alice" onclick="evil()`,
			workspace: "Acme",
			wantNotInBody: []string{
				`Alice" onclick="evil()`,
			},
		},
		{
			name:      "escapes anchor tag in workspace",
			inviter:   "Alice",
			workspace: `<a href="https://evil.example">Click</a>`,
			wantInBody: []string{
				"&lt;a href=",
				"&gt;Click&lt;/a&gt;",
			},
			wantNotInBody: []string{
				`<a href="https://evil.example">Click</a>`,
			},
		},
		{
			name:      "benign text unchanged",
			inviter:   "Alice",
			workspace: "Acme",
			wantInBody: []string{
				"Alice",
				"Acme",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, html := invitationBody(tt.inviter, tt.workspace, "https://app.multica.ai/invite/abc-123")
			for _, needle := range tt.wantInBody {
				if !strings.Contains(html, needle) {
					t.Errorf("body missing %q\nbody: %s", needle, html)
				}
			}
			for _, needle := range tt.wantNotInBody {
				if strings.Contains(html, needle) {
					t.Errorf("body should not contain raw %q\nbody: %s", needle, html)
				}
			}
		})
	}
}

func TestInvitationSubject_StripsControls(t *testing.T) {
	subj := invitationSubject("Alice\r\n", "Acme\t")
	if strings.ContainsAny(subj, "\r\n\t") {
		t.Errorf("subject still contains control characters: %q", subj)
	}
	if subj != "Alice invited you to Acme on Multica" {
		t.Errorf("unexpected subject: %q", subj)
	}
}

func TestInvitationSubject_NotHTMLEscaped(t *testing.T) {
	// Subject is not HTML-rendered; entities would render literally in inboxes.
	subj := invitationSubject("Alice", "Acme & Co.")
	if strings.Contains(subj, "&amp;") {
		t.Errorf("subject should not be HTML-escaped, got %q", subj)
	}
	if !strings.Contains(subj, "Acme & Co.") {
		t.Errorf("subject missing literal ampersand: %q", subj)
	}
}

func TestInvitationSubject_Truncated(t *testing.T) {
	longWorkspace := strings.Repeat("A", 200)
	subj := invitationSubject("Alice", longWorkspace)
	// Template: "Alice invited you to <ws> on Multica"
	// ws is capped at maxSubjectFieldRunes; overall subject should also be bounded.
	maxExpected := len("Alice invited you to  on Multica") + maxSubjectFieldRunes
	if runes := len([]rune(subj)); runes > maxExpected {
		t.Errorf("subject not bounded: %d runes, max %d: %q", runes, maxExpected, subj)
	}
	if !strings.Contains(subj, "…") {
		t.Errorf("truncated subject should contain ellipsis marker: %q", subj)
	}
}

func TestInvitationBody_InviteURLInHTML(t *testing.T) {
	_, html := invitationBody("Alice", "Acme", "https://app.multica.ai/invite/abc")
	if !strings.Contains(html, "https://app.multica.ai/invite/abc") {
		t.Errorf("body missing invite URL: %s", html)
	}
}

func TestCallWithTimeout_ReturnsResultOnSuccess(t *testing.T) {
	err := callWithTimeout(func() error { return nil })
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestCallWithTimeout_PropagatesError(t *testing.T) {
	sentinel := errors.New("send failed")
	err := callWithTimeout(func() error { return sentinel })
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error, got %v", err)
	}
}

func TestCallWithTimeout_TimesOut(t *testing.T) {
	// Override the package-level timeout for this test.
	orig := emailSendTimeout
	emailSendTimeout = 50 * time.Millisecond
	defer func() { emailSendTimeout = orig }()

	done := make(chan struct{})
	err := callWithTimeout(func() error {
		<-done // blocks until the test exits
		return nil
	})
	close(done)

	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("unexpected error message: %v", err)
	}
}
