package handler

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// validBriefBody has every required heading in order.
func validBriefBody() string {
	return strings.Join([]string{
		"## Headline", "Nothing needs you today.", "",
		"## Needs your decision", "None.", "",
		"## Progress", "Research shipped the pricing teardown.", "",
		"## Blockers and risks", "None.", "",
		"## Watch", "A competitor moved to usage pricing.", "",
		"## FYI", "Spend is in line.", "",
	}, "\n")
}

func briefRequestBody(over map[string]any) map[string]any {
	body := map[string]any{"kind": "daily", "title": "Daily brief", "body": validBriefBody()}
	for k, v := range over {
		body[k] = v
	}
	return body
}

// publishAs posts a brief to the digest env's workspace as the given actor.
// An empty userID means the default test user (an owner of the workspace).
func (e digestEnv) publish(t *testing.T, userID, agentID string, body map[string]any) *testutil.Response {
	t.Helper()
	req := newRequest("POST", "/api/briefs", body)
	req.Header.Set("X-Workspace-ID", e.WorkspaceID)
	if userID != "" {
		req = asUser(req, userID)
	}
	if agentID != "" {
		taskID := dbfx.Task(t, agentID, testutil.Cols{"status": "running", "runtime_id": e.RuntimeID})
		req = asAgent(req, agentID, taskID)
	}
	return testutil.Call(t, testHandler.CreateBrief, req)
}

func (e digestEnv) setSecretary(t *testing.T, agentID string) {
	t.Helper()
	dbfx.Exec(t, `UPDATE workspace SET secretary_agent_id = $1 WHERE id = $2`, agentID, e.WorkspaceID)
}

func (e digestEnv) otherAgent(t *testing.T) string {
	t.Helper()
	return dbfx.Agent(t, "Other agent "+uuid.NewString(), e.RuntimeID, testutil.Cols{"workspace_id": e.WorkspaceID})
}

func TestPublishBrief_Permissions(t *testing.T) {
	e := newDigestEnv(t)
	secretary := e.AgentID
	e.setSecretary(t, secretary)
	other := e.otherAgent(t)
	admin := newWorkspaceMemberUserIn(t, e.WorkspaceID, "admin")
	member := newWorkspaceMemberUserIn(t, e.WorkspaceID, "member")

	// The secretary agent, an admin and an owner may publish.
	var resp BriefResponse
	e.publish(t, "", secretary, briefRequestBody(nil)).Want(http.StatusCreated).JSON(&resp)
	if resp.CreatedByType != "agent" || resp.CreatedByID != secretary || resp.Kind != "daily" || resp.WorkspaceID != e.WorkspaceID {
		t.Fatalf("secretary brief wrong: %+v", resp)
	}
	e.publish(t, admin, "", briefRequestBody(nil)).Want(http.StatusCreated).JSON(&resp)
	if resp.CreatedByType != "member" || resp.CreatedByID != admin {
		t.Fatalf("admin brief wrong: %+v", resp)
	}
	e.publish(t, "", "", briefRequestBody(nil)).Want(http.StatusCreated)

	// Any other agent, and a plain member, may not.
	e.publish(t, "", other, briefRequestBody(nil)).Want(http.StatusForbidden)
	e.publish(t, member, "", briefRequestBody(nil)).Want(http.StatusForbidden)

	// With no secretary set, no agent may publish.
	dbfx.Exec(t, `UPDATE workspace SET secretary_agent_id = NULL WHERE id = $1`, e.WorkspaceID)
	e.publish(t, "", secretary, briefRequestBody(nil)).Want(http.StatusForbidden)
}

func TestPublishBrief_SecretaryOfAnotherWorkspaceIsNotTheSecretary(t *testing.T) {
	mine := newDigestEnv(t)
	theirs := newDigestEnv(t)
	theirs.setSecretary(t, theirs.AgentID)
	// Their secretary, acting for a plain member of my workspace, is refused:
	// the agent is not in this workspace, so only the member's role counts.
	member := newWorkspaceMemberUserIn(t, mine.WorkspaceID, "member")
	mine.publish(t, member, theirs.AgentID, briefRequestBody(nil)).Want(http.StatusForbidden)
}

