package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/corpustransfer"
	"github.com/multica-ai/multica/server/internal/storage"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	testCorpusWorkspace  = "10000000-0000-0000-0000-000000000001"
	testCorpusActor      = "20000000-0000-0000-0000-000000000002"
	otherCorpusWorkspace = "30000000-0000-0000-0000-000000000003"
)

func TestCorpusTransferCreateIsValidatedIdempotentAndWorkspaceScoped(t *testing.T) {
	h, ledger, _ := newCorpusTransferTestHandler()
	payload := []byte("zip-payload")
	reqBody := validCorpusCreateBody(t, "same-key", payload)

	w := doCorpusHandler(t, h.CreateCorpusTransfer, http.MethodPost, "/api/workspaces/"+testCorpusWorkspace+"/corpus-transfers", testCorpusWorkspace, "", reqBody, -2)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body=%s", w.Code, w.Body.String())
	}
	transfer := ledger.onlyTransfer(t)

	w = doCorpusHandler(t, h.CreateCorpusTransfer, http.MethodPost, "/api/workspaces/"+testCorpusWorkspace+"/corpus-transfers", testCorpusWorkspace, "", reqBody, -2)
	if w.Code != http.StatusOK {
		t.Fatalf("idempotent create status = %d, body=%s", w.Code, w.Body.String())
	}
	if got := ledger.transferCount(); got != 1 {
		t.Fatalf("transfer count = %d, want 1", got)
	}

	w = doCorpusHandler(t, h.CreateCorpusTransfer, http.MethodPost, "/api/workspaces/"+testCorpusWorkspace+"/corpus-transfers", testCorpusWorkspace, "", append(append([]byte(nil), reqBody...), []byte("{}")...), -2)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("trailing create JSON status = %d, body=%s", w.Code, w.Body.String())
	}

	conflictBody := validCorpusCreateBody(t, "same-key", []byte("different"))
	w = doCorpusHandler(t, h.CreateCorpusTransfer, http.MethodPost, "/api/workspaces/"+testCorpusWorkspace+"/corpus-transfers", testCorpusWorkspace, "", conflictBody, -2)
	if w.Code != http.StatusConflict {
		t.Fatalf("idempotency conflict status = %d, body=%s", w.Code, w.Body.String())
	}

	w = doCorpusHandler(t, h.GetCorpusTransfer, http.MethodGet, "/api/workspaces/"+otherCorpusWorkspace+"/corpus-transfers/"+uuidToString(transfer.ID), otherCorpusWorkspace, uuidToString(transfer.ID), nil, -2)
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-workspace status = %d, body=%s", w.Code, w.Body.String())
	}

	var oversized map[string]any
	if err := json.Unmarshal(reqBody, &oversized); err != nil {
		t.Fatal(err)
	}
	oversized["archive"].(map[string]any)["size_bytes"] = float64(MaxCorpusTransferBytes + 1)
	body, _ := json.Marshal(oversized)
	w = doCorpusHandler(t, h.CreateCorpusTransfer, http.MethodPost, "/api/workspaces/"+testCorpusWorkspace+"/corpus-transfers", testCorpusWorkspace, "", body, -2)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("oversized create status = %d, body=%s", w.Code, w.Body.String())
	}
}

