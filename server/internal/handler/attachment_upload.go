package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/imagemetadata"
	"github.com/multica-ai/multica/server/internal/storage"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Presigned direct-to-object-storage attachments.
//
// POST /api/attachments/presign-upload issues a PUT credential for one staging
// key and records an upload session; POST /api/attachments/{id}/complete-upload
// inspects what the client stored, copies it to a permanent key the client
// cannot write, and publishes the attachment row. The legacy multipart endpoint
// (/api/upload-file) is untouched, so every existing client keeps working.
//
// Why two steps rather than one "confirm" call taking a client-supplied key:
// the key decides what the finished attachment points at, and a client that
// chooses it can point an attachment at another workspace's object — or at an
// object it can keep rewriting after publication. Both keys here are
// server-generated, and the final one is never presigned.

const (
	// uploadPresignTTL bounds one PUT credential. Fifteen minutes covers the
	// slowest 100 MiB upload this deployment accepts and keeps a leaked URL
	// worthless quickly.
	uploadPresignTTL = 15 * time.Minute

	// uploadStagingPrefix namespaces objects that exist only between presign and
	// complete. No attachment row ever carries one of these keys, so nothing
	// serves them through any read path even while they exist.
	uploadStagingPrefix = "upload-staging"

	// attachmentMetadataProbeBytes bounds how much of a stored object
	// complete-upload reads to sniff its media type and extract image
	// dimensions. It is a bound, not a buffer of the file: a 100 MiB upload
	// reads this prefix and no more. Image headers — including the JPEG EXIF
	// block holding orientation — sit far inside it in practice; an image whose
	// metadata lies beyond loses only its dimensions, the same degrade the
	// shared extractor already applies to a malformed file.
	attachmentMetadataProbeBytes = 1 << 20

	// completeUploadGrace is how long past the presigned URL's expiry a
	// completion is still accepted: a PUT that began just before the credential
	// died may still be finishing. It is short and finite, so a session cannot
	// be completed at an arbitrary later point, and it is DERIVED from
	// expires_at rather than stored, so the two cannot disagree.
	completeUploadGrace = 5 * time.Minute

	// attachmentUploadStorageTimeout bounds the object-storage work inside one
	// complete-upload (HEAD + prefix read + server-side copy), so a
	// black-holed store cannot hold the session row lock — and, for a comment
	// attachment, the comment lock — indefinitely.
	attachmentUploadStorageTimeout = 2 * time.Minute

	// attachmentObjectCacheControl matches the multipart path's object metadata
	// so a directly-uploaded attachment caches identically.
	attachmentObjectCacheControl = "max-age=432000,public"

	// presignRequestBodyLimit bounds the metadata request body. These requests
	// never carry file bytes, so an oversized one is a client bug.
	presignRequestBodyLimit = 32 << 10
)

// Stable machine-readable error codes. A client decides whether to fall back to
// the legacy upload path from these, never from the English message and never
// from the status alone — 501 is the only status that means "this server can
// upload, just not this way", so 401/403/5xx must not be retried through the
// proxied path.
const (
	codeUploadPresignUnsupported = "attachment_presign_unsupported"
	codeUploadSessionNotFound    = "attachment_upload_session_not_found"
	codeUploadSessionExpired     = "attachment_upload_session_expired"
	codeUploadObjectMissing      = "attachment_upload_object_missing"
	codeUploadObjectChanged      = "attachment_upload_object_changed"
	codeUploadSizeMismatch       = "attachment_upload_size_mismatch"
	codeUploadTargetInvalid      = "attachment_upload_target_invalid"
)

const (
	// completedPublishState is the publish_state of a session whose attachment
	// row exists. The cleanup worker moving its own cleanup_state never changes
	// this, which is what keeps an idempotent replay working.
	completedPublishState = "completed"
)

// sessionExpired reports whether a session is past its hard completion
// deadline: expires_at plus completeUploadGrace.
//
// The deadline is derived, never stored, so it always follows expires_at — an
// operator or test that moves the expiry moves the deadline with it. Crucially
// this does NOT depend on the cleanup worker having reached the row: a session
// must stop being completable at its deadline even if the sweep is late, which
// is what makes the policy a policy rather than an accident of sweep timing.
func sessionExpired(session db.AttachmentUploadSession) bool {
	if !session.ExpiresAt.Valid {
		return true
	}
	return !time.Now().Before(session.ExpiresAt.Time.Add(completeUploadGrace))
}

// StorageSupportsDirectUpload reports whether a backend can serve the
// presigned-upload endpoints. The capability is all-or-nothing: a backend that
// can presign a PUT but not inspect or finalize the object would strand every
// upload it accepted, so the wiring checks the whole set before starting the
// cleanup worker or advertising the endpoints.
func StorageSupportsDirectUpload(s storage.Storage) bool {
	_, ok := storage.AsUploadCapable(s)
	return ok
}

type presignUploadRequest struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	// SizeBytes stays a plain int64 so it reads naturally at every call site.
	// Whether the client SENT it is tracked separately by sizeBytesSet, because
	// an omitted field and an explicit 0 mean different things: the multipart
	// endpoint has always published empty files, so 0 is valid, while a missing
	// size is a malformed request.
	SizeBytes     int64  `json:"size_bytes"`
	IssueID       string `json:"issue_id"`
	CommentID     string `json:"comment_id"`
	ChatSessionID string `json:"chat_session_id"`
	TaskID        string `json:"task_id"`

	// sizeBytesSet records whether the JSON body actually contained
	// "size_bytes". Hand-built requests (tests, internal callers) set the value
	// directly and are treated as present.
	sizeBytesSet bool
}

