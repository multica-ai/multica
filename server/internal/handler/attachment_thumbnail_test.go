package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/testutil"
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

// pausingThumbStorage holds the first thumbnail write until released, so a
// test can finish a delete after the render has read the original.
type pausingThumbStorage struct {
	*mockStorage
	reached chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *pausingThumbStorage) Upload(ctx context.Context, key string, data []byte, contentType, filename string) (string, error) {
	if strings.HasSuffix(key, ".thumb") {
		s.once.Do(func() {
			close(s.reached)
			<-s.release
		})
	}
	return s.mockStorage.Upload(ctx, key, data, contentType, filename)
}

// A delete that completes while a render is running must not leave the
// render's thumbnail behind: nothing would clean it up after the row is gone.
func TestAttachmentThumbnail_DeleteDuringRenderLeavesNoThumbnail(t *testing.T) {
	if testPool == nil {
		t.Skip("test database not available")
	}
	base := &mockStorage{}
	store := &pausingThumbStorage{mockStorage: base, reached: make(chan struct{}), release: make(chan struct{})}
	origStorage := testHandler.Storage
	testHandler.Storage = store
	t.Cleanup(func() { testHandler.Storage = origStorage })

	key := "thumbs/delete-race.jpg"
	base.put(key, testJPEG(t, 2000, 1500))
	id := seedAttachmentURL(t, "https://s3.example.com/test-bucket/"+key, "race.jpg", "image/jpeg", 600<<10)

	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(store.release) }) }
	rendered := make(chan *httptest.ResponseRecorder, 1)
	renderDone := make(chan struct{})
	go func() {
		defer close(renderDone)
		rendered <- thumbnailRequest(t, id)
	}()
	// A failed assertion must not leave the render blocked on its write,
	// holding one of the shared render slots.
	t.Cleanup(func() {
		release()
		<-renderDone
	})
	select {
	case <-store.reached:
	case <-time.After(10 * time.Second):
		t.Fatal("render did not reach the thumbnail write")
	}

	req := newRequest(http.MethodDelete, "/api/attachments/"+id, nil)
	testutil.Call(t, testHandler.DeleteAttachment, withURLParam(req, "id", id)).Want(http.StatusNoContent)
	release()

	if w := <-rendered; w.Code != http.StatusNotFound {
		t.Fatalf("thumbnail status = %d, want 404 once the attachment is gone", w.Code)
	}
	base.mu.Lock()
	defer base.mu.Unlock()
	if len(base.files) != 0 {
		t.Fatalf("left behind: %v", keysOf(base.files))
	}
}

// deadlineStorage stands in for S3 under a render that runs out of time: a
// write can land and then end the render's context before it returns, and
// any call made with an ended context is refused.
type deadlineStorage struct {
	*mockStorage
	expireOnUpload context.CancelFunc
}

func (s *deadlineStorage) Upload(ctx context.Context, key string, data []byte, contentType, filename string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	url, err := s.mockStorage.Upload(ctx, key, data, contentType, filename)
	if s.expireOnUpload != nil {
		s.expireOnUpload()
		return "", ctx.Err()
	}
	return url, err
}

func (s *deadlineStorage) DeleteObject(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.mockStorage.DeleteObject(ctx, key)
}

