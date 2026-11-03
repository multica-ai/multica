package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Initiative API (spec 3.1). Reads are open to every member. Creating is for
// owners and admins, and for the Secretary on request; other agents propose an
// opportunity instead. Advance, attest and artifact waivers are open to the
// owning squad as well. Send-back is open to the owning leader. Shelve, resume,
// close, force and the review days are for owners and admins, and agents cannot
// take them.

// Limits on the text an initiative holds, bounded for the reader.
const (
	initiativeMaxTitle  = 200
	initiativeMaxThesis = 4000
	initiativeMaxNote   = 4000
)

// ── Response types ──────────────────────────────────────────────────────────

// PipelineResponse is one pipeline with its stages.
type PipelineResponse struct {
	ID        string                  `json:"id"`
	Name      string                  `json:"name"`
	IsDefault bool                    `json:"is_default"`
	Stages    []service.PipelineStage `json:"stages"`
}

// InitiativeResponse is an initiative as the board and the panel show it.
// SLAState is "stalled" when the stage has run past its sla_days.
type InitiativeResponse struct {
	ID                    string  `json:"id"`
	Identifier            string  `json:"identifier"`
	WorkspaceID           string  `json:"workspace_id"`
	Title                 string  `json:"title"`
	Thesis                string  `json:"thesis"`
	PipelineID            string  `json:"pipeline_id"`
	StageKey              string  `json:"stage_key"`
	OwnerSquadID          *string `json:"owner_squad_id"`
	Status                string  `json:"status"`
	IssueID               string  `json:"issue_id"`
	BuildProjectID        *string `json:"build_project_id"`
	StageEnteredAt        string  `json:"stage_entered_at"`
	DaysInStage           int     `json:"days_in_stage"`
	SLAState              string  `json:"sla_state"`
	ArtifactsRequired     int     `json:"artifacts_required"`
	ArtifactsPresent      int     `json:"artifacts_present"`
	PendingGateProposalID *string `json:"pending_gate_proposal_id"`
	ClosedAt              *string `json:"closed_at"`
	ClosedReason          string  `json:"closed_reason"`
	CreatedAt             string  `json:"created_at"`
}

// InitiativeArtifactResponse is one checklist row of a stage.
type InitiativeArtifactResponse struct {
	Kind            string  `json:"kind"`
	StageKey        string  `json:"stage_key"`
	Required        bool    `json:"required"`
	KnowledgeLinkID *string `json:"knowledge_link_id"`
	Waived          bool    `json:"waived"`
	WaivedNote      string  `json:"waived_note"`
}

// InitiativeEventResponse is one row of the stage timeline.
type InitiativeEventResponse struct {
	ID        string  `json:"id"`
	Kind      string  `json:"kind"`
	FromStage *string `json:"from_stage"`
	ToStage   *string `json:"to_stage"`
	ActorType string  `json:"actor_type"`
	ActorID   *string `json:"actor_id"`
	Note      string  `json:"note"`
	CreatedAt string  `json:"created_at"`
}

// InitiativeGateResponse summarises a stage_gate proposal of the initiative.
type InitiativeGateResponse struct {
	ID    string `json:"id"`
	State string `json:"state"`
	// GateDay is the operate review's day after launch, null for a stage gate.
	GateDay         *int            `json:"gate_day"`
	Payload         json.RawMessage `json:"payload"`
	IssueIdentifier string          `json:"issue_identifier"`
	Title           string          `json:"title"`
	DecisionNote    string          `json:"decision_note"`
	CreatedAt       string          `json:"created_at"`
	DecidedAt       *string         `json:"decided_at"`
}

// InitiativeDetailResponse is one initiative with its checklist, timeline and gates.
type InitiativeDetailResponse struct {
	Initiative InitiativeResponse           `json:"initiative"`
	Artifacts  []InitiativeArtifactResponse `json:"artifacts"`
	Events     []InitiativeEventResponse    `json:"events"`
	Gates      []InitiativeGateResponse     `json:"gates"`
}

// ── Caller ──────────────────────────────────────────────────────────────────

// initiativeCaller is who acts, and what the rules let them do.
type initiativeCaller struct {
	actor service.InitiativeActor
	// admin is set for a workspace member who is an owner or admin.
	admin bool
}

