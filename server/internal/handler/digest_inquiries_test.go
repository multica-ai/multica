package handler

import (
	"net/http"
	"sort"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// The digest's inquiries section. Its JSON contract, which the Secretary reads
// and the web client may later display:
//
//	"inquiries": [{"question": string, "answered": bool, "answer_excerpt": string | null}]
//
// The list holds the questions asked today or yesterday (UTC), newest first.
// It is always an array, even when empty. answer_excerpt is null until the squad
// answers, and then carries the first 280 characters of the reply.
func TestDigest_InquiriesOfTodayAndYesterdayOnly(t *testing.T) {
	e := newDigestEnv(t)
	sq := e.squad(t, "Inquiry squad")

	dbfx.Insert(t, "brief_inquiry", testutil.Cols{
		"workspace_id": e.WorkspaceID, "asked_by_agent_id": e.AgentID, "squad_id": sq,
		"question": "answered today", "asked_on": testutil.Raw("(now() AT TIME ZONE 'UTC')::date"),
		"answered_at": testutil.Raw("now()"), "answer_excerpt": "the reply",
	})
	dbfx.Insert(t, "brief_inquiry", testutil.Cols{
		"workspace_id": e.WorkspaceID, "asked_by_agent_id": e.AgentID, "squad_id": sq,
		"question": "asked yesterday", "asked_on": testutil.Raw("(now() AT TIME ZONE 'UTC')::date - 1"),
	})
	dbfx.Insert(t, "brief_inquiry", testutil.Cols{
		"workspace_id": e.WorkspaceID, "asked_by_agent_id": e.AgentID, "squad_id": sq,
		"question": "asked two days ago", "asked_on": testutil.Raw("(now() AT TIME ZONE 'UTC')::date - 2"),
	})

	req := newRequest("GET", "/api/company/digest", nil)
	req.Header.Set("X-Workspace-ID", e.WorkspaceID)
	res := testutil.Call(t, testHandler.GetCompanyDigest, req).Want(http.StatusOK)

	raw, ok := res.Map()["inquiries"].([]any)
	if !ok || len(raw) != 2 {
		t.Fatalf("inquiries should be an array of today's and yesterday's questions, got %#v", res.Map()["inquiries"])
	}
	for _, item := range raw {
		keys := make([]string, 0, 3)
		for k := range item.(map[string]any) {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if len(keys) != 3 || keys[0] != "answer_excerpt" || keys[1] != "answered" || keys[2] != "question" {
			t.Fatalf("inquiry keys = %v, want exactly answer_excerpt, answered, question", keys)
		}
	}

	byQuestion := map[string]map[string]any{}
	for _, item := range raw {
		m := item.(map[string]any)
		byQuestion[m["question"].(string)] = m
	}
	answered := byQuestion["answered today"]
	if answered == nil || answered["answered"] != true || answered["answer_excerpt"] != "the reply" {
		t.Errorf("answered inquiry = %#v, want answered with its excerpt", answered)
	}
	unanswered := byQuestion["asked yesterday"]
	if unanswered == nil || unanswered["answered"] != false || unanswered["answer_excerpt"] != nil {
		t.Errorf("unanswered inquiry = %#v, want answered=false and a null excerpt", unanswered)
	}
	if byQuestion["asked two days ago"] != nil {
		t.Error("an inquiry from two days ago must not be carried")
	}
}

func TestDigest_InquiriesIsAnArrayWhenEmpty(t *testing.T) {
	e := newDigestEnv(t)
	req := newRequest("GET", "/api/company/digest", nil)
	req.Header.Set("X-Workspace-ID", e.WorkspaceID)
	res := testutil.Call(t, testHandler.GetCompanyDigest, req).Want(http.StatusOK)
	raw, ok := res.Map()["inquiries"].([]any)
	if !ok || len(raw) != 0 {
		t.Fatalf("an empty inquiries section must be [], got %#v", res.Map()["inquiries"])
	}
}
