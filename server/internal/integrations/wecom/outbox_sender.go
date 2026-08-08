package wecom

// outbox_sender.go — the WeCom half of the durable outbound queue. The queue
// itself (claim/lease, backoff, dead letter, reconcile) is channel-agnostic
// and lives in channel/outbox; everything platform-specific is here: the
// payload document, how it renders to aibot markdown, and which of WeCom's
// errcodes are worth retrying.
//
// Why this exists at all: aibot has no outbound REST path, so a reply can only
// be written by the replica holding that bot's WebSocket. The reply itself is
// produced wherever the agent task ran. queueSender is the far end of that
// handoff — it runs on the lease holder, reading rows any replica may have
// enqueued.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/multica-ai/multica/server/internal/integrations/channel/outbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Source kinds written onto the queue. These are the business keys' first
// component, so renaming one silently un-deduplicates every in-flight row of
// that kind — and, for the two task-derived kinds, must stay in sync with
// reconcileBuilder.SourceKinds.
const (
	sourceKindChatDone    = "chat_done"
	sourceKindTaskFailed  = "task_failed"
	sourceKindInboxNotify = "inbox_notify"
)

// payloadVersionV1 is the only payload document this build renders. A row
// stamped with anything higher was written by a newer replica during a rolling
// deploy: it is dead-lettered rather than guessed at, because rendering a
// document whose schema we do not know risks delivering a mangled or
// mis-addressed message to a user.
const payloadVersionV1 = 1

// WeCom errcodes an aibot_send_msg can answer with. Only the ones we act on
// differently are named; the rest fall to the default in retryableSendError.
const (
	// errCodeSystemBusy is WeCom's generic "try again" — a platform-side
	// hiccup, and not a statement about this frame.
	errCodeSystemBusy = -1

	// errCodeMsgTooLong: the frame was over the per-message cap, and NOTHING
	// was written. Re-sending the identical bytes earns the identical answer.
	errCodeMsgTooLong = 45002

	// errCodeAPIFreqLimit and errCodeAPIConcurrencyLimit are quota, not
	// content: the same frame succeeds once the window moves. The rate gate
	// exists to keep us under these, so reaching one means our accounting and
	// WeCom's have drifted — which a later attempt resolves by itself.
	errCodeAPIFreqLimit        = 45009
	errCodeAPIConcurrencyLimit = 45033
)

// maxSendContentBytes is WeCom's ceiling for one aibot_send_msg. Past it the
// server refuses the whole frame with errCodeMsgTooLong and delivers nothing.
//
// Checked before the write so an over-cap row dead-letters on its first attempt
// instead of being refused identically eight times by a server that has already
// made up its mind.
//
// This is a floor, not a splitter, and deliberately so: a row settles as ONE
// Disposition, so splitting inside Send would make a failure on the third frame
// retry the whole row and re-deliver the first two. There is no way to express
// "two of three landed". A long answer therefore has to be split at enqueue
// time — one row per piece, each with its own business key in SourceID — which
// keeps every piece independently retryable.
const maxSendContentBytes = 20480

// outboundPayload is the v1 queue payload. It is deliberately pre-rendered
// data rather than a rendered string: the row may sit through a deploy, and
// re-rendering at send time means a copy fix applies to queued messages too.
type outboundPayload struct {
	// Template selects the rendering. Empty means "deliver Content verbatim",
	// which is the agent-reply case.
	Template string `json:"template,omitempty"`
	Content  string `json:"content,omitempty"`

	// FailureReason and AgentName back the task_failed template.
	FailureReason string `json:"failure_reason,omitempty"`
	AgentName     string `json:"agent_name,omitempty"`
}

const templateTaskFailed = "task_failed"

// renderOutbound turns a payload document into the text to push. A non-nil
// error means the document is unrenderable and the row must be dead-lettered.
func renderOutbound(raw []byte) (string, error) {
	var payload outboundPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", fmt.Errorf("wecom: invalid outbound payload: %w", err)
	}
	switch payload.Template {
	case "":
		content := strings.TrimSpace(payload.Content)
		if content == "" {
			return "", errors.New("wecom: outbound payload has no content")
		}
		return content, nil
	case templateTaskFailed:
		return renderTaskFailed(payload), nil
	default:
		return "", fmt.Errorf("wecom: unknown outbound template %q", payload.Template)
	}
}

