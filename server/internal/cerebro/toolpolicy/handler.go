package toolpolicy

// handler.go exposes the tool-policy engine over HTTP (FIR-2230 phase 2 — the
// data layer the admin screen reads from). It is deliberately thin: the table
// view and the resolution live in table.go / chain.go; here we only translate
// request strings into a TableQuery / SetParams and shape the JSON.
//
// Routes (mounted under /api/workspaces/{id} in cmd/server/router.go):
//
//	GET    /tool-policy   list every tool with per-layer settings + Effective,
//	                      for the (runtime, agent, user, groups) context in the
//	                      query string. Any workspace member.
//	PUT    /tool-policy   set one layer's choice for one tool. Admin/owner.
//	DELETE /tool-policy   clear one layer's choice for one tool. Admin/owner.
//
// Read is member-level; writes are gated to admin/owner by the router group,
// mirroring grants. The admin table never shows raw ids: Title/Category come
// from the capability register, and subject ids are resolved to names by the
// frontend from data it already holds (members, agents, runtimes, groups).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// RegistryProjection carries everything the firtal_registry per-data-source
// fold-in (FIR-1609 Phase 5) needs to project: the workspace's data-source
// catalog and the agent's grant. The handler owns the FDR lister + the
// agent_tool_grant read and hands these in, so this package keeps no registry-API
// or grant dependency and toolpolicy.Table (the pure builder) stays pure.
type RegistryProjection struct {
	DataSources []RegistryDataSource
	Grant       RegistryGrant
}

// RegistryRowsFunc lists the registry projection for one (workspace, agent). It
// returns ok=false when the fold-in does not apply (no agent in the query, the
// registry is not configured for the workspace, or there are no data sources), in
// which case the table is unchanged. A non-nil error is logged but never fails
// the whole table — the registry fold-in is best-effort, like the web_fetch one.
type RegistryRowsFunc func(ctx context.Context, workspaceID, agentID pgtype.UUID) (RegistryProjection, bool, error)

// SystemOwnerFunc resolves the human a System actor is capped on at authoring
// time (FIR-1609): a System-layer subject is an autopilot, and an autopilot is
// born from — and capped at — its owner. ok=false means the subject is not a
// known autopilot with a resolvable human owner (e.g. an agent-created
// autopilot), in which case the cap check is skipped rather than blocking the
// write. The handler injects this so the toolpolicy package keeps no autopilot /
// main-db dependency.
type SystemOwnerFunc func(ctx context.Context, workspaceID, autopilotID pgtype.UUID) (ownerID pgtype.UUID, ok bool, err error)

// Handler wires the tool-policy Store to chi routes.
type Handler struct {
	Store *Store
	// RegistryRows, when set, supplies the firtal_registry per-data-source
	// projection appended to the table for an agent-scoped query (Phase 5). Left
	// nil (e.g. in tests) the table degrades to the pure builder's rows.
	RegistryRows RegistryRowsFunc
	// SystemOwner, when set, resolves the owner a System-layer (autopilot) subject
	// is capped on, so a System rule that would be looser than its owner's own
	// ceiling is rejected at authoring time. Left nil the cap check is skipped (the
	// resolution-time owner ceiling still caps every run regardless).
	SystemOwner SystemOwnerFunc
}

// NewHandler constructs a Handler over the given Store.
func NewHandler(s *Store) *Handler { return &Handler{Store: s} }

// --- response types ---------------------------------------------------------

// layerSettings is the explicit per-layer choice for one tool, each field nil
// when that layer carries no explicit setting (Inherit). Keeping them as
// pointers lets the UI distinguish "no override" from an explicit "inherit".
type layerSettings struct {
	Workspace *string `json:"workspace"`
	Runtime   *string `json:"runtime"`
	Agent     *string `json:"agent"`
	Group     *string `json:"group"`
	User      *string `json:"user"`
	// System is the per-autopilot mandate-actor layer (FIR-1609), populated only
	// when the view is scoped to an autopilot (system_id query param). null on the
	// human-context views that pass no autopilot.
	System *string `json:"system"`
}

// conditionResponse mirrors toolpolicy.Condition over the wire — the optional
// WHEN layer (FIR-1609) on a rule. All fields omitempty so a row with no
// condition serializes to null, not an empty object.
type conditionResponse struct {
	HostAllowlist []string   `json:"host_allowlist,omitempty"`
	Actions       []string   `json:"actions,omitempty"`
	ArgAllowlist  []ArgAllow `json:"arg_allowlist,omitempty"`
	Expr          string     `json:"expr,omitempty"`
}

