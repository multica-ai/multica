package approvals

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/claudehook"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Handler wires the approvals service to chi routes.
// Read operations (GET) require any workspace member.
// Decisions (approve/reject/delegate) and the intake seam require admin/owner —
// enforced on the router's admin group.
//
// ToolPolicy + Agents are the optional "grant at a permission level" seam
// (TECH-3498): when an admin approves an ask with grant_at_level, the handler
// writes a permanent Allow into the unified tool-policy chain. Both are nil-safe
// — if either is unwired the approve still succeeds, only the level grant is
// skipped (best-effort secondary write).
type Handler struct {
	Svc        *Service
	ToolPolicy *toolpolicy.Store
	Agents     *db.Queries
}

func NewHandler(svc *Service) *Handler { return &Handler{Svc: svc} }

// WithToolPolicy wires the tool-policy store + agent queries the "grant at a
// permission level" path needs. Returns the handler for chaining.
func (h *Handler) WithToolPolicy(store *toolpolicy.Store, agents *db.Queries) *Handler {
	h.ToolPolicy = store
	h.Agents = agents
	return h
}

// --- response types ---------------------------------------------------------

type approvalResponse struct {
	ID              string   `json:"id"`
	WorkspaceID     string   `json:"workspace_id"`
	RequesterType   string   `json:"requester_type"`
	RequesterID     string   `json:"requester_id"`
	AgentID         *string  `json:"agent_id"`
	Capability      string   `json:"capability"`
	Resource        string   `json:"resource"`
	Reason          string   `json:"reason"`
	MatchedGrantIDs []string `json:"matched_grant_ids"`
	Context         any      `json:"context"`
	Status          string   `json:"status"`
	DecidedByID     *string  `json:"decided_by_id"`
	DecidedAt       *string  `json:"decided_at"`
	DecisionNote    *string  `json:"decision_note"`
	DelegatedToType *string  `json:"delegated_to_type"`
	DelegatedToID   *string  `json:"delegated_to_id"`
	ExpiresAt       *string  `json:"expires_at"`
	CreatedAt       string   `json:"created_at"`
	UpdatedAt       string   `json:"updated_at"`

	// Human-readable enrichment (TECH-3498). The UI shows NAMES, not raw UUIDs.
	// All best-effort: a lookup failure leaves the field empty/nil and never
	// fails the request. RequesterName always carries at least a humanized label
	// (never a raw UUID).
	RequesterName   string  `json:"requester_name"`
	AgentName       *string `json:"agent_name,omitempty"`
	DecidedByName   *string `json:"decided_by_name,omitempty"`
	DelegatedToName *string `json:"delegated_to_name,omitempty"`
	IssueID         *string `json:"issue_id,omitempty"`
	IssueIdentifier *string `json:"issue_identifier,omitempty"`
	IssueTitle      *string `json:"issue_title,omitempty"`

	// TriggeredBy names the human (member) whose message/task started the agent
	// run that hit this approval, so the inbox can show "requested by <member>".
	// Best-effort: empty/nil when no trigger context is available.
	TriggeredByID   *string `json:"triggered_by_id,omitempty"`
	TriggeredByName *string `json:"triggered_by_name,omitempty"`
}

type approvalAuditResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	ApprovalID  *string `json:"approval_id"`
	Action      string  `json:"action"`
	ActorType   *string `json:"actor_type"`
	ActorID     *string `json:"actor_id"`
	ActorName   *string `json:"actor_name"`
	Via         string  `json:"via"`
	Note        *string `json:"note"`
	CreatedAt   string  `json:"created_at"`
}

// --- request types ----------------------------------------------------------

type decisionRequest struct {
	Note string `json:"note"`
	// GrantForSeconds time-boxes an approve (TECH-3498): >0 makes it reusable for
	// that many seconds, 0 / omitted = a one-shot approve (now()).
	GrantForSeconds int `json:"grant_for_seconds,omitempty"`
	// GrantAtLevel optionally escalates the approve to a permanent Allow in the
	// tool-policy chain: "agent" | "runtime" | "workspace". Empty = no escalation.
	GrantAtLevel string `json:"grant_at_level,omitempty"`
}

type delegateRequest struct {
	ToType string `json:"to_type"` // member | group
	ToID   string `json:"to_id"`
	Note   string `json:"note"`
}

