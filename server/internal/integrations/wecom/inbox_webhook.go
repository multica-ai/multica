package wecom

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/projectaccess"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	inboxWebhookQueueLimit = 64
	inboxWebhookSeenLimit  = 2048
	inboxWebhookInterval   = 3100 * time.Millisecond
)

type inboxWebhookQueries interface {
	inboxWebhookCardQueries
	projectaccess.Querier
}

// InboxWebhook redirects one member's task notifications, not their chat replies.
// Delivery follows the existing best-effort inbox event bus; the queue is not durable.
type InboxWebhook struct {
	q                               inboxWebhookQueries
	endpoint, workspaceID, memberID string
	logger                          *slog.Logger
	client                          *http.Client
	interval                        time.Duration
	mu                              sync.Mutex
	queue                           []map[string]any
	seen                            map[string]bool
	order                           []string
	next                            int
	running                         bool
	spawn                           func(func())
}

// NewInboxWebhookFromEnv returns nil when disabled. Invalid configurations are
// rejected as a unit so the caller can retain the original notification route.
func NewInboxWebhookFromEnv(q inboxWebhookQueries, logger *slog.Logger) (*InboxWebhook, error) {
	endpoint := os.Getenv("MULTICA_WECOM_NOTIFICATION_WEBHOOK_URL")
	workspace := os.Getenv("MULTICA_WECOM_NOTIFICATION_WORKSPACE_ID")
	member := os.Getenv("MULTICA_WECOM_NOTIFICATION_MEMBER_ID")
	if endpoint == "" && workspace == "" && member == "" {
		return nil, nil
	}
	endpoint, err := validateInboxWebhookURL(endpoint)
	if err != nil {
		return nil, err
	}
	ws, wsErr := util.ParseUUID(workspace)
	user, userErr := util.ParseUUID(member)
	if wsErr != nil || userErr != nil || !ws.Valid || !user.Valid {
		return nil, errors.New("WeCom notification workspace and member must both be UUIDs")
	}
	if inboxWebhookAppURL() == "" {
		return nil, errors.New("WeCom notification webhook requires a valid app URL")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &InboxWebhook{
		q: q, endpoint: endpoint, workspaceID: util.UUIDToString(ws), memberID: util.UUIDToString(user),
		logger: logger, client: &http.Client{Timeout: 5 * time.Second},
		interval: inboxWebhookInterval, seen: make(map[string]bool),
		spawn: func(f func()) { go f() },
	}, nil
}

func inboxWebhookKind(kind string) bool {
	_, ok := copyFor(DefaultLocale).InboxTypeLabels[kind]
	return ok
}

// Matches is shared by both delivery paths: even a failed webhook must not
// silently send the same task event to the member's private conversation.
func (w *InboxWebhook) Matches(item map[string]any) bool {
	if w == nil {
		return false
	}
	return item["recipient_type"] == "member" && item["workspace_id"] == w.workspaceID &&
		item["recipient_id"] == w.memberID &&
		inboxWebhookKind(inboxWebhookString(item["type"]))
}

func (w *InboxWebhook) Register(bus *events.Bus) {
	if w != nil {
		bus.Subscribe(protocol.EventInboxNew, w.handle)
	}
}

func (w *InboxWebhook) handle(e events.Event) {
	payload, ok := e.Payload.(map[string]any)
	if !ok || e.WorkspaceID != w.workspaceID {
		return
	}
	item, ok := payload["item"].(map[string]any)
	if !ok || !w.Matches(item) {
		return
	}
	id, _ := item["id"].(string)
	parsed, err := util.ParseUUID(id)
	if err != nil || !parsed.Valid {
		w.logger.Warn("wecom notification webhook", "outcome", "invalid_event")
		return
	}
	// Own the queued snapshot: event publishers and other listeners retain theirs.
	fields := make(map[string]any)
	for _, key := range []string{"id", "workspace_id", "recipient_type", "recipient_id", "type", "issue_id", "actor_type", "actor_id", "created_at", "details", "body"} {
		fields[key] = item[key]
	}
	raw, err := json.Marshal(fields)
	var snapshot map[string]any
	if err != nil || json.Unmarshal(raw, &snapshot) != nil {
		w.record(id, "invalid_event")
		return
	}
	w.enqueue(id, snapshot)
}

func (w *InboxWebhook) enqueue(id string, item map[string]any) {
	w.mu.Lock()
	if w.seen[id] {
		w.mu.Unlock()
		return
	}
	if len(w.queue) >= inboxWebhookQueueLimit {
		w.mu.Unlock()
		w.record(id, "queue_full")
		return
	}
	if len(w.order) == inboxWebhookSeenLimit {
		delete(w.seen, w.order[w.next])
		w.order[w.next] = id
		w.next = (w.next + 1) % inboxWebhookSeenLimit
	} else {
		w.order = append(w.order, id)
	}
	w.seen[id] = true
	w.queue = append(w.queue, item)
	start := !w.running
	w.running = true
	w.mu.Unlock()
	if start {
		w.spawn(w.drain)
	}
}

func (w *InboxWebhook) drain() {
	for {
		w.mu.Lock()
		if len(w.queue) == 0 {
			w.running = false
			w.mu.Unlock()
			return
		}
		item := w.queue[0]
		w.queue[0] = nil
		w.queue = w.queue[1:]
		w.mu.Unlock()
		w.deliver(item)
		// One worker stays below the webhook's 20 messages/minute limit.
		time.Sleep(w.interval)
	}
}

func (w *InboxWebhook) deliver(item map[string]any) {
	id, _ := item["id"].(string)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	card, outcome := w.prepare(ctx, item)
	if outcome != "" {
		w.record(id, outcome)
		return
	}
	err := sendInboxWebhook(ctx, w.client, w.endpoint, card)
	if errors.Is(err, errWebhookRateLimited) {
		timer := time.NewTimer(w.interval)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			w.record(id, "rate_limited")
			return
		case <-timer.C:
		}
		// Permissions and comment deletion can change while the sender is limited.
		card, outcome = w.prepare(ctx, item)
		if outcome != "" {
			w.record(id, outcome)
			return
		}
		err = sendInboxWebhook(ctx, w.client, w.endpoint, card)
	}
	if err != nil {
		// Transport errors deliberately exclude URLs, response text and credentials.
		w.record(id, "send_failed_or_unknown")
		return
	}
	w.record(id, "delivered")
}

