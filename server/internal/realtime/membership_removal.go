package realtime

const (
	memberRemovalWorkers   = 2
	memberRemovalQueueSize = 64
)

type memberRemovalJob struct {
	workspaceID string
	userID      string
	checker     MembershipChecker
	clients     []*Client
	notice      []byte
}

func (h *Hub) snapshotMemberRemoval(workspaceID, userID string) memberRemovalJob {
	h.mu.RLock()
	defer h.mu.RUnlock()
	job := memberRemovalJob{workspaceID: workspaceID, userID: userID, checker: h.membership}
	for client := range h.clients {
		if client.workspaceID == workspaceID && client.userID == userID {
			job.clients = append(job.clients, client)
		}
	}
	return job
}

// Removal signals are advisory invalidations. Their database revalidation
// must never block a Redis shard reader, even when this queue is saturated.
// Content writers independently enforce fresh primary-database membership.
func (h *Hub) queueMemberRemoval(workspaceID string, raw []byte, eventID string) bool {
	userID, removal := parseMemberRemoval(workspaceID, raw)
	if !removal {
		return false
	}
	if userID == "" {
		return true
	}
	// Freeze targets before fanout and queueing, not when a worker starts:
	// a later registration must not inherit this signal or lookup result.
	job := h.snapshotMemberRemoval(workspaceID, userID)
	// Peers use member:* events to refetch the current member list. Deliver
	// that invalidation in reader order, independently of lookup latency or
	// queue overflow. Exclude the target: its client would navigate away even
	// if the removal is stale and it has already rejoined.
	h.broadcastToScope(ScopeWorkspace, workspaceID, raw, eventID, userID)
	if len(job.clients) == 0 {
		return true
	}
	job.notice = raw
	h.memberRemovalOnce.Do(func() {
		for i := 0; i < memberRemovalWorkers; i++ {
			go h.runMemberRemovals()
		}
	})
	h.memberRemovalPending.Add(1)
	select {
	case h.memberRemovals <- job:
	default:
		h.memberRemovalPending.Add(-1)
		h.memberRemovalDropped.Add(1)
		M.MembershipRemovalDroppedTotal.Add(1)
	}
	return true
}

func (h *Hub) runMemberRemovals() {
	for job := range h.memberRemovals {
		h.disconnectMember(job)
		h.memberRemovalPending.Add(-1)
	}
}
