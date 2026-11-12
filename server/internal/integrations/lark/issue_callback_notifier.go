package lark

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Delivery ledger states (issue_channel_callback_delivery.status).
const (
	callbackDeliveryDelivered = "delivered"
	callbackDeliveryFailed    = "failed"
	callbackDeliverySkipped   = "skipped"
)

// IssueCallbackQueries is the DB surface IssueCallbackNotifier needs.
// *ChannelStore satisfies it.
type IssueCallbackQueries interface {
	issuestatus.Querier
	GetIssue(ctx context.Context, id pgtype.UUID) (db.Issue, error)
	GetWorkspace(ctx context.Context, id pgtype.UUID) (db.Workspace, error)
	GetLarkInstallation(ctx context.Context, id pgtype.UUID) (Installation, error)
	ListActiveIssueChannelCallbacks(ctx context.Context, arg db.ListActiveIssueChannelCallbacksParams) ([]db.IssueChannelCallback, error)
	ClaimIssueChannelCallbackDelivery(ctx context.Context, arg db.ClaimIssueChannelCallbackDeliveryParams) (db.IssueChannelCallbackDelivery, error)
	FinishIssueChannelCallbackDelivery(ctx context.Context, arg db.FinishIssueChannelCallbackDeliveryParams) error
}

// IssueCallbackNotifier reports an issue's status back to the Feishu chat
// message that created it with /issue. The notice is a reply in the thread
// under that message, so it stays attached to the request it answers. Each
// send is recorded in the delivery ledger first, which keeps a redelivered
// event from posting twice and leaves failures visible.
type IssueCallbackNotifier struct {
	queries     IssueCallbackQueries
	credentials CredentialsResolver
	client      APIClient
	appURL      string
	log         *slog.Logger
}

func NewIssueCallbackNotifier(queries IssueCallbackQueries, credentials CredentialsResolver, client APIClient, appURL string, log *slog.Logger) *IssueCallbackNotifier {
	if log == nil {
		log = slog.Default()
	}
	return &IssueCallbackNotifier{
		queries:     queries,
		credentials: credentials,
		client:      client,
		appURL:      strings.TrimRight(appURL, "/"),
		log:         log,
	}
}

// issueCallbackStatusLabel lists the statuses worth reporting back: the work
// needs the requester again or is finished. Forward progress stays silent.
var issueCallbackStatusLabel = map[string]string{
	issuestatus.InReview:  "In review",
	issuestatus.Done:      "Done",
	issuestatus.Blocked:   "Blocked",
	issuestatus.Cancelled: "Cancelled",
}