func TestPublishBrief_HeadingValidation(t *testing.T) {
	e := newDigestEnv(t)

	missing := strings.Replace(validBriefBody(), "## Watch", "### Watch", 1)
	w := e.publish(t, "", "", briefRequestBody(map[string]any{"body": missing})).Want(http.StatusBadRequest)
	if msg := w.Map()["error"].(string); !strings.Contains(msg, "## Watch") || !strings.Contains(msg, "in this order") {
		t.Fatalf("the error should name the missing heading and the layout: %s", msg)
	}

	// Headings inside a code fence do not count.
	fenced := "```\n" + validBriefBody() + "\n```"
	e.publish(t, "", "", briefRequestBody(map[string]any{"body": fenced})).Want(http.StatusBadRequest)

	// Out of order.
	swapped := strings.Replace(strings.Replace(validBriefBody(), "## Progress", "## @@", 1), "## Needs your decision", "## Progress", 1)
	swapped = strings.Replace(swapped, "## @@", "## Needs your decision", 1)
	w = e.publish(t, "", "", briefRequestBody(map[string]any{"body": swapped})).Want(http.StatusBadRequest)
	if msg := w.Map()["error"].(string); !strings.Contains(msg, "out of order") {
		t.Fatalf("want an out-of-order error, got: %s", msg)
	}

	// Twice.
	w = e.publish(t, "", "", briefRequestBody(map[string]any{"body": validBriefBody() + "\n## FYI\nagain"})).Want(http.StatusBadRequest)
	if msg := w.Map()["error"].(string); !strings.Contains(msg, "twice") {
		t.Fatalf("want a duplicate error, got: %s", msg)
	}

	// Empty body, and extra sections and different casing are fine.
	e.publish(t, "", "", briefRequestBody(map[string]any{"body": ""})).Want(http.StatusBadRequest)
	lenient := strings.Replace(validBriefBody(), "## Watch", "## watch", 1) + "\n## Appendix\nextra"
	e.publish(t, "", "", briefRequestBody(map[string]any{"body": lenient})).Want(http.StatusCreated)
}

func TestPublishBrief_SizeAndFieldValidation(t *testing.T) {
	e := newDigestEnv(t)

	atLimit := validBriefBody() + strings.Repeat("x", briefMaxBodyLen-len(validBriefBody())-1)
	if len(strings.TrimSpace(atLimit)) > briefMaxBodyLen {
		t.Fatalf("test body is over the limit")
	}
	e.publish(t, "", "", briefRequestBody(map[string]any{"body": atLimit})).Want(http.StatusCreated)

	tooLong := validBriefBody() + strings.Repeat("x", briefMaxBodyLen)
	w := e.publish(t, "", "", briefRequestBody(map[string]any{"body": tooLong})).Want(http.StatusBadRequest)
	if msg := w.Map()["error"].(string); !strings.Contains(msg, "20000") {
		t.Fatalf("the size error should state the limit: %s", msg)
	}

	e.publish(t, "", "", briefRequestBody(map[string]any{"kind": "hourly"})).Want(http.StatusBadRequest)
	e.publish(t, "", "", briefRequestBody(map[string]any{"title": "   "})).Want(http.StatusBadRequest)
	e.publish(t, "", "", briefRequestBody(map[string]any{"title": strings.Repeat("t", briefMaxTitleLen+1)})).Want(http.StatusBadRequest)
	e.publish(t, "", "", briefRequestBody(map[string]any{"period_start": "2026-05-02T00:00:00Z", "period_end": "2026-05-01T00:00:00Z"})).Want(http.StatusBadRequest)
	e.publish(t, "", "", briefRequestBody(map[string]any{"period_end": "yesterday"})).Want(http.StatusBadRequest)
}

func TestPublishBrief_PeriodDefaultsByKind(t *testing.T) {
	e := newDigestEnv(t)
	var resp BriefResponse
	e.publish(t, "", "", briefRequestBody(map[string]any{"kind": "weekly"})).Want(http.StatusCreated).JSON(&resp)
	start, _ := time.Parse(time.RFC3339, resp.PeriodStart)
	end, _ := time.Parse(time.RFC3339, resp.PeriodEnd)
	if d := end.Sub(start); d != 7*24*time.Hour {
		t.Fatalf("weekly period = %s, want 168h", d)
	}
}

