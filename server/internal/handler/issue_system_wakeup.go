package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The child_done system rule is an issue_wakeup row owned by the platform (see
// service.SystemRuleChildDone). These endpoints show what one parent is
// waiting for and let people change the rule on that issue, and set its
// workspace default. People's own rules use the ordinary wakeup endpoints.

type systemWakeupTarget struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

type systemWakeupResponse struct {
	// ID and Revision are empty until the rule exists on this issue; it is
	// created with the first sub-issue change or the first edit.
	ID       string `json:"id"`
	Revision int64  `json:"revision"`
	Rule     string `json:"rule"`
	Enabled  bool   `json:"enabled"`
	// Instruction is set on this issue; DefaultInstruction is what runs
	// receive when it is empty (the workspace's, else the platform's).
	Instruction        string  `json:"instruction"`
	DefaultInstruction string  `json:"default_instruction"`
	Customized         bool    `json:"customized"`
	PausedReason       *string `json:"paused_reason"`
	// Staged is true while a stage is open; Stage is the lowest open one.
	// Otherwise the rule waits for every sub-issue.
	Staged    bool   `json:"staged"`
	Stage     *int32 `json:"stage"`
	Total     int    `json:"total"`
	Remaining int    `json:"remaining"`
	// Identifiers of the unfinished sub-issues in scope, at most five.
	Waiting []string            `json:"waiting"`
	Target  *systemWakeupTarget `json:"target"`
	// Blocked says why the rule would not start a run right now: "backlog",
	// "member_assignee" (it notifies the member instead) or "no_assignee".
	Blocked string `json:"blocked"`
	// WorkspaceDefault is the rule's workspace-wide setting, which applies
	// until the issue sets its own.
	WorkspaceDefault bool `json:"workspace_default"`
}

// childDoneSystemWakeup describes what the parent is waiting for. It returns
// nil when there is nothing left to wait for: no sub-issues, every sub-issue
// closed, or the parent itself closed.
func (h *Handler) childDoneSystemWakeup(r *http.Request, parent db.Issue) (*systemWakeupResponse, error) {
	ctx := r.Context()
	categories := map[string]string{}
	closed := func(status string) (bool, error) {
		category, ok := categories[status]
		if !ok {
			var err error
			if category, err = issuestatus.CategoryWithError(ctx, h.Queries, parent.WorkspaceID, status); err != nil {
				return false, err
			}
			categories[status] = category
		}
		return category == "done" || category == "closed", nil
	}
	if isClosed, err := closed(parent.Status); err != nil || isClosed {
		return nil, err
	}
	children, err := h.Queries.ListChildIssues(ctx, parent.ID)
	if err != nil || len(children) == 0 {
		return nil, err
	}
	open := make([]db.Issue, 0, len(children))
	var stage pgtype.Int4
	for _, c := range children {
		isClosed, err := closed(c.Status)
		if err != nil {
			return nil, err
		}
		if isClosed {
			continue
		}
		open = append(open, c)
		if c.Stage.Valid && (!stage.Valid || c.Stage.Int32 < stage.Int32) {
			stage = c.Stage
		}
	}
	if len(open) == 0 {
		return nil, nil
	}
	out := &systemWakeupResponse{Rule: service.SystemRuleChildDone, Staged: stage.Valid, Waiting: []string{}}
	if stage.Valid {
		out.Stage = &stage.Int32
	}
	prefix := h.getIssuePrefix(ctx, parent.WorkspaceID)
	for _, c := range children {
		if stage.Valid && (!c.Stage.Valid || c.Stage.Int32 != stage.Int32) {
			continue
		}
		out.Total++
	}
	for _, c := range open {
		if stage.Valid && (!c.Stage.Valid || c.Stage.Int32 != stage.Int32) {
			continue
		}
		out.Remaining++
		if len(out.Waiting) < 5 {
			out.Waiting = append(out.Waiting, prefix+"-"+strconv.Itoa(int(c.Number)))
		}
	}
	ws, err := h.Queries.GetWorkspace(ctx, parent.WorkspaceID)
	if err != nil {
		return nil, err
	}
	out.WorkspaceDefault, _ = service.SystemWakeupDefault(ws.Settings)
	out.Enabled = out.WorkspaceDefault
	out.DefaultInstruction = service.ChildDoneInstruction("", ws.Settings)
	rule, err := h.Queries.GetSystemWakeup(ctx, db.GetSystemWakeupParams{IssueID: parent.ID, SystemRule: pgtype.Text{String: service.SystemRuleChildDone, Valid: true}})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if err == nil {
		out.ID, out.Revision, out.Enabled, out.Instruction = uuidToString(rule.ID), rule.Revision, rule.Enabled, rule.Instruction
		out.Customized = rule.CustomizedAt.Valid
		if rule.PausedReason.Valid {
			out.PausedReason = &rule.PausedReason.String
		}
	}
	switch {
	case !parent.AssigneeType.Valid || !parent.AssigneeID.Valid:
		out.Blocked = "no_assignee"
	case parent.AssigneeType.String == "member":
		out.Blocked = "member_assignee"
		if user, e := h.Queries.GetUser(ctx, parent.AssigneeID); e == nil {
			out.Target = &systemWakeupTarget{Type: "member", ID: uuidToString(user.ID), Name: user.Name}
		}
	case parent.AssigneeType.String == "agent":
		if agent, e := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: parent.AssigneeID, WorkspaceID: parent.WorkspaceID}); e == nil {
			out.Target = &systemWakeupTarget{Type: "agent", ID: uuidToString(agent.ID), Name: agent.Name}
		}
	case parent.AssigneeType.String == "squad":
		if squad, e := h.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{ID: parent.AssigneeID, WorkspaceID: parent.WorkspaceID}); e == nil {
			out.Target = &systemWakeupTarget{Type: "squad", ID: uuidToString(squad.ID), Name: squad.Name}
		}
	}
	if out.Blocked == "" && issuestatus.Effective(ctx, h.Queries, parent.WorkspaceID, parent.Status) == "backlog" {
		out.Blocked = "backlog"
	}
	if out.Blocked == "" && out.Target == nil {
		out.Blocked = "no_assignee"
	}
	return out, nil
}

