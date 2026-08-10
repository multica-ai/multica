package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/runtimeaccess"
	"github.com/multica-ai/multica/server/internal/runtimepool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var (
	errRuntimeCapabilityInUse              = errors.New("runtime capability is in use")
	errRuntimeAccessInUse                  = errors.New("runtime access is in use")
	errRuntimeMutationForbidden            = errors.New("runtime mutation forbidden")
	errRuntimeRegistrationChanged          = errors.New("runtime registration changed concurrently")
	errRuntimeRegistrationOwnerUnavailable = errors.New("runtime registration owner is no longer a member")
)

type runtimeRegistrationMutation struct {
	WorkspaceID          pgtype.UUID
	DaemonID             pgtype.Text
	Name                 string
	RuntimeMode          string
	Provider             string
	Status               string
	DeviceInfo           string
	Metadata             []byte
	OwnerID              pgtype.UUID
	ProfileID            pgtype.UUID
	Capabilities         *[]string
	PreserveCapabilities bool
	ResolveProfileFields func(db.RuntimeProfile) runtimeProfileRegistrationFields
}

type runtimeProfileRegistrationFields struct {
	Name     string
	Metadata []byte
}

type runtimeRegistrationMutationResult struct {
	Runtime  db.AgentRuntime
	Inserted bool
}

func normalizeRegisteredCapabilityPresence(advertised *[]string) (*[]string, error) {
	if advertised == nil {
		return nil, nil
	}
	normalized, err := runtimepool.NormalizeAdvertisedCapabilities(*advertised)
	if err != nil {
		return nil, err
	}
	return &normalized, nil
}

func effectiveRegisteredCapabilities(provider string, advertised *[]string) ([]string, error) {
	if advertised != nil {
		return runtimepool.NormalizeAdvertisedCapabilities(*advertised)
	}
	if provider == "platform-agent-cli" {
		return []string{runtimepool.CapabilityExtensionExecuteV1}, nil
	}
	return []string{}, nil
}

func (h *Handler) registerRuntimeMutation(ctx context.Context, mutation runtimeRegistrationMutation) (runtimeRegistrationMutationResult, error) {
	for attempt := 0; attempt < 3; attempt++ {
		result, retry, err := h.registerRuntimeMutationAttempt(ctx, mutation)
		if err != nil {
			return runtimeRegistrationMutationResult{}, err
		}
		if !retry {
			return result, nil
		}
	}
	return runtimeRegistrationMutationResult{}, errRuntimeRegistrationChanged
}

