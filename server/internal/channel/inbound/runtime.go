package inbound

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	channelconversation "github.com/multica-ai/multica/server/internal/channel/conversation"
	"github.com/multica-ai/multica/server/internal/channel/conversationctx"
	chintent "github.com/multica-ai/multica/server/internal/channel/intent"
	"github.com/multica-ai/multica/server/internal/channel/port"
	"github.com/multica-ai/multica/server/internal/channel/replyctx"
	"github.com/multica-ai/multica/server/internal/util"
)

const (
	defaultInboundWorkers              = 16
	defaultInboundClaimBatch           = 32
	defaultInboundPollInterval         = 250 * time.Millisecond
	defaultInboundIntentTaskTimeout    = 15 * time.Minute
	defaultInboundActionTaskTimeout    = 30 * time.Minute
	defaultInboundClarificationTimeout = 30 * time.Minute
	defaultInboundProcessingLease      = 5 * time.Minute
	defaultFailureNoticeCooldown       = 5 * time.Minute
	defaultContextMaxEntities          = 5
	defaultContextTTL                  = 30 * time.Minute
)

const (
	failureCodeNoChannelAgent     = "no_channel_agent"
	failureCodeChannelTurnFailed  = "channel_turn_failed"
	failureCodeChannelTurnEmpty   = "channel_turn_empty"
	failureCodeChannelTurnTimeout = "channel_turn_timeout"
	failureCodeInboundDead        = "inbound_dead"
)

type FailureNoticeKey struct {
	ConnectionID string
	ChatID       string
	SenderID     string
	Code         string
}

type FailureNoticeLimiter interface {
	ShouldSendFailureNotice(ctx context.Context, key FailureNoticeKey, cooldown time.Duration) (bool, error)
}

type memoryFailureNoticeLimiter struct {
	mu   sync.Mutex
	last map[FailureNoticeKey]time.Time
}

func newMemoryFailureNoticeLimiter() *memoryFailureNoticeLimiter {
	return &memoryFailureNoticeLimiter{last: make(map[FailureNoticeKey]time.Time)}
}

func (l *memoryFailureNoticeLimiter) ShouldSendFailureNotice(_ context.Context, key FailureNoticeKey, cooldown time.Duration) (bool, error) {
	if l == nil {
		return true, nil
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if last, ok := l.last[key]; ok && now.Sub(last) < cooldown {
		return false, nil
	}
	l.last[key] = now
	return true, nil
}

type RuntimeConfig struct {
	Store                 InboundEventStore
	PrePipeline           *Pipeline
	PostPipeline          *Pipeline
	RuleResolvers         []chintent.IntentResolver
	ChatIntent            chintent.AsyncChatIntentClient
	TurnPlanner           chintent.ChannelTurnPlanner
	ChannelTurn           chintent.ChannelAgentTurnClient
	DispatchStore         DispatchCompletionStore
	FailureLimiter        FailureNoticeLimiter
	ReplyContext          replyctx.Store
	ConversationStore     channelconversation.Store
	ConversationCtx       conversationctx.Store
	ContextMaxEntities    int
	ContextTTL            time.Duration
	ReplySink             ChannelReplySink
	Workers               int
	ClaimBatch            int
	PollInterval          time.Duration
	IntentTaskTimeout     time.Duration
	ActionTaskTimeout     time.Duration
	ClarificationTimeout  time.Duration
	ProcessingLease       time.Duration
	FailureNoticeCooldown time.Duration
}

type Runtime struct {
	cfg RuntimeConfig

	mu                sync.Mutex
	pendingAckByEvent map[string]struct{}
}

func NewRuntime(cfg RuntimeConfig) *Runtime {
	if cfg.Workers <= 0 {
		cfg.Workers = defaultInboundWorkers
	}
	if cfg.ClaimBatch <= 0 {
		cfg.ClaimBatch = defaultInboundClaimBatch
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = defaultInboundPollInterval
	}
	if cfg.IntentTaskTimeout <= 0 {
		cfg.IntentTaskTimeout = defaultInboundIntentTaskTimeout
	}
	if cfg.ActionTaskTimeout <= 0 {
		cfg.ActionTaskTimeout = defaultInboundActionTaskTimeout
	}
	if cfg.ClarificationTimeout <= 0 {
		cfg.ClarificationTimeout = defaultInboundClarificationTimeout
	}
	if cfg.ProcessingLease <= 0 {
		cfg.ProcessingLease = defaultInboundProcessingLease
	}
	if cfg.FailureNoticeCooldown <= 0 {
		cfg.FailureNoticeCooldown = defaultFailureNoticeCooldown
	}
	if cfg.ContextMaxEntities <= 0 {
		cfg.ContextMaxEntities = defaultContextMaxEntities
	}
	if cfg.ContextTTL <= 0 {
		cfg.ContextTTL = defaultContextTTL
	}
	if cfg.FailureLimiter == nil {
		cfg.FailureLimiter = newMemoryFailureNoticeLimiter()
	}
	if cfg.TurnPlanner == nil {
		if planner, ok := cfg.ChatIntent.(chintent.ChannelTurnPlanner); ok {
			cfg.TurnPlanner = planner
		}
	}
	if cfg.ChannelTurn == nil {
		if turn, ok := cfg.ChatIntent.(chintent.ChannelAgentTurnClient); ok {
			cfg.ChannelTurn = turn
		}
	}
	return &Runtime{cfg: cfg, pendingAckByEvent: make(map[string]struct{})}
}

func (r *Runtime) Run(ctx context.Context) {
	if r == nil || r.cfg.Store == nil {
		return
	}
	var wg sync.WaitGroup
	for i := 0; i < r.cfg.Workers; i++ {
		workerID := fmt.Sprintf("channel-inbound-%d", i+1)
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.workerLoop(ctx, workerID)
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		r.resumeLoop(ctx)
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		r.sweeperLoop(ctx)
	}()
	<-ctx.Done()
	wg.Wait()
}

func (r *Runtime) Accept(ctx context.Context, evt port.InboundEvent, opts AcceptOptions) (AcceptResult, error) {
	if opts.BypassLimit || ControlMessageBypassesBackpressure(evt) {
		opts.BypassLimit = true
	}
	result, err := r.cfg.Store.AcceptEvent(ctx, evt, opts)
	if err != nil {
		return result, err
	}
	if result.Duplicate {
		return result, nil
	}
	switch {
	case result.RejectedBackpressure:
		r.send(ctx, evt, fmt.Sprintf("我现在忙不过来了，当前会话还有 %d 条在排队，请稍后再发。", result.QueueDepth))
	case result.ClarificationConsumed:
		// Legacy stores may still report this, but the product model no longer
		// sends a mechanical "received clarification" acknowledgement.
	case result.Accepted && result.QueueDepth == 0:
		// Normal channel turns should feel like a teammate replying, not a bot
		// acknowledging a command queue. The final post-pipeline reply is enough.
	case result.Accepted:
		// See above: no generic "start processing" acknowledgement.
	}
	return result, nil
}

func (r *Runtime) deferProcessingAck(eventRowID string) {
	if r == nil || eventRowID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pendingAckByEvent[eventRowID] = struct{}{}
}

func (r *Runtime) sendDeferredProcessingAck(ctx context.Context, eventRowID string, evt port.InboundEvent) {
	if r == nil || eventRowID == "" {
		return
	}
	r.mu.Lock()
	_, ok := r.pendingAckByEvent[eventRowID]
	if ok {
		delete(r.pendingAckByEvent, eventRowID)
	}
	r.mu.Unlock()
	if !ok {
		return
	}
	r.send(ctx, evt, "好的，开始处理。")
}

func (r *Runtime) discardDeferredProcessingAck(eventRowID string) {
	if r == nil || eventRowID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.pendingAckByEvent, eventRowID)
}

