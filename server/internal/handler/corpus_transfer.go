package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/corpustransfer"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	MaxCorpusTransferBytes      = int64(2 << 30)
	maxCorpusCreateRequestBytes = int64(20 << 20)
	maxCorpusACKRequestBytes    = int64(8 << 10)
	corpusTransferTTL           = 24 * time.Hour
	corpusVerificationLease     = 15 * time.Minute
	corpusCleanupTimeout        = 30 * time.Second
)

var errCorpusTransferConflict = errors.New("corpus transfer conflict")

type corpusTransferStorage interface {
	UploadStream(context.Context, string, io.Reader, int64, string, string) (string, error)
	GetReader(context.Context, string) (io.ReadCloser, error)
	DeleteObject(context.Context, string) error
}

type corpusTransferStore interface {
	CreateOrGetCorpusTransfer(context.Context, db.CreateOrGetCorpusTransferParams) (db.CorpusTransfer, error)
	GetCorpusTransfer(context.Context, db.GetCorpusTransferParams) (db.CorpusTransfer, error)
	ClaimCorpusTransferUpload(context.Context, db.ClaimCorpusTransferUploadParams) (db.CorpusTransfer, error)
	MarkCorpusTransferUploaded(context.Context, db.MarkCorpusTransferUploadedParams) (db.CorpusTransfer, error)
	FailCorpusTransferUpload(context.Context, db.FailCorpusTransferUploadParams) (db.CorpusTransfer, error)
	ClaimCorpusTransferVerification(context.Context, db.ClaimCorpusTransferVerificationParams) (db.CorpusTransfer, error)
	ConfirmCorpusTransfer(context.Context, db.ConfirmCorpusTransferParams) (db.CorpusTransfer, error)
	FailCorpusTransferVerification(context.Context, db.FailCorpusTransferVerificationParams) (db.CorpusTransfer, error)
	GetConfirmedCorpusTransferContent(context.Context, db.GetConfirmedCorpusTransferContentParams) (db.CorpusTransfer, error)
	AcknowledgeCorpusTransfer(context.Context, pgtype.UUID, pgtype.UUID, string, string, pgtype.UUID) (db.CorpusTransferAck, error)
}

type sqlCorpusTransferStore struct {
	*db.Queries
	txStarter txStarter
}