func (h *Handler) registerRuntimeMutationAttempt(ctx context.Context, mutation runtimeRegistrationMutation) (runtimeRegistrationMutationResult, bool, error) {
	var result runtimeRegistrationMutationResult
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return result, false, fmt.Errorf("begin runtime registration: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)
	if err := qtx.LockRuntimeOwnerWrites(ctx, mutation.WorkspaceID); err != nil {
		return result, false, fmt.Errorf("lock Runtime owner writes: %w", err)
	}
	if mutation.OwnerID.Valid {
		// PAT/JWT registration may write owner_id. Validate and lock that Member
		// before Profile/Runtime locks. Daemon-token registration has no proposed
		// owner and is serialized by the Workspace advisory barrier above.
		if _, err := qtx.LockPoolPlacementMember(ctx, db.LockPoolPlacementMemberParams{
			WorkspaceID:     mutation.WorkspaceID,
			RequesterUserID: mutation.OwnerID,
		}); errors.Is(err, pgx.ErrNoRows) {
			return result, false, errRuntimeRegistrationOwnerUnavailable
		} else if err != nil {
			return result, false, fmt.Errorf("lock Runtime owner Member: %w", err)
		}
	}

	provider := mutation.Provider
	if mutation.ProfileID.Valid {
		profile, err := qtx.LockRuntimeProfileForRegistration(ctx, db.LockRuntimeProfileForRegistrationParams{
			ID:          mutation.ProfileID,
			WorkspaceID: mutation.WorkspaceID,
		})
		if err != nil {
			return result, false, fmt.Errorf("lock runtime profile: %w", err)
		}
		if !profile.Enabled {
			return result, false, errRuntimeProfileDisabled
		}
		provider = profile.ProtocolFamily
		if mutation.ResolveProfileFields != nil {
			resolved := mutation.ResolveProfileFields(profile)
			mutation.Name = resolved.Name
			mutation.Metadata = resolved.Metadata
		}
	}

	var existingID pgtype.UUID
	if mutation.ProfileID.Valid {
		existingID, err = qtx.FindAgentRuntimeIDForProfileRegistration(ctx, db.FindAgentRuntimeIDForProfileRegistrationParams{
			WorkspaceID: mutation.WorkspaceID,
			DaemonID:    mutation.DaemonID,
			ProfileID:   mutation.ProfileID,
		})
	} else {
		existingID, err = qtx.FindAgentRuntimeIDForBuiltinRegistration(ctx, db.FindAgentRuntimeIDForBuiltinRegistrationParams{
			WorkspaceID: mutation.WorkspaceID,
			DaemonID:    mutation.DaemonID,
			Provider:    provider,
		})
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return result, false, fmt.Errorf("find registered runtime: %w", err)
	}

	var current db.AgentRuntime
	if err == nil {
		current, err = qtx.LockRuntimeForCapabilityRegistration(ctx, existingID)
		if errors.Is(err, pgx.ErrNoRows) {
			return result, true, nil
		}
		if err != nil {
			return result, false, fmt.Errorf("lock registered runtime: %w", err)
		}
		if !mutation.OwnerID.Valid && current.OwnerID.Valid {
			// A daemon-token request authenticated before revocation may resume
			// only after the Workspace barrier. Re-read the preserved owner's
			// membership now; otherwise COALESCE would revive the removed owner's
			// offline Runtime as online.
			if _, err := qtx.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
				UserID:      current.OwnerID,
				WorkspaceID: current.WorkspaceID,
			}); errors.Is(err, pgx.ErrNoRows) {
				return result, false, errRuntimeRegistrationOwnerUnavailable
			} else if err != nil {
				return result, false, fmt.Errorf("revalidate preserved Runtime owner: %w", err)
			}
		}
	}

	capabilities := []string{}
	if mutation.PreserveCapabilities {
		if current.ID.Valid {
			capabilities = append(capabilities, current.Capabilities...)
		}
	} else {
		capabilities, err = effectiveRegisteredCapabilities(provider, mutation.Capabilities)
		if err != nil {
			return result, false, err
		}
	}

	desired := current
	desired.WorkspaceID = mutation.WorkspaceID
	desired.DaemonID = mutation.DaemonID
	desired.Provider = provider
	desired.Status = mutation.Status
	desired.Capabilities = capabilities
	if mutation.OwnerID.Valid || !current.ID.Valid {
		desired.OwnerID = mutation.OwnerID
	}
	if !current.ID.Valid {
		desired.Visibility = "private"
	}

	shouldWake := !current.ID.Valid && desired.Status == "online"
	var requeued []db.AgentTaskQueue
	if current.ID.Valid {
		var mutationWake bool
		requeued, mutationWake, err = applyRuntimeRoutingMutation(ctx, qtx, current, desired)
		if err != nil {
			return result, false, err
		}
		shouldWake = mutationWake || len(requeued) > 0
	}

	if mutation.ProfileID.Valid {
		row, err := qtx.UpsertAgentRuntimeWithProfile(ctx, db.UpsertAgentRuntimeWithProfileParams{
			WorkspaceID:  mutation.WorkspaceID,
			DaemonID:     mutation.DaemonID,
			Name:         mutation.Name,
			RuntimeMode:  mutation.RuntimeMode,
			Provider:     provider,
			Status:       mutation.Status,
			DeviceInfo:   mutation.DeviceInfo,
			Metadata:     mutation.Metadata,
			OwnerID:      desired.OwnerID,
			ProfileID:    mutation.ProfileID,
			Capabilities: capabilities,
		})
		if err != nil {
			return result, false, fmt.Errorf("upsert profile runtime: %w", err)
		}
		if !current.ID.Valid && !row.Inserted {
			return result, true, nil
		}
		result.Inserted = row.Inserted
		result.Runtime = db.AgentRuntime{
			ID: row.ID, WorkspaceID: row.WorkspaceID, DaemonID: row.DaemonID,
			Name: row.Name, CustomName: row.CustomName, RuntimeMode: row.RuntimeMode,
			Provider: row.Provider, Status: row.Status, DeviceInfo: row.DeviceInfo,
			Metadata: row.Metadata, LastSeenAt: row.LastSeenAt, CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt, OwnerID: row.OwnerID, LegacyDaemonID: row.LegacyDaemonID,
			Visibility: row.Visibility, ProfileID: row.ProfileID, Capabilities: row.Capabilities,
		}
	} else {
		row, err := qtx.UpsertAgentRuntime(ctx, db.UpsertAgentRuntimeParams{
			WorkspaceID:  mutation.WorkspaceID,
			DaemonID:     mutation.DaemonID,
			Name:         mutation.Name,
			RuntimeMode:  mutation.RuntimeMode,
			Provider:     provider,
			Status:       mutation.Status,
			DeviceInfo:   mutation.DeviceInfo,
			Metadata:     mutation.Metadata,
			OwnerID:      desired.OwnerID,
			Capabilities: capabilities,
		})
		if err != nil {
			return result, false, fmt.Errorf("upsert runtime: %w", err)
		}
		if !current.ID.Valid && !row.Inserted {
			return result, true, nil
		}
		result.Inserted = row.Inserted
		result.Runtime = db.AgentRuntime{
			ID: row.ID, WorkspaceID: row.WorkspaceID, DaemonID: row.DaemonID,
			Name: row.Name, CustomName: row.CustomName, RuntimeMode: row.RuntimeMode,
			Provider: row.Provider, Status: row.Status, DeviceInfo: row.DeviceInfo,
			Metadata: row.Metadata, LastSeenAt: row.LastSeenAt, CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt, OwnerID: row.OwnerID, LegacyDaemonID: row.LegacyDaemonID,
			Visibility: row.Visibility, ProfileID: row.ProfileID, Capabilities: row.Capabilities,
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return result, false, fmt.Errorf("commit runtime registration: %w", err)
	}
	if shouldWake {
		h.publishRuntimeRoutingMutation(ctx, result.Runtime.WorkspaceID, requeued)
	}
	return result, false, nil
}