func (r *Runtime) workerLoop(ctx context.Context, workerID string) {
	for {
		if ctx.Err() != nil {
			return
		}
		rec, err := r.cfg.Store.ClaimNext(ctx, workerID)
		if err != nil {
			slog.Error("channel inbound runtime: claim failed", "worker", workerID, "error", err)
			sleepWithContext(ctx, r.cfg.PollInterval)
			continue
		}
		if rec == nil {
			sleepWithContext(ctx, r.cfg.PollInterval)
			continue
		}
		if err := r.processRecord(ctx, rec); err != nil {
			slog.Error("channel inbound runtime: process failed",
				"event_row_id", rec.ID,
				"event_id", rec.Event.EventID,
				"phase", rec.Phase,
				"error", err,
			)
			result, markErr := r.cfg.Store.MarkRetry(ctx, rec.ID, err)
			if markErr != nil {
				slog.Error("channel inbound runtime: mark retry failed", "event_row_id", rec.ID, "error", markErr)
			} else if result.Dead {
				r.completeTurnAfterFailure(ctx, rec, channelconversation.TurnStatusDead, err)
				r.sendFailureOnce(ctx, rec, failureCodeInboundDead, "处理失败了，这条消息我先停止处理，请稍后重试。", false)
			} else {
				r.completeTurnAfterFailure(ctx, rec, channelconversation.TurnStatusFailed, err)
			}
		}
	}
}

func (r *Runtime) processRecord(ctx context.Context, rec *InboundEventRecord) error {
	for {
		switch rec.Phase {
		case InboundPhasePre:
			next, outcome, err := r.runPipeline(ctx, r.cfg.PrePipeline, rec.Event)
			if err != nil {
				return err
			}
			if outcome.Decision == DecisionSkip {
				r.discardDeferredProcessingAck(rec.ID)
				return r.cfg.Store.MarkProcessed(ctx, rec.ID)
			}
			chatCtx := r.lookupChatContext(ctx, next)
			if err := r.cfg.Store.SaveEvent(ctx, rec.ID, next, InboundPhaseIntent, chatCtx); err != nil {
				return err
			}
			r.sendDeferredProcessingAck(ctx, rec.ID, next)
			rec.Event = next
			rec.Phase = InboundPhaseIntent
			rec.WorkspaceID = chatCtx.WorkspaceID
			rec.DefaultProjectID = chatCtx.DefaultProjectID

		case InboundPhaseIntent:
			waiting, err := r.resolveIntent(ctx, rec)
			if err != nil || waiting {
				return err
			}

		case InboundPhasePost:
			_, outcome, err := r.runPipeline(ctx, r.cfg.PostPipeline, rec.Event)
			if err != nil {
				return err
			}
			if outcome.Decision == DecisionSkip || outcome.Decision == DecisionContinue {
				return r.cfg.Store.MarkProcessed(ctx, rec.ID)
			}

		case InboundPhaseDone:
			return r.cfg.Store.MarkProcessed(ctx, rec.ID)

		default:
			return fmt.Errorf("unknown inbound phase %q", rec.Phase)
		}
	}
}