func renderTaskFailed(payload outboundPayload) string {
	name := strings.TrimSpace(payload.AgentName)
	if name == "" {
		name = "智能体"
	}
	var b strings.Builder
	b.WriteString("⚠️ ")
	b.WriteString(name)
	b.WriteString("处理这条消息时失败了。")
	if reason := strings.TrimSpace(payload.FailureReason); reason != "" {
		b.WriteString("\n原因：")
		b.WriteString(reason)
	}
	return b.String()
}

// queueSender implements outbox.Sender for WeCom. One instance is shared by
// every installation's consumer: the live connection is resolved per row
// through the process-wide senders registry, so a lease that moves mid-drain
// simply stops resolving here.
type queueSender struct {
	senders *sendersRegistry
}

var _ outbox.Sender = (*queueSender)(nil)

// newQueueSender builds the sender over the same registry wecomChannel.Connect
// registers its wsSender on.
func newQueueSender(senders *sendersRegistry) *queueSender {
	return &queueSender{senders: senders}
}

// Send renders one row and pushes it over the installation's live socket.
func (s *queueSender) Send(ctx context.Context, row db.ChannelOutboundQueue) (outbox.Disposition, error) {
	// payload_version 0 predates the column default and is read as v1; anything
	// above v1 is a document this build does not know.
	if row.PayloadVersion > payloadVersionV1 {
		return outbox.DispositionFailed, fmt.Errorf("wecom: unsupported payload version %d", row.PayloadVersion)
	}
	body, err := renderOutbound(row.Payload)
	if err != nil {
		return outbox.DispositionFailed, err
	}
	// Over the cap the server refuses the frame and delivers nothing, the same
	// way every time, so this is terminal on the first attempt rather than after
	// the eighth.
	if n := len(body); n > maxSendContentBytes {
		return outbox.DispositionFailed, fmt.Errorf(
			"wecom: rendered message is %d bytes, over WeCom's %d-byte per-message cap", n, maxSendContentBytes)
	}

	if s.senders == nil {
		return outbox.DispositionRetry, errors.New("wecom: sender registry not configured")
	}
	sender := s.senders.get(row.InstallationID)
	if sender == nil {
		// No live socket on this replica: the lease moved, or the Supervisor is
		// mid-reconnect. Both are transient from the row's point of view — it
		// stays queued and the holder drains it — so retry rather than fail.
		return outbox.DispositionRetry, errors.New("wecom: connection not ready")
	}

	// ctx, not context.Background: the send now waits for WeCom's verdict, and
	// that wait belongs to the consumer. A lease loss or a shutdown has to end
	// it immediately instead of holding a worker for the full ack timeout.
	if err := sender.sendTextCtx(ctx, row.TargetChatID, int(row.TargetChatType), body); err != nil {
		if retryableSendError(err) {
			return outbox.DispositionRetry, err
		}
		return outbox.DispositionFailed, err
	}
	return outbox.DispositionSent, nil
}

// retryableSendError classifies a failed send.
//
// A refusal WeCom stated is decided on its errcode, never on its prose: the
// codes mean different things and the strings are not ours. The default for a
// stated refusal is terminal — the server looked at this exact frame and
// declined it, so eight identical frames only delay the dead letter. (The
// credential probe fails the other way on an unknown code, because there the
// cost of guessing is telling an admin to rotate a secret that was fine.)
//
// Everything that is not a verdict is treated as the socket failing rather than
// the message, which is the condition this queue exists to survive.
func retryableSendError(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *wecomAPIError
	if errors.As(err, &apiErr) {
		switch apiErr.Code {
		case errCodeSystemBusy, errCodeAPIFreqLimit, errCodeAPIConcurrencyLimit:
			return true
		default:
			return false
		}
	}
	// A lost ack is genuinely ambiguous — the frame went out, so the message may
	// well have landed, and a retry can therefore duplicate it. Retrying anyway
	// is the lesser harm: bounded attempts cap the duplication, and a reader can
	// make sense of a repeated reply, where a silently dropped one leaves them
	// waiting on an answer that is never coming. A cancelled context is the
	// consumer stopping, which is not the row's fault at all.
	if errors.Is(err, errAckTimeout) ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	// Transport failures still arrive as prose from net and gorilla.
	msg := strings.ToLower(err.Error())
	for _, transient := range []string{
		"timeout",
		"temporary",
		"broken pipe",
		"connection reset",
		"use of closed network connection",
		"websocket: close",
	} {
		if strings.Contains(msg, transient) {
			return true
		}
	}
	return false
}