type invalidRuntimeDependent struct {
	task              db.AgentTaskQueue
	capabilityInvalid bool
	accessInvalid     bool
}

func applyRuntimeRoutingMutation(ctx context.Context, qtx *db.Queries, current, desired db.AgentRuntime) ([]db.AgentTaskQueue, bool, error) {
	capabilitiesChanged := !slices.Equal(current.Capabilities, desired.Capabilities)
	accessChanged := current.OwnerID != desired.OwnerID || current.Visibility != desired.Visibility
	shouldWake := (!runtimepool.ContainsAllCapabilities(current.Capabilities, desired.Capabilities)) ||
		(current.Visibility != "public" && desired.Visibility == "public") ||
		current.OwnerID != desired.OwnerID ||
		(current.Status != "online" && desired.Status == "online")
	if !capabilitiesChanged && !accessChanged {
		return nil, shouldWake, nil
	}

	dependentIDs, err := qtx.ListPoolCapabilityDependentIDs(ctx, current.ID)
	if err != nil {
		return nil, false, fmt.Errorf("list runtime Pool dependents: %w", err)
	}
	agentIDs := make([]pgtype.UUID, 0, len(dependentIDs))
	taskIDs := make([]pgtype.UUID, 0, len(dependentIDs))
	seenAgents := make(map[pgtype.UUID]struct{}, len(dependentIDs))
	for _, dependent := range dependentIDs {
		taskIDs = append(taskIDs, dependent.TaskID)
		if _, ok := seenAgents[dependent.AgentID]; !ok {
			seenAgents[dependent.AgentID] = struct{}{}
			agentIDs = append(agentIDs, dependent.AgentID)
		}
	}
	sort.Slice(agentIDs, func(i, j int) bool {
		return bytes.Compare(agentIDs[i].Bytes[:], agentIDs[j].Bytes[:]) < 0
	})
	sort.Slice(taskIDs, func(i, j int) bool {
		return bytes.Compare(taskIDs[i].Bytes[:], taskIDs[j].Bytes[:]) < 0
	})
	lockedAgents, err := qtx.LockPoolCapabilityDependentAgents(ctx, agentIDs)
	if err != nil {
		return nil, false, fmt.Errorf("lock runtime dependent Agents: %w", err)
	}
	if len(lockedAgents) != len(agentIDs) {
		return nil, false, errors.New("runtime dependent Agent set changed")
	}
	lockedTasks, err := qtx.LockPoolCapabilityDependents(ctx, taskIDs)
	if err != nil {
		return nil, false, fmt.Errorf("lock runtime dependent Tasks: %w", err)
	}
	if len(lockedTasks) != len(taskIDs) {
		return nil, false, errors.New("runtime dependent Task set changed")
	}

	invalid := make([]invalidRuntimeDependent, 0, len(lockedTasks))
	for _, task := range lockedTasks {
		if !runtimePoolTaskNonterminal(task.Status) {
			continue
		}
		capabilityInvalid := false
		if capabilitiesChanged {
			requirements, parseErr := runtimepool.ParseRequirements(task.RuntimeRequirements)
			currentCapable := parseErr == nil && runtimepool.ContainsAllCapabilities(current.Capabilities, requirements.CapabilitiesAll)
			desiredCapable := parseErr == nil && runtimepool.ContainsAllCapabilities(desired.Capabilities, requirements.CapabilitiesAll)
			capabilityInvalid = currentCapable && !desiredCapable
		}

		accessInvalid := false
		if accessChanged {
			member, memberErr := qtx.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
				UserID:      task.RuntimeRequesterUserID,
				WorkspaceID: task.PlacementWorkspaceID,
			})
			if memberErr != nil && !errors.Is(memberErr, pgx.ErrNoRows) {
				return nil, false, fmt.Errorf("load runtime requester membership: %w", memberErr)
			}
			currentAllowed := memberErr == nil && runtimeaccess.CanUse(member, current)
			desiredAllowed := memberErr == nil && runtimeaccess.CanUse(member, desired)
			accessInvalid = currentAllowed && !desiredAllowed
		}
		if capabilityInvalid || accessInvalid {
			invalid = append(invalid, invalidRuntimeDependent{
				task:              task,
				capabilityInvalid: capabilityInvalid,
				accessInvalid:     accessInvalid,
			})
		}
	}

	var capabilityInUse, accessInUse bool
	for _, dependent := range invalid {
		if !runtimePoolTaskInFlight(dependent.task.Status) {
			continue
		}
		capabilityInUse = capabilityInUse || dependent.capabilityInvalid
		accessInUse = accessInUse || dependent.accessInvalid
	}
	if capabilityInUse {
		return nil, false, errRuntimeCapabilityInUse
	}
	if accessInUse {
		return nil, false, errRuntimeAccessInUse
	}

	requeued := make([]db.AgentTaskQueue, 0, len(invalid))
	for _, dependent := range invalid {
		reason := "no_eligible_runtime"
		if dependent.task.SessionAffinityState == runtimepool.SessionAffinityPinned {
			if dependent.capabilityInvalid {
				reason = "session_runtime_capability_mismatch"
			} else {
				reason = "session_runtime_unauthorized"
			}
		}
		params := db.RequeuePoolTaskAfterCapabilityDowngradeParams{
			TaskID: dependent.task.ID,
			Reason: pgtype.Text{String: reason, Valid: true},
		}
		switch dependent.task.Status {
		case "queued":
			updated, err := qtx.RequeuePoolTaskAfterCapabilityDowngrade(ctx, params)
			if err != nil {
				return nil, false, fmt.Errorf("requeue invalid Runtime Pool Task: %w", err)
			}
			requeued = append(requeued, updated)
		case runtimepool.StatusWaitingRuntime, "deferred":
			if dependent.task.SessionAffinityState != runtimepool.SessionAffinityPinned {
				continue
			}
			if _, err := qtx.UpdatePinnedPoolTaskWaitReason(ctx, db.UpdatePinnedPoolTaskWaitReasonParams{
				TaskID: dependent.task.ID,
				Reason: pgtype.Text{String: reason, Valid: true},
			}); err != nil {
				return nil, false, fmt.Errorf("update pinned Runtime Pool wait reason: %w", err)
			}
		}
	}
	return requeued, shouldWake, nil
}

