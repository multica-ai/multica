package email

import (
	"strings"
	"testing"
)

func TestRender_MentionedIncludesActorTitleAndLink(t *testing.T) {
	r := NewRenderer("https://app.example.com")

	out := r.Render(RenderInput{
		NotifType:     "mentioned",
		IssueTitle:    "Bug: panic on startup",
		IssueID:       "11111111-1111-1111-1111-111111111111",
		WorkspaceSlug: "acme",
	})

	if !strings.Contains(out.Subject, "Bug: panic on startup") {
		t.Errorf("subject missing issue title: %q", out.Subject)
	}
	if !strings.Contains(out.HTML, "https://app.example.com/acme/issues/11111111-1111-1111-1111-111111111111") {
		t.Errorf("html missing deep link: %q", out.HTML)
	}
	if !strings.Contains(out.Text, "https://app.example.com/acme/issues/") {
		t.Errorf("text missing deep link: %q", out.Text)
	}
}

func TestRender_HTMLEscapesIssueTitle(t *testing.T) {
	r := NewRenderer("https://app.example.com")
	out := r.Render(RenderInput{
		NotifType:     "mentioned",
		IssueTitle:    `<script>alert(1)</script>`,
		IssueID:       "11111111-1111-1111-1111-111111111111",
		WorkspaceSlug: "acme",
	})
	if strings.Contains(out.HTML, "<script>alert") {
		t.Errorf("html not escaping issue title: %q", out.HTML)
	}
	if !strings.Contains(out.HTML, "&lt;script&gt;") {
		t.Errorf("html should contain escaped title: %q", out.HTML)
	}
}

func TestRender_UnknownNotifTypeFallsBackToGenericSubject(t *testing.T) {
	r := NewRenderer("https://app.example.com")
	out := r.Render(RenderInput{
		NotifType:     "some_future_type",
		IssueTitle:    "X",
		IssueID:       "11111111-1111-1111-1111-111111111111",
		WorkspaceSlug: "acme",
	})
	if !strings.Contains(out.Subject, "[Multica]") {
		t.Errorf("subject should include [Multica] prefix: %q", out.Subject)
	}
}

func TestRender_EmptyBaseURLDoesNotCrash(t *testing.T) {
	r := NewRenderer("")
	out := r.Render(RenderInput{
		NotifType:     "mentioned",
		IssueTitle:    "X",
		IssueID:       "11111111-1111-1111-1111-111111111111",
		WorkspaceSlug: "acme",
	})
	// No complete URL is required, but at least it must not panic and Subject/Text/HTML must be non-empty
	if out.Subject == "" || out.Text == "" || out.HTML == "" {
		t.Error("empty base URL should still produce a sendable email")
	}
}