func TestCorpusTransferUploadRequiresExactSingleStreamingBody(t *testing.T) {
	h, ledger, objects := newCorpusTransferTestHandler()
	payload := []byte("zip-payload")
	transferID := createCorpusTransferForTest(t, h, ledger, "upload-ok", payload)

	w := doCorpusHandler(t, h.UploadCorpusTransferContent, http.MethodPut, "/content", testCorpusWorkspace, transferID, payload, int64(len(payload)))
	if w.Code != http.StatusOK {
		t.Fatalf("upload status = %d, body=%s", w.Code, w.Body.String())
	}
	if got := objects.bodyFor(ledger.get(t, testCorpusWorkspace, transferID).ObjectKey); !bytes.Equal(got, payload) {
		t.Fatalf("stored body = %q", got)
	}
	if got := ledger.get(t, testCorpusWorkspace, transferID).State; got != "uploaded" {
		t.Fatalf("state = %q, want uploaded", got)
	}

	w = doCorpusHandler(t, h.UploadCorpusTransferContent, http.MethodPut, "/content", testCorpusWorkspace, transferID, payload, int64(len(payload)))
	if w.Code != http.StatusConflict {
		t.Fatalf("duplicate upload status = %d, body=%s", w.Code, w.Body.String())
	}

	chunkedID := createCorpusTransferForTest(t, h, ledger, "chunked", payload)
	w = doCorpusHandler(t, h.UploadCorpusTransferContent, http.MethodPut, "/content", testCorpusWorkspace, chunkedID, payload, -1)
	if w.Code != http.StatusLengthRequired {
		t.Fatalf("chunked upload status = %d, body=%s", w.Code, w.Body.String())
	}

	truncatedID := createCorpusTransferForTest(t, h, ledger, "truncated", payload)
	w = doCorpusHandler(t, h.UploadCorpusTransferContent, http.MethodPut, "/content", testCorpusWorkspace, truncatedID, payload[:3], int64(len(payload)))
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("truncated upload status = %d, body=%s", w.Code, w.Body.String())
	}
	truncated := ledger.get(t, testCorpusWorkspace, truncatedID)
	if objects.wasDeleted(truncated.ObjectKey) || !truncated.CleanupPending {
		t.Fatalf("truncated upload cleanup = deleted %v, pending %v; want durable pending cleanup", objects.wasDeleted(truncated.ObjectKey), truncated.CleanupPending)
	}

	overlongID := createCorpusTransferForTest(t, h, ledger, "overlong", payload)
	w = doCorpusHandler(t, h.UploadCorpusTransferContent, http.MethodPut, "/content", testCorpusWorkspace, overlongID, append(append([]byte(nil), payload...), '!'), int64(len(payload)))
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("overlong upload status = %d, body=%s", w.Code, w.Body.String())
	}

	ambiguousID := createCorpusTransferForTest(t, h, ledger, "ambiguous-mark", payload)
	ledger.markUploadedErrorAfterCommit = true
	w = doCorpusHandler(t, h.UploadCorpusTransferContent, http.MethodPut, "/content", testCorpusWorkspace, ambiguousID, payload, int64(len(payload)))
	if w.Code != http.StatusOK || ledger.get(t, testCorpusWorkspace, ambiguousID).State != "uploaded" {
		t.Fatalf("ambiguous committed upload status/state = %d/%s", w.Code, ledger.get(t, testCorpusWorkspace, ambiguousID).State)
	}
	if objects.wasDeleted(ledger.get(t, testCorpusWorkspace, ambiguousID).ObjectKey) {
		t.Fatal("committed upload object was deleted after ambiguous DB error")
	}

	cancelledID := createCorpusTransferForTest(t, h, ledger, "cancelled-cleanup", payload)
	objects.uploadErr = fmt.Errorf("store failed")
	objects.rejectCanceledDelete = true
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w = doCorpusHandlerContext(t, ctx, h.UploadCorpusTransferContent, http.MethodPut, "/content", testCorpusWorkspace, cancelledID, payload, int64(len(payload)))
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("cancelled cleanup status = %d, body=%s", w.Code, w.Body.String())
	}
	cancelled := ledger.get(t, testCorpusWorkspace, cancelledID)
	if objects.wasDeleted(cancelled.ObjectKey) || cancelled.State != "failed" || !cancelled.CleanupPending {
		t.Fatalf("cancelled cleanup ledger = deleted %v, state %s, pending %v", objects.wasDeleted(cancelled.ObjectKey), cancelled.State, cancelled.CleanupPending)
	}
}