func (r *Runtime) resolveIntent(ctx context.Context, rec *InboundEventRecord) (bool, error) {
	evt := rec.Event
	chatCtx := r.lookupChatContext(ctx, evt)
	if chatCtx.WorkspaceID == "" {
		chatCtx.WorkspaceID = rec.WorkspaceID
		chatCtx.DefaultProjectID = rec.DefaultProjectID
	}
	if evt.Type != port.EventTypeMessageReceived {
		if err := r.cfg.Store.SaveEvent(ctx, rec.ID, evt, InboundPhasePost, chatCtx); err != nil {
			return false, err
		}
		rec.Phase = InboundPhasePost
		return false, nil
	}

	req := r.buildIntentRequest(ctx, rec, evt, &chatCtx)
	if result, ok, err := r.resolveMessageContextReply(ctx, rec, evt); err != nil || ok {
		if err != nil {
			return false, err
		}
		return r.applyIntentResult(ctx, rec, result, chatCtx, false)
	}

	if isDeterministicChannelInput(evt, req) {
		result, ok, err := r.resolveByRules(ctx, req)
		if err != nil {
			return false, err
		}
		if ok {
			result = applyRequestContextToIntentResult(result, req)
			return r.applyIntentResult(ctx, rec, result, chatCtx, false)
		}
		return r.applyIntentResult(ctx, rec, fallbackRuleUnknown(), chatCtx, false)
	}

	if r.cfg.ChannelTurn == nil || chatCtx.WorkspaceID == "" {
		result, ok, err := r.resolveByRules(ctx, req)
		if err != nil {
			return false, err
		}
		if ok {
			result = applyRequestContextToIntentResult(result, req)
			return r.applyIntentResult(ctx, rec, result, chatCtx, false)
		}
		return r.applyIntentResult(ctx, rec, fallbackRuleUnknown(), chatCtx, false)
	}

	taskID, err := r.cfg.ChannelTurn.StartAgentTurn(ctx, req)
	if err != nil {
		slog.Warn("channel inbound runtime: start channel turn failed", "event_row_id", rec.ID, "error", err)
		r.sendFailureOnce(ctx, rec, failureCodeNoChannelAgent, userMessageForChannelAgentError(err), true)
		return true, nil
	}
	if err := r.cfg.Store.MarkWaitingAgent(ctx, rec.ID, evt, taskID, chatCtx, WaitKindChannelTurn); err != nil {
		return false, err
	}
	r.recordIntentTurn(ctx, rec, evt, chintent.Intent{}, chatCtx, channelconversation.TurnStatusWaitingAgent, WaitKindChannelTurn, taskID)
	return true, nil
}

type userVisibleError interface {
	UserMessage() string
}

func userMessageForChannelAgentError(err error) string {
	const fallback = "我现在找不到可用的 channel agent，先不继续刷屏。等 agent 恢复后你可以再发一次。"
	var visible userVisibleError
	if errors.As(err, &visible) {
		if msg := strings.TrimSpace(visible.UserMessage()); msg != "" {
			return msg
		}
	}
	return fallback
}

func isDeterministicChannelInput(evt port.InboundEvent, req chintent.IntentRequest) bool {
	if req.SourceHint == chintent.SourceCommand {
		return true
	}
	return strings.HasPrefix(strings.TrimSpace(evt.Text), "/")
}

func (r *Runtime) resolveByRules(ctx context.Context, req chintent.IntentRequest) (chintent.IntentResult, bool, error) {
	for _, resolver := range r.cfg.RuleResolvers {
		if resolver == nil {
			continue
		}
		result, err := resolver.Resolve(ctx, req)
		if err != nil {
			return chintent.IntentResult{}, false, err
		}
		if result.Matched {
			return result, true, nil
		}
	}
	return chintent.IntentResult{}, false, nil
}

func fallbackRuleUnknown() chintent.IntentResult {
	return chintent.IntentResult{
		Matched: true,
		Intent: chintent.Intent{
			Kind:       chintent.IntentUnknown,
			Confidence: 0,
			Params:     map[string]string{},
			Source:     chintent.SourceRule,
		},
	}
}

func (r *Runtime) applyIntentResult(ctx context.Context, rec *InboundEventRecord, result chintent.IntentResult, chatCtx ChatBindingContext, requeue bool) (bool, error) {
	evt := rec.Event
	evt.Intent = toPortIntent(result.Intent)
	applyDefaultProject(&evt, chatCtx)
	if requeue {
		if err := r.cfg.Store.MarkQueued(ctx, rec.ID, evt, InboundPhasePost, chatCtx); err != nil {
			return false, err
		}
	} else {
		if err := r.cfg.Store.SaveEvent(ctx, rec.ID, evt, InboundPhasePost, chatCtx); err != nil {
			return false, err
		}
	}
	rec.Event = evt
	rec.Phase = InboundPhasePost
	r.recordIntentTurn(ctx, rec, evt, result.Intent, chatCtx, channelconversation.TurnStatusProcessing, "", "")
	return false, nil
}

func (r *Runtime) recordIntentTurn(ctx context.Context, rec *InboundEventRecord, evt port.InboundEvent, intent chintent.Intent, chatCtx ChatBindingContext, status, waitKind, waitTaskID string) {
	if r == nil || r.cfg.ConversationStore == nil || rec == nil || strings.TrimSpace(rec.ID) == "" {
		return
	}
	inboundMsg, ok, err := r.cfg.ConversationStore.FindMessageByInboundEventID(ctx, rec.ID)
	if err != nil || !ok {
		if err != nil {
			slog.Error("channel inbound runtime: lookup inbound message for turn failed", "event_row_id", rec.ID, "error", err)
		}
		return
	}
	payload, err := json.Marshal(intent)
	if err != nil {
		slog.Error("channel inbound runtime: marshal turn intent failed", "event_row_id", rec.ID, "error", err)
		return
	}
	if status == "" {
		status = channelconversation.TurnStatusProcessing
	}
	workspaceID := firstNonEmpty(chatCtx.WorkspaceID, rec.WorkspaceID, inboundMsg.WorkspaceID)
	if _, err := r.cfg.ConversationStore.UpsertTurn(ctx, channelconversation.Turn{
		Provider:         evt.ChannelName,
		ConnectionID:     evt.ConnectionID(),
		ConversationID:   inboundMsg.ConversationID,
		WorkspaceID:      workspaceID,
		InboundEventID:   rec.ID,
		InboundMessageID: inboundMsg.ID,
		SenderExternalID: evt.SenderID,
		IntentKind:       string(intent.Kind),
		IntentSource:     string(intent.Source),
		IntentPayload:    payload,
		Status:           status,
		WaitKind:         waitKind,
		WaitTaskID:       waitTaskID,
		StartedAt:        time.Now().UTC(),
	}); err != nil {
		slog.Error("channel inbound runtime: upsert channel turn failed", "event_row_id", rec.ID, "error", err)
	}
}

