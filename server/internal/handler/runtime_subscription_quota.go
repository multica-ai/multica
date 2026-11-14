package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/events"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func runtimeSupportsSubscriptionQuota(rt db.AgentRuntime) bool {
	return (rt.Provider == "codex" || protocol.ObservesTaskSubscriptionQuota(rt.Provider)) &&
		rt.RuntimeMode == "local" && !rt.ProfileID.Valid && rt.OwnerID.Valid
}

func (h *Handler) requireRuntimeQuotaOwner(w http.ResponseWriter, r *http.Request) (db.AgentRuntime, bool) {
	rt, member, ok := h.requireRuntimeReadAccess(w, r, obsmetrics.RuntimeLookupSourceRuntimeAPI, chi.URLParam(r, "runtimeId"))
	if !ok {
		return db.AgentRuntime{}, false
	}
	if !runtimeSupportsSubscriptionQuota(rt) || rt.OwnerID != member.UserID || uuidToString(rt.WorkspaceID) != h.resolveWorkspaceID(r) {
		writeError(w, http.StatusNotFound, "runtime quota not found")
		return db.AgentRuntime{}, false
	}
	return rt, true
}

func (h *Handler) GetRuntimeSubscriptionQuota(w http.ResponseWriter, r *http.Request) {
	rt, ok := h.requireRuntimeQuotaOwner(w, r)
	if !ok {
		return
	}
	h.readRuntimeQuota(w, r, rt)
}

func (h *Handler) RequestRuntimeSubscriptionQuota(w http.ResponseWriter, r *http.Request) {
	rt, ok := h.requireRuntimeQuotaOwner(w, r)
	if !ok {
		return
	}
	if rt.Provider != "codex" {
		writeError(w, http.StatusMethodNotAllowed, "quota is observed during tasks")
		return
	}
	if !runtimeHasCapability(rt.Metadata, protocol.DaemonCapabilitySubscriptionQuotaV1) {
		h.readRuntimeQuota(w, r, rt)
		return
	}
	if rt.Status != "online" {
		writeError(w, http.StatusServiceUnavailable, "runtime is offline")
		return
	}

	id, ownerID, requestID := uuidToString(rt.ID), uuidToString(rt.OwnerID), randomID()
	cooldown := 5 * time.Second
	if r.URL.Query().Get("if_stale") == "true" {
		cooldown = 2 * time.Minute
	}
	state, err := h.quotaStore.update(r.Context(), id, func(current runtimeQuotaState) (runtimeQuotaState, bool) {
		now := time.Now()
		report := current.reportFor(ownerID, now)
		switch report.Status {
		case "pending":
			return current, false
		case "complete", "failed", "unavailable":
			if now.Sub(current.RequestedAt) < cooldown {
				return current, false
			}
		}
		return runtimeQuotaState{
			OwnerID: ownerID, RequestID: requestID, RequestedAt: now,
			Report: protocol.SubscriptionQuotaReport{Status: "pending", Snapshot: report.Snapshot},
		}, true
	})
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "failed to request runtime quota")
		return
	}
	if state.RequestID == requestID {
		h.requestDaemonPendingWork(id, protocol.PendingWorkKindQuota)
	}
	writeRuntimeQuota(w, state.reportFor(ownerID, time.Now()))
}

func (h *Handler) readRuntimeQuota(w http.ResponseWriter, r *http.Request, rt db.AgentRuntime) {
	id := uuidToString(rt.ID)
	states, err := h.quotaStore.read(r.Context(), []string{id})
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "failed to load runtime quota")
		return
	}
	report := states[id].reportFor(uuidToString(rt.OwnerID), time.Now())
	if rt.Provider == "codex" && !runtimeHasCapability(rt.Metadata, protocol.DaemonCapabilitySubscriptionQuotaV1) {
		report.Status = protocol.SubscriptionQuotaUnsupported
	}
	writeRuntimeQuota(w, report)
}

func writeRuntimeQuota(w http.ResponseWriter, report protocol.SubscriptionQuotaReport) {
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, report)
}

func (h *Handler) pendingRuntimeSubscriptionQuota(ctx context.Context, id string) string {
	ctx, cancel := context.WithTimeout(ctx, heartbeatHasPendingTimeout)
	defer cancel()
	states, err := h.quotaStore.read(ctx, []string{id})
	if err != nil {
		slog.Debug("subscription quota heartbeat read failed", "runtime_id", id, "error", err)
		return ""
	}
	if state := states[id]; state.pending(time.Now()) {
		return state.RequestID
	}
	return ""
}

