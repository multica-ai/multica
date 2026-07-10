package sessions

// FIR-2283 followup point 2 — a session (a comment thread) started by an Issue
// workflow carries a workflow phase ("plan"/"build"/"review"). These DB tests
// prove the phase round-trips through the real session API: it is set + named in
// one PATCH, read back on the row and in the List response, normalized to lower
// case, and left untouched when a later PATCH omits it.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func callUpdate(h *Handler, issueID, workspaceID, sessionID, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(body))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("issueId", issueID)
	rctx.URLParams.Add("sessionId", sessionID)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = middleware.SetMemberContext(ctx, workspaceID, db.Member{})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	h.Update(rec, req)
	return rec
}

func callList(h *Handler, issueID, workspaceID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("issueId", issueID)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = middleware.SetMemberContext(ctx, workspaceID, db.Member{})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	h.List(rec, req)
	return rec
}

func TestUpdateNamesAndBadgesPhase_RoundTrips(t *testing.T) {
	issueID, workspaceID := seedIssue(t)
	rootID := seedRootComment(t, issueID, workspaceID)
	h := NewHandler(sessTestPool, db.New(sessTestPool), nil, nil)

	// Name + badge the session in one call, phase given as "Plan" (mixed case).
	rec := callUpdate(h, issueID, workspaceID, rootID, `{"name":"Plan","phase":"Plan"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: code=%d body=%s", rec.Code, strings.TrimSpace(rec.Body.String()))
	}
	var resp sessionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	if resp.Name != "Plan" {
		t.Fatalf("name = %q, want Plan", resp.Name)
	}
	if resp.Phase == nil || *resp.Phase != "plan" {
		t.Fatalf("phase = %v, want normalized \"plan\"", resp.Phase)
	}

	// The List endpoint (what the UI reads to render the badge) must carry it too.
	listRec := callList(h, issueID, workspaceID)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list: code=%d", listRec.Code)
	}
	var list []sessionResponse
	if err := json.Unmarshal(listRec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("session count = %d, want 1", len(list))
	}
	if list[0].Phase == nil || *list[0].Phase != "plan" {
		t.Fatalf("list phase = %v, want \"plan\"", list[0].Phase)
	}

	// A later rename that OMITS phase must leave the badge untouched (nil = keep).
	rec2 := callUpdate(h, issueID, workspaceID, rootID, `{"name":"Plan v2"}`)
	if rec2.Code != http.StatusOK {
		t.Fatalf("second update: code=%d body=%s", rec2.Code, strings.TrimSpace(rec2.Body.String()))
	}
	var resp2 sessionResponse
	_ = json.Unmarshal(rec2.Body.Bytes(), &resp2)
	if resp2.Name != "Plan v2" {
		t.Fatalf("name after second update = %q, want Plan v2", resp2.Name)
	}
	if resp2.Phase == nil || *resp2.Phase != "plan" {
		t.Fatalf("phase must persist when omitted, got %v", resp2.Phase)
	}
}

// TestUpdatePhaseAdvancesBuildToReview proves an explicit phase change (the
// build→review advance a running workflow makes) overwrites the prior badge.
func TestUpdatePhaseAdvancesBuildToReview(t *testing.T) {
	issueID, workspaceID := seedIssue(t)
	rootID := seedRootComment(t, issueID, workspaceID)
	h := NewHandler(sessTestPool, db.New(sessTestPool), nil, nil)

	if rec := callUpdate(h, issueID, workspaceID, rootID, `{"name":"Build 1","phase":"build"}`); rec.Code != http.StatusOK {
		t.Fatalf("set build: code=%d body=%s", rec.Code, strings.TrimSpace(rec.Body.String()))
	}
	rec := callUpdate(h, issueID, workspaceID, rootID, `{"name":"Review 1","phase":"review"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("set review: code=%d body=%s", rec.Code, strings.TrimSpace(rec.Body.String()))
	}
	var resp sessionResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Phase == nil || *resp.Phase != "review" {
		t.Fatalf("phase = %v, want review", resp.Phase)
	}
	if resp.Name != "Review 1" {
		t.Fatalf("name = %q, want Review 1", resp.Name)
	}
}
