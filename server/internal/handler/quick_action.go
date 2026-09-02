package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Issue Quick Actions (MUL-5465): workspace-level presets for "who to call and
// what to say" on an existing issue.
//
// Contract highlights:
//   - Running one is NOT a new dispatch path. It renders the prompt, posts an
//     ordinary comment carrying the target's mention markup (marked with
//     quick_action_id), and hands off to triggerTasksForComment. Permission, attribution, squad routing,
//     the execution log, and pending-task coalescing are inherited from the
//     comment path rather than reimplemented — the MUL-3375 lesson about four
//     drifting copies of one trigger decision.
//   - PERMISSION IS CHECKED IN EXACTLY ONE PLACE: RunQuickAction. The list
//     endpoint does no permission work and hides nothing beyond `private`
//     ownership. Filtering the sidebar by invoke permission was tried and
//     removed: it made two people looking at one issue see different sidebars
//     with no explanation, which is harder to debug than a button that says
//     why it refused.
//   - `visibility` is the author's stated intent, not an authorization
//     decision. A `public` action must bind a target every member can invoke,
//     so it is runnable by construction; the run-time gate still has the final
//     say, so a target that goes private later fails loudly at click time
//     rather than silently granting anything.
//   - The prompt is sent VERBATIM. There is no interpolation and no runtime
//     input: every variable considered ({{issue.title}}, {{issue.identifier}},
//     {{issue.url}}, {{user.name}}, {{date}}) named something the agent
//     already has from the issue context and the comment's own author, and the
//     `/` slash command covers "same action, one detail different" by dropping
//     the rendered body into the composer to edit. Write-time validation still
//     REJECTS any `{{...}}` so a habitual template token cannot land literally
//     in an agent's instructions.
const (
	maxActiveQuickActionsPerWorkspace = 30
	maxQuickActionNameLen             = 32
	maxQuickActionDescriptionLen      = 200
	maxQuickActionPromptLen           = 4000
)

// quickActionTemplateTokenRe catches any `{{...}}` in a prompt.
//
// Templating is deliberately unsupported, but silence is not the same as
// safety: someone carrying the habit over from autopilot's issue-title
// template would otherwise have `{{issue.title}}` rendered literally into an
// agent's instructions and never notice. Rejecting at write time is a fraction
// of the cost of the interpolation engine it replaces, and it keeps the door
// open to enabling variables later without touching stored data.
var quickActionTemplateTokenRe = regexp.MustCompile(`\{\{[^}]*\}\}`)

// quickActionSideEffectMentionRe catches mention markup that reaches somebody.
//
// The prompt is appended verbatim to a comment that then runs through the
// normal mention pipeline, so a mention inside it acts on every click:
//
//   - agent / squad / all -> enqueues a SECOND target beside the configured
//     one, breaking the invariant the permission model rests on (a public
//     action runs exactly the target it was validated against). The sidebar
//     reports only the first outcome, so the extra run is invisible.
//   - member -> notification_listeners.go adds them to the mention recipients
//     and creates an inbox item, so a saved prompt pings that person on every
//     single click. This one was initially allowed on the reasoning that it
//     "only renders a link"; that was wrong.
//
// `mention://issue/...` is the sole exception: it renders as a link and
// reaches nobody, so a prompt may legitimately point at related work.
var quickActionSideEffectMentionRe = regexp.MustCompile(`mention://(agent|squad|member|all)/`)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type QuickActionResponse struct {
	ID           string  `json:"id"`
	WorkspaceID  string  `json:"workspace_id"`
	Name         string  `json:"name"`
	Description  string  `json:"description"`
	AssigneeType string  `json:"assignee_type"`
	AssigneeID   string  `json:"assignee_id"`
	Prompt       string  `json:"prompt"`
	Visibility   string  `json:"visibility"`
	Status       string  `json:"status"`
	LastUsedAt   *string `json:"last_used_at"`
	UseCount     int64   `json:"use_count"`
	CreatedByID  string  `json:"created_by_id"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`

	// Target identity, for display. Always populated when the target resolves:
	// a `public` action binds a publicly-invocable agent whose name everyone
	// can already see, and a `private` action is only ever returned to its
	// creator — so there is nothing left to redact.
	TargetName string `json:"target_name,omitempty"`
	// TargetPublic reports whether the bound target is currently invocable by
	// every workspace member. Plain metadata, not a verdict: settings renders
	// it beside the binding so a `public` action pointing at a now-private
	// agent is visibly wrong without a bespoke "broken" state.
	TargetPublic bool `json:"target_public"`
	// TargetMissing marks an archived or deleted target.
	TargetMissing bool `json:"target_missing"`
}