func (s *sqlCorpusTransferStore) AcknowledgeCorpusTransfer(ctx context.Context, workspaceID, transferID pgtype.UUID, sinkID, digest string, actorID pgtype.UUID) (db.CorpusTransferAck, error) {
	if s == nil || s.Queries == nil || s.txStarter == nil {
		return db.CorpusTransferAck{}, fmt.Errorf("corpus transfer ledger is unavailable")
	}
	tx, err := s.txStarter.Begin(ctx)
	if err != nil {
		return db.CorpusTransferAck{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := s.Queries.WithTx(tx)
	transfer, err := qtx.GetConfirmedCorpusTransferContent(ctx, db.GetConfirmedCorpusTransferContentParams{WorkspaceID: workspaceID, ID: transferID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.CorpusTransferAck{}, errCorpusTransferConflict
		}
		return db.CorpusTransferAck{}, err
	}
	if !transfer.VerifiedSha256.Valid || transfer.VerifiedSha256.String != digest {
		return db.CorpusTransferAck{}, errCorpusTransferConflict
	}

	ack, err := qtx.CreateCorpusTransferACK(ctx, db.CreateCorpusTransferACKParams{
		WorkspaceID: workspaceID, TransferID: transferID, SinkID: sinkID,
		ConfirmedSha256: digest, AcknowledgedBy: actorID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		ack, err = qtx.GetCorpusTransferACK(ctx, db.GetCorpusTransferACKParams{WorkspaceID: workspaceID, TransferID: transferID, SinkID: sinkID})
	}
	if err != nil {
		return db.CorpusTransferAck{}, err
	}
	if ack.ConfirmedSha256 != digest {
		return db.CorpusTransferAck{}, errCorpusTransferConflict
	}
	if _, err := qtx.MarkCorpusTransferAcked(ctx, db.MarkCorpusTransferAckedParams{
		WorkspaceID: workspaceID, ID: transferID,
		ConfirmedSha256: pgtype.Text{String: digest, Valid: true},
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.CorpusTransferAck{}, errCorpusTransferConflict
		}
		return db.CorpusTransferAck{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return db.CorpusTransferAck{}, err
	}
	return ack, nil
}

type createCorpusTransferRequest struct {
	IdempotencyKey string                         `json:"idempotency_key"`
	Manifest       corpustransfer.Manifest        `json:"manifest"`
	Archive        corpustransfer.ArchiveEnvelope `json:"archive"`
}

type corpusTransferResponse struct {
	TransferID  string                         `json:"transfer_id"`
	WorkspaceID string                         `json:"workspace_id"`
	State       string                         `json:"state"`
	Manifest    json.RawMessage                `json:"manifest"`
	Archive     corpustransfer.ArchiveEnvelope `json:"archive"`
	ExpiresAt   time.Time                      `json:"expires_at"`
	CreatedAt   time.Time                      `json:"created_at"`
	ConfirmedAt *time.Time                     `json:"confirmed_at,omitempty"`
	LastSuccess *time.Time                     `json:"last_success_at,omitempty"`
	Late        bool                           `json:"late"`
	Missing     bool                           `json:"missing"`
	RetryReason *string                        `json:"retry_reason,omitempty"`
}

type acknowledgeCorpusTransferRequest struct {
	SinkID          string `json:"sink_id"`
	ConfirmedSHA256 string `json:"confirmed_sha256"`
}

func (h *Handler) CreateCorpusTransfer(w http.ResponseWriter, r *http.Request) {
	if !h.requireCorpusTransferDependencies(w, true) {
		return
	}
	workspaceID, actorID, ok := corpusTransferRequestIDs(w, r)
	if !ok {
		return
	}
	var request createCorpusTransferRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxCorpusCreateRequestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid corpus transfer request")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid corpus transfer request")
		return
	}
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	if request.IdempotencyKey == "" || len(request.IdempotencyKey) > 128 {
		writeError(w, http.StatusBadRequest, "idempotency_key must be 1-128 characters")
		return
	}
	if request.Archive.Format != "zip" || request.Archive.SizeBytes < 1 || request.Archive.SizeBytes > MaxCorpusTransferBytes ||
		!validCorpusSHA256(request.Archive.SHA256) || !validCorpusSHA256(request.Archive.ManifestSHA256) {
		writeError(w, http.StatusBadRequest, "invalid archive envelope")
		return
	}
	manifestJSON, err := request.Manifest.CanonicalJSON()
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid manifest: "+err.Error())
		return
	}
	if sha256Bytes(manifestJSON) != request.Archive.ManifestSHA256 {
		writeError(w, http.StatusBadRequest, "manifest sha256 does not match canonical manifest")
		return
	}

	transferID := parseUUID(uuid.NewString())
	objectKey := fmt.Sprintf("workspaces/%s/corpus-transfers/%s/archive.zip", uuidToString(workspaceID), uuidToString(transferID))
	created, err := h.CorpusTransfers.CreateOrGetCorpusTransfer(r.Context(), db.CreateOrGetCorpusTransferParams{
		ID: transferID, WorkspaceID: workspaceID, ActorID: actorID,
		IdempotencyKey: request.IdempotencyKey, ObjectKey: objectKey,
		Manifest: manifestJSON, ManifestSha256: request.Archive.ManifestSHA256,
		ExpectedSizeBytes: request.Archive.SizeBytes, ExpectedSha256: request.Archive.SHA256,
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().UTC().Add(corpusTransferTTL), Valid: true},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create corpus transfer")
		return
	}
	if created.ManifestSha256 != request.Archive.ManifestSHA256 || created.ExpectedSizeBytes != request.Archive.SizeBytes || created.ExpectedSha256 != request.Archive.SHA256 {
		writeError(w, http.StatusConflict, "idempotency key belongs to a different transfer")
		return
	}
	status := http.StatusCreated
	if created.ID != transferID {
		status = http.StatusOK
	}
	writeJSON(w, status, corpusTransferToResponse(created))
}

func (h *Handler) GetCorpusTransfer(w http.ResponseWriter, r *http.Request) {
	if !h.requireCorpusTransferDependencies(w, false) {
		return
	}
	workspaceID, transferID, ok := corpusTransferPathIDs(w, r)
	if !ok {
		return
	}
	transfer, err := h.CorpusTransfers.GetCorpusTransfer(r.Context(), db.GetCorpusTransferParams{WorkspaceID: workspaceID, ID: transferID})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "corpus transfer not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read corpus transfer")
		return
	}
	writeJSON(w, http.StatusOK, corpusTransferToResponse(transfer))
}

