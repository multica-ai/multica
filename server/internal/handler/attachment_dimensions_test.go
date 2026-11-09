package handler

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestUploadFileImageDimensions(t *testing.T) {
	store := &mockStorage{}
	originalStorage, originalSigner := testHandler.Storage, testHandler.CFSigner
	testHandler.Storage, testHandler.CFSigner = store, nil
	t.Cleanup(func() { testHandler.Storage, testHandler.CFSigner = originalStorage, originalSigner })

	var imageBytes bytes.Buffer
	if err := png.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 80, 40))); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		filename string
		data     []byte
		image    bool
	}{
		{"shot.png", imageBytes.Bytes(), true},
		{"notes.txt", []byte("notes"), false},
	} {
		t.Run(tc.filename, func(t *testing.T) {
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			part, err := writer.CreateFormFile("file", tc.filename)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := part.Write(tc.data); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "/api/upload-file", &body)
			req.Header.Set("Content-Type", writer.FormDataContentType())
			req.Header.Set("X-User-ID", testUserID)
			req.Header.Set("X-Workspace-ID", testWorkspaceID)
			var response AttachmentResponse
			testutil.Call(t, testHandler.UploadFile, req).Want(http.StatusOK).JSON(&response)
			t.Cleanup(func() {
				testPool.Exec(context.Background(), "DELETE FROM attachment WHERE id = $1", response.ID)
			})
			stored, err := testHandler.Queries.GetAttachment(context.Background(), db.GetAttachmentParams{
				ID: parseUUID(response.ID), WorkspaceID: parseUUID(testWorkspaceID),
			})
			if err != nil {
				t.Fatal(err)
			}
			if tc.image {
				if response.Width == nil || *response.Width != 80 || response.Height == nil || *response.Height != 40 {
					t.Fatalf("response dimensions = %v/%v", response.Width, response.Height)
				}
				if !stored.Width.Valid || stored.Width.Int32 != 80 || !stored.Height.Valid || stored.Height.Int32 != 40 {
					t.Fatalf("stored dimensions = %v/%v", stored.Width, stored.Height)
				}
			} else if response.Width != nil || response.Height != nil || stored.Width.Valid || stored.Height.Valid {
				t.Fatal("non-image acquired dimensions")
			}
		})
	}
	if len(store.files) != 2 {
		t.Fatalf("stored %d objects, want two originals", len(store.files))
	}
}