func TestPublishBrief_ProposalIDs(t *testing.T) {
	e := newDigestEnv(t)
	sq := e.squad(t, "Research")
	p1 := e.pendingProposal(t, sq, "Pilot pricing")
	other := newDigestEnv(t)
	foreign := other.pendingProposal(t, other.squad(t, "Theirs"), "Their proposal")

	var resp BriefResponse
	e.publish(t, "", "", briefRequestBody(map[string]any{"proposal_ids": []string{p1, p1}})).Want(http.StatusCreated).JSON(&resp)
	if len(resp.ProposalIDs) != 1 || resp.ProposalIDs[0] != p1 {
		t.Fatalf("proposal_ids = %v, want the one proposal once", resp.ProposalIDs)
	}

	// A proposal of another workspace, a made-up id and a non-id are refused.
	e.publish(t, "", "", briefRequestBody(map[string]any{"proposal_ids": []string{foreign}})).Want(http.StatusBadRequest)
	e.publish(t, "", "", briefRequestBody(map[string]any{"proposal_ids": []string{uuid.NewString()}})).Want(http.StatusBadRequest)
	e.publish(t, "", "", briefRequestBody(map[string]any{"proposal_ids": []string{"nope"}})).Want(http.StatusBadRequest)

	// A brief with no proposals lists an empty array, not null.
	e.publish(t, "", "", briefRequestBody(nil)).Want(http.StatusCreated).JSON(&resp)
	if resp.ProposalIDs == nil || len(resp.ProposalIDs) != 0 {
		t.Fatalf("proposal_ids = %#v, want []", resp.ProposalIDs)
	}
}

func TestBriefs_ListGetAndIsolation(t *testing.T) {
	mine := newDigestEnv(t)
	theirs := newDigestEnv(t)
	var first, second, foreign BriefResponse
	mine.publish(t, "", "", briefRequestBody(map[string]any{"title": "First"})).Want(http.StatusCreated).JSON(&first)
	mine.publish(t, "", "", briefRequestBody(map[string]any{"title": "Second", "kind": "weekly"})).Want(http.StatusCreated).JSON(&second)
	theirs.publish(t, "", "", briefRequestBody(map[string]any{"title": "Theirs"})).Want(http.StatusCreated).JSON(&foreign)

	list := func(query string) []BriefResponse {
		req := newRequest("GET", "/api/briefs"+query, nil)
		req.Header.Set("X-Workspace-ID", mine.WorkspaceID)
		var out []BriefResponse
		testutil.Call(t, testHandler.ListBriefs, req).Want(http.StatusOK).JSON(&out)
		return out
	}
	all := list("")
	if len(all) != 2 || all[0].ID != second.ID || all[1].ID != first.ID {
		t.Fatalf("list should be this workspace's briefs, newest first: %+v", all)
	}
	if weekly := list("?kind=weekly"); len(weekly) != 1 || weekly[0].ID != second.ID {
		t.Fatalf("kind filter wrong: %+v", weekly)
	}
	if one := list("?limit=1"); len(one) != 1 || one[0].ID != second.ID {
		t.Fatalf("limit wrong: %+v", one)
	}
	req := newRequest("GET", "/api/briefs?limit=0", nil)
	req.Header.Set("X-Workspace-ID", mine.WorkspaceID)
	testutil.Call(t, testHandler.ListBriefs, req).Want(http.StatusBadRequest)

	get := func(id string) *testutil.Response {
		req := withURLParam(newRequest("GET", "/api/briefs/"+id, nil), "id", id)
		req.Header.Set("X-Workspace-ID", mine.WorkspaceID)
		return testutil.Call(t, testHandler.GetBrief, req)
	}
	var got BriefResponse
	get(first.ID).Want(http.StatusOK).JSON(&got)
	if got.Title != "First" || got.Body != validBriefBody() && got.Body != strings.TrimSpace(validBriefBody()) {
		t.Fatalf("brief round trip wrong: %+v", got)
	}
	get(foreign.ID).Want(http.StatusNotFound)

	// A plain member may read them.
	member := newWorkspaceMemberUserIn(t, mine.WorkspaceID, "member")
	req = asUser(newRequest("GET", "/api/briefs", nil), member)
	req.Header.Set("X-Workspace-ID", mine.WorkspaceID)
	testutil.Call(t, testHandler.ListBriefs, req).Want(http.StatusOK)

	// And a stranger may not.
	stranger := dbfx.User(t, "Stranger", fmt.Sprintf("brief-stranger-%d@example.com", time.Now().UnixNano()))
	req = asUser(newRequest("GET", "/api/briefs", nil), stranger)
	req.Header.Set("X-Workspace-ID", mine.WorkspaceID)
	testutil.Call(t, testHandler.ListBriefs, req).WantOneOf(http.StatusForbidden, http.StatusNotFound)
}