type CreateQuickActionRequest struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	AssigneeType string `json:"assignee_type"`
	AssigneeID   string `json:"assignee_id"`
	Prompt       string `json:"prompt"`
	Visibility   string `json:"visibility"`
}

type UpdateQuickActionRequest struct {
	Name         *string `json:"name"`
	Description  *string `json:"description"`
	AssigneeType *string `json:"assignee_type"`
	AssigneeID   *string `json:"assignee_id"`
	Prompt       *string `json:"prompt"`
	Visibility   *string `json:"visibility"`
	Status       *string `json:"status"`
}

type ListQuickActionsResponse struct {
	QuickActions []QuickActionResponse `json:"quick_actions"`
}

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

func validateQuickActionName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", fmt.Errorf("name is required")
	}
	if utf8.RuneCountInString(name) > maxQuickActionNameLen {
		return "", fmt.Errorf("name must be at most %d characters", maxQuickActionNameLen)
	}
	return name, nil
}

// validateQuickActionPrompt trims, bounds, and refuses template syntax. See
// quickActionTemplateTokenRe for why the rejection outlives the feature.
func validateQuickActionPrompt(raw string) (string, error) {
	prompt := strings.TrimSpace(raw)
	if prompt == "" {
		return "", fmt.Errorf("prompt is required")
	}
	if utf8.RuneCountInString(prompt) > maxQuickActionPromptLen {
		return "", fmt.Errorf("prompt must be at most %d characters", maxQuickActionPromptLen)
	}
	if token := quickActionTemplateTokenRe.FindString(prompt); token != "" {
		return "", fmt.Errorf("template variables are not supported yet; remove %s — the agent already reads this issue", token)
	}
	if quickActionSideEffectMentionRe.MatchString(prompt) {
		return "", fmt.Errorf("the prompt cannot @mention an agent, squad, or person; a quick action reaches exactly the one target it is bound to (an issue link is fine)")
	}
	return prompt, nil
}

func validateQuickActionAssignee(assigneeType, assigneeID string) error {
	if assigneeType != "agent" && assigneeType != "squad" {
		return fmt.Errorf("assignee_type must be \"agent\" or \"squad\"")
	}
	if strings.TrimSpace(assigneeID) == "" {
		return fmt.Errorf("assignee_id is required")
	}
	return nil
}

func normalizeQuickActionVisibility(raw string) (string, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "public", nil
	}
	if v != "public" && v != "private" {
		return "", fmt.Errorf("visibility must be \"public\" or \"private\"")
	}
	return v, nil
}

// ---------------------------------------------------------------------------
// Target resolution
// ---------------------------------------------------------------------------

// quickActionTarget is the resolved execution target of an action: the agent
// that will actually run, plus the display identity the client should show.
// For a squad binding, Agent is the squad leader and Name is the SQUAD's name,
// because the squad is what the user bound and what the mention will address.
type quickActionTarget struct {
	Agent     db.Agent
	Name      string
	AvatarURL string
	// MentionType is the mention:// scheme the rendered comment must use so
	// the existing trigger path routes it exactly as a hand-typed mention.
	MentionType string
	// MentionID is the id inside the mention link: the agent id, or the SQUAD
	// id for a squad binding (the trigger path resolves the leader itself).
	MentionID string
	Found     bool
}