// UnmarshalJSON decodes the request while recording whether size_bytes was
// present. A struct-level decoder is used instead of a pointer field so the
// value stays a plain int64 everywhere it is read.
func (p *presignUploadRequest) UnmarshalJSON(data []byte) error {
	type plain presignUploadRequest
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var presence struct {
		SizeBytes *json.RawMessage `json:"size_bytes"`
	}
	if err := json.Unmarshal(data, &presence); err != nil {
		return err
	}
	*p = presignUploadRequest(decoded)
	p.sizeBytesSet = presence.SizeBytes != nil
	return nil
}

type presignUploadResponse struct {
	AttachmentID string            `json:"attachment_id"`
	UploadURL    string            `json:"upload_url"`
	Method       string            `json:"method"`
	Headers      map[string]string `json:"headers"`
	ExpiresIn    int64             `json:"expires_in"`
}

// uploadTarget is the validated binding an upload session carries. At most one
// owner group is set: an issue/comment attachment, a chat-session upload, or a
// task-scoped chat attachment. They are distinct because the completers that
// adopt an unbound upload only consider rows with every other owner NULL, so a
// row that must later be adopted by a comment cannot also carry a chat session.
type uploadTarget struct {
	issueID       pgtype.UUID
	commentID     pgtype.UUID
	chatSessionID pgtype.UUID
	taskID        pgtype.UUID
}

