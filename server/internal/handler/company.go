package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// The digital company: a workspace has up to companyMaxFloors named
// floors, each with companySlotsPerFloor squad rooms (slots 0..5). A squad holds
// at most one seat (floor_id, company_slot); an agent is drawn in its home squad.
// All relationships are validated here, never by foreign keys.
const (
	companyMaxFloors     = 16
	companySlotsPerFloor = 6
	companyFloorNameMax  = 64
	// A request can never name more squads than there are seats.
	companyMaxSeatsPerRequest = companyMaxFloors * companySlotsPerFloor
)

// ── Response types ──────────────────────────────────────────────────────────

type CompanyFloorResponse struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	Position    int32  `json:"position"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

func companyFloorToResponse(f db.CompanyFloor) CompanyFloorResponse {
	return CompanyFloorResponse{
		ID:          uuidToString(f.ID),
		WorkspaceID: uuidToString(f.WorkspaceID),
		Name:        f.Name,
		Position:    f.Position,
		CreatedAt:   timestampToString(f.CreatedAt),
		UpdatedAt:   timestampToString(f.UpdatedAt),
	}
}

// CompanySeatsResponse lists the squads named by a seat request, in request
// order, as they stand after it was applied.
type CompanySeatsResponse struct {
	Squads []SquadResponse `json:"squads"`
}

// ── Helpers ─────────────────────────────────────────────────────────────────

// normalizeCompanyFloorName trims the name and checks it against the column
// rule (1 to 64 characters, counted in code points like Postgres char_length).
func normalizeCompanyFloorName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	n := utf8.RuneCountInString(name)
	if n < 1 || n > companyFloorNameMax {
		return "", false
	}
	return name, true
}

// defaultCompanyFloorName is "Floor N" for the next floor, skipping a name an
// existing floor already carries.
func defaultCompanyFloorName(floors []db.CompanyFloor) string {
	taken := make(map[string]struct{}, len(floors))
	for _, f := range floors {
		taken[f.Name] = struct{}{}
	}
	for n := len(floors) + 1; ; n++ {
		name := fmt.Sprintf("Floor %d", n)
		if _, ok := taken[name]; !ok {
			return name
		}
	}
}

// companyAutoSeat is where a new squad lands.
type companyAutoSeat struct {
	Floor db.CompanyFloor
	Slot  int16
	// NewFloor is set when the seat required creating Floor.
	NewFloor bool
}

// pickCompanyAutoSeat returns the lowest free slot on the lowest floor,
// creating a floor when every existing one is full. It returns nil, nil when
// the workspace is at the floor cap with every seat taken: the squad is then
// created unseated. The caller must hold LockCompanyLayout. floorName names the
// floor only when one has to be created.
func pickCompanyAutoSeat(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID, floorName string) (*companyAutoSeat, error) {
	floors, err := q.ListCompanyFloors(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	seats, err := q.ListCompanySeats(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	occupied := make(map[string]map[int16]struct{}, len(floors))
	for _, s := range seats {
		key := uuidToString(s.FloorID)
		if occupied[key] == nil {
			occupied[key] = map[int16]struct{}{}
		}
		occupied[key][s.CompanySlot.Int16] = struct{}{}
	}
	for _, f := range floors {
		taken := occupied[uuidToString(f.ID)]
		for slot := int16(0); slot < companySlotsPerFloor; slot++ {
			if _, used := taken[slot]; !used {
				return &companyAutoSeat{Floor: f, Slot: slot}, nil
			}
		}
	}
	if len(floors) >= companyMaxFloors {
		return nil, nil
	}
	if floorName == "" {
		floorName = defaultCompanyFloorName(floors)
	}
	floor, err := q.CreateCompanyFloor(ctx, db.CreateCompanyFloorParams{WorkspaceID: workspaceID, Name: floorName})
	if err != nil {
		return nil, err
	}
	return &companyAutoSeat{Floor: floor, Slot: 0, NewFloor: true}, nil
}

// claimHomeSquad makes squadID the agent's home when this is its first live
// squad and it has no home yet. It returns the changed agent, or nil when the
// agent keeps what it had.
func claimHomeSquad(ctx context.Context, q *db.Queries, workspaceID, agentID, squadID pgtype.UUID) (*db.Agent, error) {
	agent, err := q.SetAgentHomeSquadOnFirstMembership(ctx, db.SetAgentHomeSquadOnFirstMembershipParams{
		ID: agentID, WorkspaceID: workspaceID, SquadID: squadID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &agent, nil
}

// releaseHomeSquad moves agents off a squad that stopped being a valid home to
// another live squad they still belong to, or clears it. With agentID invalid
// it covers every agent homed there (a squad archive).
func releaseHomeSquad(ctx context.Context, q *db.Queries, workspaceID, squadID, agentID pgtype.UUID) ([]db.Agent, error) {
	return q.ReassignAgentHomeSquad(ctx, db.ReassignAgentHomeSquadParams{
		WorkspaceID: workspaceID, SquadID: squadID, AgentID: agentID,
	})
}

// publishAgentsChanged tells clients that agents changed in a way that did not
// come through UpdateAgent (home squad moves caused by squad membership).
func (h *Handler) publishAgentsChanged(workspaceID, userID string, agents []db.Agent) {
	for _, a := range agents {
		h.publish(protocol.EventAgentStatus, workspaceID, "member", userID, map[string]any{
			"agent": broadcastAgentResponse(h.agentToResponse(a)),
		})
	}
}

// ── Floors ──────────────────────────────────────────────────────────────────

func (h *Handler) ListCompanyFloors(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	floors, err := h.Queries.ListCompanyFloors(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list floors")
		return
	}
	resp := make([]CompanyFloorResponse, len(floors))
	for i, f := range floors {
		resp[i] = companyFloorToResponse(f)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) CreateCompanyFloor(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name, ok := normalizeCompanyFloorName(req.Name)
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("name must be 1 to %d characters", companyFloorNameMax))
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create floor")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	if err := qtx.LockCompanyLayout(r.Context(), wsUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create floor")
		return
	}
	count, err := qtx.CountCompanyFloors(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create floor")
		return
	}
	if count >= companyMaxFloors {
		writeErrorCode(w, http.StatusConflict, "floor_limit", fmt.Sprintf("a workspace can have at most %d floors", companyMaxFloors))
		return
	}
	floor, err := qtx.CreateCompanyFloor(r.Context(), db.CreateCompanyFloorParams{WorkspaceID: wsUUID, Name: name})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create floor")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create floor")
		return
	}

	resp := companyFloorToResponse(floor)
	h.publish(protocol.EventCompanyFloorCreated, workspaceID, "member", requestUserID(r), map[string]any{"floor": resp})
	writeJSON(w, http.StatusCreated, resp)
}

// UpdateCompanyFloor renames a floor and/or moves it to a 0-based index in the
// floor order. The other floors are renumbered 0..n-1 in the same transaction,
// so clients never need to compute positions.
func (h *Handler) UpdateCompanyFloor(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	floorUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "floor id")
	if !ok {
		return
	}
	var req struct {
		Name     *string `json:"name"`
		Position *int    `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == nil && req.Position == nil {
		writeError(w, http.StatusBadRequest, "name or position is required")
		return
	}
	name := ""
	if req.Name != nil {
		name, ok = normalizeCompanyFloorName(*req.Name)
		if !ok {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("name must be 1 to %d characters", companyFloorNameMax))
			return
		}
	}
	if req.Position != nil && *req.Position < 0 {
		writeError(w, http.StatusBadRequest, "position must be 0 or greater")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update floor")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	if err := qtx.LockCompanyLayout(r.Context(), wsUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update floor")
		return
	}
	if _, err := qtx.GetCompanyFloorInWorkspace(r.Context(), db.GetCompanyFloorInWorkspaceParams{ID: floorUUID, WorkspaceID: wsUUID}); err != nil {
		writeError(w, http.StatusNotFound, "floor not found")
		return
	}
	if req.Name != nil {
		if _, err := qtx.RenameCompanyFloor(r.Context(), db.RenameCompanyFloorParams{ID: floorUUID, WorkspaceID: wsUUID, Name: name}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update floor")
			return
		}
	}
	if req.Position != nil {
		floors, err := qtx.ListCompanyFloors(r.Context(), wsUUID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update floor")
			return
		}
		ordered := make([]db.CompanyFloor, 0, len(floors))
		var moving db.CompanyFloor
		for _, f := range floors {
			if f.ID == floorUUID {
				moving = f
				continue
			}
			ordered = append(ordered, f)
		}
		at := *req.Position
		if at > len(ordered) {
			at = len(ordered)
		}
		ordered = append(ordered[:at], append([]db.CompanyFloor{moving}, ordered[at:]...)...)
		for i, f := range ordered {
			if f.Position == int32(i) {
				continue
			}
			if err := qtx.SetCompanyFloorPosition(r.Context(), db.SetCompanyFloorPositionParams{ID: f.ID, WorkspaceID: wsUUID, Position: int32(i)}); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to update floor")
				return
			}
		}
	}
	updated, err := qtx.GetCompanyFloorInWorkspace(r.Context(), db.GetCompanyFloorInWorkspaceParams{ID: floorUUID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update floor")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update floor")
		return
	}

	resp := companyFloorToResponse(updated)
	h.publish(protocol.EventCompanyFloorUpdated, workspaceID, "member", requestUserID(r), map[string]any{"floor": resp})
	writeJSON(w, http.StatusOK, resp)
}