// intakeRequest backs the admin-only intake seam. The live enforcement gate
// calls Service.Intake directly; this HTTP endpoint exists so the inbox flow
// can be exercised end-to-end before phase 2's gate is wired in.
type intakeRequest struct {
	RequesterType   string         `json:"requester_type"`
	RequesterID     string         `json:"requester_id"`
	AgentID         *string        `json:"agent_id"`
	Capability      string         `json:"capability"`
	Resource        string         `json:"resource"`
	Reason          string         `json:"reason"`
	MatchedGrantIDs []string       `json:"matched_grant_ids"`
	Context         map[string]any `json:"context"`
	ExpiresAt       *string        `json:"expires_at"`
}

// --- handlers ---------------------------------------------------------------

// List — GET /api/workspaces/{id}/approvals?status=&limit=&offset=
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	_, workspaceID, ok := h.loadWorkspace(w, r)
	if !ok {
		return
	}

	f := ListFilter{Status: r.URL.Query().Get("status")}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 200 {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		f.Limit = int32(n)
	}
	if raw := r.URL.Query().Get("offset"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "invalid offset")
			return
		}
		f.Offset = int32(n)
	}

	rows, err := h.Svc.List(r.Context(), workspaceID, f)
	if err != nil {
		h.serverError(w, r, "list approvals", err)
		return
	}
	pending, err := h.Svc.CountPending(r.Context(), workspaceID)
	if err != nil {
		h.serverError(w, r, "count pending approvals", err)
		return
	}

	items := make([]approvalResponse, len(rows))
	total := int32(0)
	cache := newEnrichCache()
	for i, row := range rows {
		if row.Total > total {
			total = row.Total
		}
		base := listRowToRequest(row)
		items[i] = h.enrich(r.Context(), requestToResponse(base), base, cache)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"approvals": items,
		"total":     total,
		"pending":   pending,
		"limit":     f.Limit,
		"offset":    f.Offset,
	})
}

// Get — GET /api/workspaces/{id}/approvals/{approvalId}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	_, workspaceID, ok := h.loadWorkspace(w, r)
	if !ok {
		return
	}
	id, ok := h.parseApprovalID(w, r)
	if !ok {
		return
	}
	row, err := h.Svc.Get(r.Context(), id, workspaceID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "approval not found")
			return
		}
		h.serverError(w, r, "get approval", err)
		return
	}
	writeJSON(w, http.StatusOK, h.enrich(r.Context(), requestToResponse(row), row, nil))
}

// Audit — GET /api/workspaces/{id}/approvals/audit?approval_id=&limit=&offset=
func (h *Handler) Audit(w http.ResponseWriter, r *http.Request) {
	_, workspaceID, ok := h.loadWorkspace(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	filter := cerebrodb.ListCerebroApprovalAuditParams{
		WorkspaceID: workspaceID,
		Limit:       50,
		Offset:      0,
	}
	if raw := q.Get("approval_id"); raw != "" {
		id, err := util.ParseUUID(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid approval_id")
			return
		}
		filter.Column2 = id
	}
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 200 {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		filter.Limit = int32(n)
	}
	if raw := q.Get("offset"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "invalid offset")
			return
		}
		filter.Offset = int32(n)
	}

	rows, err := h.Svc.Cerebro.ListCerebroApprovalAudit(r.Context(), filter)
	if err != nil {
		h.serverError(w, r, "list approval audit", err)
		return
	}
	items := make([]approvalAuditResponse, len(rows))
	total := int32(0)
	for i, row := range rows {
		if row.Total > total {
			total = row.Total
		}
		items[i] = auditRowToResponse(row)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":  items,
		"total":  total,
		"limit":  filter.Limit,
		"offset": filter.Offset,
	})
}

