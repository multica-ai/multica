// Package gitlab houses the domain glue between the raw gitlab REST client
// (server/pkg/gitlab) and Multica's cache schema. Pure translation lives in
// translator.go; orchestration (sync, webhook, reconcile) lives in sibling
// files.
package gitlab

import (
	"sort"
	"strings"

	gitlabapi "github.com/multica-ai/multica/server/pkg/gitlab"
)

// TranslateContext carries lookups the translator needs but doesn't fetch
// itself (so the function stays pure).
type TranslateContext struct {
	// AgentBySlug maps Multica agent slug → agent UUID (string form).
	AgentBySlug map[string]string
}

// IssueValues are the cache-row values we'll write into the issue table.
// The SQL layer converts to pgtype where needed.
type IssueValues struct {
	Title        string
	Description  string
	Status       string // backlog | todo | in_progress | in_review | done | blocked | cancelled
	Priority     string // urgent | high | medium | low | none
	AssigneeType string // "" | "member" | "agent"
	AssigneeID   string // "" | UUID string
	DueDate      string // YYYY-MM-DD or ""
	UpdatedAt    string // RFC3339 from GitLab
}

func TranslateIssue(in gitlabapi.Issue, tc *TranslateContext) IssueValues {
	if tc == nil {
		tc = &TranslateContext{}
	}
	out := IssueValues{
		Title:       in.Title,
		Description: in.Description,
		DueDate:     in.DueDate,
		UpdatedAt:   in.UpdatedAt,
		Status:      pickStatus(in.Labels, in.State),
		Priority:    pickPriority(in.Labels),
	}
	out.AssigneeType, out.AssigneeID = pickAssignee(in.Labels, tc.AgentBySlug)
	return out
}

func pickStatus(labels []string, state string) string {
	const prefix = "status::"
	var found []string
	for _, l := range labels {
		if strings.HasPrefix(l, prefix) {
			found = append(found, strings.TrimPrefix(l, prefix))
		}
	}
	if len(found) == 0 {
		if state == "closed" {
			return "done"
		}
		return "todo"
	}
	sort.Strings(found)
	return found[0]
}

func pickPriority(labels []string) string {
	const prefix = "priority::"
	var found []string
	for _, l := range labels {
		if strings.HasPrefix(l, prefix) {
			found = append(found, strings.TrimPrefix(l, prefix))
		}
	}
	if len(found) == 0 {
		return "none"
	}
	sort.Strings(found)
	return found[0]
}

func pickAssignee(labels []string, agentBySlug map[string]string) (assigneeType, assigneeID string) {
	const prefix = "agent::"
	var slugs []string
	for _, l := range labels {
		if strings.HasPrefix(l, prefix) {
			slugs = append(slugs, strings.TrimPrefix(l, prefix))
		}
	}
	if len(slugs) == 0 {
		return "", ""
	}
	sort.Strings(slugs)
	for _, s := range slugs {
		if id, ok := agentBySlug[s]; ok {
			return "agent", id
		}
	}
	return "", ""
}

type NoteValues struct {
	Body         string
	Type         string // "comment" | "system"
	AuthorType   string // "" | "agent" | "member"
	AuthorSlug   string
	GitlabUserID int64
	UpdatedAt    string
}

func TranslateNote(in gitlabapi.Note) NoteValues {
	out := NoteValues{
		Body:         in.Body,
		Type:         "comment",
		GitlabUserID: in.Author.ID,
		UpdatedAt:    in.UpdatedAt,
	}
	if in.System {
		out.Type = "system"
		return out
	}
	if slug, body, ok := parseAgentPrefix(in.Body); ok {
		out.AuthorType = "agent"
		out.AuthorSlug = slug
		out.Body = body
	}
	return out
}

// BuildCreateNoteBody constructs the GitLab note body for a write-through
// comment. Agent-authored comments are prefixed with "**[agent:<slug>]** "
// so Phase 2b's TranslateNote round-trips the authorship on webhook replay.
// Human-authored comments pass the content through unchanged.
//
// Defensive: if authorType is "agent" but the slug is empty, we fall back to
// the human-style body (no prefix) rather than emitting a malformed
// "**[agent:]** " prefix that parseAgentPrefix won't recognize.
func BuildCreateNoteBody(authorType, agentSlug, content string) string {
	if authorType == "agent" && agentSlug != "" {
		return "**[agent:" + agentSlug + "]** " + content
	}
	return content
}

func parseAgentPrefix(body string) (slug, stripped string, ok bool) {
	const open = "**[agent:"
	const close = "]** "
	if !strings.HasPrefix(body, open) {
		return "", "", false
	}
	rest := body[len(open):]
	idx := strings.Index(rest, close)
	if idx <= 0 {
		return "", "", false
	}
	return rest[:idx], rest[idx+len(close):], true
}

type AwardValues struct {
	Emoji        string
	GitlabUserID int64
	UpdatedAt    string
}

func TranslateAward(in gitlabapi.AwardEmoji) AwardValues {
	return AwardValues{
		Emoji:        in.Name,
		GitlabUserID: in.User.ID,
		UpdatedAt:    in.UpdatedAt,
	}
}