func (h *Handler) UploadCorpusTransferContent(w http.ResponseWriter, r *http.Request) {
	if !h.requireCorpusTransferDependencies(w, true) {
		return
	}
	workspaceID, transferID, ok := corpusTransferPathIDs(w, r)
	if !ok {
		return
	}
	transfer, err := h.CorpusTransfers.GetCorpusTransfer(r.Context(), db.GetCorpusTransferParams{WorkspaceID: workspaceID, ID: transferID})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "corpus transfer not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read corpus transfer")
		return
	}
	if r.ContentLength < 0 {
		writeError(w, http.StatusLengthRequired, "Content-Length is required")
		return
	}
	if r.ContentLength != transfer.ExpectedSizeBytes {
		writeError(w, http.StatusBadRequest, "Content-Length does not match declared archive size")
		return
	}
	transfer, err = h.CorpusTransfers.ClaimCorpusTransferUpload(r.Context(), db.ClaimCorpusTransferUploadParams{WorkspaceID: workspaceID, ID: transferID})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusConflict, "corpus transfer upload is not claimable")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to claim corpus transfer upload")
		return
	}

	counter := &countingReader{reader: io.LimitReader(r.Body, transfer.ExpectedSizeBytes+1)}
	_, uploadErr := h.CorpusStorage.UploadStream(r.Context(), transfer.ObjectKey, counter, transfer.ExpectedSizeBytes, "application/zip", "archive.zip")
	if uploadErr == nil && counter.count == transfer.ExpectedSizeBytes {
		var probe [1]byte
		_, probeErr := counter.Read(probe[:])
		if probeErr != nil && !errors.Is(probeErr, io.EOF) {
			uploadErr = probeErr
		}
	}
	if uploadErr != nil || counter.count != transfer.ExpectedSizeBytes {
		failure := "upload_error"
		if uploadErr == nil {
			failure = "upload_size_mismatch"
		}
		h.cleanupFailedCorpusUpload(r.Context(), transfer, failure)
		writeError(w, http.StatusUnprocessableEntity, "archive upload did not match declared size")
		return
	}
	transfer, err = h.CorpusTransfers.MarkCorpusTransferUploaded(r.Context(), db.MarkCorpusTransferUploadedParams{WorkspaceID: workspaceID, ID: transferID})
	if err != nil {
		checkCtx, cancel := detachedCorpusContext(r.Context())
		observed, getErr := h.CorpusTransfers.GetCorpusTransfer(checkCtx, db.GetCorpusTransferParams{WorkspaceID: workspaceID, ID: transferID})
		cancel()
		if getErr == nil && observed.State == "uploaded" {
			writeJSON(w, http.StatusOK, corpusTransferToResponse(observed))
			return
		}
		if getErr == nil && observed.State == "uploading" {
			h.cleanupFailedCorpusUpload(r.Context(), observed, "upload_commit_error")
		} else {
			slog.Error("corpus upload commit result is ambiguous; preserving object for reconciliation",
				"workspace_id", uuidToString(workspaceID), "transfer_id", uuidToString(transferID), "error", err, "readback_error", getErr)
		}
		writeError(w, http.StatusInternalServerError, "failed to confirm corpus upload")
		return
	}
	writeJSON(w, http.StatusOK, corpusTransferToResponse(transfer))
}

