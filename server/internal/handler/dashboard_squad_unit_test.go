package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestParseSquadScopeParam(t *testing.T) {
	validID := uuid.NewString()
	cases := []struct {
		name      string
		query     string
		wantScope string
		wantValid bool
		wantOK    bool
		wantCode  int
	}{
		{name: "empty", query: "", wantScope: "", wantValid: false, wantOK: true},
		{name: "standalone", query: "?squad_id=__standalone__", wantScope: "standalone", wantValid: false, wantOK: true},
		{name: "valid uuid", query: "?squad_id=" + validID, wantScope: "squad", wantValid: true, wantOK: true},
		{name: "invalid uuid", query: "?squad_id=not-a-uuid", wantScope: "", wantValid: false, wantOK: false, wantCode: http.StatusBadRequest},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/dashboard/usage/daily"+tc.query, nil)
			scope, id, ok := parseSquadScopeParam(rec, req)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if scope != tc.wantScope {
				t.Fatalf("scope = %q, want %q", scope, tc.wantScope)
			}
			if id.Valid != tc.wantValid {
				t.Fatalf("id.Valid = %v, want %v", id.Valid, tc.wantValid)
			}
			if !tc.wantOK && rec.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantCode)
			}
		})
	}
}

func TestFoldRestrictedUsageBySquadPreservesSquadBoundaries(t *testing.T) {
	restricted := map[string]struct{}{
		"priv-a": {},
		"priv-b": {},
	}
	rows := []DashboardUsageBySquadResponse{
		{SquadID: "squad-1", AgentID: "pub-1", Provider: "anthropic", Model: "claude", InputTokens: 100, TaskCount: 1},
		{SquadID: "squad-1", AgentID: "priv-a", Provider: "anthropic", Model: "claude", InputTokens: 200, TaskCount: 2},
		{SquadID: "squad-1", AgentID: "priv-b", Provider: "anthropic", Model: "claude", InputTokens: 300, TaskCount: 3},
		{SquadID: "squad-2", AgentID: "priv-a", Provider: "anthropic", Model: "claude", InputTokens: 50, TaskCount: 1},
		{SquadID: "", AgentID: "priv-b", Provider: "anthropic", Model: "claude", InputTokens: 75, TaskCount: 1},
	}

	got := foldRestrictedUsageBySquad(rows, restricted)
	if len(got) != 4 {
		t.Fatalf("len(got) = %d, want 4: %+v", len(got), got)
	}
	// squad-1 public row untouched, two private rows merged into one restricted row under squad-1
	if got[0].AgentID != "pub-1" || got[0].InputTokens != 100 {
		t.Fatalf("row[0] = %+v, want pub-1 100", got[0])
	}
	if got[1].SquadID != "squad-1" || got[1].AgentID != restrictedAgentsRowID || got[1].InputTokens != 500 || got[1].TaskCount != 5 {
		t.Fatalf("row[1] = %+v, want squad-1 restricted 500/5", got[1])
	}
	if got[2].SquadID != "squad-2" || got[2].AgentID != restrictedAgentsRowID || got[2].InputTokens != 50 {
		t.Fatalf("row[2] = %+v, want squad-2 restricted 50", got[2])
	}
	if got[3].SquadID != "" || got[3].AgentID != restrictedAgentsRowID || got[3].InputTokens != 75 {
		t.Fatalf("row[3] = %+v, want standalone restricted 75", got[3])
	}
}

func TestFoldRestrictedSquadRunTimeAndFailuresPreserveSquadBoundaries(t *testing.T) {
	restricted := map[string]struct{}{"priv-a": {}, "priv-b": {}}

	rt := foldRestrictedSquadRunTime([]DashboardSquadRunTimeResponse{
		{SquadID: "squad-1", AgentID: "priv-a", TotalSeconds: 60, TaskCount: 2, FailedCount: 1},
		{SquadID: "squad-1", AgentID: "priv-b", TotalSeconds: 40, TaskCount: 1, CancelledCount: 1},
		{SquadID: "", AgentID: "priv-a", TotalSeconds: 20, TaskCount: 1},
	}, restricted)
	if len(rt) != 2 {
		t.Fatalf("len(rt) = %d, want 2", len(rt))
	}
	if rt[0].SquadID != "squad-1" || rt[0].AgentID != restrictedAgentsRowID || rt[0].TotalSeconds != 100 || rt[0].TaskCount != 3 || rt[0].FailedCount != 1 || rt[0].CancelledCount != 1 {
		t.Fatalf("rt[0] = %+v", rt[0])
	}
	if rt[1].SquadID != "" || rt[1].AgentID != restrictedAgentsRowID || rt[1].TotalSeconds != 20 {
		t.Fatalf("rt[1] = %+v", rt[1])
	}

	fl := foldRestrictedFailuresBySquad([]DashboardFailureBySquadResponse{
		{SquadID: "squad-1", AgentID: "priv-a", FailureReason: "timeout", TaskCount: 2},
		{SquadID: "squad-1", AgentID: "priv-b", FailureReason: "timeout", TaskCount: 3},
		{SquadID: "squad-1", AgentID: "priv-a", FailureReason: "", TaskCount: 5},
		{SquadID: "squad-2", AgentID: "priv-b", FailureReason: "timeout", TaskCount: 1},
	}, restricted)
	if len(fl) != 3 {
		t.Fatalf("len(fl) = %d, want 3: %+v", len(fl), fl)
	}
	if fl[0].SquadID != "squad-1" || fl[0].FailureReason != "timeout" || fl[0].TaskCount != 5 {
		t.Fatalf("fl[0] = %+v", fl[0])
	}
}

func TestTaskToResponseIncludesSquadID(t *testing.T) {
	squadID := uuid.New()
	task := db.AgentTaskQueue{
		ID:           pgtype.UUID{Bytes: uuid.New(), Valid: true},
		AgentID:      pgtype.UUID{Bytes: uuid.New(), Valid: true},
		SquadID:      pgtype.UUID{Bytes: squadID, Valid: true},
		IsLeaderTask: false,
		Status:       "running",
	}
	resp := taskToResponse(task, "ws-1")
	if resp.SquadID != squadID.String() {
		t.Fatalf("resp.SquadID = %q, want %q", resp.SquadID, squadID.String())
	}
	if resp.IsLeaderTask {
		t.Fatalf("resp.IsLeaderTask = true, want false for delegated member task")
	}
}
