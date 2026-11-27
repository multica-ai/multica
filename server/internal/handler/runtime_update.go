package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/logger"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// CLI update request store

type UpdateStatus string

const (
	UpdatePending UpdateStatus = "pending"
	// UpdateScheduled: the daemon is busy and holds the request until its last
	// agent task finishes.
	UpdateScheduled UpdateStatus = "scheduled"
	UpdateRunning   UpdateStatus = "running"
	UpdateCompleted UpdateStatus = "completed"
	UpdateFailed    UpdateStatus = "failed"
	UpdateTimeout   UpdateStatus = "timeout"
	UpdateCancelled UpdateStatus = "cancelled"
	// UpdateExpired: the machine never became idle within updateQueueTimeout.
	UpdateExpired UpdateStatus = "expired"
)

// UpdateRequest represents a pending or completed CLI update request.
type UpdateRequest struct {
	ID              string `json:"id"`
	RuntimeID       string `json:"runtime_id"`
	InitiatorUserID string `json:"-"`
	MachineKey      string `json:"-"`
	// QueueWhenBusy is set by clients that can show a queued update. Only
	// such requests are held by a busy daemon; others keep failing fast.
	QueueWhenBusy bool         `json:"-"`
	Status        UpdateStatus `json:"status"`
	TargetVersion string       `json:"target_version"`
	Output        string       `json:"output,omitempty"`
	Error         string       `json:"error,omitempty"`
	// ActiveTasks is the daemon's running task count while scheduled.
	ActiveTasks  *int       `json:"active_tasks,omitempty"`
	Cancellable  bool       `json:"cancellable"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	RunStartedAt *time.Time `json:"-"`
}

const (
	updatePendingTimeout = 120 * time.Second
	updateRunningTimeout = 150 * time.Second
	updateQueueTimeout   = 24 * time.Hour
	updateStoreRetention = 5 * time.Minute
	// updateWaitingRetention keeps a waiting record (and its machine slot)
	// alive for as long as it can still be queued.
	updateWaitingRetention = updateQueueTimeout + updateStoreRetention
)

var (
	errUpdateInProgress     = &updateError{msg: "an update is already in progress or queued on this machine"}
	errUpdateWithdrawn      = &updateError{msg: "this update was cancelled, expired, or has already finished"}
	errUpdateNotCancellable = &updateError{msg: "this update can no longer be cancelled"}
)

type updateError struct{ msg string }

func (e *updateError) Error() string { return e.msg }

// updateTransition changes a request in place, or returns an error and
// leaves it untouched. Stores apply it atomically, so a cancel and a start
// racing on the same request can never both succeed.
type updateTransition func(req *UpdateRequest, now time.Time) error

type UpdateStore interface {
	Create(ctx context.Context, runtimeID, machineKey, targetVersion, initiatorUserID string, queueWhenBusy bool) (*UpdateRequest, error)
	Get(ctx context.Context, id string) (*UpdateRequest, error)
	// GetActive returns the unfinished request holding the machine, if any.
	GetActive(ctx context.Context, machineKey string) (*UpdateRequest, error)
	HasPending(ctx context.Context, runtimeID string) (bool, error)
	// PopPending returns the request to offer on this runtime's heartbeat.
	// When the daemon can hold it (offerDeferrable), the request stays
	// waiting and is offered again on every heartbeat until the daemon starts
	// it — so a restarted daemon picks it up again. Otherwise it is marked
	// running and offered once, exactly as before queued updates existed.
	PopPending(ctx context.Context, runtimeID string, daemonCanQueue bool) (*UpdateRequest, error)
	Transition(ctx context.Context, id string, fn updateTransition) (*UpdateRequest, error)
}

// updateMachineKey identifies the daemon process behind a runtime. A daemon
// registers one runtime per provider, so this is what one update applies to.
// The daemon ID is client-reported, so it is scoped by workspace and owner.
// The daemon itself refuses a second update across workspaces.
func updateMachineKey(rt db.AgentRuntime) string {
	if rt.DaemonID.Valid && rt.DaemonID.String != "" {
		return uuidToString(rt.WorkspaceID) + ":" + uuidToString(rt.OwnerID) + ":" + rt.DaemonID.String
	}
	return "runtime:" + uuidToString(rt.ID)
}

func updateRequestTerminal(status UpdateStatus) bool {
	switch status {
	case UpdateCompleted, UpdateFailed, UpdateTimeout, UpdateCancelled, UpdateExpired:
		return true
	}
	return false
}

func updateRequestWaiting(status UpdateStatus) bool {
	return status == UpdatePending || status == UpdateScheduled
}

func offerDeferrable(req *UpdateRequest, daemonCanQueue bool) bool {
	return req.QueueWhenBusy && daemonCanQueue
}

func updateRetention(req *UpdateRequest) time.Duration {
	if updateRequestTerminal(req.Status) {
		return updateStoreRetention
	}
	return updateWaitingRetention
}

func applyUpdateTimeout(req *UpdateRequest, now time.Time) bool {
	switch req.Status {
	case UpdatePending:
		if now.Sub(req.CreatedAt) > updatePendingTimeout {
			req.Status = UpdateTimeout
			req.Error = "daemon did not respond within 120 seconds"
			req.UpdatedAt = now
			return true
		}
	case UpdateScheduled:
		if now.Sub(req.CreatedAt) > updateQueueTimeout {
			req.Status = UpdateExpired
			req.Error = "the machine did not become idle within 24 hours"
			req.UpdatedAt = now
			return true
		}
	case UpdateRunning:
		if req.RunStartedAt != nil && now.Sub(*req.RunStartedAt) > updateRunningTimeout {
			req.Status = UpdateTimeout
			req.Error = "update did not complete within 150 seconds"
			req.UpdatedAt = now
			return true
		}
	}
	return false
}

// startUpdate is the daemon's go-ahead: only a request that is still waiting
// (not cancelled or expired) may start installing. A request that is already
// running stays running — that is a daemon without queue support confirming
// the offer it got.
func startUpdate(req *UpdateRequest, now time.Time) error {
	switch {
	case req.Status == UpdateRunning:
		return nil
	case updateRequestWaiting(req.Status):
		req.Status = UpdateRunning
		req.RunStartedAt = &now
		req.UpdatedAt = now
		return nil
	}
	return errUpdateWithdrawn
}

func scheduleUpdate(activeTasks *int) updateTransition {
	return func(req *UpdateRequest, now time.Time) error {
		if !updateRequestWaiting(req.Status) {
			return errUpdateWithdrawn
		}
		req.Status = UpdateScheduled
		req.ActiveTasks = activeTasks
		req.UpdatedAt = now
		return nil
	}
}

// updateCancellable: only requests from clients that chose to queue can be
// cancelled — other clients (the CLI's --wait) do not know "cancelled".
func updateCancellable(req *UpdateRequest) bool {
	return req.QueueWhenBusy && updateRequestWaiting(req.Status)
}

// cancelUpdate is idempotent: cancelling a cancelled request succeeds, so two
// tabs cancelling the same update both see it cancelled.
func cancelUpdate(req *UpdateRequest, now time.Time) error {
	if req.Status == UpdateCancelled {
		return nil
	}
	if !updateCancellable(req) {
		return errUpdateNotCancellable
	}
	req.Status = UpdateCancelled
	req.UpdatedAt = now
	return nil
}

func finishUpdate(status UpdateStatus, output, errMsg string) updateTransition {
	return func(req *UpdateRequest, now time.Time) error {
		if updateRequestTerminal(req.Status) {
			return errUpdateWithdrawn
		}
		req.Status = status
		req.Output = output
		req.Error = errMsg
		req.UpdatedAt = now
		return nil
	}
}

// ignoreWithdrawn turns a late terminal report into the no-op it always was.
func ignoreWithdrawn(err error) error {
	if errors.Is(err, errUpdateWithdrawn) {
		return nil
	}
	return err
}

// InMemoryUpdateStore is the single-node implementation. Multi-node deploys
// must use RedisUpdateStore so Web POST, daemon heartbeat, daemon report, and
// UI polling agree on the same request lifecycle.
type InMemoryUpdateStore struct {
	mu       sync.Mutex
	requests map[string]*UpdateRequest // keyed by update ID
}

func NewInMemoryUpdateStore() *InMemoryUpdateStore {
	return &InMemoryUpdateStore{
		requests: make(map[string]*UpdateRequest),
	}
}

func (s *InMemoryUpdateStore) Create(_ context.Context, runtimeID, machineKey, targetVersion, initiatorUserID string, queueWhenBusy bool) (*UpdateRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if machineKey == "" {
		machineKey = "runtime:" + runtimeID
	}
	now := time.Now()
	for id, req := range s.requests {
		applyUpdateTimeout(req, now)
		if updateRequestTerminal(req.Status) {
			if now.Sub(req.UpdatedAt) > updateStoreRetention {
				delete(s.requests, id)
			}
			continue
		}
		// One unfinished update per machine.
		if req.MachineKey == machineKey {
			return nil, errUpdateInProgress
		}
	}

	req := &UpdateRequest{
		ID:              randomID(),
		RuntimeID:       runtimeID,
		InitiatorUserID: initiatorUserID,
		MachineKey:      machineKey,
		QueueWhenBusy:   queueWhenBusy,
		Status:          UpdatePending,
		TargetVersion:   targetVersion,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	s.requests[req.ID] = req
	return cloneUpdateRequest(req), nil
}

func (s *InMemoryUpdateStore) Get(_ context.Context, id string) (*UpdateRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	req, ok := s.requests[id]
	if !ok {
		return nil, nil
	}
	applyUpdateTimeout(req, time.Now())
	return cloneUpdateRequest(req), nil
}

func (s *InMemoryUpdateStore) GetActive(_ context.Context, machineKey string) (*UpdateRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for _, req := range s.requests {
		applyUpdateTimeout(req, now)
		if req.MachineKey == machineKey && !updateRequestTerminal(req.Status) {
			return cloneUpdateRequest(req), nil
		}
	}
	return nil, nil
}

func (s *InMemoryUpdateStore) HasPending(_ context.Context, runtimeID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.oldestWaitingLocked(runtimeID, time.Now()) != nil, nil
}

func (s *InMemoryUpdateStore) oldestWaitingLocked(runtimeID string, now time.Time) *UpdateRequest {
	var oldest *UpdateRequest
	for _, req := range s.requests {
		applyUpdateTimeout(req, now)
		if req.RuntimeID == runtimeID && updateRequestWaiting(req.Status) {
			if oldest == nil || req.CreatedAt.Before(oldest.CreatedAt) {
				oldest = req
			}
		}
	}
	return oldest
}

func (s *InMemoryUpdateStore) PopPending(_ context.Context, runtimeID string, daemonCanQueue bool) (*UpdateRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	req := s.oldestWaitingLocked(runtimeID, now)
	if req == nil {
		return nil, nil
	}
	if !offerDeferrable(req, daemonCanQueue) {
		_ = startUpdate(req, now)
	}
	return cloneUpdateRequest(req), nil
}

func (s *InMemoryUpdateStore) Transition(_ context.Context, id string, fn updateTransition) (*UpdateRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	req, ok := s.requests[id]
	if !ok {
		return nil, nil
	}
	now := time.Now()
	applyUpdateTimeout(req, now)
	err := fn(req, now)
	return cloneUpdateRequest(req), err
}

func (s *InMemoryUpdateStore) Complete(ctx context.Context, id string, output string) error {
	_, err := s.Transition(ctx, id, finishUpdate(UpdateCompleted, output, ""))
	return ignoreWithdrawn(err)
}

func (s *InMemoryUpdateStore) Fail(ctx context.Context, id string, errMsg string) error {
	_, err := s.Transition(ctx, id, finishUpdate(UpdateFailed, "", errMsg))
	return ignoreWithdrawn(err)
}

func cloneUpdateRequest(req *UpdateRequest) *UpdateRequest {
	out := *req
	return &out
}

// Handlers

func writeUpdate(w http.ResponseWriter, update *UpdateRequest) {
	update.Cancellable = updateCancellable(update)
	writeJSON(w, http.StatusOK, update)
}

// InitiateUpdate creates a new CLI update request (protected route, called by frontend).
func (h *Handler) InitiateUpdate(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	rt, member, ok := h.requireRuntimeReadAccess(w, r, obsmetrics.RuntimeLookupSourceRuntimeAPI, runtimeID)
	if !ok {
		return
	}
	if !canEditRuntime(member, rt) {
		writeError(w, http.StatusForbidden, "only runtime owners and workspace admins can update runtimes")
		return
	}

	var req struct {
		TargetVersion string `json:"target_version"`
		QueueWhenBusy bool   `json:"queue_when_busy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.TargetVersion == "" {
		writeError(w, http.StatusBadRequest, "target_version is required")
		return
	}

	update, err := h.UpdateStore.Create(
		r.Context(),
		uuidToString(rt.ID),
		updateMachineKey(rt),
		req.TargetVersion,
		uuidToString(member.UserID),
		req.QueueWhenBusy,
	)
	if err != nil {
		// Only the in-progress rejection is a conflict the caller can act on.
		// Every other Create failure is infrastructure — the Redis store wraps
		// connection failures as "reserve active update: ..." / "persist update
		// request: ..." — and echoing it back would both leak internals and
		// label an outage as a user-fixable conflict. That was survivable while
		// the CLI hid 409 bodies; it no longer is, now that they are shown by
		// default.
		if errors.Is(err, errUpdateInProgress) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		slog.Error("UpdateStore Create failed", "error", err, "runtime_id", uuidToString(rt.ID))
		writeError(w, http.StatusInternalServerError, "failed to start the update")
		return
	}

	writeUpdate(w, update)
}

