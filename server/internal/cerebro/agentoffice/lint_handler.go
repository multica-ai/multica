package agentoffice

// HTTP layer for the context lint (FIR-1775 Phase 3). Three endpoints, all
// read-only analysis:
//
//   GET  /api/agents/{id}/context/lint        per-agent drift report
//   GET  /api/agents/context/lint             workspace-wide sweep
//   POST /api/agents/context/lint/repo-file   drift lint of a repo
//                                             CLAUDE.md/AGENTS.md's content
//
// The repo-file endpoint takes the file content in the request body because
// the server has no checkout of the repo — the CLI (or an agent) reads the
// file where it lives and posts it here to be checked against the harness.

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
)

// LintRepoFileRequest is the body of POST /api/agents/context/lint/repo-file.
type LintRepoFileRequest struct {
	Filename string `json:"filename"`
	Content  string `json:"content"`
}

// WorkspaceLintResponse aggregates the per-agent reports of a sweep.
type WorkspaceLintResponse struct {
	Agents        []LintReport `json:"agents"`
	TotalFindings int          `json:"total_findings"`
}

// LintAgent returns the drift report for one agent.
func (h *Handler) LintAgent(w http.ResponseWriter, r *http.Request) {
	agent, member, ok := h.loadAgent(w, r)
	if !ok {
		return
	}
	shared, err := h.loadLintSharedInputs(r, member.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load lint inputs: "+err.Error())
		return
	}
	report, err := h.lintOneAgent(r, agent, shared)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to lint agent: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// LintWorkspace sweeps every non-archived agent in the workspace.
func (h *Handler) LintWorkspace(w http.ResponseWriter, r *http.Request) {
	member, ok := middleware.MemberFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return
	}
	agents, err := h.Svc.Cerebro.ListAgentsForContextLint(r.Context(), member.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agents")
		return
	}
	shared, err := h.loadLintSharedInputs(r, member.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load lint inputs: "+err.Error())
		return
	}
	resp := WorkspaceLintResponse{Agents: []LintReport{}}
	for _, agent := range agents {
		report, err := h.lintOneAgent(r, agent, shared)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to lint agent "+agent.Name+": "+err.Error())
			return
		}
		resp.Agents = append(resp.Agents, report)
		resp.TotalFindings += len(report.Findings)
	}
	writeJSON(w, http.StatusOK, resp)
}

// LintRepoFile checks a repo CLAUDE.md/AGENTS.md's content for agent-behavior
// rules and lines duplicated from the workspace harness (agent instructions +
// skills).
func (h *Handler) LintRepoFile(w http.ResponseWriter, r *http.Request) {
	member, ok := middleware.MemberFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return
	}
	var req LintRepoFileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Filename) == "" {
		writeError(w, http.StatusBadRequest, "filename is required")
		return
	}
	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	agents, err := h.Svc.Cerebro.ListAgentsForContextLint(r.Context(), member.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agents")
		return
	}
	skills, err := h.Svc.Cerebro.ListWorkspaceSkillsForContextLint(r.Context(), member.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list skills")
		return
	}
	harness := make([]HarnessDoc, 0, len(agents)+len(skills))
	for _, a := range agents {
		harness = append(harness, HarnessDoc{Kind: "agent", Name: a.Name, Text: a.Instructions})
	}
	for _, s := range skills {
		harness = append(harness, HarnessDoc{Kind: "skill", Name: s.Name, Text: s.Content})
	}
	findings := LintRepoInstructionFile(req.Filename, req.Content, harness)
	writeJSON(w, http.StatusOK, map[string]any{
		"filename": req.Filename,
		"findings": findings,
	})
}

// lintSharedInputs is the workspace-level data every agent report needs; the
// sweep loads it once instead of per agent.
type lintSharedInputs struct {
	workspaceSkillNames map[string]bool
	knownRepoURLs       []string
}

func (h *Handler) loadLintSharedInputs(r *http.Request, workspaceID pgtype.UUID) (lintSharedInputs, error) {
	skills, err := h.Svc.Cerebro.ListWorkspaceSkillsForContextLint(r.Context(), workspaceID)
	if err != nil {
		return lintSharedInputs{}, err
	}
	names := make(map[string]bool, len(skills))
	for _, s := range skills {
		names[strings.ToLower(s.Name)] = true
	}
	return lintSharedInputs{
		workspaceSkillNames: names,
		knownRepoURLs:       h.loadKnownRepoURLs(r, workspaceID),
	}, nil
}

// loadKnownRepoURLs merges the workspace repos JSONB with the project-bound
// github_repo resources. Best-effort: an unreadable blob just shrinks the
// known set (the repo-link check skips itself when the set is empty).
func (h *Handler) loadKnownRepoURLs(r *http.Request, workspaceID pgtype.UUID) []string {
	urls := []string{}
	if raw, err := h.Svc.Cerebro.GetWorkspaceReposForContextLint(r.Context(), workspaceID); err == nil && len(raw) > 0 {
		var repos []struct {
			URL string `json:"url"`
		}
		if json.Unmarshal(raw, &repos) == nil {
			for _, repo := range repos {
				if repo.URL != "" {
					urls = append(urls, repo.URL)
				}
			}
		}
	}
	if refs, err := h.Svc.Cerebro.ListWorkspaceGithubRepoRefsForContextLint(r.Context(), workspaceID); err == nil {
		for _, raw := range refs {
			var ref struct {
				URL string `json:"url"`
			}
			if json.Unmarshal(raw, &ref) == nil && ref.URL != "" {
				urls = append(urls, ref.URL)
			}
		}
	}
	return urls
}

func (h *Handler) lintOneAgent(r *http.Request, agent cerebrodb.Agent, shared lintSharedInputs) (LintReport, error) {
	skillRows, err := h.Svc.Cerebro.ListAgentSkillsForContextLint(r.Context(), agent.ID)
	if err != nil {
		return LintReport{}, err
	}
	bound := make([]SkillDoc, 0, len(skillRows))
	for _, s := range skillRows {
		bound = append(bound, SkillDoc{ID: util.UUIDToString(s.ID), Name: s.Name, Content: s.Content})
	}
	findings := LintAgentContext(AgentLintInput{
		Instructions:        agent.Instructions,
		BoundSkills:         bound,
		WorkspaceSkillNames: shared.workspaceSkillNames,
		KnownRepoURLs:       shared.knownRepoURLs,
		HasContextOwner:     agent.ContextOwnerID.Valid,
		ApproverCount:       len(agent.ContextApproverIds),
	})
	return LintReport{
		AgentID:        util.UUIDToString(agent.ID),
		AgentName:      agent.Name,
		ContextVersion: agent.ContextVersion,
		Findings:       findings,
	}, nil
}