// initiativeCallerFor resolves the acting member or agent. It writes the error
// and reports false when there is none.
func (h *Handler) initiativeCallerFor(w http.ResponseWriter, r *http.Request, workspaceID string) (initiativeCaller, bool) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return initiativeCaller{}, false
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if actorType == "agent" {
		agentID, ok := parseUUIDOrBadRequest(w, actorID, "actor id")
		if !ok {
			return initiativeCaller{}, false
		}
		return initiativeCaller{actor: service.InitiativeActor{Type: "agent", ID: agentID}}, true
	}
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return initiativeCaller{}, false
	}
	return initiativeCaller{
		actor: service.InitiativeActor{Type: "member", ID: member.UserID},
		admin: roleAllowed(member.Role, "owner", "admin"),
	}, true
}

// isOwningSquadMember reports whether the caller is a member of the squad that
// owns the initiative's current stage (an agent of it, or a member of it).
func (h *Handler) isOwningSquadMember(ctx context.Context, ini db.Initiative, c initiativeCaller) bool {
	if !ini.OwnerSquadID.Valid {
		return false
	}
	isMember, err := h.Queries.IsSquadMember(ctx, db.IsSquadMemberParams{
		SquadID: ini.OwnerSquadID, MemberType: c.actor.Type, MemberID: c.actor.ID,
	})
	return err == nil && isMember
}

// isOwningLeader reports whether the caller is the owning squad's leader agent.
func (h *Handler) isOwningLeader(ctx context.Context, ini db.Initiative, c initiativeCaller) bool {
	if c.actor.Type != "agent" || !ini.OwnerSquadID.Valid {
		return false
	}
	squad, err := h.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{ID: ini.OwnerSquadID, WorkspaceID: ini.WorkspaceID})
	return err == nil && squad.LeaderID == c.actor.ID
}

// ── Reads ───────────────────────────────────────────────────────────────────

// ListPipelines returns the workspace's pipelines, the default first.
func (h *Handler) ListPipelines(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	rows, err := h.Queries.ListPipelines(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list pipelines")
		return
	}
	out := make([]PipelineResponse, 0, len(rows))
	for _, p := range rows {
		resp, err := pipelineToResponse(p)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read pipeline")
			return
		}
		out = append(out, resp)
	}
	writeJSON(w, http.StatusOK, map[string]any{"pipelines": out})
}

func pipelineToResponse(p db.Pipeline) (PipelineResponse, error) {
	var stages []service.PipelineStage
	if err := json.Unmarshal(p.Stages, &stages); err != nil {
		return PipelineResponse{}, err
	}
	if stages == nil {
		stages = []service.PipelineStage{}
	}
	return PipelineResponse{ID: uuidToString(p.ID), Name: p.Name, IsDefault: p.IsDefault, Stages: stages}, nil
}

// initiativeReads caches what the list and the detail read for each initiative
// of one request: its pipeline's stages and its pending gate.
type initiativeReads struct {
	h         *Handler
	prefix    string
	pipelines map[string][]service.PipelineStage
}

func (h *Handler) newInitiativeReads(ctx context.Context, wsUUID pgtype.UUID) *initiativeReads {
	return &initiativeReads{h: h, prefix: h.getIssuePrefix(ctx, wsUUID), pipelines: map[string][]service.PipelineStage{}}
}

func (rd *initiativeReads) stagesOf(ctx context.Context, ini db.Initiative) ([]service.PipelineStage, error) {
	key := uuidToString(ini.PipelineID)
	if stages, ok := rd.pipelines[key]; ok {
		return stages, nil
	}
	p, err := rd.h.Queries.GetPipelineInWorkspace(ctx, db.GetPipelineInWorkspaceParams{ID: ini.PipelineID, WorkspaceID: ini.WorkspaceID})
	if err != nil {
		return nil, err
	}
	var stages []service.PipelineStage
	if err := json.Unmarshal(p.Stages, &stages); err != nil {
		return nil, err
	}
	rd.pipelines[key] = stages
	return stages, nil
}

