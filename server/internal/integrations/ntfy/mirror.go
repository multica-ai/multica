package ntfy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	kindMention      = "mentioned"
	kindAssignment   = "issue_assigned"
	kindTaskComplete = "task_completed"
	kindTaskFailed   = "task_failed"
	kindIssueBlocked = "issue_blocked"

	logInterval = time.Minute
)

type notificationQueries interface {
	IsIssueSubscriber(context.Context, db.IsIssueSubscriberParams) (bool, error)
	GetNotificationPreference(context.Context, db.GetNotificationPreferenceParams) (db.NotificationPreference, error)
}

type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type job struct {
	kind        string
	workspaceID string
	issueID     string
	requireSub  bool
}

type message struct {
	title    string
	body     string
	priority int
	tags     []string
	click    string
}

type publishPayload struct {
	Topic    string   `json:"topic"`
	Message  string   `json:"message"`
	Title    string   `json:"title"`
	Priority int      `json:"priority"`
	Tags     []string `json:"tags,omitempty"`
	Click    string   `json:"click,omitempty"`
}

type throttledLog struct {
	mu      sync.Mutex
	last    time.Time
	pending int64
}

// Mirror copies a privacy-minimized subset of Multica notifications to one
// member's ntfy topic. Event handlers only enqueue fixed-size metadata; the
// worker owns all database and network I/O.
type Mirror struct {
	config      Config
	queries     notificationQueries
	client      httpDoer
	logger      *slog.Logger
	recipientID pgtype.UUID

	ctx    context.Context
	cancel context.CancelFunc
	jobs   chan job
	done   chan struct{}
	stop   atomic.Bool

	dropLog    throttledLog
	failureLog throttledLog
}

// New creates and starts an ntfy mirror. Config must come from ConfigFromEnv
// (or be equivalently validated by a test).
func New(config Config, queries notificationQueries, logger *slog.Logger, client httpDoer) *Mirror {
	if logger == nil {
		logger = slog.Default()
	}
	if config.Timeout <= 0 {
		config.Timeout = defaultTimeout
	}
	if config.QueueCapacity <= 0 {
		config.QueueCapacity = defaultQueueCapacity
	}
	if client == nil {
		client = &http.Client{Timeout: config.Timeout}
	}
	recipientID, err := util.ParseUUID(config.RecipientID)
	if err != nil {
		panic("ntfy: New called with an invalid recipient UUID")
	}

	ctx, cancel := context.WithCancel(context.Background())
	m := &Mirror{
		config:      config,
		queries:     queries,
		client:      client,
		logger:      logger,
		recipientID: recipientID,
		ctx:         ctx,
		cancel:      cancel,
		jobs:        make(chan job, config.QueueCapacity),
		done:        make(chan struct{}),
	}
	go m.run()
	return m
}

// Register subscribes the mirror to the in-app notification stream for direct
// member notifications and to task terminal events for completion/failure.
func (m *Mirror) Register(bus *events.Bus) {
	bus.Subscribe(protocol.EventInboxNew, m.handleInboxNew)
	bus.Subscribe(protocol.EventTaskCompleted, func(e events.Event) {
		m.handleTaskEvent(e, kindTaskComplete)
	})
	bus.Subscribe(protocol.EventTaskFailed, func(e events.Event) {
		m.handleTaskEvent(e, kindTaskFailed)
	})
}

// Stop cancels any in-flight delivery and drops queued work. Shutdown is
// intentionally bounded; ntfy is a best-effort mirror, not durable state.
func (m *Mirror) Stop(ctx context.Context) bool {
	if m.stop.CompareAndSwap(false, true) {
		m.cancel()
	}
	select {
	case <-m.done:
		return true
	case <-ctx.Done():
		return false
	}
}

func (m *Mirror) handleInboxNew(e events.Event) {
	payload, ok := e.Payload.(map[string]any)
	if !ok {
		return
	}
	item, ok := payload["item"].(map[string]any)
	if !ok {
		return
	}
	if stringValue(item["recipient_type"]) != "member" || stringValue(item["recipient_id"]) != m.config.RecipientID {
		return
	}

	kind := ""
	switch stringValue(item["type"]) {
	case kindMention:
		kind = kindMention
	case kindAssignment:
		kind = kindAssignment
	case "status_changed":
		if stringValue(item["issue_status"]) == "blocked" {
			kind = kindIssueBlocked
		}
	}
	if kind == "" {
		return
	}

	workspaceID := stringValue(item["workspace_id"])
	if workspaceID == "" {
		workspaceID = e.WorkspaceID
	}
	m.enqueue(job{
		kind:        kind,
		workspaceID: workspaceID,
		issueID:     stringValue(item["issue_id"]),
	})
}

func (m *Mirror) handleTaskEvent(e events.Event, kind string) {
	payload, ok := e.Payload.(map[string]any)
	if !ok {
		return
	}
	if kind == kindTaskFailed {
		if retryPending, _ := payload["retry_pending"].(bool); retryPending {
			return
		}
	}
	issueID := stringValue(payload["issue_id"])
	if issueID == "" || e.WorkspaceID == "" {
		return
	}
	m.enqueue(job{
		kind:        kind,
		workspaceID: e.WorkspaceID,
		issueID:     issueID,
		requireSub:  true,
	})
}

func (m *Mirror) enqueue(j job) {
	if m.stop.Load() {
		return
	}
	select {
	case m.jobs <- j:
	default:
		m.warnThrottled(&m.dropLog, "ntfy mirror: queue full; notification dropped",
			"event_kind", j.kind)
	}
}