// layerConditions carries the per-layer Condition for one tool, each field nil
// when that layer's rule has no condition ("always applies"). Mirrors
// layerSettings; Group is omitted because the group layer is a combined value
// for which a single condition is undefined (see TableRow.Conditions).
type layerConditions struct {
	Workspace *conditionResponse `json:"workspace"`
	Runtime   *conditionResponse `json:"runtime"`
	Agent     *conditionResponse `json:"agent"`
	User      *conditionResponse `json:"user"`
	System    *conditionResponse `json:"system"`
}

type effectiveResponse struct {
	Setting   string `json:"setting"`
	DecidedBy string `json:"decided_by"`
	CappedBy  string `json:"capped_by"`
	Reason    string `json:"reason"`
	// Openable is a display hint (FIR-3091): true when the verdict resolved under
	// the member-override model, so a member layer may still open a broader
	// default. The admin table uses it to reserve the lock/"no effect" affordance
	// for genuine floors instead of every workspace Deny. Omitted-by-older-backend
	// drifts to false client-side, which fails safe (treated as an unopenable floor).
	Openable bool `json:"openable"`
}

// groupAttributionResponse names a group that drives a group-layer cap on a row
// and the person who owns it, so the UI can say "Capped by group <name>
// (owner: <person>)" (TECH-3287 hul 5).
type groupAttributionResponse struct {
	Name  string `json:"name"`
	Owner string `json:"owner"`
}

type toolPolicyRow struct {
	ToolKey           string          `json:"tool_key"`
	ResourcePattern   string          `json:"resource_pattern"`
	Title             string          `json:"title"`
	Category          string          `json:"category"`
	Description       string          `json:"description"`
	DescriptionZh     string          `json:"description_zh"`
	Source            string          `json:"source"`
	ManagedExternally bool            `json:"managed_externally"`
	Layers            layerSettings   `json:"layers"`
	Conditions        layerConditions `json:"conditions"`
	// EnforcedConditions names the WHEN condition kinds (action/host/cel) that
	// actually bite for this capability, so the editor renders only those
	// (FIR-1708 C). Derived server-side from the gate facts; see
	// EnforcedConditionKinds.
	EnforcedConditions []string                   `json:"enforced_conditions"`
	Effective          effectiveResponse          `json:"effective"`
	CappedByGroups     []groupAttributionResponse `json:"capped_by_groups"`
}

// --- request types ----------------------------------------------------------

type setRequest struct {
	ToolKey         string `json:"tool_key"`
	Layer           string `json:"layer"`
	SubjectID       string `json:"subject_id"`
	ResourcePattern string `json:"resource_pattern"`
	Setting         string `json:"setting"`
	// Condition is the optional WHEN layer (FIR-1609). Omit or send null to store
	// no condition; a zero-value condition is also treated as "no condition".
	Condition *conditionRequest `json:"condition"`
}

// conditionRequest is the wire form of a Condition on a write. It mirrors
// conditionResponse; an absent or all-empty condition stores NULL.
type conditionRequest struct {
	HostAllowlist []string   `json:"host_allowlist"`
	Actions       []string   `json:"actions"`
	ArgAllowlist  []ArgAllow `json:"arg_allowlist"`
	Expr          string     `json:"expr"`
}

// toCondition converts the request form to the domain Condition, or nil when the
// request carries no constraint (so the store writes NULL).
func (c *conditionRequest) toCondition() *Condition {
	if c == nil {
		return nil
	}
	cond := &Condition{
		HostAllowlist: c.HostAllowlist,
		Actions:       c.Actions,
		ArgAllowlist:  c.ArgAllowlist,
		Expr:          c.Expr,
	}
	if cond.IsZero() {
		return nil
	}
	return cond
}

// --- handlers ---------------------------------------------------------------

