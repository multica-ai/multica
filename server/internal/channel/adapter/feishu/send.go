package feishu

// This file owns the outbound text-reply path and the Retryable
// classification for Send results.
//
// Concrete Client implementations communicate "this error is worth retrying"
// by wrapping the underlying error with RetryableError. The adapter probes
// that wrapper via errors.As (not errors.Is — the wrapper carries an
// underlying error, not a sentinel) and surfaces the verdict on
// SendResult.Retryable. The default for any error not wrapped is "do not
// retry", which keeps a buggy Client from triggering an infinite send loop
// in the outbound queue (T15).
//
// Usage example for Client implementations:
//
//	resp, err := lark.SendMessage(ctx, lark.NewMessageReq().Build())
//	switch {
//	case err != nil:
//	    // Local transport failure (timeout, DNS, TLS reset). Retry.
//	    return feishu.SendResponse{}, feishu.RetryableError(err)
//	case resp.Code/100 == 5:
//	    // Server-side 5xx. Retry.
//	    return feishu.SendResponse{}, feishu.RetryableError(
//	        fmt.Errorf("feishu 5xx: code=%d msg=%s", resp.Code, resp.Msg))
//	case resp.Code != 0:
//	    // Client-side 4xx (bad request, permission denied, chat not
//	    // found). Permanent — do NOT wrap.
//	    return feishu.SendResponse{}, fmt.Errorf("feishu 4xx: code=%d msg=%s", resp.Code, resp.Msg)
//	}
//	return feishu.SendResponse{MessageID: resp.Data.MessageID}, nil

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	feishucard "github.com/multica-ai/multica/server/internal/channel/adapter/feishu/card"
	"github.com/multica-ai/multica/server/internal/channel/port"
)

// sendText handles the outbound text-reply path used by Adapter.Send. It is
// extracted from adapter.go to keep that file focused on lifecycle / the
// Channel interface plumbing — and so the Retryable judgement (which is
// non-trivial and likely to grow as we discover more Feishu error codes)
// has a single home.
func (a *Adapter) sendText(ctx context.Context, msg port.OutboundMessage) (port.SendResult, error) {
	receiveIDType, receiveID := resolveReceiveID(msg.Target)
	if receiveID == "" {
		// 4xx-class: no point retrying, the caller built a malformed
		// outbound message. Surface as Retryable=false so the outbound
		// queue (T8/T15) drops it instead of looping.
		return port.SendResult{Retryable: false}, errors.New("feishu: OutboundMessage target is empty")
	}

	// Feishu wraps even plain text in a JSON envelope. We marshal here
	// (rather than on the SDK side) so the seam Client only deals with a
	// pre-baked content string — that is exactly the shape the OpenAPI
	// expects, so concrete clients become a thin transport layer.
	contentJSON, err := json.Marshal(feishuTextContent{Text: msg.Text})
	if err != nil {
		// Practically unreachable (json.Marshal of a single string field
		// cannot fail), but treating it as a non-retryable client-side
		// programming bug is the right classification.
		return port.SendResult{Retryable: false}, fmt.Errorf("feishu: marshal text content: %w", err)
	}

	resp, err := a.client.SendMessage(ctx, SendRequest{
		ReceiveIDType: receiveIDType,
		ReceiveID:     receiveID,
		MsgType:       "text",
		Content:       string(contentJSON),
	})
	if err != nil {
		return port.SendResult{Retryable: isRetryable(err)}, fmt.Errorf("feishu: send message: %w", err)
	}
	return port.SendResult{
		PlatformMessageID: resp.MessageID,
		Retryable:         false,
	}, nil
}