// resolveQuickActionTarget loads the execution target. A missing/archived
// agent, or a missing/archived squad, resolves to Found=false — the caller
// renders that as an unavailable target rather than failing the whole list.
func (h *Handler) resolveQuickActionTarget(ctx context.Context, qa db.QuickAction) quickActionTarget {
	switch qa.AssigneeType {
	case "squad":
		squad, err := h.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
			ID:          qa.AssigneeID,
			WorkspaceID: qa.WorkspaceID,
		})
		if err != nil || squad.ArchivedAt.Valid {
			return quickActionTarget{}
		}
		leader, err := h.Queries.GetAgent(ctx, squad.LeaderID)
		if err != nil || leader.ArchivedAt.Valid {
			return quickActionTarget{}
		}
		avatar := ""
		if squad.AvatarUrl.Valid {
			avatar = squad.AvatarUrl.String
		}
		return quickActionTarget{
			Agent:       leader,
			Name:        squad.Name,
			AvatarURL:   avatar,
			MentionType: "squad",
			MentionID:   uuidToString(squad.ID),
			Found:       true,
		}
	default:
		agent, err := h.Queries.GetAgent(ctx, qa.AssigneeID)
		if err != nil || agent.ArchivedAt.Valid || uuidToString(agent.WorkspaceID) != uuidToString(qa.WorkspaceID) {
			return quickActionTarget{}
		}
		avatar := ""
		if agent.AvatarUrl.Valid {
			avatar = agent.AvatarUrl.String
		}
		return quickActionTarget{
			Agent:       agent,
			Name:        agent.Name,
			AvatarURL:   avatar,
			MentionType: "agent",
			MentionID:   uuidToString(agent.ID),
			Found:       true,
		}
	}
}

// agentInvocableByEveryone reports whether every workspace member may invoke
// this agent — `public_to` carrying a `workspace` target.
//
// A `public_to` agent whose allow-list names specific members does NOT count:
// binding one to a public quick action would make the "everyone" promise false
// for the members left out. Fails closed on a lookup error.
func (h *Handler) agentInvocableByEveryone(ctx context.Context, agent db.Agent) bool {
	if agent.PermissionMode != "public_to" {
		return false
	}
	targets, err := h.Queries.ListAgentInvocationTargets(ctx, agent.ID)
	if err != nil {
		return false
	}
	for _, t := range targets {
		if t.TargetType == "workspace" {
			return true
		}
	}
	return false
}

// quickActionCatalog is a batched resolver for the list endpoint.
//
// The single-row path issues a GetAgent (plus GetSquad) AND a
// ListAgentInvocationTargets per action. At the 30-action cap that is 60-90
// sequential round-trips on every sidebar and settings load. This loads the
// workspace's agents, squads, and invocation targets in THREE queries and
// answers from memory. Workspaces are small-team scale, so listing all agents
// costs less than thirty point lookups.
type quickActionCatalog struct {
	agents map[string]db.Agent
	squads map[string]db.Squad
	// publicAgents holds agent ids every workspace member may invoke.
	publicAgents map[string]bool
}

func (h *Handler) loadQuickActionCatalog(ctx context.Context, workspaceID pgtype.UUID) quickActionCatalog {
	cat := quickActionCatalog{
		agents:       map[string]db.Agent{},
		squads:       map[string]db.Squad{},
		publicAgents: map[string]bool{},
	}

	agents, err := h.Queries.ListAgents(ctx, workspaceID)
	if err != nil {
		// Degrade to "target unresolved" rather than failing the whole list:
		// the catalog is display metadata, and the run path re-checks
		// everything that matters anyway.
		return cat
	}
	agentIDs := make([]pgtype.UUID, 0, len(agents))
	for _, a := range agents {
		cat.agents[uuidToString(a.ID)] = a
		agentIDs = append(agentIDs, a.ID)
	}

	if squads, err := h.Queries.ListSquads(ctx, workspaceID); err == nil {
		for _, sq := range squads {
			cat.squads[uuidToString(sq.ID)] = sq
		}
	}

	if len(agentIDs) > 0 {
		targets, err := h.Queries.ListAgentInvocationTargetsByAgentIDs(ctx, agentIDs)
		if err == nil {
			for _, t := range targets {
				if t.TargetType == "workspace" {
					cat.publicAgents[uuidToString(t.AgentID)] = true
				}
			}
		}
	}
	return cat
}