func (r *Runtime) resolveMessageContextReply(ctx context.Context, rec *InboundEventRecord, evt port.InboundEvent) (chintent.IntentResult, bool, error) {
	if r == nil || r.cfg.ConversationStore == nil || rec == nil || evt.Type != port.EventTypeMessageReceived {
		return chintent.IntentResult{}, false, nil
	}
	action, ok := classifyShortContextReply(evt.Text)
	if !ok {
		return chintent.IntentResult{}, false, nil
	}
	target, ok, err := r.contextTargetMessage(ctx, rec, evt)
	if err != nil || !ok {
		return chintent.IntentResult{}, ok, err
	}
	if !isContextReplyTarget(target) {
		return chintent.IntentResult{}, false, nil
	}
	refs, err := r.cfg.ConversationStore.ListEntityRefsByMessageID(ctx, target.ID)
	if err != nil {
		return chintent.IntentResult{}, true, err
	}
	issueKey := contextIssueKey(target, refs)
	if issueKey == "" {
		return chintent.IntentResult{}, false, nil
	}
	comment := composeContextReplyComment(action, evt.Text, contextAgentMention(refs))
	return chintent.IntentResult{
		Matched: true,
		Intent: chintent.Intent{
			Kind:       chintent.IntentAddComment,
			Confidence: 1,
			Source:     chintent.SourceRule,
			Params: map[string]string{
				"issue_key":                   issueKey,
				"comment":                     comment,
				"_channel_context_message_id": target.ID,
			},
		},
	}, true, nil
}

func (r *Runtime) contextTargetMessage(ctx context.Context, rec *InboundEventRecord, evt port.InboundEvent) (channelconversation.Message, bool, error) {
	hasExplicitContext := false
	for _, platformMessageID := range []string{evt.QuotedMessageID, evt.ReplyToMessageID} {
		if strings.TrimSpace(platformMessageID) != "" {
			hasExplicitContext = true
		}
		msg, ok, err := r.cfg.ConversationStore.FindMessageByPlatformID(ctx, evt.ConnectionID(), platformMessageID)
		if err != nil || ok {
			return msg, ok, err
		}
	}
	if hasExplicitContext {
		return channelconversation.Message{}, false, nil
	}
	inboundMsg, ok, err := r.cfg.ConversationStore.FindMessageByInboundEventID(ctx, rec.ID)
	if err != nil || !ok {
		return channelconversation.Message{}, false, err
	}
	recent, err := r.cfg.ConversationStore.ListRecentHandoffMessages(ctx, evt.ConnectionID(), inboundMsg.ConversationID, evt.ThreadID, time.Now().Add(-30*time.Minute), 2)
	if err != nil || len(recent) != 1 {
		return channelconversation.Message{}, false, err
	}
	return recent[0], true, nil
}

func isContextReplyTarget(msg channelconversation.Message) bool {
	if msg.HandoffKind == "" || msg.HandoffKind == channelconversation.HandoffKindNone {
		return false
	}
	if msg.Direction != "" && msg.Direction != channelconversation.DirectionOutbound {
		return false
	}
	switch msg.MessageType {
	case channelconversation.MessageTypeAgent, channelconversation.MessageTypeBot, channelconversation.MessageTypeNotification:
		return true
	default:
		return false
	}
}

type contextReplyAction string

const (
	contextReplyApprove  contextReplyAction = "approve"
	contextReplyContinue contextReplyAction = "continue"
	contextReplyRetry    contextReplyAction = "retry"
)

func classifyShortContextReply(text string) (contextReplyAction, bool) {
	clean := strings.Trim(strings.ToLower(strings.TrimSpace(text)), " \t\r\n.,，。!！?？")
	if clean == "" || len([]rune(clean)) > 32 {
		return "", false
	}
	switch {
	case clean == "重试" || clean == "retry" || clean == "再试一次" || clean == "再跑一次" || clean == "重跑" || strings.HasPrefix(clean, "重试 "):
		return contextReplyRetry, true
	case clean == "继续" || clean == "继续推进" || clean == "推进" || clean == "go" || clean == "go ahead":
		return contextReplyContinue, true
	case clean == "同意" || clean == "可以" || clean == "批准" || clean == "通过" || clean == "ok" || clean == "okay" || clean == "yes" || clean == "y" ||
		strings.HasPrefix(clean, "ok ") || strings.HasPrefix(clean, "同意 "):
		return contextReplyApprove, true
	default:
		return "", false
	}
}

func contextIssueKey(msg channelconversation.Message, refs []channelconversation.EntityRef) string {
	for _, ref := range refs {
		if ref.EntityType == channelconversation.EntityTypeIssue && strings.TrimSpace(ref.EntityKey) != "" {
			return strings.ToUpper(strings.TrimSpace(ref.EntityKey))
		}
	}
	if key := singleExtractedIssueKey(msg.Text); key != "" {
		return key
	}
	if key := singleExtractedIssueKey(string(msg.Body)); key != "" {
		return key
	}
	return ""
}

func contextAgentMention(refs []channelconversation.EntityRef) string {
	for _, ref := range refs {
		if ref.EntityType != channelconversation.EntityTypeAgent ||
			ref.Role != channelconversation.EntityRoleHandoffTarget ||
			strings.TrimSpace(ref.EntityID) == "" {
			continue
		}
		label := strings.TrimSpace(ref.Display)
		if label == "" {
			label = "Agent"
		}
		label = strings.TrimPrefix(label, "@")
		return fmt.Sprintf("[@%s](mention://agent/%s)", label, strings.TrimSpace(ref.EntityID))
	}
	return ""
}

