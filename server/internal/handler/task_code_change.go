package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/redact"
)

// Run code changes. When a run ends, the daemon reports what the agent
// changed in each git checkout under the run's working directory (the diff
// from the commit the run started on to the working tree, untracked files
// included). Members read it back from the run's row in the execution log, so
// the changes stay viewable after the daemon removes the working directory.

const (
	// The daemon trims a report to 3 MiB encoded before upload; this limit
	// only stops a misbehaving client from making the server buffer an
	// unbounded document.
	maxTaskCodeChangeBodyBytes = 4 << 20
	maxTaskCodeChangeRepos     = 20
	maxTaskCodeChangeFiles     = 1000
)

type taskCodeChangeFile struct {
	Path      string `json:"path"`
	OldPath   string `json:"old_path,omitempty"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Binary    bool   `json:"binary,omitempty"`
}

// taskCodeChangeRepo is one checkout's changes. Path is relative to the run's
// working directory ("." when the working directory is the checkout).
type taskCodeChangeRepo struct {
	Path           string               `json:"path"`
	Branch         string               `json:"branch,omitempty"`
	Base           string               `json:"base"`
	Head           string               `json:"head,omitempty"`
	Files          []taskCodeChangeFile `json:"files"`
	FilesTruncated bool                 `json:"files_truncated,omitempty"`
	Patch          string               `json:"patch"`
	PatchTruncated bool                 `json:"patch_truncated,omitempty"`
}

type taskCodeChangeReport struct {
	Repos []taskCodeChangeRepo `json:"repos"`
	// ReposTruncated: some checkouts are not in Repos (the daemon's scan or
	// upload budget, or the server's repo cap below).
	ReposTruncated bool `json:"repos_truncated,omitempty"`
}

type TaskCodeChangesResponse struct {
	TaskID         string               `json:"task_id"`
	Repos          []taskCodeChangeRepo `json:"repos"`
	ReposTruncated bool                 `json:"repos_truncated"`
	// UpdatedAt is null until the daemon has reported for this run.
	UpdatedAt *string `json:"updated_at"`
}

// normalize applies the server's caps and makes every string storable.
// Over-cap input is trimmed and flagged rather than rejected: losing a whole
// report because one checkout had too many files helps nobody.
func (r *taskCodeChangeReport) normalize() {
	if r.Repos == nil {
		r.Repos = []taskCodeChangeRepo{}
	}
	if len(r.Repos) > maxTaskCodeChangeRepos {
		r.Repos = r.Repos[:maxTaskCodeChangeRepos]
		r.ReposTruncated = true
	}
	for i := range r.Repos {
		repo := &r.Repos[i]
		if repo.Files == nil {
			repo.Files = []taskCodeChangeFile{}
		}
		if len(repo.Files) > maxTaskCodeChangeFiles {
			repo.Files = repo.Files[:maxTaskCodeChangeFiles]
			repo.FilesTruncated = true
		}
		// JSONB rejects NUL, and a patch is file content: mask secret-shaped
		// text here as the transcript ingest does, even though the daemon
		// already redacted before upload.
		repo.Path = util.SanitizeTextForPostgres(repo.Path)
		repo.Branch = util.SanitizeTextForPostgres(repo.Branch)
		repo.Base = util.SanitizeTextForPostgres(repo.Base)
		repo.Head = util.SanitizeTextForPostgres(repo.Head)
		repo.Patch = util.SanitizeTextForPostgres(redact.Text(repo.Patch))
		for j := range repo.Files {
			f := &repo.Files[j]
			f.Path = util.SanitizeTextForPostgres(f.Path)
			f.OldPath = util.SanitizeTextForPostgres(f.OldPath)
			switch f.Status {
			case "added", "modified", "deleted", "renamed":
			default:
				f.Status = "modified"
			}
		}
	}
}

// ReportTaskCodeChanges stores the daemon's snapshot for a task, replacing
// any earlier one.
func (h *Handler) ReportTaskCodeChanges(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxTaskCodeChangeBodyBytes)
	task, workspaceID, ok := h.requireDaemonTaskAccessWithWorkspace(w, r, chi.URLParam(r, "taskId"))
	if !ok {
		return
	}

	var req taskCodeChangeReport
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "code changes too large")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.normalize()
	repos, err := json.Marshal(req.Repos)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode code changes")
		return
	}
	if err := h.Queries.UpsertTaskCodeChange(r.Context(), db.UpsertTaskCodeChangeParams{
		TaskID:         task.ID,
		WorkspaceID:    parseUUID(workspaceID),
		IssueID:        task.IssueID,
		Repos:          repos,
		ReposTruncated: req.ReposTruncated,
	}); err != nil {
		slog.Warn("store task code changes failed", "task_id", uuidToString(task.ID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to store code changes")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetTaskCodeChanges returns a run's code changes to a member who may read
// the run's transcript. A run without a snapshot (still running, an older
// daemon, or a run with no git checkout) answers 200 with no repos and a null
// updated_at.
func (h *Handler) GetTaskCodeChanges(w http.ResponseWriter, r *http.Request) {
	task, ok := h.loadTaskForUser(w, r)
	if !ok {
		return
	}
	resp := TaskCodeChangesResponse{TaskID: uuidToString(task.ID), Repos: []taskCodeChangeRepo{}}
	row, err := h.Queries.GetTaskCodeChange(r.Context(), task.ID)
	if err != nil {
		if isNotFound(err) {
			writeJSON(w, http.StatusOK, resp)
			return
		}
		slog.Warn("load task code changes failed", "task_id", uuidToString(task.ID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load code changes")
		return
	}
	if err := json.Unmarshal(row.Repos, &resp.Repos); err != nil {
		slog.Warn("decode task code changes failed", "task_id", uuidToString(task.ID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load code changes")
		return
	}
	resp.ReposTruncated = row.ReposTruncated
	updatedAt := row.UpdatedAt.Time.UTC().Format(time.RFC3339)
	resp.UpdatedAt = &updatedAt
	writeJSON(w, http.StatusOK, resp)
}
