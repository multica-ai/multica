package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/redact"
)

// Provider quota pools are user-owned account records, shared across all the
// workspaces in which that user can manage the mapped agents. No credentials
// or raw provider errors are accepted by this API.
func (h *Handler) ListProviderQuotaPools(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.ListProviderQuotaPoolsByOwner(r.Context(), util.MustParseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list provider quota pools")
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

func (h *Handler) CreateProviderQuotaPool(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var request struct {
		Name         string `json:"name"`
		ProviderHint string `json:"provider_hint"`
		Timezone     string `json:"timezone"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid provider quota pool request")
		return
	}
	request.Name = strings.TrimSpace(request.Name)
	request.ProviderHint = strings.TrimSpace(request.ProviderHint)
	request.Timezone = strings.TrimSpace(request.Timezone)
	if request.Timezone == "" {
		request.Timezone = "UTC"
	}
	if request.Name == "" || len(request.Name) > 100 || len(request.ProviderHint) > 100 || len(request.Timezone) > 100 {
		writeError(w, http.StatusBadRequest, "invalid provider quota pool fields")
		return
	}
	if _, err := time.LoadLocation(request.Timezone); err != nil {
		writeError(w, http.StatusBadRequest, "invalid timezone")
		return
	}
	created, err := h.Queries.CreateProviderQuotaPool(r.Context(), db.CreateProviderQuotaPoolParams{
		ID: dbid.NewV7(), OwnerID: util.MustParseUUID(userID),
		Name: request.Name, ProviderHint: request.ProviderHint, Timezone: request.Timezone,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeError(w, http.StatusConflict, "provider quota pool name already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create provider quota pool")
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (h *Handler) loadOwnedProviderQuotaPool(w http.ResponseWriter, r *http.Request) (db.ProviderQuotaPool, string, bool) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return db.ProviderQuotaPool{}, "", false
	}
	poolID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "poolId"), "pool_id")
	if !ok {
		return db.ProviderQuotaPool{}, "", false
	}
	pool, err := h.Queries.GetProviderQuotaPool(r.Context(), poolID)
	if err != nil || uuidToString(pool.OwnerID) != userID {
		writeError(w, http.StatusNotFound, "provider quota pool not found")
		return db.ProviderQuotaPool{}, "", false
	}
	return pool, userID, true
}

func (h *Handler) GetProviderQuotaPool(w http.ResponseWriter, r *http.Request) {
	pool, userID, ok := h.loadOwnedProviderQuotaPool(w, r)
	if !ok {
		return
	}
	if !h.canManageProviderQuotaPoolAgents(r.Context(), userID, pool.ID) {
		writeError(w, http.StatusForbidden, "insufficient permissions for a mapped agent")
		return
	}
	agents, err := h.Queries.ListProviderQuotaPoolAgents(r.Context(), pool.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list provider quota pool agents")
		return
	}
	events, err := h.Queries.ListProviderQuotaPoolEvents(r.Context(), db.ListProviderQuotaPoolEventsParams{PoolID: pool.ID, Limit: 100})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list provider quota pool events")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"pool": pool, "agent_ids": agents, "events": events})
}

func (h *Handler) canManageProviderQuotaPoolAgent(ctx context.Context, userID string, agent db.Agent) bool {
	member, err := h.getWorkspaceMember(ctx, userID, uuidToString(agent.WorkspaceID))
	return err == nil && (uuidToString(agent.OwnerID) == userID || roleAllowed(member.Role, "owner", "admin"))
}

func (h *Handler) canManageProviderQuotaPoolAgents(ctx context.Context, userID string, poolID pgtype.UUID) bool {
	ids, err := h.Queries.ListProviderQuotaPoolAgents(ctx, poolID)
	if err != nil {
		return false
	}
	for _, id := range ids {
		agent, err := h.Queries.GetAgent(ctx, id)
		if err != nil || !h.canManageProviderQuotaPoolAgent(ctx, userID, agent) {
			return false
		}
	}
	return true
}

func (h *Handler) AssignAgentToProviderQuotaPool(w http.ResponseWriter, r *http.Request) {
	pool, userID, ok := h.loadOwnedProviderQuotaPool(w, r)
	if !ok {
		return
	}
	agentID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "agentId"), "agent_id")
	if !ok {
		return
	}
	// Lock the agent before the pool, matching the task claim lock order.
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start provider quota pool update")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	agent, err := qtx.GetAgentForUpdate(r.Context(), agentID)
	if err != nil || agent.Kind != "user" || !h.canManageProviderQuotaPoolAgent(r.Context(), userID, agent) {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	lockedPool, err := qtx.GetProviderQuotaPoolForUpdate(r.Context(), pool.ID)
	if err != nil || uuidToString(lockedPool.OwnerID) != userID {
		writeError(w, http.StatusNotFound, "provider quota pool not found")
		return
	}
	if !h.canManageProviderQuotaPoolAgents(r.Context(), userID, pool.ID) {
		writeError(w, http.StatusForbidden, "insufficient permissions for a mapped agent")
		return
	}
	existing, err := qtx.GetProviderQuotaPoolForAgent(r.Context(), agentID)
	if err == nil {
		if existing.ID != pool.ID {
			writeError(w, http.StatusConflict, "agent belongs to another provider quota pool")
			return
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to check provider quota pool membership")
		return
	} else {
		if err := qtx.AssignAgentProviderQuotaPool(r.Context(), db.AssignAgentProviderQuotaPoolParams{AgentID: agentID, PoolID: pool.ID}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to assign provider quota pool")
			return
		}
		if err := qtx.CreateProviderQuotaPoolEvent(r.Context(), db.CreateProviderQuotaPoolEventParams{
			ID: dbid.NewV7(), PoolID: pool.ID, ActorID: util.MustParseUUID(userID),
			EventType: "membership_changed", Reason: "agent assigned", OldState: lockedPool.State, NewState: lockedPool.State,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to audit provider quota pool assignment")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit provider quota pool assignment")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"pool_id": pool.ID, "agent_id": agentID})
}

func (h *Handler) RemoveAgentFromProviderQuotaPool(w http.ResponseWriter, r *http.Request) {
	pool, userID, ok := h.loadOwnedProviderQuotaPool(w, r)
	if !ok {
		return
	}
	agentID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "agentId"), "agent_id")
	if !ok {
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start provider quota pool update")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	agent, err := qtx.GetAgentForUpdate(r.Context(), agentID)
	if err != nil || !h.canManageProviderQuotaPoolAgent(r.Context(), userID, agent) {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	lockedPool, err := qtx.GetProviderQuotaPoolForAgentForUpdate(r.Context(), agentID)
	if err != nil || lockedPool.ID != pool.ID {
		writeError(w, http.StatusNotFound, "provider quota pool membership not found")
		return
	}
	if !h.canManageProviderQuotaPoolAgents(r.Context(), userID, pool.ID) {
		writeError(w, http.StatusForbidden, "insufficient permissions for a mapped agent")
		return
	}
	if err := qtx.RemoveAgentProviderQuotaPool(r.Context(), agentID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove provider quota pool membership")
		return
	}
	if err := qtx.CreateProviderQuotaPoolEvent(r.Context(), db.CreateProviderQuotaPoolEventParams{
		ID: dbid.NewV7(), PoolID: pool.ID, ActorID: util.MustParseUUID(userID),
		EventType: "membership_changed", Reason: "agent removed", OldState: lockedPool.State, NewState: lockedPool.State,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to audit provider quota pool removal")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit provider quota pool removal")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) HoldProviderQuotaPool(w http.ResponseWriter, r *http.Request) {
	h.changeProviderQuotaPoolState(w, r, false)
}

func (h *Handler) ReleaseProviderQuotaPool(w http.ResponseWriter, r *http.Request) {
	h.changeProviderQuotaPoolState(w, r, true)
}

func (h *Handler) changeProviderQuotaPoolState(w http.ResponseWriter, r *http.Request, release bool) {
	pool, userID, ok := h.loadOwnedProviderQuotaPool(w, r)
	if !ok {
		return
	}
	var request struct {
		ResetAt   string `json:"reset_at"`
		ResetDate string `json:"reset_date"`
		Reason    string `json:"reason"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid provider quota pool state request")
		return
	}
	reason := strings.TrimSpace(util.SanitizeTextForPostgres(redact.Text(request.Reason)))
	if reason == "" || len(reason) > 500 || (request.ResetAt != "" && request.ResetDate != "") {
		writeError(w, http.StatusBadRequest, "invalid provider quota pool state fields")
		return
	}
	arg := db.SetProviderQuotaPoolStateParams{}
	if !release {
		switch {
		case request.ResetAt != "":
			at, err := time.Parse(time.RFC3339Nano, request.ResetAt)
			if err != nil || !at.After(time.Now()) {
				writeError(w, http.StatusBadRequest, "reset_at must be a future timestamp")
				return
			}
			arg.State = "held_exact"
			arg.ResetAt = pgtype.Timestamptz{Time: at.UTC(), Valid: true}
		case request.ResetDate != "":
			date, err := time.Parse("2006-01-02", request.ResetDate)
			location, zoneErr := time.LoadLocation(pool.Timezone)
			if err != nil || zoneErr != nil || date.Format("2006-01-02") != request.ResetDate {
				writeError(w, http.StatusBadRequest, "invalid reset_date or pool timezone")
				return
			}
			localToday := time.Now().In(location).Format("2006-01-02")
			if request.ResetDate < localToday {
				writeError(w, http.StatusBadRequest, "reset_date must be today or later in the pool timezone")
				return
			}
			arg.State = "held_date"
			arg.ResetDate = pgtype.Date{Time: date, Valid: true}
		default:
			arg.State = "reset_unknown"
		}
		arg.ObservedAt = pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
	} else {
		arg.State = "open"
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start provider quota pool update")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	locked, err := qtx.GetProviderQuotaPoolForUpdate(r.Context(), pool.ID)
	if err != nil || uuidToString(locked.OwnerID) != userID {
		writeError(w, http.StatusNotFound, "provider quota pool not found")
		return
	}
	if !h.canManageProviderQuotaPoolAgents(r.Context(), userID, pool.ID) {
		writeError(w, http.StatusForbidden, "insufficient permissions for a mapped agent")
		return
	}
	if release && locked.State == "probing" {
		writeError(w, http.StatusConflict, "cancel the active probe before releasing this pool")
		return
	}
	if release && locked.State == "open" {
		writeJSON(w, http.StatusOK, locked)
		return
	}
	arg.ID, arg.ExpectedRevision = pool.ID, locked.Revision
	updated, err := qtx.SetProviderQuotaPoolState(r.Context(), arg)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update provider quota pool")
		return
	}
	eventType := "held"
	if release {
		eventType = "released"
	} else if locked.State != "open" {
		eventType = "extended"
	}
	if err := qtx.CreateProviderQuotaPoolEvent(r.Context(), db.CreateProviderQuotaPoolEventParams{
		ID: dbid.NewV7(), PoolID: pool.ID, ActorID: util.MustParseUUID(userID),
		EventType: eventType, Reason: reason, OldState: locked.State, NewState: updated.State,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to audit provider quota pool update")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit provider quota pool update")
		return
	}
	if release && h.TaskService != nil {
		h.TaskService.NotifyProviderQuotaPoolReleased(r.Context(), pool.ID)
	}
	writeJSON(w, http.StatusOK, updated)
}