func composeContextReplyComment(action contextReplyAction, rawText, agentMention string) string {
	text := strings.TrimSpace(rawText)
	if text == "" {
		switch action {
		case contextReplyRetry:
			text = "重试一次"
		case contextReplyContinue:
			text = "继续推进"
		default:
			text = "同意"
		}
	}
	if action == contextReplyRetry && (text == "重试" || strings.EqualFold(text, "retry")) {
		text = "重试一次"
	}
	if action == contextReplyContinue && text == "继续" {
		text = "继续推进"
	}
	if agentMention != "" && !strings.Contains(text, "mention://agent/") {
		text = strings.TrimSpace(text + " " + agentMention)
	}
	return text
}

func (r *Runtime) completeTurnAfterFailure(ctx context.Context, rec *InboundEventRecord, status string, runErr error) {
	if r == nil || r.cfg.ConversationStore == nil || rec == nil || strings.TrimSpace(rec.ID) == "" {
		return
	}
	payload, _ := json.Marshal(struct {
		Error string `json:"error,omitempty"`
	}{Error: truncateErr(runErr)})
	if err := r.cfg.ConversationStore.CompleteTurnForInboundEvent(ctx, rec.ID, "", status, payload, truncateErr(runErr)); err != nil {
		slog.Error("channel inbound runtime: complete failed turn failed", "event_row_id", rec.ID, "status", status, "error", err)
	}
}

func (r *Runtime) resumeLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.resumeWaitingAgents(ctx)
		}
	}
}

func (r *Runtime) resumeWaitingAgents(ctx context.Context) {
	items, err := r.cfg.Store.ListWaitingAgent(ctx, r.cfg.ClaimBatch)
	if err != nil {
		slog.Error("channel inbound runtime: list waiting agents failed", "error", err)
		return
	}
	for _, item := range items {
		if item.WaitTaskID == "" {
			err := errors.New("waiting agent event has no task id")
			_ = r.cfg.Store.MarkDead(ctx, item.ID, err)
			r.completeTurnAfterFailure(ctx, &InboundEventRecord{ID: item.ID}, channelconversation.TurnStatusDead, err)
			continue
		}
		timeout := r.cfg.IntentTaskTimeout
		if item.WaitKind == WaitKindAction {
			timeout = r.cfg.ActionTaskTimeout
		} else if item.WaitKind == WaitKindChannelTurn {
			timeout = r.cfg.ActionTaskTimeout
		}
		if time.Since(item.UpdatedAt) > timeout {
			rec, _ := r.cfg.Store.Load(ctx, item.ID)
			err := fmt.Errorf("channel %s task timed out after %s", item.WaitKind, timeout)
			if markErr := r.cfg.Store.MarkDead(ctx, item.ID, err); markErr != nil {
				slog.Error("channel inbound runtime: mark timed-out event dead failed", "event_row_id", item.ID, "error", markErr)
			}
			r.completeTurnAfterFailure(ctx, &InboundEventRecord{ID: item.ID}, channelconversation.TurnStatusDead, err)
			if rec != nil {
				r.sendFailureOnce(ctx, rec, failureCodeChannelTurnTimeout, "处理超时了，这条消息我先停止处理，请稍后重试。", false)
			}
			continue
		}
		if item.WaitKind == WaitKindChannelTurn {
			r.resumeChannelTurn(ctx, item)
			continue
		}
		if item.WaitKind != WaitKindIntent {
			continue
		}
		if r.cfg.TurnPlanner == nil {
			continue
		}
		result, done, err := r.cfg.TurnPlanner.ParseTurnResult(ctx, item.WaitTaskID)
		if !done {
			continue
		}
		rec, loadErr := r.cfg.Store.Load(ctx, item.ID)
		if loadErr != nil {
			slog.Error("channel inbound runtime: load waiting event failed", "event_row_id", item.ID, "error", loadErr)
			continue
		}
		if err != nil {
			chatCtx := r.lookupChatContext(ctx, rec.Event)
			if chatCtx.WorkspaceID == "" {
				chatCtx.WorkspaceID = rec.WorkspaceID
				chatCtx.DefaultProjectID = rec.DefaultProjectID
			}
			req := r.buildIntentRequest(ctx, rec, rec.Event, &chatCtx)
			if result, ok, ruleErr := r.resolveByRules(ctx, req); ruleErr != nil {
				slog.Error("channel inbound runtime: fallback rule intent failed", "event_row_id", item.ID, "error", ruleErr)
			} else if ok {
				result = applyRequestContextToIntentResult(result, req)
				if _, applyErr := r.applyIntentResult(ctx, rec, result, chatCtx, true); applyErr != nil {
					slog.Error("channel inbound runtime: apply fallback rule intent failed", "event_row_id", item.ID, "error", applyErr)
				}
				continue
			}
			if _, applyErr := r.applyIntentResult(ctx, rec, fallbackRuleUnknown(), chatCtx, true); applyErr != nil {
				slog.Error("channel inbound runtime: apply fallback unknown failed", "event_row_id", item.ID, "error", applyErr)
			}
			continue
		}
		chatCtx := r.lookupChatContext(ctx, rec.Event)
		if chatCtx.WorkspaceID == "" {
			chatCtx.WorkspaceID = rec.WorkspaceID
			chatCtx.DefaultProjectID = rec.DefaultProjectID
		}
		req := r.buildIntentRequest(ctx, rec, rec.Event, &chatCtx)
		result = applyRequestContextToIntentResult(result, req)
		if _, err := r.applyIntentResult(ctx, rec, result, chatCtx, true); err != nil {
			slog.Error("channel inbound runtime: resume intent failed", "event_row_id", item.ID, "error", err)
		}
	}
}