func (h *Handler) CompleteCorpusTransfer(w http.ResponseWriter, r *http.Request) {
	if !h.requireCorpusTransferDependencies(w, true) {
		return
	}
	workspaceID, transferID, ok := corpusTransferPathIDs(w, r)
	if !ok {
		return
	}
	transfer, err := h.CorpusTransfers.GetCorpusTransfer(r.Context(), db.GetCorpusTransferParams{WorkspaceID: workspaceID, ID: transferID})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "corpus transfer not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read corpus transfer")
		return
	}
	if transfer.State == "confirmed" || transfer.State == "acked" {
		writeJSON(w, http.StatusOK, corpusTransferToResponse(transfer))
		return
	}
	verificationToken := parseUUID(uuid.NewString())
	transfer, err = h.CorpusTransfers.ClaimCorpusTransferVerification(r.Context(), db.ClaimCorpusTransferVerificationParams{
		VerificationToken:          verificationToken,
		VerificationLeaseExpiresAt: pgtype.Timestamptz{Time: time.Now().UTC().Add(corpusVerificationLease), Valid: true},
		WorkspaceID:                workspaceID, ID: transferID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusConflict, "corpus transfer is not ready for verification")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to claim corpus verification")
		return
	}
	reader, err := h.CorpusStorage.GetReader(r.Context(), transfer.ObjectKey)
	if err != nil {
		h.cleanupFailedCorpusVerification(r.Context(), transfer, verificationToken, "verification_read_error")
		writeError(w, http.StatusBadGateway, "failed to read stored archive")
		return
	}
	hash := sha256.New()
	size, copyErr := io.Copy(hash, io.LimitReader(reader, transfer.ExpectedSizeBytes+1))
	closeErr := reader.Close()
	digest := hex.EncodeToString(hash.Sum(nil))
	if copyErr != nil || closeErr != nil {
		h.cleanupFailedCorpusVerification(r.Context(), transfer, verificationToken, "verification_read_error")
		writeError(w, http.StatusBadGateway, "failed to verify stored archive")
		return
	}
	if size != transfer.ExpectedSizeBytes || digest != transfer.ExpectedSha256 {
		h.cleanupFailedCorpusVerification(r.Context(), transfer, verificationToken, "verification_mismatch")
		writeError(w, http.StatusUnprocessableEntity, "stored archive does not match declared size or sha256")
		return
	}
	transfer, err = h.CorpusTransfers.ConfirmCorpusTransfer(r.Context(), db.ConfirmCorpusTransferParams{
		VerifiedSizeBytes: pgtype.Int8{Int64: size, Valid: true},
		VerifiedSha256:    pgtype.Text{String: digest, Valid: true},
		WorkspaceID:       workspaceID, ID: transferID, VerificationToken: verificationToken,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusConflict, "corpus verification lease was lost")
		return
	}
	if err != nil {
		checkCtx, cancel := detachedCorpusContext(r.Context())
		observed, getErr := h.CorpusTransfers.GetCorpusTransfer(checkCtx, db.GetCorpusTransferParams{WorkspaceID: workspaceID, ID: transferID})
		cancel()
		if getErr == nil && (observed.State == "confirmed" || observed.State == "acked") {
			writeJSON(w, http.StatusOK, corpusTransferToResponse(observed))
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to confirm corpus transfer")
		return
	}
	writeJSON(w, http.StatusOK, corpusTransferToResponse(transfer))
}

