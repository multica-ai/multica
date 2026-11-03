package service

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/multica-ai/multica/server/internal/localconversation/store"
)

// outputEvent is a provider-neutral normalized event, as produced by the
// Claude and Codex normalizers.
type outputEvent struct {
	ItemID  string
	Type    string
	Final   bool
	Payload map[string]any
	// ImageMime and ImageData carry an inline image the recorder stores as
	// an attachment before the event is committed.
	ImageMime string
	ImageData string
}

type turnError struct {
	kind    string
	message string
}

// turnRecorder builds the batches a runner commits for one conversation:
// turn start, output, completion and native session changes. It holds no
// turn state; runners own the lifecycle.
type turnRecorder struct {
	conversationID string
	scopeID        int64
	files          AttachmentFiles
	now            func() time.Time
}

// startBatch records the user input, turn start and command receipt.
func (t *turnRecorder) startBatch(req SendRequest) store.Batch {
	now := t.now()
	running := "running"
	if req.Hidden {
		batch := store.Batch{
			ConversationID: t.conversationID,
			StartTurn:      &store.NewTurn{ID: req.TurnID, RequestID: req.RequestID, StartedAt: now},
			Events:         []store.NewEvent{t.event(req.TurnID, "", "turn.started", false, map[string]any{"request_id": req.RequestID}, now)},
			Summary:        &store.SummaryPatch{Status: &running, UpdatedAt: now},
		}
		if req.Fingerprint != "" {
			batch.Receipt = &store.CommandReceipt{ScopeID: t.scopeID, RequestID: req.RequestID, Fingerprint: req.Fingerprint, Result: req.TurnID, CreatedAt: now}
		}
		return batch
	}
	preview := truncateRunes(req.Text, previewRunes)
	return store.Batch{
		ConversationID: t.conversationID,
		Receipt:        &store.CommandReceipt{ScopeID: t.scopeID, RequestID: req.RequestID, Fingerprint: req.Fingerprint, Result: req.TurnID, CreatedAt: now},
		StartTurn:      &store.NewTurn{ID: req.TurnID, RequestID: req.RequestID, StartedAt: now},
		Events: []store.NewEvent{
			t.event(req.TurnID, "", "user.message", true, userMessagePayload(req), now),
			t.event(req.TurnID, "", "turn.started", false, map[string]any{"request_id": req.RequestID, "input_message_id": req.MessageID}, now),
		},
		Messages: []store.NewMessage{{ID: req.MessageID, TurnID: req.TurnID, Role: "user", Content: req.Text, AttachmentIDs: req.AttachmentIDs, CreatedAt: now}},
		Summary:  &store.SummaryPatch{Status: &running, LastMessagePreview: &preview, Title: req.Title, UpdatedAt: now},
	}
}

// outputBatch records normalized output: images become attachments, and
// each finished assistant text becomes a message and the list preview.
func (t *turnRecorder) outputBatch(turnID string, events []outputEvent) store.Batch {
	now := t.now()
	batch := store.Batch{ConversationID: t.conversationID}
	var preview *string
	for _, e := range events {
		if e.ImageData != "" {
			e = t.storeImage(&batch, e, now)
		}
		batch.Events = append(batch.Events, t.event(turnID, e.ItemID, e.Type, e.Final, e.Payload, now))
		if e.Final && e.Payload["kind"] == "assistant_text" && e.Payload["parent_tool_use_id"] == nil {
			text, _ := e.Payload["text"].(string)
			batch.Messages = append(batch.Messages, store.NewMessage{ID: e.ItemID, TurnID: turnID, Role: "assistant", Content: text, CreatedAt: now})
			p := truncateRunes(text, previewRunes)
			preview = &p
		}
	}
	if preview != nil {
		batch.Summary = &store.SummaryPatch{LastMessagePreview: preview, UpdatedAt: now}
	}
	return batch
}

// completionBatch records the finals of items the turn left open, then
// turn.completed, the turn row and the conversation status, together.
func (t *turnRecorder) completionBatch(turnID, providerTurnID, status string, failure *turnError, finals []outputEvent) store.Batch {
	now := t.now()
	batch := store.Batch{ConversationID: t.conversationID}
	for _, e := range finals {
		batch.Events = append(batch.Events, t.event(turnID, e.ItemID, e.Type, e.Final, e.Payload, now))
	}
	payload := map[string]any{"status": status}
	completion := &store.TurnCompletion{TurnID: turnID, Status: status, CompletedAt: now, ProviderTurnID: providerTurnID}
	if failure != nil {
		payload["error"] = map[string]any{"kind": failure.kind, "message": failure.message}
		completion.ErrorKind, completion.Error = failure.kind, failure.message
	}
	batch.Events = append(batch.Events, t.event(turnID, "", "turn.completed", true, payload, now))
	batch.CompleteTurn = completion
	conversationStatus := "idle"
	if status == "failed" {
		conversationStatus = "failed"
	}
	batch.Summary = &store.SummaryPatch{Status: &conversationStatus, UpdatedAt: now}
	return batch
}

// sessionBatch records the native session the conversation resumes.
func (t *turnRecorder) sessionBatch(id string) store.Batch {
	return store.Batch{ConversationID: t.conversationID, ProviderSessionID: &id, Events: []store.NewEvent{
		t.event("", "", "conversation.updated", false, map[string]any{"provider_session_id": id}, t.now()),
	}}
}

// storeImage writes an inline image as an attachment committed with the
// batch and points the image event at it. A failed write becomes an error
// item, so the image's absence is visible instead of silent.
func (t *turnRecorder) storeImage(batch *store.Batch, e outputEvent, now time.Time) outputEvent {
	data, err := base64.StdEncoding.DecodeString(e.ImageData)
	var attachment store.NewAttachment
	if err == nil {
		attachment, err = t.files.WriteAttachmentFile(t.scopeID, t.conversationID, e.ImageMime, data, now)
	}
	if err != nil {
		payload := map[string]any{
			"kind": "error", "status": "failed", "error": map[string]any{"kind": "attachment_failed", "message": err.Error()},
		}
		if parent := e.Payload["parent_tool_use_id"]; parent != nil {
			payload["kind"], payload["item_kind"], payload["parent_tool_use_id"] = "subagent", "error", parent
		}
		return outputEvent{ItemID: e.ItemID, Type: "item.completed", Final: true, Payload: payload}
	}
	batch.Attachments = append(batch.Attachments, attachment)
	payload := map[string]any{"attachment_id": attachment.ID}
	for k, v := range e.Payload {
		payload[k] = v
	}
	return outputEvent{ItemID: e.ItemID, Type: e.Type, Final: e.Final, Payload: payload}
}

func (t *turnRecorder) event(turnID, itemID, typ string, final bool, payload map[string]any, at time.Time) store.NewEvent {
	data, err := json.Marshal(payload)
	if err != nil {
		data = []byte(fmt.Sprintf(`{"error":{"kind":"encoding","message":%q}}`, err.Error()))
	}
	return store.NewEvent{TurnID: turnID, ItemID: itemID, Type: typ, Final: final, Payload: data, CreatedAt: at}
}

func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

func userMessagePayload(req SendRequest) map[string]any {
	payload := map[string]any{"message_id": req.MessageID, "request_id": req.RequestID, "content": req.Text}
	if len(req.AttachmentIDs) > 0 {
		payload["attachment_ids"] = req.AttachmentIDs
	}
	return payload
}
