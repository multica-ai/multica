package handler

import (
	"context"
	"errors"
	"log/slog"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/runtimeaccess"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// revokeAndRemoveMember converges all server-side state that should follow a
// member leaving a workspace: every runtime they own becomes unusable, every
// agent pinned to one of those runtimes is archived, every in-flight task on
// those runtimes is cancelled (cancelled rather than failed so the daemon's
// per-task status poller interrupts the running agent gracefully), the
// daemon_token rows for those runtimes are deleted, and finally the member row
// itself is removed.
//
// All DB writes run inside a single transaction so a partial revocation never
// leaves the workspace half-converged — e.g. a member who is "gone" but whose
// runtime row is still active. Once the transaction commits, daemon_token
// cache entries are invalidated and events are published (see
// publishRevocation) so connected clients and other workspace members observe
// the new state immediately.
//
// Note on scope: this revokes every runtime whose owner_id matches userID,
// regardless of how the daemon authenticates. Today most daemons fall back to
// PAT/JWT and `daemon_token` rows are unused in production; deleting them is
// a no-op for those daemons but takes effect once the mdt_ flow is live.
// Either way the agent-archive + task-cancel + force-offline writes are the
// actual production safety net: even if the daemon races back online with a
// still-valid PAT, it finds no agent it can run for, no queued task to claim,
// and the dispatcher (which gates on agent.archived_at IS NULL) won't hand it
// new work — and the member-row deletion in the same tx means subsequent
// requireWorkspaceMember checks will reject the daemon's PAT-authenticated
// requests with 404.
//
// archivedBy is the actor who triggered the revocation. For DeleteMember it's
// the requester (the admin doing the kick); for LeaveWorkspace it's the leaver
// themselves.
func (h *Handler) revokeAndRemoveMember(ctx context.Context, workspaceID, userID, memberID, archivedBy pgtype.UUID) (revocationResult, error) {
	return h.mutateMemberRuntimeAccess(ctx, workspaceID, userID, memberID, archivedBy, "", "", true)
}

func (h *Handler) downgradeMemberRuntimeAccess(ctx context.Context, workspaceID, userID, memberID, archivedBy pgtype.UUID, expectedCurrentRole, nextRole string) (revocationResult, error) {
	return h.mutateMemberRuntimeAccess(ctx, workspaceID, userID, memberID, archivedBy, expectedCurrentRole, nextRole, false)
}

var errMemberRevocationSnapshotDrift = errors.New("member revocation snapshot changed")

func (h *Handler) mutateMemberRuntimeAccess(ctx context.Context, workspaceID, userID, memberID, archivedBy pgtype.UUID, expectedCurrentRole, nextRole string, remove bool) (revocationResult, error) {
	var empty revocationResult
	for attempt := 0; attempt < 3; attempt++ {
		result, err := h.mutateMemberRuntimeAccessAttempt(ctx, workspaceID, userID, memberID, archivedBy, expectedCurrentRole, nextRole, remove)
		if !errors.Is(err, errMemberRevocationSnapshotDrift) {
			return result, err
		}
	}
	return empty, errMemberRevocationSnapshotDrift
}

func (h *Handler) mutateMemberRuntimeAccessAttempt(ctx context.Context, workspaceID, userID, memberID, archivedBy pgtype.UUID, expectedCurrentRole, nextRole string, remove bool) (revocationResult, error) {
	var empty revocationResult

	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)

	qtx := h.Queries.WithTx(tx)

	// Taken FIRST, before this tx touches member or issue_subscriber. The
	// delegated auto-subscribe rule takes the same (workspace, user) lock, so
	// a run that is mid-decomposition cannot slip a new subscriber row in
	// between this tx's membership delete and its subscription cleanup below.
	// First also means every holder acquires it in the same order, so these
	// paths cannot deadlock against each other (MUL-5483 review round 7).
	if err := qtx.LockSubscriberWrites(ctx, db.LockSubscriberWritesParams{
		WorkspaceID: workspaceID,
		UserID:      userID,
	}); err != nil {
		return empty, err
	}
	if err := qtx.LockRuntimeOwnerWrites(ctx, workspaceID); err != nil {
		return empty, err
	}
	// Preview the target requester's Pool write set before taking any
	// relational row lock. A cancelled resolved Chat head may expose a tail
	// owned by another member, so every such Member row must join the first
	// lock phase. Including the target in the same UUID-sorted set prevents two
	// concurrent revocations from taking A->B and B->A Member locks.
	previewDependents, err := qtx.ListPoolMemberRevocationDependents(ctx, db.ListPoolMemberRevocationDependentsParams{
		WorkspaceID:     workspaceID,
		RequesterUserID: userID,
	})
	if err != nil {
		return empty, err
	}
	previewChatIDs := memberRevocationChatIDs(previewDependents)
	previewTails, err := qtx.ListPoolChatMemberRevocationTails(ctx, sortedRevocationUUIDs(previewChatIDs))
	if err != nil {
		return empty, err
	}
	memberLockSet := map[string]pgtype.UUID{uuidToString(userID): userID}
	for _, tail := range previewTails {
		addRevocationUUID(memberLockSet, tail.RuntimeRequesterUserID)
	}
	lockedMembersByUser := make(map[string]db.Member, len(memberLockSet))
	var lockedMember db.Member
	for _, requesterUserID := range sortedRevocationUUIDs(memberLockSet) {
		member, lockErr := qtx.LockPoolPlacementMember(ctx, db.LockPoolPlacementMemberParams{
			WorkspaceID:     workspaceID,
			RequesterUserID: requesterUserID,
		})
		if errors.Is(lockErr, pgx.ErrNoRows) && requesterUserID != userID {
			// An already-revoked tail owner is not an eligible successor. Keep
			// its Task locked later, but never pass it to the promotion query.
			continue
		}
		if lockErr != nil {
			return empty, lockErr
		}
		lockedMembersByUser[uuidToString(requesterUserID)] = member
		if requesterUserID == userID {
			lockedMember = member
		}
	}
	if lockedMember.ID != memberID {
		return empty, errMemberRevocationSnapshotDrift
	}
	if !remove && lockedMember.Role != expectedCurrentRole {
		return empty, errMemberRevocationSnapshotDrift
	}

	var runtimes []db.AgentRuntime
	if remove {
		runtimes, err = qtx.ListAgentRuntimesByOwner(ctx, db.ListAgentRuntimesByOwnerParams{
			WorkspaceID: workspaceID,
			OwnerID:     userID,
		})
		if err != nil {
			return empty, err
		}
	}
	poolDependents, err := qtx.ListPoolMemberRevocationDependents(ctx, db.ListPoolMemberRevocationDependentsParams{
		WorkspaceID:     workspaceID,
		RequesterUserID: userID,
	})
	if err != nil {
		return empty, err
	}
	// Re-read after the Member locks. If a newly visible Chat tail belongs to
	// a requester outside the preview, retry the transaction before entering
	// the Runtime phase; acquiring that extra Member now would break the global
	// UUID lock order.
	preRuntimeChatIDs := memberRevocationChatIDs(poolDependents)
	preRuntimeTails, err := qtx.ListPoolChatMemberRevocationTails(ctx, sortedRevocationUUIDs(preRuntimeChatIDs))
	if err != nil {
		return empty, err
	}
	if memberRevocationHasUnlockedTailRequester(preRuntimeTails, memberLockSet) {
		return empty, errMemberRevocationSnapshotDrift
	}

	// Member is the first relational row lock. Derive every subsequent lock
	// set from that protected snapshot and take each entity class in UUID order.
	ownedRuntimeIDs := make(map[string]pgtype.UUID, len(runtimes))
	runtimeLockSet := make(map[string]pgtype.UUID, len(runtimes)+len(poolDependents))
	for _, runtime := range runtimes {
		addRevocationUUID(ownedRuntimeIDs, runtime.ID)
		addRevocationUUID(runtimeLockSet, runtime.ID)
	}
	for _, dependent := range poolDependents {
		addRevocationUUID(runtimeLockSet, dependent.EffectiveRuntimeID)
	}

	lockedRuntimeByID := make(map[string]db.AgentRuntime, len(runtimeLockSet))
	result := revocationResult{}
	for _, runtimeID := range sortedRevocationUUIDs(runtimeLockSet) {
		lockedRuntime, lockErr := qtx.LockAgentRuntime(ctx, runtimeID)
		if errors.Is(lockErr, pgx.ErrNoRows) {
			if _, owned := ownedRuntimeIDs[uuidToString(runtimeID)]; owned {
				return empty, errMemberRevocationSnapshotDrift
			}
			continue
		}
		if lockErr != nil {
			return empty, lockErr
		}
		key := uuidToString(lockedRuntime.ID)
		lockedRuntimeByID[key] = lockedRuntime
		if _, owned := ownedRuntimeIDs[key]; owned {
			if lockedRuntime.WorkspaceID != workspaceID || lockedRuntime.OwnerID != userID {
				return empty, errMemberRevocationSnapshotDrift
			}
			result.Runtimes = append(result.Runtimes, lockedRuntime)
		}
	}
	if remove {
		lockedOwnerSnapshot, err := qtx.ListAgentRuntimesByOwner(ctx, db.ListAgentRuntimesByOwnerParams{
			WorkspaceID: workspaceID,
			OwnerID:     userID,
		})
		if err != nil {
			return empty, err
		}
		if len(lockedOwnerSnapshot) != len(ownedRuntimeIDs) {
			return empty, errMemberRevocationSnapshotDrift
		}
		for _, runtime := range lockedOwnerSnapshot {
			if _, expected := ownedRuntimeIDs[uuidToString(runtime.ID)]; !expected {
				return empty, errMemberRevocationSnapshotDrift
			}
		}
	}

	chatLockSet := memberRevocationChatIDs(poolDependents)
	lockedChatByID := make(map[string]db.ChatSession, len(chatLockSet))
	lockedChatIDSet := make(map[string]pgtype.UUID, len(chatLockSet))
	for _, chatSessionID := range sortedRevocationUUIDs(chatLockSet) {
		lockedChat, lockErr := qtx.LockPoolChatSessionForPlacement(ctx, chatSessionID)
		if errors.Is(lockErr, pgx.ErrNoRows) {
			continue
		}
		if lockErr != nil {
			return empty, lockErr
		}
		lockedChatByID[uuidToString(lockedChat.ID)] = lockedChat
		addRevocationUUID(lockedChatIDSet, lockedChat.ID)
	}
	chatTails, err := qtx.ListPoolChatMemberRevocationTails(ctx, sortedRevocationUUIDs(lockedChatIDSet))
	if err != nil {
		return empty, err
	}
	if memberRevocationHasUnlockedTailRequester(chatTails, memberLockSet) {
		return empty, errMemberRevocationSnapshotDrift
	}

	lockedOwnedRuntimeIDs := make([]pgtype.UUID, 0, len(result.Runtimes))
	for _, runtime := range result.Runtimes {
		lockedOwnedRuntimeIDs = append(lockedOwnedRuntimeIDs, runtime.ID)
	}
	ownedAgentIDs, err := qtx.ListMemberRevocationAgentIDs(ctx, lockedOwnedRuntimeIDs)
	if err != nil {
		return empty, err
	}
	agentLockSet := make(map[string]pgtype.UUID, len(ownedAgentIDs)+len(poolDependents))
	for _, agentID := range ownedAgentIDs {
		addRevocationUUID(agentLockSet, agentID)
	}
	for _, dependent := range poolDependents {
		addRevocationUUID(agentLockSet, dependent.AgentID)
	}
	for _, tail := range chatTails {
		addRevocationUUID(agentLockSet, tail.AgentID)
	}
	if _, err := qtx.LockPoolCapabilityDependentAgents(ctx, sortedRevocationUUIDs(agentLockSet)); err != nil {
		return empty, err
	}

	legacyTaskIDs, err := qtx.ListMemberRevocationLegacyTaskIDs(ctx, db.ListMemberRevocationLegacyTaskIDsParams{
		RuntimeIds: lockedOwnedRuntimeIDs,
		AgentIds:   ownedAgentIDs,
	})
	if err != nil {
		return empty, err
	}
	taskLockSet := make(map[string]pgtype.UUID, len(legacyTaskIDs)+len(poolDependents))
	for _, taskID := range legacyTaskIDs {
		addRevocationUUID(taskLockSet, taskID)
	}
	for _, dependent := range poolDependents {
		addRevocationUUID(taskLockSet, dependent.TaskID)
	}
	chatTailSet := make(map[string]pgtype.UUID, len(chatTails))
	chatTailsBySession := make(map[string][]pgtype.UUID, len(lockedChatByID))
	for _, tail := range chatTails {
		addRevocationUUID(taskLockSet, tail.TaskID)
		addRevocationUUID(chatTailSet, tail.TaskID)
		if _, requesterLocked := lockedMembersByUser[uuidToString(tail.RuntimeRequesterUserID)]; !requesterLocked {
			continue
		}
		chatKey := uuidToString(tail.ChatSessionID)
		chatTailsBySession[chatKey] = append(chatTailsBySession[chatKey], tail.TaskID)
	}
	lockedTaskIDs := sortedRevocationUUIDs(taskLockSet)
	lockedTasks, err := qtx.LockPoolCapabilityDependents(ctx, lockedTaskIDs)
	if err != nil {
		return empty, err
	}
	cancelSet := make(map[string]pgtype.UUID, len(lockedTasks))
	if remove {
		cancelTargets := make(map[string]pgtype.UUID, len(legacyTaskIDs)+len(poolDependents))
		for _, taskID := range legacyTaskIDs {
			addRevocationUUID(cancelTargets, taskID)
		}
		for _, dependent := range poolDependents {
			addRevocationUUID(cancelTargets, dependent.TaskID)
		}
		for _, task := range lockedTasks {
			if _, target := cancelTargets[uuidToString(task.ID)]; target {
				addRevocationUUID(cancelSet, task.ID)
			}
		}
	} else {
		desiredMember := lockedMember
		desiredMember.Role = nextRole
		for _, task := range lockedTasks {
			if task.RuntimeBindingMode != "pool" ||
				task.PlacementWorkspaceID != workspaceID ||
				task.RuntimeRequesterUserID != userID ||
				!isMemberRevocationNonterminal(task.Status) {
				continue
			}
			effectiveRuntimeID := memberRevocationEffectiveRuntime(task, lockedChatByID)
			if !effectiveRuntimeID.Valid {
				// Fresh/unassigned Pool work remains valid for a plain member;
				// the allocator will choose only public or member-owned Runtime.
				continue
			}
			lockedRuntime, exists := lockedRuntimeByID[uuidToString(effectiveRuntimeID)]
			if !exists || !runtimeaccess.CanUse(desiredMember, lockedRuntime) {
				addRevocationUUID(cancelSet, task.ID)
			}
		}
	}
	result.CancelledTasks, err = qtx.CancelMemberRevocationTasksByIDs(ctx, sortedRevocationUUIDs(cancelSet))
	if err != nil {
		return empty, err
	}
	resolvedCancelledChats := make(map[string]pgtype.UUID)
	for _, cancelled := range result.CancelledTasks {
		if cancelled.RuntimeBindingMode != "pool" || !cancelled.ChatSessionID.Valid {
			continue
		}
		if _, wasUnresolved := chatTailSet[uuidToString(cancelled.ID)]; wasUnresolved {
			continue
		}
		addRevocationUUID(resolvedCancelledChats, cancelled.ChatSessionID)
	}
	for _, chatSessionID := range sortedRevocationUUIDs(resolvedCancelledChats) {
		candidateIDs := chatTailsBySession[uuidToString(chatSessionID)]
		if len(candidateIDs) == 0 {
			continue
		}
		promoted, promoteErr := qtx.PromoteNextAuthorizedPoolChatTaskAfterMemberRevocation(ctx, db.PromoteNextAuthorizedPoolChatTaskAfterMemberRevocationParams{
			CandidateTaskIds: candidateIDs,
			ChatSessionID:    chatSessionID,
		})
		if errors.Is(promoteErr, pgx.ErrNoRows) {
			continue
		}
		if promoteErr != nil {
			return empty, promoteErr
		}
		result.PromotedTasks = append(result.PromotedTasks, promoted)
	}

	if len(lockedOwnedRuntimeIDs) > 0 {
		result.ArchivedAgents, err = qtx.ArchiveAgentsByRuntime(ctx, db.ArchiveAgentsByRuntimeParams{
			ArchivedBy: archivedBy,
			RuntimeIds: lockedOwnedRuntimeIDs,
		})
		if err != nil {
			return empty, err
		}
		result.OfflineRuntimeIDs, err = qtx.ForceOfflineRuntimesByIDs(ctx, lockedOwnedRuntimeIDs)
		if err != nil {
			return empty, err
		}
		daemonIDs := make([]string, 0, len(result.Runtimes))
		for _, runtime := range result.Runtimes {
			if runtime.DaemonID.Valid && runtime.DaemonID.String != "" {
				daemonIDs = append(daemonIDs, runtime.DaemonID.String)
			}
		}
		if len(daemonIDs) > 0 {
			result.RevokedTokenHashes, err = qtx.DeleteDaemonTokensByWorkspaceAndDaemons(ctx, db.DeleteDaemonTokensByWorkspaceAndDaemonsParams{
				WorkspaceID: workspaceID,
				DaemonIds:   daemonIDs,
			})
			if err != nil {
				return empty, err
			}
		}
	}

	if !remove {
		result.UpdatedMember, err = qtx.UpdateMemberRole(ctx, db.UpdateMemberRoleParams{
			ID:                  memberID,
			Role:                nextRole,
			ExpectedCurrentRole: expectedCurrentRole,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return empty, errMemberRevocationSnapshotDrift
		}
		if err != nil {
			return empty, err
		}
		if err := tx.Commit(ctx); err != nil {
			return empty, err
		}
		return result, nil
	}

	// channel_user_binding used to carry a member FK with ON DELETE CASCADE, so
	// a removed member's IM bindings vanished automatically. MUL-3515 §4 dropped
	// every channel_* foreign key, moving that integrity rule to the application
	// layer: prune the bindings here, in the same tx as the member-row delete.
	// The inbound path also re-checks membership (see ChannelStore.IsWorkspaceMember),
	// but pruning stops a stale binding from lingering across a remove/re-add.
	if err := qtx.DeleteChannelUserBindingsByWorkspaceMember(ctx, db.DeleteChannelUserBindingsByWorkspaceMemberParams{
		WorkspaceID:   workspaceID,
		MulticaUserID: userID,
	}); err != nil {
		return empty, err
	}

	// agent_invocation_target carries member-target grants with NO database FK
	// (MUL-3963 keeps the new table FK-free, matching the MUL-3515 channel
	// generalization). Prune this leaving member's grants in the same tx as the
	// member-row delete so a re-invited user does not silently reclaim old
	// invocation permission on agents that had allow-listed them. SCOPED to
	// this workspace: the same user may belong to other workspaces, and
	// removing them here must not touch their grants on agents elsewhere.
	if err := qtx.DeleteAgentInvocationTargetsByMember(ctx, db.DeleteAgentInvocationTargetsByMemberParams{
		WorkspaceID: workspaceID,
		TargetID:    userID,
	}); err != nil {
		return empty, err
	}

	// A private quick action is visible and runnable ONLY by its creator
	// (quick_action carries no FK, so nothing removes it implicitly). Once the
	// creator is gone the row is unreachable by every remaining member while
	// still consuming the workspace's active-action limit, so drop those in
	// the same tx. Public actions are workspace furniture and survive.
	if err := qtx.DeletePrivateQuickActionsByCreator(ctx, db.DeletePrivateQuickActionsByCreatorParams{
		WorkspaceID: workspaceID,
		CreatedByID: userID,
	}); err != nil {
		return empty, err
	}

	// issue_subscriber carries no FK either (same MUL-3515 rule as the two
	// prunes above), and MUL-5483 gave agents a path that writes member
	// subscriber rows on their own initiative. Dropping them in this tx is what
	// stops a departed member from accruing inbox rows, and stops a re-invite
	// from silently restoring visibility of everything they used to watch.
	if err := qtx.DeleteSubscriptionsByMember(ctx, db.DeleteSubscriptionsByMemberParams{
		WorkspaceID: workspaceID,
		UserID:      userID,
	}); err != nil {
		return empty, err
	}

	// Member row deletion lives inside the same tx so a successful revoke is
	// never followed by a failed member-delete (which would leave the user
	// still a member with a dead runtime), and a failed revoke never leaves
	// the user out of the workspace with a still-online runtime.
	if err := qtx.DeleteMember(ctx, memberID); err != nil {
		return empty, err
	}

	if err := tx.Commit(ctx); err != nil {
		return empty, err
	}

	return result, nil
}

func addRevocationUUID(set map[string]pgtype.UUID, id pgtype.UUID) {
	if !id.Valid {
		return
	}
	set[uuidToString(id)] = id
}

func memberRevocationChatIDs(dependents []db.ListPoolMemberRevocationDependentsRow) map[string]pgtype.UUID {
	chatIDs := make(map[string]pgtype.UUID, len(dependents))
	for _, dependent := range dependents {
		addRevocationUUID(chatIDs, dependent.ChatSessionID)
	}
	return chatIDs
}

func memberRevocationHasUnlockedTailRequester(tails []db.ListPoolChatMemberRevocationTailsRow, memberLockSet map[string]pgtype.UUID) bool {
	for _, tail := range tails {
		if !tail.RuntimeRequesterUserID.Valid {
			continue
		}
		if _, locked := memberLockSet[uuidToString(tail.RuntimeRequesterUserID)]; !locked {
			return true
		}
	}
	return false
}

func sortedRevocationUUIDs(set map[string]pgtype.UUID) []pgtype.UUID {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	ids := make([]pgtype.UUID, 0, len(keys))
	for _, key := range keys {
		ids = append(ids, set[key])
	}
	return ids
}

func isMemberRevocationNonterminal(status string) bool {
	switch status {
	case "waiting_runtime", "queued", "deferred", "dispatched", "running", "waiting_local_directory":
		return true
	default:
		return false
	}
}

func memberRevocationEffectiveRuntime(task db.AgentTaskQueue, lockedChats map[string]db.ChatSession) pgtype.UUID {
	if task.RuntimeID.Valid {
		return task.RuntimeID
	}
	if task.SessionAffinityRuntimeID.Valid {
		return task.SessionAffinityRuntimeID
	}
	if task.SessionAffinityState == "unresolved" && task.ChatSessionID.Valid {
		if chat, ok := lockedChats[uuidToString(task.ChatSessionID)]; ok {
			return chat.RuntimeID
		}
	}
	return pgtype.UUID{}
}

// revocationResult captures everything revokeMemberRuntimes touched so the
// caller can fan out events and analytics after the transaction commits.
// Publishing inside the transaction would let subscribers observe a state the
// tx might still roll back (see TaskService.BroadcastCancelledTasks docstring).
type revocationResult struct {
	Runtimes           []db.AgentRuntime
	ArchivedAgents     []db.Agent
	CancelledTasks     []db.AgentTaskQueue
	PromotedTasks      []db.AgentTaskQueue
	OfflineRuntimeIDs  []db.ForceOfflineRuntimesByIDsRow
	RevokedTokenHashes []string
	UpdatedMember      db.Member
}

func (r revocationResult) isEmpty() bool {
	return len(r.Runtimes) == 0 &&
		len(r.ArchivedAgents) == 0 &&
		len(r.CancelledTasks) == 0 &&
		len(r.PromotedTasks) == 0 &&
		len(r.OfflineRuntimeIDs) == 0 &&
		len(r.RevokedTokenHashes) == 0
}

// publishRevocation runs all post-commit side effects: invalidate daemon token
// cache, broadcast task:cancelled with per-agent reconciliation, broadcast
// agent:archived, and signal a runtime-list refresh. Safe to call on an empty
// result — it returns immediately.
func (h *Handler) publishRevocation(ctx context.Context, result revocationResult, workspaceIDStr, actorType, actorIDStr string) {
	if result.isEmpty() {
		return
	}

	for _, hash := range result.RevokedTokenHashes {
		h.DaemonTokenCache.Invalidate(ctx, hash)
	}

	// Per-task cancellation: TaskService handles status reconciliation and
	// per-task event broadcast. Run this before the agent:archived burst so
	// subscribers see "task cancelled" before the parent agent disappears
	// from active lists, matching the order ArchiveAgent uses.
	if h.TaskService != nil && len(result.CancelledTasks) > 0 {
		h.TaskService.BroadcastCancelledTasks(ctx, result.CancelledTasks)
	}
	// Cancelling a resolved Pool Chat head also resolves one authorized tail in
	// the same committed transaction. BroadcastCancelledTasks above already
	// wakes each affected Workspace exactly once; after that allocator attempt,
	// only a tail that is still waiting needs its own observable waiting event.
	// Assigned rows were already published as queued by the shared allocator,
	// while deferred tails remain intentionally silent until their deadline.
	if h.TaskService != nil {
		for _, promoted := range result.PromotedTasks {
			persisted, err := h.Queries.GetAgentTask(ctx, promoted.ID)
			if err != nil {
				slog.Warn("reload promoted Pool Chat tail after member revocation",
					"task_id", uuidToString(promoted.ID), "error", err)
				continue
			}
			if persisted.RuntimeBindingMode == "pool" && persisted.Status == "waiting_runtime" {
				h.TaskService.BroadcastTaskWaitingRuntime(ctx, persisted)
			}
		}
	}

	for _, agent := range result.ArchivedAgents {
		h.publish(protocol.EventAgentArchived, workspaceIDStr, actorType, actorIDStr, map[string]any{
			"agent": h.agentToResponse(agent),
		})
	}

	// Tell connected clients to refresh the runtime list. We piggyback on
	// EventDaemonRegister with a "revoke" action — same channel the runtime
	// delete handler uses — so the frontend invalidates its cached list
	// without us having to introduce a new event type the desktop app would
	// need a build to learn about.
	if len(result.OfflineRuntimeIDs) > 0 {
		h.publish(protocol.EventDaemonRegister, workspaceIDStr, actorType, actorIDStr, map[string]any{
			"action": "revoke",
		})
	}
}

// logRevocation emits a structured info line summarising the revocation. Kept
// separate from publish so the log is identical whether or not the bus is wired.
func logRevocation(result revocationResult, workspaceID, userID string, attrs ...any) {
	if result.isEmpty() {
		return
	}
	base := []any{
		"workspace_id", workspaceID,
		"user_id", userID,
		"runtimes_revoked", len(result.Runtimes),
		"agents_archived", len(result.ArchivedAgents),
		"tasks_cancelled", len(result.CancelledTasks),
		"runtimes_taken_offline", len(result.OfflineRuntimeIDs),
		"daemon_tokens_revoked", len(result.RevokedTokenHashes),
	}
	slog.Info("member runtimes revoked", append(base, attrs...)...)
}