func TestCorpusTransferCompletionDownloadAndACK(t *testing.T) {
	h, ledger, objects := newCorpusTransferTestHandler()
	payload := []byte("zip-payload")

	expiredID := createCorpusTransferForTest(t, h, ledger, "expired-before-complete", payload)
	assertCorpusUpload(t, h, expiredID, payload)
	ledger.setExpiry(t, testCorpusWorkspace, expiredID, time.Now().Add(-time.Minute))
	w := doCorpusHandler(t, h.CompleteCorpusTransfer, http.MethodPost, "/complete", testCorpusWorkspace, expiredID, nil, -2)
	if w.Code != http.StatusConflict || ledger.get(t, testCorpusWorkspace, expiredID).State != "uploaded" {
		t.Fatalf("expired completion status/state = %d/%s body=%s", w.Code, ledger.get(t, testCorpusWorkspace, expiredID).State, w.Body.String())
	}

	badID := createCorpusTransferForTest(t, h, ledger, "bad-readback", payload)
	assertCorpusUpload(t, h, badID, payload)
	bad := ledger.get(t, testCorpusWorkspace, badID)
	objects.setBody(bad.ObjectKey, []byte("tampered"))
	w = doCorpusHandler(t, h.CompleteCorpusTransfer, http.MethodPost, "/complete", testCorpusWorkspace, badID, nil, -2)
	bad = ledger.get(t, testCorpusWorkspace, badID)
	if w.Code != http.StatusUnprocessableEntity || bad.State != "failed" || !bad.CleanupPending || objects.wasDeleted(bad.ObjectKey) {
		t.Fatalf("tampered completion = status %d, state %s, pending %v, deleted %v, body=%s", w.Code, bad.State, bad.CleanupPending, objects.wasDeleted(bad.ObjectKey), w.Body.String())
	}

	okID := createCorpusTransferForTest(t, h, ledger, "good-readback", payload)
	assertCorpusUpload(t, h, okID, payload)
	w = doCorpusHandler(t, h.CompleteCorpusTransfer, http.MethodPost, "/complete", testCorpusWorkspace, okID, nil, -2)
	if w.Code != http.StatusOK {
		t.Fatalf("complete status = %d, body=%s", w.Code, w.Body.String())
	}
	w = doCorpusHandler(t, h.CompleteCorpusTransfer, http.MethodPost, "/complete", testCorpusWorkspace, okID, nil, -2)
	if w.Code != http.StatusOK {
		t.Fatalf("idempotent complete status = %d, body=%s", w.Code, w.Body.String())
	}

	w = doCorpusHandler(t, h.DownloadCorpusTransferContent, http.MethodGet, "/content", testCorpusWorkspace, okID, nil, -2)
	if w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), payload) {
		t.Fatalf("download status/body = %d/%q", w.Code, w.Body.Bytes())
	}
	objects.setBody(ledger.get(t, testCorpusWorkspace, okID).ObjectKey, append(append([]byte(nil), payload...), []byte("excess")...))
	w = doCorpusHandler(t, h.DownloadCorpusTransferContent, http.MethodGet, "/content", testCorpusWorkspace, okID, nil, -2)
	if !bytes.Equal(w.Body.Bytes(), payload) {
		t.Fatalf("oversized download was not bounded: %q", w.Body.Bytes())
	}
	objects.setBody(ledger.get(t, testCorpusWorkspace, okID).ObjectKey, payload[:3])
	w = doCorpusHandler(t, h.DownloadCorpusTransferContent, http.MethodGet, "/content", testCorpusWorkspace, okID, nil, -2)
	if w.Header().Get("Content-Length") != fmt.Sprint(len(payload)) || !bytes.Equal(w.Body.Bytes(), payload[:3]) {
		t.Fatalf("truncated download evidence = length %q body %q", w.Header().Get("Content-Length"), w.Body.Bytes())
	}
	objects.setBody(ledger.get(t, testCorpusWorkspace, okID).ObjectKey, payload)
	ledger.setExpiry(t, testCorpusWorkspace, okID, time.Now().Add(-time.Minute))
	w = doCorpusHandler(t, h.GetCorpusTransfer, http.MethodGet, "/status", testCorpusWorkspace, okID, nil, -2)
	var expiredStatus corpusTransferResponse
	if err := json.Unmarshal(w.Body.Bytes(), &expiredStatus); err != nil {
		t.Fatal(err)
	}
	if !expiredStatus.Late || !expiredStatus.Missing {
		t.Fatalf("expired confirmed status late/missing = %v/%v", expiredStatus.Late, expiredStatus.Missing)
	}
	ledger.setExpiry(t, testCorpusWorkspace, okID, time.Now().Add(time.Hour))

	digest := sha256Hex(payload)
	ackBody, _ := json.Marshal(map[string]string{"sink_id": "stable-sink", "confirmed_sha256": digest})
	w = doCorpusHandler(t, h.AcknowledgeCorpusTransfer, http.MethodPost, "/acks", testCorpusWorkspace, okID, ackBody, -2)
	if w.Code != http.StatusOK {
		t.Fatalf("ack status = %d, body=%s", w.Code, w.Body.String())
	}
	w = doCorpusHandler(t, h.AcknowledgeCorpusTransfer, http.MethodPost, "/acks", testCorpusWorkspace, okID, ackBody, -2)
	if w.Code != http.StatusOK {
		t.Fatalf("ack replay status = %d, body=%s", w.Code, w.Body.String())
	}
	w = doCorpusHandler(t, h.AcknowledgeCorpusTransfer, http.MethodPost, "/acks", testCorpusWorkspace, okID, append(append([]byte(nil), ackBody...), []byte("{}")...), -2)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("trailing ACK JSON status = %d, body=%s", w.Code, w.Body.String())
	}
	conflictACK, _ := json.Marshal(map[string]string{"sink_id": "stable-sink", "confirmed_sha256": strings.Repeat("0", 64)})
	w = doCorpusHandler(t, h.AcknowledgeCorpusTransfer, http.MethodPost, "/acks", testCorpusWorkspace, okID, conflictACK, -2)
	if w.Code != http.StatusConflict {
		t.Fatalf("conflicting ack status = %d, body=%s", w.Code, w.Body.String())
	}
}