func (h *Handler) DownloadCorpusTransferContent(w http.ResponseWriter, r *http.Request) {
	if !h.requireCorpusTransferDependencies(w, true) {
		return
	}
	workspaceID, transferID, ok := corpusTransferPathIDs(w, r)
	if !ok {
		return
	}
	transfer, err := h.CorpusTransfers.GetConfirmedCorpusTransferContent(r.Context(), db.GetConfirmedCorpusTransferContentParams{WorkspaceID: workspaceID, ID: transferID})
	if errors.Is(err, pgx.ErrNoRows) {
		if _, getErr := h.CorpusTransfers.GetCorpusTransfer(r.Context(), db.GetCorpusTransferParams{WorkspaceID: workspaceID, ID: transferID}); errors.Is(getErr, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "corpus transfer not found")
		} else if getErr == nil {
			writeError(w, http.StatusConflict, "corpus transfer is not confirmed")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to read corpus transfer")
		}
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read corpus transfer")
		return
	}
	reader, err := h.CorpusStorage.GetReader(r.Context(), transfer.ObjectKey)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to read stored archive")
		return
	}
	defer reader.Close()
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", transfer.ExpectedSizeBytes))
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": "archive.zip"}))
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(http.StatusOK)
	written, copyErr := io.CopyN(w, reader, transfer.ExpectedSizeBytes)
	if copyErr != nil {
		slog.Error("corpus content stream ended before verified size",
			"workspace_id", uuidToString(workspaceID), "transfer_id", uuidToString(transferID), "written", written, "expected", transfer.ExpectedSizeBytes, "error", copyErr)
		return
	}
	var probe [1]byte
	n, probeErr := reader.Read(probe[:])
	if n != 0 || (probeErr != nil && !errors.Is(probeErr, io.EOF)) {
		slog.Error("corpus content stream differs from verified size",
			"workspace_id", uuidToString(workspaceID), "transfer_id", uuidToString(transferID), "extra_bytes_observed", n, "error", probeErr)
	}
}

func (h *Handler) AcknowledgeCorpusTransfer(w http.ResponseWriter, r *http.Request) {
	if !h.requireCorpusTransferDependencies(w, false) {
		return
	}
	workspaceID, transferID, ok := corpusTransferPathIDs(w, r)
	if !ok {
		return
	}
	actorID, ok := parseUUIDOrBadRequest(w, requestUserID(r), "actor id")
	if !ok {
		return
	}
	var request acknowledgeCorpusTransferRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxCorpusACKRequestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid corpus transfer ACK")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid corpus transfer ACK")
		return
	}
	request.SinkID = strings.TrimSpace(request.SinkID)
	if request.SinkID == "" || len(request.SinkID) > 255 || strings.ContainsRune(request.SinkID, '\x00') || !validCorpusSHA256(request.ConfirmedSHA256) {
		writeError(w, http.StatusBadRequest, "invalid corpus transfer ACK")
		return
	}
	transfer, err := h.CorpusTransfers.GetCorpusTransfer(r.Context(), db.GetCorpusTransferParams{WorkspaceID: workspaceID, ID: transferID})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "corpus transfer not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read corpus transfer")
		return
	}
	if (transfer.State != "confirmed" && transfer.State != "acked") || !transfer.VerifiedSha256.Valid || transfer.VerifiedSha256.String != request.ConfirmedSHA256 {
		writeError(w, http.StatusConflict, "ACK digest does not match a confirmed transfer")
		return
	}
	ack, err := h.CorpusTransfers.AcknowledgeCorpusTransfer(r.Context(), workspaceID, transferID, request.SinkID, request.ConfirmedSHA256, actorID)
	if errors.Is(err, errCorpusTransferConflict) {
		writeError(w, http.StatusConflict, "conflicting corpus transfer ACK")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record corpus transfer ACK")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"transfer_id":      uuidToString(ack.TransferID),
		"sink_id":          ack.SinkID,
		"confirmed_sha256": ack.ConfirmedSha256,
		"acknowledged_at":  ack.AcknowledgedAt.Time,
	})
}

