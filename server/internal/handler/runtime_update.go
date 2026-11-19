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
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// CLI update request store

type UpdateStatus string

const (
	UpdatePending UpdateStatus = "pending"
	// UpdateScheduled means the daemon was busy and the update starts
	// automatically once the machine is idle. Still cancellable.
	UpdateScheduled UpdateStatus = "scheduled"
	UpdateRunning   UpdateStatus = "running"
	UpdateCompleted UpdateStatus = "completed"
	UpdateFailed    UpdateStatus = "failed"
	UpdateTimeout   UpdateStatus = "timeout"
	UpdateCancelled UpdateStatus = "cancelled"
	// UpdateExpired is a scheduled update the machine never became idle for
	// within updateScheduledTimeout.
	UpdateExpired UpdateStatus = "expired"
)

// UpdateRequest represents a pending or completed CLI update request.
type UpdateRequest struct {
	ID              string       `json:"id"`
	RuntimeID       string       `json:"runtime_id"`
	InitiatorUserID string       `json:"-"`
	Status          UpdateStatus `json:"status"`
	TargetVersion   string       `json:"target_version"`
	Output          string       `json:"output,omitempty"`
	Error           string       `json:"error,omitempty"`
	// ActiveTasks is the machine-wide running-task count the daemon reported
	// when it deferred the update. Nil for daemons that predate deferrable
	// updates, which only report that they are busy.
	ActiveTasks  *int       `json:"active_tasks,omitempty"`
	ScheduledAt  *time.Time `json:"scheduled_at,omitempty"`
	RunStartedAt *time.Time `json:"run_started_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	// QueueWhenBusy is set when the client that started the update renders a
	// queued update. Requests from older clients (installed CLIs, Desktop
	// builds) keep failing fast when the machine is busy, which is all they
	// know how to show.
	QueueWhenBusy bool `json:"-"`
	// OfferLeaseUntil blocks cancelling while a non-deferrable offer is out:
	// a daemon on that path installs even when its "running" report is
	// refused, so a cancel accepted in that window would not be honoured.
	OfferLeaseUntil *time.Time `json:"-"`
}

const (
	updatePendingTimeout = 120 * time.Second
	updateRunningTimeout = 150 * time.Second
	// updateScheduledTimeout bounds how long a queued update waits for an idle
	// machine. An update that lands days after the click would surprise the
	// person who asked for it.
	updateScheduledTimeout = 24 * time.Hour
	// updateStoreRetention is how long a finished request stays readable.
	updateStoreRetention = 5 * time.Minute
	// updateOfferLease is how long a non-deferrable offer waits for the
	// daemon's answer before the request is cancellable again. Daemons answer
	// within seconds (their report retries end after about 7s).
	updateOfferLease = 60 * time.Second
)

// updateReofferInterval is how often a delivered but unstarted request is
// offered to the daemon again. Daemons that predate deferrable updates reject
// a busy offer and only start when an offer lands while they are idle; newer
// daemons hold the request and treat re-offers as a keepalive that survives
// their own restart. A variable so tests can shorten it.
var updateReofferInterval = 30 * time.Second

var (
	errUpdateInProgress = &updateError{msg: "an update is already in progress for this runtime"}
	// errUpdateNotRunnable rejects a daemon starting or re-queueing a request
	// that was cancelled, expired or already finished.
	errUpdateNotRunnable = &updateError{msg: "this update was cancelled or is no longer queued"}
	// errUpdateNotCancellable rejects cancelling once installation started.
	errUpdateNotCancellable = &updateError{msg: "this update has already started or finished"}
	// errUpdateBeingDelivered rejects cancelling during an offer lease.
	errUpdateBeingDelivered = &updateError{msg: "the daemon is picking this update up; try again in a minute"}
)

type updateError struct{ msg string }

func (e *updateError) Error() string { return e.msg }

type UpdateStore interface {
	Create(ctx context.Context, runtimeID, targetVersion, initiatorUserID string, queueWhenBusy bool) (*UpdateRequest, error)
	Get(ctx context.Context, id string) (*UpdateRequest, error)
	// GetActive returns the runtime's unfinished request, or nil.
	GetActive(ctx context.Context, runtimeID string) (*UpdateRequest, error)
	// HasPending reports whether a request is due to be offered to the daemon.
	HasPending(ctx context.Context, runtimeID string) (bool, error)
	// PopPending claims the oldest due request for delivery to a daemon that
	// does (deferrableDaemon) or does not understand deferrable offers. The
	// request keeps its status: it only becomes running when the daemon says
	// it started, and it is offered again after updateReofferInterval until
	// then. A queued request that is already running is offered again only to
	// a deferrable daemon, which may have missed the answer to its "running"
	// report. A non-deferrable offer takes an offer lease (see prepareOffer).
	PopPending(ctx context.Context, runtimeID string, deferrableDaemon bool) (*UpdateRequest, error)
	// MarkRunning records that the daemon started installing. It returns
	// errUpdateNotRunnable once the request was cancelled, expired or finished,
	// which is how a daemon holding a queued request learns it was cancelled.
	MarkRunning(ctx context.Context, id string) (*UpdateRequest, error)
	// Schedule records that the daemon deferred the update until it is idle.
	Schedule(ctx context.Context, id string, activeTasks *int) (*UpdateRequest, error)
	// Cancel stops a pending or scheduled request; errUpdateNotCancellable
	// once it is running or finished, errUpdateBeingDelivered during an offer
	// lease.
	Cancel(ctx context.Context, id string) (*UpdateRequest, error)
	Complete(ctx context.Context, id string, output string) error
	Fail(ctx context.Context, id string, errMsg string) error
}

func updateRequestTerminal(status UpdateStatus) bool {
	switch status {
	case UpdateCompleted, UpdateFailed, UpdateTimeout, UpdateCancelled, UpdateExpired:
		return true
	}
	return false
}

// updateRequestWaiting is a request the daemon has not started yet.
func updateRequestWaiting(status UpdateStatus) bool {
	return status == UpdatePending || status == UpdateScheduled
}

// updateOfferLeased reports an offer still waiting for a non-deferrable
// daemon's answer.
func updateOfferLeased(req *UpdateRequest, now time.Time) bool {
	return req.OfferLeaseUntil != nil && now.Before(*req.OfferLeaseUntil)
}

// offerDeferrable is whether an offer of req to this daemon may be deferred.
func offerDeferrable(req *UpdateRequest, deferrableDaemon bool) bool {
	return deferrableDaemon && req.QueueWhenBusy
}

// prepareOffer decides whether a due request is delivered to this daemon and
// records the offer on req. A non-deferrable offer takes a lease so it cannot
// be cancelled until the daemon answers (or the lease runs out): that daemon
// would install regardless of a refused "running" report.
func prepareOffer(req *UpdateRequest, deferrableDaemon bool, now time.Time) bool {
	switch {
	case updateRequestWaiting(req.Status):
	case req.Status == UpdateRunning && offerDeferrable(req, deferrableDaemon):
		return true
	default:
		return false
	}
	if !offerDeferrable(req, deferrableDaemon) {
		lease := now.Add(updateOfferLease)
		req.OfferLeaseUntil = &lease
	}
	return true
}

func applyUpdateTimeout(req *UpdateRequest, now time.Time) bool {
	// Never end a request a daemon may be starting right now.
	if updateOfferLeased(req, now) {
		return false
	}
	switch req.Status {
	case UpdatePending:
		if now.Sub(req.CreatedAt) > updatePendingTimeout {
			req.Status = UpdateTimeout
			req.Error = "daemon did not respond within 120 seconds"
			req.UpdatedAt = now
			return true
		}
	case UpdateScheduled:
		if req.ScheduledAt != nil && now.Sub(*req.ScheduledAt) > updateScheduledTimeout {
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

// Transitions shared by the in-memory and Redis stores. Each mutates req in
// place and reports an error when the transition is not allowed.

func transitionToRunning(req *UpdateRequest, now time.Time) error {
	if req.Status != UpdateRunning && !updateRequestWaiting(req.Status) {
		return errUpdateNotRunnable
	}
	// A repeated report means the daemon did not get the answer to the first
	// one and is only starting now, so the run clock restarts too.
	req.Status = UpdateRunning
	req.RunStartedAt = &now
	req.OfferLeaseUntil = nil
	req.UpdatedAt = now
	return nil
}

func transitionToScheduled(req *UpdateRequest, activeTasks *int, now time.Time) error {
	if !updateRequestWaiting(req.Status) {
		return errUpdateNotRunnable
	}
	if req.ScheduledAt == nil {
		req.ScheduledAt = &now
	}
	req.Status = UpdateScheduled
	req.ActiveTasks = activeTasks
	req.OfferLeaseUntil = nil
	req.UpdatedAt = now
	return nil
}

func transitionToCancelled(req *UpdateRequest, now time.Time) error {
	if !updateRequestWaiting(req.Status) {
		return errUpdateNotCancellable
	}
	if updateOfferLeased(req, now) {
		return errUpdateBeingDelivered
	}
	req.Status = UpdateCancelled
	req.UpdatedAt = now
	return nil
}

// transitionToResult records a terminal daemon report. Reports for a request
// that already ended are ignored rather than rewriting its outcome.
func transitionToResult(req *UpdateRequest, status UpdateStatus, output, errMsg string, now time.Time) bool {
	if updateRequestTerminal(req.Status) {
		return false
	}
	req.Status = status
	req.Output = output
	req.Error = errMsg
	req.OfferLeaseUntil = nil
	req.UpdatedAt = now
	return true
}

// InMemoryUpdateStore is the single-node implementation. Multi-node deploys
// must use RedisUpdateStore so Web POST, daemon heartbeat, daemon report, and
// UI polling agree on the same request lifecycle.
type InMemoryUpdateStore struct {
	mu        sync.Mutex
	requests  map[string]*UpdateRequest // keyed by update ID
	nextOffer map[string]time.Time      // update ID -> earliest next delivery
}

func NewInMemoryUpdateStore() *InMemoryUpdateStore {
	return &InMemoryUpdateStore{
		requests:  make(map[string]*UpdateRequest),
		nextOffer: make(map[string]time.Time),
	}
}

func (s *InMemoryUpdateStore) Create(_ context.Context, runtimeID, targetVersion, initiatorUserID string, queueWhenBusy bool) (*UpdateRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for id, req := range s.requests {
		applyUpdateTimeout(req, now)
		// Drop finished requests once they are no longer worth reading; a
		// queued one can legitimately be older than the retention window.
		if updateRequestTerminal(req.Status) && now.Sub(req.UpdatedAt) > updateStoreRetention {
			delete(s.requests, id)
			delete(s.nextOffer, id)
		}
	}

	// Reject if this runtime already has an unfinished update.
	for _, req := range s.requests {
		if req.RuntimeID == runtimeID && !updateRequestTerminal(req.Status) {
			return nil, errUpdateInProgress
		}
	}

	req := &UpdateRequest{
		ID:              randomID(),
		RuntimeID:       runtimeID,
		InitiatorUserID: initiatorUserID,
		Status:          UpdatePending,
		TargetVersion:   targetVersion,
		CreatedAt:       now,
		UpdatedAt:       now,
		QueueWhenBusy:   queueWhenBusy,
	}
	s.requests[req.ID] = req
	s.nextOffer[req.ID] = now
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

func (s *InMemoryUpdateStore) GetActive(_ context.Context, runtimeID string) (*UpdateRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for _, req := range s.requests {
		applyUpdateTimeout(req, now)
		if req.RuntimeID == runtimeID && !updateRequestTerminal(req.Status) {
			return cloneUpdateRequest(req), nil
		}
	}
	return nil, nil
}

// dueLocked returns the runtime's oldest unfinished request due for delivery.
func (s *InMemoryUpdateStore) dueLocked(runtimeID string, now time.Time) *UpdateRequest {
	var oldest *UpdateRequest
	for id, req := range s.requests {
		applyUpdateTimeout(req, now)
		if req.RuntimeID != runtimeID || updateRequestTerminal(req.Status) {
			continue
		}
		if next, ok := s.nextOffer[id]; ok && next.After(now) {
			continue
		}
		if oldest == nil || req.CreatedAt.Before(oldest.CreatedAt) {
			oldest = req
		}
	}
	return oldest
}

func (s *InMemoryUpdateStore) HasPending(_ context.Context, runtimeID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dueLocked(runtimeID, time.Now()) != nil, nil
}

func (s *InMemoryUpdateStore) PopPending(_ context.Context, runtimeID string, deferrableDaemon bool) (*UpdateRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	req := s.dueLocked(runtimeID, now)
	if req == nil {
		return nil, nil
	}
	s.nextOffer[req.ID] = now.Add(updateReofferInterval)
	if !prepareOffer(req, deferrableDaemon, now) {
		return nil, nil
	}
	return cloneUpdateRequest(req), nil
}

// mutate applies fn to a stored request under the lock. A nil request means
// the ID is unknown (or already dropped from retention).
func (s *InMemoryUpdateStore) mutate(id string, fn func(*UpdateRequest, time.Time) error) (*UpdateRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	req, ok := s.requests[id]
	if !ok {
		return nil, nil
	}
	now := time.Now()
	applyUpdateTimeout(req, now)
	err := fn(req, now)
	if updateRequestTerminal(req.Status) {
		delete(s.nextOffer, id)
	}
	return cloneUpdateRequest(req), err
}

func (s *InMemoryUpdateStore) MarkRunning(_ context.Context, id string) (*UpdateRequest, error) {
	return s.mutate(id, transitionToRunning)
}

func (s *InMemoryUpdateStore) Schedule(_ context.Context, id string, activeTasks *int) (*UpdateRequest, error) {
	return s.mutate(id, func(req *UpdateRequest, now time.Time) error {
		return transitionToScheduled(req, activeTasks, now)
	})
}

func (s *InMemoryUpdateStore) Cancel(_ context.Context, id string) (*UpdateRequest, error) {
	return s.mutate(id, transitionToCancelled)
}

func (s *InMemoryUpdateStore) Complete(_ context.Context, id string, output string) error {
	_, err := s.mutate(id, func(req *UpdateRequest, now time.Time) error {
		transitionToResult(req, UpdateCompleted, output, "", now)
		return nil
	})
	return err
}

func (s *InMemoryUpdateStore) Fail(_ context.Context, id string, errMsg string) error {
	_, err := s.mutate(id, func(req *UpdateRequest, now time.Time) error {
		transitionToResult(req, UpdateFailed, "", errMsg, now)
		return nil
	})
	return err
}

// cloneUpdateRequest hands callers a snapshot so a handler reading fields
// never races the store mutating the stored request.
func cloneUpdateRequest(req *UpdateRequest) *UpdateRequest {
	if req == nil {
		return nil
	}
	out := *req
	if req.ActiveTasks != nil {
		n := *req.ActiveTasks
		out.ActiveTasks = &n
	}
	if req.ScheduledAt != nil {
		t := *req.ScheduledAt
		out.ScheduledAt = &t
	}
	if req.RunStartedAt != nil {
		t := *req.RunStartedAt
		out.RunStartedAt = &t
	}
	if req.OfferLeaseUntil != nil {
		t := *req.OfferLeaseUntil
		out.OfferLeaseUntil = &t
	}
	return &out
}

// Handlers

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
		// QueueWhenBusy is sent by clients that show a queued update; see
		// UpdateRequest.QueueWhenBusy.
		QueueWhenBusy bool `json:"queue_when_busy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.TargetVersion == "" {
		writeError(w, http.StatusBadRequest, "target_version is required")
		return
	}

	// The update is machine-wide; one queued through another runtime on the
	// same machine already covers it.
	if existing, err := h.findMachineActiveUpdate(r.Context(), rt, member); err != nil {
		slog.Error("load machine active update failed", "error", err, "runtime_id", uuidToString(rt.ID))
		writeError(w, http.StatusInternalServerError, "failed to start the update")
		return
	} else if existing != nil {
		writeError(w, http.StatusConflict, errUpdateInProgress.Error())
		return
	}

	update, err := h.UpdateStore.Create(
		r.Context(),
		uuidToString(rt.ID),
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

	writeJSON(w, http.StatusOK, update)
}