func TestCorpusTransferLocalStoragePackThroughACK(t *testing.T) {
	source := t.TempDir()
	duplicate := []byte("same private corpus payload")
	for name, body := range map[string][]byte{
		"皮皮/会话.jsonl": duplicate,
		"copy.jsonl":  duplicate,
	} {
		filename := filepath.Join(source, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, body, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	staging := filepath.Join(t.TempDir(), "staging")
	manifest, err := corpustransfer.StageAndBuildManifest(context.Background(), []corpustransfer.SourceRoot{{
		Type: "codex", Name: "pipi", Path: source,
	}}, time.Time{}, staging)
	if err != nil {
		t.Fatalf("pack manifest: %v", err)
	}
	hasNonASCIIPath := false
	hasReplica := false
	for _, entry := range manifest.Entries {
		hasNonASCIIPath = hasNonASCIIPath || strings.Contains(entry.Path, "皮皮/会话.jsonl")
		hasReplica = hasReplica || entry.ReplicaOf != ""
	}
	if manifest.EntryCount != 2 || !hasNonASCIIPath || !hasReplica {
		t.Fatalf("duplicate/non-ASCII pack evidence = %#v", manifest.Entries)
	}
	archivePath := filepath.Join(t.TempDir(), "archive.zip")
	packedEnvelope, err := corpustransfer.BuildZIP(staging, archivePath, manifest)
	if err != nil {
		t.Fatalf("pack archive: %v", err)
	}
	archiveBytes, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	manifestJSON, err := manifest.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if got := sha256Hex(archiveBytes); got != packedEnvelope.SHA256 {
		t.Fatalf("packed archive sha256 = %s, envelope = %s", got, packedEnvelope.SHA256)
	}
	if got := sha256Hex(manifestJSON); got != packedEnvelope.ManifestSHA256 {
		t.Fatalf("packed manifest sha256 = %s, envelope = %s", got, packedEnvelope.ManifestSHA256)
	}

	t.Setenv("LOCAL_UPLOAD_DIR", t.TempDir())
	localStore := storage.NewLocalStorageFromEnv()
	if localStore == nil {
		t.Fatal("initialize local storage")
	}
	ledger := &fakeCorpusTransferStore{transfers: make(map[string]db.CorpusTransfer), acks: make(map[string]db.CorpusTransferAck)}
	h := &Handler{CorpusTransfers: ledger, CorpusStorage: localStore}
	createBody, err := json.Marshal(createCorpusTransferRequest{
		IdempotencyKey: "local-storage-e2e",
		Manifest:       manifest,
		Archive:        packedEnvelope,
	})
	if err != nil {
		t.Fatal(err)
	}
	w := doCorpusHandler(t, h.CreateCorpusTransfer, http.MethodPost, "/corpus-transfers", testCorpusWorkspace, "", createBody, -2)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body=%s", w.Code, w.Body.String())
	}
	var receipt corpusTransferResponse
	if err := json.Unmarshal(w.Body.Bytes(), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Archive.SizeBytes != packedEnvelope.SizeBytes ||
		receipt.Archive.SHA256 != packedEnvelope.SHA256 ||
		receipt.Archive.ManifestSHA256 != packedEnvelope.ManifestSHA256 {
		t.Fatalf("receipt archive = %#v, packed = %#v", receipt.Archive, packedEnvelope)
	}
	var receiptManifest corpustransfer.Manifest
	if err := json.Unmarshal(receipt.Manifest, &receiptManifest); err != nil {
		t.Fatal(err)
	}
	receiptManifestJSON, err := receiptManifest.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(receiptManifestJSON, manifestJSON) || sha256Hex(receiptManifestJSON) != packedEnvelope.ManifestSHA256 {
		t.Fatal("receipt manifest does not match the packed canonical manifest")
	}

	transfer := ledger.get(t, testCorpusWorkspace, receipt.TransferID)
	w = doCorpusHandler(t, h.UploadCorpusTransferContent, http.MethodPut, "/content", testCorpusWorkspace, receipt.TransferID, archiveBytes, int64(len(archiveBytes)))
	if w.Code != http.StatusOK {
		t.Fatalf("upload status = %d, body=%s", w.Code, w.Body.String())
	}
	storedBytes, err := os.ReadFile(localStore.GetFilePath(transfer.ObjectKey))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(storedBytes, archiveBytes) {
		t.Fatal("local storage bytes differ from the packed archive")
	}

	w = doCorpusHandler(t, h.CompleteCorpusTransfer, http.MethodPost, "/complete", testCorpusWorkspace, receipt.TransferID, nil, -2)
	if w.Code != http.StatusOK {
		t.Fatalf("complete status = %d, body=%s", w.Code, w.Body.String())
	}
	confirmed := ledger.get(t, testCorpusWorkspace, receipt.TransferID)
	if confirmed.State != "confirmed" || !confirmed.VerifiedSizeBytes.Valid || confirmed.VerifiedSizeBytes.Int64 != packedEnvelope.SizeBytes ||
		!confirmed.VerifiedSha256.Valid || confirmed.VerifiedSha256.String != packedEnvelope.SHA256 {
		t.Fatalf("server readback evidence = state %s, size %#v, sha %#v", confirmed.State, confirmed.VerifiedSizeBytes, confirmed.VerifiedSha256)
	}

	w = doCorpusHandler(t, h.DownloadCorpusTransferContent, http.MethodGet, "/content", testCorpusWorkspace, receipt.TransferID, nil, -2)
	if w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), archiveBytes) {
		t.Fatalf("download status/size = %d/%d, want 200/%d", w.Code, w.Body.Len(), len(archiveBytes))
	}
	downloadedPath := filepath.Join(t.TempDir(), "downloaded.zip")
	if err := os.WriteFile(downloadedPath, w.Body.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	downloadedManifest, downloadedEnvelope, err := corpustransfer.InspectZIP(downloadedPath, corpustransfer.SourceInfo{})
	if err != nil {
		t.Fatalf("inspect downloaded archive: %v", err)
	}
	if downloadedEnvelope.SizeBytes != packedEnvelope.SizeBytes || downloadedEnvelope.SHA256 != packedEnvelope.SHA256 ||
		downloadedEnvelope.ManifestSHA256 != packedEnvelope.ManifestSHA256 {
		t.Fatalf("downloaded envelope = %#v, packed = %#v", downloadedEnvelope, packedEnvelope)
	}
	downloadedManifestJSON, err := downloadedManifest.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(downloadedManifestJSON, manifestJSON) {
		t.Fatal("downloaded manifest differs from packed manifest")
	}
	installed := filepath.Join(t.TempDir(), manifest.PackageID)
	if err := corpustransfer.ExtractVerified(downloadedPath, installed, downloadedManifest); err != nil {
		t.Fatalf("extract downloaded archive: %v", err)
	}
	if err := corpustransfer.VerifyExtracted(installed, downloadedManifest); err != nil {
		t.Fatalf("verify installed package: %v", err)
	}

	ackBody, err := json.Marshal(acknowledgeCorpusTransferRequest{SinkID: "local-corpus-sink", ConfirmedSHA256: packedEnvelope.SHA256})
	if err != nil {
		t.Fatal(err)
	}
	w = doCorpusHandler(t, h.AcknowledgeCorpusTransfer, http.MethodPost, "/acks", testCorpusWorkspace, receipt.TransferID, ackBody, -2)
	if w.Code != http.StatusOK {
		t.Fatalf("ack status = %d, body=%s", w.Code, w.Body.String())
	}
	acked := ledger.get(t, testCorpusWorkspace, receipt.TransferID)
	ledger.mu.Lock()
	ackCount := len(ledger.acks)
	ledger.mu.Unlock()
	if acked.State != "acked" || ackCount != 1 {
		t.Fatalf("final state/ACK rows = %s/%d, want acked/1", acked.State, ackCount)
	}
}

func TestSQLCorpusTransferStoreACKTransaction(t *testing.T) {
	if testPool == nil {
		t.Skip("handler test database is unavailable")
	}
	var exists bool
	if err := testPool.QueryRow(context.Background(), `SELECT to_regclass('corpus_transfer') IS NOT NULL`).Scan(&exists); err != nil || !exists {
		t.Skip("corpus transfer migration is not installed")
	}
	workspaceID, transferID, actorID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	digest := strings.Repeat("a", 64)
	_, err := testPool.Exec(context.Background(), `
		INSERT INTO corpus_transfer (
			id, workspace_id, actor_id, idempotency_key, object_key, manifest,
			manifest_sha256, expected_size_bytes, expected_sha256, state,
			verified_size_bytes, verified_sha256, expires_at, confirmed_at
		) VALUES ($1, $2, $3, $4, $5, '{}'::jsonb, $6, 1, $7, 'confirmed', 1, $7, now() + interval '1 hour', now())
	`, transferID, workspaceID, actorID, uuid.NewString(), "workspaces/test/archive.zip", strings.Repeat("b", 64), digest)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM corpus_transfer_ack WHERE workspace_id = $1 AND transfer_id = $2`, workspaceID, transferID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM corpus_transfer WHERE workspace_id = $1 AND id = $2`, workspaceID, transferID)
	})
	store := &sqlCorpusTransferStore{Queries: db.New(testPool), txStarter: testPool}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.AcknowledgeCorpusTransfer(context.Background(), parseUUID(workspaceID), parseUUID(transferID), "sink", digest, parseUUID(actorID))
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent ACK: %v", err)
		}
	}
	if _, err := store.AcknowledgeCorpusTransfer(context.Background(), parseUUID(workspaceID), parseUUID(transferID), "sink", strings.Repeat("c", 64), parseUUID(actorID)); !errors.Is(err, errCorpusTransferConflict) {
		t.Fatalf("conflicting ACK error = %v", err)
	}
	var state string
	var ackCount int
	if err := testPool.QueryRow(context.Background(), `SELECT state FROM corpus_transfer WHERE workspace_id = $1 AND id = $2`, workspaceID, transferID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM corpus_transfer_ack WHERE workspace_id = $1 AND transfer_id = $2`, workspaceID, transferID).Scan(&ackCount); err != nil {
		t.Fatal(err)
	}
	if state != "acked" || ackCount != 1 {
		t.Fatalf("ACK transaction state/count = %s/%d", state, ackCount)
	}
}

func newCorpusTransferTestHandler() (*Handler, *fakeCorpusTransferStore, *fakeCorpusObjectStore) {
	ledger := &fakeCorpusTransferStore{transfers: make(map[string]db.CorpusTransfer), acks: make(map[string]db.CorpusTransferAck)}
	objects := &fakeCorpusObjectStore{objects: make(map[string][]byte), deleted: make(map[string]bool)}
	return &Handler{CorpusTransfers: ledger, CorpusStorage: objects}, ledger, objects
}

func validCorpusCreateBody(t *testing.T, key string, payload []byte) []byte {
	t.Helper()
	manifest := corpustransfer.Manifest{
		SchemaVersion: corpustransfer.ManifestSchemaVersion,
		PackageID:     "package-" + key,
		CreatedAt:     time.Unix(1, 0).UTC(),
		Source:        corpustransfer.SourceInfo{Adapter: "test", Type: "test", Name: "fixture"},
	}
	manifestJSON, err := manifest.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{
		"idempotency_key": key,
		"manifest":        manifest,
		"archive": corpustransfer.ArchiveEnvelope{
			Format:         "zip",
			Filename:       "package.zip",
			SizeBytes:      int64(len(payload)),
			SHA256:         sha256Hex(payload),
			ManifestSHA256: sha256Hex(manifestJSON),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func createCorpusTransferForTest(t *testing.T, h *Handler, ledger *fakeCorpusTransferStore, key string, payload []byte) string {
	t.Helper()
	w := doCorpusHandler(t, h.CreateCorpusTransfer, http.MethodPost, "/create", testCorpusWorkspace, "", validCorpusCreateBody(t, key, payload), -2)
	if w.Code != http.StatusCreated {
		t.Fatalf("create %s status = %d, body=%s", key, w.Code, w.Body.String())
	}
	return ledger.idForIdempotency(t, key)
}

func assertCorpusUpload(t *testing.T, h *Handler, transferID string, payload []byte) {
	t.Helper()
	w := doCorpusHandler(t, h.UploadCorpusTransferContent, http.MethodPut, "/content", testCorpusWorkspace, transferID, payload, int64(len(payload)))
	if w.Code != http.StatusOK {
		t.Fatalf("upload status = %d, body=%s", w.Code, w.Body.String())
	}
}

func doCorpusHandler(t *testing.T, fn http.HandlerFunc, method, target, workspaceID, transferID string, body []byte, contentLength int64) *httptest.ResponseRecorder {
	return doCorpusHandlerContext(t, context.Background(), fn, method, target, workspaceID, transferID, body, contentLength)
}

func doCorpusHandlerContext(t *testing.T, ctx context.Context, fn http.HandlerFunc, method, target, workspaceID, transferID string, body []byte, contentLength int64) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, bytes.NewReader(body)).WithContext(ctx)
	req.Header.Set("X-User-ID", testCorpusActor)
	if contentLength != -2 {
		req.ContentLength = contentLength
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", workspaceID)
	if transferID != "" {
		rctx.URLParams.Add("transferID", transferID)
	}
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	fn(w, req)
	return w
}

func sha256Hex(body []byte) string {
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:])
}

type fakeCorpusTransferStore struct {
	mu                           sync.Mutex
	transfers                    map[string]db.CorpusTransfer
	acks                         map[string]db.CorpusTransferAck
	markUploadedErrorAfterCommit bool
}

func transferMapKey(workspaceID, transferID pgtype.UUID) string {
	return uuidToString(workspaceID) + "/" + uuidToString(transferID)
}

func (f *fakeCorpusTransferStore) CreateOrGetCorpusTransfer(_ context.Context, arg db.CreateOrGetCorpusTransferParams) (db.CorpusTransfer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, transfer := range f.transfers {
		if transfer.WorkspaceID == arg.WorkspaceID && transfer.ActorID == arg.ActorID && transfer.IdempotencyKey == arg.IdempotencyKey {
			return transfer, nil
		}
	}
	now := pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
	transfer := db.CorpusTransfer{
		ID: arg.ID, WorkspaceID: arg.WorkspaceID, ActorID: arg.ActorID,
		IdempotencyKey: arg.IdempotencyKey, ObjectKey: arg.ObjectKey,
		Manifest: arg.Manifest, ManifestSha256: arg.ManifestSha256,
		ExpectedSizeBytes: arg.ExpectedSizeBytes, ExpectedSha256: arg.ExpectedSha256,
		State: "created", ExpiresAt: arg.ExpiresAt, CreatedAt: now, UpdatedAt: now,
	}
	f.transfers[transferMapKey(arg.WorkspaceID, arg.ID)] = transfer
	return transfer, nil
}

func (f *fakeCorpusTransferStore) GetCorpusTransfer(_ context.Context, arg db.GetCorpusTransferParams) (db.CorpusTransfer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	transfer, ok := f.transfers[transferMapKey(arg.WorkspaceID, arg.ID)]
	if !ok {
		return db.CorpusTransfer{}, pgx.ErrNoRows
	}
	return transfer, nil
}

func (f *fakeCorpusTransferStore) ClaimCorpusTransferUpload(_ context.Context, arg db.ClaimCorpusTransferUploadParams) (db.CorpusTransfer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := transferMapKey(arg.WorkspaceID, arg.ID)
	transfer, ok := f.transfers[key]
	if !ok || transfer.State != "created" {
		return db.CorpusTransfer{}, pgx.ErrNoRows
	}
	transfer.State = "uploading"
	f.transfers[key] = transfer
	return transfer, nil
}

func (f *fakeCorpusTransferStore) MarkCorpusTransferUploaded(_ context.Context, arg db.MarkCorpusTransferUploadedParams) (db.CorpusTransfer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := transferMapKey(arg.WorkspaceID, arg.ID)
	transfer, ok := f.transfers[key]
	if !ok || transfer.State != "uploading" {
		return db.CorpusTransfer{}, pgx.ErrNoRows
	}
	transfer.State = "uploaded"
	f.transfers[key] = transfer
	if f.markUploadedErrorAfterCommit {
		f.markUploadedErrorAfterCommit = false
		return db.CorpusTransfer{}, fmt.Errorf("ambiguous commit result")
	}
	return transfer, nil
}

func (f *fakeCorpusTransferStore) FailCorpusTransferUpload(_ context.Context, arg db.FailCorpusTransferUploadParams) (db.CorpusTransfer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := transferMapKey(arg.WorkspaceID, arg.ID)
	transfer, ok := f.transfers[key]
	if !ok || transfer.State != "uploading" {
		return db.CorpusTransfer{}, pgx.ErrNoRows
	}
	transfer.State = "failed"
	transfer.FailureCode = arg.FailureCode
	transfer.CleanupPending = true
	f.transfers[key] = transfer
	return transfer, nil
}

func (f *fakeCorpusTransferStore) ClaimCorpusTransferVerification(_ context.Context, arg db.ClaimCorpusTransferVerificationParams) (db.CorpusTransfer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := transferMapKey(arg.WorkspaceID, arg.ID)
	transfer, ok := f.transfers[key]
	if !ok || transfer.State != "uploaded" || !time.Now().Before(transfer.ExpiresAt.Time) {
		return db.CorpusTransfer{}, pgx.ErrNoRows
	}
	transfer.State = "verifying"
	transfer.VerificationToken = arg.VerificationToken
	transfer.VerificationLeaseExpiresAt = arg.VerificationLeaseExpiresAt
	f.transfers[key] = transfer
	return transfer, nil
}

func (f *fakeCorpusTransferStore) ConfirmCorpusTransfer(_ context.Context, arg db.ConfirmCorpusTransferParams) (db.CorpusTransfer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := transferMapKey(arg.WorkspaceID, arg.ID)
	transfer, ok := f.transfers[key]
	if !ok || transfer.State != "verifying" || transfer.VerificationToken != arg.VerificationToken || !time.Now().Before(transfer.ExpiresAt.Time) || transfer.ExpectedSizeBytes != arg.VerifiedSizeBytes.Int64 || transfer.ExpectedSha256 != arg.VerifiedSha256.String {
		return db.CorpusTransfer{}, pgx.ErrNoRows
	}
	transfer.State = "confirmed"
	transfer.VerificationToken = pgtype.UUID{}
	transfer.VerificationLeaseExpiresAt = pgtype.Timestamptz{}
	transfer.VerifiedSizeBytes = arg.VerifiedSizeBytes
	transfer.VerifiedSha256 = arg.VerifiedSha256
	f.transfers[key] = transfer
	return transfer, nil
}

func (f *fakeCorpusTransferStore) FailCorpusTransferVerification(_ context.Context, arg db.FailCorpusTransferVerificationParams) (db.CorpusTransfer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := transferMapKey(arg.WorkspaceID, arg.ID)
	transfer, ok := f.transfers[key]
	if !ok || transfer.State != "verifying" || transfer.VerificationToken != arg.VerificationToken {
		return db.CorpusTransfer{}, pgx.ErrNoRows
	}
	transfer.State = "failed"
	transfer.VerificationToken = pgtype.UUID{}
	transfer.VerificationLeaseExpiresAt = pgtype.Timestamptz{}
	transfer.FailureCode = arg.FailureCode
	transfer.CleanupPending = true
	f.transfers[key] = transfer
	return transfer, nil
}

func (f *fakeCorpusTransferStore) GetConfirmedCorpusTransferContent(_ context.Context, arg db.GetConfirmedCorpusTransferContentParams) (db.CorpusTransfer, error) {
	transfer, err := f.GetCorpusTransfer(context.Background(), db.GetCorpusTransferParams(arg))
	if err != nil || (transfer.State != "confirmed" && transfer.State != "acked") {
		return db.CorpusTransfer{}, pgx.ErrNoRows
	}
	return transfer, nil
}

func (f *fakeCorpusTransferStore) AcknowledgeCorpusTransfer(_ context.Context, workspaceID, transferID pgtype.UUID, sinkID, digest string, actorID pgtype.UUID) (db.CorpusTransferAck, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	transferKey := transferMapKey(workspaceID, transferID)
	transfer, ok := f.transfers[transferKey]
	if !ok || (transfer.State != "confirmed" && transfer.State != "acked") || !transfer.VerifiedSha256.Valid || transfer.VerifiedSha256.String != digest {
		return db.CorpusTransferAck{}, errCorpusTransferConflict
	}
	ackKey := transferKey + "/" + sinkID
	if ack, ok := f.acks[ackKey]; ok {
		if ack.ConfirmedSha256 != digest {
			return db.CorpusTransferAck{}, errCorpusTransferConflict
		}
		return ack, nil
	}
	ack := db.CorpusTransferAck{WorkspaceID: workspaceID, TransferID: transferID, SinkID: sinkID, ConfirmedSha256: digest, AcknowledgedBy: actorID, AcknowledgedAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}}
	f.acks[ackKey] = ack
	transfer.State = "acked"
	f.transfers[transferKey] = transfer
	return ack, nil
}

func (f *fakeCorpusTransferStore) onlyTransfer(t *testing.T) db.CorpusTransfer {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.transfers) != 1 {
		t.Fatalf("transfer count = %d, want 1", len(f.transfers))
	}
	for _, transfer := range f.transfers {
		return transfer
	}
	panic("unreachable")
}

func (f *fakeCorpusTransferStore) transferCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.transfers)
}

func (f *fakeCorpusTransferStore) idForIdempotency(t *testing.T, key string) string {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, transfer := range f.transfers {
		if transfer.IdempotencyKey == key {
			return uuidToString(transfer.ID)
		}
	}
	t.Fatalf("missing transfer for %s", key)
	return ""
}

func (f *fakeCorpusTransferStore) get(t *testing.T, workspaceID, transferID string) db.CorpusTransfer {
	t.Helper()
	transfer, err := f.GetCorpusTransfer(context.Background(), db.GetCorpusTransferParams{WorkspaceID: parseUUID(workspaceID), ID: parseUUID(transferID)})
	if err != nil {
		t.Fatal(err)
	}
	return transfer
}

func (f *fakeCorpusTransferStore) setExpiry(t *testing.T, workspaceID, transferID string, expiry time.Time) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	key := transferMapKey(parseUUID(workspaceID), parseUUID(transferID))
	transfer, ok := f.transfers[key]
	if !ok {
		t.Fatalf("missing transfer %s", transferID)
	}
	transfer.ExpiresAt = pgtype.Timestamptz{Time: expiry, Valid: true}
	f.transfers[key] = transfer
}

type fakeCorpusObjectStore struct {
	mu                   sync.Mutex
	objects              map[string][]byte
	deleted              map[string]bool
	uploadErr            error
	rejectCanceledDelete bool
}

func (f *fakeCorpusObjectStore) UploadStream(_ context.Context, key string, body io.Reader, _ int64, _, _ string) (string, error) {
	f.mu.Lock()
	uploadErr := f.uploadErr
	f.uploadErr = nil
	f.mu.Unlock()
	if uploadErr != nil {
		return "", uploadErr
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return "", err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objects[key] = data
	return "object://" + key, nil
}

func (f *fakeCorpusObjectStore) GetReader(_ context.Context, key string) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	body, ok := f.objects[key]
	if !ok {
		return nil, fmt.Errorf("object not found")
	}
	return io.NopCloser(bytes.NewReader(append([]byte(nil), body...))), nil
}

func (f *fakeCorpusObjectStore) DeleteObject(ctx context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.rejectCanceledDelete && ctx.Err() != nil {
		return ctx.Err()
	}
	delete(f.objects, key)
	f.deleted[key] = true
	return nil
}

func (f *fakeCorpusObjectStore) bodyFor(key string) []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]byte(nil), f.objects[key]...)
}

func (f *fakeCorpusObjectStore) setBody(key string, body []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objects[key] = append([]byte(nil), body...)
}

func (f *fakeCorpusObjectStore) wasDeleted(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.deleted[key]
}