func runtimePoolTaskNonterminal(status string) bool {
	switch status {
	case runtimepool.StatusWaitingRuntime, "queued", "deferred", "dispatched", "running", "waiting_local_directory":
		return true
	default:
		return false
	}
}

func runtimePoolTaskInFlight(status string) bool {
	return status == "dispatched" || status == "running" || status == "waiting_local_directory"
}

func (h *Handler) updateRuntimeVisibilitySafely(ctx context.Context, runtimeID pgtype.UUID, visibility string, actorUserID pgtype.UUID) (db.AgentRuntime, error) {
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return db.AgentRuntime{}, fmt.Errorf("begin Runtime visibility update: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)
	current, err := qtx.LockRuntimeForCapabilityRegistration(ctx, runtimeID)
	if err != nil {
		return db.AgentRuntime{}, err
	}
	member, err := qtx.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      actorUserID,
		WorkspaceID: current.WorkspaceID,
	})
	if err != nil || !canEditRuntime(member, current) {
		return db.AgentRuntime{}, errRuntimeMutationForbidden
	}
	desired := current
	desired.Visibility = visibility
	requeued, shouldWake, err := applyRuntimeRoutingMutation(ctx, qtx, current, desired)
	if err != nil {
		return db.AgentRuntime{}, err
	}
	updated, err := qtx.UpdateAgentRuntimeVisibility(ctx, db.UpdateAgentRuntimeVisibilityParams{
		ID:         runtimeID,
		Visibility: visibility,
	})
	if err != nil {
		return db.AgentRuntime{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return db.AgentRuntime{}, err
	}
	if shouldWake || len(requeued) > 0 {
		h.publishRuntimeRoutingMutation(ctx, updated.WorkspaceID, requeued)
	}
	return updated, nil
}