// sendCard is the SendCard entry point for the adapter. It renders the
// platform-neutral OutboundCardMessage into Feishu's interactive-card JSON and
// sends it with msg_type "interactive". The rest of the channel runtime should
// never construct Feishu card JSON directly.
func (a *Adapter) sendCard(ctx context.Context, msg port.OutboundCardMessage) (port.SendResult, error) {
	receiveIDType, receiveID := resolveReceiveID(msg.Target)
	if receiveID == "" {
		return port.SendResult{Retryable: false}, errors.New("feishu: OutboundCardMessage target is empty")
	}
	content, err := renderCard(msg.Title, msg.Body, msg.Actions, msg.Mentions)
	if err != nil {
		return port.SendResult{Retryable: false}, fmt.Errorf("feishu: render card: %w", err)
	}

	resp, err := a.client.SendMessage(ctx, SendRequest{
		ReceiveIDType: receiveIDType,
		ReceiveID:     receiveID,
		MsgType:       "interactive",
		Content:       content,
	})
	if err != nil {
		return port.SendResult{Retryable: isRetryable(err)}, fmt.Errorf("feishu: send card: %w", err)
	}
	return port.SendResult{
		PlatformMessageID: resp.MessageID,
		Retryable:         false,
	}, nil
}

func renderCard(title, body string, actions []port.OutboundAction, mentions []port.OutboundMention) (string, error) {
	card := feishucard.NewCard(title, "blue")
	if mentionLine := renderMentionLine(mentions); mentionLine != "" {
		card.AddMarkdown(mentionLine)
	}
	if body != "" {
		card.AddMarkdown(body)
	}
	for _, action := range actions {
		if action.Label == "" || action.URL == "" {
			continue
		}
		card.AddButton(action.Label, action.URL)
	}
	return card.Render()
}

func renderMentionLine(mentions []port.OutboundMention) string {
	var b strings.Builder
	for _, mention := range mentions {
		if mention.Type != port.OutboundTargetUser || strings.TrimSpace(mention.ID) == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString("<at id=")
		b.WriteString(sanitizeMentionID(mention.ID))
		b.WriteString("></at>")
	}
	return b.String()
}

func sanitizeMentionID(id string) string {
	id = strings.TrimSpace(id)
	id = strings.ReplaceAll(id, ">", "")
	id = strings.ReplaceAll(id, "<", "")
	id = strings.ReplaceAll(id, " ", "")
	return id
}

func resolveReceiveID(target port.OutboundTarget) (string, string) {
	if target.ID == "" {
		return "", ""
	}
	switch target.Type {
	case port.OutboundTargetUser:
		return "open_id", target.ID
	case port.OutboundTargetChat:
		return "chat_id", target.ID
	default:
		return "", ""
	}
}

// retryableError is the error type concrete Client implementations should
// wrap transient failures in (or use the shorthand RetryableError helper).
// Keeping the type internal to the feishu package — rather than in port —
// reflects DESIGN §3.1: the Retryable judgement is platform-specific (Feishu
// 5xx vs 4xx vs token-rotation errors), and pushing the type up would force
// every other adapter to share Feishu's error vocabulary.
//
// We intentionally wrap (not embed a sentinel) so future fields like
// RetryAfter or PlatformErrorCode can attach without churning callers — they
// already use errors.As, which sees the typed wrapper regardless of payload.
type retryableError struct{ inner error }

func (e *retryableError) Error() string { return e.inner.Error() }
func (e *retryableError) Unwrap() error { return e.inner }

// RetryableError marks an error as worth retrying by the outbound queue.
// Concrete Client implementations should call this for network errors and
// 5xx responses; 4xx-class platform errors stay un-marked. See the package
// doc comment at the top of send.go for a worked usage example.
func RetryableError(err error) error {
	if err == nil {
		return nil
	}
	return &retryableError{inner: err}
}

// isRetryable reports whether the outbound queue should re-enqueue an error.
// It only returns true for explicitly-marked retryable errors — the safe
// default is "do not retry" so a buggy concrete Client cannot cause an
// infinite send loop.
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	var re *retryableError
	return errors.As(err, &re)
}
