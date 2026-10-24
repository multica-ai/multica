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
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
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

func TestUploadFile_LargeImageGetsDimensionsAndPreview(t *testing.T) {
	store := &mockStorage{}
	origStorage, origSigner := testHandler.Storage, testHandler.CFSigner
	testHandler.Storage, testHandler.CFSigner = store, nil
	t.Cleanup(func() { testHandler.Storage, testHandler.CFSigner = origStorage, origSigner })

	resp := uploadTestFile(t, "screenshot.png", encodeTestPNG(t, 3200, 2000))

	if resp["width"] != float64(3200) || resp["height"] != float64(2000) {
		t.Fatalf("width/height = %v/%v, want 3200/2000", resp["width"], resp["height"])
	}
	originalURL, _ := resp["url"].(string)
	previewURL, _ := resp["preview_url"].(string)
	if want := originalURL + ".preview.png"; previewURL != want {
		t.Fatalf("preview_url = %q, want the public preview object %q", previewURL, want)
	}
	stored, ok := store.files[store.KeyFromURL(previewURL)]
	if !ok {
		t.Fatalf("preview object was not uploaded; have %d objects", len(store.files))
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(stored))
	if err != nil || cfg.Width != 1600 || cfg.Height != 1000 {
		t.Fatalf("stored preview is %dx%d (err %v), want 1600x1000", cfg.Width, cfg.Height, err)
	}

	att, err := testHandler.Queries.GetAttachment(context.Background(), db.GetAttachmentParams{
		ID: parseUUID(resp["id"].(string)), WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatal(err)
	}
	if att.Width.Int32 != 3200 || att.Height.Int32 != 2000 || att.PreviewUrl.String != previewURL {
		t.Fatalf("row width=%v height=%v preview=%v", att.Width, att.Height, att.PreviewUrl)
	}
}

func TestUploadFile_SmallImageAndOtherFilesHaveNoPreview(t *testing.T) {
	store := &mockStorage{}
	origStorage, origSigner := testHandler.Storage, testHandler.CFSigner
	testHandler.Storage, testHandler.CFSigner = store, nil
	t.Cleanup(func() { testHandler.Storage, testHandler.CFSigner = origStorage, origSigner })

	small := uploadTestFile(t, "icon.png", encodeTestPNG(t, 640, 480))
	if small["width"] != float64(640) || small["height"] != float64(480) {
		t.Fatalf("width/height = %v/%v, want 640/480", small["width"], small["height"])
	}
	if _, ok := small["preview_url"]; ok {
		t.Fatalf("small image got a preview: %v", small["preview_url"])
	}

	text := uploadTestFile(t, "notes.txt", []byte("hello"))
	for _, field := range []string{"width", "height", "preview_url"} {
		if _, ok := text[field]; ok {
			t.Fatalf("text file response carries %q: %v", field, text)
		}
	}
	if len(store.files) != 2 {
		t.Fatalf("stored %d objects, want only the two originals", len(store.files))
	}
}

func seedImageWithPreview(t *testing.T, originalURL, previewURL string) string {
	t.Helper()
	id := seedAttachmentURL(t, originalURL, "shot.png", "image/png", 999)
	if _, err := testPool.Exec(context.Background(),
		`UPDATE attachment SET width = 3200, height = 2000, preview_url = $2 WHERE id = $1`,
		id, previewURL,
	); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestDownloadAttachment_PreviewVariantProxiesPreviewObject(t *testing.T) {
	store := &mockStorage{}
	origStorage, origCfg, origSigner := testHandler.Storage, testHandler.cfg, testHandler.CFSigner
	testHandler.Storage = store
	testHandler.cfg.AttachmentDownloadMode = "proxy"
	testHandler.CFSigner = nil
	t.Cleanup(func() {
		testHandler.Storage, testHandler.cfg, testHandler.CFSigner = origStorage, origCfg, origSigner
	})

	const key = "workspaces/ws/a.png"
	store.put(key, []byte("original-bytes"))
	store.put(key+".preview.png", []byte("preview-bytes"))
	withPreview := seedImageWithPreview(t, "http://rustfs:9000/test-bucket/"+key, "http://rustfs:9000/test-bucket/"+key+".preview.png")
	store.put("workspaces/ws/b.png", []byte("plain-bytes"))
	withoutPreview := seedAttachmentURL(t, "http://rustfs:9000/test-bucket/workspaces/ws/b.png", "b.png", "image/png", 11)

	cases := []struct {
		name, id, query, wantBody string
	}{
		{"preview variant", withPreview, "?variant=preview", "preview-bytes"},
		{"no variant serves the original", withPreview, "", "original-bytes"},
		{"variant without a preview serves the original", withoutPreview, "?variant=preview", "plain-bytes"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, w := newDownloadRequest(t, tc.id, testWorkspaceID)
			req.URL.RawQuery = strings.TrimPrefix(tc.query, "?")
			testHandler.DownloadAttachment(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
			}
			if got := w.Body.String(); got != tc.wantBody {
				t.Fatalf("body = %q, want %q", got, tc.wantBody)
			}
			if got := w.Header().Get("Content-Type"); got != "image/png" {
				t.Fatalf("Content-Type = %q", got)
			}
		})
	}
}

func TestGetAttachmentByID_PresignsPreview(t *testing.T) {
	store := &mockStorageNoCdn{}
	origStorage, origCfg, origSigner := testHandler.Storage, testHandler.cfg, testHandler.CFSigner
	testHandler.Storage = store
	testHandler.cfg.AttachmentDownloadMode = "auto"
	testHandler.CFSigner = nil
	t.Cleanup(func() {
		testHandler.Storage, testHandler.cfg, testHandler.CFSigner = origStorage, origCfg, origSigner
	})

	const key = "workspaces/ws/presigned.png"
	id := seedImageWithPreview(t, "https://s3.example.com/test-bucket/"+key, "https://s3.example.com/test-bucket/"+key+".preview.png")

	req := httptest.NewRequest("GET", "/api/attachments/"+id, nil)
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	testHandler.GetAttachmentByID(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
	}
	var resp AttachmentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(resp.PreviewURL)
	if err != nil || parsed.Path != "/"+key+".preview.png" || parsed.Query().Get("X-Amz-Signature") != "mock" {
		t.Fatalf("preview_url = %q, want the presigned preview object", resp.PreviewURL)
	}
	if parsed.Query().Get("response-content-disposition") != "" {
		t.Fatalf("preview_url must stay inline-loadable: %q", resp.PreviewURL)
	}
	if resp.Width == nil || *resp.Width != 3200 || resp.Height == nil || *resp.Height != 2000 {
		t.Fatalf("width/height = %v/%v", resp.Width, resp.Height)
	}
}

func TestAttachmentToResponse_PreviewURLFollowsDeploymentShape(t *testing.T) {
	withPreview := func(original, preview string) db.Attachment {
		return db.Attachment{
			ID:         parseUUID("01a16526-61a2-7d74-b911-b9a1a3863d31"),
			Url:        original,
			PreviewUrl: pgtype.Text{String: preview, Valid: true},
		}
	}
	stablePath := "/api/attachments/01a16526-61a2-7d74-b911-b9a1a3863d31/download?variant=preview"

	t.Run("public CDN serves the preview object directly", func(t *testing.T) {
		h := &Handler{Storage: &mockStorage{}}
		resp := h.attachmentToResponse(withPreview("https://cdn.example.com/a.png", "https://cdn.example.com/a.png.preview.png"), attachmentURLModeSigned)
		if resp.PreviewURL != "https://cdn.example.com/a.png.preview.png" {
			t.Fatalf("preview_url = %q", resp.PreviewURL)
		}
	})
	t.Run("private bucket goes through the download endpoint", func(t *testing.T) {
		h := &Handler{Storage: &mockStorageNoCdn{}}
		resp := h.attachmentToResponse(withPreview("https://s3.example.com/test-bucket/a.png", "https://s3.example.com/test-bucket/a.png.preview.png"), attachmentURLModeSigned)
		if resp.PreviewURL != stablePath {
			t.Fatalf("preview_url = %q, want %q", resp.PreviewURL, stablePath)
		}
	})
	t.Run("local uploads keep their site-relative path", func(t *testing.T) {
		h := &Handler{Storage: &mockStorageNoCdn{}}
		resp := h.attachmentToResponse(withPreview("/uploads/a.png", "/uploads/a.png.preview.png"), attachmentURLModeSigned)
		if resp.PreviewURL != "/uploads/a.png.preview.png" {
			t.Fatalf("preview_url = %q", resp.PreviewURL)
		}
	})
	t.Run("cloudfront signs it, unless the caller asked for stable paths", func(t *testing.T) {
		h := &Handler{Storage: &mockStorage{}, CFSigner: testCloudFrontSigner(t)}
		att := withPreview("https://static.example.test/a.png", "https://static.example.test/a.png.preview.png")
		signed := h.attachmentToResponse(att, attachmentURLModeSigned)
		if !strings.HasPrefix(signed.PreviewURL, "https://static.example.test/a.png.preview.png?") || !strings.Contains(signed.PreviewURL, "Signature=") {
			t.Fatalf("preview_url = %q, want a CloudFront-signed preview URL", signed.PreviewURL)
		}
		if stable := h.attachmentToResponse(att, attachmentURLModeStable); stable.PreviewURL != stablePath {
			t.Fatalf("stable preview_url = %q, want %q", stable.PreviewURL, stablePath)
		}
	})
	t.Run("no preview, no field", func(t *testing.T) {
		h := &Handler{Storage: &mockStorage{}}
		resp := h.attachmentToResponse(db.Attachment{Url: "https://cdn.example.com/a.png"}, attachmentURLModeSigned)
		if resp.PreviewURL != "" || resp.Width != nil || resp.Height != nil {
			t.Fatalf("resp = %+v", resp)
		}
	})
}

type recordingDeleteStorage struct {
	mockStorage
	mu      sync.Mutex
	deleted []string
}

func (s *recordingDeleteStorage) DeleteKeys(_ context.Context, keys []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleted = append(s.deleted, keys...)
}

func TestDeleteS3Objects_RemovesImagePreviews(t *testing.T) {
	store := &recordingDeleteStorage{}
	h := &Handler{Storage: store}
	h.deleteS3Objects(context.Background(), []string{
		"https://cdn.example.com/workspaces/ws/a.png",
		"https://cdn.example.com/workspaces/ws/b.JPG",
		"https://cdn.example.com/workspaces/ws/c.pdf",
		"",
	})
	h.deleteS3Object(context.Background(), "https://cdn.example.com/workspaces/ws/d.webp")
	want := []string{
		"workspaces/ws/a.png", "workspaces/ws/a.png.preview.png",
		"workspaces/ws/b.JPG", "workspaces/ws/b.JPG.preview.jpg",
		"workspaces/ws/c.pdf",
		"workspaces/ws/d.webp", "workspaces/ws/d.webp.preview.png",
	}
	if strings.Join(store.deleted, ",") != strings.Join(want, ",") {
		t.Fatalf("deleted %v, want %v", store.deleted, want)
	}
}