func (r *Runtime) resumeChannelTurn(ctx context.Context, item WaitingAgentEvent) {
	if r.cfg.ChannelTurn == nil {
		return
	}
	reply, done, err := r.cfg.ChannelTurn.ParseAgentTurnResult(ctx, item.WaitTaskID)
	if !done {
		return
	}
	rec, loadErr := r.cfg.Store.Load(ctx, item.ID)
	if loadErr != nil {
		slog.Error("channel inbound runtime: load channel turn event failed", "event_row_id", item.ID, "error", loadErr)
		return
	}
	if err != nil {
		msg := "这次 channel agent 没能处理成功，请稍后重试。"
		if strings.TrimSpace(err.Error()) != "" {
			slog.Warn("channel inbound runtime: channel turn failed", "event_row_id", item.ID, "task_id", item.WaitTaskID, "error", err)
		}
		r.completeTurnAfterFailure(ctx, rec, channelconversation.TurnStatusDead, err)
		r.sendFailureOnce(ctx, rec, failureCodeChannelTurnFailed, msg, true)
		return
	}
	if strings.TrimSpace(reply) == "" {
		r.completeTurnAfterFailure(ctx, rec, channelconversation.TurnStatusDead, errors.New("channel turn returned empty reply"))
		r.sendFailureOnce(ctx, rec, failureCodeChannelTurnEmpty, "我这边没有拿到有效回复，请再发一次。", true)
		return
	}
	r.persistAndSendTurnReply(ctx, rec, strings.TrimSpace(reply))
}

func (r *Runtime) persistAndSendTurnReply(ctx context.Context, rec *InboundEventRecord, reply string) {
	if rec == nil {
		return
	}
	if r.cfg.DispatchStore != nil {
		if saved, ok, err := r.cfg.DispatchStore.GetDispatchCompletion(ctx, rec.ID); err == nil && ok {
			if saved != "" {
				slog.Info("channel inbound runtime: channel turn completion already sent", "event_row_id", rec.ID)
			}
			if err := r.cfg.Store.MarkProcessed(ctx, rec.ID); err != nil {
				slog.Error("channel inbound runtime: mark completed channel turn processed failed", "event_row_id", rec.ID, "error", err)
			}
			return
		} else if err != nil {
			slog.Error("channel inbound runtime: load channel turn completion failed", "event_row_id", rec.ID, "error", err)
		} else if markErr := r.cfg.DispatchStore.MarkDispatchCompleted(ctx, rec.ID, reply); markErr != nil {
			slog.Error("channel inbound runtime: persist channel turn completion failed", "event_row_id", rec.ID, "error", markErr)
		}
	}
	r.send(ctx, rec.Event, reply)
	r.appendTurnContextEntities(ctx, rec, reply)
	if err := r.cfg.Store.MarkProcessed(ctx, rec.ID); err != nil {
		slog.Error("channel inbound runtime: mark channel turn processed failed", "event_row_id", rec.ID, "error", err)
	}
}

func (r *Runtime) appendTurnContextEntities(ctx context.Context, rec *InboundEventRecord, reply string) {
	if r == nil || r.cfg.ConversationCtx == nil || rec == nil {
		return
	}
	entities := conversationctx.ExtractEntityKeys(reply)
	if len(entities) == 0 {
		return
	}
	workspaceID := strings.TrimSpace(rec.WorkspaceID)
	if workspaceID == "" {
		workspaceID = strings.TrimSpace(r.lookupChatContext(ctx, rec.Event).WorkspaceID)
	}
	if workspaceID == "" {
		slog.Debug("channel inbound runtime: skip conversation context write without workspace", "event_row_id", rec.ID)
		return
	}
	scope := conversationctx.Scope{
		ConnectionID: rec.Event.ConnectionID(),
		WorkspaceID:  workspaceID,
		ChatID:       rec.Event.ChatID,
		SenderID:     rec.Event.SenderID,
		ThreadID:     rec.Event.ThreadID,
	}
	if err := r.cfg.ConversationCtx.AppendEntities(ctx, scope, entities, r.cfg.ContextMaxEntities, r.cfg.ContextTTL); err != nil {
		slog.Error("channel inbound runtime: append conversation context entities failed", "event_row_id", rec.ID, "error", err)
	}
}

func (r *Runtime) sendFailureOnce(ctx context.Context, rec *InboundEventRecord, code, reply string, markProcessed bool) {
	if rec == nil {
		return
	}
	if r.cfg.DispatchStore != nil {
		if _, ok, err := r.cfg.DispatchStore.GetDispatchCompletion(ctx, rec.ID); err == nil && ok {
			r.markFailureTerminal(ctx, rec.ID, markProcessed)
			return
		} else if err != nil {
			slog.Error("channel inbound runtime: load failure completion failed", "event_row_id", rec.ID, "failure_code", code, "error", err)
		}
	}
	shouldSend := true
	if r.cfg.FailureLimiter != nil {
		key := FailureNoticeKey{
			ConnectionID: rec.Event.ConnectionID(),
			ChatID:       rec.Event.ChatID,
			SenderID:     rec.Event.SenderID,
			Code:         code,
		}
		ok, err := r.cfg.FailureLimiter.ShouldSendFailureNotice(ctx, key, r.cfg.FailureNoticeCooldown)
		if err != nil {
			slog.Error("channel inbound runtime: failure limiter failed", "event_row_id", rec.ID, "failure_code", code, "error", err)
		} else {
			shouldSend = ok
		}
	}
	persistedReply := reply
	if !shouldSend {
		persistedReply = ""
		slog.Warn("channel inbound runtime: suppressing repeated failure notice",
			"event_row_id", rec.ID,
			"failure_code", code,
			"connection_id", rec.Event.ConnectionID(),
			"chat_id", rec.Event.ChatID,
			"sender_id", rec.Event.SenderID,
		)
	}
	if r.cfg.DispatchStore != nil {
		if err := r.cfg.DispatchStore.MarkDispatchCompleted(ctx, rec.ID, persistedReply); err != nil {
			slog.Error("channel inbound runtime: persist failure completion failed", "event_row_id", rec.ID, "failure_code", code, "error", err)
		}
	}
	if shouldSend {
		r.send(ctx, rec.Event, reply)
	}
	r.markFailureTerminal(ctx, rec.ID, markProcessed)
}

