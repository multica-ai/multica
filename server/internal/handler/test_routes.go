package handler

import (
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
)

// testRoutesEnv turns on the test-only routes. They let an end-to-end test run a
// scheduled job's work on demand: the retrospective triggers run in the hourly
// retro_trigger job and nothing else calls them. The routes are off by default,
// and APP_ENV=production refuses them even when the flag is set. Runbook phase 4
// documents the flag.
const testRoutesEnv = "MULTICA_TEST_ROUTES"

// TestRoutesEnabled reports whether the test-only routes are registered.
func TestRoutesEnabled() bool {
	return strings.TrimSpace(os.Getenv(testRoutesEnv)) == "1" && !isProductionEnv()
}

// EvaluateRetrosForTest runs the retrospective triggers for one workspace now, the
// way the hourly retro_trigger job runs them for every workspace. It answers how
// many retrospectives fired. Owners and admins only.
//
//	POST /api/test/retro/evaluate
func (h *Handler) EvaluateRetrosForTest(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.companyActorFor(w, r, workspaceID); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	svc := &service.RetroService{Queries: h.Queries, Issues: h.IssueService}
	fired, err := svc.EvaluateWorkspace(r.Context(), wsUUID)
	if err != nil {
		slog.Warn("test retro evaluation failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "retro evaluation failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"fired": fired})
}
