package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/jpeg"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func useThumbnailTestStorage(t *testing.T) *mockStorage {
	t.Helper()
	store := &mockStorage{}
	origStorage := testHandler.Storage
	testHandler.Storage = store
	t.Cleanup(func() { testHandler.Storage = origStorage })
	return store
}

func thumbnailRequest(t *testing.T, attachmentID string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	r.Get("/api/attachments/{id}/thumbnail", testHandler.GetAttachmentThumbnail)
	req := httptest.NewRequest("GET", "/api/attachments/"+attachmentID+"/thumbnail", nil)
	req.Header.Set("X-User-ID", testUserID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func testJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h)), nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestAttachmentThumbnail_GeneratesStoresAndReuses(t *testing.T) {
	if testPool == nil {
		t.Skip("test database not available")
	}
	store := useThumbnailTestStorage(t)
	key := "thumbs/generated.jpg"
	store.put(key, testJPEG(t, 2000, 1500))
	id := seedAttachmentURL(t, "https://s3.example.com/test-bucket/"+key, "render.jpg", "image/jpeg", 600<<10)

	w := thumbnailRequest(t, id)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); got != "image/jpeg" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := w.Header().Get("Cache-Control"); got != thumbnailCacheControl {
		t.Fatalf("Cache-Control = %q", got)
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(w.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != 640 || cfg.Height != 480 {
		t.Fatalf("thumbnail = %dx%d, want 640x480", cfg.Width, cfg.Height)
	}
	stored := store.files[key+".thumb"]
	if !bytes.Equal(stored, w.Body.Bytes()) {
		t.Fatal("generated thumbnail was not stored next to the original")
	}

	// A stored thumbnail is served without reading the original again.
	store.Delete(context.Background(), key)
	w = thumbnailRequest(t, id)
	if w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), stored) {
		t.Fatalf("second request: status = %d", w.Code)
	}
}

func TestUploadFile_PrewarmsThumbnail(t *testing.T) {
	if testPool == nil {
		t.Skip("test database not available")
	}
	store := useThumbnailTestStorage(t)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "render.jpg")
	if err != nil {
		t.Fatal(err)
	}
	part.Write(testJPEG(t, 2400, 1800))
	writer.Close()
	req := httptest.NewRequest("POST", "/api/upload-file", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	w := httptest.NewRecorder()
	testHandler.UploadFile(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("upload: status = %d, body=%s", w.Code, w.Body.String())
	}
	var resp AttachmentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM attachment WHERE id = $1`, resp.ID) })
	if resp.ThumbnailURL != "/api/attachments/"+resp.ID+"/thumbnail" {
		t.Fatalf("thumbnail_url = %q", resp.ThumbnailURL)
	}

	thumbKey := "workspaces/" + testWorkspaceID + "/" + resp.ID + ".jpg.thumb"
	deadline := time.Now().Add(10 * time.Second)
	for {
		store.mu.Lock()
		_, ok := store.files[thumbKey]
		store.mu.Unlock()
		if ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("upload did not store a thumbnail")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestAttachmentThumbnail_OnlyLargeRasterImagesGetOne(t *testing.T) {
	if testPool == nil {
		t.Skip("test database not available")
	}
	store := useThumbnailTestStorage(t)
	for _, tc := range []struct {
		name, key, contentType string
		size                   int64
		want                   bool
	}{
		{"large jpeg", "thumbs/large.jpg", "image/jpeg", 600 << 10, true},
		{"large webp", "thumbs/large.webp", "image/webp", 2 << 20, true},
		{"small jpeg", "thumbs/small.jpg", "image/jpeg", 100 << 10, false},
		{"gif", "thumbs/anim.gif", "image/gif", 4 << 20, false},
		{"svg", "thumbs/vector.svg", "image/svg+xml", 4 << 20, false},
		{"pdf", "thumbs/doc.pdf", "application/pdf", 4 << 20, false},
		{"jpeg without image extension", "thumbs/blob.bin", "image/jpeg", 4 << 20, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := seedAttachmentURL(t, "https://s3.example.com/test-bucket/"+tc.key, "f", tc.contentType, tc.size)
			resp := testHandler.attachmentToResponse(db.Attachment{
				ID: parseUUID(id), Url: "https://s3.example.com/test-bucket/" + tc.key,
				ContentType: tc.contentType, SizeBytes: tc.size,
			}, attachmentURLModeSigned)
			if got := resp.ThumbnailURL != ""; got != tc.want {
				t.Fatalf("thumbnail_url = %q, want present=%v", resp.ThumbnailURL, tc.want)
			}
			if tc.want {
				if resp.ThumbnailURL != "/api/attachments/"+id+"/thumbnail" {
					t.Fatalf("thumbnail_url = %q", resp.ThumbnailURL)
				}
				return
			}
			if w := thumbnailRequest(t, id); w.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", w.Code)
			}
		})
	}
	if len(store.files) != 0 {
		t.Fatalf("ineligible attachments wrote objects: %v", store.files)
	}
}

func TestAttachmentThumbnail_UndecodableImageIsNotAvailable(t *testing.T) {
	if testPool == nil {
		t.Skip("test database not available")
	}
	store := useThumbnailTestStorage(t)
	key := "thumbs/corrupt.png"
	store.put(key, []byte("not really a png"))
	id := seedAttachmentURL(t, "https://s3.example.com/test-bucket/"+key, "corrupt.png", "image/png", 1<<20)

	if w := thumbnailRequest(t, id); w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", w.Code, w.Body.String())
	}
	if _, ok := store.files[key+".thumb"]; ok {
		t.Fatal("stored a thumbnail for an undecodable image")
	}
}

func TestAttachmentThumbnail_NonMemberGets404(t *testing.T) {
	if testPool == nil {
		t.Skip("test database not available")
	}
	store := useThumbnailTestStorage(t)
	ctx := context.Background()
	var foreignWorkspaceID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('Thumb Foreign', 'thumb-foreign', '', 'THF')
		RETURNING id::text
	`).Scan(&foreignWorkspaceID); err != nil {
		t.Fatalf("seed foreign workspace: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, foreignWorkspaceID) })

	key := "thumbs/foreign.jpg"
	store.put(key, testJPEG(t, 2000, 1500))
	var id string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO attachment (workspace_id, uploader_type, uploader_id, filename, url, content_type, size_bytes)
		VALUES ($1, 'member', $2, 'foreign.jpg', $3, 'image/jpeg', $4)
		RETURNING id::text
	`, foreignWorkspaceID, testUserID, "https://s3.example.com/test-bucket/"+key, 600<<10).Scan(&id); err != nil {
		t.Fatalf("seed foreign attachment: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM attachment WHERE id = $1`, id) })

	if w := thumbnailRequest(t, id); w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for non-member", w.Code)
	}
	if _, ok := store.files[key+".thumb"]; ok {
		t.Fatal("non-member request generated a thumbnail")
	}
	if w := originalRequest(t, id); w.Code != http.StatusNotFound {
		t.Fatalf("original: status = %d, want 404 for non-member", w.Code)
	}
}

