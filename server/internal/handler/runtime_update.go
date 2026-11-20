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
//
// A request moves through:
//
//	pending ─┬─> running ─> completed | failed | timeout
//	         ├─> scheduled ─┬─> running
//	         │              ├─> cancelled
//	         │              └─> expired
//	         ├─> cancelled
//	         └─> timeout (the daemon never picked it up)
//
// "scheduled" only exists for a request the client asked to queue
// (QueueWhenBusy) and that was offered to a daemon that can hold it
// (SupportsDeferredUpdate): the daemon was busy and starts the update once its
// last task finishes. Such a daemon installs only after the server accepts its
// "running" report, and the server refuses that report once the request was
// cancelled or expired. Accepting "running" and cancelling are both
// compare-and-set transitions on the same record, so exactly one of them wins.
//
// Every other request keeps the original contract: it becomes running the
// moment it is delivered, and a busy daemon fails it.

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
	// updateScheduledTimeout of the click.
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
	// while it held the update.
	ActiveTasks  *int       `json:"active_tasks,omitempty"`
	ScheduledAt  *time.Time `json:"scheduled_at,omitempty"`
	RunStartedAt *time.Time `json:"run_started_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	// Cancellable is computed for responses (see updateCancellable).
	Cancellable bool `json:"cancellable"`

	// MachineKey names the machine the update is for (see updateMachineKey).
	// A machine has at most one unfinished request, whichever of its runtimes
	// it was started from.
	MachineKey string `json:"-"`
	// QueueWhenBusy is set when the client that started the update can show a
	// queued one. Requests from clients that cannot (installed CLIs and Desktop
	// builds from before queued updates) keep failing fast when the machine is
	// busy and are never cancelled, which is all those clients can render.
	QueueWhenBusy bool `json:"-"`
}

const (
	updatePendingTimeout = 120 * time.Second
	updateRunningTimeout = 150 * time.Second
	// updateScheduledTimeout bounds how long a queued update waits for an idle
	// machine, counted from the click. An update that lands days after someone
	// asked for it would surprise them.
	updateScheduledTimeout = 24 * time.Hour
	// updateStoreRetention is how long a finished request stays readable.
	updateStoreRetention = 5 * time.Minute
	// updateActiveRetention keeps an unfinished request (and its machine slot)
	// alive for as long as it can possibly stay unfinished. Reads end it
	// earlier through applyUpdateTimeout.
	updateActiveRetention = updateScheduledTimeout + updateStoreRetention
	// updateInstallLease is how long after it started an install may still be
	// running although the server stopped waiting for its result. Until the
	// daemon reports how it ended, or this passes, the machine takes no other
	// update: a second one would be installed right after the first, possibly
	// to another version. Daemons stop their installer after
	// protocol.RuntimeUpdateInstallTimeout, so once this passes the earlier
	// install has ended one way or the other.
	updateInstallLease = 3 * protocol.RuntimeUpdateInstallTimeout
)

// updateReofferInterval is how often an unfinished queued request is offered
// to a daemon that can hold it. The daemon treats a re-offer of the request it
// holds as a keepalive, so a daemon that restarted picks the request up again;
// a request that is already running is re-offered too, in case the daemon
// missed the answer to its "running" report. A variable so tests can shorten
// it.
var updateReofferInterval = 30 * time.Second

var (
	errUpdateInProgress = &updateError{msg: "an update is already in progress for this machine"}
	// errUpdateNotRunnable rejects a daemon starting or re-queueing a request
	// that was cancelled, expired or already finished.
	errUpdateNotRunnable = &updateError{msg: "this update was cancelled or is no longer queued"}
	// errUpdateNotCancellable rejects cancelling once installation started, or
	// a request whose client cannot show a cancelled update.
	errUpdateNotCancellable = &updateError{msg: "this update can no longer be cancelled"}
)

type updateError struct{ msg string }

func (e *updateError) Error() string { return e.msg }

type UpdateStore interface {
	// Create starts a request for runtimeID unless another request holds its
	// machine (errUpdateInProgress; see updateHoldsMachine).
	Create(ctx context.Context, runtimeID, machineKey, targetVersion, initiatorUserID string, queueWhenBusy bool) (*UpdateRequest, error)
	Get(ctx context.Context, id string) (*UpdateRequest, error)
	// GetActive returns the request holding the machine (see
	// updateHoldsMachine), or nil.
	GetActive(ctx context.Context, machineKey string) (*UpdateRequest, error)
	// HasPending reports whether a request is due to be offered to the daemon.
	HasPending(ctx context.Context, runtimeID string) (bool, error)
	// PopPending claims the oldest due request for delivery to a daemon that
	// does (deferrableDaemon) or does not support deferred updates. See
	// prepareOffer for what each one gets.
	PopPending(ctx context.Context, runtimeID string, deferrableDaemon bool) (*UpdateRequest, error)
	// MarkRunning records that the daemon is about to install. It returns
	// errUpdateNotRunnable once the request was cancelled, expired or
	// finished, which is how a daemon holding a queued request learns to drop
	// it.
	MarkRunning(ctx context.Context, id string) (*UpdateRequest, error)
	// Schedule records that the daemon is holding the update until it is idle.
	Schedule(ctx context.Context, id string, activeTasks *int) (*UpdateRequest, error)
	// Cancel stops a queued request that has not started.
	Cancel(ctx context.Context, id string) (*UpdateRequest, error)
	Complete(ctx context.Context, id string, output string) error
	Fail(ctx context.Context, id string, errMsg string) error
}

// updateMachineKey identifies the machine a runtime runs on. One daemon serves
// every runtime (and workspace) on its machine and updates them together, so
// its runtimes share the key. The daemon ID is reported by the daemon, so it is
// scoped by the runtime owner its credential belongs to (MUL-8154): another
// account reporting the same ID gets a different key. A runtime without a
// daemon ID or owner stands for itself.
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

// updateHoldsMachine is whether req still keeps other updates off its machine:
// it is unfinished, or it timed out while an install it started may still be
// running.
func updateHoldsMachine(req *UpdateRequest, now time.Time) bool {
	if !updateRequestTerminal(req.Status) {
		return true
	}
	return updateInstallUnsettled(req) && now.Before(req.RunStartedAt.Add(updateInstallLease))
}

// updateInstallUnsettled is a request that timed out after its install
// started, so how the install ended is unknown.
func updateInstallUnsettled(req *UpdateRequest) bool {
	return req.Status == UpdateTimeout && req.RunStartedAt != nil
}

// updateRequestWaiting is a request the daemon has not started yet.
func updateRequestWaiting(status UpdateStatus) bool {
	return status == UpdatePending || status == UpdateScheduled
}

// offerDeferrable is whether an offer of req to this daemon may be deferred.
func offerDeferrable(req *UpdateRequest, deferrableDaemon bool) bool {
	return deferrableDaemon && req.QueueWhenBusy
}

// updateOfferPlan says what happens to a request's place in the offer queue.
type updateOfferPlan struct {
	remove bool
	next   time.Time // next offer time when !remove; zero keeps the current one
}

// prepareOffer decides whether a due request is delivered to this daemon,
// records the delivery on req, and says when to offer it next.
//
//   - A queued request goes to a daemon that can hold it every
//     updateReofferInterval while it is waiting or running. Delivery changes
//     nothing: the daemon's "running" report is what starts it.
//   - Any other pending request is delivered once and is running from then
//     on, as before queued updates. It can no longer be cancelled, because a
//     daemon on that path installs without waiting for the server's answer.
func prepareOffer(req *UpdateRequest, deferrableDaemon bool, now time.Time) (bool, updateOfferPlan) {
	if updateRequestTerminal(req.Status) {
		return false, updateOfferPlan{remove: true}
	}
	if offerDeferrable(req, deferrableDaemon) {
		return true, updateOfferPlan{next: now.Add(updateReofferInterval)}
	}
	if req.Status == UpdatePending {
		req.Status = UpdateRunning
		req.RunStartedAt = &now
		req.UpdatedAt = now
		return true, updateOfferPlan{remove: true}
	}
	if req.QueueWhenBusy {
		// Held by a deferrable daemon, and this heartbeat came from one that
		// is not (a downgrade). Keep offering; a deferrable daemon picks it up.
		return false, updateOfferPlan{next: now.Add(updateReofferInterval)}
	}
	return false, updateOfferPlan{remove: true}
}

// updateCancellable is whether Cancel would succeed right now.
func updateCancellable(req *UpdateRequest) bool {
	return req.QueueWhenBusy && updateRequestWaiting(req.Status)
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
			req.Error = "update did not report back within 150 seconds; it may still be installing, so the machine takes another update once it reports or after an hour"
			req.UpdatedAt = now
			return true
		}
	}
	return false
}

// Transitions shared by the in-memory and Redis stores. Each mutates req in
// place (timeouts already applied) and reports an error when the transition is
// not allowed.

func transitionToRunning(req *UpdateRequest, now time.Time) error {
	if req.Status != UpdateRunning && !updateRequestWaiting(req.Status) {
		return errUpdateNotRunnable
	}
	// A repeated report means the daemon did not get the answer to the first
	// one and is only starting now, so the run clock restarts too.
	req.Status = UpdateRunning
	req.RunStartedAt = &now
	req.UpdatedAt = now
	return nil
}

func transitionToScheduled(req *UpdateRequest, activeTasks *int, now time.Time) error {
	if !updateRequestWaiting(req.Status) || !req.QueueWhenBusy {
		return errUpdateNotRunnable
	}
	if req.ScheduledAt == nil {
		req.ScheduledAt = &now
	}
	req.Status = UpdateScheduled
	req.ActiveTasks = activeTasks
	req.UpdatedAt = now
	return nil
}

func transitionToCancelled(req *UpdateRequest, now time.Time) error {
	if !updateCancellable(req) {
		return errUpdateNotCancellable
	}
	req.Status = UpdateCancelled
	req.UpdatedAt = now
	return nil
}

// transitionToResult records a terminal daemon report. Reports for a request
// that already ended are ignored rather than rewriting its outcome, except for
// an install that timed out: the late report says how it actually ended, and
// frees the machine.
func transitionToResult(req *UpdateRequest, status UpdateStatus, output, errMsg string, now time.Time) {
	if updateRequestTerminal(req.Status) && !updateInstallUnsettled(req) {
		return
	}
	req.Status = status
	req.Output = output
	req.Error = errMsg
	req.UpdatedAt = now
}

// InMemoryUpdateStore is the single-node implementation. Multi-node deploys
// must use RedisUpdateStore so Web POST, daemon heartbeat, daemon report, and
// UI polling agree on the same request lifecycle.
type InMemoryUpdateStore struct {
	mu        sync.Mutex
	requests  map[string]*UpdateRequest // keyed by update ID
	nextOffer map[string]time.Time      // update ID -> next delivery; absent once delivered for good
}

func NewInMemoryUpdateStore() *InMemoryUpdateStore {
	return &InMemoryUpdateStore{
		requests:  make(map[string]*UpdateRequest),
		nextOffer: make(map[string]time.Time),
	}
}

func (s *InMemoryUpdateStore) Create(_ context.Context, runtimeID, machineKey, targetVersion, initiatorUserID string, queueWhenBusy bool) (*UpdateRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for id, req := range s.requests {
		s.applyTimeoutLocked(req, now)
		if !updateHoldsMachine(req, now) && now.Sub(req.UpdatedAt) > updateStoreRetention {
			delete(s.requests, id)
		}
	}
	if machineKey == "" {
		machineKey = "runtime:" + runtimeID
	}
	for _, req := range s.requests {
		if req.MachineKey == machineKey && updateHoldsMachine(req, now) {
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

func (s *InMemoryUpdateStore) applyTimeoutLocked(req *UpdateRequest, now time.Time) {
	applyUpdateTimeout(req, now)
	if updateRequestTerminal(req.Status) {
		delete(s.nextOffer, req.ID)
	}
}

func (s *InMemoryUpdateStore) Get(_ context.Context, id string) (*UpdateRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	req, ok := s.requests[id]
	if !ok {
		return nil, nil
	}
	s.applyTimeoutLocked(req, time.Now())
	return cloneUpdateRequest(req), nil
}

func (s *InMemoryUpdateStore) GetActive(_ context.Context, machineKey string) (*UpdateRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for _, req := range s.requests {
		s.applyTimeoutLocked(req, now)
		if machineKey != "" && req.MachineKey == machineKey && updateHoldsMachine(req, now) {
			return cloneUpdateRequest(req), nil
		}
	}
	return nil, nil
}

// dueLocked returns the runtime's oldest unfinished request due for delivery.
func (s *InMemoryUpdateStore) dueLocked(runtimeID string, now time.Time) *UpdateRequest {
	var oldest *UpdateRequest
	for id, next := range s.nextOffer {
		req, ok := s.requests[id]
		if !ok {
			delete(s.nextOffer, id)
			continue
		}
		s.applyTimeoutLocked(req, now)
		if req.RuntimeID != runtimeID || updateRequestTerminal(req.Status) || next.After(now) {
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
	for {
		req := s.dueLocked(runtimeID, now)
		if req == nil {
			return nil, nil
		}
		offer, plan := prepareOffer(req, deferrableDaemon, now)
		if plan.remove {
			delete(s.nextOffer, req.ID)
		} else if !plan.next.IsZero() {
			s.nextOffer[req.ID] = plan.next
		}
		if offer {
			return cloneUpdateRequest(req), nil
		}
	}
}

// mutate applies fn to a stored request under the lock, after timeouts. A nil
// request means the ID is unknown (or already dropped from retention).
func (s *InMemoryUpdateStore) mutate(id string, fn func(*UpdateRequest, time.Time) error) (*UpdateRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	req, ok := s.requests[id]
	if !ok {
		return nil, nil
	}
	now := time.Now()
	s.applyTimeoutLocked(req, now)
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
	for _, p := range []**time.Time{&out.ScheduledAt, &out.RunStartedAt} {
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

// GetActiveUpdate returns the update holding the runtime's machine, so a
// queued update stays visible across reloads and to everyone who can manage
// it. The returned request's runtime_id is the one to poll and cancel through:
// it can be another runtime of the same machine, in another workspace.
func (h *Handler) GetActiveUpdate(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	rt, member, ok := h.requireRuntimeReadAccess(w, r, obsmetrics.RuntimeLookupSourceRuntimeUpdatePoll, runtimeID)
	if !ok {
		return
	}

	update, err := h.UpdateStore.GetActive(r.Context(), updateMachineKey(rt))
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

// canSeeUpdate is whether the caller can currently manage the runtime the
// update was started on, judged by their membership in that runtime's
// workspace. rt is the runtime the caller asked through, already authorized.
// Unlike polling a known update ID, being its initiator is not enough: these
// lookups lead to cancelling it.
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
	return canEditRuntime(member, target), nil
}

// CancelUpdate stops a queued update that has not started installing yet.
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
	if !canEditRuntime(member, rt) {
		writeError(w, http.StatusForbidden, "only runtime owners and workspace admins can cancel this update")
		return
	}

	update, err := h.UpdateStore.Cancel(r.Context(), updateID)
	switch {
	case errors.Is(err, errUpdateNotCancellable):
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
		var (
			moved         *UpdateRequest
			transitionErr error
		)
		if req.Status == "running" {
			moved, transitionErr = h.UpdateStore.MarkRunning(r.Context(), updateID)
		} else {
			moved, transitionErr = h.UpdateStore.Schedule(r.Context(), updateID, req.ActiveTasks)
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
		if moved == nil {
			// The record vanished after the read above (dropped during a
			// switch-over, or past retention). Never accept a start for it.
			writeError(w, http.StatusNotFound, "update not found")
			return
		}
		// A daemon holding a queued request installs only on this explicit
		// acceptance, never on a bare 200.
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "accepted": req.Status})
		return
	default:
		writeError(w, http.StatusBadRequest, "invalid status: "+req.Status)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