// Approve — POST /api/workspaces/{id}/approvals/{approvalId}/approve
//
// Body (all optional): { note, grant_for_seconds, grant_at_level }. The approver
// can time-box the grant (grant_for_seconds) and/or escalate it to a permanent
// Allow in the tool-policy chain (grant_at_level). The level write is best-effort
// secondary: a failure logs a warning but the approve still succeeds (TECH-3498).
func (h *Handler) Approve(w http.ResponseWriter, r *http.Request) {
	member, workspaceID, ok := h.loadWorkspace(w, r)
	if !ok {
		return
	}
	id, ok := h.parseApprovalID(w, r)
	if !ok {
		return
	}
	req := decisionRequest{}
	_ = decodeOptional(r, &req)
	if req.GrantAtLevel != "" && !validGrantLevel(req.GrantAtLevel) {
		writeError(w, http.StatusBadRequest, "grant_at_level must be 'agent', 'runtime' or 'workspace'")
		return
	}

	// Caller computes the expiry: a period grant (grant_for_seconds > 0) is
	// reusable until it lapses; a one-shot approve expires immediately (now()) so
	// only the in-flight blocked call proceeds, not a future one.
	now := time.Now()
	expiresAt := pgtype.Timestamptz{Time: now, Valid: true}
	if req.GrantForSeconds > 0 {
		expiresAt = pgtype.Timestamptz{Time: now.Add(time.Duration(req.GrantForSeconds) * time.Second), Valid: true}
	}

	row, err := h.Svc.Approve(r.Context(), id, workspaceID, member.UserID, req.Note, surfaceFromHeader(r.Header.Get("X-Cerebro-Surface")), expiresAt)
	if err != nil {
		h.writeDecisionError(w, r, "decide approval", err)
		return
	}

	// Grant-at-level: best-effort permanent Allow. A failure must NOT fail the
	// approve — the ask is already approved.
	if req.GrantAtLevel != "" {
		if grantErr := h.grantAtLevel(r.Context(), row, req.GrantAtLevel, member.UserID); grantErr != nil {
			slog.Warn("approval grant-at-level write failed (approve still succeeded)",
				append(logger.RequestAttrs(r), "approval_id", util.UUIDToString(row.ID), "level", req.GrantAtLevel, "error", grantErr)...)
		}
	}

	writeJSON(w, http.StatusOK, h.enrich(r.Context(), requestToResponse(row), row, nil))
}

// Reject — POST /api/workspaces/{id}/approvals/{approvalId}/reject
func (h *Handler) Reject(w http.ResponseWriter, r *http.Request) {
	h.decide(w, r, h.Svc.Reject)
}

// decideFn is the shared shape of Service.Reject (and any decision that takes no
// expiry). Approve has its own handler because it threads expiry + level grant.
type decideFn func(ctx context.Context, id, workspaceID, actorID pgtype.UUID, note, surface string) (cerebrodb.CerebroApprovalRequest, error)

func (h *Handler) decide(w http.ResponseWriter, r *http.Request, fn decideFn) {
	member, workspaceID, ok := h.loadWorkspace(w, r)
	if !ok {
		return
	}
	id, ok := h.parseApprovalID(w, r)
	if !ok {
		return
	}
	req := decisionRequest{}
	_ = decodeOptional(r, &req)

	row, err := fn(r.Context(), id, workspaceID, member.UserID, req.Note, surfaceFromHeader(r.Header.Get("X-Cerebro-Surface")))
	if err != nil {
		h.writeDecisionError(w, r, "decide approval", err)
		return
	}
	writeJSON(w, http.StatusOK, h.enrich(r.Context(), requestToResponse(row), row, nil))
}

// validGrantLevel reports whether a grant_at_level string is one of the three
// chain layers an approver may escalate to.
func validGrantLevel(level string) bool {
	switch level {
	case "agent", "runtime", "workspace":
		return true
	default:
		return false
	}
}

