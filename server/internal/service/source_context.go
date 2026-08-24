package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

const (
	SourceContextVersion     = int16(1)
	SourceContextUsage       = "read_only_historical_background"
	SourceContextMaxComments = 256
	// SourceContextMaxTextBytes preserves the general/manual capture limit.
	SourceContextMaxTextBytes = 1 << 20
	// SourceContextMaxAgentSnapshotBytes is lower because agent mode quotes the
	// immutable snapshot into its first user prompt.
	SourceContextMaxAgentSnapshotBytes = 64 << 10
	// SourceContextMaxAgentInputBytes includes the snapshot plus the new
	// instruction. A tokenizer cannot emit more tokens than the input has bytes;
	// keeping the dynamic input below 72 KiB leaves more than 100k tokens for the
	// stable quick-create wrapper, runtime brief, system prompt, and tools.
	SourceContextMaxAgentInputBytes  = 72 << 10
	SourceContextMaxAgentPromptBytes = 96 << 10
	SourceContextMaxAttachments      = 100
	SourceContextMaxAttachmentBytes  = int64(500 << 20)
)

var (
	ErrAnchorCommentDeleted          = errors.New("anchor comment deleted")
	ErrSourceIssueDeleted            = errors.New("source issue deleted")
	ErrSourceContextChanged          = errors.New("source context changed")
	ErrSourceContextInvalid          = errors.New("source context invalid comment thread")
	ErrSourceContextTooLarge         = errors.New("source context too large")
	ErrSourceContextRetryUnavailable = errors.New("source context retry unavailable")
)

func validateSourceContextAgentBytes(snapshotBytes int, prompt string) error {
	if snapshotBytes < 0 || snapshotBytes > SourceContextMaxAgentSnapshotBytes || snapshotBytes+len(prompt) > SourceContextMaxAgentInputBytes {
		return ErrSourceContextTooLarge
	}
	return nil
}

// ValidateSourceContextAgentInput keeps every accepted source-context
// quick-create within the prompt budget that the daemon regression test locks
// against its complete user-message wrapper. Project the persisted snapshot,
// including capture metadata, cloned attachment IDs, and rewritten URLs, so
// the preview payload cannot undercount what the daemon will actually quote.
// This runs before object cloning or task enqueue and has no external effects.
func ValidateSourceContextAgentInput(build SourceContextBuild, capturedBy pgtype.UUID, prompt string) error {
	projectedCloneID := pgtype.UUID{Valid: true}
	clones := make([]SourceContextClone, len(build.Rows))
	for i := range clones {
		clones[i].ID = projectedCloneID
	}
	// RFC3339Nano emits its longest representation when nanoseconds are nonzero.
	projectedCapturedAt := time.Date(2000, time.January, 1, 0, 0, 0, 999999999, time.UTC)
	projected, err := PrepareSourceContextCapture(
		build,
		pgtype.UUID{Valid: true},
		pgtype.UUID{Valid: true},
		capturedBy,
		projectedCapturedAt,
		clones,
	)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(projected.Snapshot)
	if err != nil {
		return fmt.Errorf("marshal projected source context: %w", err)
	}
	return validateSourceContextAgentBytes(len(payload), prompt)
}

type SourceContextLimitUsage struct {
	CommentCount    int   `json:"comment_count"`
	TextBytes       int   `json:"text_bytes"`
	AttachmentCount int   `json:"attachment_count"`
	AttachmentBytes int64 `json:"attachment_bytes"`
}

type SourceContextAttachment struct {
	ID                 string `json:"id"`
	SourceAttachmentID string `json:"source_attachment_id,omitempty"`
	OwnerType          string `json:"owner_type"`
	OwnerID            string `json:"owner_id"`
	Filename           string `json:"filename"`
	ContentType        string `json:"content_type"`
	SizeBytes          int64  `json:"size_bytes"`
	CreatedAt          string `json:"created_at"`
}

type SourceContextAuthor struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

type SourceContextIssueSnapshot struct {
	ID          string                    `json:"id"`
	Identifier  string                    `json:"identifier"`
	Number      int32                     `json:"number"`
	Title       string                    `json:"title"`
	Description *string                   `json:"description"`
	CreatedAt   string                    `json:"created_at"`
	UpdatedAt   string                    `json:"updated_at"`
	Revision    int64                     `json:"revision"`
	Attachments []SourceContextAttachment `json:"attachments"`
}