func originalRequest(t *testing.T, attachmentID string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	r.Get("/api/attachments/{id}/original", testHandler.GetAttachmentOriginal)
	req := httptest.NewRequest("GET", "/api/attachments/"+attachmentID+"/original", nil)
	req.Header.Set("X-User-ID", testUserID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestAttachmentOriginal_StreamsThroughTheAPI(t *testing.T) {
	if testPool == nil {
		t.Skip("test database not available")
	}
	store := useThumbnailTestStorage(t)
	origMode := testHandler.cfg.AttachmentDownloadMode
	// /download redirects to storage in this mode; /original never does.
	testHandler.cfg.AttachmentDownloadMode = "presign"
	t.Cleanup(func() { testHandler.cfg.AttachmentDownloadMode = origMode })

	key := "thumbs/original.jpg"
	original := testJPEG(t, 2000, 1500)
	store.put(key, original)
	id := seedAttachmentURL(t, "https://s3.example.com/test-bucket/"+key, "render.jpg", "image/jpeg", 600<<10)

	w := originalRequest(t, id)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); got != "image/jpeg" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}
	if !bytes.Equal(w.Body.Bytes(), original) {
		t.Fatal("body is not the original")
	}

	// Only images that have a thumbnail are served: anything else keeps
	// /download as its one route.
	smallKey := "thumbs/original-small.jpg"
	store.put(smallKey, testJPEG(t, 100, 100))
	smallID := seedAttachmentURL(t, "https://s3.example.com/test-bucket/"+smallKey, "small.jpg", "image/jpeg", 10<<10)
	if w := originalRequest(t, smallID); w.Code != http.StatusNotFound {
		t.Fatalf("small image: status = %d, want 404", w.Code)
	}
}

func TestDeleteS3ObjectsRemovesThumbnails(t *testing.T) {
	store := useThumbnailTestStorage(t)
	for _, key := range []string{"a.png", "a.png.thumb", "b.pdf", "c.JPG", "c.JPG.thumb"} {
		store.put(key, []byte("x"))
	}
	testHandler.deleteS3Objects(context.Background(), []string{
		"https://cdn.example.com/a.png",
		"https://cdn.example.com/b.pdf",
	})
	testHandler.deleteS3Object(context.Background(), "https://cdn.example.com/c.JPG")
	if len(store.files) != 0 {
		t.Fatalf("left behind: %v", store.files)
	}
}