// DeleteCompanyFloor removes an empty floor. A floor that still holds a live
// squad is refused: the caller moves or unseats the squads first (one
// PUT /api/company/seats request), nothing cascades.
func (h *Handler) DeleteCompanyFloor(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	floorUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "floor id")
	if !ok {
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete floor")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	if err := qtx.LockCompanyLayout(r.Context(), wsUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete floor")
		return
	}
	if _, err := qtx.GetCompanyFloorInWorkspace(r.Context(), db.GetCompanyFloorInWorkspaceParams{ID: floorUUID, WorkspaceID: wsUUID}); err != nil {
		writeError(w, http.StatusNotFound, "floor not found")
		return
	}
	occupants, err := qtx.CountLiveSquadsOnFloor(r.Context(), db.CountLiveSquadsOnFloorParams{WorkspaceID: wsUUID, FloorID: floorUUID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete floor")
		return
	}
	if occupants > 0 {
		writeErrorCode(w, http.StatusConflict, "floor_not_empty", "move or unseat the squads on this floor before deleting it")
		return
	}
	if _, err := qtx.DeleteCompanyFloor(r.Context(), db.DeleteCompanyFloorParams{ID: floorUUID, WorkspaceID: wsUUID}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete floor")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete floor")
		return
	}

	h.publish(protocol.EventCompanyFloorDeleted, workspaceID, "member", requestUserID(r), map[string]any{
		"floor_id": uuidToString(floorUUID),
	})
	w.WriteHeader(http.StatusNoContent)
}