// GetUpdate returns the status of an update request (protected route, called by frontend).
func (h *Handler) GetUpdate(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	rt, member, ok := h.requireRuntimeReadAccess(w, r, obsmetrics.RuntimeLookupSourceRuntimeUpdatePoll, runtimeID)
	if !ok {
		return
	}
	updateID := chi.URLParam(r, "updateId")

	update, err := h.UpdateStore.Get(r.Context(), updateID)
	if err != nil {
		slog.Warn("load update failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load update")
		return
	}
	if update == nil || update.RuntimeID != uuidToString(rt.ID) {
		writeError(w, http.StatusNotFound, "update not found")
		return
	}
	// Keep an in-flight poll alive if an admin is downgraded after starting
	// the update. This exception is scoped to the immutable request initiator;
	// other plain members still cannot read another runtime's update status.
	if !canEditRuntime(member, rt) && update.InitiatorUserID != uuidToString(member.UserID) {
		writeError(w, http.StatusForbidden, "only runtime owners, workspace admins, and the update initiator can view this update")
		return
	}

	writeJSON(w, http.StatusOK, update)
}

// GetActiveUpdate returns the unfinished update on the runtime's machine, so a
// queued update stays visible across reloads and to everyone who can manage
// it. The returned request's runtime_id is the one to poll and cancel through:
// it can be another runtime of the same machine.
func (h *Handler) GetActiveUpdate(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	rt, member, ok := h.requireRuntimeReadAccess(w, r, obsmetrics.RuntimeLookupSourceRuntimeUpdatePoll, runtimeID)
	if !ok {
		return
	}

	update, err := h.findMachineActiveUpdate(r.Context(), rt, member)
	if err != nil {
		slog.Warn("load active update failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load update")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"update": update})
}