// GetActiveUpdate returns the unfinished update of this runtime's machine,
// started through any of the machine's runtimes, so a reload or another
// runtime of the same daemon shows the queue.
func (h *Handler) GetActiveUpdate(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	rt, member, ok := h.requireRuntimeReadAccess(w, r, obsmetrics.RuntimeLookupSourceRuntimeUpdatePoll, runtimeID)
	if !ok {
		return
	}
	if !canEditRuntime(member, rt) {
		writeJSON(w, http.StatusOK, map[string]any{"update": nil})
		return
	}

	update, err := h.UpdateStore.GetActive(r.Context(), updateMachineKey(rt))
	if err != nil {
		slog.Warn("load active update failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load update")
		return
	}
	if update != nil {
		update.Cancellable = updateCancellable(update)
	}
	writeJSON(w, http.StatusOK, map[string]any{"update": update})
}

// loadMachineUpdate loads an update that belongs to this runtime's machine.
func (h *Handler) loadMachineUpdate(w http.ResponseWriter, r *http.Request, rt db.AgentRuntime) (*UpdateRequest, bool) {
	update, err := h.UpdateStore.Get(r.Context(), chi.URLParam(r, "updateId"))
	if err != nil {
		slog.Warn("load update failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load update")
		return nil, false
	}
	if update == nil || (update.RuntimeID != uuidToString(rt.ID) && update.MachineKey != updateMachineKey(rt)) {
		writeError(w, http.StatusNotFound, "update not found")
		return nil, false
	}
	return update, true
}