func (m *Mirror) run() {
	defer close(m.done)
	for {
		select {
		case <-m.ctx.Done():
			return
		case j := <-m.jobs:
			ctx, cancel := context.WithTimeout(m.ctx, m.config.Timeout)
			msg, ok := m.resolve(ctx, j)
			if ok {
				m.deliver(ctx, j.kind, msg)
			}
			cancel()
		}
	}
}

func (m *Mirror) resolve(ctx context.Context, j job) (message, bool) {
	if _, err := util.ParseUUID(j.workspaceID); err != nil {
		return message{}, false
	}
	issueID, err := util.ParseUUID(j.issueID)
	if err != nil {
		return message{}, false
	}
	if j.requireSub {
		if m.queries == nil {
			return message{}, false
		}
		subscribed, err := m.queries.IsIssueSubscriber(ctx, db.IsIssueSubscriberParams{
			IssueID:  issueID,
			UserType: "member",
			UserID:   m.recipientID,
		})
		if err != nil {
			m.warnThrottled(&m.failureLog, "ntfy mirror: recipient lookup failed",
				"event_kind", j.kind)
			return message{}, false
		}
		if !subscribed || m.agentActivityMuted(ctx, j.workspaceID) {
			return message{}, false
		}
	}

	msg, ok := messageForKind(j.kind)
	if !ok {
		return message{}, false
	}
	msg.click = m.issueLink(j.workspaceID, j.issueID)
	return msg, true
}

func (m *Mirror) agentActivityMuted(ctx context.Context, workspaceID string) bool {
	workspaceUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return true
	}
	pref, err := m.queries.GetNotificationPreference(ctx, db.GetNotificationPreferenceParams{
		WorkspaceID: workspaceUUID,
		UserID:      m.recipientID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false
	}
	if err != nil {
		m.warnThrottled(&m.failureLog, "ntfy mirror: notification preference lookup failed")
		return true
	}
	var prefs map[string]string
	if err := json.Unmarshal(pref.Preferences, &prefs); err != nil {
		m.warnThrottled(&m.failureLog, "ntfy mirror: notification preference decode failed")
		return true
	}
	return prefs["agent_activity"] == "muted"
}

func messageForKind(kind string) (message, bool) {
	switch kind {
	case kindMention:
		return message{
			title:    "Multica: Mention",
			body:     "You were mentioned on an issue. Open Multica to review it.",
			priority: 3,
			tags:     []string{"speech_balloon"},
		}, true
	case kindAssignment:
		return message{
			title:    "Multica: Assignment",
			body:     "An issue was assigned to you. Open Multica to review it.",
			priority: 3,
			tags:     []string{"clipboard"},
		}, true
	case kindTaskComplete:
		return message{
			title:    "Multica: Agent run completed",
			body:     "An agent run completed for an issue. Open Multica for details.",
			priority: 3,
			tags:     []string{"white_check_mark"},
		}, true
	case kindTaskFailed:
		return message{
			title:    "Multica: Agent run failed",
			body:     "An agent run failed for an issue. Open Multica for details.",
			priority: 4,
			tags:     []string{"warning"},
		}, true
	case kindIssueBlocked:
		return message{
			title:    "Multica: Issue blocked",
			body:     "An issue you follow entered blocked. Open Multica for details.",
			priority: 4,
			tags:     []string{"warning"},
		}, true
	default:
		return message{}, false
	}
}

func (m *Mirror) issueLink(workspaceID, issueID string) string {
	if m.config.AppURL == "" {
		return ""
	}
	u, err := url.Parse(m.config.AppURL)
	if err != nil {
		return ""
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/" + url.PathEscape(workspaceID) + "/inbox"
	q := u.Query()
	q.Set("issue", issueID)
	u.RawQuery = q.Encode()
	return u.String()
}

func (m *Mirror) deliver(ctx context.Context, kind string, msg message) {
	body, err := json.Marshal(publishPayload{
		Topic:    m.config.Topic,
		Message:  msg.body,
		Title:    msg.title,
		Priority: msg.priority,
		Tags:     msg.tags,
		Click:    msg.click,
	})
	if err != nil {
		m.warnThrottled(&m.failureLog, "ntfy mirror: request encoding failed",
			"event_kind", kind)
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.config.BaseURL+"/", bytes.NewReader(body))
	if err != nil {
		m.warnThrottled(&m.failureLog, "ntfy mirror: request creation failed",
			"event_kind", kind)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if m.config.Token != "" {
		req.Header.Set("Authorization", "Bearer "+m.config.Token)
	}

	resp, err := m.client.Do(req)
	if err != nil {
		reason := "transport"
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			reason = "timeout"
		}
		m.warnThrottled(&m.failureLog, "ntfy mirror: delivery failed",
			"event_kind", kind, "reason", reason)
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		m.warnThrottled(&m.failureLog, "ntfy mirror: delivery rejected",
			"event_kind", kind, "status", resp.StatusCode)
		return
	}
	m.logger.Debug("ntfy mirror: notification delivered",
		"event_kind", kind, "status", resp.StatusCode)
}

func (m *Mirror) warnThrottled(state *throttledLog, message string, attrs ...any) {
	now := time.Now()
	state.mu.Lock()
	state.pending++
	if !state.last.IsZero() && now.Sub(state.last) < logInterval {
		state.mu.Unlock()
		return
	}
	count := state.pending
	state.pending = 0
	state.last = now
	state.mu.Unlock()

	attrs = append(attrs, "occurrences", count)
	m.logger.Warn(message, attrs...)
}

func stringValue(v any) string {
	switch value := v.(type) {
	case string:
		return value
	case *string:
		if value != nil {
			return *value
		}
	}
	return ""
}