// A render's timeout must not keep it from removing the thumbnail of an
// attachment deleted meanwhile, whether the timeout hits during the write or
// during the check that follows it.
func TestGenerateAttachmentThumbnail_CleansUpAfterRenderTimeout(t *testing.T) {
	for _, tc := range []struct {
		name       string
		lateUpload bool
		slowCheck  bool
	}{
		{name: "write times out after landing", lateUpload: true},
		{name: "check outlives the render", slowCheck: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, expire := context.WithCancel(context.Background())
			defer expire()
			base := &mockStorage{}
			store := &deadlineStorage{mockStorage: base}
			if tc.lateUpload {
				store.expireOnUpload = expire
			}
			objectKey := "thumbs/timeout.jpg"
			base.put(objectKey, testJPEG(t, 2000, 1500))

			// The attachment is gone; the check succeeds only if its context
			// outlives the render's.
			exists := func(checkCtx context.Context) (bool, error) {
				if tc.slowCheck {
					expire()
				}
				return false, checkCtx.Err()
			}
			_, err := generateAttachmentThumbnail(ctx, store, objectKey, objectKey+".thumb", exists)
			if !errors.Is(err, errThumbnailUnavailable) {
				t.Fatalf("err = %v, want errThumbnailUnavailable", err)
			}
			base.mu.Lock()
			defer base.mu.Unlock()
			if _, ok := base.files[objectKey+".thumb"]; ok {
				t.Fatal("thumbnail left behind after the render timed out")
			}
		})
	}
}

func keysOf(m map[string][]byte) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
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

// A task token is bound to one workspace. Its user may also belong to the
// attachment's workspace, but the task must not read files from there.
func TestAttachmentReads_TaskTokenStaysInItsBoundWorkspace(t *testing.T) {
	if testPool == nil {
		t.Skip("test database not available")
	}
	store := useThumbnailTestStorage(t)
	ctx := context.Background()
	var otherWorkspaceID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('Thumb Task Other', 'thumb-task-other', '', 'TTO')
		RETURNING id::text
	`).Scan(&otherWorkspaceID); err != nil {
		t.Fatalf("seed other workspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM member WHERE workspace_id = $1`, otherWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, otherWorkspaceID)
	})
	if _, err := testPool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`, otherWorkspaceID, testUserID); err != nil {
		t.Fatalf("seed membership: %v", err)
	}

	key := "thumbs/task-other.jpg"
	store.put(key, testJPEG(t, 2000, 1500))
	var id string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO attachment (workspace_id, uploader_type, uploader_id, filename, url, content_type, size_bytes)
		VALUES ($1, 'member', $2, 'task-other.jpg', $3, 'image/jpeg', $4)
		RETURNING id::text
	`, otherWorkspaceID, testUserID, "https://s3.example.com/test-bucket/"+key, 600<<10).Scan(&id); err != nil {
		t.Fatalf("seed attachment: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM attachment WHERE id = $1`, id) })

	r := chi.NewRouter()
	r.Get("/api/attachments/{id}/download", testHandler.DownloadAttachment)
	r.Get("/api/attachments/{id}/thumbnail", testHandler.GetAttachmentThumbnail)
	r.Get("/api/attachments/{id}/original", testHandler.GetAttachmentOriginal)
	r.Get("/api/attachments/{id}/download-metadata", testHandler.GetAttachmentForDownload)
	read := func(route, boundWorkspaceID string) int {
		req := httptest.NewRequest("GET", "/api/attachments/"+id+"/"+route, nil)
		req.Header.Set("X-User-ID", testUserID)
		if boundWorkspaceID != "" {
			// What Auth stamps for a mat_ token.
			req.Header.Set("X-Actor-Source", "task_token")
			req.Header.Set("X-Workspace-ID", boundWorkspaceID)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}

	for _, route := range []string{"thumbnail", "original", "download", "download-metadata"} {
		if got := read(route, testWorkspaceID); got != http.StatusNotFound {
			t.Fatalf("%s from a task bound to another workspace: status = %d, want 404", route, got)
		}
	}
	if _, ok := store.files[key+".thumb"]; ok {
		t.Fatal("a task bound to another workspace generated a thumbnail")
	}

	for _, route := range []string{"thumbnail", "original", "download-metadata"} {
		if got := read(route, otherWorkspaceID); got != http.StatusOK {
			t.Fatalf("%s from a task bound to the attachment's workspace: status = %d, want 200", route, got)
		}
		if got := read(route, ""); got != http.StatusOK {
			t.Fatalf("%s from the member's own session: status = %d, want 200", route, got)
		}
	}
}