// Table — GET /api/workspaces/{id}/tool-policy
// Query params: runtime_id, agent_id, user_id, group_id (repeatable), base.
func (h *Handler) Table(w http.ResponseWriter, r *http.Request) {
	member, workspaceID, ok := h.loadWorkspace(w, r)
	if !ok {
		return
	}

	q := r.URL.Query()
	runtimeID, err := uuidParam(q.Get("runtime_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid runtime_id")
		return
	}
	agentID, err := uuidParam(q.Get("agent_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid agent_id")
		return
	}
	userID, err := uuidParam(q.Get("user_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user_id")
		return
	}
	groupIDs, err := parseUUIDList(q["group_id"])
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid group_id")
		return
	}
	// system_id scopes the System layer to one autopilot (FIR-1609). Absent on the
	// human-context views; present when an admin views an autopilot's System rules.
	systemID, err := uuidParam(q.Get("system_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid system_id")
		return
	}

	base := Setting(q.Get("base"))
	if base != "" && !validSetting(base) {
		writeError(w, http.StatusBadRequest, "invalid base")
		return
	}

	rows, err := h.Store.Table(r.Context(), TableQuery{
		WorkspaceID:        workspaceID,
		RuntimeID:          runtimeID,
		AgentID:            agentID,
		UserID:             userID,
		GroupIDs:           groupIDs,
		SystemID:           systemID,
		Base:               base,
		IncludePlatform:    h.Store.PlatformCapabilitiesEnabled(r.Context(), workspaceID, member.UserID),
		IncludeAgentStart:  h.Store.AgentStartCapabilitiesEnabled(r.Context(), workspaceID, member.UserID),
		IncludeCredentials: h.Store.CredentialAuthoringEnabled(r.Context(), workspaceID, member.UserID),
	})
	if err != nil {
		h.serverError(w, r, "list tool policy table", err)
		return
	}

	// FIR-1609 Phase 5 fold-in: append the firtal_registry per-data-source rows
	// for an agent-scoped query. The data-source list comes from the FDR proxy and
	// the grant from agent_tool_grant — both owned by the handler layer (injected
	// via RegistryRows), so toolpolicy.Table stays pure. Like the web_fetch fold-in
	// it is read-side only (agent_tool_grant stays the enforced source until the
	// gate reads the chain in Phase 6), and an admin's explicit row for a data
	// source always wins, so we never project over an authored rule.
	if h.RegistryRows != nil {
		if proj, ok, perr := h.RegistryRows(r.Context(), workspaceID, agentID); perr != nil {
			slog.Warn("tool-policy registry fold-in skipped", append(logger.RequestAttrs(r), "error", perr)...)
		} else if ok {
			if agentID.Valid {
				// Agent scope: authored per-data-source Permissions rows win first;
				// legacy agent_tool_grant then fills only sources without an authored row.
				q := TableQuery{
					WorkspaceID: workspaceID,
					RuntimeID:   runtimeID,
					AgentID:     agentID,
					UserID:      userID,
					GroupIDs:    groupIDs,
					SystemID:    systemID,
					Base:        base,
				}
				if folded, ferr := h.Store.AppendAuthoredRegistryDataSourceRows(r.Context(), q, proj.DataSources, rows); ferr != nil {
					slog.Warn("tool-policy registry authored fold-in skipped", append(logger.RequestAttrs(r), "error", ferr)...)
				} else {
					rows = folded
				}
				rows = AppendRegistryProjection(rows, proj, LayerAgent, base)
			} else {
				// FIR-2269: every other actor layer (workspace, runtime, group, user,
				// system) has no per-agent grant, so each data source is folded in as
				// an authorable row carrying the AUTHORED chain settings — the same
				// rows the gate enforces in chainGateDataSource. This is what surfaces
				// the "Data sources (N)" picker on the runtime/workspace/group/member
				// permission views, not only the agent's Tools tab.
				q := TableQuery{
					WorkspaceID: workspaceID,
					RuntimeID:   runtimeID,
					UserID:      userID,
					GroupIDs:    groupIDs,
					SystemID:    systemID,
					Base:        base,
				}
				if folded, ferr := h.Store.AppendRegistryDataSourceRows(r.Context(), q, proj.DataSources, rows); ferr != nil {
					slog.Warn("tool-policy registry layer fold-in skipped", append(logger.RequestAttrs(r), "error", ferr)...)
				} else {
					rows = folded
				}
			}
		}
	}

	resp := make([]toolPolicyRow, len(rows))
	for i, row := range rows {
		resp[i] = toRowResponse(row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"tools": resp})
}