// view builds the response for one initiative.
func (rd *initiativeReads) view(ctx context.Context, ini db.Initiative, now time.Time) (InitiativeResponse, error) {
	stages, err := rd.stagesOf(ctx, ini)
	if err != nil {
		return InitiativeResponse{}, err
	}
	artifacts, err := rd.h.Queries.ListInitiativeArtifacts(ctx, db.ListInitiativeArtifactsParams{WorkspaceID: ini.WorkspaceID, InitiativeID: ini.ID})
	if err != nil {
		return InitiativeResponse{}, err
	}
	days := int(now.Sub(ini.StageEnteredAt.Time).Hours() / 24)
	resp := InitiativeResponse{
		ID:             uuidToString(ini.ID),
		Identifier:     fmt.Sprintf("INI-%d", ini.Seq),
		WorkspaceID:    uuidToString(ini.WorkspaceID),
		Title:          ini.Title,
		Thesis:         ini.Thesis,
		PipelineID:     uuidToString(ini.PipelineID),
		StageKey:       ini.StageKey,
		OwnerSquadID:   uuidToPtr(ini.OwnerSquadID),
		Status:         ini.Status,
		IssueID:        uuidToString(ini.IssueID),
		BuildProjectID: uuidToPtr(ini.BuildProjectID),
		StageEnteredAt: timestampToString(ini.StageEnteredAt),
		DaysInStage:    days,
		SLAState:       "ok",
		ClosedAt:       timestampToPtr(ini.ClosedAt),
		ClosedReason:   ini.ClosedReason,
		CreatedAt:      timestampToString(ini.CreatedAt),
	}
	for _, st := range stages {
		if st.Key == ini.StageKey && st.SLADays > 0 && days > st.SLADays && ini.Status != service.InitiativeStatusClosed {
			resp.SLAState = "stalled"
		}
	}
	for _, a := range artifacts {
		if a.StageKey == ini.StageKey && a.Required {
			resp.ArtifactsRequired++
			if a.KnowledgeLinkID.Valid || a.WaivedBy.Valid {
				resp.ArtifactsPresent++
			}
		}
	}
	gates, err := rd.h.Queries.ListGateProposalsForInitiative(ctx, db.ListGateProposalsForInitiativeParams{
		WorkspaceID: ini.WorkspaceID, InitiativeID: uuidToString(ini.ID),
	})
	if err != nil {
		return InitiativeResponse{}, err
	}
	for _, g := range gates {
		if g.Proposal.State == service.ProposalStatePending {
			pid := uuidToString(g.Proposal.ID)
			resp.PendingGateProposalID = &pid
		}
	}
	return resp, nil
}

