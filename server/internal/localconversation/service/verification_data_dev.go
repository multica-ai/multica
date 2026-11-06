//go:build verificationdev

package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/multica-ai/multica/server/internal/localconversation/store"
)

// VerificationData is a content protocol, independent of the SQLite file schema.
type VerificationData struct {
	SchemaVersion int                        `json:"schemaVersion"`
	RestoreID     string                     `json:"restoreId"`
	Conversations []VerificationConversation `json:"conversations"`
}
type VerificationConversation struct {
	OriginalID string `json:"originalId"`
	Snapshot   struct {
		Conversation struct {
			ID           string `json:"conversation_id"`
			Title        string `json:"title"`
			Provider     string `json:"provider"`
			SideSourceID string `json:"side_source_id"`
		} `json:"conversation"`
		Turns []struct {
			ID          string `json:"id"`
			RequestID   string `json:"request_id"`
			Status      string `json:"status"`
			StartedAt   string `json:"started_at"`
			CompletedAt string `json:"completed_at"`
			Messages    []struct {
				ID            string   `json:"id"`
				Role          string   `json:"role"`
				Content       string   `json:"content"`
				AttachmentIDs []string `json:"attachment_ids"`
				Steered       bool     `json:"steered"`
				Failed        bool     `json:"failed"`
				CreatedAt     string   `json:"created_at"`
			} `json:"messages"`
			Events []struct {
				Type      string          `json:"type"`
				ItemID    string          `json:"item_id"`
				Final     bool            `json:"final"`
				Payload   json.RawMessage `json:"payload"`
				CreatedAt string          `json:"created_at"`
			} `json:"events"`
		} `json:"turns"`
		Queue struct {
			Items []json.RawMessage `json:"items"`
		} `json:"queue"`
		// Open submissions as the snapshot reports them (schema 6 and later).
		Submissions []struct {
			RequestID     string          `json:"request_id"`
			State         string          `json:"state"`
			Item          json.RawMessage `json:"item"`
			BlockedReason string          `json:"blocked_reason"`
		} `json:"submissions"`
		ForkedFromID     string          `json:"forked_from_id"`
		ForkedFromTurnID string          `json:"forked_from_turn_id"`
		SideContext      json.RawMessage `json:"side_context"`
	} `json:"snapshot"`
	Attachments []struct {
		OriginalID string `json:"originalId"`
		Name       string `json:"name"`
		MediaType  string `json:"mediaType"`
		Data       string `json:"data"`
		SHA256     string `json:"sha256"`
	} `json:"attachments"`
}

func verificationID(namespace, id string) string {
	sum := sha256.Sum256([]byte(namespace + "\x00" + id))
	return hex.EncodeToString(sum[:16])
}
func verificationTime(text string) time.Time {
	value, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return time.Now().UTC()
	}
	return value
}

