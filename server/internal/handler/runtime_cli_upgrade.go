package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/logger"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/agentcli"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Agent CLI upgrade job statuses.
const (
	CLIUpgradeQueued    = "queued"
	CLIUpgradeDraining  = "draining"
	CLIUpgradeUpgrading = "upgrading"
	CLIUpgradeSucceeded = "succeeded"
	CLIUpgradeFailed    = "failed"
)

const (
	cliUpgradePendingTimeout = 120 * time.Second
	cliUpgradeRunningTimeout = 15 * time.Minute
	cliUpgradeStoreRetention = 30 * time.Minute
)

// CLIUpgradeJob is one Ensure/UpgradeManagedAgentCLI request tracked for UI poll.
type CLIUpgradeJob struct {
	ID              string    `json:"job_id"`
	RuntimeID       string    `json:"runtime_id"`
	CLI             string    `json:"cli"`
	InitiatorUserID string    `json:"-"`
	Status          string    `json:"status"`
	Error           string    `json:"error,omitempty"`
	Current         string    `json:"current,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	// QueuedAt is the wait-clock for the "daemon never claimed" pending
	// timeout. Refreshed on Busy requeue, when a sibling leaves in-flight via
	// SetStatus, and when a sibling times out draining/upgrading → failed.
	QueuedAt     time.Time  `json:"-"`
	DispatchedAt *time.Time `json:"-"`
}

func cliUpgradeTerminal(status string) bool {
	return status == CLIUpgradeSucceeded || status == CLIUpgradeFailed
}

func applyCLIUpgradeTimeout(job *CLIUpgradeJob, now time.Time, skipPendingTimeout bool) bool {
	switch job.Status {
	case CLIUpgradeQueued:
		// While another job on the same runtime is draining/upgrading, this
		// job is only waiting its serial turn — do not treat that as "daemon
		// never claimed" (drain may run up to ~10 minutes).
		if skipPendingTimeout {
			return false
		}
		start := job.CreatedAt
		if !job.QueuedAt.IsZero() {
			start = job.QueuedAt
		}
		if now.Sub(start) > cliUpgradePendingTimeout {
			job.Status = CLIUpgradeFailed
			job.Error = "daemon did not pick up the upgrade within 120 seconds"
			job.UpdatedAt = now
			return true
		}
	case CLIUpgradeDraining, CLIUpgradeUpgrading:
		start := job.CreatedAt
		if job.DispatchedAt != nil {
			start = *job.DispatchedAt
		}
		if now.Sub(start) > cliUpgradeRunningTimeout {
			job.Status = CLIUpgradeFailed
			job.Error = "upgrade did not finish within the drain/upgrade window"
			job.UpdatedAt = now
			return true
		}
	}
	return false
}

func cliUpgradeInFlight(status string) bool {
	return status == CLIUpgradeDraining || status == CLIUpgradeUpgrading
}

// applyTimeoutsLocked evaluates timeouts for every job. Pending (120s) timeout
// is skipped for queued jobs whose runtime already has a draining/upgrading job.
// When a draining/upgrading job times out to failed, sibling QueuedAt clocks are
// refreshed the same way SetStatus does when leaving in-flight — otherwise the
// next apply sees no in-flight + a stale QueuedAt and false-fails waiters.
func (s *InMemoryCLIUpgradeStore) applyTimeoutsLocked(now time.Time) {
	jobs := make([]*CLIUpgradeJob, 0, len(s.jobs))
	for _, job := range s.jobs {
		jobs = append(jobs, job)
	}
	applyCLIUpgradeTimeouts(jobs, now)
}

// applyCLIUpgradeTimeouts is the shared timeout pass for the in-memory and
// Redis stores. Pending (120s) timeout is skipped for queued jobs whose
// runtime already has a draining or upgrading job. When a draining or
// upgrading job times out to failed, sibling QueuedAt clocks are refreshed
// the same way SetStatus does when leaving in-flight.
func applyCLIUpgradeTimeouts(jobs []*CLIUpgradeJob, now time.Time) {
	inFlight := make(map[string]bool)
	for _, job := range jobs {
		if job != nil && cliUpgradeInFlight(job.Status) {
			inFlight[job.RuntimeID] = true
		}
	}
	timedOut := make(map[string]string) // runtimeID -> timed-out job ID
	for _, job := range jobs {
		if job == nil {
			continue
		}
		prev := job.Status
		if !applyCLIUpgradeTimeout(job, now, job.Status == CLIUpgradeQueued && inFlight[job.RuntimeID]) {
			continue
		}
		if cliUpgradeInFlight(prev) && job.Status == CLIUpgradeFailed {
			timedOut[job.RuntimeID] = job.ID
		}
	}
	for runtimeID, exceptID := range timedOut {
		for _, other := range jobs {
			if other != nil && other.RuntimeID == runtimeID && other.ID != exceptID && other.Status == CLIUpgradeQueued {
				other.QueuedAt = now
				other.UpdatedAt = now
			}
		}
	}
}

func (s *InMemoryCLIUpgradeStore) refreshQueuedWaitClockLocked(runtimeID, exceptID string, now time.Time) {
	for _, other := range s.jobs {
		if other.RuntimeID == runtimeID && other.ID != exceptID && other.Status == CLIUpgradeQueued {
			other.QueuedAt = now
			other.UpdatedAt = now
		}
	}
}

// CLIUpgradeStore tracks agent-CLI upgrade jobs across POST / heartbeat / poll.
type CLIUpgradeStore interface {
	Create(ctx context.Context, runtimeID, cli, initiatorUserID string) (*CLIUpgradeJob, error)
	Get(ctx context.Context, id string) (*CLIUpgradeJob, error)
	HasPending(ctx context.Context, runtimeID string) (bool, error)
	PopPending(ctx context.Context, runtimeID string) (*CLIUpgradeJob, error)
	SetStatus(ctx context.Context, id, status, errMsg, current string) error
}

// InMemoryCLIUpgradeStore is the single-node job store.
type InMemoryCLIUpgradeStore struct {
	mu   sync.Mutex
	jobs map[string]*CLIUpgradeJob
}

// NewInMemoryCLIUpgradeStore constructs an empty in-memory store.
func NewInMemoryCLIUpgradeStore() *InMemoryCLIUpgradeStore {
	return &InMemoryCLIUpgradeStore{jobs: make(map[string]*CLIUpgradeJob)}
}

func (s *InMemoryCLIUpgradeStore) Create(_ context.Context, runtimeID, cli, initiatorUserID string) (*CLIUpgradeJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cli = strings.ToLower(strings.TrimSpace(cli))
	now := time.Now()
	for id, job := range s.jobs {
		if now.Sub(job.CreatedAt) > cliUpgradeStoreRetention {
			delete(s.jobs, id)
			continue
		}
	}
	s.applyTimeoutsLocked(now)
	for _, job := range s.jobs {
		if job.RuntimeID == runtimeID && job.CLI == cli && !cliUpgradeTerminal(job.Status) {
			// Idempotent: return the active job for the same runtime+cli.
			cp := *job
			return &cp, nil
		}
	}

	job := &CLIUpgradeJob{
		ID:              randomID(),
		RuntimeID:       runtimeID,
		CLI:             cli,
		InitiatorUserID: initiatorUserID,
		Status:          CLIUpgradeQueued,
		CreatedAt:       now,
		UpdatedAt:       now,
		QueuedAt:        now,
	}
	s.jobs[job.ID] = job
	cp := *job
	return &cp, nil
}

func (s *InMemoryCLIUpgradeStore) Get(_ context.Context, id string) (*CLIUpgradeJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return nil, nil
	}
	s.applyTimeoutsLocked(time.Now())
	cp := *job
	return &cp, nil
}

func (s *InMemoryCLIUpgradeStore) HasPending(_ context.Context, runtimeID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.applyTimeoutsLocked(now)
	for _, job := range s.jobs {
		if job.RuntimeID == runtimeID && job.Status == CLIUpgradeQueued {
			return true, nil
		}
	}
	return false, nil
}

func (s *InMemoryCLIUpgradeStore) PopPending(_ context.Context, runtimeID string) (*CLIUpgradeJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.applyTimeoutsLocked(now)
	// Serialize per runtime: while any job is draining/upgrading, leave
	// other queued jobs queued. A busy runtime requeues; it does not fail them.
	for _, job := range s.jobs {
		if job.RuntimeID == runtimeID && cliUpgradeInFlight(job.Status) {
			return nil, nil
		}
	}
	var oldest *CLIUpgradeJob
	for _, job := range s.jobs {
		if job.RuntimeID == runtimeID && job.Status == CLIUpgradeQueued {
			if oldest == nil || job.CreatedAt.Before(oldest.CreatedAt) {
				oldest = job
			}
		}
	}
	if oldest == nil {
		return nil, nil
	}
	oldest.Status = CLIUpgradeDraining
	started := now
	oldest.DispatchedAt = &started
	oldest.UpdatedAt = now
	cp := *oldest
	return &cp, nil
}

func (s *InMemoryCLIUpgradeStore) SetStatus(_ context.Context, id, status, errMsg, current string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return nil
	}
	if cliUpgradeTerminal(job.Status) {
		return nil
	}
	prev := job.Status
	now := time.Now()
	switch status {
	case CLIUpgradeQueued:
		// Daemon Busy / serialize: put the job back on the queue and refresh
		// the pending wait clock so Busy→queued does not inherit the old timer.
		job.Status = CLIUpgradeQueued
		job.DispatchedAt = nil
		job.Error = ""
		job.QueuedAt = now
	case CLIUpgradeDraining, CLIUpgradeUpgrading, CLIUpgradeSucceeded, CLIUpgradeFailed:
		job.Status = status
	default:
		return errors.New("invalid cli upgrade status")
	}
	if status != CLIUpgradeQueued {
		if errMsg != "" {
			job.Error = errMsg
		}
		if current != "" {
			job.Current = current
		}
	}
	job.UpdatedAt = now
	// Leaving draining/upgrading unblocks the serial queue: restart sibling
	// pending clocks so a long drain does not immediately 120s-fail waiters.
	if cliUpgradeInFlight(prev) && !cliUpgradeInFlight(job.Status) {
		s.refreshQueuedWaitClockLocked(job.RuntimeID, job.ID, now)
	}
	return nil
}

// runtimeCLIUpgradeAllowed reports who may install or replace agent CLIs.
//
// A local daemon runs on a person's machine. That matches
// canSetRuntimeVisibility: only the runtime owner may change what is
// installed there. Workspace admins keep canEditRuntime for rename and
// delete, which do not replace binaries on someone else's computer.
//
// A cloud runtime is workspace infrastructure. canEditRuntime already admits
// a workspace owner, a workspace admin, or the runtime's own owner.
func runtimeCLIUpgradeAllowed(member db.Member, rt db.AgentRuntime) bool {
	if rt.RuntimeMode == "cloud" {
		return canEditRuntime(member, rt)
	}
	return canSetRuntimeVisibility(member, rt)
}

func (h *Handler) requireRuntimeCLIUpgradePermission(w http.ResponseWriter, r *http.Request, rt db.AgentRuntime) (db.Member, bool) {
	member, ok := h.requireWorkspaceMember(w, r, uuidToString(rt.WorkspaceID), "runtime not found")
	if !ok {
		return db.Member{}, false
	}
	if runtimeCLIUpgradeAllowed(member, rt) {
		return member, true
	}
	writeError(w, http.StatusForbidden, "insufficient permissions")
	return db.Member{}, false
}

// InitiateCLIUpgrade starts an agent-CLI upgrade job.
func (h *Handler) InitiateCLIUpgrade(w http.ResponseWriter, r *http.Request) {
	if h.CLIUpgradeStore == nil {
		writeError(w, http.StatusServiceUnavailable, "agent CLI upgrade is not configured")
		return
	}
	runtimeID := chi.URLParam(r, "runtimeId")
	cliName := strings.ToLower(strings.TrimSpace(chi.URLParam(r, "cli")))
	if !agentcli.IsManaged(cliName) {
		writeError(w, http.StatusBadRequest, "unknown managed agent cli")
		return
	}

	rt, _, ok := h.requireRuntimeReadAccess(w, r, obsmetrics.RuntimeLookupSourceRuntimeAPI, runtimeID)
	if !ok {
		return
	}
	member, ok := h.requireRuntimeCLIUpgradePermission(w, r, rt)
	if !ok {
		return
	}

	job, err := h.CLIUpgradeStore.Create(r.Context(), uuidToString(rt.ID), cliName, uuidToString(member.UserID))
	if err != nil {
		slog.Error("CLIUpgradeStore Create failed", "error", err, "runtime_id", uuidToString(rt.ID))
		writeError(w, http.StatusInternalServerError, "failed to start the agent CLI upgrade")
		return
	}
	h.requestDaemonPendingWork(uuidToString(rt.ID), protocol.PendingWorkKindAgentCLIUpgrade)
	writeJSON(w, http.StatusOK, job)
}

// GetCLIUpgrade returns job status for UI polling.
func (h *Handler) GetCLIUpgrade(w http.ResponseWriter, r *http.Request) {
	if h.CLIUpgradeStore == nil {
		writeError(w, http.StatusServiceUnavailable, "agent CLI upgrade is not configured")
		return
	}
	runtimeID := chi.URLParam(r, "runtimeId")
	rt, member, ok := h.requireRuntimeReadAccess(w, r, obsmetrics.RuntimeLookupSourceRuntimeAPI, runtimeID)
	if !ok {
		return
	}
	jobID := chi.URLParam(r, "jobId")
	job, err := h.CLIUpgradeStore.Get(r.Context(), jobID)
	if err != nil {
		slog.Warn("load cli upgrade failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load upgrade job")
		return
	}
	if job == nil || job.RuntimeID != uuidToString(rt.ID) {
		writeError(w, http.StatusNotFound, "upgrade job not found")
		return
	}
	if !runtimeCLIUpgradeAllowed(member, rt) && job.InitiatorUserID != uuidToString(member.UserID) {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}
	writeJSON(w, http.StatusOK, job)
}

// ReportCLIUpgradeResult receives drain/upgrade progress from the daemon.
func (h *Handler) ReportCLIUpgradeResult(w http.ResponseWriter, r *http.Request) {
	if h.CLIUpgradeStore == nil {
		writeError(w, http.StatusServiceUnavailable, "agent CLI upgrade is not configured")
		return
	}
	runtimeID := chi.URLParam(r, "runtimeId")
	if _, ok := h.requireDaemonRuntimeAccess(w, r, runtimeID); !ok {
		return
	}
	jobID := chi.URLParam(r, "jobId")
	existing, err := h.CLIUpgradeStore.Get(r.Context(), jobID)
	if err != nil {
		slog.Warn("load cli upgrade failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load upgrade job")
		return
	}
	if existing == nil || existing.RuntimeID != runtimeID {
		writeError(w, http.StatusNotFound, "upgrade job not found")
		return
	}
	if cliUpgradeTerminal(existing.Status) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	var req struct {
		Status  string `json:"status"`
		Error   string `json:"error"`
		Current string `json:"current"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	switch req.Status {
	case CLIUpgradeQueued, CLIUpgradeDraining, CLIUpgradeUpgrading, CLIUpgradeSucceeded, CLIUpgradeFailed:
	default:
		writeError(w, http.StatusBadRequest, "invalid status")
		return
	}
	if err := h.CLIUpgradeStore.SetStatus(r.Context(), jobID, req.Status, req.Error, req.Current); err != nil {
		slog.Error("CLIUpgradeStore SetStatus failed", "error", err, "job_id", jobID)
		writeError(w, http.StatusInternalServerError, "failed to persist upgrade status")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// GetRuntimeCLIStatus returns codex, claude, and grok rows for one runtime.
// Readers who can open the runtime may see the rows. Upgrade permission is
// enforced on the upgrade routes, not here.
func (h *Handler) GetRuntimeCLIStatus(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	rt, _, ok := h.requireRuntimeReadAccess(w, r, obsmetrics.RuntimeLookupSourceRuntimeAPI, runtimeID)
	if !ok {
		return
	}
	lookup := h.agentCLILatestLookup()
	clis := agentcli.Build(agentcli.ProbesFromMetadata(rt.Metadata), func(cli string) (string, bool) {
		if lookup == nil {
			return "", false
		}
		return lookup.Version(r.Context(), cli)
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"runtime_id": uuidToString(rt.ID),
		"clis":       clis,
	})
}

func (h *Handler) agentCLILatestLookup() *agentcli.LatestLookup {
	if h != nil && h.AgentCLILatest != nil {
		return h.AgentCLILatest
	}
	return agentcli.DefaultLatestLookup()
}

// persistAgentCLIProbes merges a daemon probe snapshot into runtime metadata.
// An empty snapshot is ignored so an early heartbeat cannot wipe probes the
// server already stored. A write failure is logged and does not fail the
// heartbeat: the next beat retries.
func (h *Handler) persistAgentCLIProbes(ctx context.Context, runtimeID string, probes []agentcli.Probe) {
	if h == nil || h.Queries == nil || len(probes) == 0 {
		return
	}
	normalized := agentcli.NormalizeProbes(probes)
	if len(normalized) == 0 {
		return
	}
	body, err := json.Marshal(normalized)
	if err != nil {
		return
	}
	id, err := util.ParseUUID(runtimeID)
	if err != nil {
		slog.Warn("persist agent cli probes: invalid runtime id", "runtime_id", runtimeID, "error", err)
		return
	}
	if err := h.Queries.SetAgentRuntimeAgentCLIProbes(ctx, db.SetAgentRuntimeAgentCLIProbesParams{
		ID:     id,
		Probes: body,
	}); err != nil {
		slog.Warn("persist agent cli probes failed", "runtime_id", runtimeID, "error", err)
	}
}

func withAgentCLIProbes(metadata []byte, probes []agentcli.Probe) []byte {
	if len(probes) == 0 {
		return metadata
	}
	return agentcli.MetadataWithProbes(metadata, probes)
}
