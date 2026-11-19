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
	"github.com/multica-ai/multica/server/internal/util"
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
	// UpdateExpired is a queued update that never started within
	// updateScheduledTimeout.
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
	// Cancellable is computed for responses (see updateCancellable).
	Cancellable bool `json:"cancellable"`

	// MachineKey names the machine the update is for (see updateMachineKey).
	// One machine has at most one unfinished request.
	MachineKey string `json:"-"`
	// MachineRuntimeIDs are the runtimes the machine served when the request
	// was created; the Redis store holds each one's slot (see
	// RedisUpdateStore).
	MachineRuntimeIDs []string `json:"-"`
	// QueueWhenBusy is set when the client that started the update renders a
	// queued update. Requests from older clients (installed CLIs, Desktop
	// builds) keep failing fast when the machine is busy, and are never
	// cancelled under them, which is all they know how to show.
	QueueWhenBusy bool `json:"-"`
	// LegacyOfferAt marks a fail-fast offer the daemon has not answered yet.
	// A daemon on that path installs even when its "running" report is
	// refused, so while one is out the request is not offered again, cannot
	// be cancelled and does not expire. An answer clears it; without one it
	// ends as a timeout, because whether it installed is unknown.
	LegacyOfferAt *time.Time `json:"-"`
	// LegacyQuietUntil holds the next fail-fast offer back after an answer.
	// Those daemons cannot say which offer a report belongs to, so a late
	// retry of an earlier answer must not be mistaken for the next offer's.
	LegacyQuietUntil *time.Time `json:"-"`
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
	// updateLegacyOfferTimeout ends a request whose fail-fast offer was never
	// answered. It outlasts a daemon's longest report retry: four attempts
	// with a 30s HTTP timeout plus about 7s of backoff.
	updateLegacyOfferTimeout = 5 * time.Minute
	// updateLegacyQuiet is the pause after an answered fail-fast offer, for the
	// same retry window.
	updateLegacyQuiet = 3 * time.Minute
)

// updateReofferInterval is how often an unfinished request is offered to the
// daemon again. Daemons that predate deferrable updates reject a busy offer
// and only start when an offer lands while they are idle; newer daemons hold
// the request and treat re-offers as a keepalive that survives their own
// restart. A variable so tests can shorten it.
var updateReofferInterval = 30 * time.Second

var (
	errUpdateInProgress = &updateError{msg: "an update is already in progress for this machine"}
	// errUpdateNotRunnable rejects a daemon starting or re-queueing a request
	// that was cancelled, expired or already finished.
	errUpdateNotRunnable = &updateError{msg: "this update was cancelled or is no longer queued"}
	// errUpdateNotCancellable rejects cancelling once installation started, or
	// a request whose client cannot show a cancelled update.
	errUpdateNotCancellable = &updateError{msg: "this update can no longer be cancelled"}
	// errUpdateBeingDelivered rejects cancelling while a fail-fast offer is out.
	errUpdateBeingDelivered = &updateError{msg: "the daemon is picking this update up; try again in a minute"}
)

type updateError struct{ msg string }

func (e *updateError) Error() string { return e.msg }

type UpdateStore interface {
	// Create starts a request for runtimeID unless its machine already has an
	// unfinished one. machineRuntimeIDs lists the machine's runtimes.
	Create(ctx context.Context, runtimeID, machineKey, targetVersion, initiatorUserID string, queueWhenBusy bool, machineRuntimeIDs ...string) (*UpdateRequest, error)
	Get(ctx context.Context, id string) (*UpdateRequest, error)
	// GetActive returns the machine's unfinished request, or nil.
	GetActive(ctx context.Context, machineKey string, machineRuntimeIDs ...string) (*UpdateRequest, error)
	// HasPending reports whether a request is due to be offered to the daemon.
	HasPending(ctx context.Context, runtimeID string) (bool, error)
	// PopPending claims the oldest due request for delivery to a daemon that
	// does (deferrableDaemon) or does not understand deferrable offers. The
	// request keeps its status: it only becomes running when the daemon says
	// it started, and it is offered again after updateReofferInterval until
	// it ends (see prepareOffer for what each kind of daemon gets).
	PopPending(ctx context.Context, runtimeID string, deferrableDaemon bool) (*UpdateRequest, error)
	// MarkRunning records that the daemon started installing. It returns
	// errUpdateNotRunnable once the request was cancelled, expired or finished,
	// which is how a daemon holding a queued request learns it was cancelled.
	MarkRunning(ctx context.Context, id string) (*UpdateRequest, error)
	// Schedule records that the daemon deferred the update until it is idle.
	Schedule(ctx context.Context, id string, activeTasks *int) (*UpdateRequest, error)
	// Cancel stops a request that has not started; see transitionToCancelled.
	Cancel(ctx context.Context, id string) (*UpdateRequest, error)
	Complete(ctx context.Context, id string, output string) error
	Fail(ctx context.Context, id string, errMsg string) error
}

