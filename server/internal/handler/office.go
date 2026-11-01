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

// The digital company office: a workspace has up to officeMaxFloors named
// floors, each with officeSlotsPerFloor squad rooms (slots 0..5). A squad holds
// at most one seat (floor_id, office_slot); an agent is drawn in its home squad.
// All relationships are validated here, never by foreign keys.
const (
	officeMaxFloors     = 16
	officeSlotsPerFloor = 6
	officeFloorNameMax  = 64
	// A request can never name more squads than there are seats.
	officeMaxSeatsPerRequest = officeMaxFloors * officeSlotsPerFloor
)

// ── Response types ──────────────────────────────────────────────────────────

type OfficeFloorResponse struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	Position    int32  `json:"position"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

func officeFloorToResponse(f db.OfficeFloor) OfficeFloorResponse {
	return OfficeFloorResponse{
		ID:          uuidToString(f.ID),
		WorkspaceID: uuidToString(f.WorkspaceID),
		Name:        f.Name,
		Position:    f.Position,
		CreatedAt:   timestampToString(f.CreatedAt),
		UpdatedAt:   timestampToString(f.UpdatedAt),
	}
}

// OfficeSeatsResponse lists the squads named by a seat request, in request
// order, as they stand after it was applied.
type OfficeSeatsResponse struct {
	Squads []SquadResponse `json:"squads"`
}

// ── Helpers ─────────────────────────────────────────────────────────────────

// normalizeOfficeFloorName trims the name and checks it against the column
// rule (1 to 64 characters, counted in code points like Postgres char_length).
func normalizeOfficeFloorName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	n := utf8.RuneCountInString(name)
	if n < 1 || n > officeFloorNameMax {
		return "", false
	}
	return name, true
}

// defaultOfficeFloorName is "Floor N" for the next floor, skipping a name an
// existing floor already carries.
func defaultOfficeFloorName(floors []db.OfficeFloor) string {
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

// officeAutoSeat is where a new squad lands.
type officeAutoSeat struct {
	Floor db.OfficeFloor
	Slot  int16
	// NewFloor is set when the seat required creating Floor.
	NewFloor bool
}

// pickOfficeAutoSeat returns the lowest free slot on the lowest floor,
// creating a floor when every existing one is full. It returns nil, nil when
// the workspace is at the floor cap with every seat taken: the squad is then
// created unseated. The caller must hold LockOfficeLayout. floorName names the
// floor only when one has to be created.
func pickOfficeAutoSeat(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID, floorName string) (*officeAutoSeat, error) {
	floors, err := q.ListOfficeFloors(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	seats, err := q.ListOfficeSeats(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	occupied := make(map[string]map[int16]struct{}, len(floors))
	for _, s := range seats {
		key := uuidToString(s.FloorID)
		if occupied[key] == nil {
			occupied[key] = map[int16]struct{}{}
		}
		occupied[key][s.OfficeSlot.Int16] = struct{}{}
	}
	for _, f := range floors {
		taken := occupied[uuidToString(f.ID)]
		for slot := int16(0); slot < officeSlotsPerFloor; slot++ {
			if _, used := taken[slot]; !used {
				return &officeAutoSeat{Floor: f, Slot: slot}, nil
			}
		}
	}
	if len(floors) >= officeMaxFloors {
		return nil, nil
	}
	if floorName == "" {
		floorName = defaultOfficeFloorName(floors)
	}
	floor, err := q.CreateOfficeFloor(ctx, db.CreateOfficeFloorParams{WorkspaceID: workspaceID, Name: floorName})
	if err != nil {
		return nil, err
	}
	return &officeAutoSeat{Floor: floor, Slot: 0, NewFloor: true}, nil
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

func (h *Handler) ListOfficeFloors(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	floors, err := h.Queries.ListOfficeFloors(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list floors")
		return
	}
	resp := make([]OfficeFloorResponse, len(floors))
	for i, f := range floors {
		resp[i] = officeFloorToResponse(f)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) CreateOfficeFloor(w http.ResponseWriter, r *http.Request) {
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
	name, ok := normalizeOfficeFloorName(req.Name)
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("name must be 1 to %d characters", officeFloorNameMax))
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create floor")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	if err := qtx.LockOfficeLayout(r.Context(), wsUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create floor")
		return
	}
	count, err := qtx.CountOfficeFloors(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create floor")
		return
	}
	if count >= officeMaxFloors {
		writeErrorCode(w, http.StatusConflict, "floor_limit", fmt.Sprintf("a workspace can have at most %d floors", officeMaxFloors))
		return
	}
	floor, err := qtx.CreateOfficeFloor(r.Context(), db.CreateOfficeFloorParams{WorkspaceID: wsUUID, Name: name})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create floor")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create floor")
		return
	}

	resp := officeFloorToResponse(floor)
	h.publish(protocol.EventOfficeFloorCreated, workspaceID, "member", requestUserID(r), map[string]any{"floor": resp})
	writeJSON(w, http.StatusCreated, resp)
}

// UpdateOfficeFloor renames a floor and/or moves it to a 0-based index in the
// floor order. The other floors are renumbered 0..n-1 in the same transaction,
// so clients never need to compute positions.
func (h *Handler) UpdateOfficeFloor(w http.ResponseWriter, r *http.Request) {
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
		name, ok = normalizeOfficeFloorName(*req.Name)
		if !ok {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("name must be 1 to %d characters", officeFloorNameMax))
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
	if err := qtx.LockOfficeLayout(r.Context(), wsUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update floor")
		return
	}
	if _, err := qtx.GetOfficeFloorInWorkspace(r.Context(), db.GetOfficeFloorInWorkspaceParams{ID: floorUUID, WorkspaceID: wsUUID}); err != nil {
		writeError(w, http.StatusNotFound, "floor not found")
		return
	}
	if req.Name != nil {
		if _, err := qtx.RenameOfficeFloor(r.Context(), db.RenameOfficeFloorParams{ID: floorUUID, WorkspaceID: wsUUID, Name: name}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update floor")
			return
		}
	}
	if req.Position != nil {
		floors, err := qtx.ListOfficeFloors(r.Context(), wsUUID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update floor")
			return
		}
		ordered := make([]db.OfficeFloor, 0, len(floors))
		var moving db.OfficeFloor
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
		ordered = append(ordered[:at], append([]db.OfficeFloor{moving}, ordered[at:]...)...)
		for i, f := range ordered {
			if f.Position == int32(i) {
				continue
			}
			if err := qtx.SetOfficeFloorPosition(r.Context(), db.SetOfficeFloorPositionParams{ID: f.ID, WorkspaceID: wsUUID, Position: int32(i)}); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to update floor")
				return
			}
		}
	}
	updated, err := qtx.GetOfficeFloorInWorkspace(r.Context(), db.GetOfficeFloorInWorkspaceParams{ID: floorUUID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update floor")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update floor")
		return
	}

	resp := officeFloorToResponse(updated)
	h.publish(protocol.EventOfficeFloorUpdated, workspaceID, "member", requestUserID(r), map[string]any{"floor": resp})
	writeJSON(w, http.StatusOK, resp)
}

// DeleteOfficeFloor removes an empty floor. A floor that still holds a live
// squad is refused: the caller moves or unseats the squads first (one
// PUT /api/office/seats request), nothing cascades.
func (h *Handler) DeleteOfficeFloor(w http.ResponseWriter, r *http.Request) {
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
	if err := qtx.LockOfficeLayout(r.Context(), wsUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete floor")
		return
	}
	if _, err := qtx.GetOfficeFloorInWorkspace(r.Context(), db.GetOfficeFloorInWorkspaceParams{ID: floorUUID, WorkspaceID: wsUUID}); err != nil {
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
	if _, err := qtx.DeleteOfficeFloor(r.Context(), db.DeleteOfficeFloorParams{ID: floorUUID, WorkspaceID: wsUUID}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete floor")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete floor")
		return
	}

	h.publish(protocol.EventOfficeFloorDeleted, workspaceID, "member", requestUserID(r), map[string]any{
		"floor_id": uuidToString(floorUUID),
	})
	w.WriteHeader(http.StatusNoContent)
}

// ── Seats ───────────────────────────────────────────────────────────────────

type officeSeatRequest struct {
	SquadID string  `json:"squad_id"`
	FloorID *string `json:"floor_id"`
	Slot    *int    `json:"slot"`
}

type officeSeatTarget struct {
	squadID pgtype.UUID
	floorID pgtype.UUID // invalid = unseat
	slot    int16
}

// PutOfficeSeats applies a list of seat moves atomically. A swap is two
// entries in one request. Each squad ends up exactly where the request says or
// the whole request fails: 409 names the squad holding a contested seat.
//
// Admins may seat any squad; other members only the squads they created (the
// existing squad management rule).
func (h *Handler) PutOfficeSeats(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	var req []officeSeatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req) == 0 {
		writeError(w, http.StatusBadRequest, "at least one seat is required")
		return
	}
	if len(req) > officeMaxSeatsPerRequest {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("at most %d seats per request", officeMaxSeatsPerRequest))
		return
	}

	targets := make([]officeSeatTarget, len(req))
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
		if *item.Slot < 0 || *item.Slot >= officeSlotsPerFloor {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("slot must be between 0 and %d", officeSlotsPerFloor-1))
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
	if err := qtx.LockOfficeLayout(r.Context(), wsUUID); err != nil {
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
				if _, err := qtx.GetOfficeFloorInWorkspace(r.Context(), db.GetOfficeFloorInWorkspaceParams{ID: t.floorID, WorkspaceID: wsUUID}); err != nil {
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
		holder, err := qtx.GetLiveSquadAtSeat(r.Context(), db.GetLiveSquadAtSeatParams{WorkspaceID: wsUUID, FloorID: t.floorID, OfficeSlot: slot})
		if err == nil {
			writeSeatConflict(w, holder, t)
			return
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "failed to update seats")
			return
		}
		squad, err := qtx.SetSquadSeat(r.Context(), db.SetSquadSeatParams{
			ID: t.squadID, WorkspaceID: wsUUID, FloorID: t.floorID, OfficeSlot: slot,
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

	resp := OfficeSeatsResponse{Squads: make([]SquadResponse, 0, len(targets))}
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
func seatDiffers(squad db.Squad, t officeSeatTarget) bool {
	if !t.floorID.Valid {
		return squad.FloorID.Valid
	}
	return !squad.FloorID.Valid || squad.FloorID != t.floorID || !squad.OfficeSlot.Valid || squad.OfficeSlot.Int16 != t.slot
}

func writeSeatConflict(w http.ResponseWriter, holder db.Squad, t officeSeatTarget) {
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