// CreateIssueRequest mirrors the subset of Multica's create-issue HTTP body
// we translate to GitLab. Defined here so the translator stays handler-free.
type CreateIssueRequest struct {
	Title        string
	Description  string
	Status       string // backlog|todo|in_progress|in_review|done|blocked|cancelled
	Priority     string // urgent|high|medium|low|none
	AssigneeType string // "" | "member" | "agent"
	AssigneeID   string // UUID string when AssigneeType is set
	DueDate      string // YYYY-MM-DD or ""
	Labels       []string
}

// BuildCreateIssueInput converts a Multica create-issue request into the
// GitLab REST body. agentSlugByUUID maps Multica agent UUID → slug so we
// can express agent assignment as the agent::<slug> label.
//
// Phase 3a behaviour: member assignees are dropped (no GitLab user mapping
// yet — Phase 3b adds it). Agent assignees become the corresponding label.
func BuildCreateIssueInput(req CreateIssueRequest, agentSlugByUUID map[string]string) gitlabapi.CreateIssueInput {
	labels := append([]string(nil), req.Labels...)
	if req.Status != "" {
		labels = append(labels, "status::"+req.Status)
	}
	if req.Priority != "" && req.Priority != "none" {
		labels = append(labels, "priority::"+req.Priority)
	}
	if req.AssigneeType == "agent" && req.AssigneeID != "" && agentSlugByUUID != nil {
		if slug, ok := agentSlugByUUID[req.AssigneeID]; ok {
			labels = append(labels, "agent::"+slug)
		}
	}
	return gitlabapi.CreateIssueInput{
		Title:       req.Title,
		Description: req.Description,
		Labels:      labels,
		DueDate:     req.DueDate,
	}
}

// UpdateIssueRequest is the translator-facing input for PATCH /api/issues/{id}.
// Fields are pointers so the translator can distinguish "not in request"
// (nil) from "cleared" (pointer to zero value).
type UpdateIssueRequest struct {
	Title        *string
	Description  *string
	Status       *string
	Priority     *string
	AssigneeType *string // "agent", "member", or "" to clear
	AssigneeID   *string // UUID string, or "" to clear
	DueDate      *string
}

// OldIssueSnapshot is the view of the cache row's current state needed to
// compute a label diff. The handler populates this from the cache row before
// calling the translator.
type OldIssueSnapshot struct {
	Status       string
	Priority     string
	AssigneeType string
	AssigneeUUID string // UUID string; empty if unassigned or member (member-assignees are cache-only in Phase 3b)
}

// BuildUpdateIssueInput diffs the old cache state against the PATCH request
// and emits the GitLab-side update payload — add/remove labels for status,
// priority, and agent-assignee changes; state_event for close/reopen; plus
// pass-through of title/description/due_date when present.
func BuildUpdateIssueInput(old OldIssueSnapshot, req UpdateIssueRequest, agentSlugByUUID map[string]string) gitlabapi.UpdateIssueInput {
	out := gitlabapi.UpdateIssueInput{
		Title:       req.Title,
		Description: req.Description,
		DueDate:     req.DueDate,
	}

	// Status transitions.
	if req.Status != nil && *req.Status != old.Status {
		out.RemoveLabels = append(out.RemoveLabels, "status::"+old.Status)
		out.AddLabels = append(out.AddLabels, "status::"+*req.Status)
		wasClosed := old.Status == "done" || old.Status == "cancelled"
		isClosed := *req.Status == "done" || *req.Status == "cancelled"
		switch {
		case !wasClosed && isClosed:
			ev := "close"
			out.StateEvent = &ev
		case wasClosed && !isClosed:
			ev := "reopen"
			out.StateEvent = &ev
		}
	}

	// Priority transitions. "none" means no label.
	if req.Priority != nil && *req.Priority != old.Priority {
		if old.Priority != "none" && old.Priority != "" {
			out.RemoveLabels = append(out.RemoveLabels, "priority::"+old.Priority)
		}
		if *req.Priority != "none" {
			out.AddLabels = append(out.AddLabels, "priority::"+*req.Priority)
		}
	}

	// Agent assignee transitions. Any change away from the current agent
	// assignee removes the current agent::<slug> label. A new agent assignee
	// adds agent::<new-slug>. Member assignees and unassignment just remove.
	oldAgentSlug := ""
	if old.AssigneeType == "agent" && old.AssigneeUUID != "" {
		oldAgentSlug = agentSlugByUUID[old.AssigneeUUID]
	}
	if req.AssigneeType != nil || req.AssigneeID != nil {
		newType := old.AssigneeType
		if req.AssigneeType != nil {
			newType = *req.AssigneeType
		}
		newID := old.AssigneeUUID
		if req.AssigneeID != nil {
			newID = *req.AssigneeID
		}

		newAgentSlug := ""
		if newType == "agent" && newID != "" {
			newAgentSlug = agentSlugByUUID[newID]
		}

		if oldAgentSlug != newAgentSlug {
			if oldAgentSlug != "" {
				out.RemoveLabels = append(out.RemoveLabels, "agent::"+oldAgentSlug)
			}
			if newAgentSlug != "" {
				out.AddLabels = append(out.AddLabels, "agent::"+newAgentSlug)
			}
		}
	}

	return out
}