// grantAtLevel writes a permanent Allow into the unified tool-policy chain after
// a successful approve, derived from the approval row (TECH-3498):
//   - connection tool → ToolKey "connection:<conn>", ResourcePattern = the tool name.
//   - other tool      → ToolKey claudehook.PolicyToolKey(capability), ResourcePattern "".
//
// Layer + subject: agent → LayerAgent + approval.agent_id; runtime → LayerRuntime
// + the agent's runtime_id; workspace → LayerWorkspace + zero subject. Returns an
// error so the caller can log it; the approve never fails on a grant error.
func (h *Handler) grantAtLevel(ctx context.Context, row cerebrodb.CerebroApprovalRequest, level string, actorID pgtype.UUID) error {
	if h.ToolPolicy == nil {
		return errors.New("tool-policy store not wired")
	}

	conn, connTool := approvalConnection(row.Context)
	toolKey := claudehook.PolicyToolKey(row.Capability)
	resourcePattern := ""
	if connTool && conn != "" {
		toolKey = "connection:" + conn
		resourcePattern = row.Capability
	}

	layer := toolpolicy.LayerAgent
	subject := row.AgentID
	switch level {
	case "agent":
		layer = toolpolicy.LayerAgent
		subject = row.AgentID
	case "runtime":
		layer = toolpolicy.LayerRuntime
		if h.Agents == nil {
			return errors.New("agent queries not wired for runtime grant")
		}
		if !row.AgentID.Valid {
			return errors.New("runtime grant needs an agent on the approval")
		}
		agent, err := h.Agents.GetAgent(ctx, row.AgentID)
		if err != nil {
			return err
		}
		subject = agent.RuntimeID
	case "workspace":
		layer = toolpolicy.LayerWorkspace
		subject = pgtype.UUID{} // zero subject = the workspace-wide row
	}

	_, err := h.ToolPolicy.Set(ctx, toolpolicy.SetParams{
		WorkspaceID:     row.WorkspaceID,
		ToolKey:         toolKey,
		Layer:           layer,
		SubjectID:       subject,
		ResourcePattern: resourcePattern,
		Setting:         toolpolicy.SettingAllow,
		UpdatedBy:       actorID,
	})
	return err
}

// --- human-readable enrichment (TECH-3498) ----------------------------------

// enrichCache memoizes name/issue lookups for the span of a single List call so
// an inbox of N rows that share an agent or an issue does the lookup once. A nil
// cache is valid (Get/decision endpoints enrich a single row, no reuse).
type enrichCache struct {
	agents map[pgtype.UUID]string // agent_id → name ("" = looked up, not found)
	users  map[pgtype.UUID]string // user_id  → name
	issues map[pgtype.UUID]issueRef
}

type issueRef struct {
	identifier string
	title      string
}

func newEnrichCache() *enrichCache {
	return &enrichCache{
		agents: map[pgtype.UUID]string{},
		users:  map[pgtype.UUID]string{},
		issues: map[pgtype.UUID]issueRef{},
	}
}

// approvalIssueContext pulls issue_id (preferred) and task_id (fallback) out of
// an approval row's stored Context JSON. Both default to "" when absent.
func approvalIssueContext(raw []byte) (issueID, taskID string) {
	if len(raw) == 0 {
		return "", ""
	}
	var ctx struct {
		IssueID string `json:"issue_id"`
		TaskID  string `json:"task_id"`
	}
	if err := json.Unmarshal(raw, &ctx); err != nil {
		return "", ""
	}
	return ctx.IssueID, ctx.TaskID
}

// approvalTriggerContext pulls the triggering human (trigger_user_id +
// trigger_user_name) out of an approval row's stored Context JSON. Both default
// to "" when absent — the gateway populates them from the run's meta (TECH-3498).
func approvalTriggerContext(raw []byte) (userID, userName string) {
	if len(raw) == 0 {
		return "", ""
	}
	var ctx struct {
		TriggerUserID   string `json:"trigger_user_id"`
		TriggerUserName string `json:"trigger_user_name"`
	}
	if err := json.Unmarshal(raw, &ctx); err != nil {
		return "", ""
	}
	return ctx.TriggerUserID, ctx.TriggerUserName
}