// GetUpdate returns the status of an update request (protected route, called by frontend).
func (h *Handler) GetUpdate(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	rt, member, ok := h.requireRuntimeReadAccess(w, r, obsmetrics.RuntimeLookupSourceRuntimeUpdatePoll, runtimeID)
	if !ok {
		return
	}
	update, ok := h.loadMachineUpdate(w, r, rt)
	if !ok {
		return
	}
	// Keep an in-flight poll alive if an admin is downgraded after starting
	// the update. This exception is scoped to the immutable request initiator;
	// other plain members still cannot read another runtime's update status.
	if !canEditRuntime(member, rt) && update.InitiatorUserID != uuidToString(member.UserID) {
		writeError(w, http.StatusForbidden, "only runtime owners, workspace admins, and the update initiator can view this update")
		return
	}

	writeUpdate(w, update)
}

// CancelUpdate withdraws an update that has not started installing yet.
func (h *Handler) CancelUpdate(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	rt, member, ok := h.requireRuntimeReadAccess(w, r, obsmetrics.RuntimeLookupSourceRuntimeAPI, runtimeID)
	if !ok {
		return
	}
	if !canEditRuntime(member, rt) {
		writeError(w, http.StatusForbidden, "only runtime owners and workspace admins can cancel updates")
		return
	}
	existing, ok := h.loadMachineUpdate(w, r, rt)
	if !ok {
		return
	}

	update, err := h.UpdateStore.Transition(r.Context(), existing.ID, cancelUpdate)
	switch {
	case errors.Is(err, errUpdateNotCancellable):
		writeError(w, http.StatusConflict, err.Error())
		return
	case err != nil:
		slog.Error("UpdateStore cancel failed", "error", err, "update_id", existing.ID)
		writeError(w, http.StatusInternalServerError, "failed to cancel the update")
		return
	case update == nil:
		writeError(w, http.StatusNotFound, "update not found")
		return
	}
	writeUpdate(w, update)
}

