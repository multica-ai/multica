package weixin

import (
	"context"
	"encoding/json"
	"errors"
	"hash/fnv"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Final replies only: iLink has no message-edit contract. The shared delivery
// lease fences replicas and retry attempts; an ambiguous send is never replayed.
type Outbound struct {
	q       *db.Queries
	box     *secretbox.Box
	client  *http.Client
	logger  *slog.Logger
	queues  [4]chan events.Event
	done    chan struct{}
	once    sync.Once
	wg      sync.WaitGroup
	objects objectStore
}

func NewOutbound(q *db.Queries, box *secretbox.Box, logger *slog.Logger) *Outbound {
	if logger == nil {
		logger = slog.Default()
	}
	o := &Outbound{q: q, box: box, logger: logger, done: make(chan struct{})}
	for i := range o.queues {
		o.queues[i] = make(chan events.Event, 32)
	}
	return o
}

func (o *Outbound) Register(bus *events.Bus) {
	for _, typ := range []string{protocol.EventChatDone, protocol.EventTaskFailed, protocol.EventTaskCancelled} {
		bus.Subscribe(typ, o.enqueue)
	}
}

func (o *Outbound) Start(ctx context.Context) {
	o.once.Do(func() {
		for _, queue := range o.queues {
			o.wg.Add(1)
			go func() {
				defer o.wg.Done()
				for {
					select {
					case <-ctx.Done():
						return
					case event := <-queue:
						call, cancel := context.WithTimeout(ctx, 5*time.Minute)
						if err := o.processEvent(call, event); err != nil {
							o.logger.WarnContext(ctx, "weixin: reply delivery failed", "task_id", event.TaskID, "error", err)
						}
						cancel()
					}
				}
			}()
		}
		go func() { <-ctx.Done(); close(o.done) }()
	})
}

func (o *Outbound) WaitWithTimeout(timeout time.Duration) bool {
	done := make(chan struct{})
	go func() { o.wg.Wait(); close(done) }()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

func (o *Outbound) enqueue(e events.Event) {
	if e.ChatSessionID == "" {
		return
	}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(e.ChatSessionID))
	// Bound memory and preserve per-conversation ordering. Backpressure never
	// silently discards a final answer. HTTP does not run on the event bus.
	select {
	case o.queues[hash.Sum32()%uint32(len(o.queues))] <- e:
	case <-o.done:
	}
}

type completion struct {
	TaskID       string `json:"task_id"`
	MessageID    string `json:"message_id"`
	Content      string `json:"content"`
	RetryPending bool   `json:"retry_pending"`
}

func (o *Outbound) processEvent(ctx context.Context, event events.Event) error {
	var payload completion
	raw, _ := json.Marshal(event.Payload)
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	if payload.RetryPending {
		return nil
	}
	if event.TaskID != "" {
		payload.TaskID = event.TaskID
	}
	taskID, err := util.ParseUUID(payload.TaskID)
	if err != nil || !taskID.Valid {
		return nil
	}
	delivery, err := o.q.GetChannelTaskDelivery(ctx, taskID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if delivery.ChannelType != string(TypeWeixin) {
		return nil
	}
	task, err := o.q.GetAgentTask(ctx, taskID)
	if err != nil {
		return err
	}
	fromChannel, err := engine.TaskInputIsChannelIngested(ctx, o.q, task)
	if err != nil || !fromChannel {
		return err
	}
	inst, err := o.q.GetChannelInstallation(ctx, db.GetChannelInstallationParams{ID: delivery.InstallationID, ChannelType: string(TypeWeixin)})
	if err != nil {
		return err
	}
	if inst.Status != "active" {
		return nil
	}
	creds, err := decodeCredentials(inst.Config, o.box)
	if err != nil {
		return err
	}
	var route weixinBindingConfig
	if json.Unmarshal(delivery.Config, &route) != nil || route.BotID != creds.AppID || route.UserID != creds.UserID || delivery.ChannelChatID != creds.UserID {
		return ErrAccess // a disconnected/rebound bot cannot receive old tasks
	}
	turn, err := o.q.GetChannelReplyTurn(ctx, taskID)
	if err != nil {
		return err
	}
	if !turn.TurnID.Valid {
		return ErrProtocol
	}
	owner := dbid.NewV7()
	lease, err := o.q.AcquireChannelReplyDelivery(ctx, db.AcquireChannelReplyDeliveryParams{TurnID: turn.TurnID, TaskID: taskID, AttemptDepth: int32(turn.AttemptDepth), BindingID: delivery.BindingID, InstallationID: inst.ID, ChannelType: string(TypeWeixin), ChatID: creds.UserID, Phase: "terminal", OwnerToken: owner, LeaseSeconds: 60})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() {
		record, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_, _ = o.q.ReleaseChannelReplyDelivery(record, db.ReleaseChannelReplyDeliveryParams{TurnID: turn.TurnID, OwnerToken: owner})
	}()
	settle := func(reason string) error {
		record, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_, err := o.q.SettleChannelReplyDelivery(record, db.SettleChannelReplyDeliveryParams{TurnID: turn.TurnID, OwnerToken: owner, SettledReason: reason})
		return err
	}
	if lease.SendState == "in_flight" || lease.SendState == "unknown" {
		return settle("send_result_unknown")
	}
	if event.Type == protocol.EventTaskFailed {
		payload.Content = "This run failed. Open Multica to review the error and try again."
	}
	if event.Type == protocol.EventTaskCancelled {
		return settle("cancelled")
	}
	chunks := splitText(payload.Content, 4000)
	var attachments []db.Attachment
	if o.objects != nil && event.Type == protocol.EventChatDone && payload.MessageID != "" {
		messageID, parseErr := util.ParseUUID(payload.MessageID)
		if parseErr != nil {
			return parseErr
		}
		attachments, err = o.q.ListAttachmentsByChatMessage(ctx, db.ListAttachmentsByChatMessageParams{ChatMessageID: messageID, WorkspaceID: inst.WorkspaceID})
		if err != nil {
			return err
		}
	}
	for i := int(lease.ChunksSent); i < len(chunks)+len(attachments); i++ {
		// Revalidate revocation and the current context before every provider
		// call. The 15s send budget fits inside the renewed 60s owner lease.
		api, token, err := o.sender(ctx, inst.ID, creds.AppID, creds.UserID)
		if err != nil {
			return err
		}
		msg := textMessage(creds.UserID, token, util.UUIDToString(dbid.NewV7()), "")
		if i < len(chunks) {
			msg = textMessage(creds.UserID, token, msg.ClientID, chunks[i])
		} else {
			item, prepErr := o.attachment(ctx, api, creds.UserID, attachments[i-len(chunks)])
			if prepErr != nil {
				msg = textMessage(creds.UserID, token, msg.ClientID, "This file could not be sent to WeChat. It is still attached to the reply in Multica.")
				o.logger.WarnContext(ctx, "weixin: attachment unavailable", "error", prepErr)
			} else {
				msg.Items = []messageItem{item}
			}
		}
		// Upload preparation may take longer than a lease. Recheck installation
		// and ownership after it, before posting anything into the conversation.
		api, token, err = o.sender(ctx, inst.ID, creds.AppID, creds.UserID)
		if err != nil {
			return err
		}
		msg.ContextToken = token
		rows, err := o.q.RenewChannelReplyDelivery(ctx, db.RenewChannelReplyDeliveryParams{TurnID: turn.TurnID, OwnerToken: owner, LeaseSeconds: 60})
		if err != nil || rows != 1 {
			return err
		}
		rows, err = o.q.MarkChannelReplyDeliverySending(ctx, db.MarkChannelReplyDeliverySendingParams{TurnID: turn.TurnID, OwnerToken: owner})
		if err != nil || rows != 1 {
			return err
		}
		messageID, sendErr := api.Send(ctx, msg)
		record, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		if sendErr != nil {
			// Do not infer non-delivery from a timeout, malformed response, or
			// HTTP error. Keeping unknown also covers a process dying mid-send.
			_, _ = o.q.MarkChannelReplyDeliverySendUnknown(record, turn.TurnID)
			cancel()
			_ = settle("send_result_unknown")
			return sendErr
		}
		rows, err = o.q.RecordChannelReplyDeliveryChunk(record, db.RecordChannelReplyDeliveryChunkParams{TurnID: turn.TurnID, OwnerToken: owner, MessageID: messageID, ChunksSent: int32(i + 1)})
		if err == nil && rows == 1 {
			err = o.q.RecordChannelOutboundMessage(record, db.RecordChannelOutboundMessageParams{OutboundInstallationID: inst.ID, OutboundChannelType: string(TypeWeixin), OutboundMessageID: messageID, OutboundBindingID: delivery.BindingID, OutboundRouteRevision: delivery.RouteRevision, OutboundTaskID: taskID, OutboundKind: "task_reply"})
		}
		cancel()
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrBusy
		}
	}
	return settle("delivered")
}

func (o *Outbound) sender(ctx context.Context, installationID pgtype.UUID, botID, userID string) (*apiClient, string, error) {
	inst, err := o.q.GetChannelInstallation(ctx, db.GetChannelInstallationParams{ID: installationID, ChannelType: string(TypeWeixin)})
	if err != nil {
		return nil, "", err
	}
	if inst.Status != "active" {
		return nil, "", ErrNotFound
	}
	creds, err := decodeCredentials(inst.Config, o.box)
	if err != nil {
		return nil, "", err
	}
	if creds.AppID != botID || creds.UserID != userID {
		return nil, "", ErrAccess
	}
	state, err := o.q.GetWeixinAccountState(ctx, db.GetWeixinAccountStateParams{InstallationID: inst.ID, BotID: botID})
	if err != nil {
		return nil, "", err
	}
	token, err := o.box.Open(state.ContextTokenEncrypted)
	if err != nil || len(token) == 0 {
		return nil, "", ErrNoContext
	}
	return newAPI(creds.BaseURL, creds.Token, o.client), string(token), nil
}

func splitText(text string, limit int) []string {
	var chunks []string
	runes := []rune(text)
	for len(runes) > 0 {
		n := min(limit, len(runes))
		chunks = append(chunks, string(runes[:n]))
		runes = runes[n:]
	}
	return chunks
}
