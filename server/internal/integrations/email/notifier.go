package email

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
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
	return c
}

// Notifier subscribes to EventInboxNew and sends an email per inbox row.
type Notifier struct {
	queries  NotifierQueries
	sender   EmailSender
	cfg      NotifierConfig
	inflight sync.WaitGroup // tracks goroutines spawned by handleEvent; tests Wait() on it
}

func NewNotifier(queries NotifierQueries, sender EmailSender, cfg NotifierConfig) *Notifier {
	return &Notifier{queries: queries, sender: sender, cfg: cfg.withDefaults()}
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

	if err := n.sender.SendNotification(to, out.Subject, out.Text, out.HTML); err != nil {
		n.cfg.Logger.Warn("email notifier: send failed",
			"to", to, "notif_type", notifType, "error", err)
		return
	}
}

func (n *Notifier) isEmailMuted(ctx context.Context, wsID, userID pgtype.UUID) bool {
	raw, err := n.queries.GetNotificationPreference(ctx,
		GetNotificationPreferenceParams{WorkspaceID: wsID, UserID: userID})
	if err != nil {
		return false // missing prefs row = default on
	}
	var prefs map[string]string
	if err := json.Unmarshal(raw, &prefs); err != nil {
		return false
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