// NotifyStatusChanged reports the issue's stored status to each active Feishu
// callback. It re-reads the issue instead of trusting the event payload, and
// keys the ledger by status and update time, so a replayed event for the
// same change is a no-op while a later change to the same status is not.
func (n *IssueCallbackNotifier) NotifyStatusChanged(ctx context.Context, issueID pgtype.UUID) error {
	issue, err := n.queries.GetIssue(ctx, issueID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("load issue: %w", err)
	}
	status := issuestatus.Effective(ctx, n.queries, issue.WorkspaceID, issue.Status)
	label, ok := issueCallbackStatusLabel[status]
	if !ok {
		return nil
	}
	callbacks, err := n.queries.ListActiveIssueChannelCallbacks(ctx, db.ListActiveIssueChannelCallbacksParams{
		IssueID:     issue.ID,
		ChannelType: channelTypeFeishu,
	})
	if err != nil {
		return fmt.Errorf("list issue callbacks: %w", err)
	}
	if len(callbacks) == 0 {
		return nil
	}

	prefix, slug := "", ""
	if ws, err := n.queries.GetWorkspace(ctx, issue.WorkspaceID); err == nil {
		prefix, slug = ws.IssuePrefix, ws.Slug
	}
	text := issueCallbackStatusText(label, issue, prefix, slug, n.appURL)
	event := "status:" + status
	dedupKey := event + ":" + strconv.FormatInt(issue.UpdatedAt.Time.UnixMicro(), 10)

	var errs []error
	for _, cb := range callbacks {
		if err := n.deliver(ctx, cb, event, dedupKey, text); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (n *IssueCallbackNotifier) deliver(ctx context.Context, cb db.IssueChannelCallback, event, dedupKey, text string) error {
	if _, err := n.queries.ClaimIssueChannelCallbackDelivery(ctx, db.ClaimIssueChannelCallbackDeliveryParams{
		CallbackID: cb.ID,
		DedupKey:   dedupKey,
		Event:      event,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("claim callback delivery: %w", err)
	}

	messageID, skipReason, sendErr := n.send(ctx, cb, text)
	finish := db.FinishIssueChannelCallbackDeliveryParams{
		CallbackID:        cb.ID,
		DedupKey:          dedupKey,
		Status:            callbackDeliveryDelivered,
		ProviderMessageID: messageID,
	}
	switch {
	case sendErr != nil:
		finish.Status, finish.Error = callbackDeliveryFailed, sendErr.Error()
	case skipReason != "":
		finish.Status, finish.Error = callbackDeliverySkipped, skipReason
	}
	if err := n.queries.FinishIssueChannelCallbackDelivery(ctx, finish); err != nil {
		return errors.Join(sendErr, fmt.Errorf("record callback delivery: %w", err))
	}
	if sendErr != nil {
		return fmt.Errorf("callback %s: %w", uuidString(cb.ID), sendErr)
	}
	return nil
}

// send posts text into the thread under the callback's anchor message. A
// non-empty skip reason means nothing was sent because the installation can
// no longer speak for this workspace.
func (n *IssueCallbackNotifier) send(ctx context.Context, cb db.IssueChannelCallback, text string) (messageID, skipReason string, err error) {
	inst, err := n.queries.GetLarkInstallation(ctx, cb.InstallationID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "installation removed", nil
		}
		return "", "", fmt.Errorf("load installation: %w", err)
	}
	if InstallationStatus(inst.Status) != InstallationActive || inst.WorkspaceID != cb.WorkspaceID {
		return "", "installation inactive", nil
	}
	creds, err := n.installationCredentials(inst)
	if err != nil {
		return "", "", err
	}
	target := ReplyTarget{MessageID: cb.AnchorMessageID, InThread: true}
	err = sendWithReplyFallback(n.log, "send issue callback notice", target, func(t ReplyTarget) error {
		id, sendErr := n.client.SendTextMessage(ctx, SendTextParams{
			InstallationID: creds,
			ChatID:         ChatID(cb.ChatID),
			Text:           text,
			ReplyTarget:    t,
		})
		messageID = id
		return sendErr
	})
	return messageID, "", err
}

func issueCallbackStatusText(label string, issue db.Issue, prefix, slug, appURL string) string {
	identifier := service.IssueIdentifier(prefix, issue.Number)
	line := fmt.Sprintf("%s: %s", label, identifier)
	if title := strings.TrimSpace(issue.Title); title != "" {
		line += " — " + title
	}
	// A bare "#42" fallback is a display label, not a routable identifier.
	if prefix != "" {
		if link := channel.IssueWebLink(appURL, slug, identifier); link != "" {
			line += "\n" + link
		}
	}
	return line
}

func (n *IssueCallbackNotifier) installationCredentials(inst Installation) (InstallationCredentials, error) {
	if n.credentials == nil {
		return InstallationCredentials{}, errors.New("lark issue callback notifier: credentials resolver missing")
	}
	secret, err := n.credentials.DecryptAppSecret(inst)
	if err != nil {
		return InstallationCredentials{}, fmt.Errorf("decrypt app_secret: %w", err)
	}
	creds := InstallationCredentials{
		AppID:     inst.AppID,
		AppSecret: secret,
		Region:    RegionOrDefault(inst.Region),
	}
	if inst.TenantKey.Valid {
		creds.TenantKey = inst.TenantKey.String
	}
	return creds, nil
}
