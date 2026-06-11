package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const localCLIMessageOutboxKind = "message_side_effects"

type createLocalCLIMessageResult struct {
	Message protocol.TaskMessagePayload
	Created bool
}

type localCLIMessageOutboxItem struct {
	ID        pgtype.UUID
	MessageID pgtype.UUID
	RunID     pgtype.UUID
	Attempts  int32
	AppOrigin pgtype.Text
}

type localCLIMessageOutboxMessage struct {
	MessageID pgtype.UUID
	CommentID pgtype.UUID
	Payload   protocol.TaskMessagePayload
	Request   createLocalCLIMessageRequest
	Run       localCLIRun
}

func (h *Handler) createLocalCLIMessageAndOutbox(ctx context.Context, run localCLIRun, req createLocalCLIMessageRequest, appOrigin string) (createLocalCLIMessageResult, error) {
	if h.TxStarter == nil {
		return createLocalCLIMessageResult{}, errors.New("transaction starter is not configured")
	}
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return createLocalCLIMessageResult{}, err
	}
	defer tx.Rollback(ctx)

	if req.Source != "" && req.SourceKey != "" {
		msg, found, err := loadLocalCLIMessageBySourceWithQuerier(ctx, tx, run, req.Source, req.SourceKey)
		if err != nil {
			return createLocalCLIMessageResult{}, err
		}
		if found {
			if err := tx.Commit(ctx); err != nil {
				return createLocalCLIMessageResult{}, err
			}
			return createLocalCLIMessageResult{Message: msg, Created: false}, nil
		}
	}

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, uuidToString(run.ID)); err != nil {
		return createLocalCLIMessageResult{}, err
	}

	input, _ := json.Marshal(req.Input)
	if req.Input == nil {
		input = nil
	}

	row := tx.QueryRow(ctx, `
		INSERT INTO local_cli_message (run_id, seq, type, tool, content, input, output, source, source_key)
		VALUES (
			$1,
			COALESCE((SELECT MAX(seq) + 1 FROM local_cli_message WHERE run_id = $1), 1),
			$2, NULLIF($3, ''), NULLIF($4, ''), $5::jsonb, NULLIF($6, ''), NULLIF($7, ''), NULLIF($8, '')
		)
		ON CONFLICT (run_id, source, source_key)
			WHERE source IS NOT NULL AND source_key IS NOT NULL
			DO NOTHING
		RETURNING id, run_id, seq, type, tool, content, input, output, created_at, source, source_key
	`, run.ID, req.Type, req.Tool, req.Content, input, req.Output, req.Source, req.SourceKey)

	messageID, msg, err := scanLocalCLIMessageWithID(row, uuidToString(run.IssueID))
	if err != nil {
		if req.Source != "" && req.SourceKey != "" && err == pgx.ErrNoRows {
			existing, found, loadErr := loadLocalCLIMessageBySourceWithQuerier(ctx, tx, run, req.Source, req.SourceKey)
			if loadErr != nil {
				return createLocalCLIMessageResult{}, loadErr
			}
			if found {
				if err := tx.Commit(ctx); err != nil {
					return createLocalCLIMessageResult{}, err
				}
				return createLocalCLIMessageResult{Message: existing, Created: false}, nil
			}
		}
		return createLocalCLIMessageResult{}, err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO local_cli_message_outbox (message_id, run_id, kind, app_origin)
		VALUES ($1, $2, $3, NULLIF($4, ''))
		ON CONFLICT (message_id, kind) DO NOTHING
	`, messageID, run.ID, localCLIMessageOutboxKind, appOrigin); err != nil {
		return createLocalCLIMessageResult{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return createLocalCLIMessageResult{}, err
	}
	return createLocalCLIMessageResult{Message: msg, Created: true}, nil
}

func loadLocalCLIMessageBySourceWithQuerier(ctx context.Context, q dbExecutor, run localCLIRun, source, sourceKey string) (protocol.TaskMessagePayload, bool, error) {
	row := q.QueryRow(ctx, `
		SELECT id, run_id, seq, type, tool, content, input, output, created_at, source, source_key
		FROM local_cli_message
		WHERE run_id = $1 AND source = $2 AND source_key = $3
	`, run.ID, source, sourceKey)
	msg, err := scanLocalCLIMessage(row, uuidToString(run.IssueID))
	if err == nil {
		return msg, true, nil
	}
	if err == pgx.ErrNoRows {
		return protocol.TaskMessagePayload{}, false, nil
	}
	return protocol.TaskMessagePayload{}, false, err
}

func scanLocalCLIMessageWithID(row localCLIMessageScanner, issueID string) (pgtype.UUID, protocol.TaskMessagePayload, error) {
	var (
		id         pgtype.UUID
		runID      pgtype.UUID
		seq        int32
		msgType    string
		tool       pgtype.Text
		content    pgtype.Text
		inputBytes []byte
		output     pgtype.Text
		createdAt  pgtype.Timestamptz
		source     pgtype.Text
		sourceKey  pgtype.Text
	)
	err := row.Scan(&id, &runID, &seq, &msgType, &tool, &content, &inputBytes, &output, &createdAt, &source, &sourceKey)
	if err != nil {
		return pgtype.UUID{}, protocol.TaskMessagePayload{}, err
	}
	var input map[string]any
	if len(inputBytes) > 0 {
		_ = json.Unmarshal(inputBytes, &input)
	}
	return id, protocol.TaskMessagePayload{
		TaskID:  uuidToString(runID),
		IssueID: issueID,
		Seq:     int(seq),
		Type:    msgType,
		Tool:    tool.String,
		Content: content.String,
		Input:   input,
		Output:  output.String,
	}, nil
}

func (h *Handler) RunLocalCLIMessageOutbox(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := h.processLocalCLIMessageOutboxBatch(ctx, 25); err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("local cli message outbox: batch failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (h *Handler) processLocalCLIMessageOutboxBatch(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		limit = 25
	}
	processed := 0
	for processed < limit {
		item, found, err := h.claimLocalCLIMessageOutbox(ctx)
		if err != nil {
			return processed, err
		}
		if !found {
			return processed, nil
		}
		if err := h.processLocalCLIMessageOutboxItem(ctx, item); err != nil {
			h.failLocalCLIMessageOutbox(context.Background(), item, err)
			processed++
			continue
		}
		if err := h.completeLocalCLIMessageOutbox(ctx, item.ID); err != nil {
			return processed, err
		}
		processed++
	}
	return processed, nil
}

func (h *Handler) claimLocalCLIMessageOutbox(ctx context.Context) (localCLIMessageOutboxItem, bool, error) {
	if h.DB == nil {
		return localCLIMessageOutboxItem{}, false, nil
	}
	row := h.DB.QueryRow(ctx, `
		UPDATE local_cli_message_outbox
		SET status = 'processing',
		    attempts = attempts + 1,
		    locked_until = now() + interval '60 seconds',
		    updated_at = now()
		WHERE id = (
			SELECT id
			FROM local_cli_message_outbox
			WHERE kind = $1
				AND (
					(status = 'pending' AND next_attempt_at <= now())
					OR (status = 'processing' AND locked_until <= now())
				)
			ORDER BY created_at ASC
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		RETURNING id, message_id, run_id, attempts, app_origin
	`, localCLIMessageOutboxKind)
	var item localCLIMessageOutboxItem
	if err := row.Scan(&item.ID, &item.MessageID, &item.RunID, &item.Attempts, &item.AppOrigin); err != nil {
		if err == pgx.ErrNoRows {
			return localCLIMessageOutboxItem{}, false, nil
		}
		return localCLIMessageOutboxItem{}, false, err
	}
	return item, true, nil
}

func (h *Handler) processLocalCLIMessageOutboxItem(ctx context.Context, item localCLIMessageOutboxItem) error {
	msg, err := h.loadLocalCLIMessageOutboxMessage(ctx, item.MessageID)
	if err != nil {
		return err
	}
	var comment *db.Comment
	if localCLIMessageCreatesReply(msg.Run, msg.Request) {
		created, err := h.ensureLocalCLIMessageComment(ctx, msg)
		if err != nil {
			return err
		}
		comment = created
	}
	if comment != nil {
		var displayName *string
		if msg.Request.Type == "final" || localCLIMessageIsCodexProposedPlan(msg.Request) {
			name := h.localCLIDisplayName(ctx, msg.Run)
			displayName = &name
		}
		resp := commentToResponseWithDisplay(*comment, nil, nil, displayName)
		h.publish(protocol.EventCommentCreated, uuidToString(msg.Run.WorkspaceID), "member", uuidToString(msg.Run.OwnerID), map[string]any{
			"comment":    resp,
			"app_origin": item.AppOrigin.String,
		})
	}
	h.publishTask(protocol.EventTaskMessage, uuidToString(msg.Run.WorkspaceID), "member", uuidToString(msg.Run.OwnerID), uuidToString(msg.Run.ID), msg.Payload)
	return nil
}

func (h *Handler) loadLocalCLIMessageOutboxMessage(ctx context.Context, messageID pgtype.UUID) (localCLIMessageOutboxMessage, error) {
	row := h.DB.QueryRow(ctx, `
		SELECT
			lcm.id, lcm.comment_id, lcm.run_id, lcm.seq, lcm.type, lcm.tool, lcm.content, lcm.input, lcm.output, lcm.created_at, lcm.source, lcm.source_key,
			lcr.id, lcr.workspace_id, lcr.issue_id, lcr.owner_id, lcr.cli_name, lcr.status,
			lcr.started_at, lcr.completed_at, lcr.exit_code, lcr.work_dir, lcr.context_dir,
			lcr.comments_mode, lcr.top_comment_id, lcr.error, lcr.source, lcr.source_key, lcr.created_at, lcr.updated_at
		FROM local_cli_message lcm
		JOIN local_cli_run lcr ON lcr.id = lcm.run_id
		WHERE lcm.id = $1
	`, messageID)
	var (
		commentID pgtype.UUID
		run       localCLIRun
	)
	id, payload, err := scanLocalCLIMessageWithID(localCLIMessageAndRunScanner{row: row, commentID: &commentID, run: &run}, "")
	if err != nil {
		return localCLIMessageOutboxMessage{}, err
	}
	payload.IssueID = uuidToString(run.IssueID)
	req := createLocalCLIMessageRequest{
		Type:      payload.Type,
		Tool:      payload.Tool,
		Content:   payload.Content,
		Input:     payload.Input,
		Output:    payload.Output,
		Source:    "",
		SourceKey: "",
	}
	return localCLIMessageOutboxMessage{MessageID: id, CommentID: commentID, Payload: payload, Request: req, Run: run}, nil
}

type localCLIMessageAndRunScanner struct {
	row       localCLIMessageScanner
	commentID *pgtype.UUID
	run       *localCLIRun
}

func (s localCLIMessageAndRunScanner) Scan(dest ...any) error {
	all := make([]any, 0, len(dest)+1+18)
	all = append(all, dest[0])
	all = append(all, s.commentID)
	all = append(all, dest[1:]...)
	all = append(all,
		&s.run.ID, &s.run.WorkspaceID, &s.run.IssueID, &s.run.OwnerID,
		&s.run.CLIName, &s.run.Status, &s.run.StartedAt, &s.run.CompletedAt,
		&s.run.ExitCode, &s.run.WorkDir, &s.run.ContextDir, &s.run.CommentsMode,
		&s.run.TopCommentID, &s.run.Error, &s.run.Source, &s.run.SourceKey, &s.run.CreatedAt, &s.run.UpdatedAt,
	)
	return s.row.Scan(all...)
}

func localCLIMessageCreatesReply(run localCLIRun, req createLocalCLIMessageRequest) bool {
	createsThreadReply := (req.Type == "final" || (req.Type == "user_input" && !localCLIMessageIsNonCommentableCommand(req)) || localCLIMessageIsCodexProposedPlan(req)) &&
		run.CommentsMode == "thread" &&
		run.TopCommentID.Valid
	return createsThreadReply && strings.TrimSpace(req.Content) != ""
}

func (h *Handler) ensureLocalCLIMessageComment(ctx context.Context, msg localCLIMessageOutboxMessage) (*db.Comment, error) {
	if msg.CommentID.Valid {
		comment, err := h.Queries.GetComment(ctx, msg.CommentID)
		if err != nil {
			return nil, err
		}
		return &comment, nil
	}
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var existing pgtype.UUID
	if err := tx.QueryRow(ctx, `
		SELECT comment_id
		FROM local_cli_message
		WHERE id = $1
		FOR UPDATE
	`, msg.MessageID).Scan(&existing); err != nil {
		return nil, err
	}
	if existing.Valid {
		comment, err := db.New(tx).GetComment(ctx, existing)
		if err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return &comment, nil
	}
	q := db.New(tx)
	comment, err := q.CreateComment(ctx, db.CreateCommentParams{
		IssueID:     msg.Run.IssueID,
		WorkspaceID: msg.Run.WorkspaceID,
		AuthorType:  "member",
		AuthorID:    msg.Run.OwnerID,
		Content:     msg.Request.Content,
		Type:        "comment",
		ParentID:    msg.Run.TopCommentID,
	})
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE local_cli_message
		SET comment_id = $2
		WHERE id = $1
	`, msg.MessageID, comment.ID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &comment, nil
}

func (h *Handler) completeLocalCLIMessageOutbox(ctx context.Context, id pgtype.UUID) error {
	if h.DB == nil {
		return nil
	}
	_, err := h.DB.Exec(ctx, `
		UPDATE local_cli_message_outbox
		SET status = 'done',
		    processed_at = now(),
		    locked_until = NULL,
		    updated_at = now()
		WHERE id = $1
	`, id)
	return err
}

func (h *Handler) failLocalCLIMessageOutbox(ctx context.Context, item localCLIMessageOutboxItem, err error) {
	if h.DB == nil {
		return
	}
	backoff := localCLIOutboxBackoff(item.Attempts)
	_, execErr := h.DB.Exec(ctx, `
		UPDATE local_cli_message_outbox
		SET status = 'pending',
		    last_error = $2,
		    next_attempt_at = $3,
		    locked_until = NULL,
		    updated_at = now()
		WHERE id = $1
	`, item.ID, err.Error(), time.Now().Add(backoff))
	if execErr != nil {
		slog.Warn("local cli message outbox: failed to record failure", "error", execErr)
	}
}

func localCLIOutboxBackoff(attempts int32) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	if attempts > 8 {
		attempts = 8
	}
	return time.Duration(1<<uint(attempts-1)) * time.Second
}