// publishRuntimeRoutingMutation runs the single Workspace allocator wake for a
// committed Runtime mutation, then makes every requeued Task observable in its
// persisted post-allocation state. Assigned Tasks were already published as
// queued by the allocator; only rows that remain waiting need this event.
func (h *Handler) publishRuntimeRoutingMutation(ctx context.Context, workspaceID pgtype.UUID, requeued []db.AgentTaskQueue) {
	h.wakeRuntimePoolWorkspaceBestEffort(ctx, workspaceID)
	if h.TaskService == nil {
		return
	}
	for _, task := range requeued {
		persisted, err := h.Queries.GetAgentTask(ctx, task.ID)
		if err != nil {
			slog.Warn("reload requeued Pool Task after Runtime mutation",
				"task_id", uuidToString(task.ID), "error", err)
			continue
		}
		if persisted.RuntimeBindingMode == "pool" && persisted.Status == runtimepool.StatusWaitingRuntime {
			h.TaskService.BroadcastTaskWaitingRuntime(ctx, persisted)
		}
	}
}

func (h *Handler) wakeRuntimePoolWorkspaceBestEffort(ctx context.Context, workspaceID pgtype.UUID) {
	if h.TaskService == nil {
		return
	}
	if err := h.TaskService.WakePoolWorkspace(ctx, workspaceID); err != nil {
		slog.Warn("runtime Pool wake failed after committed Runtime mutation",
			"workspace_id", uuidToString(workspaceID), "error", err)
	}
}