type SourceContextCommentSnapshot struct {
	ID          string                    `json:"id"`
	ParentID    *string                   `json:"parent_id"`
	Type        string                    `json:"type"`
	Content     string                    `json:"content"`
	Author      SourceContextAuthor       `json:"author"`
	CreatedAt   string                    `json:"created_at"`
	UpdatedAt   string                    `json:"updated_at"`
	Revision    int64                     `json:"revision"`
	Attachments []SourceContextAttachment `json:"attachments"`
}

type SourceContextSnapshot struct {
	Version          int16                          `json:"version,omitempty"`
	CapturedByUserID string                         `json:"captured_by_user_id,omitempty"`
	CapturedAt       string                         `json:"captured_at,omitempty"`
	SourceIssue      SourceContextIssueSnapshot     `json:"source_issue"`
	CommentThread    []SourceContextCommentSnapshot `json:"comment_thread"`
	AnchorCommentID  string                         `json:"anchor_comment_id"`
}

type SourceContextBuild struct {
	Snapshot SourceContextSnapshot
	Digest   string
	Token    string
	Limits   SourceContextLimitUsage
	Rows     []db.Attachment
}

type SourceContextClone struct {
	ID          pgtype.UUID
	Filename    string
	URL         string
	ContentType string
	SizeBytes   int64
}

type SourceContextCapture struct {
	ID               pgtype.UUID
	WorkspaceID      pgtype.UUID
	SourceIssueID    pgtype.UUID
	AnchorCommentID  pgtype.UUID
	CapturedByUserID pgtype.UUID
	CapturedAt       time.Time
	Snapshot         SourceContextSnapshot
	Digest           string
	Clones           []SourceContextClone
}

func sourceContextTime(v pgtype.Timestamptz) string {
	return v.Time.UTC().Format(time.RFC3339Nano)
}

func sourceContextText(v pgtype.Text) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}

func sourceContextAuthorName(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID, authorType string, authorID pgtype.UUID) (string, error) {
	switch authorType {
	case "member":
		user, err := q.GetUser(ctx, authorID)
		if err != nil {
			return "", fmt.Errorf("resolve source-context member author: %w", err)
		}
		return user.Name, nil
	case "agent":
		agent, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: authorID, WorkspaceID: workspaceID})
		if err != nil {
			return "", fmt.Errorf("resolve source-context agent author: %w", err)
		}
		return agent.Name, nil
	}
	return "", fmt.Errorf("unsupported source-context author type %q", authorType)
}

func attachmentSnapshot(row db.Attachment, ownerType, ownerID string) SourceContextAttachment {
	return SourceContextAttachment{
		ID:          util.UUIDToString(row.ID),
		OwnerType:   ownerType,
		OwnerID:     ownerID,
		Filename:    row.Filename,
		ContentType: row.ContentType,
		SizeBytes:   row.SizeBytes,
		CreatedAt:   sourceContextTime(row.CreatedAt),
	}
}

func sourceContextDigest(snapshot SourceContextSnapshot) (string, error) {
	canonical := snapshot
	// CommentThread is a slice, so copying the enclosing struct alone would keep
	// sharing its backing array with the snapshot that will be persisted. The
	// digest projection must never erase capture metadata from that snapshot.
	canonical.CommentThread = append([]SourceContextCommentSnapshot(nil), snapshot.CommentThread...)
	canonical.Version = 0
	canonical.CapturedByUserID = ""
	canonical.CapturedAt = ""
	canonical.SourceIssue.UpdatedAt = ""
	canonical.SourceIssue.Revision = 0
	for i := range canonical.CommentThread {
		// Display names are live identity metadata, not source content. A rename
		// between preview and creation must not invalidate an otherwise identical
		// capture; detail rendering reports the rename independently.
		canonical.CommentThread[i].Author.Name = ""
		canonical.CommentThread[i].UpdatedAt = ""
		canonical.CommentThread[i].Revision = 0
	}
	payload, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("marshal canonical source context: %w", err)
	}
	digestBytes := sha256.Sum256(payload)
	return hex.EncodeToString(digestBytes[:]), nil
}

