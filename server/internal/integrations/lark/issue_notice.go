package lark

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// IssueNoticeWorker owns a distinct durable queue: a later Issue transition
// must not reopen the already-settled channel_reply_delivery chat turn.
type IssueNoticeWorker struct {
	q           *db.Queries
	credentials CredentialsResolver
	client      APIClient
	appURL      string
	mayInvoke   func(context.Context, pgtype.UUID, pgtype.UUID) (bool, error)
}

func NewIssueNoticeWorker(q *db.Queries, credentials CredentialsResolver, client APIClient, appURL string, mayInvoke func(context.Context, pgtype.UUID, pgtype.UUID) (bool, error)) *IssueNoticeWorker {
	return &IssueNoticeWorker{q: q, credentials: credentials, client: client, appURL: strings.TrimRight(appURL, "/"), mayInvoke: mayInvoke}
}

func issueNoticeText(agent, key, title, kind, link string) string {
	state := map[string]string{"completed": "Completed", "blocked": "Blocked: action required", "failed": "Run failed: action required"}[kind]
	text := fmt.Sprintf("%s\n%s - %s\n%s", agent, key, state, title)
	if link != "" {
		text += "\n" + link
	}
	if kind == "failed" {
		text += "\nThe latest run failed; this does not mark the issue complete."
	}
	// Group recipients must not receive private comments or raw runtime errors.
	return html.EscapeString(text)
}

func (w *IssueNoticeWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			op, cancel := context.WithTimeout(ctx, 30*time.Second)
			err := w.deliver(op)
			cancel()
			if err != nil {
				slog.Warn("lark issue notification cycle failed")
			}
		}
	}
}

func (w *IssueNoticeWorker) deliver(ctx context.Context) error {
	if err := w.q.ExpireChannelIssueNotices(ctx); err != nil {
		return err
	}
	owner := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	n, err := w.q.ClaimChannelIssueNotice(ctx, owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	status, message := "sent", ""
	r, err := w.q.GetChannelIssueNoticeRoute(ctx, db.GetChannelIssueNoticeRouteParams{ID: n.ID, OwnerToken: owner})
	if errors.Is(err, pgx.ErrNoRows) {
		status = "cancelled"
	} else if err == nil {
		// Reuse the inbound invocation policy; a revoked recipient must not
		// continue receiving asynchronous updates from a private agent.
		allowed := false
		if w.mayInvoke != nil {
			allowed, err = w.mayInvoke(ctx, r.AgentID, n.RecipientUserID)
		}
		if err == nil && !allowed {
			status = "cancelled"
		}
	}
	if err == nil && status == "sent" {
		inst, decodeErr := installationFromRow(db.ChannelInstallation{
			ID: r.InstallationID, WorkspaceID: r.WorkspaceID, AgentID: r.AgentID, ChannelType: r.ChannelType, Config: r.Config,
			Status: r.Status, InstallerUserID: r.InstallerUserID, WsLeaseToken: r.WsLeaseToken, WsLeaseExpiresAt: r.WsLeaseExpiresAt,
			InstalledAt: r.InstalledAt, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
		})
		err = decodeErr
		if err == nil {
			var secret string
			secret, err = w.credentials.DecryptAppSecret(inst)
			if err == nil {
				binding := ChatSessionBinding{ID: n.BindingID, InstallationID: n.InstallationID, ChannelChatID: n.ChannelChatID, ChatType: "group", Config: n.RouteConfig, LastMessageID: n.ChannelMessageID, LastThreadID: n.ChannelThreadID}
				if topicSendWithoutTrigger(binding) {
					status = "cancelled"
				} else {
					link := channel.IssueWebLink(w.appURL, r.Slug, util.UUIDToString(n.IssueID))
					key := fmt.Sprintf("%s-%d", r.IssuePrefix, r.Number)
					if r.IssuePrefix == "" {
						key = fmt.Sprintf("#%d", r.Number)
					}
					text := issueNoticeText(r.AgentName, key, r.Title, n.Kind, link)
					message, err = w.client.SendTextMessage(ctx, SendTextParams{
						InstallationID: InstallationCredentials{AppID: inst.AppID, AppSecret: secret, TenantKey: inst.TenantKey.String, Region: RegionOrDefault(inst.Region)},
						ChatID:         outboundChatID(binding), ReplyTarget: threadReplyTarget(binding), Text: prependTextMention(n.ChannelSenderID, text), UUID: util.UUIDToString(n.ID),
					})
					if err == nil && message == "" {
						err = errors.New("issue notification missing delivery receipt")
					}
				}
			}
		}
	}
	delay := 0.0
	if err != nil && status != "cancelled" {
		status = "queued"
		delay = float64((15 << uint(n.Attempts)) + rand.IntN(11))
		if n.Attempts >= 3 || time.Since(n.FirstAttemptAt.Time) > 4*time.Minute {
			status = "failed"
		}
	}
	// Persist even if the request deadline expired after Feishu accepted it.
	record, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	changed, persistErr := w.q.FinishChannelIssueNotice(record, db.FinishChannelIssueNoticeParams{ID: n.ID, OwnerToken: owner, Status: status, MessageID: message, RetrySeconds: delay})
	if persistErr != nil {
		return persistErr
	}
	if changed != 1 {
		return errors.New("issue notification ownership changed")
	}
	slog.Info("lark issue notification delivery", "notice_id", util.UUIDToString(n.ID), "status", status, "attempt", n.Attempts, "message_id", message)
	return nil
}
