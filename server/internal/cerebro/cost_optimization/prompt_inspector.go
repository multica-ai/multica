// CEREBRO: system prompt inspector for cost optimization (FIR-2765).
package cost_optimization

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

type promptInspectorResponse struct {
	Provider    string                    `json:"provider"`
	RepoURL     string                    `json:"repo_url,omitempty"`
	RepoNote    string                    `json:"repo_note,omitempty"`
	Inspection  execenv.MetaSkillInspection `json:"inspection"`
}

// PromptInspector returns the composed meta-skill the daemon injects for a
// representative assignment-style task in this workspace.
func (h *Handler) PromptInspector(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return
	}
	repoURL := strings.TrimSpace(r.URL.Query().Get("repo_url"))

	if h.MainQueries == nil {
		writeError(w, http.StatusInternalServerError, "prompt inspector not configured")
		return
	}
	ws, err := h.MainQueries.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		slog.Error("prompt inspector: workspace load failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load workspace")
		return
	}

	ctx := buildInspectorTaskContext(ws, repoURL)
	inspection := execenv.InspectMetaSkill("claude", ctx)

	repoNote := ""
	if repoURL != "" {
		repoNote = "Repo-specific AGENTS.md / CLAUDE.md from the checked-out repository is merged into the runtime file at task start; this view shows the server-built Multica brief only."
	}

	writeJSON(w, http.StatusOK, promptInspectorResponse{
		Provider:   "claude",
		RepoURL:    repoURL,
		RepoNote:   repoNote,
		Inspection: inspection,
	})
}

func buildInspectorTaskContext(ws db.Workspace, repoURL string) execenv.TaskContextForEnv {
	ctx := execenv.TaskContextForEnv{
		IssueID:          "00000000-0000-0000-0000-000000000001",
		WorkspaceContext: "",
	}
	if ws.Context.Valid {
		ctx.WorkspaceContext = ws.Context.String
	}
	if ws.Repos != nil {
		var repos []struct {
			URL         string `json:"url"`
			Description string `json:"description"`
		}
		if json.Unmarshal(ws.Repos, &repos) == nil {
			for _, r := range repos {
				ctx.Repos = append(ctx.Repos, execenv.RepoContextForEnv{
					URL:         r.URL,
					Description: r.Description,
				})
			}
		}
	}
	if repoURL != "" {
		// Highlight the selected repo in the Repositories section ordering.
		filtered := make([]execenv.RepoContextForEnv, 0, len(ctx.Repos)+1)
		for _, r := range ctx.Repos {
			if r.URL == repoURL {
				filtered = append(filtered, r)
			}
		}
		if len(filtered) == 0 {
			filtered = append(filtered, execenv.RepoContextForEnv{URL: repoURL})
		}
		for _, r := range ctx.Repos {
			if r.URL != repoURL {
				filtered = append(filtered, r)
			}
		}
		ctx.Repos = filtered
	}
	ctx.AgentName = "Inspector preview"
	ctx.AgentInstructions = "You are a preview agent used only to render the composed system prompt in workspace settings."
	return ctx
}