// enrich fills the human-readable name/issue fields on a response from the row.
// Best-effort: any lookup error leaves that field empty/nil and is logged at
// warn — it NEVER fails the request. requester_name always carries at least a
// humanized type label so the UI never has to fall back to a raw UUID.
//
// cache may be nil (single-row enrich); pass one across a List to dedupe lookups.
func (h *Handler) enrich(ctx context.Context, resp approvalResponse, row cerebrodb.CerebroApprovalRequest, cache *enrichCache) approvalResponse {
	// Requester + agent names.
	if row.AgentID.Valid {
		if name := h.agentName(ctx, row.AgentID, cache); name != "" {
			resp.AgentName = &name
			resp.RequesterName = name
		}
	}
	if resp.RequesterName == "" {
		switch row.RequesterType {
		case RequesterMember:
			if name := h.userName(ctx, row.RequesterID, cache); name != "" {
				resp.RequesterName = name
			}
		case RequesterAgent:
			if name := h.agentName(ctx, row.RequesterID, cache); name != "" {
				resp.RequesterName = name
			}
		}
	}
	if resp.RequesterName == "" {
		resp.RequesterName = humanizeRequesterType(row.RequesterType)
	}

	// Decided-by name (members decide approvals).
	if row.DecidedByID.Valid {
		if name := h.userName(ctx, row.DecidedByID, cache); name != "" {
			resp.DecidedByName = &name
		}
	}

	// Delegated-to name: member resolves to a user name; group leaves the
	// type label (no group-name query in the upstream query set).
	if row.DelegatedToID.Valid && row.DelegatedToType.Valid {
		switch row.DelegatedToType.String {
		case DelegateToMember:
			if name := h.userName(ctx, row.DelegatedToID, cache); name != "" {
				resp.DelegatedToName = &name
			}
		}
	}

	// Issue: context.issue_id (preferred) → context.task_id → GetAgentTask.IssueID.
	issueID := h.resolveIssueID(ctx, row.Context, cache)
	if issueID.Valid {
		ref := h.issueRef(ctx, row.WorkspaceID, issueID, cache)
		if ref.identifier != "" || ref.title != "" {
			s := util.UUIDToString(issueID)
			resp.IssueID = &s
			if ref.identifier != "" {
				resp.IssueIdentifier = &ref.identifier
			}
			if ref.title != "" {
				resp.IssueTitle = &ref.title
			}
		}
	}

	// Triggering human: the member whose message/task started the run. Prefer the
	// trigger_user_* fields the gateway stored; fall back to the task's original
	// human principal. Best-effort — never fails the request, never leaks a UUID.
	if id, name := h.resolveTriggeredBy(ctx, row.Context, cache); name != "" {
		resp.TriggeredByName = &name
		if id != "" {
			resp.TriggeredByID = &id
		}
	}

	return resp
}

// resolveTriggeredBy returns the (id, name) of the human who triggered the run
// behind an approval. It prefers context.trigger_user_name / trigger_user_id
// (set by the gateway); when no name is in context it resolves the name from
// trigger_user_id, and when no trigger context exists at all it falls back to the
// task's OriginalUserID (the task's human origin). Best-effort: any miss yields
// "" and is logged at warn — it never returns a raw UUID as a name.
func (h *Handler) resolveTriggeredBy(ctx context.Context, rawContext []byte, cache *enrichCache) (id, name string) {
	if h.Agents == nil {
		return "", ""
	}
	triggerID, triggerName := approvalTriggerContext(rawContext)
	if triggerName != "" || triggerID != "" {
		if uid, err := util.ParseUUID(triggerID); err == nil && uid.Valid {
			id = util.UUIDToString(uid)
			if triggerName == "" {
				triggerName = h.userName(ctx, uid, cache)
			}
		}
		return id, triggerName
	}

	// Fallback: resolve the task's original human principal.
	_, taskRaw := approvalIssueContext(rawContext)
	if taskRaw == "" {
		return "", ""
	}
	taskID, err := util.ParseUUID(taskRaw)
	if err != nil || !taskID.Valid {
		return "", ""
	}
	task, err := h.Agents.GetAgentTask(ctx, taskID)
	if err != nil {
		slog.Warn("approval enrich: triggered-by task lookup failed", "task_id", taskRaw, "error", err)
		return "", ""
	}
	if !task.OriginalUserID.Valid {
		return "", ""
	}
	name = h.userName(ctx, task.OriginalUserID, cache)
	if name == "" {
		return "", ""
	}
	return util.UUIDToString(task.OriginalUserID), name
}

func (h *Handler) agentName(ctx context.Context, id pgtype.UUID, cache *enrichCache) string {
	if h.Agents == nil || !id.Valid {
		return ""
	}
	if cache != nil {
		if v, ok := cache.agents[id]; ok {
			return v
		}
	}
	agent, err := h.Agents.GetAgent(ctx, id)
	name := ""
	if err != nil {
		slog.Warn("approval enrich: agent lookup failed", "agent_id", util.UUIDToString(id), "error", err)
	} else {
		name = agent.Name
	}
	if cache != nil {
		cache.agents[id] = name
	}
	return name
}