func (h *Handler) ReportRuntimeSubscriptionQuota(w http.ResponseWriter, r *http.Request) {
	rt, ok := h.requireDaemonRuntimeAccess(w, r, chi.URLParam(r, "runtimeId"))
	if !ok {
		return
	}
	if rt.Provider != "codex" || !runtimeSupportsSubscriptionQuota(rt) {
		writeError(w, http.StatusNotFound, "runtime quota not found")
		return
	}

	var report protocol.SubscriptionQuotaReport
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if json.NewDecoder(r.Body).Decode(&report) != nil || !validSubscriptionQuotaReport(report, time.Now()) ||
		(report.Snapshot != nil && report.Snapshot.Exhausted) {
		writeError(w, http.StatusBadRequest, "invalid runtime quota report")
		return
	}
	id, ownerID := uuidToString(rt.ID), uuidToString(rt.OwnerID)
	requestID := chi.URLParam(r, "requestId")
	if _, err := h.quotaStore.update(r.Context(), id, func(state runtimeQuotaState) (runtimeQuotaState, bool) {
		now := time.Now()
		if state.OwnerID != ownerID || state.RequestID != requestID || !state.pending(now) {
			return state, false
		}
		if report.Status == protocol.SubscriptionQuotaFailed {
			report.Snapshot = state.reportFor(ownerID, now).Snapshot
		}
		state.Report = report
		return state, true
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save runtime quota")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ObserveRuntimeSubscriptionQuota accepts only normalized task observations.
// It does not create or satisfy a Codex refresh request.
func (h *Handler) ObserveRuntimeSubscriptionQuota(w http.ResponseWriter, r *http.Request) {
	rt, ok := h.requireDaemonRuntimeAccess(w, r, chi.URLParam(r, "runtimeId"))
	if !ok {
		return
	}
	if !protocol.ObservesTaskSubscriptionQuota(rt.Provider) || !runtimeSupportsSubscriptionQuota(rt) {
		writeError(w, http.StatusNotFound, "runtime quota not found")
		return
	}

	var snapshot protocol.SubscriptionQuotaSnapshot
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	report := protocol.SubscriptionQuotaReport{Status: protocol.SubscriptionQuotaComplete, Snapshot: &snapshot}
	if json.NewDecoder(r.Body).Decode(&snapshot) != nil || !validSubscriptionQuotaReport(report, time.Now()) ||
		(rt.Provider == "dsh" && (!snapshot.Exhausted || snapshot.Primary != nil || snapshot.Secondary != nil || snapshot.LimitResetsAt != nil)) {
		writeError(w, http.StatusBadRequest, "invalid runtime quota observation")
		return
	}

	id, ownerID := uuidToString(rt.ID), uuidToString(rt.OwnerID)
	changed := false
	_, err := h.quotaStore.update(r.Context(), id, func(state runtimeQuotaState) (runtimeQuotaState, bool) {
		previous := state.Report.Snapshot
		changed = state.OwnerID != ownerID || previous == nil || previous.ObservedAt < snapshot.ObservedAt
		if !changed {
			return state, false
		}
		return runtimeQuotaState{OwnerID: ownerID, Report: report}, true
	})
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "failed to save runtime quota observation")
		return
	}
	if changed {
		h.Bus.Publish(events.Event{
			Type: protocol.EventDaemonRegister, WorkspaceID: uuidToString(rt.WorkspaceID),
			MemberPrivateUserID: ownerID, ActorType: "system",
		})
	}
	w.WriteHeader(http.StatusNoContent)
}

func validSubscriptionQuotaReport(report protocol.SubscriptionQuotaReport, now time.Time) bool {
	switch report.Status {
	case protocol.SubscriptionQuotaUnavailable, protocol.SubscriptionQuotaFailed:
		return report.Snapshot == nil
	case protocol.SubscriptionQuotaComplete:
		s := report.Snapshot
		// A machine's clock can differ from the server's. Bound timestamps
		// while tolerating moderate drift.
		if s == nil || s.ObservedAt < now.Add(-5*time.Minute).Unix() || s.ObservedAt > now.Add(5*time.Minute).Unix() ||
			(!s.Exhausted && s.Primary == nil && s.Secondary == nil) ||
			(s.LimitResetsAt != nil && (!s.Exhausted || !validQuotaReset(*s.LimitResetsAt, now))) {
			return false
		}
		for _, w := range []*protocol.SubscriptionQuotaWindow{s.Primary, s.Secondary} {
			if w == nil {
				continue
			}
			if w.UsedPercent == nil || *w.UsedPercent < 0 || *w.UsedPercent > 100 ||
				(w.DurationMins != nil && (*w.DurationMins <= 0 || *w.DurationMins > 365*24*60)) ||
				(w.ResetsAt != nil && !validQuotaReset(*w.ResetsAt, now)) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func validQuotaReset(reset int64, now time.Time) bool {
	return reset > 0 && reset <= now.AddDate(1, 0, 0).Unix()
}