func writeRuntimeMutationConflict(w http.ResponseWriter, err error) bool {
	var code, message string
	switch {
	case errors.Is(err, errRuntimeCapabilityInUse):
		code = "RUNTIME_CAPABILITY_IN_USE"
		message = "runtime capability is still required by in-flight tasks"
	case errors.Is(err, errRuntimeAccessInUse):
		code = "RUNTIME_ACCESS_IN_USE"
		message = "runtime access is still required by in-flight tasks"
	default:
		return false
	}
	writeJSON(w, http.StatusConflict, map[string]any{"error": message, "code": code})
	return true
}

// ---------------------------------------------------------------------------
// CLI update request store
// ---------------------------------------------------------------------------

type UpdateStatus string

const (
	UpdatePending   UpdateStatus = "pending"
	UpdateRunning   UpdateStatus = "running"
	UpdateCompleted UpdateStatus = "completed"
	UpdateFailed    UpdateStatus = "failed"
	UpdateTimeout   UpdateStatus = "timeout"
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
	CreatedAt       time.Time    `json:"created_at"`
	UpdatedAt       time.Time    `json:"updated_at"`
	RunStartedAt    *time.Time   `json:"-"`
}

const (
	updatePendingTimeout = 120 * time.Second
	updateRunningTimeout = 150 * time.Second
	updateStoreRetention = 5 * time.Minute
)

type UpdateStore interface {
	Create(ctx context.Context, runtimeID, targetVersion, initiatorUserID string) (*UpdateRequest, error)
	Get(ctx context.Context, id string) (*UpdateRequest, error)
	HasPending(ctx context.Context, runtimeID string) (bool, error)
	PopPending(ctx context.Context, runtimeID string) (*UpdateRequest, error)
	Complete(ctx context.Context, id string, output string) error
	Fail(ctx context.Context, id string, errMsg string) error
}