// Set — PUT /api/workspaces/{id}/tool-policy
// Body: { tool_key, layer, subject_id, setting }.
func (h *Handler) Set(w http.ResponseWriter, r *http.Request) {
	member, workspaceID, ok := h.loadWorkspace(w, r)
	if !ok {
		return
	}

	var req setRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ToolKey == "" {
		writeError(w, http.StatusBadRequest, "tool_key required")
		return
	}
	if !validLayer(Layer(req.Layer)) {
		writeError(w, http.StatusBadRequest, "invalid layer")
		return
	}
	if !validSetting(Setting(req.Setting)) {
		writeError(w, http.StatusBadRequest, "invalid setting")
		return
	}
	subjectID, err := util.ParseUUID(req.SubjectID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid subject_id")
		return
	}

	// FIR-1609: a System actor is born from a User and may never be authored
	// looser than that owner's own ceiling. Reject a System rule that would loosen
	// access beyond what the owning human holds for the same tool/resource. The
	// resolution-time owner ceiling caps every run anyway; this guard keeps the
	// admin table honest (no "Allow" row that is silently capped to Deny).
	if Layer(req.Layer) == LayerSystem {
		msg, capErr := h.systemCapViolation(r.Context(), workspaceID, subjectID, req.ToolKey, req.ResourcePattern, Setting(req.Setting))
		if capErr != nil {
			h.serverError(w, r, "validate system cap", capErr)
			return
		}
		if msg != "" {
			writeError(w, http.StatusUnprocessableEntity, msg)
			return
		}
	}

	if _, err := h.Store.Set(r.Context(), SetParams{
		WorkspaceID:     workspaceID,
		ToolKey:         req.ToolKey,
		Layer:           Layer(req.Layer),
		SubjectID:       subjectID,
		ResourcePattern: req.ResourcePattern,
		Setting:         Setting(req.Setting),
		Conditions:      req.Condition.toCondition(),
		UpdatedBy:       member.UserID,
	}); err != nil {
		if errors.Is(err, ErrUnknownLayer) || errors.Is(err, ErrUnknownSetting) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		h.serverError(w, r, "set tool policy", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// systemCapViolation checks that a proposed System-layer setting is not looser
// than the owner the System actor is capped on (FIR-1609). It returns a non-empty
// message when the write must be rejected (HTTP 422), an error for an internal
// failure, or ("", nil) when the write is allowed.
//
// The cap is skipped — allowed — when: no SystemOwner resolver is wired; the
// subject is not a known autopilot with a human owner; or the proposed setting is
// Inherit/Deny (Inherit grants nothing; Deny is the tightest and can never exceed
// any owner). Only a concrete Allow/Ask can over-reach, so only those resolve the
// owner's ceiling and compare restrictiveness.
func (h *Handler) systemCapViolation(ctx context.Context, workspaceID, autopilotID pgtype.UUID, toolKey, resourcePattern string, proposed Setting) (string, error) {
	if h.SystemOwner == nil {
		return "", nil
	}
	if !capCanExceed(proposed) {
		return "", nil // Inherit or Deny can never be looser than any owner.
	}
	ownerID, ok, err := h.SystemOwner(ctx, workspaceID, autopilotID)
	if err != nil {
		return "", err
	}
	if !ok || !ownerID.Valid {
		return "", nil // No human owner to cap against — leave it to resolution.
	}
	// The owner's own ceiling for this tool/resource — resolve the owner as a human
	// (their User layer + auto-expanded groups, workspace base). No System actor in
	// this query, so it is purely the owner's reach.
	ownerEff, err := h.Store.Resolve(ctx, Query{
		WorkspaceID:     workspaceID,
		ToolKey:         toolKey,
		ResourcePattern: resourcePattern,
		UserID:          ownerID,
	})
	if err != nil {
		return "", err
	}
	if looserThanOwner(proposed, ownerEff.Setting) {
		return fmt.Sprintf(
			"a System rule may not be more permissive than its owner — owner allows %q here, so System cannot be set to %q",
			ownerEff.Setting, proposed,
		), nil
	}
	return "", nil
}

// capCanExceed reports whether a proposed System setting is even capable of
// exceeding an owner ceiling. Only a concrete Allow/Ask can over-reach; Inherit
// grants nothing and Deny is the tightest, so neither is ever looser than any
// owner — they skip the owner lookup entirely.
func capCanExceed(proposed Setting) bool {
	return rank(proposed) >= 0 && proposed != SettingDeny
}

// looserThanOwner reports whether a proposed System setting is more permissive
// (less restrictive) than the owner's effective ceiling — the condition that must
// be rejected at authoring time (FIR-1609).
func looserThanOwner(proposed, ownerEffective Setting) bool {
	return capCanExceed(proposed) && rank(proposed) < rank(ownerEffective)
}

// Clear — DELETE /api/workspaces/{id}/tool-policy
// Query params: tool_key, layer, subject_id.
func (h *Handler) Clear(w http.ResponseWriter, r *http.Request) {
	_, workspaceID, ok := h.loadWorkspace(w, r)
	if !ok {
		return
	}

	q := r.URL.Query()
	toolKey := q.Get("tool_key")
	if toolKey == "" {
		writeError(w, http.StatusBadRequest, "tool_key required")
		return
	}
	layer := Layer(q.Get("layer"))
	if !validLayer(layer) {
		writeError(w, http.StatusBadRequest, "invalid layer")
		return
	}
	subjectID, err := util.ParseUUID(q.Get("subject_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid subject_id")
		return
	}

	resourcePattern := q.Get("resource_pattern")
	if err := h.Store.Clear(r.Context(), workspaceID, toolKey, layer, subjectID, resourcePattern); err != nil {
		if errors.Is(err, ErrUnknownLayer) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		h.serverError(w, r, "clear tool policy", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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

func (h *Handler) serverError(w http.ResponseWriter, r *http.Request, op string, err error) {
	slog.Error("tool-policy request failed", append(logger.RequestAttrs(r), "op", op, "error", err)...)
	writeError(w, http.StatusInternalServerError, op+" failed")
}

func toRowResponse(row TableRow) toolPolicyRow {
	groups := make([]groupAttributionResponse, len(row.CappedByGroups))
	for i, g := range row.CappedByGroups {
		groups[i] = groupAttributionResponse{Name: g.Name, Owner: g.Owner}
	}
	enforced := EnforcedConditionKinds(row.ToolKey, row.Source, row.ManagedExternally)
	enforcedStrs := make([]string, len(enforced))
	for i, k := range enforced {
		enforcedStrs[i] = string(k)
	}
	return toolPolicyRow{
		ToolKey:            row.ToolKey,
		ResourcePattern:    row.ResourcePattern,
		Title:              row.Title,
		Category:           row.Category,
		Description:        row.Description,
		DescriptionZh:      row.DescriptionZh,
		Source:             row.Source,
		ManagedExternally:  row.ManagedExternally,
		EnforcedConditions: enforcedStrs,
		Layers: layerSettings{
			Workspace: settingPtr(row.Layers, LayerWorkspace),
			Runtime:   settingPtr(row.Layers, LayerRuntime),
			Agent:     settingPtr(row.Layers, LayerAgent),
			Group:     settingPtr(row.Layers, LayerGroup),
			User:      settingPtr(row.Layers, LayerUser),
			System:    settingPtr(row.Layers, LayerSystem),
		},
		Conditions: layerConditions{
			Workspace: conditionPtr(row.Conditions, LayerWorkspace),
			Runtime:   conditionPtr(row.Conditions, LayerRuntime),
			Agent:     conditionPtr(row.Conditions, LayerAgent),
			User:      conditionPtr(row.Conditions, LayerUser),
			System:    conditionPtr(row.Conditions, LayerSystem),
		},
		Effective: effectiveResponse{
			Setting:   string(row.Effective.Setting),
			DecidedBy: string(row.Effective.DecidedBy),
			CappedBy:  string(row.Effective.CappedBy),
			Reason:    row.Effective.Reason,
			Openable:  row.Effective.Openable,
		},
		CappedByGroups: groups,
	}
}

func settingPtr(layers map[Layer]Setting, l Layer) *string {
	if s, ok := layers[l]; ok {
		v := string(s)
		return &v
	}
	return nil
}

// conditionPtr returns the wire form of the condition stored at layer l, or nil
// when that layer carries no condition (a nil map or absent key both yield nil).
func conditionPtr(conditions map[Layer]*Condition, l Layer) *conditionResponse {
	c, ok := conditions[l]
	if !ok || c == nil || c.IsZero() {
		return nil
	}
	return &conditionResponse{
		HostAllowlist: c.HostAllowlist,
		Actions:       c.Actions,
		ArgAllowlist:  c.ArgAllowlist,
		Expr:          c.Expr,
	}
}

func parseUUIDList(raw []string) ([]pgtype.UUID, error) {
	out := make([]pgtype.UUID, 0, len(raw))
	for _, item := range raw {
		if item == "" {
			continue
		}
		id, err := util.ParseUUID(item)
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