func (h *Handler) listSystemWakeups(r *http.Request, issue db.Issue) ([]systemWakeupResponse, error) {
	rules := []systemWakeupResponse{}
	childDone, err := h.childDoneSystemWakeup(r, issue)
	if err != nil {
		return nil, err
	}
	if childDone != nil {
		rules = append(rules, *childDone)
	}
	return rules, nil
}

func (h *Handler) ListIssueSystemWakeups(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	rules, err := h.listSystemWakeups(r, issue)
	if err != nil {
		slog.Warn("list system wakeups failed", "error", err, "issue_id", uuidToString(issue.ID))
		writeError(w, 500, "could not load system wakeups")
		return
	}
	writeJSON(w, 200, rules)
}

// systemWakeupHuman requires a person: agents cannot change platform rules.
// Any workspace member who can see the issue may change it on that issue:
// the rule wakes the issue's own assignee, whose invocation was already
// authorized when the issue was assigned.
func (h *Handler) systemWakeupHuman(w http.ResponseWriter, r *http.Request, workspaceID string) (db.Member, bool) {
	actorType, actorID := h.resolveActor(r, requestUserID(r), workspaceID)
	if actorType != "member" {
		writeError(w, 403, "only members can change system wakeups")
		return db.Member{}, false
	}
	member, err := h.getWorkspaceMember(r.Context(), actorID, workspaceID)
	if err != nil {
		writeError(w, 403, "wakeup permission denied")
		return db.Member{}, false
	}
	return member, true
}