// BuildSourceIssueSnapshot builds the issue-owned portion of a source
// snapshot without requiring the anchor comment to still exist. Detail
// rendering uses it to report source_issue_state accurately after a source
// comment (or one of its ancestors) has been deleted.
func BuildSourceIssueSnapshot(ctx context.Context, q *db.Queries, workspaceID, issueID pgtype.UUID) (SourceContextIssueSnapshot, []db.Attachment, error) {
	issue, err := q.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: issueID, WorkspaceID: workspaceID})
	if errors.Is(err, pgx.ErrNoRows) {
		return SourceContextIssueSnapshot{}, nil, ErrSourceIssueDeleted
	}
	if err != nil {
		return SourceContextIssueSnapshot{}, nil, fmt.Errorf("load source issue: %w", err)
	}
	workspace, err := q.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return SourceContextIssueSnapshot{}, nil, fmt.Errorf("load source workspace: %w", err)
	}
	attachments, err := q.ListSourceContextIssueAttachments(ctx, db.ListSourceContextIssueAttachmentsParams{IssueID: issue.ID, WorkspaceID: workspaceID})
	if err != nil {
		return SourceContextIssueSnapshot{}, nil, fmt.Errorf("load issue attachments: %w", err)
	}
	sort.Slice(attachments, func(i, j int) bool {
		return util.UUIDToString(attachments[i].ID) < util.UUIDToString(attachments[j].ID)
	})
	attachmentSnapshots := make([]SourceContextAttachment, 0, len(attachments))
	for _, attachment := range attachments {
		attachmentSnapshots = append(attachmentSnapshots, attachmentSnapshot(attachment, "issue", util.UUIDToString(issue.ID)))
	}
	return SourceContextIssueSnapshot{
		ID: util.UUIDToString(issue.ID), Identifier: workspace.IssuePrefix + "-" + fmt.Sprint(issue.Number), Number: issue.Number,
		Title: issue.Title, Description: sourceContextText(issue.Description),
		CreatedAt: sourceContextTime(issue.CreatedAt), UpdatedAt: sourceContextTime(issue.UpdatedAt), Revision: issue.Revision,
		Attachments: attachmentSnapshots,
	}, attachments, nil
}