// findMachineActiveUpdate returns the unfinished update on rt's machine that
// member may see: one on rt itself (for its managers or the initiator), or one
// on another runtime of the same daemon that member owns. A machine card picks
// whichever of its runtimes is online as the command channel, so the update
// may have been queued through a sibling.
func (h *Handler) findMachineActiveUpdate(ctx context.Context, rt db.AgentRuntime, member db.Member) (*UpdateRequest, error) {
	runtimeID := uuidToString(rt.ID)
	update, err := h.UpdateStore.GetActive(ctx, runtimeID)
	if err != nil {
		return nil, err
	}
	if update != nil && (canEditRuntime(member, rt) || update.InitiatorUserID == uuidToString(member.UserID)) {
		return update, nil
	}
	if !rt.DaemonID.Valid || rt.DaemonID.String == "" {
		return nil, nil
	}
	siblings, err := h.Queries.ListRuntimeOwnersByDaemon(ctx, db.ListRuntimeOwnersByDaemonParams{
		WorkspaceID: rt.WorkspaceID,
		DaemonID:    rt.DaemonID,
	})
	if err != nil {
		return nil, err
	}
	for _, sibling := range siblings {
		siblingID := uuidToString(sibling.ID)
		if siblingID == runtimeID || !sibling.OwnerID.Valid || uuidToString(sibling.OwnerID) != uuidToString(member.UserID) {
			continue
		}
		update, err := h.UpdateStore.GetActive(ctx, siblingID)
		if err != nil {
			return nil, err
		}
		if update != nil {
			return update, nil
		}
	}
	return nil, nil
}