func (w *InboxWebhook) prepare(ctx context.Context, item map[string]any) ([]byte, string) {
	ws, _ := util.ParseUUID(w.workspaceID)
	user, _ := util.ParseUUID(w.memberID)
	member, err := w.q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		WorkspaceID: ws, UserID: user,
	})
	if err != nil {
		return nil, "membership_unavailable"
	}
	issueID, err := util.ParseUUID(inboxItemIssueID(item))
	if err != nil || !issueID.Valid {
		return nil, "issue_unavailable"
	}
	issue, err := w.q.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: issueID, WorkspaceID: ws})
	if err != nil {
		return nil, "issue_unavailable"
	}
	if issue.ProjectID.Valid {
		hidden, err := projectaccess.ForMember(ctx, w.q, ws, user, member.Role)
		if err != nil || !hidden.CanSeeProject(issue.ProjectID) {
			return nil, "project_access_unavailable"
		}
	}
	card, err := buildInboxWebhookCardForIssue(ctx, w.q, item, issue)
	if err != nil {
		return nil, "card_build_failed"
	}
	return card, ""
}

func (w *InboxWebhook) record(id, outcome string) {
	if outcome == "delivered" {
		w.logger.Info("wecom notification webhook", "inbox_id", id, "outcome", outcome)
	} else {
		w.logger.Warn("wecom notification webhook", "inbox_id", id, "outcome", outcome)
	}
}

// WithInboxWebhook makes notification routing exclusive; chat delivery is untouched.
func WithInboxWebhook(w *InboxWebhook) OutboundOption {
	return func(o *Outbound) { o.inboxWebhook = w }
}