// ReportUpdateResult receives the update result from the daemon.
//
// "scheduled" (busy, holding the update) and "running" (about to install) are
// answered with 409 once the request was cancelled, expired, or finished; a
// daemon holding a queued update installs only after a 2xx to "running".
func (h *Handler) ReportUpdateResult(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")

	// Verify the caller owns this runtime's workspace.
	if _, ok := h.requireDaemonRuntimeAccess(w, r, runtimeID); !ok {
		return
	}

	updateID := chi.URLParam(r, "updateId")

	existing, err := h.UpdateStore.Get(r.Context(), updateID)
	if err != nil {
		slog.Warn("load update failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load update")
		return
	}
	if existing == nil || existing.RuntimeID != runtimeID {
		writeError(w, http.StatusNotFound, "update not found")
		return
	}

	var req struct {
		Status      string `json:"status"` // "scheduled", "running", "completed", or "failed"
		Output      string `json:"output"`
		Error       string `json:"error"`
		ActiveTasks *int   `json:"active_tasks"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var fn updateTransition
	switch req.Status {
	case "completed":
		fn = finishUpdate(UpdateCompleted, req.Output, "")
	case "failed":
		fn = finishUpdate(UpdateFailed, "", req.Error)
	case "running":
		fn = startUpdate
	case "scheduled":
		fn = scheduleUpdate(req.ActiveTasks)
	default:
		writeError(w, http.StatusBadRequest, "invalid status: "+req.Status)
		return
	}

	updated, err := h.UpdateStore.Transition(r.Context(), updateID, fn)
	switch {
	case errors.Is(err, errUpdateWithdrawn) && (req.Status == "completed" || req.Status == "failed"):
		slog.Debug("ignoring stale update report", "runtime_id", runtimeID, "update_id", updateID, "status", req.Status)
	case errors.Is(err, errUpdateWithdrawn):
		writeError(w, http.StatusConflict, err.Error())
		return
	case err != nil:
		slog.Error("UpdateStore report failed", "error", err, "update_id", updateID, "status", req.Status)
		writeError(w, http.StatusInternalServerError, "failed to persist update status")
		return
	case updated == nil:
		writeError(w, http.StatusNotFound, "update not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