func buildSourceContextCommentSnapshots(
	ctx context.Context,
	q *db.Queries,
	workspaceID pgtype.UUID,
	issueID pgtype.UUID,
	historyRows []db.ListCommentThreadHistoryRow,
) ([]SourceContextCommentSnapshot, []db.Attachment, error) {
	commentIDs := make([]pgtype.UUID, 0, len(historyRows))
	for _, row := range historyRows {
		commentIDs = append(commentIDs, row.ID)
	}
	commentAttachments := make([]db.Attachment, 0)
	if len(commentIDs) > 0 {
		var err error
		commentAttachments, err = q.ListSourceContextCommentAttachments(ctx, db.ListSourceContextCommentAttachmentsParams{
			WorkspaceID: workspaceID, IssueID: issueID, CommentIds: commentIDs,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("load comment attachments: %w", err)
		}
	}
	sort.Slice(commentAttachments, func(i, j int) bool {
		return util.UUIDToString(commentAttachments[i].ID) < util.UUIDToString(commentAttachments[j].ID)
	})

	byComment := make(map[string][]SourceContextAttachment)
	for _, attachment := range commentAttachments {
		commentID := util.UUIDToString(attachment.CommentID)
		byComment[commentID] = append(byComment[commentID], attachmentSnapshot(attachment, "comment", commentID))
	}
	commentThread := make([]SourceContextCommentSnapshot, 0, len(historyRows))
	for _, row := range historyRows {
		commentID := util.UUIDToString(row.ID)
		authorName, err := sourceContextAuthorName(ctx, q, workspaceID, row.AuthorType, row.AuthorID)
		if err != nil {
			return nil, nil, err
		}
		var parentID *string
		if row.ParentID.Valid {
			value := util.UUIDToString(row.ParentID)
			parentID = &value
		}
		attachments := byComment[commentID]
		if attachments == nil {
			attachments = []SourceContextAttachment{}
		}
		commentThread = append(commentThread, SourceContextCommentSnapshot{
			ID: commentID, ParentID: parentID, Type: row.Type, Content: row.Content,
			Author:    SourceContextAuthor{Type: row.AuthorType, ID: util.UUIDToString(row.AuthorID), Name: authorName},
			CreatedAt: sourceContextTime(row.CreatedAt), UpdatedAt: sourceContextTime(row.UpdatedAt), Revision: row.Revision,
			Attachments: attachments,
		})
	}
	return commentThread, commentAttachments, nil
}

// BuildSourceContextThreadAtCaptureBoundary rebuilds the surviving portion of
// a captured thread when its anchor no longer exists. The immutable root and
// anchor timestamp preserve the original chronological boundary, allowing the
// detail response to classify changes to every captured node instead of only
// reporting the deleted anchor.
func BuildSourceContextThreadAtCaptureBoundary(
	ctx context.Context,
	q *db.Queries,
	workspaceID pgtype.UUID,
	captured SourceContextSnapshot,
) ([]SourceContextCommentSnapshot, error) {
	if len(captured.CommentThread) == 0 || captured.CommentThread[0].ParentID != nil {
		return nil, ErrSourceContextInvalid
	}
	var anchor *SourceContextCommentSnapshot
	for i := range captured.CommentThread {
		if captured.CommentThread[i].ID == captured.AnchorCommentID {
			anchor = &captured.CommentThread[i]
			break
		}
	}
	if anchor == nil {
		return nil, ErrSourceContextInvalid
	}
	rootID, err := util.ParseUUID(captured.CommentThread[0].ID)
	if err != nil {
		return nil, fmt.Errorf("parse captured thread root: %w", err)
	}
	issueID, err := util.ParseUUID(captured.SourceIssue.ID)
	if err != nil {
		return nil, fmt.Errorf("parse captured source issue: %w", err)
	}
	anchorID, err := util.ParseUUID(captured.AnchorCommentID)
	if err != nil {
		return nil, fmt.Errorf("parse captured anchor: %w", err)
	}
	anchorCreatedAt, err := time.Parse(time.RFC3339Nano, anchor.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse captured anchor timestamp: %w", err)
	}
	historyRows, err := q.ListCommentThreadHistory(ctx, db.ListCommentThreadHistoryParams{
		RootID: rootID, WorkspaceID: workspaceID, IssueID: issueID,
		AnchorCreatedAt: pgtype.Timestamptz{Time: anchorCreatedAt, Valid: true}, AnchorID: anchorID,
		RowLimit: SourceContextMaxComments + 1,
	})
	if err != nil {
		return nil, fmt.Errorf("load source thread at capture boundary: %w", err)
	}
	commentThread, _, err := buildSourceContextCommentSnapshots(ctx, q, workspaceID, issueID, historyRows)
	return commentThread, err
}

func BuildSourceContext(ctx context.Context, q *db.Queries, workspaceID, anchorCommentID pgtype.UUID) (SourceContextBuild, error) {
	anchor, err := q.GetCommentInWorkspace(ctx, db.GetCommentInWorkspaceParams{ID: anchorCommentID, WorkspaceID: workspaceID})
	if errors.Is(err, pgx.ErrNoRows) {
		return SourceContextBuild{}, ErrAnchorCommentDeleted
	}
	if err != nil {
		return SourceContextBuild{}, fmt.Errorf("load anchor comment: %w", err)
	}
	if anchor.Type != "comment" {
		return SourceContextBuild{}, ErrSourceContextInvalid
	}
	issueSnapshot, issueAttachments, err := BuildSourceIssueSnapshot(ctx, q, workspaceID, anchor.IssueID)
	if err != nil {
		return SourceContextBuild{}, err
	}
	ancestorRows, err := q.ListCommentAncestorPath(ctx, db.ListCommentAncestorPathParams{
		CommentID: anchorCommentID, WorkspaceID: workspaceID, IssueID: anchor.IssueID,
	})
	if err != nil {
		return SourceContextBuild{}, fmt.Errorf("load source path: %w", err)
	}
	if len(ancestorRows) == 0 || ancestorRows[len(ancestorRows)-1].ID != anchorCommentID || ancestorRows[0].ParentID.Valid {
		return SourceContextBuild{}, ErrSourceContextInvalid
	}
	if len(ancestorRows) > SourceContextMaxComments {
		return SourceContextBuild{Limits: SourceContextLimitUsage{CommentCount: len(ancestorRows)}}, ErrSourceContextTooLarge
	}
	seenAncestors := make(map[string]struct{}, len(ancestorRows))
	for _, row := range ancestorRows {
		id := util.UUIDToString(row.ID)
		if row.Cycle {
			return SourceContextBuild{}, ErrSourceContextInvalid
		}
		if _, exists := seenAncestors[id]; exists {
			return SourceContextBuild{}, ErrSourceContextInvalid
		}
		seenAncestors[id] = struct{}{}
	}

	historyRows, err := q.ListCommentThreadHistory(ctx, db.ListCommentThreadHistoryParams{
		RootID:          ancestorRows[0].ID,
		WorkspaceID:     workspaceID,
		IssueID:         anchor.IssueID,
		AnchorCreatedAt: anchor.CreatedAt,
		AnchorID:        anchorCommentID,
		RowLimit:        SourceContextMaxComments + 1,
	})
	if err != nil {
		return SourceContextBuild{}, fmt.Errorf("load source thread history: %w", err)
	}
	if len(historyRows) == 0 || historyRows[0].ID != ancestorRows[0].ID || historyRows[len(historyRows)-1].ID != anchorCommentID {
		return SourceContextBuild{}, ErrSourceContextInvalid
	}
	if len(historyRows) > SourceContextMaxComments {
		return SourceContextBuild{Limits: SourceContextLimitUsage{CommentCount: len(historyRows)}}, ErrSourceContextTooLarge
	}
	seenHistory := make(map[string]struct{}, len(historyRows))
	for _, row := range historyRows {
		id := util.UUIDToString(row.ID)
		if _, exists := seenHistory[id]; exists {
			return SourceContextBuild{}, ErrSourceContextInvalid
		}
		seenHistory[id] = struct{}{}
	}

	commentThread, commentAttachments, err := buildSourceContextCommentSnapshots(ctx, q, workspaceID, anchor.IssueID, historyRows)
	if err != nil {
		return SourceContextBuild{}, err
	}

	allRows := make([]db.Attachment, 0, len(issueAttachments)+len(commentAttachments))
	for _, attachment := range issueAttachments {
		allRows = append(allRows, attachment)
	}
	for _, attachment := range commentAttachments {
		allRows = append(allRows, attachment)
	}
	snapshot := SourceContextSnapshot{
		SourceIssue:     issueSnapshot,
		CommentThread:   commentThread,
		AnchorCommentID: util.UUIDToString(anchorCommentID),
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return SourceContextBuild{}, fmt.Errorf("marshal source context: %w", err)
	}
	limits := SourceContextLimitUsage{CommentCount: len(commentThread), TextBytes: len(payload), AttachmentCount: len(allRows)}
	for _, row := range allRows {
		limits.AttachmentBytes += row.SizeBytes
	}
	if limits.TextBytes > SourceContextMaxTextBytes || limits.AttachmentCount > SourceContextMaxAttachments || limits.AttachmentBytes > SourceContextMaxAttachmentBytes {
		return SourceContextBuild{Snapshot: snapshot, Limits: limits}, ErrSourceContextTooLarge
	}
	// Revisions and update timestamps are useful capture metadata, but they are
	// not part of the canonical content comparison. A source that was edited
	// and restored byte-for-byte must match the preview token again.
	digest, err := sourceContextDigest(snapshot)
	if err != nil {
		return SourceContextBuild{}, err
	}
	return SourceContextBuild{
		Snapshot: snapshot,
		Digest:   digest,
		Token:    "sha256:" + digest + ":" + util.UUIDToString(anchor.IssueID),
		Limits:   limits,
		Rows:     allRows,
	}, nil
}

func ParseSourceContextToken(token string) (digest string, sourceIssueID pgtype.UUID, ok bool) {
	parts := strings.Split(token, ":")
	if len(parts) != 3 || parts[0] != "sha256" || len(parts[1]) != sha256.Size*2 {
		return "", pgtype.UUID{}, false
	}
	if _, err := hex.DecodeString(parts[1]); err != nil {
		return "", pgtype.UUID{}, false
	}
	id, err := util.ParseUUID(parts[2])
	if err != nil {
		return "", pgtype.UUID{}, false
	}
	return parts[1], id, true
}

func PrepareSourceContextCapture(build SourceContextBuild, contextID, workspaceID, capturedBy pgtype.UUID, capturedAt time.Time, clones []SourceContextClone) (SourceContextCapture, error) {
	if len(clones) != len(build.Rows) {
		return SourceContextCapture{}, fmt.Errorf("source context clone count mismatch")
	}
	// The preview build can remain cached by the caller while storage cloning
	// runs. Deep-copy before replacing original attachment IDs with clone IDs so
	// the preview/token input remains immutable.
	snapshotJSON, err := json.Marshal(build.Snapshot)
	if err != nil {
		return SourceContextCapture{}, fmt.Errorf("copy source context snapshot: %w", err)
	}
	var snapshot SourceContextSnapshot
	if err := json.Unmarshal(snapshotJSON, &snapshot); err != nil {
		return SourceContextCapture{}, fmt.Errorf("copy source context snapshot: %w", err)
	}
	cloneBySource := make(map[string]string, len(clones))
	replacements := make([]string, 0, len(clones)*2)
	for i, row := range build.Rows {
		sourceID := util.UUIDToString(row.ID)
		cloneID := util.UUIDToString(clones[i].ID)
		sourceDownloadPath := util.AttachmentDownloadPath(sourceID)
		cloneDownloadPath := util.AttachmentDownloadPath(cloneID)
		cloneBySource[sourceID] = cloneID
		replacements = append(
			replacements,
			sourceDownloadPath,
			cloneDownloadPath,
		)
		// Public-storage deployments persist the attachment object's URL
		// directly. It is just as source-owned as the stable API route and must
		// resolve through the clone after the original object is removed.
		if row.Url != "" && row.Url != sourceDownloadPath {
			replacements = append(replacements, row.Url, cloneDownloadPath)
		}
	}
	replacer := strings.NewReplacer(replacements...)
	rewriteContent := func(content string) string {
		return replacer.Replace(content)
	}
	rewrite := func(items []SourceContextAttachment) {
		for i := range items {
			sourceID := items[i].ID
			items[i].SourceAttachmentID = sourceID
			items[i].ID = cloneBySource[sourceID]
		}
	}
	rewrite(snapshot.SourceIssue.Attachments)
	if snapshot.SourceIssue.Description != nil {
		description := rewriteContent(*snapshot.SourceIssue.Description)
		snapshot.SourceIssue.Description = &description
	}
	for i := range snapshot.CommentThread {
		rewrite(snapshot.CommentThread[i].Attachments)
		snapshot.CommentThread[i].Content = rewriteContent(snapshot.CommentThread[i].Content)
	}
	snapshot.Version = SourceContextVersion
	snapshot.CapturedByUserID = util.UUIDToString(capturedBy)
	snapshot.CapturedAt = capturedAt.UTC().Format(time.RFC3339Nano)
	return SourceContextCapture{
		ID: contextID, WorkspaceID: workspaceID,
		SourceIssueID: issueUUID(build.Snapshot.SourceIssue.ID), AnchorCommentID: issueUUID(build.Snapshot.AnchorCommentID),
		CapturedByUserID: capturedBy, CapturedAt: capturedAt.UTC(), Snapshot: snapshot, Digest: build.Digest, Clones: clones,
	}, nil
}

func issueUUID(value string) pgtype.UUID {
	id, err := util.ParseUUID(value)
	if err != nil {
		panic(err)
	}
	return id
}

func PersistSourceContext(ctx context.Context, q *db.Queries, capture SourceContextCapture, issueID, taskID pgtype.UUID) (db.IssueSourceContext, error) {
	state := "pending"
	var attachedAt pgtype.Timestamptz
	if issueID.Valid {
		state = "attached"
		attachedAt = pgtype.Timestamptz{Time: capture.CapturedAt, Valid: true}
	}
	snapshotJSON, err := json.Marshal(capture.Snapshot)
	if err != nil {
		return db.IssueSourceContext{}, err
	}
	row, err := q.CreateIssueSourceContext(ctx, db.CreateIssueSourceContextParams{
		ID: capture.ID, WorkspaceID: capture.WorkspaceID, IssueID: issueID, OriginTaskID: taskID,
		SourceIssueID: capture.SourceIssueID, AnchorCommentID: capture.AnchorCommentID,
		CapturedByUserID: capture.CapturedByUserID, SnapshotVersion: SourceContextVersion,
		Snapshot: snapshotJSON, CaptureDigest: capture.Digest, State: state,
		CapturedAt: pgtype.Timestamptz{Time: capture.CapturedAt, Valid: true}, AttachedAt: attachedAt,
	})
	if err != nil {
		return db.IssueSourceContext{}, err
	}
	for _, clone := range capture.Clones {
		if _, err := q.CreateSourceContextAttachment(ctx, db.CreateSourceContextAttachmentParams{
			ID: clone.ID, WorkspaceID: capture.WorkspaceID, SourceContextID: capture.ID,
			UploaderType: "member", UploaderID: capture.CapturedByUserID,
			Filename: clone.Filename, Url: clone.URL, ContentType: clone.ContentType, SizeBytes: clone.SizeBytes,
		}); err != nil {
			return db.IssueSourceContext{}, err
		}
	}
	intents, err := q.DeletePendingSourceContextObjectIntents(ctx, db.DeletePendingSourceContextObjectIntentsParams{
		WorkspaceID: capture.WorkspaceID, SourceContextID: capture.ID,
	})
	if err != nil {
		return db.IssueSourceContext{}, fmt.Errorf("clear source context object intents: %w", err)
	}
	if len(intents) != len(capture.Clones) {
		return db.IssueSourceContext{}, fmt.Errorf("source context object intent ownership changed: cleared %d, want %d", len(intents), len(capture.Clones))
	}
	return row, nil
}

// CleanupSourceContextObjectIntents reconciles clone uploads that never
// reached the context transaction (request cancellation, crash, or ambiguous
// storage/DB failure). The one-hour settle window is fenced by a DB state
// claim: once cleanup owns an intent, PersistSourceContext cannot delete the
// full pending intent set and therefore rolls back instead of attaching an
// object being reclaimed.
func (s *TaskService) CleanupSourceContextObjectIntents(ctx context.Context, limit int) (int, error) {
	if s == nil || s.SourceContextStorage == nil || limit <= 0 {
		return 0, nil
	}
	cleaned := 0
	for cleaned < limit {
		leaseToken := dbid.NewV7()
		intent, err := s.Queries.ClaimSourceContextObjectIntentForCleanup(ctx, leaseToken)
		if errors.Is(err, pgx.ErrNoRows) {
			break
		}
		if err != nil {
			return cleaned, fmt.Errorf("claim source context object intent: %w", err)
		}
		referenced, err := s.Queries.SourceContextObjectIntentIsReferenced(ctx, db.SourceContextObjectIntentIsReferencedParams{
			WorkspaceID: intent.WorkspaceID, SourceContextID: intent.SourceContextID,
			AttachmentID: intent.AttachmentID, ObjectUrl: intent.ObjectUrl,
		})
		if err != nil {
			_, _ = s.Queries.ReleaseSourceContextObjectIntent(ctx, db.ReleaseSourceContextObjectIntentParams{
				LastError: pgtype.Text{String: err.Error(), Valid: true}, StorageKey: intent.StorageKey, WorkspaceID: intent.WorkspaceID, LeaseToken: leaseToken,
			})
			return cleaned, fmt.Errorf("check source context object reference: %w", err)
		}
		if !referenced {
			deleteCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			deleteErr := s.SourceContextStorage.DeleteObject(deleteCtx, intent.StorageKey)
			cancel()
			if deleteErr != nil {
				_, _ = s.Queries.ReleaseSourceContextObjectIntent(ctx, db.ReleaseSourceContextObjectIntentParams{
					LastError: pgtype.Text{String: deleteErr.Error(), Valid: true}, StorageKey: intent.StorageKey, WorkspaceID: intent.WorkspaceID, LeaseToken: leaseToken,
				})
				continue
			}
		}
		changed, err := s.Queries.DeleteClaimedSourceContextObjectIntent(ctx, db.DeleteClaimedSourceContextObjectIntentParams{
			StorageKey: intent.StorageKey, WorkspaceID: intent.WorkspaceID, LeaseToken: leaseToken,
		})
		if err != nil || changed != 1 {
			return cleaned, fmt.Errorf("delete source context object intent: changed=%d: %w", changed, err)
		}
		cleaned++
	}
	return cleaned, nil
}

// CleanupAbandonedSourceContexts abandons terminal quick-create captures only
// after their 30-day retry window. Retry creation transfers origin_task_id to
// the successor, so the terminal row selected here is the latest attempt.
// Object deletion deliberately runs after the state transaction commits and
// without a row lock. Failed deletes leave the abandoned context and its
// attachment rows for an idempotent retry. Once every object is gone, the
// attachment inventory and full captured snapshot are removed together.
func (s *TaskService) CleanupAbandonedSourceContexts(ctx context.Context, limit int32) (int, error) {
	if s == nil || s.TxStarter == nil || s.SourceContextStorage == nil || limit <= 0 {
		return 0, nil
	}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin source context cleanup: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.Queries.WithTx(tx)
	contexts, err := qtx.ListExpiredPendingSourceContextsForCleanup(ctx, limit)
	if err != nil {
		return 0, fmt.Errorf("list expired source contexts: %w", err)
	}
	for _, sourceContext := range contexts {
		changed, err := qtx.AbandonIssueSourceContext(ctx, db.AbandonIssueSourceContextParams{
			ID: sourceContext.ID, WorkspaceID: sourceContext.WorkspaceID,
		})
		if err != nil || changed != 1 {
			return 0, fmt.Errorf("abandon expired source context: changed=%d: %w", changed, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit source context cleanup: %w", err)
	}

	abandoned, err := s.Queries.ListAbandonedSourceContextsForCleanup(ctx, limit)
	if err != nil {
		return 0, fmt.Errorf("list abandoned source contexts: %w", err)
	}
	removed := 0
	for _, sourceContext := range abandoned {
		attachments, err := s.Queries.ListAttachmentsBySourceContext(ctx, db.ListAttachmentsBySourceContextParams{
			WorkspaceID: sourceContext.WorkspaceID, SourceContextID: sourceContext.ID,
		})
		if err != nil {
			return removed, fmt.Errorf("list abandoned source context attachments: %w", err)
		}
		deleteFailed := false
		for _, attachment := range attachments {
			key := s.SourceContextStorage.KeyFromURL(attachment.Url)
			if err := s.SourceContextStorage.DeleteObject(ctx, key); err != nil {
				deleteFailed = true
				slog.Warn("source context cleanup: object delete failed; retained row for retry",
					"source_context_id", util.UUIDToString(sourceContext.ID), "attachment_id", util.UUIDToString(attachment.ID), "error", err)
			}
		}
		if deleteFailed {
			continue
		}
		if _, err := s.Queries.DeleteAttachmentsBySourceContext(ctx, db.DeleteAttachmentsBySourceContextParams{
			WorkspaceID: sourceContext.WorkspaceID, SourceContextID: sourceContext.ID,
		}); err != nil {
			return removed, fmt.Errorf("delete abandoned source context attachment rows: %w", err)
		}
		changed, err := s.Queries.DeleteAbandonedIssueSourceContext(ctx, db.DeleteAbandonedIssueSourceContextParams{
			WorkspaceID: sourceContext.WorkspaceID, ID: sourceContext.ID,
		})
		if err != nil {
			return removed, fmt.Errorf("delete abandoned source context: %w", err)
		}
		// Multiple server instances can sweep the same abandoned row. Storage
		// deletes are idempotent; a zero-row delete means another instance won
		// the final DB cleanup and is therefore already the desired outcome.
		if changed == 0 {
			continue
		}
		removed++
	}
	return removed, nil
}