// updateMachineKey identifies the machine a runtime runs on. A daemon serves
// every runtime (and workspace) on its machine and updates them together, so
// its runtimes share the key. The daemon ID is reported by the daemon, so it is
// scoped by the runtime owner the daemon's credential belongs to (MUL-8154):
// another account reporting the same ID gets a different key. A runtime
// without a daemon ID or owner stands for itself.
func updateMachineKey(rt db.AgentRuntime) string {
	if rt.DaemonID.Valid && rt.DaemonID.String != "" && rt.OwnerID.Valid {
		return "daemon:" + uuidToString(rt.OwnerID) + ":" + rt.DaemonID.String
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

// updateRequestWaiting is a request the daemon has not started yet.
func updateRequestWaiting(status UpdateStatus) bool {
	return status == UpdatePending || status == UpdateScheduled
}

// offerDeferrable is whether an offer of req to this daemon may be deferred.
func offerDeferrable(req *UpdateRequest, deferrableDaemon bool) bool {
	return deferrableDaemon && req.QueueWhenBusy
}

// prepareOffer decides whether a due request is delivered to this daemon and
// records the offer on req.
//
//   - A fail-fast request keeps the previous release's contract: delivered
//     once, running from delivery.
//   - A queued request goes to a deferrable daemon every interval, also once
//     it is running: the daemon may have missed the answer to its "running"
//     report, and that answer is the only thing it installs on.
//   - A queued request goes to a fail-fast daemon only while no other offer to
//     it is unanswered, and not during the quiet period after an answer.
func prepareOffer(req *UpdateRequest, deferrableDaemon bool, now time.Time) bool {
	if !req.QueueWhenBusy {
		if req.Status != UpdatePending {
			return false
		}
		req.Status = UpdateRunning
		req.RunStartedAt = &now
		req.UpdatedAt = now
		return true
	}
	if deferrableDaemon {
		return updateRequestWaiting(req.Status) || req.Status == UpdateRunning
	}
	if !updateRequestWaiting(req.Status) || req.LegacyOfferAt != nil {
		return false
	}
	if req.LegacyQuietUntil != nil && now.Before(*req.LegacyQuietUntil) {
		return false
	}
	req.LegacyOfferAt = &now
	return true
}

// answerLegacyOffer records the daemon's answer to an outstanding fail-fast
// offer.
func answerLegacyOffer(req *UpdateRequest, now time.Time) {
	if req.LegacyOfferAt == nil {
		return
	}
	req.LegacyOfferAt = nil
	quiet := now.Add(updateLegacyQuiet)
	req.LegacyQuietUntil = &quiet
}

// updateCancellable is whether Cancel would succeed right now.
func updateCancellable(req *UpdateRequest) bool {
	return req.QueueWhenBusy && updateRequestWaiting(req.Status) && req.LegacyOfferAt == nil
}

func applyUpdateTimeout(req *UpdateRequest, now time.Time) bool {
	// While a fail-fast offer is unanswered the daemon may be installing, so
	// nothing else may end the request.
	if req.LegacyOfferAt != nil {
		if now.Sub(*req.LegacyOfferAt) <= updateLegacyOfferTimeout {
			return false
		}
		req.Status = UpdateTimeout
		req.Error = "the daemon never answered this update, so it may or may not have been installed"
		req.LegacyOfferAt = nil
		req.UpdatedAt = now
		return true
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
		// Counted from the click, so the update never lands more than a day
		// after someone asked for it.
		if now.Sub(req.CreatedAt) > updateScheduledTimeout {
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
	req.LegacyOfferAt = nil
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
	answerLegacyOffer(req, now)
	req.UpdatedAt = now
	return nil
}

// transitionToCancelled cancels a request that has not started. Requests from
// clients that cannot show a cancelled update are never cancelled, and none is
// while a fail-fast offer is unanswered.
func transitionToCancelled(req *UpdateRequest, now time.Time) error {
	if !updateRequestWaiting(req.Status) || !req.QueueWhenBusy {
		return errUpdateNotCancellable
	}
	if req.LegacyOfferAt != nil {
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
	req.LegacyOfferAt = nil
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

// A single node has no replicas of another release, so the machine key alone
// decides which requests share a machine.
func (s *InMemoryUpdateStore) Create(_ context.Context, runtimeID, machineKey, targetVersion, initiatorUserID string, queueWhenBusy bool, _ ...string) (*UpdateRequest, error) {
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

	// Reject if this runtime or machine already has an unfinished update.
	for _, req := range s.requests {
		if updateRequestTerminal(req.Status) {
			continue
		}
		if req.RuntimeID == runtimeID || (machineKey != "" && req.MachineKey == machineKey) {
			return nil, errUpdateInProgress
		}
	}

	req := &UpdateRequest{
		ID:              randomID(),
		RuntimeID:       runtimeID,
		MachineKey:      machineKey,
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

func (s *InMemoryUpdateStore) GetActive(_ context.Context, machineKey string, _ ...string) (*UpdateRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for _, req := range s.requests {
		applyUpdateTimeout(req, now)
		if machineKey != "" && req.MachineKey == machineKey && !updateRequestTerminal(req.Status) {
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
	for _, p := range []**time.Time{&out.ScheduledAt, &out.RunStartedAt, &out.LegacyOfferAt, &out.LegacyQuietUntil} {
		if *p != nil {
			t := **p
			*p = &t
		}
	}
	if req.ActiveTasks != nil {
		n := *req.ActiveTasks
		out.ActiveTasks = &n
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

	machineRuntimeIDs, err := h.machineRuntimeIDs(r.Context(), rt)
	if err != nil {
		slog.Error("list machine runtimes failed", "error", err, "runtime_id", uuidToString(rt.ID))
		writeError(w, http.StatusInternalServerError, "failed to start the update")
		return
	}
	update, err := h.UpdateStore.Create(
		r.Context(),
		uuidToString(rt.ID),
		updateMachineKey(rt),
		req.TargetVersion,
		uuidToString(member.UserID),
		req.QueueWhenBusy,
		machineRuntimeIDs...,
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

// machineRuntimeIDs lists the runtimes on rt's machine (see updateMachineKey),
// rt included, across the owner's workspaces.
func (h *Handler) machineRuntimeIDs(ctx context.Context, rt db.AgentRuntime) ([]string, error) {
	ids := []string{uuidToString(rt.ID)}
	if !rt.DaemonID.Valid || rt.DaemonID.String == "" || !rt.OwnerID.Valid {
		return ids, nil
	}
	rows, err := h.Queries.ListMachineRuntimeIDs(ctx, db.ListMachineRuntimeIDsParams{
		OwnerID:  rt.OwnerID,
		DaemonID: rt.DaemonID.String,
	})
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if id := uuidToString(row); id != ids[0] {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// writeUpdate answers with an update request and whether it can be cancelled.
func writeUpdate(w http.ResponseWriter, update *UpdateRequest) {
	update.Cancellable = updateCancellable(update)
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

	writeUpdate(w, update)
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

	machineRuntimeIDs, err := h.machineRuntimeIDs(r.Context(), rt)
	if err != nil {
		slog.Warn("list machine runtimes failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load update")
		return
	}
	update, err := h.UpdateStore.GetActive(r.Context(), updateMachineKey(rt), machineRuntimeIDs...)
	if err != nil {
		slog.Warn("load active update failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load update")
		return
	}
	if update != nil {
		visible, err := h.canSeeUpdate(r.Context(), update, rt, member)
		if err != nil {
			slog.Warn("authorize active update failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to load update")
			return
		}
		if !visible {
			update = nil
		}
	}
	if update != nil {
		update.Cancellable = updateCancellable(update)
	}

	writeJSON(w, http.StatusOK, map[string]any{"update": update})
}

// canSeeUpdate applies the same rule as polling the update through the runtime
// it was queued on: its initiator, or someone who can see and manage that
// runtime. rt is the runtime the caller asked through, already authorized.
// That runtime may be in another of the machine's workspaces; the caller's
// membership there decides.
func (h *Handler) canSeeUpdate(ctx context.Context, update *UpdateRequest, rt db.AgentRuntime, member db.Member) (bool, error) {
	target := rt
	if update.RuntimeID != uuidToString(rt.ID) {
		id, err := util.ParseUUID(update.RuntimeID)
		if err != nil {
			return false, nil
		}
		other, err := h.getAgentRuntime(ctx, obsmetrics.RuntimeLookupSourceRuntimeUpdatePoll, id)
		if err != nil {
			if isNotFound(err) {
				return false, nil
			}
			return false, err
		}
		if uuidToString(other.WorkspaceID) != uuidToString(member.WorkspaceID) {
			member, err = h.getWorkspaceMember(ctx, uuidToString(member.UserID), uuidToString(other.WorkspaceID))
			if err != nil {
				if isNotFound(err) {
					return false, nil
				}
				return false, err
			}
		}
		if !canUseRuntimeForAgent(member, other) {
			return false, nil
		}
		target = other
	}
	// The initiator exception applies only while the caller can still see
	// that runtime, as when polling the update through it.
	return canEditRuntime(member, target) || update.InitiatorUserID == uuidToString(member.UserID), nil
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

	writeUpdate(w, update)
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
		// A daemon holding a queued request installs only on this explicit
		// acceptance; a server that predates queued updates answers a bare
		// "ok" without checking for cancellation.
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "accepted": req.Status})
		return
	default:
		writeError(w, http.StatusBadRequest, "invalid status: "+req.Status)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