// resolve mirrors resolveQuickActionTarget + agentInvocableByEveryone, but
// entirely from the pre-loaded maps. The two must agree; the shared shape of
// quickActionTarget is what keeps them honest.
func (c quickActionCatalog) resolve(qa db.QuickAction) (quickActionTarget, bool) {
	if qa.AssigneeType == "squad" {
		squad, ok := c.squads[uuidToString(qa.AssigneeID)]
		if !ok || squad.ArchivedAt.Valid {
			return quickActionTarget{}, false
		}
		leader, ok := c.agents[uuidToString(squad.LeaderID)]
		if !ok {
			return quickActionTarget{}, false
		}
		name := squad.Name
		return quickActionTarget{Agent: leader, Name: name, Found: true},
			c.publicAgents[uuidToString(leader.ID)] && leader.PermissionMode == "public_to"
	}
	agent, ok := c.agents[uuidToString(qa.AssigneeID)]
	if !ok {
		return quickActionTarget{}, false
	}
	return quickActionTarget{Agent: agent, Name: agent.Name, Found: true},
		c.publicAgents[uuidToString(agent.ID)] && agent.PermissionMode == "public_to"
}

// quickActionToResponseFrom builds a row's response off the batched catalog.
func quickActionToResponseFrom(qa db.QuickAction, cat quickActionCatalog) QuickActionResponse {
	resp := baseQuickActionResponse(qa)
	target, targetPublic := cat.resolve(qa)
	resp.TargetMissing = !target.Found
	if target.Found {
		resp.TargetName = target.Name
		resp.TargetPublic = targetPublic
	}
	return resp
}

// baseQuickActionResponse maps the stored columns. Target metadata is layered
// on by whichever resolver the caller used.
func baseQuickActionResponse(qa db.QuickAction) QuickActionResponse {
	return QuickActionResponse{
		ID:           uuidToString(qa.ID),
		WorkspaceID:  uuidToString(qa.WorkspaceID),
		Name:         qa.Name,
		Description:  qa.Description,
		AssigneeType: qa.AssigneeType,
		AssigneeID:   uuidToString(qa.AssigneeID),
		Prompt:       qa.Prompt,
		Visibility:   qa.Visibility,
		Status:       qa.Status,
		LastUsedAt:   timestampToPtr(qa.LastUsedAt),
		UseCount:     qa.UseCount,
		CreatedByID:  uuidToString(qa.CreatedByID),
		CreatedAt:    timestampToString(qa.CreatedAt),
		UpdatedAt:    timestampToString(qa.UpdatedAt),
	}
}

// quickActionToResponse is the single-row path, used by create/update where
// exactly one action is being returned and a batch would be pointless. The
// list endpoint uses quickActionToResponseFrom instead.
func (h *Handler) quickActionToResponse(ctx context.Context, qa db.QuickAction) QuickActionResponse {
	resp := baseQuickActionResponse(qa)
	target := h.resolveQuickActionTarget(ctx, qa)
	resp.TargetMissing = !target.Found
	if target.Found {
		resp.TargetName = target.Name
		resp.TargetPublic = h.agentInvocableByEveryone(ctx, target.Agent)
	}
	return resp
}

// ---------------------------------------------------------------------------
// Management gate
// ---------------------------------------------------------------------------

// requireQuickActionActor resolves the acting member. Agents are rejected: an
// agent inherits its runtime owner's credentials, and without this an admin's
// agent could mass-produce actions that then appear in everyone's sidebar.
//
// The role check is deliberately NOT here — it depends on the visibility being
// written. A `public` action is workspace furniture and needs owner/admin; a
// `private` one is personal and any member may create it.
func (h *Handler) requireQuickActionActor(w http.ResponseWriter, r *http.Request) (workspaceID, userID string, ok bool) {
	workspaceID = h.resolveWorkspaceID(r)
	userID, ok = requireUserID(w, r)
	if !ok {
		return "", "", false
	}
	if actorType, _ := h.resolveActor(r, userID, workspaceID); actorType == "agent" {
		writeError(w, http.StatusForbidden, "agents cannot manage quick actions")
		return "", "", false
	}
	if _, memberOK := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !memberOK {
		return "", "", false
	}
	return workspaceID, userID, true
}