func (r *Runtime) markFailureTerminal(ctx context.Context, eventRowID string, markProcessed bool) {
	if !markProcessed || eventRowID == "" {
		return
	}
	if err := r.cfg.Store.MarkProcessed(ctx, eventRowID); err != nil {
		slog.Error("channel inbound runtime: mark failed event processed failed", "event_row_id", eventRowID, "error", err)
	}
}

func (r *Runtime) sweeperLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.sweepOnce(ctx)
		}
	}
}

func (r *Runtime) sweepOnce(ctx context.Context) {
	n, err := r.cfg.Store.RequeueStaleProcessing(ctx, r.cfg.ProcessingLease)
	if err != nil {
		slog.Error("channel inbound runtime: requeue stale processing failed", "error", err)
	} else if n > 0 {
		slog.Warn("channel inbound runtime: requeued stale processing events", "count", n)
	}
	expired, err := r.cfg.Store.ExpireWaitingUser(ctx, r.cfg.ClaimBatch)
	if err != nil {
		slog.Error("channel inbound runtime: expire waiting_user failed", "error", err)
	}
	for _, item := range expired {
		r.send(ctx, item.Event, "长时间没有补充信息，已停止处理，请重新发送完整需求。")
	}
	if r.cfg.ConversationCtx == nil {
		return
	}
	deleted, err := r.cfg.ConversationCtx.DeleteExpired(ctx, time.Now())
	if err != nil {
		slog.Error("channel inbound runtime: delete expired conversation context failed", "error", err)
	} else if deleted > 0 {
		slog.Debug("channel inbound runtime: deleted expired conversation contexts", "count", deleted)
	}
}

func (r *Runtime) runPipeline(ctx context.Context, pipeline *Pipeline, evt port.InboundEvent) (port.InboundEvent, Outcome, error) {
	if pipeline == nil {
		return evt, Outcome{Decision: DecisionContinue}, nil
	}
	return pipeline.RunEvent(ctx, evt)
}

func (r *Runtime) lookupChatContext(ctx context.Context, evt port.InboundEvent) ChatBindingContext {
	chatCtx, err := r.cfg.Store.LookupChatContext(ctx, evt.ConnectionID(), evt.ChatID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		slog.Error("channel inbound runtime: lookup chat context failed",
			"channel", evt.ChannelName,
			"chat_id", evt.ChatID,
			"error", err,
		)
	}
	return chatCtx
}

func (r *Runtime) buildIntentRequest(ctx context.Context, rec *InboundEventRecord, evt port.InboundEvent, chatCtx *ChatBindingContext) chintent.IntentRequest {
	req := chintent.IntentRequest{
		Text:           evt.Text,
		Channel:        evt.ChannelName,
		ConnectionID:   evt.ConnectionID(),
		ChatID:         evt.ChatID,
		ChatType:       string(evt.ChatType),
		SenderID:       evt.SenderID,
		SenderName:     evt.SenderName,
		InboundEventID: rec.ID,
		SourceHint:     chintent.IntentSource(evt.Intent.Source),
	}
	if chatCtx != nil {
		req.WorkspaceID = chatCtx.WorkspaceID
		req.DefaultProjectID = chatCtx.DefaultProjectID
		req.AgentID = strings.TrimSpace(chatCtx.AgentID)
	}
	return r.applyReplyContext(ctx, req, evt, chatCtx)
}

func (r *Runtime) applyReplyContext(ctx context.Context, req chintent.IntentRequest, evt port.InboundEvent, chatCtx *ChatBindingContext) chintent.IntentRequest {
	// Always pass through explicit signals from the inbound event, regardless
	// of whether a reply-context store is configured.
	req.ThreadID = evt.ThreadID
	req.QuotedMessageID = evt.QuotedMessageID
	req.QuotedText = evt.QuotedText
	req.ReplyToMessageID = evt.ReplyToMessageID
	req = r.applyConversationContext(ctx, req, evt)

	if r.cfg.ReplyContext == nil || evt.ChatType != port.ChatTypeDirect {
		return req
	}
	rc, ok, err := r.cfg.ReplyContext.Lookup(ctx, evt.ConnectionID(), evt.SenderID, evt.ChatID, time.Now())
	if err != nil {
		slog.Error("channel inbound runtime: lookup reply context failed",
			"connection_id", evt.ConnectionID(),
			"sender_id", evt.SenderID,
			"error", err,
		)
		return req
	}
	if !ok {
		return req
	}
	if chatCtx != nil && chatCtx.WorkspaceID == "" && rc.WorkspaceID.Valid {
		chatCtx.WorkspaceID = util.UUIDToString(rc.WorkspaceID)
		req.WorkspaceID = chatCtx.WorkspaceID
	}
	if req.ContextIssueKey == "" && strings.TrimSpace(rc.IssueIdentifier) != "" {
		req.ContextIssueKey = strings.TrimSpace(rc.IssueIdentifier)
		req.ContextMode = "reply"
	}
	return req
}