// CancelUpdate stops an update that has not started installing yet.
func (h *Handler) CancelUpdate(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	rt, member, ok := h.requireRuntimeReadAccess(w, r, obsmetrics.RuntimeLookupSourceRuntimeAPI, runtimeID)
	if !ok {
		return
	}
	updateID := chi.URLParam(r, "updateId")

	existing, err := h.UpdateStore.Get(r.Context(), updateID)
	if err != nil {
		slog.Warn("load update failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load update")
		return
	}
	if existing == nil || existing.RuntimeID != uuidToString(rt.ID) {
		writeError(w, http.StatusNotFound, "update not found")
		return
	}
	if !canEditRuntime(member, rt) && existing.InitiatorUserID != uuidToString(member.UserID) {
		writeError(w, http.StatusForbidden, "only runtime owners, workspace admins, and the update initiator can cancel this update")
		return
	}

	update, err := h.UpdateStore.Cancel(r.Context(), updateID)
	switch {
	case errors.Is(err, errUpdateNotCancellable), errors.Is(err, errUpdateBeingDelivered):
		writeError(w, http.StatusConflict, err.Error())
		return
	case err != nil:
		slog.Error("UpdateStore Cancel failed", "error", err, "update_id", updateID)
		writeError(w, http.StatusInternalServerError, "failed to cancel the update")
		return
	case update == nil:
		writeError(w, http.StatusNotFound, "update not found")
		return
	}

	writeJSON(w, http.StatusOK, update)
}