// loadReachableQuickAction fetches an action and enforces the ONE rule that
// governs who may touch a private one: it belongs to its creator and nobody
// else, whatever they know its id.
//
// This exists because the check used to be inline and only in RunQuickAction.
// Update and Render reached rows by (id, workspace) alone, so any member who
// learned a UUID — an action-created comment carries one — could rewrite or
// read back another member's private prompt. Centralizing it means a new
// endpoint cannot forget the rule by omission.
//
// A private action the caller does not own reports 404, not 403: whether a
// given UUID exists is itself not the caller's business.
func (h *Handler) loadReachableQuickAction(
	w http.ResponseWriter,
	r *http.Request,
	id, workspaceID pgtype.UUID,
	userID string,
) (db.QuickAction, bool) {
	qa, err := h.Queries.GetQuickAction(r.Context(), db.GetQuickActionParams{ID: id, WorkspaceID: workspaceID})
	if err != nil {
		writeError(w, http.StatusNotFound, "quick action not found")
		return db.QuickAction{}, false
	}
	if qa.Visibility == "private" && uuidToString(qa.CreatedByID) != userID {
		writeError(w, http.StatusNotFound, "quick action not found")
		return db.QuickAction{}, false
	}
	return qa, true
}

// requirePublicQuickActionRole gates writes that leave an action `public`.
// Returns false having already written the error response.
func (h *Handler) requirePublicQuickActionRole(w http.ResponseWriter, r *http.Request, workspaceID string) bool {
	_, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin")
	return ok
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// ListQuickActions returns the workspace catalog. It performs NO permission
// work: `private` rows are scoped to their creator by the query (that is what
// the field means, not a permission check), and every other row is returned to
// everyone. Whether the caller may RUN one is answered by RunQuickAction.
func (h *Handler) ListQuickActions(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	viewerUUID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}

	includeArchived := r.URL.Query().Get("include_archived") == "true"
	rows, err := h.Queries.ListQuickActions(r.Context(), db.ListQuickActionsParams{
		WorkspaceID:     wsUUID,
		IncludeArchived: includeArchived,
		ViewerID:        viewerUUID,
	})
	if err != nil {
		slog.Warn("list quick actions failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list quick actions")
		return
	}

	catalog := h.loadQuickActionCatalog(r.Context(), wsUUID)
	out := make([]QuickActionResponse, 0, len(rows))
	for _, qa := range rows {
		out = append(out, quickActionToResponseFrom(qa, catalog))
	}
	writeJSON(w, http.StatusOK, ListQuickActionsResponse{QuickActions: out})
}

