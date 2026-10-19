package realtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/gorilla/websocket"
)

const (
	membershipLookupTimeout = 2 * time.Second
	maxWriteBatch           = 32
	removalNoticeWait       = 100 * time.Millisecond
)

// SetMembershipChecker wires the primary database lookup before serving sockets.
// The handshake and content writer must use this same authority.
func (h *Hub) SetMembershipChecker(checker MembershipChecker) {
	h.mu.Lock()
	h.membership = checker
	h.mu.Unlock()
}

func checkMembership(checker MembershipChecker, userID, workspaceID string) (bool, error) {
	if checker == nil {
		return false, errors.New("membership checker unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), membershipLookupTimeout)
	defer cancel()
	member, err := checker.IsMember(ctx, userID, workspaceID)
	if err != nil {
		slog.Warn("ws membership lookup failed", "user_id", userID, "workspace_id", workspaceID, "error", err)
	}
	return member, err
}

func (h *Hub) registerClient(client *Client) {
	// Keep the Hub event loop free of database waits. A connection whose
	// handshake raced revocation is denied by its first content write.
	h.mu.Lock()
	// A transport/write failure can abort this client before registration.
	// Check under the same lock used by removeClient.
	if client.stopped.Load() {
		h.mu.Unlock()
		return
	}
	h.clients[client] = true
	total := len(h.clients)
	h.mu.Unlock()
	M.ConnectsTotal.Add(1)
	M.ActiveConnections.Add(1)
	h.subscribe(client, ScopeWorkspace, client.workspaceID)
	h.subscribe(client, ScopeUser, client.userID)
	slog.Info("ws client connected", "workspace_id", client.workspaceID, "user_id", client.userID, "total_clients", total)
}

// DisconnectMember is a post-commit signal, not an authorization cache. Check
// current membership so delayed/replayed signals cannot disconnect a rejoined
// member. Lookup errors fail closed without claiming membership was removed.
func (h *Hub) DisconnectMember(workspaceID, userID, memberID string) {
	if h == nil || workspaceID == "" || userID == "" {
		return
	}
	notice, _ := json.Marshal(map[string]any{
		"type":    "member:removed",
		"payload": map[string]string{"workspace_id": workspaceID, "user_id": userID, "member_id": memberID},
	})
	job := h.snapshotMemberRemoval(workspaceID, userID)
	if len(job.clients) == 0 {
		return
	}
	job.notice = notice
	h.disconnectMember(job)
}

// Only confirmed removals send the target a notice; stale signals are ignored
// and lookup failures close captured connections without a navigation notice.
func (h *Hub) disconnectMember(job memberRemovalJob) {
	member, err := checkMembership(job.checker, job.userID, job.workspaceID)
	if err == nil && member {
		return
	}
	if err != nil {
		job.notice = nil
	}
	// One total deadline bounds notification attempts across all sockets. An
	// idle writer can send the existing client navigation signal; busy/slow
	// writers are closed immediately instead of draining content first.
	deadline := time.Now().Add(removalNoticeWait)
	for _, client := range job.clients {
		h.stopClient(client, job.notice, deadline)
	}
}

// Returns the target and whether this is a removal frame. An empty target on
// a removal frame means its payload is invalid and the frame must be dropped.
func parseMemberRemoval(workspaceID string, raw []byte) (string, bool) {
	if !memberRemovalCandidate(raw) {
		return "", false
	}
	var frame struct {
		Type    string `json:"type"`
		Payload struct {
			UserID      string `json:"user_id"`
			WorkspaceID string `json:"workspace_id"`
		} `json:"payload"`
	}
	if json.Unmarshal(raw, &frame) != nil || frame.Type != "member:removed" {
		return "", false
	}
	if frame.Payload.UserID == "" || frame.Payload.WorkspaceID != workspaceID {
		return "", true
	}
	return frame.Payload.UserID, true
}

func memberRemovalCandidate(raw []byte) bool {
	// Require an event prefix, not the actor_type value "member" attached to
	// ordinary comments and issue updates. A separator escape starts with '\'.
	// An escaped "member" prefix must contain \u006* (m/e/b) or \u0072
	// (r). HTML and line-separator escapes in ordinary content do not qualify.
	return bytes.Contains(raw, []byte("member:")) || bytes.Contains(raw, []byte(`member\`)) ||
		bytes.Contains(raw, []byte(`\u006`)) || bytes.Contains(raw, []byte(`\u0072`))
}

func (h *Hub) stopClient(client *Client, notice []byte, deadline time.Time) {
	client.stopped.Store(true)
	h.removeClient(client)
	client.closeSend() // a writer may stop before registration completes
	for range client.send {
		// Discard queued content, including frames whose writer is doing a
		// membership lookup concurrently. The writer also checks stopped.
	}
	if client.conn == nil {
		return
	}
	if len(notice) != 0 && time.Now().Before(deadline) && client.writeMu.TryLock() {
		client.conn.SetWriteDeadline(deadline)
		_ = client.conn.WriteMessage(websocket.TextMessage, notice)
		client.writeMu.Unlock()
	}
	client.conn.Close()
}

// Capture the bounded batch BEFORE querying membership. A successful lookup
// authorizes only these already queued frames, never content arriving during
// or after the lookup. In particular there is no positive TTL or singleflight
// result that a post-revocation event on another node can reuse.
func (c *Client) writeBatch(first []byte) bool {
	if c.stopped.Load() {
		return false
	}
	batch := make([][]byte, 0, maxWriteBatch)
	batch = append(batch, first)
drain:
	for len(batch) < maxWriteBatch {
		select {
		case frame, open := <-c.send:
			if !open {
				break drain
			}
			batch = append(batch, frame)
		default:
			break drain
		}
	}
	if !c.hub.authorizeWrites(MembershipIdentity{WorkspaceID: c.workspaceID, UserID: c.userID}) {
		c.hub.stopClient(c, nil, time.Time{})
		return false
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	// One deadline bounds the whole batch instead of allowing each frame a
	// fresh timeout. A revocation can still interrupt it via conn.Close.
	c.conn.SetWriteDeadline(time.Now().Add(writeWait))
	for _, frame := range batch {
		if c.stopped.Load() {
			return false
		}
		if err := c.conn.WriteMessage(websocket.TextMessage, frame); err != nil {
			slog.Warn("websocket write error", "error", err, "user_id", c.userID, "workspace_id", c.workspaceID)
			return false
		}
	}
	return true
}