// ReportUpdateResult receives the update result from the daemon.
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
		Status      string `json:"status"` // "running", "scheduled", "completed", or "failed"
		Output      string `json:"output"`
		Error       string `json:"error"`
		ActiveTasks *int   `json:"active_tasks"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// A daemon released before deferrable updates fails a busy offer with
	// this exact text. When the client asked for queueing, keep the request
	// queued; the next re-offer that lands while the machine is idle starts it.
	if req.Status == "failed" && req.Error == protocol.RuntimeUpdateBusyError && existing.QueueWhenBusy && updateRequestWaiting(existing.Status) {
		req.Status = string(UpdateScheduled)
		req.ActiveTasks = nil
	}

	switch req.Status {
	case "completed":
		if err := h.UpdateStore.Complete(r.Context(), updateID, req.Output); err != nil {
			slog.Error("UpdateStore Complete failed", "error", err, "update_id", updateID)
			writeError(w, http.StatusInternalServerError, "failed to persist completion")
			return
		}
	case "failed":
		if err := h.UpdateStore.Fail(r.Context(), updateID, req.Error); err != nil {
			slog.Error("UpdateStore Fail failed", "error", err, "update_id", updateID)
			writeError(w, http.StatusInternalServerError, "failed to persist failure")
			return
		}
	case "running", "scheduled":
		var transitionErr error
		if req.Status == "running" {
			_, transitionErr = h.UpdateStore.MarkRunning(r.Context(), updateID)
		} else {
			_, transitionErr = h.UpdateStore.Schedule(r.Context(), updateID, req.ActiveTasks)
		}
		if errors.Is(transitionErr, errUpdateNotRunnable) {
			// The daemon must drop the request: it was cancelled, expired or
			// finished while queued.
			slog.Debug("rejecting update report for a request that is no longer queued",
				"runtime_id", runtimeID, "update_id", updateID, "status", req.Status)
			writeError(w, http.StatusConflict, transitionErr.Error())
			return
		}
		if transitionErr != nil {
			slog.Error("UpdateStore transition failed", "error", transitionErr, "update_id", updateID, "status", req.Status)
			writeError(w, http.StatusInternalServerError, "failed to persist update status")
			return
		}
	default:
		writeError(w, http.StatusBadRequest, "invalid status: "+req.Status)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