// UpdateIssueSystemWakeup turns a platform rule on or off for one issue and
// sets its instruction. Omitted fields keep their value, so a list can toggle
// the rule without reading its instruction.
func (h *Handler) UpdateIssueSystemWakeup(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if chi.URLParam(r, "rule") != service.SystemRuleChildDone {
		writeError(w, 404, "system wakeup not found")
		return
	}
	var in service.SystemWakeupInput
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		writeError(w, 400, "invalid system wakeup body")
		return
	}
	if _, ok := h.systemWakeupHuman(w, r, uuidToString(issue.WorkspaceID)); !ok {
		return
	}
	if _, err := (&service.IssueWakeupService{Tasks: h.TaskService}).UpdateChildDoneRule(r.Context(), issue.ID, in); err != nil {
		if errors.Is(err, service.ErrWakeupInput) {
			writeError(w, 400, err.Error())
			return
		}
		slog.Warn("update system wakeup failed", "error", err, "issue_id", uuidToString(issue.ID))
		writeError(w, 500, "could not save system wakeup")
		return
	}
	rules, err := h.listSystemWakeups(r, issue)
	if err != nil {
		writeError(w, 500, "could not load system wakeups")
		return
	}
	writeJSON(w, 200, rules)
}

type workspaceSystemWakeupResponse struct {
	Rule    string `json:"rule"`
	Enabled bool   `json:"enabled"`
	// Instruction is the workspace's; empty means runs get BuiltinInstruction.
	Instruction        string `json:"instruction"`
	BuiltinInstruction string `json:"builtin_instruction"`
	// Customized counts open issues whose rule a person changed; they no
	// longer follow this default.
	Customized int64 `json:"customized"`
}

func (h *Handler) workspaceSystemWakeups(r *http.Request, workspaceID pgtype.UUID) ([]workspaceSystemWakeupResponse, error) {
	ws, err := h.Queries.GetWorkspace(r.Context(), workspaceID)
	if err != nil {
		return nil, err
	}
	customized, err := h.Queries.CountCustomizedSystemWakeups(r.Context(), db.CountCustomizedSystemWakeupsParams{WorkspaceID: workspaceID, SystemRule: pgtype.Text{String: service.SystemRuleChildDone, Valid: true}})
	if err != nil {
		return nil, err
	}
	enabled, instruction := service.SystemWakeupDefault(ws.Settings)
	return []workspaceSystemWakeupResponse{{
		Rule: service.SystemRuleChildDone, Enabled: enabled, Instruction: instruction,
		BuiltinInstruction: service.ChildDoneDefaultInstruction, Customized: customized,
	}}, nil
}

// ListWorkspaceSystemWakeups returns the workspace default of each platform rule.
func (h *Handler) ListWorkspaceSystemWakeups(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	rules, err := h.workspaceSystemWakeups(r, parseUUID(workspaceID))
	if err != nil {
		writeError(w, 500, "could not load system wakeups")
		return
	}
	writeJSON(w, 200, rules)
}

// UpdateWorkspaceSystemWakeup sets a platform rule's workspace default. Rules
// nobody changed on their issue follow it right away. Owners and admins only.
func (h *Handler) UpdateWorkspaceSystemWakeup(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if chi.URLParam(r, "rule") != service.SystemRuleChildDone {
		writeError(w, 404, "system wakeup not found")
		return
	}
	var in service.SystemWakeupInput
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		writeError(w, 400, "invalid system wakeup body")
		return
	}
	member, ok := h.systemWakeupHuman(w, r, workspaceID)
	if !ok {
		return
	}
	if !roleAllowed(member.Role, "owner", "admin") {
		writeError(w, 403, "only owners and admins can change workspace defaults")
		return
	}
	if _, err := (&service.IssueWakeupService{Tasks: h.TaskService}).SetChildDoneDefault(r.Context(), parseUUID(workspaceID), in.Enabled, in.Instruction); err != nil {
		if errors.Is(err, service.ErrWakeupInput) {
			writeError(w, 400, err.Error())
			return
		}
		slog.Warn("update workspace system wakeup failed", "error", err, "workspace_id", workspaceID)
		writeError(w, 500, "could not save system wakeup")
		return
	}
	rules, err := h.workspaceSystemWakeups(r, parseUUID(workspaceID))
	if err != nil {
		writeError(w, 500, "could not load system wakeups")
		return
	}
	writeJSON(w, 200, rules)
}
