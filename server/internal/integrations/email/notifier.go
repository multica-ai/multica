package email

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/resend/resend-go/v2"
)

// NotifierQueries is the DB surface the Notifier needs. Production wires
// it via an adapter (cmd/server) that pulls .Email / .Slug / .Preferences
// off the corresponding *db.Queries return rows. Tests substitute a fake.
type NotifierQueries interface {
	GetUserEmail(ctx context.Context, userID pgtype.UUID) (string, error)
	GetNotificationPreference(ctx context.Context, arg GetNotificationPreferenceParams) ([]byte, error)
	GetWorkspaceSlug(ctx context.Context, workspaceID pgtype.UUID) (string, error)
}

// GetNotificationPreferenceParams mirrors the sqlc-generated params struct
// (workspace_id, user_id). Re-declared locally to keep this package decoupled
// from db types.
type GetNotificationPreferenceParams struct {
	WorkspaceID pgtype.UUID
	UserID      pgtype.UUID
}

// NotifierConfig holds optional dependencies.
type NotifierConfig struct {
	Renderer *Renderer
	Logger   *slog.Logger
	Timeout  time.Duration
	// MaxConcurrent bounds how many deliveries run at once, so the fan-out
	// of one comment to many subscribers stays under the provider's request
	// rate (Resend defaults to 2 requests/second).
	MaxConcurrent int
	// MaxAttempts is the total number of tries for a rate-limited send.
	MaxAttempts int
	// RetryBackoff is the wait before retrying a rate-limited send when the
	// provider does not say how long to wait; it doubles on each retry.
	RetryBackoff time.Duration
}

func (c NotifierConfig) withDefaults() NotifierConfig {
	if c.Renderer == nil {
		c.Renderer = NewRenderer("")
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	if c.Timeout == 0 {
		c.Timeout = 10 * time.Second
	}
	if c.MaxConcurrent <= 0 {
		c.MaxConcurrent = 2
	}
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = 4
	}
	if c.RetryBackoff <= 0 {
		c.RetryBackoff = time.Second
	}
	return c
}

// maxRetryWait caps a provider-supplied Retry-After so one rate-limited
// email cannot hold a delivery slot indefinitely.
const maxRetryWait = 30 * time.Second

// Notifier subscribes to EventInboxNew and sends an email per inbox row.
type Notifier struct {
	queries  NotifierQueries
	sender   EmailSender
	cfg      NotifierConfig
	slots    chan struct{}  // bounds concurrent deliveries to cfg.MaxConcurrent
	inflight sync.WaitGroup // tracks goroutines spawned by handleEvent; tests Wait() on it
}

func NewNotifier(queries NotifierQueries, sender EmailSender, cfg NotifierConfig) *Notifier {
	cfg = cfg.withDefaults()
	return &Notifier{
		queries: queries,
		sender:  sender,
		cfg:     cfg,
		slots:   make(chan struct{}, cfg.MaxConcurrent),
	}
}

// Register subscribes the notifier to the bus. Call exactly once during boot,
// after construction and before HTTP traffic starts.
//
// The subscribed callback returns immediately by spawning processEvent on a
// fresh goroutine. This is required because events.Bus.Publish is synchronous —
// a stuck SMTP send would otherwise block the HTTP request that triggered the
// inbox row, scaled by the number of recipients (e.g. an @-all on a 10-person
// workspace could hang the comment POST for up to 30s × 10 = 5 min). The
// goroutine inherits no caller context and uses its own timeout from cfg.
func (n *Notifier) Register(bus *events.Bus) {
	bus.Subscribe(protocol.EventInboxNew, func(e events.Event) {
		n.inflight.Add(1)
		go func() {
			defer n.inflight.Done()
			n.slots <- struct{}{}
			defer func() { <-n.slots }()
			n.processEvent(e)
		}()
	})
}

// WaitInflight blocks until all goroutines spawned by Register's handler have
// finished. Tests call this after bus.Publish to synchronize on delivery; the
// production server never calls it (goroutines run to completion or are torn
// down with the process).
func (n *Notifier) WaitInflight() {
	n.inflight.Wait()
}