// VerificationImport creates frozen copies through the store's normal atomic import path.
func (s *Service) VerificationImport(ctx context.Context, scopeID int64, input VerificationData) (map[string]string, error) {
	if !VerificationEnabled() || input.SchemaVersion != 1 || len(input.RestoreID) < 8 || len(input.RestoreID) > 160 || len(input.Conversations) == 0 || len(input.Conversations) > 100 {
		return nil, ErrInvalidRequest
	}
	mapping := map[string]string{}
	for _, source := range input.Conversations {
		if source.OriginalID == "" || source.Snapshot.Conversation.ID != source.OriginalID || (source.Snapshot.Conversation.Provider != "claude" && source.Snapshot.Conversation.Provider != "codex") || len(source.Snapshot.Turns) > 20000 {
			return nil, ErrInvalidRequest
		}
		if _, exists := mapping[source.OriginalID]; exists {
			return nil, ErrInvalidRequest
		}
		mapping[source.OriginalID] = verificationID(input.RestoreID, source.OriginalID)
		for _, attachment := range source.Attachments {
			data, err := base64.StdEncoding.DecodeString(attachment.Data)
			sum := sha256.Sum256(data)
			if err != nil || len(data) > store.MaxUploadBytes || hex.EncodeToString(sum[:]) != attachment.SHA256 {
				return nil, ErrInvalidRequest
			}
			mapping[attachment.OriginalID] = verificationID(input.RestoreID, attachment.OriginalID)
		}
		for _, turn := range source.Snapshot.Turns {
			if turn.ID == "" || (turn.Status != "completed" && turn.Status != "failed" && turn.Status != "cancelled" && turn.Status != "running") {
				return nil, ErrInvalidRequest
			}
			mapping[turn.ID] = verificationID(input.RestoreID, turn.ID)
			if turn.RequestID != "" {
				mapping[turn.RequestID] = verificationID(input.RestoreID, turn.RequestID)
			}
			for _, message := range turn.Messages {
				mapping[message.ID] = verificationID(input.RestoreID, message.ID)
			}
			for _, event := range turn.Events {
				if event.ItemID != "" {
					mapping[event.ItemID] = verificationID(input.RestoreID, event.ItemID)
				}
			}
		}
		for _, raw := range source.Snapshot.Queue.Items {
			var item struct {
				RequestID string `json:"requestId"`
			}
			if err := json.Unmarshal(raw, &item); err != nil || item.RequestID == "" {
				return nil, ErrInvalidRequest
			}
			mapping[item.RequestID] = verificationID(input.RestoreID, item.RequestID)
		}
		for _, submission := range source.Snapshot.Submissions {
			if submission.RequestID == "" || (submission.State != "queued" && submission.State != "starting" && submission.State != "recovery_required") {
				return nil, ErrInvalidRequest
			}
			mapping[submission.RequestID] = verificationID(input.RestoreID, submission.RequestID)
		}
	}
	for _, source := range input.Conversations {
		id := mapping[source.OriginalID]
		encoded, _ := json.Marshal(source)
		sum := sha256.Sum256(encoded)
		fingerprint := hex.EncodeToString(sum[:])
		requestID := "verification:" + verificationID(input.RestoreID, source.OriginalID)
		if receipt, err := s.store.Receipt(ctx, scopeID, requestID); err == nil {
			if receipt.Fingerprint != fingerprint || receipt.Result != id {
				return nil, ErrRequestConflict
			}
			continue
		} else if !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		for _, attachment := range source.Attachments {
			data, _ := base64.StdEncoding.DecodeString(attachment.Data)
			if _, err := s.Upload(ctx, scopeID, mapping[attachment.OriginalID], attachment.Name, attachment.MediaType, bytes.NewReader(data)); err != nil {
				return nil, err
			}
		}
		cwd := filepath.Join(filepath.Dir(s.opts.Path), "workdirs", fmt.Sprint(scopeID), id)
		if err := os.MkdirAll(cwd, 0o700); err != nil {
			return nil, err
		}
		history := []store.Batch{}
		for _, turn := range source.Snapshot.Turns {
			status := turn.Status
			if status != "completed" && status != "failed" && status != "cancelled" {
				status = "cancelled"
			}
			batch := store.Batch{StartTurn: &store.NewTurn{ID: mapping[turn.ID], RequestID: mapping[turn.RequestID], StartedAt: verificationTime(turn.StartedAt)}, CompleteTurn: &store.TurnCompletion{TurnID: mapping[turn.ID], Status: status, CompletedAt: verificationTime(turn.CompletedAt)}}
			for _, message := range turn.Messages {
				attachments := []string{}
				for _, old := range message.AttachmentIDs {
					target, ok := mapping[old]
					if !ok {
						return nil, ErrInvalidRequest
					}
					attachments = append(attachments, target)
				}
				batch.Messages = append(batch.Messages, store.NewMessage{ID: mapping[message.ID], TurnID: mapping[turn.ID], Role: message.Role, Content: message.Content, AttachmentIDs: attachments, Steered: message.Steered, Failed: message.Failed, CreatedAt: verificationTime(message.CreatedAt)})
			}
			for _, event := range turn.Events {
				payload, err := verificationRewrite(event.Payload, mapping)
				if err != nil {
					return nil, err
				}
				batch.Events = append(batch.Events, store.NewEvent{TurnID: mapping[turn.ID], ItemID: mapping[event.ItemID], Type: event.Type, Final: event.Final, Payload: payload, CreatedAt: verificationTime(event.CreatedAt)})
			}
			idle := "idle"
			batch.Summary = &store.SummaryPatch{Status: &idle, UpdatedAt: time.Now().UTC()}
			history = append(history, batch)
		}
		// Submissions that were being sent or need review are not queue entries;
		// the snapshot's queue view lists the latter, so keep them out of the queue.
		review := map[string]string{}
		reasons := map[string]string{}
		for _, submission := range source.Snapshot.Submissions {
			switch submission.State {
			case "recovery_required":
				review[submission.RequestID] = submission.BlockedReason
			case "starting":
				review[submission.RequestID] = verificationReviewReason
			case "queued":
				reasons[submission.RequestID] = submission.BlockedReason
			}
		}
		queued := 0
		for _, raw := range source.Snapshot.Queue.Items {
			oldID, rewritten, err := verificationQueueItem(raw, mapping)
			if err != nil {
				return nil, err
			}
			if _, skip := review[oldID]; skip {
				continue
			}
			queued++
			count := int64(queued)
			batch := store.Batch{Queue: &store.QueueChange{Revision: count, Paused: true, Append: &store.QueueItem{RequestID: mapping[oldID], Item: rewritten}}, Summary: &store.SummaryPatch{QueueCount: &count, UpdatedAt: time.Now().UTC()}}
			if reason := reasons[oldID]; reason != "" {
				batch.Submissions = []store.SubmissionChange{{RequestID: mapping[oldID], State: "queued", Reason: reason}}
			}
			history = append(history, batch)
		}
		history = append(history, store.Batch{Queue: &store.QueueChange{Revision: int64(queued + 1), Paused: true}})
		for _, submission := range source.Snapshot.Submissions {
			reason, ok := review[submission.RequestID]
			if !ok || len(submission.Item) == 0 || string(submission.Item) == "{}" {
				continue
			}
			_, rewritten, err := verificationQueueItem(submission.Item, mapping)
			if err != nil {
				return nil, err
			}
			if reason == "" {
				reason = verificationReviewReason
			}
			history = append(history, store.Batch{Submissions: []store.SubmissionChange{{RequestID: mapping[submission.RequestID], Item: rewritten, State: "recovery_required", Reason: reason}}})
		}
		conversation := store.NewConversation{ID: id, ScopeID: scopeID, Provider: source.Snapshot.Conversation.Provider, Title: source.Snapshot.Conversation.Title, TitleMode: "manual", Cwd: cwd, PermissionMode: "default", ForkedFromID: mapping[source.Snapshot.ForkedFromID], ForkedFromTurnID: mapping[source.Snapshot.ForkedFromTurnID], CreatedAt: time.Now().UTC(), Receipt: &store.CommandReceipt{ScopeID: scopeID, RequestID: requestID, Fingerprint: fingerprint, Result: id, CreatedAt: time.Now().UTC()}}
		if source.Snapshot.Conversation.SideSourceID != "" {
			if mapping[source.Snapshot.Conversation.SideSourceID] == "" || len(source.Snapshot.SideContext) == 0 {
				return nil, ErrInvalidRequest
			}
			context, err := verificationRewrite(source.Snapshot.SideContext, mapping)
			if err != nil {
				return nil, err
			}
			conversation.SideSourceID = mapping[source.Snapshot.Conversation.SideSourceID]
			conversation.Side = string(context)
		}
		if _, err := s.store.ImportConversation(ctx, conversation, history); err != nil {
			return nil, err
		}
		if _, err := s.publishCreated(ctx, id); err != nil {
			return nil, err
		}
	}
	return mapping, nil
}

