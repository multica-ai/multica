package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/accessdiagnostics"
)

type runtimeDiagnosticsToolsService struct {
	tools []RuntimeToolView
}

func (s runtimeDiagnosticsToolsService) ListTools(context.Context, pgtype.UUID) ([]RuntimeToolView, error) {
	return s.tools, nil
}

func (runtimeDiagnosticsToolsService) StampScanned(context.Context, pgtype.UUID) error {
	return nil
}

func TestGetRuntimeAccessDiagnosticsUsesSharedContractAndAdminGate(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	oldService := testHandler.runtimeToolsAdmin
	testHandler.runtimeToolsAdmin = runtimeDiagnosticsToolsService{tools: []RuntimeToolView{{
		Name: "get_issue", Source: "mcp", MCPServerName: "multica",
	}}}
	t.Cleanup(func() { testHandler.runtimeToolsAdmin = oldService })

	var outsiderID string
	email := fmt.Sprintf("runtime-diagnostics-%d@example.test", time.Now().UnixNano())
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO "user" (name, email) VALUES ('Runtime diagnostics member', $1) RETURNING id
	`, email).Scan(&outsiderID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id=$1`, outsiderID) })

	request := func(userID string) *httptest.ResponseRecorder {
		req := newRequest(http.MethodGet, "/api/runtimes/"+testRuntimeID+"/access-diagnostics", nil)
		req.Header.Set("X-User-ID", userID)
		req = withURLParam(req, "runtimeId", testRuntimeID)
		w := httptest.NewRecorder()
		testHandler.GetRuntimeAccessDiagnostics(w, req)
		return w
	}

	if w := request(outsiderID); w.Code != http.StatusNotFound {
		t.Fatalf("cross-workspace status = %d, want 404: %s", w.Code, w.Body.String())
	}
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1,$2,'member')
	`, testWorkspaceID, outsiderID); err != nil {
		t.Fatalf("insert member: %v", err)
	}
	if w := request(outsiderID); w.Code != http.StatusForbidden {
		t.Fatalf("member status = %d, want 403: %s", w.Code, w.Body.String())
	}

	w := request(testUserID)
	if w.Code != http.StatusOK {
		t.Fatalf("owner status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var report accessdiagnostics.RuntimeReport
	if err := json.NewDecoder(w.Body).Decode(&report); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	if report.RuntimeID != testRuntimeID || len(report.Diagnostics) != 2 ||
		report.Diagnostics[0].Code != accessdiagnostics.CodeProviderProbe ||
		report.Diagnostics[1].Code != accessdiagnostics.CodeMCPDiscovery {
		t.Fatalf("report = %#v, want shared provider and MCP diagnostics", report)
	}
}