func (h *Handler) userName(ctx context.Context, id pgtype.UUID, cache *enrichCache) string {
	if h.Agents == nil || !id.Valid {
		return ""
	}
	if cache != nil {
		if v, ok := cache.users[id]; ok {
			return v
		}
	}
	user, err := h.Agents.GetUser(ctx, id)
	name := ""
	if err != nil {
		slog.Warn("approval enrich: user lookup failed", "user_id", util.UUIDToString(id), "error", err)
	} else {
		name = user.Name
	}
	if cache != nil {
		cache.users[id] = name
	}
	return name
}

// resolveIssueID prefers context.issue_id; falls back to context.task_id →
// GetAgentTask.IssueID. Returns an invalid UUID when neither resolves.
func (h *Handler) resolveIssueID(ctx context.Context, rawContext []byte, cache *enrichCache) pgtype.UUID {
	issueRaw, taskRaw := approvalIssueContext(rawContext)
	if issueRaw != "" {
		if id, err := util.ParseUUID(issueRaw); err == nil && id.Valid {
			return id
		}
	}
	if h.Agents == nil || taskRaw == "" {
		return pgtype.UUID{}
	}
	taskID, err := util.ParseUUID(taskRaw)
	if err != nil || !taskID.Valid {
		return pgtype.UUID{}
	}
	task, err := h.Agents.GetAgentTask(ctx, taskID)
	if err != nil {
		slog.Warn("approval enrich: task lookup failed", "task_id", taskRaw, "error", err)
		return pgtype.UUID{}
	}
	return task.IssueID
}

func (h *Handler) issueRef(ctx context.Context, workspaceID, issueID pgtype.UUID, cache *enrichCache) issueRef {
	if h.Agents == nil || !issueID.Valid {
		return issueRef{}
	}
	if cache != nil {
		if v, ok := cache.issues[issueID]; ok {
			return v
		}
	}
	ref := issueRef{}
	issue, err := h.Agents.GetIssue(ctx, issueID)
	if err != nil {
		slog.Warn("approval enrich: issue lookup failed", "issue_id", util.UUIDToString(issueID), "error", err)
	} else {
		ref.title = issue.Title
		prefix := h.issuePrefix(ctx, workspaceID)
		if prefix != "" {
			ref.identifier = prefix + "-" + strconv.Itoa(int(issue.Number))
		} else {
			ref.identifier = "#" + strconv.Itoa(int(issue.Number))
		}
	}
	if cache != nil {
		cache.issues[issueID] = ref
	}
	return ref
}

// issuePrefix returns the workspace's issue-key prefix (e.g. "TECH") used to
// build a human-readable issue identifier. Mirrors handler.getIssuePrefix:
// prefer the stored prefix, else derive a short uppercase token from the name.
func (h *Handler) issuePrefix(ctx context.Context, workspaceID pgtype.UUID) string {
	if h.Agents == nil || !workspaceID.Valid {
		return ""
	}
	ws, err := h.Agents.GetWorkspace(ctx, workspaceID)
	if err != nil {
		slog.Warn("approval enrich: workspace lookup failed", "workspace_id", util.UUIDToString(workspaceID), "error", err)
		return ""
	}
	if ws.IssuePrefix != "" {
		return ws.IssuePrefix
	}
	return deriveIssuePrefix(ws.Name)
}

// deriveIssuePrefix is the fallback used when a workspace has no stored prefix —
// the first up-to-3 alphabetic characters of the name, uppercased ("WS" when the
// name has no letters). Kept in sync with handler.generateIssuePrefix.
func deriveIssuePrefix(name string) string {
	letters := make([]rune, 0, 3)
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			letters = append(letters, r)
			if len(letters) == 3 {
				break
			}
		}
	}
	if len(letters) == 0 {
		return "WS"
	}
	return strings.ToUpper(string(letters))
}

// humanizeRequesterType maps a requester_type to a plain label so the UI never
// shows a raw UUID even when the name lookup fails.
func humanizeRequesterType(t string) string {
	switch t {
	case RequesterAgent:
		return "Agent"
	case RequesterMember:
		return "Member"
	default:
		return "Unknown"
	}
}

// approvalConnection pulls the connection name + connection_tool flag out of an
// approval row's stored Context JSON. Both default to zero values when absent.
func approvalConnection(raw []byte) (conn string, connTool bool) {
	if len(raw) == 0 {
		return "", false
	}
	var ctx struct {
		Connection     string `json:"connection"`
		ConnectionTool bool   `json:"connection_tool"`
	}
	if err := json.Unmarshal(raw, &ctx); err != nil {
		return "", false
	}
	return ctx.Connection, ctx.ConnectionTool
}