func updateRequestTerminal(status UpdateStatus) bool {
	return status == UpdateCompleted || status == UpdateFailed || status == UpdateTimeout
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

func (s *InMemoryUpdateStore) Create(_ context.Context, runtimeID, targetVersion, initiatorUserID string) (*UpdateRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Clean up old requests.
	for id, req := range s.requests {
		if time.Since(req.CreatedAt) > updateStoreRetention {
			delete(s.requests, id)
		}
	}

	// Reject if there is already a pending or running update for this runtime.
	for _, req := range s.requests {
		if req.RuntimeID == runtimeID && (req.Status == UpdatePending || req.Status == UpdateRunning) {
			return nil, errUpdateInProgress
		}
	}

	req := &UpdateRequest{
		ID:              randomID(),
		RuntimeID:       runtimeID,
		InitiatorUserID: initiatorUserID,
		Status:          UpdatePending,
		TargetVersion:   targetVersion,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	s.requests[req.ID] = req
	return req, nil
}

var errUpdateInProgress = &updateError{msg: "an update is already in progress for this runtime"}

type updateError struct{ msg string }

func (e *updateError) Error() string { return e.msg }

func (s *InMemoryUpdateStore) Get(_ context.Context, id string) (*UpdateRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	req, ok := s.requests[id]
	if !ok {
		return nil, nil
	}
	applyUpdateTimeout(req, time.Now())
	return req, nil
}

func (s *InMemoryUpdateStore) HasPending(_ context.Context, runtimeID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for _, req := range s.requests {
		applyUpdateTimeout(req, now)
		if req.RuntimeID == runtimeID && req.Status == UpdatePending {
			return true, nil
		}
	}
	return false, nil
}

// PopPending returns and marks as running the pending update for a runtime.
func (s *InMemoryUpdateStore) PopPending(_ context.Context, runtimeID string) (*UpdateRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var oldest *UpdateRequest
	now := time.Now()
	for _, req := range s.requests {
		applyUpdateTimeout(req, now)
		if req.RuntimeID == runtimeID && req.Status == UpdatePending {
			if oldest == nil || req.CreatedAt.Before(oldest.CreatedAt) {
				oldest = req
			}
		}
	}
	if oldest != nil {
		oldest.Status = UpdateRunning
		startedAt := now
		oldest.RunStartedAt = &startedAt
		oldest.UpdatedAt = now
	}
	return oldest, nil
}

func (s *InMemoryUpdateStore) Complete(_ context.Context, id string, output string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if req, ok := s.requests[id]; ok {
		req.Status = UpdateCompleted
		req.Output = output
		req.UpdatedAt = time.Now()
	}
	return nil
}

func (s *InMemoryUpdateStore) Fail(_ context.Context, id string, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if req, ok := s.requests[id]; ok {
		req.Status = UpdateFailed
		req.Error = errMsg
		req.UpdatedAt = time.Now()
	}
	return nil
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// InitiateUpdate creates a new CLI update request (protected route, called by frontend).
func (h *Handler) InitiateUpdate(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	runtimeUUID, ok := parseUUIDOrBadRequest(w, runtimeID, "runtime_id")
	if !ok {
		return
	}

	rt, err := h.Queries.GetAgentRuntime(r.Context(), runtimeUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "runtime not found")
		return
	}

	member, ok := h.requireWorkspaceMember(w, r, uuidToString(rt.WorkspaceID), "runtime not found")
	if !ok {
		return
	}
	if !canEditRuntime(member, rt) {
		writeError(w, http.StatusForbidden, "only runtime owners and workspace admins can update runtimes")
		return
	}

	var req struct {
		TargetVersion string `json:"target_version"`
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
		req.TargetVersion,
		uuidToString(member.UserID),
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
	runtimeUUID, ok := parseUUIDOrBadRequest(w, runtimeID, "runtime_id")
	if !ok {
		return
	}

	rt, err := h.Queries.GetAgentRuntime(r.Context(), runtimeUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "runtime not found")
		return
	}
	member, ok := h.requireWorkspaceMember(w, r, uuidToString(rt.WorkspaceID), "runtime not found")
	if !ok {
		return
	}
	updateID := chi.URLParam(r, "updateId")

	update, err := h.UpdateStore.Get(r.Context(), updateID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load update: "+err.Error())
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
		writeError(w, http.StatusInternalServerError, "failed to load update: "+err.Error())
		return
	}
	if existing == nil || existing.RuntimeID != runtimeID {
		writeError(w, http.StatusNotFound, "update not found")
		return
	}
	if updateRequestTerminal(existing.Status) {
		slog.Debug("ignoring stale update report", "runtime_id", runtimeID, "update_id", updateID, "status", existing.Status)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	var req struct {
		Status string `json:"status"` // "running", "completed", or "failed"
		Output string `json:"output"`
		Error  string `json:"error"`
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
	case "running":
		// No-op: status is already "running" from PopPending. This call is
		// just a progress signal from the daemon to confirm it received the
		// update command and is executing it.
	default:
		writeError(w, http.StatusBadRequest, "invalid status: "+req.Status)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