// ListInitiatives returns the workspace's initiatives, newest first. The
// pipeline_id, status and owner_squad_id filters are optional.
func (h *Handler) ListInitiatives(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	params := db.ListInitiativesParams{WorkspaceID: wsUUID}
	q := r.URL.Query()
	if raw := q.Get("pipeline_id"); raw != "" {
		if params.PipelineID, ok = parseUUIDOrBadRequest(w, raw, "pipeline_id"); !ok {
			return
		}
	}
	if raw := q.Get("owner_squad_id"); raw != "" {
		if params.OwnerSquadID, ok = parseUUIDOrBadRequest(w, raw, "owner_squad_id"); !ok {
			return
		}
	}
	if raw := q.Get("status"); raw != "" {
		switch raw {
		case service.InitiativeStatusActive, service.InitiativeStatusShelved,
			service.InitiativeStatusOperating, service.InitiativeStatusClosed:
		default:
			writeError(w, http.StatusBadRequest, "status must be active, shelved, operating or closed")
			return
		}
		params.Status = pgtype.Text{String: raw, Valid: true}
	}
	rows, err := h.Queries.ListInitiatives(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list initiatives")
		return
	}
	rd := h.newInitiativeReads(r.Context(), wsUUID)
	now := time.Now()
	out := make([]InitiativeResponse, 0, len(rows))
	for _, ini := range rows {
		view, err := rd.view(r.Context(), ini, now)
		if err != nil {
			slog.Warn("list initiatives: could not read one", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to read initiative")
			return
		}
		out = append(out, view)
	}
	writeJSON(w, http.StatusOK, map[string]any{"initiatives": out})
}

// GetInitiative returns one initiative with its checklist, timeline and gates.
func (h *Handler) GetInitiative(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "initiative id")
	if !ok {
		return
	}
	ini, err := h.Queries.GetInitiativeInWorkspace(r.Context(), db.GetInitiativeInWorkspaceParams{ID: id, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "initiative not found")
		return
	}
	resp, status, msg := h.initiativeDetail(r.Context(), ini)
	if status != 0 {
		writeError(w, status, msg)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// initiativeDetail builds the detail response for an initiative.
func (h *Handler) initiativeDetail(ctx context.Context, ini db.Initiative) (InitiativeDetailResponse, int, string) {
	rd := h.newInitiativeReads(ctx, ini.WorkspaceID)
	view, err := rd.view(ctx, ini, time.Now())
	if err != nil {
		return InitiativeDetailResponse{}, http.StatusInternalServerError, "failed to read initiative"
	}
	artifacts, err := h.Queries.ListInitiativeArtifacts(ctx, db.ListInitiativeArtifactsParams{WorkspaceID: ini.WorkspaceID, InitiativeID: ini.ID})
	if err != nil {
		return InitiativeDetailResponse{}, http.StatusInternalServerError, "failed to read artifacts"
	}
	events, err := h.Queries.ListInitiativeStageEvents(ctx, db.ListInitiativeStageEventsParams{WorkspaceID: ini.WorkspaceID, InitiativeID: ini.ID})
	if err != nil {
		return InitiativeDetailResponse{}, http.StatusInternalServerError, "failed to read timeline"
	}
	gates, err := h.Queries.ListGateProposalsForInitiative(ctx, db.ListGateProposalsForInitiativeParams{
		WorkspaceID: ini.WorkspaceID, InitiativeID: uuidToString(ini.ID),
	})
	if err != nil {
		return InitiativeDetailResponse{}, http.StatusInternalServerError, "failed to read gates"
	}
	resp := InitiativeDetailResponse{
		Initiative: view,
		Artifacts:  make([]InitiativeArtifactResponse, 0, len(artifacts)),
		Events:     make([]InitiativeEventResponse, 0, len(events)),
		Gates:      make([]InitiativeGateResponse, 0, len(gates)),
	}
	for _, a := range artifacts {
		resp.Artifacts = append(resp.Artifacts, InitiativeArtifactResponse{
			Kind: a.Kind, StageKey: a.StageKey, Required: a.Required,
			KnowledgeLinkID: uuidToPtr(a.KnowledgeLinkID),
			Waived:          a.WaivedBy.Valid, WaivedNote: a.WaivedNote,
		})
	}
	for _, e := range events {
		resp.Events = append(resp.Events, InitiativeEventResponse{
			ID: uuidToString(e.ID), Kind: e.Kind,
			FromStage: textToPtr(e.FromStage), ToStage: textToPtr(e.ToStage),
			ActorType: e.ActorType, ActorID: uuidToPtr(e.ActorID),
			Note: e.Note, CreatedAt: timestampToString(e.CreatedAt),
		})
	}
	for _, g := range gates {
		var review struct {
			GateDay int `json:"gate_day"`
		}
		var gateDay *int
		if json.Unmarshal(g.Proposal.Payload, &review) == nil && review.GateDay > 0 {
			day := review.GateDay
			gateDay = &day
		}
		resp.Gates = append(resp.Gates, InitiativeGateResponse{
			ID: uuidToString(g.Proposal.ID), State: g.Proposal.State, GateDay: gateDay,
			Payload:         rawJSONOrEmpty(g.Proposal.Payload),
			IssueIdentifier: service.IssueIdentifier(h.getIssuePrefix(ctx, ini.WorkspaceID), g.IssueNumber),
			Title:           g.IssueTitle, DecisionNote: g.Proposal.DecisionNote,
			CreatedAt: timestampToString(g.Proposal.CreatedAt), DecidedAt: timestampToPtr(g.Proposal.DecidedAt),
		})
	}
	return resp, 0, ""
}

func rawJSONOrEmpty(raw []byte) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("{}")
	}
	return json.RawMessage(raw)
}

// ── Create ──────────────────────────────────────────────────────────────────

// CreateInitiativeRequest starts an initiative directly. pipeline_id is optional;
// the default pipeline is used when it is absent.
type CreateInitiativeRequest struct {
	PipelineID string `json:"pipeline_id"`
	Title      string `json:"title"`
	Thesis     string `json:"thesis"`
}

// mayStartInitiative: an owner or admin directly, and the Secretary on request.
// No other agent starts one; a squad proposes an opportunity instead.
func (h *Handler) mayStartInitiative(r *http.Request, workspaceID string, c initiativeCaller) bool {
	switch c.actor.Type {
	case "member":
		return c.admin
	case "agent":
		return h.isWorkspaceSecretary(r, workspaceID, uuidToString(c.actor.ID))
	}
	return false
}