func (h *Handler) requireCorpusTransferDependencies(w http.ResponseWriter, requireStorage bool) bool {
	if h.CorpusTransfers == nil || (requireStorage && h.CorpusStorage == nil) {
		writeError(w, http.StatusServiceUnavailable, "corpus transfer service is unavailable")
		return false
	}
	return true
}

func corpusTransferRequestIDs(w http.ResponseWriter, r *http.Request) (pgtype.UUID, pgtype.UUID, bool) {
	workspaceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	actorID, ok := parseUUIDOrBadRequest(w, requestUserID(r), "actor id")
	return workspaceID, actorID, ok
}

func corpusTransferPathIDs(w http.ResponseWriter, r *http.Request) (pgtype.UUID, pgtype.UUID, bool) {
	workspaceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	transferID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "transferID"), "transfer id")
	return workspaceID, transferID, ok
}

func detachedCorpusContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(parent), corpusCleanupTimeout)
}

func (h *Handler) cleanupFailedCorpusUpload(parent context.Context, transfer db.CorpusTransfer, code string) {
	ctx, cancel := detachedCorpusContext(parent)
	defer cancel()
	_, ledgerErr := h.CorpusTransfers.FailCorpusTransferUpload(ctx, db.FailCorpusTransferUploadParams{
		FailureCode: pgtype.Text{String: code, Valid: true}, WorkspaceID: transfer.WorkspaceID, ID: transfer.ID,
	})
	if ledgerErr != nil {
		slog.Error("failed to persist corpus upload cleanup intent",
			"workspace_id", uuidToString(transfer.WorkspaceID), "transfer_id", uuidToString(transfer.ID), "error", ledgerErr)
	}
}

func (h *Handler) cleanupFailedCorpusVerification(parent context.Context, transfer db.CorpusTransfer, token pgtype.UUID, code string) {
	ctx, cancel := detachedCorpusContext(parent)
	defer cancel()
	_, ledgerErr := h.CorpusTransfers.FailCorpusTransferVerification(ctx, db.FailCorpusTransferVerificationParams{
		FailureCode: pgtype.Text{String: code, Valid: true}, WorkspaceID: transfer.WorkspaceID, ID: transfer.ID, VerificationToken: token,
	})
	if ledgerErr != nil {
		slog.Error("failed to persist corpus verification cleanup intent",
			"workspace_id", uuidToString(transfer.WorkspaceID), "transfer_id", uuidToString(transfer.ID), "error", ledgerErr)
	}
}

func corpusTransferToResponse(transfer db.CorpusTransfer) corpusTransferResponse {
	pastDeadline := time.Now().After(transfer.ExpiresAt.Time)
	confirmedButExpired := pastDeadline && transfer.State == "confirmed"
	response := corpusTransferResponse{
		TransferID: uuidToString(transfer.ID), WorkspaceID: uuidToString(transfer.WorkspaceID),
		State: transfer.State, Manifest: json.RawMessage(transfer.Manifest),
		Archive: corpustransfer.ArchiveEnvelope{
			Format: "zip", Filename: "archive.zip", SizeBytes: transfer.ExpectedSizeBytes,
			SHA256: transfer.ExpectedSha256, ManifestSHA256: transfer.ManifestSha256,
		},
		ExpiresAt: transfer.ExpiresAt.Time, CreatedAt: transfer.CreatedAt.Time,
		Late:    pastDeadline && transfer.State != "acked",
		Missing: transfer.State == "failed" || transfer.State == "expired" || transfer.State == "purged" || confirmedButExpired,
	}
	if transfer.ConfirmedAt.Valid {
		confirmed := transfer.ConfirmedAt.Time
		response.ConfirmedAt = &confirmed
		response.LastSuccess = &confirmed
	}
	if transfer.FailureCode.Valid {
		reason := transfer.FailureCode.String
		response.RetryReason = &reason
	}
	return response
}

func validCorpusSHA256(value string) bool {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func sha256Bytes(body []byte) string {
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:])
}

type countingReader struct {
	reader io.Reader
	count  int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.count += int64(n)
	return n, err
}
