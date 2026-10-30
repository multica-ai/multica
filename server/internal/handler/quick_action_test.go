package handler

import (
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// TestValidateQuickActionPromptRejectsTemplateTokens locks the guard that
// OUTLIVED the templating feature (MUL-5465).
//
// Variables were removed because every one of them named something the agent
// already had. But someone carrying the habit over from autopilot's title
// template would otherwise get `{{issue.title}}` rendered literally into an
// agent's instructions and never notice — the exact silent-typo failure the
// whitelist existed to prevent. So the rejection stays even though the feature
// is gone.
func TestValidateQuickActionPromptRejectsTemplateTokens(t *testing.T) {
	for _, tc := range []struct {
		name    string
		prompt  string
		wantErr bool
	}{
		{"plain text", "review this code", false},
		{"prose with braces-free punctuation", "review this: correctness, edge cases (all of them)", false},
		{"former variable", "review {{issue.title}}", true},
		{"former input slot", "review this. Focus: {{input}}", true},
		{"whitespace inside braces", "review {{ issue.title }}", true},
		{"unknown token", "review {{whatever}}", true},
		{"empty braces", "review {{}}", true},
		{"empty prompt", "   ", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := validateQuickActionPrompt(tc.prompt)
			if tc.wantErr && err == nil {
				t.Fatalf("expected %q to be rejected", tc.prompt)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected %q to be accepted, got %v", tc.prompt, err)
			}
		})
	}
}

