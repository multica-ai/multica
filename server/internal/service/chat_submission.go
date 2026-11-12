package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var ErrChatSubmissionConflict = errors.New("submission_id was already accepted with a different payload")

var ErrChatSessionNotFound = errors.New("chat session not found")

// ChatSubmissionResponse is the original acceptance snapshot. Cancellation,
// queue edits and attachment deletion do not change what was accepted.
type ChatSubmissionResponse struct {
	MessageID     string `json:"message_id"`
	TaskID        string `json:"task_id"`
	SupportsQueue bool   `json:"supports_queue"`
	Queued        bool   `json:"queued"`
	// AttachmentIDs are the attachment rows actually bound to this message by
	// the server. The client diffs these against the ids it requested so it
	// can warn the user when an attachment silently failed to bind — no extra
	// round-trip needed. No `omitempty`: a send that requested attachments but
	// bound none must serialize `[]` (not be omitted), otherwise the client
	// can't tell "all binds failed" from "older server without this field" and
	// would silently skip the very warning this exists for. When no
	// attachments were requested the value is nil → `null`, which the client's
	// guard short-circuits on the requested-ids check.
	AttachmentIDs []string `json:"attachment_ids"`
	// CreatedAt anchors the chat StatusPill timer the instant the user
	// hits send. Without it the front-end falls back to its local clock
	// and the timer "snaps backwards" later when WS events deliver the
	// real created_at. Returning it here means the pill renders 0s from
	// the start with a stable anchor.
	CreatedAt string `json:"created_at"`
}

type chatSubmission struct {
	key         db.GetChatSubmissionReceiptParams
	fingerprint []byte
}

func newChatSubmission(session db.ChatSession, userID pgtype.UUID, content string, attachmentIDs []pgtype.UUID, actorType string, actorID, submissionID pgtype.UUID) chatSubmission {
	ids := make([]string, 0, len(attachmentIDs))
	for _, id := range attachmentIDs {
		ids = append(ids, util.UUIDToString(id))
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	// A structured encoding preserves content bytes and unambiguous boundaries.
	payload, _ := json.Marshal(struct {
		Content       string   `json:"content"`
		AttachmentIDs []string `json:"attachment_ids"`
	}{content, ids})
	hash := sha256.Sum256(payload)
	return chatSubmission{
		key: db.GetChatSubmissionReceiptParams{
			ChatSessionID: session.ID, WorkspaceID: session.WorkspaceID,
			InitiatorUserID: userID, ActorType: actorType, ActorID: actorID, SubmissionID: submissionID,
		},
		fingerprint: hash[:],
	}
}

func (submission chatSubmission) lookup(ctx context.Context, q *db.Queries) (*ChatSubmissionResponse, error) {
	receipt, err := q.GetChatSubmissionReceipt(ctx, submission.key)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read chat submission receipt: %w", err)
	}
	if !bytes.Equal(receipt.RequestFingerprint, submission.fingerprint) {
		return nil, ErrChatSubmissionConflict
	}
	var response ChatSubmissionResponse
	if err := json.Unmarshal(receipt.Response, &response); err != nil {
		return nil, fmt.Errorf("decode chat submission receipt: %w", err)
	}
	return &response, nil
}

// LookupDirectChatSubmission must be called only after current authorization.
// It intentionally avoids runtime readiness, overlays and provider lookups.
func (s *TaskService) LookupDirectChatSubmission(ctx context.Context, session db.ChatSession, userID pgtype.UUID, content string, attachmentIDs []pgtype.UUID, actorType string, actorID, submissionID pgtype.UUID) (*ChatSubmissionResponse, error) {
	return newChatSubmission(session, userID, content, attachmentIDs, actorType, actorID, submissionID).lookup(ctx, s.Queries)
}

// SendDirectChatMessageWithSubmission requires a real transaction even for
// callers which construct TaskService without its usual database wiring.
func (s *TaskService) SendDirectChatMessageWithSubmission(ctx context.Context, session db.ChatSession, agent db.Agent, userID pgtype.UUID, content string, attachmentIDs []pgtype.UUID, actorType string, actorID, submissionID pgtype.UUID) (*DirectChatSendResult, error) {
	if !submissionID.Valid {
		return nil, errors.New("submission_id is required for keyed chat send")
	}
	if s.TxStarter == nil {
		return nil, errors.New("chat submission requires a transaction starter")
	}
	submission := newChatSubmission(session, userID, content, attachmentIDs, actorType, actorID, submissionID)
	if response, err := submission.lookup(ctx, s.Queries); err != nil {
		return nil, err
	} else if response != nil {
		return &DirectChatSendResult{Replayed: true, Acceptance: response}, nil
	}
	return s.sendDirectChatMessage(ctx, session, agent, userID, content, attachmentIDs, actorType, actorID, &submission)
}

func (submission chatSubmission) store(ctx context.Context, q *db.Queries, sent *DirectChatSendResult, attachmentsRequested bool) error {
	response := ChatSubmissionResponse{
		MessageID: util.UUIDToString(sent.Message.ID), TaskID: util.UUIDToString(sent.Task.ID),
		SupportsQueue: true, Queued: sent.Queued, CreatedAt: util.TimestampToString(sent.Task.CreatedAt),
	}
	if attachmentsRequested {
		response.AttachmentIDs = make([]string, 0, len(sent.BoundAttachmentIDs))
		for _, id := range sent.BoundAttachmentIDs {
			response.AttachmentIDs = append(response.AttachmentIDs, util.UUIDToString(id))
		}
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		return err
	}
	key := submission.key
	if err := q.CreateChatSubmissionReceipt(ctx, db.CreateChatSubmissionReceiptParams{
		ChatSessionID: key.ChatSessionID, WorkspaceID: key.WorkspaceID, InitiatorUserID: key.InitiatorUserID,
		ActorType: key.ActorType, ActorID: key.ActorID, SubmissionID: key.SubmissionID,
		RequestFingerprint: submission.fingerprint, Response: encoded,
	}); err != nil {
		return fmt.Errorf("store chat submission receipt: %w", err)
	}
	sent.Acceptance = &response
	return nil
}