// CreateInitiative starts an initiative at its pipeline's first stage. An owner
// or admin may do this directly, and the Secretary on request; a squad proposes
// an opportunity instead.
func (h *Handler) CreateInitiative(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	c, ok := h.initiativeCallerFor(w, r, workspaceID)
	if !ok {
		return
	}
	if !h.mayStartInitiative(r, workspaceID, c) {
		writeError(w, http.StatusForbidden, "only an owner or admin, or the Secretary, may start an initiative directly; squads propose one as an opportunity")
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	var req CreateInitiativeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Title = strings.TrimSpace(sanitizeNullBytes(req.Title))
	req.Thesis = strings.TrimSpace(sanitizeNullBytes(req.Thesis))
	switch {
	case req.Title == "":
		writeError(w, http.StatusBadRequest, "title is required")
		return
	case len([]rune(req.Title)) > initiativeMaxTitle:
		writeError(w, http.StatusBadRequest, fmt.Sprintf("title must be at most %d characters", initiativeMaxTitle))
		return
	case req.Thesis == "":
		writeError(w, http.StatusBadRequest, "thesis is required: say why this is worth doing")
		return
	case len(req.Thesis) > initiativeMaxThesis:
		writeError(w, http.StatusBadRequest, fmt.Sprintf("thesis must be at most %d characters", initiativeMaxThesis))
		return
	}
	var pipelineID pgtype.UUID
	if req.PipelineID != "" {
		if pipelineID, ok = parseUUIDOrBadRequest(w, req.PipelineID, "pipeline_id"); !ok {
			return
		}
	}
	// The sponsor is the boss the initiative answers to. The Secretary acts for
	// the workspace's owner, so that owner sponsors what it starts.
	sponsor := c.actor.ID
	if c.actor.Type == "agent" {
		boss, err := h.Queries.GetOldestWorkspaceOwnerMember(r.Context(), wsUUID)
		if err != nil {
			writeError(w, http.StatusConflict, "the workspace has no owner to sponsor the initiative")
			return
		}
		sponsor = boss.UserID
	}
	ini, err := h.initiativeService().Create(r.Context(), service.CreateInitiativeParams{
		WorkspaceID:   wsUUID,
		PipelineID:    pipelineID,
		Title:         req.Title,
		Thesis:        req.Thesis,
		SponsorUserID: sponsor,
		Actor:         c.actor,
	})
	if writeInitiativeError(w, err) {
		return
	}
	if err != nil {
		slog.Warn("create initiative failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create initiative")
		return
	}
	h.finishInitiativeWrite(w, r, wsUUID, c, ini, http.StatusCreated)
}

// ── Settings ────────────────────────────────────────────────────────────────

// UpdateInitiativeRequest changes an initiative's settings. gate_days sets the
// days after launch at which the operate stage files its review gates.
type UpdateInitiativeRequest struct {
	GateDays *[]int32 `json:"gate_days"`
}

// UpdateInitiative changes an initiative's review days. Only an owner or admin
// may: the owning squad does not set the boss's review schedule.
func (h *Handler) UpdateInitiative(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	c, ok := h.initiativeCallerFor(w, r, workspaceID)
	if !ok {
		return
	}
	if c.actor.Type != "member" || !c.admin {
		writeError(w, http.StatusForbidden, "only an owner or admin may change an initiative's review days")
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	ini, ok := h.loadInitiativeFor(w, r, wsUUID)
	if !ok {
		return
	}
	var req UpdateInitiativeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.GateDays == nil {
		writeError(w, http.StatusBadRequest, "gate_days is required")
		return
	}
	updated, err := h.initiativeService().SetGateDays(r.Context(), service.ActionParams{
		WorkspaceID: wsUUID, InitiativeID: ini.ID, Actor: c.actor,
	}, *req.GateDays)
	if writeInitiativeError(w, err) {
		return
	}
	if err != nil {
		slog.Warn("update initiative failed", append(logger.RequestAttrs(r), "error", err, "initiative_id", uuidToString(ini.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to change the initiative")
		return
	}
	h.finishInitiativeWrite(w, r, wsUUID, c, updated, http.StatusOK)
}

// ── Transitions ─────────────────────────────────────────────────────────────

// InitiativeNoteRequest carries a note or a reason. Which fields a transition
// needs is checked by the service.
type InitiativeNoteRequest struct {
	Note   string `json:"note"`
	Reason string `json:"reason"`
}

// ForceInitiativeRequest moves an initiative to any stage, with a reason.
type ForceInitiativeRequest struct {
	ToStage string `json:"to_stage"`
	Reason  string `json:"reason"`
}

// transitionRule says who may take a transition.
type transitionRule func(ctx context.Context, h *Handler, ini db.Initiative, c initiativeCaller) bool

var (
	ruleAdminOnly transitionRule = func(_ context.Context, _ *Handler, _ db.Initiative, c initiativeCaller) bool {
		return c.actor.Type == "member" && c.admin
	}
	ruleOwningSquad transitionRule = func(ctx context.Context, h *Handler, ini db.Initiative, c initiativeCaller) bool {
		return c.admin || h.isOwningSquadMember(ctx, ini, c)
	}
	ruleOwningLeader transitionRule = func(ctx context.Context, h *Handler, ini db.Initiative, c initiativeCaller) bool {
		return c.admin || h.isOwningLeader(ctx, ini, c)
	}
)

// AdvanceInitiative moves the initiative past its current stage when the exit
// allows it.
func (h *Handler) AdvanceInitiative(w http.ResponseWriter, r *http.Request) {
	h.transitionInitiative(w, r, ruleOwningSquad, bodyNote, func(ctx context.Context, p service.ActionParams) (db.Initiative, error) {
		return h.initiativeService().Advance(ctx, p)
	})
}

// AttestInitiative records the owning squad's attestation of the current stage.
func (h *Handler) AttestInitiative(w http.ResponseWriter, r *http.Request) {
	h.transitionInitiative(w, r, ruleOwningSquad, bodyNote, func(ctx context.Context, p service.ActionParams) (db.Initiative, error) {
		return h.initiativeService().Attest(ctx, p)
	})
}

// SendBackInitiative returns the initiative to the previous stage.
func (h *Handler) SendBackInitiative(w http.ResponseWriter, r *http.Request) {
	h.transitionInitiative(w, r, ruleOwningLeader, bodyNote, func(ctx context.Context, p service.ActionParams) (db.Initiative, error) {
		return h.initiativeService().SendBack(ctx, p)
	})
}

// ShelveInitiative pauses the initiative.
func (h *Handler) ShelveInitiative(w http.ResponseWriter, r *http.Request) {
	h.transitionInitiative(w, r, ruleAdminOnly, bodyNote, func(ctx context.Context, p service.ActionParams) (db.Initiative, error) {
		return h.initiativeService().Shelve(ctx, p)
	})
}

// ResumeInitiative brings a shelved initiative back.
func (h *Handler) ResumeInitiative(w http.ResponseWriter, r *http.Request) {
	h.transitionInitiative(w, r, ruleAdminOnly, bodyNote, func(ctx context.Context, p service.ActionParams) (db.Initiative, error) {
		return h.initiativeService().Resume(ctx, p)
	})
}

// CloseInitiative ends the initiative with a reason.
func (h *Handler) CloseInitiative(w http.ResponseWriter, r *http.Request) {
	h.transitionInitiative(w, r, ruleAdminOnly, bodyReason, func(ctx context.Context, p service.ActionParams) (db.Initiative, error) {
		return h.initiativeService().Close(ctx, p)
	})
}

// ForceInitiative moves the initiative to any stage, with a reason.
func (h *Handler) ForceInitiative(w http.ResponseWriter, r *http.Request) {
	h.transitionInitiative(w, r, ruleAdminOnly, bodyForce, func(ctx context.Context, p service.ActionParams) (db.Initiative, error) {
		return h.initiativeService().Force(ctx, p)
	})
}

// WaiveInitiativeArtifact accepts a required artifact of the current stage
// without its document.
func (h *Handler) WaiveInitiativeArtifact(w http.ResponseWriter, r *http.Request) {
	kind := strings.TrimSpace(chi.URLParam(r, "kind"))
	if kind == "" || len(kind) > 64 {
		writeError(w, http.StatusBadRequest, "artifact kind is required")
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	c, ok := h.initiativeCallerFor(w, r, workspaceID)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	ini, ok := h.loadInitiativeFor(w, r, wsUUID)
	if !ok {
		return
	}
	if !ruleOwningSquad(r.Context(), h, ini, c) {
		writeError(w, http.StatusForbidden, "only an owner or admin, or the owning squad, may waive an artifact")
		return
	}
	var req InitiativeNoteRequest
	if !decodeInitiativeBody(w, r, &req) {
		return
	}
	if _, err := h.initiativeService().WaiveArtifact(r.Context(), service.ActionParams{
		WorkspaceID: wsUUID, InitiativeID: ini.ID, Actor: c.actor,
		Note: cleanInitiativeText(req.Note), ArtifactKind: kind,
	}); writeInitiativeError(w, err) {
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to waive artifact")
		return
	}
	h.respondInitiative(w, r, wsUUID, ini.ID, c)
}

// transitionBody says which fields a transition's body carries.
type transitionBody int

const (
	bodyNote   transitionBody = iota // {note}
	bodyReason                       // {reason}, as close takes
	bodyForce                        // {to_stage, reason}
)

// transitionInitiative is the shared shape of the transitions: load, check the
// rule, read the body, run the service, publish.
func (h *Handler) transitionInitiative(w http.ResponseWriter, r *http.Request, rule transitionRule, body transitionBody, run func(context.Context, service.ActionParams) (db.Initiative, error)) {
	workspaceID := h.resolveWorkspaceID(r)
	c, ok := h.initiativeCallerFor(w, r, workspaceID)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	ini, ok := h.loadInitiativeFor(w, r, wsUUID)
	if !ok {
		return
	}
	if !rule(r.Context(), h, ini, c) {
		writeError(w, http.StatusForbidden, "you may not make this change to the initiative")
		return
	}
	var p service.ActionParams
	p.WorkspaceID, p.InitiativeID, p.Actor = wsUUID, ini.ID, c.actor
	switch body {
	case bodyForce:
		var req ForceInitiativeRequest
		if !decodeInitiativeBody(w, r, &req) {
			return
		}
		p.ToStage = strings.TrimSpace(req.ToStage)
		p.Note = cleanInitiativeText(req.Reason)
		if p.ToStage == "" {
			writeError(w, http.StatusBadRequest, "to_stage is required")
			return
		}
	case bodyReason:
		var req InitiativeNoteRequest
		if !decodeInitiativeBody(w, r, &req) {
			return
		}
		p.Note = cleanInitiativeText(req.Reason)
	default:
		var req InitiativeNoteRequest
		if !decodeInitiativeBody(w, r, &req) {
			return
		}
		p.Note = cleanInitiativeText(req.Note)
	}
	updated, err := run(r.Context(), p)
	if writeInitiativeError(w, err) {
		return
	}
	if err != nil {
		slog.Warn("initiative transition failed", append(logger.RequestAttrs(r), "error", err, "initiative_id", uuidToString(ini.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to change the initiative")
		return
	}
	h.finishInitiativeWrite(w, r, wsUUID, c, updated, http.StatusOK)
}

// loadInitiativeFor loads the initiative named in the URL, in this workspace.
func (h *Handler) loadInitiativeFor(w http.ResponseWriter, r *http.Request, wsUUID pgtype.UUID) (db.Initiative, bool) {
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "initiative id")
	if !ok {
		return db.Initiative{}, false
	}
	ini, err := h.Queries.GetInitiativeInWorkspace(r.Context(), db.GetInitiativeInWorkspaceParams{ID: id, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "initiative not found")
		return db.Initiative{}, false
	}
	return ini, true
}

// decodeInitiativeBody reads a JSON body. An empty body is an empty request.
func decodeInitiativeBody(w http.ResponseWriter, r *http.Request, into any) bool {
	if r.Body == nil || r.ContentLength == 0 {
		return true
	}
	if err := json.NewDecoder(r.Body).Decode(into); err != nil && !errors.Is(err, errEmptyBody) {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

var errEmptyBody = errors.New("empty body")

// cleanInitiativeText trims a note or reason and strips the bytes PostgreSQL rejects.
func cleanInitiativeText(s string) string {
	s = strings.TrimSpace(sanitizeNullBytes(s))
	if len(s) > initiativeMaxNote {
		s = s[:initiativeMaxNote]
	}
	return s
}

// finishInitiativeWrite publishes the change and writes the initiative back.
func (h *Handler) finishInitiativeWrite(w http.ResponseWriter, r *http.Request, wsUUID pgtype.UUID, c initiativeCaller, ini db.Initiative, status int) {
	rd := h.newInitiativeReads(r.Context(), wsUUID)
	view, err := rd.view(r.Context(), ini, time.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read initiative")
		return
	}
	h.publish(protocol.EventInitiativeUpdated, uuidToString(wsUUID), c.actor.Type, uuidToString(c.actor.ID),
		map[string]any{"initiative": view})
	writeJSON(w, status, view)
}

// respondInitiative writes the detail after a change that returns no initiative.
func (h *Handler) respondInitiative(w http.ResponseWriter, r *http.Request, wsUUID, id pgtype.UUID, c initiativeCaller) {
	ini, err := h.Queries.GetInitiativeInWorkspace(r.Context(), db.GetInitiativeInWorkspaceParams{ID: id, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read initiative")
		return
	}
	resp, status, msg := h.initiativeDetail(r.Context(), ini)
	if status != 0 {
		writeError(w, status, msg)
		return
	}
	h.publish(protocol.EventInitiativeUpdated, uuidToString(wsUUID), c.actor.Type, uuidToString(c.actor.ID),
		map[string]any{"initiative": resp.Initiative})
	writeJSON(w, http.StatusOK, resp)
}

// writeInitiativeError answers a service refusal. It reports false when err is
// not one the API names, so the caller can answer 500.
func writeInitiativeError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	var notReady *service.StageNotReadyError
	switch {
	case errors.As(err, &notReady):
		writeJSON(w, http.StatusConflict, map[string]any{
			"error": notReady.Error(), "code": "stage_not_ready", "missing": notReady.Missing,
		})
	case errors.Is(err, service.ErrInitiativeNotFound), errors.Is(err, service.ErrArtifactNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, service.ErrStageNeedsAttest):
		writeErrorCode(w, http.StatusConflict, "needs_attest", err.Error())
	case errors.Is(err, service.ErrStageIsGate):
		writeErrorCode(w, http.StatusConflict, "stage_is_gate", err.Error())
	case errors.Is(err, service.ErrInitiativeClosed):
		writeErrorCode(w, http.StatusConflict, "initiative_closed", err.Error())
	case errors.Is(err, service.ErrInitiativeShelved):
		writeErrorCode(w, http.StatusConflict, "initiative_shelved", err.Error())
	case errors.Is(err, service.ErrInitiativeNotShelved):
		writeErrorCode(w, http.StatusConflict, "initiative_not_shelved", err.Error())
	case errors.Is(err, service.ErrNoPreviousStage):
		writeErrorCode(w, http.StatusConflict, "no_previous_stage", err.Error())
	case errors.Is(err, service.ErrNotAttestStage):
		writeErrorCode(w, http.StatusConflict, "not_attest_stage", err.Error())
	case errors.Is(err, service.ErrArtifactNotRequired):
		writeErrorCode(w, http.StatusConflict, "artifact_not_required", err.Error())
	case errors.Is(err, service.ErrGateStale):
		writeErrorCode(w, http.StatusConflict, "gate_stale", err.Error())
	case errors.Is(err, service.ErrBuildNeedsSDLCSquad):
		writeErrorCode(w, http.StatusConflict, "build_squad_not_sdlc", err.Error())
	case errors.Is(err, service.ErrStageOwnerRequired):
		writeErrorCode(w, http.StatusConflict, "stage_unowned", err.Error())
	case errors.Is(err, service.ErrNoDefaultPipeline):
		writeErrorCode(w, http.StatusConflict, "no_pipeline", err.Error())
	case errors.Is(err, service.ErrUnknownStage):
		writeErrorCode(w, http.StatusBadRequest, "unknown_stage", err.Error())
	case errors.Is(err, service.ErrInitiativeNoteRequired):
		writeErrorCode(w, http.StatusBadRequest, "note_required", err.Error())
	case errors.Is(err, service.ErrOutcomeRequired), errors.Is(err, service.ErrOutcomeInvalid), errors.Is(err, service.ErrOutcomeNotAllowed):
		writeErrorCode(w, http.StatusBadRequest, "invalid_outcome", err.Error())
	case errors.Is(err, service.ErrGateDaysInvalid):
		writeErrorCode(w, http.StatusBadRequest, "invalid_gate_days", err.Error())
	default:
		return false
	}
	return true
}

// initiativeService builds the initiative service for a request.
func (h *Handler) initiativeService() *service.InitiativeService {
	ini, _ := service.NewInitiativeSet(h.IssueService, nil)
	return ini
}

// RunInitiativeSweep is the work of the initiative job: it advances initiatives
// whose stage is ready, files their stage gates, and files the review gates whose
// day has come. It returns how many things it did.
func (h *Handler) RunInitiativeSweep(ctx context.Context) (int, error) {
	svc := h.initiativeService()
	res, err := svc.SweepStages(ctx, 500)
	filed, err2 := svc.ScheduleOperate(ctx, 500)
	return res.Advanced + res.GatesFiled + filed, errors.Join(err, err2)
}
