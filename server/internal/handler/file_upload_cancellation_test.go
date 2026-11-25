package handler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/testutil"
)

type cancellationUploadStorage struct {
	mockStorage
	fail func() error
}

func (s *cancellationUploadStorage) Upload(context.Context, string, []byte, string, string) (string, error) {
	return "", s.fail()
}

func uploadCancellationRequest(t *testing.T, ctx context.Context, filename string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("upload body")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/upload-file", &body)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	r.Header.Set("X-User-ID", testUserID)
	r.Header.Set("X-Workspace-ID", testWorkspaceID)
	return r
}

func TestUploadFileStorageCancellation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		direct bool
		cancel bool
		err    error
		status int
	}{
		{"workspace cancellation", false, true, context.Canceled, 499},
		{"direct cancellation", true, true, context.Canceled, 499},
		{"operation cancellation with live request", false, false, context.Canceled, http.StatusInternalServerError},
		{"storage timeout", false, false, context.DeadlineExceeded, http.StatusInternalServerError},
		{"unrelated failure after cancellation", false, true, errors.New("storage unavailable"), http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			h := *testHandler
			h.Storage = &cancellationUploadStorage{fail: func() error {
				if tc.cancel {
					cancel()
				}
				return fmt.Errorf("upload: %w", tc.err)
			}}
			filename := uuid.NewString() + ".txt"
			r := uploadCancellationRequest(t, ctx, filename)
			if tc.direct {
				r.Header.Del("X-Workspace-ID")
			}
			response := testutil.Call(t, h.UploadFile, r).Want(tc.status)
			if tc.status == 499 && response.Body.Len() != 0 {
				t.Fatal("cancelled upload returned a response body")
			}
			if dbfx.Count(t, "SELECT count(*) FROM attachment WHERE workspace_id=$1 AND filename=$2", testWorkspaceID, filename) != 0 {
				t.Fatal("failed upload created an attachment")
			}
		})
	}
}

type uploadRecordErrorStarter struct {
	txStarter
	fail func() error
}

type uploadRecordErrorTx struct {
	pgx.Tx
	fail func() error
}

type uploadRecordErrorRow struct{ err error }

func (s uploadRecordErrorStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.txStarter.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return uploadRecordErrorTx{tx, s.fail}, nil
}

func (tx uploadRecordErrorTx) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	if strings.Contains(query, "-- name: CreateAttachment ") {
		return uploadRecordErrorRow{tx.fail()}
	}
	return tx.Tx.QueryRow(ctx, query, args...)
}

func (r uploadRecordErrorRow) Scan(...any) error { return r.err }

func TestUploadFileRecordCancellation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		cancel bool
		err    error
		status int
	}{
		{"request cancelled during record write", true, context.Canceled, 499},
		{"operation cancellation with live request", false, context.Canceled, http.StatusOK},
		{"unrelated failure after cancellation", true, errors.New("record unavailable"), http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			h := *testHandler
			store := &mockStorage{}
			h.Storage = store
			h.TxStarter = uploadRecordErrorStarter{h.TxStarter, func() error {
				if tc.cancel {
					cancel()
				}
				return tc.err
			}}
			filename := uuid.NewString() + ".txt"
			response := testutil.Call(t, h.UploadFile, uploadCancellationRequest(t, ctx, filename)).Want(tc.status)
			if tc.status == 499 && response.Body.Len() != 0 {
				t.Fatal("cancelled record write returned a response body")
			}
			if len(store.files) != 1 {
				t.Fatal("uncertain record write deleted uploaded bytes")
			}
			if dbfx.Count(t, "SELECT count(*) FROM attachment WHERE workspace_id=$1 AND filename=$2", testWorkspaceID, filename) != 0 {
				t.Fatal("injected record failure committed an attachment")
			}
		})
	}
}