func (h *Handler) CreateQuickAction(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.requireQuickActionActor(w, r)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	var req CreateQuickActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	visibility, err := normalizeQuickActionVisibility(req.Visibility)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if visibility == "public" && !h.requirePublicQuickActionRole(w, r, workspaceID) {
		return
	}
	name, err := validateQuickActionName(req.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateQuickActionAssignee(req.AssigneeType, req.AssigneeID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	assigneeUUID, ok := parseUUIDOrBadRequest(w, req.AssigneeID, "assignee_id")
	if !ok {
		return
	}
	prompt, err := validateQuickActionPrompt(req.Prompt)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	description, ok := trimmedWithinLimit(w, req.Description, maxQuickActionDescriptionLen, "description")
	if !ok {
		return
	}
	if !h.validateQuickActionBinding(w, r, req.AssigneeType, assigneeUUID, wsUUID, visibility) {
		return
	}

	count, err := h.Queries.CountActiveQuickActions(r.Context(), wsUUID)
	if err == nil && count >= maxActiveQuickActionsPerWorkspace {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("a workspace can have at most %d active quick actions; archive one first", maxActiveQuickActionsPerWorkspace))
		return
	}

	qa, err := h.Queries.CreateQuickAction(r.Context(), db.CreateQuickActionParams{
		WorkspaceID:   wsUUID,
		Name:          name,
		Description:   description,
		AssigneeType:  req.AssigneeType,
		AssigneeID:    assigneeUUID,
		Prompt:        prompt,
		Visibility:    visibility,
		CreatedByType: "member",
		CreatedByID:   parseUUID(userID),
	})
	if err != nil {
		slog.Warn("create quick action failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create quick action")
		return
	}
	writeJSON(w, http.StatusCreated, h.quickActionToResponse(r.Context(), qa))
}

func (h *Handler) UpdateQuickAction(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.requireQuickActionActor(w, r)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	idUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "quick action id")
	if !ok {
		return
	}
	existing, ok := h.loadReachableQuickAction(w, r, idUUID, wsUUID, userID)
	if !ok {
		return
	}

	var req UpdateQuickActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// The resulting visibility decides the role needed, so resolve it first.
	// Editing an already-public action also requires the role, otherwise a
	// member could rewrite the prompt behind a workspace-wide button.
	visibility := existing.Visibility
	if req.Visibility != nil {
		normalized, err := normalizeQuickActionVisibility(*req.Visibility)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		visibility = normalized
	}
	if (visibility == "public" || existing.Visibility == "public") && !h.requirePublicQuickActionRole(w, r, workspaceID) {
		return
	}

	params := db.UpdateQuickActionParams{ID: idUUID, WorkspaceID: wsUUID}
	if req.Visibility != nil {
		params.Visibility = pgtype.Text{String: visibility, Valid: true}
	}

	if req.Name != nil {
		name, err := validateQuickActionName(*req.Name)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		params.Name = pgtype.Text{String: name, Valid: true}
	}
	if req.Description != nil {
		description, ok := trimmedWithinLimit(w, *req.Description, maxQuickActionDescriptionLen, "description")
		if !ok {
			return
		}
		params.Description = pgtype.Text{String: description, Valid: true}
	}

	// assignee_type and assignee_id move together so a type swap can never
	// land with a mismatched id (the same rule autopilot enforces). The
	// binding is re-validated against the RESULTING visibility, so flipping an
	// action to public with a private agent still bound is caught here.
	newType := existing.AssigneeType
	newID := uuidToString(existing.AssigneeID)
	if req.AssigneeType != nil {
		newType = *req.AssigneeType
	}
	if req.AssigneeID != nil {
		newID = *req.AssigneeID
	}
	if err := validateQuickActionAssignee(newType, newID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	newUUID, ok := parseUUIDOrBadRequest(w, newID, "assignee_id")
	if !ok {
		return
	}
	if req.AssigneeType != nil || req.AssigneeID != nil {
		params.AssigneeType = pgtype.Text{String: newType, Valid: true}
		params.AssigneeID = newUUID
	}
	if !h.validateQuickActionBinding(w, r, newType, newUUID, wsUUID, visibility) {
		return
	}

	if req.Prompt != nil {
		prompt, err := validateQuickActionPrompt(*req.Prompt)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		params.Prompt = pgtype.Text{String: prompt, Valid: true}
	}
	if req.Status != nil {
		if *req.Status != "active" && *req.Status != "archived" {
			writeError(w, http.StatusBadRequest, "status must be \"active\" or \"archived\"")
			return
		}
		if *req.Status == "active" && existing.Status != "active" {
			count, err := h.Queries.CountActiveQuickActions(r.Context(), wsUUID)
			if err == nil && count >= maxActiveQuickActionsPerWorkspace {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("a workspace can have at most %d active quick actions", maxActiveQuickActionsPerWorkspace))
				return
			}
		}
		params.Status = pgtype.Text{String: *req.Status, Valid: true}
	}

	qa, err := h.Queries.UpdateQuickAction(r.Context(), params)
	if err != nil {
		slog.Warn("update quick action failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to update quick action")
		return
	}
	writeJSON(w, http.StatusOK, h.quickActionToResponse(r.Context(), qa))
}