func (r *Runtime) applyConversationContext(ctx context.Context, req chintent.IntentRequest, evt port.InboundEvent) chintent.IntentRequest {
	explicitEntities := conversationctx.ExtractEntityKeys(evt.QuotedText)
	req.ExplicitEntities = mergeContextEntities(req.ExplicitEntities, explicitEntities, r.cfg.ContextMaxEntities)
	var entities []conversationctx.EntityRef
	if r.cfg.ConversationCtx != nil && strings.TrimSpace(req.WorkspaceID) != "" {
		scope := conversationctx.Scope{
			ConnectionID: evt.ConnectionID(),
			WorkspaceID:  req.WorkspaceID,
			ChatID:       evt.ChatID,
			SenderID:     evt.SenderID,
			ThreadID:     evt.ThreadID,
		}
		if cc, ok, err := r.cfg.ConversationCtx.Get(ctx, scope); err != nil {
			slog.Error("channel inbound runtime: lookup conversation context failed",
				"connection_id", evt.ConnectionID(),
				"workspace_id", req.WorkspaceID,
				"chat_id", evt.ChatID,
				"sender_id", evt.SenderID,
				"thread_id", evt.ThreadID,
				"error", err,
			)
		} else if ok {
			entities = cc.Entities
		}
	}
	req.ContextEntities = mergeContextEntities(req.ContextEntities, entities, r.cfg.ContextMaxEntities)
	if req.ContextIssueKey == "" {
		if key := requestContextIssueKey(req); key != "" {
			req.ContextIssueKey = key
			req.ContextMode = "conversation"
		}
	}
	return req
}

func mergeContextEntities(existing []conversationctx.EntityRef, incoming []conversationctx.EntityRef, max int) []conversationctx.EntityRef {
	if len(existing) == 0 && len(incoming) == 0 {
		return nil
	}
	merged := make([]conversationctx.EntityRef, 0, len(existing)+len(incoming))
	seen := make(map[string]int, len(existing)+len(incoming))
	for _, entity := range append(existing, incoming...) {
		entity.Key = strings.ToUpper(strings.TrimSpace(entity.Key))
		if entity.Key == "" {
			continue
		}
		if entity.Type == "" {
			entity.Type = conversationctx.EntityTypeIssue
		}
		if i, ok := seen[entity.Key]; ok {
			if entity.MentionedAt.After(merged[i].MentionedAt) {
				merged[i].MentionedAt = entity.MentionedAt
			}
			if entity.ID != "" {
				merged[i].ID = entity.ID
			}
			if entity.Display != "" {
				merged[i].Display = entity.Display
			}
			if entity.URL != "" {
				merged[i].URL = entity.URL
			}
			continue
		}
		seen[entity.Key] = len(merged)
		merged = append(merged, entity)
	}
	if max > 0 && len(merged) > max {
		merged = merged[:max]
	}
	return merged
}

func requestContextIssueKey(req chintent.IntentRequest) string {
	if key := singleExtractedIssueKey(req.QuotedText); key != "" {
		return key
	}
	if len(req.ExplicitEntities) == 1 {
		entity := req.ExplicitEntities[0]
		if entity.Type == "" || entity.Type == conversationctx.EntityTypeIssue {
			return strings.ToUpper(strings.TrimSpace(entity.Key))
		}
	}
	if len(req.ExplicitEntities) > 1 {
		return ""
	}
	if len(req.ContextEntities) != 1 {
		return ""
	}
	entity := req.ContextEntities[0]
	if entity.Type != "" && entity.Type != conversationctx.EntityTypeIssue {
		return ""
	}
	return strings.ToUpper(strings.TrimSpace(entity.Key))
}

func singleExtractedIssueKey(text string) string {
	entities := conversationctx.ExtractEntityKeys(text)
	if len(entities) != 1 {
		return ""
	}
	return strings.ToUpper(strings.TrimSpace(entities[0].Key))
}

func applyRequestContextToIntentResult(result chintent.IntentResult, req chintent.IntentRequest) chintent.IntentResult {
	if !result.Matched || !intentCanUseRequestContextIssue(result.Intent) {
		return result
	}
	key := strings.TrimSpace(req.ContextIssueKey)
	if key == "" {
		key = requestContextIssueKey(req)
	}
	if key == "" {
		return result
	}
	if result.Intent.Params == nil {
		result.Intent.Params = map[string]string{}
	}
	if strings.TrimSpace(result.Intent.Params["issue_key"]) == "" {
		result.Intent.Params["issue_key"] = strings.ToUpper(key)
	}
	if result.Intent.Kind == chintent.IntentQueryProgress && strings.TrimSpace(result.Intent.Params["scope"]) == "" {
		result.Intent.Params["scope"] = "issue"
	}
	return result
}

func intentCanUseRequestContextIssue(in chintent.Intent) bool {
	if strings.TrimSpace(in.Params["issue_key"]) != "" {
		return false
	}
	switch in.Kind {
	case chintent.IntentAddComment,
		chintent.IntentIssueDetail,
		chintent.IntentIssueTimeline,
		chintent.IntentIssueLogs,
		chintent.IntentSetStatus,
		chintent.IntentSetAssignee,
		chintent.IntentSetPriority,
		chintent.IntentSetLabel:
		return true
	case chintent.IntentQueryProgress:
		scope := strings.TrimSpace(in.Params["scope"])
		return scope == "" || scope == "issue"
	default:
		return false
	}
}

func (r *Runtime) send(ctx context.Context, evt port.InboundEvent, text string) {
	if r.cfg.ReplySink == nil || text == "" {
		return
	}
	if err := r.cfg.ReplySink.SendText(ctx, evt, port.OutboundMessage{Text: text}); err != nil {
		slog.Error("channel inbound runtime: send reply failed",
			"channel", evt.ChannelName,
			"chat_id", evt.ChatID,
			"event_id", evt.EventID,
			"error", err,
		)
	}
}

func applyDefaultProject(evt *port.InboundEvent, chatCtx ChatBindingContext) {
	if evt == nil || evt.Intent.Kind != port.IntentCreateIssue || chatCtx.DefaultProjectID == "" {
		return
	}
	if evt.Intent.Params == nil {
		evt.Intent.Params = map[string]string{}
	}
	if evt.Intent.Params["project_id"] == "" {
		evt.Intent.Params["project_id"] = chatCtx.DefaultProjectID
	}
}

func sleepWithContext(ctx context.Context, d time.Duration) {
	if d <= 0 {
		d = defaultInboundPollInterval
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