// Delegate — POST /api/workspaces/{id}/approvals/{approvalId}/delegate
func (h *Handler) Delegate(w http.ResponseWriter, r *http.Request) {
	member, workspaceID, ok := h.loadWorkspace(w, r)
	if !ok {
		return
	}
	id, ok := h.parseApprovalID(w, r)
	if !ok {
		return
	}
	var req delegateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ToType != DelegateToMember && req.ToType != DelegateToGroup {
		writeError(w, http.StatusBadRequest, "to_type must be 'member' or 'group'")
		return
	}
	toID, err := util.ParseUUID(req.ToID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid to_id")
		return
	}

	row, err := h.Svc.Delegate(r.Context(), id, workspaceID, member.UserID, req.ToType, toID, req.Note, surfaceFromHeader(r.Header.Get("X-Cerebro-Surface")))
	if err != nil {
		h.writeDecisionError(w, r, "delegate approval", err)
		return
	}
	writeJSON(w, http.StatusOK, h.enrich(r.Context(), requestToResponse(row), row, nil))
}

// Intake — POST /api/workspaces/{id}/approvals/intake (admin/owner).
// Test/integration seam; the live gate calls Service.Intake directly.
func (h *Handler) Intake(w http.ResponseWriter, r *http.Request) {
	member, workspaceID, ok := h.loadWorkspace(w, r)
	if !ok {
		return
	}
	var req intakeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.RequesterType != RequesterMember && req.RequesterType != RequesterAgent {
		writeError(w, http.StatusBadRequest, "requester_type must be 'member' or 'agent'")
		return
	}
	if req.Capability == "" {
		writeError(w, http.StatusBadRequest, "capability required")
		return
	}

	requesterID := member.UserID
	if req.RequesterID != "" {
		parsed, err := util.ParseUUID(req.RequesterID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid requester_id")
			return
		}
		requesterID = parsed
	}

	p := IntakeParams{
		WorkspaceID:   workspaceID,
		RequesterType: req.RequesterType,
		RequesterID:   requesterID,
		Capability:    req.Capability,
		Resource:      req.Resource,
		Reason:        req.Reason,
		Context:       req.Context,
		Surface:       surfaceFromHeader(r.Header.Get("X-Cerebro-Surface")),
	}
	if req.AgentID != nil {
		agentID, err := util.ParseUUID(*req.AgentID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid agent_id")
			return
		}
		p.Agent = agentID
	}
	for _, g := range req.MatchedGrantIDs {
		gid, err := util.ParseUUID(g)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid matched_grant_id")
			return
		}
		p.MatchedGrantIDs = append(p.MatchedGrantIDs, gid)
	}
	if req.ExpiresAt != nil && *req.ExpiresAt != "" {
		var ts pgtype.Timestamptz
		if err := ts.Scan(*req.ExpiresAt); err != nil {
			writeError(w, http.StatusBadRequest, "invalid expires_at")
			return
		}
		p.ExpiresAt = ts
	}

	row, err := h.Svc.Intake(r.Context(), p)
	if err != nil {
		h.serverError(w, r, "intake approval", err)
		return
	}
	writeJSON(w, http.StatusCreated, h.enrich(r.Context(), requestToResponse(row), row, nil))
}

// --- helpers ----------------------------------------------------------------

func (h *Handler) loadWorkspace(w http.ResponseWriter, r *http.Request) (db.Member, pgtype.UUID, bool) {
	member, ok := middleware.MemberFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return db.Member{}, pgtype.UUID{}, false
	}
	wsID, err := util.ParseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return db.Member{}, pgtype.UUID{}, false
	}
	return member, wsID, true
}

func (h *Handler) parseApprovalID(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	id, err := util.ParseUUID(chi.URLParam(r, "approvalId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid approval id")
		return pgtype.UUID{}, false
	}
	return id, true
}

func (h *Handler) writeDecisionError(w http.ResponseWriter, r *http.Request, op string, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "approval not found")
	case errors.Is(err, ErrAlreadyDecided):
		// 409 lets the UI tell the approver someone else already handled it.
		writeError(w, http.StatusConflict, "approval already decided")
	default:
		h.serverError(w, r, op, err)
	}
}