func (h *Handler) DeleteQuickAction(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.requireQuickActionActor(w, r)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	idUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "quick action id")
	if !ok {
		return
	}
	existing, ok := h.loadReachableQuickAction(w, r, idUUID, wsUUID, userID)
	if !ok {
		return
	}
	// A public action is workspace furniture; a private one belongs to its
	// creator and nobody else may remove it.
	if existing.Visibility == "public" {
		if !h.requirePublicQuickActionRole(w, r, workspaceID) {
			return
		}
	} else if uuidToString(existing.CreatedByID) != userID {
		writeError(w, http.StatusForbidden, "only the creator can delete a private quick action")
		return
	}

	if err := h.Queries.DeleteQuickAction(r.Context(), db.DeleteQuickActionParams{ID: idUUID, WorkspaceID: wsUUID}); err != nil {
		slog.Warn("delete quick action failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to delete quick action")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// trimmedWithinLimit trims a free-text field and enforces its rune ceiling,
// writing the 400 itself so callers stay linear.
func trimmedWithinLimit(w http.ResponseWriter, raw string, limit int, field string) (string, bool) {
	v := strings.TrimSpace(raw)
	if utf8.RuneCountInString(v) > limit {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("%s must be at most %d characters", field, limit))
		return "", false
	}
	return v, true
}

// validateQuickActionBinding checks that the target exists in this workspace
// and, for a `public` action, that every member can invoke it. Writes the 400
// itself and returns false when the binding is rejected.
func (h *Handler) validateQuickActionBinding(w http.ResponseWriter, r *http.Request, assigneeType string, id, workspaceID pgtype.UUID, visibility string) bool {
	qa := db.QuickAction{AssigneeType: assigneeType, AssigneeID: id, WorkspaceID: workspaceID}
	target := h.resolveQuickActionTarget(r.Context(), qa)
	if !target.Found {
		writeError(w, http.StatusBadRequest, "assignee not found in this workspace")
		return false
	}
	// A public action promises "everyone can run this". Binding a target the
	// team cannot invoke would make that promise false on the first click, so
	// it is refused at write time instead.
	if visibility == "public" && !h.agentInvocableByEveryone(r.Context(), target.Agent) {
		writeError(w, http.StatusBadRequest, "a public quick action must use an agent every workspace member can trigger; make the agent public or set this action to private")
		return false
	}
	return true
}

// RenderQuickAction returns what the action WOULD post, without posting it.
//
// This backs the composer hand-off (the `/` slash command): the user gets the
// fully rendered text in the comment box to edit before sending. Rendering
// stays on the server even for this read-only path so there is exactly one
// interpolation implementation — a client-side copy would drift from the run
// path and quietly send something different from the preview.
func (h *Handler) RenderQuickAction(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := uuidToString(issue.WorkspaceID)
	idUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "quickActionId"), "quick action id")
	if !ok {
		return
	}
	qa, ok := h.loadReachableQuickAction(w, r, idUUID, issue.WorkspaceID, userID)
	if !ok {
		return
	}

	target := h.resolveQuickActionTarget(r.Context(), qa)
	if !target.Found {
		h.writeDispatchBlocked(w, http.StatusConflict, ReasonTargetUnavailable)
		return
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	// Same gate as the run path: the preview would otherwise hand a user the
	// exact text needed to trigger an agent they may not invoke.
	if !h.canInvokeAgent(r.Context(), target.Agent, actorType, actorID, h.invokeOriginatorFromRequest(r, actorType, actorID), workspaceID) {
		h.writeDispatchBlocked(w, http.StatusForbidden, ReasonInvocationNotAllowed)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"content": buildQuickActionBody(qa, target),
	})
}

// buildQuickActionBody prepends the mention line that actually triggers the
// run. It is parsed by the same ParseMentions the composer's output goes
// through, so a quick action is indistinguishable from a hand-typed @mention
// downstream. The prompt itself is passed through untouched — see the file
// header for why there is no interpolation step.
func buildQuickActionBody(qa db.QuickAction, target quickActionTarget) string {
	return fmt.Sprintf("[@%s](mention://%s/%s)\n\n%s", target.Name, target.MentionType, target.MentionID, qa.Prompt)
}

