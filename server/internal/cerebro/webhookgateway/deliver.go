// Package webhookgateway delivers Firtal Gateway webhook events into Multica
// channels (FIR-1766). It is the server-side counterpart to the registry's
// webhook control plane and Cloudflare consumer.
//
// The single rule this package enforces: a webhook posts ON BEHALF OF a chosen
// Multica principal (an agent OR a user/member), and the gate is the LIVE
// permission system — channel membership. We REUSE the exact membership check
// that canAccessIssue applies to channels (IsIssueSubscriber): only a participant
// of a channel may post into it, admins included only if they participate. There
// is no new permission model here, and explicitly NO persona/grants (that system
// is being removed).
//
// The endpoint is mounted OUTSIDE the auth middleware (like the autopilot /
// GitHub / Stripe webhooks) and authenticates itself with a shared gateway
// delivery token. The token only attests "this is the gateway"; it grants no
// channel access on its own — the on-behalf principal's membership decides
// whether the post is allowed.
package webhookgateway

import (
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Deliverer posts gateway events into channels on behalf of a principal.
type Deliverer struct {
	Queries *db.Queries
	Bus     *events.Bus
	// Token is the shared gateway delivery secret. When empty the endpoint is
	// disabled (acts as the feature kill switch) and every call 404s.
	Token string
}

func New(queries *db.Queries, bus *events.Bus, token string) *Deliverer {
	return &Deliverer{Queries: queries, Bus: bus, Token: token}
}

// Enabled reports whether the endpoint is live (a delivery token is configured).
func (d *Deliverer) Enabled() bool {
	return d != nil && d.Queries != nil && d.Token != ""
}

type deliverRequest struct {
	ChannelID string `json:"channel_id"`
	Content   string `json:"content"`
}

type principal struct {
	Type string // "agent" | "member"
	ID   pgtype.UUID
}

// Handle is the HTTP entry point: POST /api/webhooks/gateway/channel-message.
// Headers: Authorization: Bearer <gateway token>, X-On-Behalf-Of: <type>:<uuid>.
// Body: {"channel_id":"<uuid>","content":"..."}.
func (d *Deliverer) Handle(w http.ResponseWriter, r *http.Request) {
	// Disabled, unauthorized, and unknown-target all return the same opaque 404
	// so a caller cannot probe whether the endpoint is live or which channels
	// exist. Genuine input errors (bad identity / body) return 400/403 so a
	// legitimate integrator can fix the call.
	if !d.Enabled() {
		notFound(w)
		return
	}
	if !d.authorized(r) {
		notFound(w)
		return
	}

	p, ok := parsePrincipal(r.Header.Get("X-On-Behalf-Of"))
	if !ok {
		writeError(w, http.StatusBadRequest, "x-on-behalf-of must be <agent|member>:<uuid>")
		return
	}

	var req deliverRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256*1024)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}
	channelID, err := util.ParseUUID(req.ChannelID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "channel_id must be a uuid")
		return
	}

	issue, err := d.Queries.GetIssue(r.Context(), channelID)
	if err != nil {
		// No such channel — opaque 404, same as an unknown endpoint.
		notFound(w)
		return
	}
	if issue.Kind != "channel" && issue.Kind != "dm" {
		writeError(w, http.StatusBadRequest, "target is not a channel")
		return
	}

	// The principal must be a real agent/member in this channel's workspace.
	if !d.principalInWorkspace(r, p, issue.WorkspaceID) {
		writeError(w, http.StatusForbidden, "unknown principal for this workspace")
		return
	}

	// THE gate: channel membership. Identical to canAccessIssue's channel rule.
	subscribed, err := d.Queries.IsIssueSubscriber(r.Context(), db.IsIssueSubscriberParams{
		IssueID:  issue.ID,
		UserType: p.Type,
		UserID:   p.ID,
	})
	if err != nil {
		slog.Error("gateway delivery: membership check failed", "error", err, "channel_id", req.ChannelID)
		writeError(w, http.StatusInternalServerError, "membership check failed")
		return
	}
	if !subscribed {
		// The principal cannot reach this channel — refuse, never escalate.
		writeError(w, http.StatusForbidden, "principal is not a member of this channel")
		return
	}

	comment, err := d.Queries.CreateComment(r.Context(), db.CreateCommentParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		AuthorType:  p.Type,
		AuthorID:    p.ID,
		Content:     req.Content,
		Type:        "comment",
	})
	if err != nil {
		slog.Error("gateway delivery: create comment failed", "error", err, "channel_id", req.ChannelID)
		writeError(w, http.StatusInternalServerError, "failed to post message")
		return
	}

	// Broadcast so the message appears live in the channel (same event the
	// normal comment path and the agent path publish).
	d.Bus.Publish(events.Event{
		Type:        protocol.EventCommentCreated,
		WorkspaceID: util.UUIDToString(issue.WorkspaceID),
		ActorType:   p.Type,
		ActorID:     util.UUIDToString(p.ID),
		Payload: map[string]any{
			"comment": map[string]any{
				"id":          util.UUIDToString(comment.ID),
				"issue_id":    util.UUIDToString(comment.IssueID),
				"author_type": comment.AuthorType,
				"author_id":   util.UUIDToString(comment.AuthorID),
				"content":     comment.Content,
				"type":        comment.Type,
				"parent_id":   util.UUIDToPtr(comment.ParentID),
				"created_at":  comment.CreatedAt.Time.Format("2006-01-02T15:04:05Z"),
			},
			"issue_title":  issue.Title,
			"issue_status": issue.Status,
		},
	})

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":          util.UUIDToString(comment.ID),
		"issue_id":    util.UUIDToString(comment.IssueID),
		"author_type": comment.AuthorType,
		"author_id":   util.UUIDToString(comment.AuthorID),
	})
}

// authorized does a constant-time comparison of the bearer token so a wrong
// token costs the same time as a right one (no timing oracle on the secret).
func (d *Deliverer) authorized(r *http.Request) bool {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, prefix) {
		return false
	}
	got := strings.TrimSpace(h[len(prefix):])
	return subtle.ConstantTimeCompare([]byte(got), []byte(d.Token)) == 1
}

func (d *Deliverer) principalInWorkspace(r *http.Request, p principal, workspaceID pgtype.UUID) bool {
	switch p.Type {
	case "agent":
		agent, err := d.Queries.GetAgent(r.Context(), p.ID)
		if err != nil {
			return false
		}
		return util.UUIDToString(agent.WorkspaceID) == util.UUIDToString(workspaceID)
	case "member":
		_, err := d.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
			UserID:      p.ID,
			WorkspaceID: workspaceID,
		})
		return err == nil
	default:
		return false
	}
}

// parsePrincipal reads the `<type>:<uuid>` on-behalf identity. The type must be
// agent or member; the id must be a uuid. Anything else is rejected.
func parsePrincipal(raw string) (principal, bool) {
	raw = strings.TrimSpace(raw)
	idx := strings.IndexByte(raw, ':')
	if idx <= 0 {
		return principal{}, false
	}
	kind := raw[:idx]
	if kind != "agent" && kind != "member" {
		return principal{}, false
	}
	id, err := util.ParseUUID(raw[idx+1:])
	if err != nil {
		return principal{}, false
	}
	return principal{Type: kind, ID: id}, true
}

func notFound(w http.ResponseWriter) {
	writeError(w, http.StatusNotFound, "not found")
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
