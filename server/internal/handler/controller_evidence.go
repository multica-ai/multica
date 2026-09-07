package handler

import (
	"errors"
	"net/http"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/runcontrol"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type controllerEvidence struct {
	Version  string              `json:"version"`
	Timeline []TimelineEntry     `json:"timeline"`
	Runs     []db.AgentTaskQueue `json:"runs"`
}

func (h *Handler) readControllerEvidence(r *http.Request, tx pgx.Tx, issue db.Issue) (controllerEvidence, error) {
	q := db.New(tx)
	c, err := q.ListCommentsForIssue(r.Context(), db.ListCommentsForIssueParams{IssueID: issue.ID, WorkspaceID: issue.WorkspaceID, Limit: 1001})
	if err != nil {
		return controllerEvidence{}, err
	}
	a, err := q.ListActivitiesForIssue(r.Context(), db.ListActivitiesForIssueParams{IssueID: issue.ID, Limit: 1001})
	if err != nil {
		return controllerEvidence{}, err
	}
	tasks, err := q.ListControllerRuns(r.Context(), issue.ID)
	if err != nil {
		return controllerEvidence{}, err
	}
	if len(c) > 1000 || len(a) > 1000 || len(tasks) > 1000 {
		return controllerEvidence{}, errors.New("controller evidence window exceeded; compact reviewed history before enabling")
	}
	// Controller evidence uses stable authenticated authors, without UI-only
	// attachment/reaction hydration that requires an ordinary user principal.
	entries := make([]TimelineEntry, 0, len(c)+len(a))
	for _, comment := range c {
		body := comment.Content
		entries = append(entries, TimelineEntry{Type: "comment", ID: uuidToString(comment.ID), ActorType: comment.AuthorType, ActorID: uuidToString(comment.AuthorID), Content: &body, ParentID: uuidToPtr(comment.ParentID), CreatedAt: timestampToString(comment.CreatedAt), Revision: comment.Revision})
	}
	for _, activity := range a {
		entries = append(entries, activityToEntry(activity))
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].CreatedAt == entries[j].CreatedAt {
			return entries[i].ID < entries[j].ID
		}
		return entries[i].CreatedAt < entries[j].CreatedAt
	})
	return controllerEvidence{Version: runcontrol.Digest([]any{c, a, tasks}), Timeline: entries, Runs: tasks}, nil
}

func (h *Handler) controllerEvidenceMatches(w http.ResponseWriter, r *http.Request, tx pgx.Tx, issue db.Issue, version string) bool {
	e, err := h.readControllerEvidence(r, tx, issue)
	if err != nil {
		writeError(w, 503, "current controller evidence unavailable")
		return false
	}
	if version == "" || version != e.Version {
		writeError(w, 409, "native evidence changed; reread and decide again")
		return false
	}
	return true
}