// ── Seats ───────────────────────────────────────────────────────────────────

type companySeatRequest struct {
	SquadID string  `json:"squad_id"`
	FloorID *string `json:"floor_id"`
	Slot    *int    `json:"slot"`
}

type companySeatTarget struct {
	squadID pgtype.UUID
	floorID pgtype.UUID // invalid = unseat
	slot    int16
}

// PutCompanySeats applies a list of seat moves atomically. A swap is two
// entries in one request. Each squad ends up exactly where the request says or
// the whole request fails: 409 names the squad holding a contested seat.
//
// Admins may seat any squad; other members only the squads they created (the
// existing squad management rule).
func (h *Handler) PutCompanySeats(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	var req []companySeatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req) == 0 {
		writeError(w, http.StatusBadRequest, "at least one seat is required")
		return
	}
	if len(req) > companyMaxSeatsPerRequest {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("at most %d seats per request", companyMaxSeatsPerRequest))
		return
	}

	targets := make([]companySeatTarget, len(req))
	seenSquads := make(map[string]struct{}, len(req))
	for i, item := range req {
		squadUUID, ok := parseUUIDOrBadRequest(w, item.SquadID, "squad_id")
		if !ok {
			return
		}
		key := uuidToString(squadUUID)
		if _, dup := seenSquads[key]; dup {
			writeError(w, http.StatusBadRequest, "a squad can appear only once")
			return
		}
		seenSquads[key] = struct{}{}
		targets[i].squadID = squadUUID

		if (item.FloorID == nil) != (item.Slot == nil) {
			writeError(w, http.StatusBadRequest, "floor_id and slot must both be set or both be null")
			return
		}
		if item.FloorID == nil {
			continue
		}
		floorUUID, ok := parseUUIDOrBadRequest(w, *item.FloorID, "floor_id")
		if !ok {
			return
		}
		if *item.Slot < 0 || *item.Slot >= companySlotsPerFloor {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("slot must be between 0 and %d", companySlotsPerFloor-1))
			return
		}
		targets[i].floorID = floorUUID
		targets[i].slot = int16(*item.Slot)
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update seats")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	if err := qtx.LockCompanyLayout(r.Context(), wsUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update seats")
		return
	}

	// Load and authorize every squad before writing anything.
	squads := make([]db.Squad, len(targets))
	var changed []int
	floorsChecked := map[string]struct{}{}
	for i, t := range targets {
		squad, err := qtx.GetSquadInWorkspace(r.Context(), db.GetSquadInWorkspaceParams{ID: t.squadID, WorkspaceID: wsUUID})
		if err != nil || squad.ArchivedAt.Valid {
			writeError(w, http.StatusBadRequest, "squad not found or archived: "+uuidToString(t.squadID))
			return
		}
		if !canManageSquad(member, squad) {
			writeError(w, http.StatusForbidden, "insufficient permissions")
			return
		}
		squads[i] = squad
		if t.floorID.Valid {
			key := uuidToString(t.floorID)
			if _, done := floorsChecked[key]; !done {
				if _, err := qtx.GetCompanyFloorInWorkspace(r.Context(), db.GetCompanyFloorInWorkspaceParams{ID: t.floorID, WorkspaceID: wsUUID}); err != nil {
					writeError(w, http.StatusBadRequest, "floor not found: "+key)
					return
				}
				floorsChecked[key] = struct{}{}
			}
		}
		if seatDiffers(squad, t) {
			changed = append(changed, i)
		}
	}

	// Free every seat that is about to change before placing anything, so a
	// swap never trips the unique index halfway through.
	updated := make(map[int]db.Squad, len(changed))
	for _, i := range changed {
		cleared, err := qtx.SetSquadSeat(r.Context(), db.SetSquadSeatParams{ID: targets[i].squadID, WorkspaceID: wsUUID})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update seats")
			return
		}
		updated[i] = cleared
	}
	for _, i := range changed {
		t := targets[i]
		if !t.floorID.Valid {
			continue
		}
		slot := pgtype.Int2{Int16: t.slot, Valid: true}
		holder, err := qtx.GetLiveSquadAtSeat(r.Context(), db.GetLiveSquadAtSeatParams{WorkspaceID: wsUUID, FloorID: t.floorID, CompanySlot: slot})
		if err == nil {
			writeSeatConflict(w, holder, t)
			return
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "failed to update seats")
			return
		}
		squad, err := qtx.SetSquadSeat(r.Context(), db.SetSquadSeatParams{
			ID: t.squadID, WorkspaceID: wsUUID, FloorID: t.floorID, CompanySlot: slot,
		})
		if err != nil {
			if isUniqueViolation(err) {
				writeErrorCode(w, http.StatusConflict, "seat_conflict", "that seat was just taken")
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to update seats")
			return
		}
		updated[i] = squad
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update seats")
		return
	}
	for _, i := range changed {
		h.recomputeSquadKnowledgeAccess(r, wsUUID, targets[i].squadID)
	}

	resp := CompanySeatsResponse{Squads: make([]SquadResponse, 0, len(targets))}
	for i := range targets {
		squad := squads[i]
		if u, ok := updated[i]; ok {
			squad = u
		}
		sr, err := h.squadToResponseWithPreview(r.Context(), squad)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load squad member preview")
			return
		}
		resp.Squads = append(resp.Squads, sr)
		if _, ok := updated[i]; ok {
			h.publish(protocol.EventSquadUpdated, workspaceID, "member", requestUserID(r), map[string]any{"squad": sr})
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// seatDiffers reports whether the target moves the squad.
func seatDiffers(squad db.Squad, t companySeatTarget) bool {
	if !t.floorID.Valid {
		return squad.FloorID.Valid
	}
	return !squad.FloorID.Valid || squad.FloorID != t.floorID || !squad.CompanySlot.Valid || squad.CompanySlot.Int16 != t.slot
}

func writeSeatConflict(w http.ResponseWriter, holder db.Squad, t companySeatTarget) {
	writeJSON(w, http.StatusConflict, map[string]any{
		"error": fmt.Sprintf("seat is already taken by squad %q", holder.Name),
		"code":  "seat_conflict",
		"conflict": map[string]any{
			"floor_id":   uuidToString(t.floorID),
			"slot":       t.slot,
			"squad_id":   uuidToString(holder.ID),
			"squad_name": holder.Name,
		},
	})
}