func (n *Notifier) processEvent(e events.Event) {
	ctx, cancel := context.WithTimeout(context.Background(), n.cfg.Timeout)
	defer cancel()

	payload, ok := e.Payload.(map[string]any)
	if !ok {
		return
	}
	item, ok := payload["item"].(map[string]any)
	if !ok {
		return
	}

	recipientType, _ := item["recipient_type"].(string)
	if recipientType != "member" {
		return // agents don't get emails
	}

	recipientIDStr, _ := item["recipient_id"].(string)
	notifType, _ := item["type"].(string)
	issueTitle, _ := item["title"].(string)
	// inboxItemToResponse stores issue_id via util.UUIDToPtr — so the payload
	// value is *string, not string. Accept both forms so a future serializer
	// change to a plain string doesn't silently break deep links.
	issueID := stringOrPtr(item["issue_id"])

	if recipientIDStr == "" || notifType == "" {
		return
	}

	recipientID := parseUUID(recipientIDStr)
	workspaceID := parseUUID(e.WorkspaceID)
	if !recipientID.Valid || !workspaceID.Valid {
		return
	}

	if n.isEmailMuted(ctx, workspaceID, recipientID) {
		return
	}

	to, err := n.queries.GetUserEmail(ctx, recipientID)
	if err != nil || to == "" {
		n.cfg.Logger.Debug("email notifier: no email for recipient",
			"user_id", recipientIDStr, "error", err)
		return
	}

	slug, err := n.queries.GetWorkspaceSlug(ctx, workspaceID)
	if err != nil {
		n.cfg.Logger.Debug("email notifier: workspace slug lookup failed",
			"workspace_id", e.WorkspaceID, "error", err)
		slug = ""
	}

	out := n.cfg.Renderer.Render(RenderInput{
		NotifType:     notifType,
		IssueTitle:    issueTitle,
		IssueID:       issueID,
		WorkspaceSlug: slug,
	})

	n.send(to, notifType, out)
}

// send delivers one rendered email, retrying only when the provider reports
// a rate limit. Other errors are not retried: an SMTP failure after the
// message was accepted would otherwise deliver a duplicate.
func (n *Notifier) send(to, notifType string, out RenderOutput) {
	backoff := n.cfg.RetryBackoff
	for attempt := 1; ; attempt++ {
		err := n.sender.SendNotification(to, out.Subject, out.Text, out.HTML)
		if err == nil {
			return
		}
		if !errors.Is(err, resend.ErrRateLimit) || attempt >= n.cfg.MaxAttempts {
			n.cfg.Logger.Warn("email notifier: send failed",
				"to", to, "notif_type", notifType, "attempts", attempt, "error", err)
			return
		}
		time.Sleep(retryWait(err, backoff))
		backoff *= 2
	}
}

// retryWait honours the provider's Retry-After (seconds) when present,
// capped at maxRetryWait, and otherwise uses the exponential fallback.
func retryWait(err error, fallback time.Duration) time.Duration {
	wait := fallback
	var rl *resend.RateLimitError
	if errors.As(err, &rl) {
		if secs, perr := strconv.ParseFloat(rl.RetryAfter, 64); perr == nil && secs > 0 {
			wait = time.Duration(secs * float64(time.Second))
		}
	}
	if wait > maxRetryWait {
		wait = maxRetryWait
	}
	return wait
}

func (n *Notifier) isEmailMuted(ctx context.Context, wsID, userID pgtype.UUID) bool {
	raw, err := n.queries.GetNotificationPreference(ctx,
		GetNotificationPreferenceParams{WorkspaceID: wsID, UserID: userID})
	if err != nil {
		// A confirmed missing row means the user still has the default
		// preferences (email enabled). Any other lookup failure must fail
		// closed so a transient DB error cannot bypass an explicit opt-out.
		return !errors.Is(err, pgx.ErrNoRows)
	}
	var prefs map[string]string
	if err := json.Unmarshal(raw, &prefs); err != nil {
		return true
	}
	return prefs["email_notifications"] == "muted"
}

func parseUUID(s string) pgtype.UUID {
	var u pgtype.UUID
	_ = u.Scan(s)
	return u
}

// stringOrPtr accepts both the production payload shape (*string from
// util.UUIDToPtr) and the simpler plain-string form used in some tests, and
// returns the empty string when the value is nil or absent.
func stringOrPtr(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case *string:
		if s == nil {
			return ""
		}
		return *s
	default:
		return ""
	}
}