// TestValidateQuickActionPromptPassesThroughVerbatim: whatever survives
// validation is exactly what gets sent. Trimming is the only transformation.
func TestValidateQuickActionPromptPassesThroughVerbatim(t *testing.T) {
	const raw = "  Review the code linked to this issue.\nFocus on correctness.  "
	got, err := validateQuickActionPrompt(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := strings.TrimSpace(raw); got != want {
		t.Fatalf("prompt must pass through verbatim:\n got: %q\nwant: %q", got, want)
	}
}

// TestValidateQuickActionPromptLimits guards the length ceilings so a
// pathological prompt cannot be stored and then re-rendered on every click.
func TestValidateQuickActionPromptLimits(t *testing.T) {
	if _, err := validateQuickActionPrompt(strings.Repeat("x", maxQuickActionPromptLen+1)); err == nil {
		t.Fatal("an over-long prompt must be rejected")
	}
	if _, err := validateQuickActionName(strings.Repeat("x", maxQuickActionNameLen+1)); err == nil {
		t.Fatal("an over-long name must be rejected")
	}
	if _, err := validateQuickActionName("  "); err == nil {
		t.Fatal("a blank name must be rejected")
	}
}

// TestNormalizeQuickActionVisibility locks the two-value intent field. An
// empty value defaults to public (the common case); anything outside the pair
// is rejected rather than silently stored.
func TestNormalizeQuickActionVisibility(t *testing.T) {
	for _, tc := range []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"", "public", false},
		{"public", "public", false},
		{"private", "private", false},
		{"workspace", "", true},
		{"owner_only", "", true},
	} {
		got, err := normalizeQuickActionVisibility(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("expected %q to be rejected", tc.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("expected %q to be accepted, got %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("normalize(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestValidateQuickActionAssignee locks the polymorphic binding to the two
// supported actor kinds; anything else would store a target the run path
// cannot resolve.
func TestValidateQuickActionAssignee(t *testing.T) {
	if err := validateQuickActionAssignee("agent", "0f1e2d3c-4b5a-6978-8796-a5b4c3d2e1f0"); err != nil {
		t.Fatalf("agent binding must be accepted, got %v", err)
	}
	if err := validateQuickActionAssignee("squad", "0f1e2d3c-4b5a-6978-8796-a5b4c3d2e1f0"); err != nil {
		t.Fatalf("squad binding must be accepted, got %v", err)
	}
	if err := validateQuickActionAssignee("member", "0f1e2d3c-4b5a-6978-8796-a5b4c3d2e1f0"); err == nil {
		t.Fatal("a member binding must be rejected: members are not runnable targets")
	}
	if err := validateQuickActionAssignee("agent", "  "); err == nil {
		t.Fatal("a blank assignee id must be rejected")
	}
}

// TestValidateQuickActionPromptRejectsSideEffectMentions locks the
// one-action / one-reached-party invariant (MUL-5465, review findings #3 and
// round-two #1).
//
// The prompt is appended verbatim to a comment that runs through the normal
// mention pipeline, so every mention inside it acts on every click: agent /
// squad / all enqueue a second target, and MEMBER creates an inbox
// notification for that person. Member was allowed in the first pass on the
// reasoning that it "only renders a link" — notification_listeners.go shows
// otherwise, so it is refused too. Only issue links reach nobody.
func TestValidateQuickActionPromptRejectsSideEffectMentions(t *testing.T) {
	const id = "0f1e2d3c-4b5a-6978-8796-a5b4c3d2e1f0"
	for _, tc := range []struct {
		name    string
		prompt  string
		wantErr bool
	}{
		{"plain prose", "review this code", false},
		{"an @ that is not mention markup", "ask @someone on the team", false},
		{"issue mention reaches nobody", "see [MUL-1](mention://issue/" + id + ")", false},
		{"member mention pings an inbox on every click", "ask [@Jia](mention://member/" + id + ")", true},
		{"agent mention would enqueue a second target", "also [@Nova](mention://agent/" + id + ")", true},
		{"squad mention would enqueue a second target", "also [@Core](mention://squad/" + id + ")", true},
		{"@all would fan out", "[@all](mention://all/all)", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := validateQuickActionPrompt(tc.prompt)
			if tc.wantErr && err == nil {
				t.Fatalf("expected %q to be rejected", tc.prompt)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected %q to be accepted, got %v", tc.prompt, err)
			}
		})
	}
}

func TestQuickActionClearsExistingAgentTarget(t *testing.T) {
	const prompt = "Please check the latest changes when you pick this up."
	agentID := dbfx.Agent(t, "Quick action target", testRuntimeID)
	var action QuickActionResponse
	testutil.Call(t, testHandler.CreateQuickAction, newRequest(http.MethodPost, "/api/quick-actions", map[string]any{
		"name": "Check changes", "prompt": prompt, "visibility": "private",
		"assignee_type": "agent", "assignee_id": agentID,
	})).Want(http.StatusCreated).JSON(&action)
	dbfx.Cleanup(t, `DELETE FROM quick_action WHERE id = $1`, action.ID)
	if action.AssigneeType != "agent" || action.AssigneeID != agentID {
		t.Fatalf("expected an existing agent target, got %+v", action)
	}

	var updated QuickActionResponse
	testutil.Call(t, testHandler.UpdateQuickAction, testutil.WithURLParams(
		newRequest(http.MethodPatch, "/api/quick-actions/"+action.ID, map[string]any{
			"assignee_type": "", "assignee_id": "",
		}), "id", action.ID,
	)).Want(http.StatusOK).JSON(&updated)
	if updated.AssigneeType != "" || updated.AssigneeID != "" {
		t.Fatalf("clearing the target must empty both fields, got type %q and id %q", updated.AssigneeType, updated.AssigneeID)
	}

	issueID := dbfx.Issue(t, "Quick action cleared target", nil)
	var preview struct {
		Content string `json:"content"`
	}
	path := "/api/issues/" + issueID + "/quick-actions/" + action.ID + "/render"
	testutil.Call(t, testHandler.RenderQuickAction, testutil.WithURLParams(
		newRequest(http.MethodPost, path, nil), "id", issueID, "quickActionId", action.ID,
	)).Want(http.StatusOK).JSON(&preview)
	if preview.Content != prompt {
		t.Fatalf("preview after clearing target = %q, want the prompt without mention markup %q", preview.Content, prompt)
	}
}

func TestQuickActionWithoutAgentPostsPromptWithoutMentions(t *testing.T) {
	const prompt = "Please check the latest changes when you pick this up."
	var action QuickActionResponse
	testutil.Call(t, testHandler.CreateQuickAction, newRequest(http.MethodPost, "/api/quick-actions", map[string]any{
		"name": "Check changes", "prompt": prompt, "visibility": "public",
	})).Want(http.StatusCreated).JSON(&action)
	dbfx.Cleanup(t, `DELETE FROM quick_action WHERE id = $1`, action.ID)

	// The issue can have an assignee without the action explicitly mentioning it.
	agentID := dbfx.Agent(t, "Issue assignee", testRuntimeID)
	issueID := dbfx.Issue(t, "Quick action message", testutil.Cols{
		"assignee_type": "agent", "assignee_id": agentID,
	})
	dbfx.Cleanup(t, `DELETE FROM comment WHERE issue_id = $1`, issueID)
	dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
	path := "/api/issues/" + issueID + "/quick-actions/" + action.ID
	request := func(suffix string) *http.Request {
		return testutil.WithURLParams(newRequest(http.MethodPost, path+suffix, nil),
			"id", issueID, "quickActionId", action.ID)
	}

	var preview struct {
		Content string `json:"content"`
	}
	testutil.Call(t, testHandler.RenderQuickAction, request("/render")).Want(http.StatusOK).JSON(&preview)
	if preview.Content != prompt {
		t.Fatalf("preview = %q, want the prompt without mention markup %q", preview.Content, prompt)
	}

	var comment CommentResponse
	testutil.Call(t, testHandler.RunQuickAction, request("/run")).Want(http.StatusCreated).JSON(&comment)
	if comment.ID == "" || comment.IssueID != issueID || comment.Content != prompt {
		t.Fatalf("expected an issue comment containing only the prompt, got %+v", comment)
	}
	if comment.QuickActionID == nil || *comment.QuickActionID != action.ID {
		t.Fatalf("comment must retain its quick action attribution, got %+v", comment.QuickActionID)
	}
	if len(comment.TriggerOutcomes) != 0 {
		t.Fatalf("a mention-free action must not explicitly trigger a target, got %+v", comment.TriggerOutcomes)
	}
}