func verificationRewrite(raw json.RawMessage, mapping map[string]string) (json.RawMessage, error) {
	var value any
	if len(raw) == 0 {
		return json.RawMessage(`{}`), nil
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	var rewrite func(any, string) any
	rewrite = func(value any, key string) any {
		switch typed := value.(type) {
		case map[string]any:
			for name, child := range typed {
				typed[name] = rewrite(child, name)
			}
			return typed
		case []any:
			for index, child := range typed {
				typed[index] = rewrite(child, key)
			}
			return typed
		case string:
			switch key {
			case "id", "source_id", "message_id", "turn_id", "conversation_id", "request_id", "item_id", "attachment_ids", "attachmentIds", "requestId", "clientSubmitId", "interaction_id":
				if mapped, ok := mapping[typed]; ok {
					return mapped
				}
			}
		}
		return value
	}
	return json.Marshal(rewrite(value, ""))
}

// A copy cannot resume a send that was in flight, so it asks for review like
// any send whose delivery is unknown.
const verificationReviewReason = "This message was being sent when the conversation was copied. Review it before retrying."

// verificationQueueItem remaps a queued prompt's identifiers for the copy and
// returns its original request ID.
func verificationQueueItem(raw json.RawMessage, mapping map[string]string) (string, json.RawMessage, error) {
	var item map[string]json.RawMessage
	if err := json.Unmarshal(raw, &item); err != nil {
		return "", nil, err
	}
	var oldID string
	if err := json.Unmarshal(item["requestId"], &oldID); err != nil || oldID == "" {
		return "", nil, ErrInvalidRequest
	}
	rewritten, err := verificationRewrite(raw, mapping)
	if err != nil {
		return "", nil, err
	}
	var queued map[string]any
	if err := json.Unmarshal(rewritten, &queued); err != nil {
		return "", nil, err
	}
	delete(queued, "agents")
	delete(queued, "references")
	queued["requestId"] = mapping[oldID]
	queued["display"] = map[string]any{"id": mapping[oldID], "clientSubmitId": mapping[oldID]}
	rewritten, err = json.Marshal(queued)
	return oldID, rewritten, err
}