func (h *Handler) serverError(w http.ResponseWriter, r *http.Request, op string, err error) {
	slog.Error("approvals request failed", append(logger.RequestAttrs(r), "op", op, "error", err)...)
	writeError(w, http.StatusInternalServerError, op+" failed")
}

func decodeOptional(r *http.Request, v any) error {
	if r.Body == nil || r.ContentLength == 0 {
		return nil
	}
	return json.NewDecoder(r.Body).Decode(v)
}

func surfaceFromHeader(h string) string {
	switch h {
	case SurfaceCLI, SurfaceUI, SurfaceMCP:
		return h
	}
	return SurfaceAPI
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func requestToResponse(a cerebrodb.CerebroApprovalRequest) approvalResponse {
	return approvalResponse{
		ID:              util.UUIDToString(a.ID),
		WorkspaceID:     util.UUIDToString(a.WorkspaceID),
		RequesterType:   a.RequesterType,
		RequesterID:     util.UUIDToString(a.RequesterID),
		AgentID:         util.UUIDToPtr(a.AgentID),
		Capability:      a.Capability,
		Resource:        a.Resource,
		Reason:          a.Reason,
		MatchedGrantIDs: decodeGrantIDs(a.MatchedGrantIds),
		Context:         decodeJSON(a.Context),
		Status:          a.Status,
		DecidedByID:     util.UUIDToPtr(a.DecidedByID),
		DecidedAt:       timestampPtr(a.DecidedAt),
		DecisionNote:    util.TextToPtr(a.DecisionNote),
		DelegatedToType: util.TextToPtr(a.DelegatedToType),
		DelegatedToID:   util.UUIDToPtr(a.DelegatedToID),
		ExpiresAt:       timestampPtr(a.ExpiresAt),
		CreatedAt:       util.TimestampToString(a.CreatedAt),
		UpdatedAt:       util.TimestampToString(a.UpdatedAt),
	}
}

// listRowToRequest projects a list row back into the canonical row struct so the
// shared requestToResponse + enrich pipeline can run over it.
func listRowToRequest(a cerebrodb.ListCerebroApprovalRequestsRow) cerebrodb.CerebroApprovalRequest {
	return cerebrodb.CerebroApprovalRequest{
		ID:              a.ID,
		WorkspaceID:     a.WorkspaceID,
		RequesterType:   a.RequesterType,
		RequesterID:     a.RequesterID,
		AgentID:         a.AgentID,
		Capability:      a.Capability,
		Resource:        a.Resource,
		Reason:          a.Reason,
		MatchedGrantIds: a.MatchedGrantIds,
		Context:         a.Context,
		Status:          a.Status,
		DecidedByID:     a.DecidedByID,
		DecidedAt:       a.DecidedAt,
		DecisionNote:    a.DecisionNote,
		DelegatedToType: a.DelegatedToType,
		DelegatedToID:   a.DelegatedToID,
		ExpiresAt:       a.ExpiresAt,
		CreatedAt:       a.CreatedAt,
		UpdatedAt:       a.UpdatedAt,
	}
}

func auditRowToResponse(row cerebrodb.ListCerebroApprovalAuditRow) approvalAuditResponse {
	var actorName *string
	if row.ActorName != "" {
		actorName = &row.ActorName
	}
	return approvalAuditResponse{
		ID:          util.UUIDToString(row.ID),
		WorkspaceID: util.UUIDToString(row.WorkspaceID),
		ApprovalID:  util.UUIDToPtr(row.ApprovalID),
		Action:      row.Action,
		ActorType:   util.TextToPtr(row.ActorType),
		ActorID:     util.UUIDToPtr(row.ActorID),
		ActorName:   actorName,
		Via:         row.Surface,
		Note:        util.TextToPtr(row.Note),
		CreatedAt:   util.TimestampToString(row.CreatedAt),
	}
}

func timestampPtr(t pgtype.Timestamptz) *string {
	if !t.Valid {
		return nil
	}
	s := util.TimestampToString(t)
	return &s
}

func decodeGrantIDs(b []byte) []string {
	out := []string{}
	if len(b) == 0 {
		return out
	}
	_ = json.Unmarshal(b, &out)
	if out == nil {
		out = []string{}
	}
	return out
}

func decodeJSON(b []byte) any {
	if len(b) == 0 {
		return map[string]any{}
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return map[string]any{}
	}
	return v
}