// PresignAttachmentUpload — POST /api/attachments/presign-upload
func (h *Handler) PresignAttachmentUpload(w http.ResponseWriter, r *http.Request) {
	r = h.withWakeupActor(r)
	capable, ok := storage.AsUploadCapable(h.Storage)
	if !ok {
		// 501, not 403: this server does accept the upload, just not this way.
		// The client falls back to /api/upload-file, which works everywhere.
		writeErrorCode(w, http.StatusNotImplemented, codeUploadPresignUnsupported,
			"this deployment's storage backend does not support presigned direct upload")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if _, err := h.getWorkspaceMember(r.Context(), userID, workspaceID); err != nil {
		writeError(w, http.StatusForbidden, "not a member of this workspace")
		return
	}

	var req presignUploadRequest
	if !decodeBoundedJSON(w, r, presignRequestBodyLimit, &req) {
		return
	}

	filename, contentType, sizeBytes, ok := validatePresignUploadRequest(w, req)
	if !ok {
		return
	}
	target, ok := h.resolveAttachmentUploadTarget(w, r, req, userID, workspaceID)
	if !ok {
		return
	}

	uploaderType, uploaderID := h.resolveActor(r, userID, workspaceID)

	sessionID, err := uuid.NewV7()
	if err != nil {
		slog.Error("failed to generate upload session id", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// The attachment id is fixed before the bytes exist so the client has one
	// identifier to complete with. The attachment ROW is only written by a
	// successful complete-upload.
	finalKey := attachmentObjectKey(workspaceID, userID, sessionID.String(), filename)
	stagingKey := uploadStagingPrefix + "/" + workspaceID + "/" + sessionID.String()

	expiresAt := time.Now().Add(uploadPresignTTL)
	uploadURL, headers, err := capable.PresignPut(r.Context(), stagingKey, uploadPresignTTL, contentType, sizeBytes)
	if err != nil {
		slog.Error("failed to presign upload", "error", err)
		writeError(w, http.StatusBadGateway, "failed to create upload URL")
		return
	}

	if _, err := h.Queries.CreateAttachmentUploadSession(r.Context(), db.CreateAttachmentUploadSessionParams{
		ID:                  pgtype.UUID{Bytes: sessionID, Valid: true},
		WorkspaceID:         parseUUID(workspaceID),
		ActorType:           uploaderType,
		ActorID:             parseUUID(uploaderID),
		ActorUserID:         parseUUID(userID),
		IssueID:             target.issueID,
		CommentID:           target.commentID,
		ChatSessionID:       target.chatSessionID,
		TaskID:              target.taskID,
		Filename:            filename,
		DeclaredContentType: contentType,
		ExpectedSize:        sizeBytes,
		StagingKey:          stagingKey,
		FinalKey:            finalKey,
		ExpiresAt:           pgtype.Timestamptz{Time: expiresAt, Valid: true},
	}); err != nil {
		slog.Error("failed to record upload session", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to start upload")
		return
	}

	writeJSON(w, http.StatusOK, presignUploadResponse{
		AttachmentID: sessionID.String(),
		UploadURL:    uploadURL,
		Method:       http.MethodPut,
		Headers:      headers,
		ExpiresIn:    int64(uploadPresignTTL / time.Second),
	})
}

// CompleteAttachmentUpload — POST /api/attachments/{id}/complete-upload
func (h *Handler) CompleteAttachmentUpload(w http.ResponseWriter, r *http.Request) {
	r = h.withWakeupActor(r)
	capable, ok := storage.AsUploadCapable(h.Storage)
	if !ok {
		writeErrorCode(w, http.StatusNotImplemented, codeUploadPresignUnsupported,
			"this deployment's storage backend does not support presigned direct upload")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	sessionUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "attachment id")
	if !ok {
		return
	}
	session, ok := h.loadAttachmentUploadSession(w, r, sessionUUID, workspaceID)
	if !ok {
		return
	}

	// Only the actor that asked for the credential may publish it, and only
	// while presenting the same authenticated user. An agent acting for a member
	// is a different actor from that member, so an agent cannot complete (and
	// thereby publish into a comment) a session a human started, nor the reverse.
	uploaderType, uploaderID := h.resolveActor(r, userID, workspaceID)
	if session.ActorType != uploaderType ||
		uuidToString(session.ActorID) != uploaderID ||
		uuidToString(session.ActorUserID) != userID {
		writeErrorCode(w, http.StatusNotFound, codeUploadSessionNotFound, "upload session not found")
		return
	}

	// Both the replay and the first publication must re-check the CURRENT
	// access of the session's target. The credential was issued earlier; a
	// member removed from a private project in the meantime must not be able to
	// publish into it, and must not be able to use a replay to read the
	// attachment it already published. The saved actor check above is not
	// enough on its own: it only proves the same caller came back.
	if !h.revalidateAttachmentUploadTarget(w, r, h.Queries, session) {
		return
	}

	if session.PublishState == completedPublishState {
		// Idempotent replay: hand back what the first completion published, so a
		// client that timed out can retry without creating anything new.
		//
		// cleanup_state is deliberately irrelevant here. The reclaim sweep moves
		// it while this row's attachment stays published, and refusing a replay
		// because the sweep had run would break the one case replay exists for:
		// a committed publish whose response was lost.
		h.writeCompletedAttachmentUpload(w, r, session)
		return
	}

	// A session that was never published is only completable inside its
	// deadline. Checking expires_at here — rather than trusting the sweep to
	// have noticed the row — is what makes the deadline real: the sweep runs
	// once a minute and may not have reached this row yet.
	if sessionExpired(session) {
		writeErrorCode(w, http.StatusConflict, codeUploadSessionExpired,
			"upload session expired; request a new upload URL")
		return
	}

	published, changed, ok := h.publishAttachmentUpload(w, r, capable, session, uploaderType, uploaderID)
	if !ok {
		return
	}
	if changed {
		h.publishAttachmentUploadEvents(r, published.created, uuidToString(session.WorkspaceID), uploaderType, uploaderID)
	}
	writeJSON(w, http.StatusOK, h.attachmentToResponse(published.row, attachmentURLModeFromRequest(r)))
}

// publishAttachmentUpload runs the completing transaction, publishing the staged
// object as a permanent attachment exactly once.
//
// The session row is locked for the whole transaction, so two concurrent
// completes serialize and the loser observes 'completed' instead of creating a
// second attachment row and a second revision bump. That lock also keeps the
// reconciler's claim (which skips locked rows) off the session while its staged
// object is being finalized.
//
// Holding the lock across the object-storage calls is deliberate and is why
// those calls carry their own timeout: releasing it would let a sweep claim the
// session and delete the staged object mid-copy. The store work is bounded and
// metadata-only from this process's side (a HEAD, a 1 MiB prefix read, and a
// server-side copy), so the hold is short against a working store and capped by
// attachmentUploadStorageTimeout against a broken one.
func (h *Handler) publishAttachmentUpload(
	w http.ResponseWriter,
	r *http.Request,
	capable storage.UploadCapable,
	session db.AttachmentUploadSession,
	uploaderType, uploaderID string,
) (publishedAttachmentUpload, bool, bool) {
	storageCtx, cancelStorage := context.WithTimeout(r.Context(), attachmentUploadStorageTimeout)
	defer cancelStorage()

	tx, err := h.beginWakeupWrite(r.Context())
	if err != nil {
		slog.Error("failed to begin attachment upload transaction", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to complete upload")
		return publishedAttachmentUpload{}, false, false
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	locked, err := qtx.LockAttachmentUploadSessionForComplete(r.Context(), session.ID)
	if err != nil {
		slog.Error("failed to lock upload session", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to complete upload")
		return publishedAttachmentUpload{}, false, false
	}
	if locked.PublishState != "pending" {
		// Another caller won the race while this one waited. Return what it
		// published without touching the finalized object.
		if err := tx.Commit(r.Context()); err != nil {
			slog.Error("failed to commit attachment upload transaction", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to complete upload")
			return publishedAttachmentUpload{}, false, false
		}
		attachment, ok := h.loadPublishedAttachment(w, r, locked)
		return attachment, false, ok
	}

	// Re-validate the target inside the transaction that publishes the row: a
	// target deleted, or access lost, while the bytes uploaded must refuse the
	// attachment rather than leave a row pointing at a gone or now-invisible
	// issue/comment/chat. This runs under the session lock, so the decision
	// cannot be raced by another completion.
	if !h.revalidateAttachmentUploadTarget(w, r, qtx, locked) {
		return publishedAttachmentUpload{}, false, false
	}
	if sessionExpired(locked) {
		writeErrorCode(w, http.StatusConflict, codeUploadSessionExpired,
			"upload session expired; request a new upload URL")
		return publishedAttachmentUpload{}, false, false
	}

	info, err := capable.HeadObject(storageCtx, locked.StagingKey)
	if err != nil {
		if errors.Is(err, storage.ErrObjectNotFound) {
			writeErrorCode(w, http.StatusConflict, codeUploadObjectMissing,
				"the uploaded object was not found; upload the file before completing")
			return publishedAttachmentUpload{}, false, false
		}
		slog.Error("failed to inspect uploaded object", "error", err)
		writeError(w, http.StatusBadGateway, "failed to inspect uploaded object")
		return publishedAttachmentUpload{}, false, false
	}
	if info.Size != locked.ExpectedSize {
		// The store's own count, not the client's declaration: a truncated or
		// padded PUT must not be published as the declared file.
		writeErrorCode(w, http.StatusConflict, codeUploadSizeMismatch,
			fmt.Sprintf("uploaded object is %d bytes, expected %d", info.Size, locked.ExpectedSize))
		return publishedAttachmentUpload{}, false, false
	}
	if info.Size > maxUploadSize {
		writeError(w, http.StatusRequestEntityTooLarge, "file exceeds the 100 MiB upload limit")
		return publishedAttachmentUpload{}, false, false
	}

	// Read the probe bytes from the EXACT version HEAD just measured. Passing
	// the ETag is what binds the description to the object: the presigned URL
	// stays usable, so a client can PUT a different same-length object between
	// the HEAD and this read. Without the condition the probe would describe
	// that object while the conditional copy (pinned to the HEAD's ETag) would
	// publish the other one — bytes and metadata describing different files.
	//
	// A verified-empty object is read as no bytes at all. There is nothing to
	// sniff, and asking the store for a byte RANGE of an empty object is a 416
	// on real S3-compatible servers — which is what made empty uploads fail
	// completion with a 502 while in-memory doubles happily returned an empty
	// slice.
	var probe []byte
	if info.Size > 0 {
		probe, err = capable.ReadPrefix(storageCtx, locked.StagingKey, attachmentMetadataProbeBytes, info.ETag)
		if err != nil {
			switch {
			case errors.Is(err, storage.ErrObjectNotFound):
				writeErrorCode(w, http.StatusConflict, codeUploadObjectMissing,
					"the uploaded object was not found; upload the file before completing")
			case errors.Is(err, storage.ErrSourceChanged):
				// The object moved between measurement and read. Refusing is the
				// only safe answer: re-reading would describe a version the copy
				// is not pinned to, and retrying the whole completion would race
				// the same way. The client re-uploads, which is cheap and
				// unambiguous.
				writeErrorCode(w, http.StatusConflict, codeUploadObjectChanged,
					"uploaded object changed while completing; upload the file again")
			default:
				slog.Error("failed to read uploaded object", "error", err)
				writeError(w, http.StatusBadGateway, "failed to read uploaded object")
			}
			return publishedAttachmentUpload{}, false, false
		}
	}

	// The uploader's declared Content-Type is deliberately not the published
	// one: a PUT header is client-controlled and would let an HTML or SVG
	// payload be published as an image. Sniff the stored bytes and let the
	// shared extension overrides win where Go's sniffer is known to be wrong,
	// exactly as the multipart path does.
	contentType := http.DetectContentType(probe)
	if ct, ok := extContentTypes[strings.ToLower(path.Ext(locked.Filename))]; ok {
		contentType = ct
	}

	// Copy the staged object to its permanent key under an If-Match on the ETag
	// just inspected, replacing its metadata. The client's presigned URL still
	// works until it expires, so without this precondition a re-PUT between
	// inspection and copy could substitute different bytes into a published
	// attachment.
	if err := capable.FinalizeObject(
		storageCtx,
		locked.StagingKey,
		info.ETag,
		locked.FinalKey,
		locked.ExpectedSize,
		contentType,
		storage.ContentDisposition(contentType, locked.Filename),
		attachmentObjectCacheControl,
	); err != nil {
		switch {
		case errors.Is(err, storage.ErrSourceChanged):
			writeErrorCode(w, http.StatusConflict, codeUploadObjectChanged,
				"uploaded object changed while completing; upload the file again")
		case errors.Is(err, storage.ErrObjectNotFound):
			writeErrorCode(w, http.StatusConflict, codeUploadObjectMissing,
				"the uploaded object was not found; upload the file before completing")
		default:
			slog.Error("failed to finalize uploaded object", "error", err)
			writeError(w, http.StatusBadGateway, "failed to finalize uploaded object")
		}
		return publishedAttachmentUpload{}, false, false
	}

	params := db.CreateAttachmentParams{
		ID:            locked.ID,
		WorkspaceID:   locked.WorkspaceID,
		UploaderType:  locked.ActorType,
		UploaderID:    locked.ActorID,
		Filename:      locked.Filename,
		ContentType:   contentType,
		SizeBytes:     locked.ExpectedSize,
		Url:           h.Storage.ObjectURL(locked.FinalKey),
		IssueID:       locked.IssueID,
		CommentID:     locked.CommentID,
		ChatSessionID: locked.ChatSessionID,
		TaskID:        locked.TaskID,
	}
	if dimensions, ok := imagemetadata.Dimensions(probe); ok {
		params.Width = pgtype.Int4{Int32: dimensions.Width, Valid: true}
		params.Height = pgtype.Int4{Int32: dimensions.Height, Valid: true}
	}

	var created db.CreateAttachmentRow
	if params.CommentID.Valid {
		// Comment attachments are written under the comment's lock, matching the
		// multipart path: a delete that commits while the object uploaded cannot
		// leave an attachment on a tombstone.
		if _, err := qtx.LockLiveComment(r.Context(), db.LockLiveCommentParams{
			ID:          params.CommentID,
			WorkspaceID: params.WorkspaceID,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeErrorCode(w, http.StatusConflict, codeUploadTargetInvalid,
					"the comment this upload targeted was deleted")
				return publishedAttachmentUpload{}, false, false
			}
			slog.Error("failed to lock comment for attachment", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to complete upload")
			return publishedAttachmentUpload{}, false, false
		}
	}

	if created, err = qtx.CreateAttachment(r.Context(), params); err != nil {
		slog.Error("failed to create attachment record", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to complete upload")
		return publishedAttachmentUpload{}, false, false
	}
	if _, err := qtx.CompleteAttachmentUploadSession(r.Context(), db.CompleteAttachmentUploadSessionParams{
		AttachmentID: locked.ID,
		ID:           locked.ID,
	}); err != nil {
		slog.Error("failed to mark upload session complete", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to complete upload")
		return publishedAttachmentUpload{}, false, false
	}

	if err := tx.Commit(r.Context()); err != nil {
		// The commit may or may not have landed. Delete neither object: a landed
		// commit means the attachment reads final_key, and a lost commit leaves
		// the session 'pending' so the same complete-upload can be replayed —
		// the staged object is still there, and re-copying the same source ETag
		// to the same destination key is idempotent.
		slog.Error("failed to commit attachment upload", "error", err, "session_id", uuidToString(locked.ID))
		writeError(w, http.StatusInternalServerError, "failed to complete upload")
		return publishedAttachmentUpload{}, false, false
	}

	// The staged object is garbage now that the attachment reads final_key. The
	// completing transaction set next_attempt_at = now(), so the reconciler
	// deletes it on its next sweep; deleting inline would put an object-store
	// round trip in the response path of every upload for no user-visible gain.
	return publishedAttachmentUpload{created: created, row: created.Attachment()}, true, true
}

// loadAttachmentUploadSession resolves the session named in the path, denying a
// cross-workspace id exactly like a missing one.
func (h *Handler) loadAttachmentUploadSession(w http.ResponseWriter, r *http.Request, sessionID pgtype.UUID, workspaceID string) (db.AttachmentUploadSession, bool) {
	session, err := h.Queries.GetAttachmentUploadSession(r.Context(), sessionID)
	if err != nil || uuidToString(session.WorkspaceID) != workspaceID {
		// Non-member and non-existent look identical, so the route never
		// confirms that a session id belongs to another workspace.
		writeErrorCode(w, http.StatusNotFound, codeUploadSessionNotFound, "upload session not found")
		return db.AttachmentUploadSession{}, false
	}
	return session, true
}

// writeCompletedAttachmentUpload answers a repeated complete-upload with the
// attachment the first one published, which is what makes the call safe to retry.
func (h *Handler) writeCompletedAttachmentUpload(w http.ResponseWriter, r *http.Request, session db.AttachmentUploadSession) {
	attachment, ok := h.loadPublishedAttachment(w, r, session)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, h.attachmentToResponse(attachment.row, attachmentURLModeFromRequest(r)))
}

// publishedAttachmentUpload is what a completion produced: the raw insert row
// (whose revision columns drive the realtime events) and the attachment model
// (which is what responses are rendered from).
type publishedAttachmentUpload struct {
	created db.CreateAttachmentRow
	row     db.Attachment
}

func (h *Handler) loadPublishedAttachment(w http.ResponseWriter, r *http.Request, session db.AttachmentUploadSession) (publishedAttachmentUpload, bool) {
	if !session.AttachmentID.Valid {
		// A completed session always names its attachment; this is only
		// reachable if a row was hand-edited or an older build left it unset.
		writeErrorCode(w, http.StatusNotFound, codeUploadSessionNotFound, "upload session has no published attachment")
		return publishedAttachmentUpload{}, false
	}
	attachment, err := h.Queries.GetAttachment(r.Context(), db.GetAttachmentParams{
		ID:          session.AttachmentID,
		WorkspaceID: session.WorkspaceID,
	})
	if err != nil {
		writeErrorCode(w, http.StatusNotFound, codeUploadSessionNotFound, "upload session has no published attachment")
		return publishedAttachmentUpload{}, false
	}
	// Re-check access on the attachment as it is bound NOW, not on the target
	// the session recorded at presign time.
	//
	// The session's own target is usually empty: the CLI uploads unbound first
	// and binds afterwards (`issue create --attachment` needs the file before
	// the issue exists), so a replay that only re-validated the session would
	// check nothing. The attachment's binding is what actually decides who may
	// read it, and it can change after publication.
	if !h.requireAttachmentAccessForRequest(w, r, session, attachment) {
		return publishedAttachmentUpload{}, false
	}
	return publishedAttachmentUpload{row: attachment}, true
}

// requireAttachmentAccessForRequest re-proves that this request may still act
// on a PUBLISHED attachment, using its current binding. It is the replay-path
// counterpart of revalidateAttachmentUploadTarget, which can only see the
// session's original target.
//
// Each branch mirrors the gate the corresponding read route applies, so a
// replay cannot hand out a response the attachment's own endpoints would deny.
func (h *Handler) requireAttachmentAccessForRequest(w http.ResponseWriter, r *http.Request, session db.AttachmentUploadSession, att db.Attachment) bool {
	workspaceID := uuidToString(session.WorkspaceID)
	userID := requestUserID(r)
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return false
	}
	if _, err := h.getWorkspaceMember(r.Context(), userID, workspaceID); err != nil {
		writeErrorCode(w, http.StatusForbidden, codeUploadTargetInvalid,
			"you no longer have access to this workspace")
		return false
	}
	// Project visibility covers issue-bound and comment-bound attachments,
	// including one adopted into a private project after it was published. This
	// is the same helper the attachment read routes use, so the two cannot
	// disagree about what "visible" means.
	if !h.requireAttachmentVisible(w, r, workspaceID, att) {
		return false
	}
	if att.CommentID.Valid {
		// A comment is written under its own lock at publication, but a replay
		// happens later: a comment deleted or tombstoned since then must not
		// yield attachment URLs.
		comment, err := h.Queries.GetCommentInWorkspace(r.Context(), db.GetCommentInWorkspaceParams{
			ID:          att.CommentID,
			WorkspaceID: att.WorkspaceID,
		})
		if err != nil || comment.DeletedAt.Valid {
			writeErrorCode(w, http.StatusConflict, codeUploadTargetInvalid,
				"the comment this upload was bound to no longer exists")
			return false
		}
	}
	if att.ChatSessionID.Valid && !att.ChatMessageID.Valid {
		// Chat-bound rows follow the same gates as a chat upload: the session
		// must still be a member-visible Chat and its agent still reachable.
		actorType, actorID := h.resolveActor(r, userID, workspaceID)
		chatSession, err := h.Queries.GetChatSessionInWorkspace(r.Context(), db.GetChatSessionInWorkspaceParams{
			ID:          att.ChatSessionID,
			WorkspaceID: att.WorkspaceID,
		})
		if err != nil {
			writeErrorCode(w, http.StatusConflict, codeUploadTargetInvalid,
				"the chat session this upload was bound to no longer exists")
			return false
		}
		if _, err := h.Queries.GetPublicChatSessionInWorkspace(r.Context(), db.GetPublicChatSessionInWorkspaceParams{
			ID:          att.ChatSessionID,
			WorkspaceID: att.WorkspaceID,
		}); err != nil {
			writeErrorCode(w, http.StatusConflict, codeUploadTargetInvalid,
				"the chat session this upload was bound to is no longer available")
			return false
		}
		agent, err := h.Queries.GetAgent(r.Context(), chatSession.AgentID)
		if err != nil {
			writeErrorCode(w, http.StatusConflict, codeUploadTargetInvalid,
				"the agent for this upload's chat session no longer exists")
			return false
		}
		if !h.canAccessPrivateAgent(r.Context(), agent, actorType, actorID, workspaceID) {
			writeErrorCode(w, http.StatusForbidden, codeUploadTargetInvalid,
				"you no longer have access to this chat session's agent")
			return false
		}
	}
	if att.TaskID.Valid && !att.ChatMessageID.Valid {
		// A task-scoped row is only meaningful to the run that produced it. A
		// replay from a different credential (or after that run ended) must not
		// resolve it.
		if r.Header.Get("X-Actor-Source") != "task_token" {
			writeErrorCode(w, http.StatusForbidden, codeUploadTargetInvalid,
				"task uploads may only be replayed from within the same agent task")
			return false
		}
		if boundTaskID := strings.TrimSpace(r.Header.Get("X-Task-ID")); !strings.EqualFold(boundTaskID, uuidToString(att.TaskID)) {
			writeErrorCode(w, http.StatusForbidden, codeUploadTargetInvalid,
				"this task token does not match the upload's task")
			return false
		}
	}
	return true
}

// revalidateAttachmentUploadTarget re-proves, from the CURRENT database state,
// that this caller may still act on the session's target. It is the same set of
// gates presign applies, so revocation between presign and complete is refused
// for both the first publication and an idempotent replay.
//
// It deliberately re-derives the caller from the request rather than trusting
// the session's stored actor: the stored actor proves who STARTED the upload,
// not what they may still reach. Every gate below consults live membership,
// project visibility, private-agent access or the task token, exactly as the
// corresponding route would.
func (h *Handler) revalidateAttachmentUploadTarget(w http.ResponseWriter, r *http.Request, qtx *db.Queries, session db.AttachmentUploadSession) bool {
	workspaceID := uuidToString(session.WorkspaceID)
	userID := requestUserID(r)
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return false
	}
	// A session can outlive a membership change; membership is the first thing
	// every path below assumes.
	if _, err := h.getWorkspaceMember(r.Context(), userID, workspaceID); err != nil {
		writeErrorCode(w, http.StatusForbidden, codeUploadTargetInvalid,
			"you no longer have access to this workspace")
		return false
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)

	if session.IssueID.Valid {
		issue, err := qtx.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
			ID:          session.IssueID,
			WorkspaceID: session.WorkspaceID,
		})
		if err != nil {
			writeErrorCode(w, http.StatusConflict, codeUploadTargetInvalid,
				"the issue this upload targeted no longer exists")
			return false
		}
		// The project gate is the one a member loses when they are removed from
		// a private project, so it must run on publication and on replay.
		if !h.requireProjectVisible(w, r, workspaceID, issue.ProjectID, http.StatusConflict,
			"the issue this upload targeted is no longer available") {
			return false
		}
	}
	if session.CommentID.Valid {
		comment, err := qtx.GetCommentInWorkspace(r.Context(), db.GetCommentInWorkspaceParams{
			ID:          session.CommentID,
			WorkspaceID: session.WorkspaceID,
		})
		if err != nil || comment.DeletedAt.Valid {
			writeErrorCode(w, http.StatusConflict, codeUploadTargetInvalid,
				"the comment this upload targeted no longer exists")
			return false
		}
		// A comment inherits its issue's project visibility. Checking only
		// existence let a revoked member publish into a private comment.
		if !h.requireIssueIDVisible(w, r, workspaceID, comment.IssueID, http.StatusConflict,
			"the comment this upload targeted is no longer available") {
			return false
		}
	}
	if session.ChatSessionID.Valid {
		// Mirror the presign gate: the session must still be a member-visible
		// Chat AND the caller must still be able to reach its agent. A cached
		// session id cannot be used to resurrect a command-only chat, and losing
		// access to a private agent withdraws the ability to upload into it.
		chatSession, err := qtx.GetChatSessionInWorkspace(r.Context(), db.GetChatSessionInWorkspaceParams{
			ID:          session.ChatSessionID,
			WorkspaceID: session.WorkspaceID,
		})
		if err != nil {
			writeErrorCode(w, http.StatusConflict, codeUploadTargetInvalid,
				"the chat session this upload targeted no longer exists")
			return false
		}
		if _, err := qtx.GetPublicChatSessionInWorkspace(r.Context(), db.GetPublicChatSessionInWorkspaceParams{
			ID:          session.ChatSessionID,
			WorkspaceID: session.WorkspaceID,
		}); err != nil {
			writeErrorCode(w, http.StatusConflict, codeUploadTargetInvalid,
				"the chat session this upload targeted is no longer available")
			return false
		}
		agent, err := qtx.GetAgent(r.Context(), chatSession.AgentID)
		if err != nil {
			writeErrorCode(w, http.StatusConflict, codeUploadTargetInvalid,
				"the agent for this upload's chat session no longer exists")
			return false
		}
		if !h.canAccessPrivateAgent(r.Context(), agent, actorType, actorID, workspaceID) {
			writeErrorCode(w, http.StatusForbidden, codeUploadTargetInvalid,
				"you no longer have access to this chat session's agent")
			return false
		}
	}
	if session.TaskID.Valid {
		// The task binding is a task-token scope, not a stored fact: re-prove
		// that THIS request is the same task-scoped run, that the task still
		// belongs to this actor, and that it has not finished. A stored task id
		// alone must never authorize a write.
		if r.Header.Get("X-Actor-Source") != "task_token" {
			writeErrorCode(w, http.StatusForbidden, codeUploadTargetInvalid,
				"task uploads may only be completed from within the same agent task")
			return false
		}
		if boundTaskID := strings.TrimSpace(r.Header.Get("X-Task-ID")); !strings.EqualFold(boundTaskID, uuidToString(session.TaskID)) {
			writeErrorCode(w, http.StatusForbidden, codeUploadTargetInvalid,
				"this task token does not match the upload's task")
			return false
		}
		task, err := qtx.GetAgentTaskInWorkspace(r.Context(), db.GetAgentTaskInWorkspaceParams{
			ID:          session.TaskID,
			WorkspaceID: session.WorkspaceID,
		})
		if err != nil || uuidToString(task.AgentID) != uuidToString(session.ActorID) || actorType != "agent" || actorID != uuidToString(session.ActorID) {
			writeErrorCode(w, http.StatusConflict, codeUploadTargetInvalid,
				"the task this upload targeted is no longer available")
			return false
		}
		if task.Status == "completed" || task.Status == "failed" || task.Status == "cancelled" {
			writeErrorCode(w, http.StatusConflict, codeUploadTargetInvalid,
				"the task this upload targeted has already finished")
			return false
		}
	}
	return true
}

// resolveAttachmentUploadTarget validates the requested binding and its access
// checks, mirroring UploadFile's form handling. ok=false means the response has
// already been written.
func (h *Handler) resolveAttachmentUploadTarget(w http.ResponseWriter, r *http.Request, req presignUploadRequest, userID, workspaceID string) (uploadTarget, bool) {
	var target uploadTarget
	hasDocument := strings.TrimSpace(req.IssueID) != "" || strings.TrimSpace(req.CommentID) != ""
	hasChat := strings.TrimSpace(req.ChatSessionID) != "" || strings.TrimSpace(req.TaskID) != ""
	if hasDocument && hasChat {
		writeErrorCode(w, http.StatusBadRequest, codeUploadTargetInvalid,
			"issue_id/comment_id cannot be combined with chat_session_id/task_id")
		return uploadTarget{}, false
	}
	if strings.TrimSpace(req.ChatSessionID) != "" && strings.TrimSpace(req.TaskID) != "" {
		writeErrorCode(w, http.StatusBadRequest, codeUploadTargetInvalid,
			"chat_session_id and task_id are mutually exclusive")
		return uploadTarget{}, false
	}

	if v := strings.TrimSpace(req.IssueID); v != "" {
		issueUUID, ok := parseUUIDOrBadRequest(w, v, "issue_id")
		if !ok {
			return uploadTarget{}, false
		}
		issue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
			ID:          issueUUID,
			WorkspaceID: parseUUID(workspaceID),
		})
		if err != nil {
			writeError(w, http.StatusForbidden, "invalid issue_id")
			return uploadTarget{}, false
		}
		if !h.requireProjectVisible(w, r, workspaceID, issue.ProjectID, http.StatusForbidden, "invalid issue_id") {
			return uploadTarget{}, false
		}
		target.issueID = issue.ID
	}
	if v := strings.TrimSpace(req.CommentID); v != "" {
		commentUUID, ok := parseUUIDOrBadRequest(w, v, "comment_id")
		if !ok {
			return uploadTarget{}, false
		}
		comment, err := h.Queries.GetComment(r.Context(), commentUUID)
		// A deleted comment's tombstone takes no attachments.
		if err != nil || uuidToString(comment.WorkspaceID) != workspaceID || comment.DeletedAt.Valid {
			writeError(w, http.StatusForbidden, "invalid comment_id")
			return uploadTarget{}, false
		}
		if !h.requireIssueIDVisible(w, r, workspaceID, comment.IssueID, http.StatusForbidden, "invalid comment_id") {
			return uploadTarget{}, false
		}
		target.commentID = comment.ID
	}
	if v := strings.TrimSpace(req.ChatSessionID); v != "" {
		// Require the member-visible Chat projection as well as private-agent
		// access, so a cached command-only session id cannot accept an upload
		// that later resurrects the session.
		session, ok := h.gatePublicChatSessionForUser(w, r, userID, workspaceID, v)
		if !ok {
			return uploadTarget{}, false
		}
		target.chatSessionID = session.ID
	}
	if v := strings.TrimSpace(req.TaskID); v != "" {
		// The same authoritative task-token boundary UploadFile enforces. X-Task-ID
		// is only trustworthy when the auth middleware set it from a task-scoped
		// mat_ token, so this is the only credential that may tag an upload with a
		// task — and only with its own.
		if r.Header.Get("X-Actor-Source") != "task_token" {
			writeError(w, http.StatusForbidden, "task_id upload is only available from within an agent task")
			return uploadTarget{}, false
		}
		taskUUID, ok := parseUUIDOrBadRequest(w, v, "task_id")
		if !ok {
			return uploadTarget{}, false
		}
		boundTaskID := strings.TrimSpace(r.Header.Get("X-Task-ID"))
		if boundTaskID == "" || !strings.EqualFold(boundTaskID, strings.TrimSpace(v)) {
			writeError(w, http.StatusForbidden, "task_id must match the request's task token")
			return uploadTarget{}, false
		}
		task, err := h.Queries.GetAgentTaskInWorkspace(r.Context(), db.GetAgentTaskInWorkspaceParams{
			ID:          taskUUID,
			WorkspaceID: parseUUID(workspaceID),
		})
		if err != nil {
			writeError(w, http.StatusForbidden, "invalid task_id")
			return uploadTarget{}, false
		}
		uploaderType, uploaderID := h.resolveActor(r, userID, workspaceID)
		if uploaderType != "agent" || uuidToString(task.AgentID) != uploaderID {
			writeError(w, http.StatusForbidden, "task_id upload requires the task's own agent")
			return uploadTarget{}, false
		}
		if !task.ChatSessionID.Valid {
			writeError(w, http.StatusBadRequest, "task_id upload requires a chat task")
			return uploadTarget{}, false
		}
		target.taskID = task.ID
		// Bind the session too so reads and GC classify the row consistently
		// before it gains a message id, exactly as the multipart path does.
		target.chatSessionID = task.ChatSessionID
	}
	return target, true
}

// publishAttachmentUploadEvents emits the same realtime events the multipart
// path emits, so a client watching an issue or comment sees a direct upload land
// exactly like a proxied one.
func (h *Handler) publishAttachmentUploadEvents(r *http.Request, created db.CreateAttachmentRow, workspaceID, uploaderType, uploaderID string) {
	if created.IssueRevision > 0 && created.IssueID.Valid {
		h.publish(protocol.EventIssueAttachmentsChanged, workspaceID, uploaderType, uploaderID, map[string]any{
			"issue_id":       uuidToString(created.IssueID),
			"issue_revision": created.IssueRevision,
		})
	}
	if created.CommentRevision > 0 && created.CommentID.Valid {
		if comment, err := h.Queries.GetCommentInWorkspace(r.Context(), db.GetCommentInWorkspaceParams{
			ID:          created.CommentID,
			WorkspaceID: created.WorkspaceID,
		}); err == nil {
			commentID := uuidToString(comment.ID)
			reactions := h.groupReactions(r, []pgtype.UUID{comment.ID})
			attachments := h.groupAttachments(r, []pgtype.UUID{comment.ID})
			h.publish(protocol.EventCommentUpdated, workspaceID, uploaderType, uploaderID, map[string]any{
				"comment": commentToResponse(comment, reactions[commentID], attachments[commentID]),
			})
		}
	}
}

// validatePresignUploadRequest normalizes and bounds the declared upload.
func validatePresignUploadRequest(w http.ResponseWriter, req presignUploadRequest) (filename, contentType string, sizeBytes int64, ok bool) {
	filename = strings.TrimSpace(req.Filename)
	if filename == "" {
		writeError(w, http.StatusBadRequest, "filename is required")
		return "", "", 0, false
	}
	// A path is never part of a filename: the object key is built from the
	// server's own id plus this extension, so accepting separators would only
	// let a client shape the key.
	if filename != path.Base(filename) || filename == "." || filename == ".." ||
		strings.ContainsAny(filename, `/\`) {
		writeError(w, http.StatusBadRequest, "filename must be a bare file name")
		return "", "", 0, false
	}
	if len(filename) > 255 {
		writeError(w, http.StatusBadRequest, "filename is too long")
		return "", "", 0, false
	}
	contentType = strings.TrimSpace(req.ContentType)
	if contentType == "" {
		writeError(w, http.StatusBadRequest, "content_type is required")
		return "", "", 0, false
	}
	// The declared type only shapes the PUT signature — never the published
	// object — but a malformed value would make signing fail in a way the client
	// cannot diagnose, so reject it here with a clear message.
	if _, _, err := mime.ParseMediaType(contentType); err != nil {
		writeError(w, http.StatusBadRequest, "content_type is not a valid media type")
		return "", "", 0, false
	}
	// A request that reached here through JSON with no size_bytes is malformed.
	// A directly constructed request carries no presence bit, so its value is
	// taken at face value.
	if !req.sizeBytesSet && req.SizeBytes != 0 {
		req.sizeBytesSet = true
	}
	if !req.sizeBytesSet && req.SizeBytes == 0 {
		writeError(w, http.StatusBadRequest, "size_bytes is required")
		return "", "", 0, false
	}
	sizeBytes = req.SizeBytes
	if sizeBytes < 0 {
		writeError(w, http.StatusBadRequest, "size_bytes must not be negative")
		return "", "", 0, false
	}
	// 0 is accepted: the multipart path has always published empty files, and a
	// client that cannot upload one here would also not fall back (a 400 is not
	// a fallback signal), so rejecting it would be a silent regression.
	if sizeBytes > maxUploadSize {
		writeError(w, http.StatusRequestEntityTooLarge, "file exceeds the 100 MiB upload limit")
		return "", "", 0, false
	}
	return filename, contentType, sizeBytes, true
}

// attachmentObjectKey builds the permanent key for a published attachment,
// matching the multipart path's layout so both writers produce one namespace.
func attachmentObjectKey(workspaceID, userID, attachmentID, filename string) string {
	if workspaceID != "" {
		return "workspaces/" + workspaceID + "/" + attachmentID + path.Ext(filename)
	}
	return "users/" + userID + "/" + attachmentID + path.Ext(filename)
}

// decodeBoundedJSON decodes a JSON request body into out, writing a 400 (or 413)
// on failure and reporting whether it succeeded.
func decodeBoundedJSON(w http.ResponseWriter, r *http.Request, limit int64, out any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		var maxBytes *http.MaxBytesError
		if errors.As(err, &maxBytes) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body is too large")
			return false
		}
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}