// RunQuickAction renders the action and posts it as a quick_action comment,
// then hands off to the standard comment trigger path.
//
// This is the ONLY place invoke permission is checked. The list endpoint hides
// nothing beyond `private` ownership, so a member may well click an action
// they cannot run; that returns a structured 403 the client renders as a
// dialog, which is more informative than a button that silently was not there.
//
// The success response is a CommentResponse carrying trigger_outcomes,
// identical in shape to POST /comments — so the client reuses one result
// handler and gets `coalesced` (merged into the target's pending task),
// `deferred` (runtime offline), and `blocked` for free.
func (h *Handler) RunQuickAction(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := uuidToString(issue.WorkspaceID)
	idUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "quickActionId"), "quick action id")
	if !ok {
		return
	}
	qa, ok := h.loadReachableQuickAction(w, r, idUUID, issue.WorkspaceID, userID)
	if !ok {
		return
	}
	if qa.Status != "active" {
		writeError(w, http.StatusBadRequest, "quick action is archived")
		return
	}

	target := h.resolveQuickActionTarget(r.Context(), qa)
	if !target.Found {
		h.writeDispatchBlocked(w, http.StatusConflict, ReasonTargetUnavailable)
		return
	}

	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	originatorUserID := h.invokeOriginatorFromRequest(r, actorType, actorID)
	// The single permission gate. Issue visibility never implies the right to
	// trigger someone's private agent.
	if !h.canInvokeAgent(r.Context(), target.Agent, actorType, actorID, originatorUserID, workspaceID) {
		h.writeDispatchBlocked(w, http.StatusForbidden, ReasonInvocationNotAllowed)
		return
	}

	body := sanitizeNullBytes(buildQuickActionBody(qa, target))

	created, err := h.Queries.CreateComment(r.Context(), db.CreateCommentParams{
		ID:          dbid.NewV7(),
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		AuthorType:  actorType,
		AuthorID:    parseUUID(actorID),
		Content:     body,
		// Deliberately an ordinary comment. The collapsed card is driven by
		// quick_action_id, which no client can set — `type` is client-supplied
		// on the generic comment endpoint, so a dedicated type value would be
		// forgeable and would also have cost a CHECK rebuild on a hot table.
		Type:          "comment",
		QuickActionID: qa.ID,
	})
	if err != nil {
		slog.Warn("quick action comment create failed", append(logger.RequestAttrs(r), "error", err, "quick_action_id", uuidToString(qa.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to run quick action")
		return
	}
	comment := created.Comment()

	resp := commentToResponse(comment, nil, nil)
	resp.IssueRevision = created.IssueRevision
	h.publish(protocol.EventCommentCreated, workspaceID, actorType, actorID, map[string]any{
		"comment":             resp,
		"issue_title":         issue.Title,
		"issue_assignee_type": textToPtr(issue.AssigneeType),
		"issue_assignee_id":   uuidToPtr(issue.AssigneeID),
		"issue_status":        issue.Status,
		"issue_revision":      created.IssueRevision,
	})

	resp.TriggerOutcomes = h.triggerTasksForComment(r.Context(), issue, comment, nil, actorType, actorID, originatorUserID, nil)

	// Usage telemetry is best-effort and deliberately outside the run's
	// success path: a failed counter must never cost the user the run.
	if err := h.Queries.TouchQuickActionUsage(r.Context(), db.TouchQuickActionUsageParams{ID: qa.ID, WorkspaceID: issue.WorkspaceID}); err != nil {
		slog.Debug("quick action usage touch failed", "quick_action_id", uuidToString(qa.ID), "error", err)
	}

	slog.Info("quick action run", append(logger.RequestAttrs(r),
		"quick_action_id", uuidToString(qa.ID),
		"issue_id", uuidToString(issue.ID),
		"comment_id", uuidToString(comment.ID),
	)...)
	writeJSON(w, http.StatusCreated, resp)
}
