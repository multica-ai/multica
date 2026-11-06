package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func encodeTestPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 0xff
	}
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func uploadTestFile(t *testing.T, filename string, data []byte) map[string]any {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	part.Write(data)
	writer.Close()

	req := httptest.NewRequest("POST", "/api/upload-file", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	w := httptest.NewRecorder()
	testHandler.UploadFile(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UploadFile: status %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	id, _ := resp["id"].(string)
	if id == "" {
		t.Fatalf("no attachment row created: %s", w.Body.String())
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM attachment WHERE id = $1`, id)
	})
	return resp
}

func TestUploadFile_ImageDimensionsPreserveOriginal(t *testing.T) {
	// Force multipart's disk-backed path at the supported file-size boundary.
	largeImage := make([]byte, 100<<20)
	copy(largeImage, encodeTestPNG(t, 32, 20))
	store := &mockStorage{}
	original := testHandler.Storage
	testHandler.Storage = store
	t.Cleanup(func() { testHandler.Storage = original })
	for _, tc := range []struct {
		name          string
		data          []byte
		width, height int
	}{
		{"shot.png", encodeTestPNG(t, 3200, 2000), 3200, 2000},
		{"at-limit.png", largeImage, 32, 20},
		{"notes.txt", []byte("hello"), 0, 0},
		{"broken.png", []byte("\x89PNG\r\n\x1a\ntruncated"), 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := uploadTestFile(t, tc.name, tc.data)
			if tc.width > 0 {
				if resp["width"] != float64(tc.width) || resp["height"] != float64(tc.height) {
					t.Fatalf("dimensions: %v", resp)
				}
			} else if resp["width"] != nil || resp["height"] != nil {
				t.Fatalf("unexpected dimensions: %v", resp)
			}
			stored := store.files[store.KeyFromURL(resp["url"].(string))]
			if !bytes.Equal(stored, tc.data) {
				t.Fatal("upload did not preserve the original bytes")
			}
			att, err := testHandler.Queries.GetAttachment(context.Background(), db.GetAttachmentParams{
				ID: parseUUID(resp["id"].(string)), WorkspaceID: parseUUID(testWorkspaceID),
			})
			if err != nil {
				t.Fatal(err)
			}
			if att.Width.Valid != (tc.width > 0) || int(att.Width.Int32) != tc.width || int(att.Height.Int32) != tc.height {
				t.Fatalf("stored dimensions: %v/%v", att.Width, att.Height)
			}
		})
	}
	if len(store.files) != 4 {
		t.Fatalf("stored %d objects for 4 uploads", len(store.files))
	}
}
