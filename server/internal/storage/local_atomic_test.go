package storage

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type failingReader struct {
	head     string
	consumed bool
}

func (r *failingReader) Read(p []byte) (int, error) {
	if !r.consumed {
		r.consumed = true
		n := copy(p, r.head)
		return n, nil
	}
	return 0, errors.New("injected stream failure")
}

// A failed UploadStream must never destroy an object a previous successful
// upload of the same key produced: writing straight to the destination would
// truncate it up front (and the old cleanup removed it outright), leaving any
// attachment row that already points at it dangling.
func TestLocalStorageUploadStreamFailureKeepsPreviousObject(t *testing.T) {
	dir := t.TempDir()
	s := &LocalStorage{uploadDir: dir}
	key := "workspaces/ws/lark/dup"

	if _, err := s.UploadStream(context.Background(), key, strings.NewReader("first-upload"), 12, "image/png", "a.png"); err != nil {
		t.Fatalf("first UploadStream: %v", err)
	}
	if _, err := s.UploadStream(context.Background(), key, &failingReader{head: "partial"}, 7, "image/png", "a.png"); err == nil {
		t.Fatal("second UploadStream must fail")
	}

	got, err := os.ReadFile(filepath.Join(dir, key))
	if err != nil {
		t.Fatalf("previous object must survive a failed re-upload: %v", err)
	}
	if string(got) != "first-upload" {
		t.Fatalf("object content = %q, want the first upload intact", got)
	}
	// No temp files left behind.
	entries, err := os.ReadDir(filepath.Dir(filepath.Join(dir, key)))
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Fatalf("leftover temp file %q", e.Name())
		}
	}
}

// Both upload paths land 0644 objects. CreateTemp makes its file 0600, so
// without the explicit mode the rename-into-place rewrite would have silently
// narrowed permissions on deployments that serve the upload dir with a
// front-end web server running as another user.
func TestLocalStorageUploadsAreWorldReadable(t *testing.T) {
	dir := t.TempDir()
	s := &LocalStorage{uploadDir: dir}
	if _, err := s.Upload(context.Background(), "buffered", []byte("a"), "text/plain", "a.txt"); err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if _, err := s.UploadStream(context.Background(), "streamed", strings.NewReader("a"), 1, "text/plain", "a.txt"); err != nil {
		t.Fatalf("UploadStream: %v", err)
	}
	for _, key := range []string{"buffered", "streamed"} {
		info, err := os.Stat(filepath.Join(dir, key))
		if err != nil {
			t.Fatalf("stat %s: %v", key, err)
		}
		if info.Mode().Perm() != 0644 {
			t.Fatalf("%s mode = %o, want 0644", key, info.Mode().Perm())
		}
	}
}

// A crash between the staging write and the rename leaves the temp file
// behind — no defer can cover that. The name is derived from the object key,
// so the delete path reclaims it: the media ledger records the intent before
// the upload, so the reconciler always reaches DeleteObject for an abandoned
// key. A randomized temp name would leave an orphan nothing could name.
func TestLocalStorageDeleteObjectReclaimsCrashLeftoverTemp(t *testing.T) {
	dir := t.TempDir()
	s := &LocalStorage{uploadDir: dir}
	key := "workspaces/ws/lark/crashed"
	dest := filepath.Join(dir, key)
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// The crash shape: a staging file, no object, no sidecar.
	leftover := tempPath(dest)
	if err := os.WriteFile(leftover, []byte("half-written body"), 0644); err != nil {
		t.Fatalf("plant leftover: %v", err)
	}

	if err := s.DeleteObject(context.Background(), key); err != nil {
		t.Fatalf("DeleteObject: %v", err)
	}
	if _, err := os.Stat(leftover); !os.IsNotExist(err) {
		t.Fatalf("crash leftover survived the delete path (stat err = %v)", err)
	}
}

// The staging file must not be readable through the object read paths: keys
// come straight from the request URL, so a half-written body would otherwise
// be exposed under a guessable name.
func TestLocalStorageRefusesToServeStagingFile(t *testing.T) {
	dir := t.TempDir()
	s := &LocalStorage{uploadDir: dir}
	key := "workspaces/ws/lark/staged"
	dest := filepath.Join(dir, key)
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(tempPath(dest), []byte("half"), 0644); err != nil {
		t.Fatalf("plant staging file: %v", err)
	}
	stagingKey := "workspaces/ws/lark/." + filepath.Base(key) + ".tmp"
	if _, err := s.GetReader(context.Background(), stagingKey); err == nil {
		t.Fatal("GetReader must refuse the staging file")
	}
	// A user-supplied ".tmp" extension is still a normal object: the key is a
	// generated UUID, so it never collides with the dot-prefixed staging name.
	if _, err := s.Upload(context.Background(), "workspaces/ws/0198-abc.tmp", []byte("real"), "application/octet-stream", "notes.tmp"); err != nil {
		t.Fatalf("Upload of a .tmp-extension object: %v", err)
	}
	rc, err := s.GetReader(context.Background(), "workspaces/ws/0198-abc.tmp")
	if err != nil {
		t.Fatalf("a .tmp-extension object must stay readable: %v", err)
	}
	rc.Close()
}

// The buffered Upload now goes through the same temp-file rename as the
// stream path. A byte slice cannot fail mid-copy, so what is observable here
// is the tail: a failed write leaves no temp litter behind and no damage to a
// previous object. (The truncate-up-front hazard itself is pinned on the
// stream path, which is the one that can fail mid-body.)
func TestLocalStorageUploadFailureLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	s := &LocalStorage{uploadDir: dir}
	key := "workspaces/ws/lark/buffered-dup"
	if _, err := s.Upload(context.Background(), key, []byte("first-upload"), "image/png", "a.png"); err != nil {
		t.Fatalf("first Upload: %v", err)
	}
	// A destination the rename cannot replace (a non-empty directory) is the
	// portable way to fail the write after the temp file is fully written.
	blocked := filepath.Join(dir, "blocked")
	if err := os.MkdirAll(filepath.Join(blocked, "child"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, err := s.Upload(context.Background(), "blocked", []byte("nope"), "image/png", "a.png"); err == nil {
		t.Fatal("Upload onto a non-empty directory must fail")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Fatalf("leftover temp file %q", e.Name())
		}
	}
	got, err := os.ReadFile(filepath.Join(dir, key))
	if err != nil || string(got) != "first-upload" {
		t.Fatalf("previous object = %q (err %v), want it intact", got, err)
	}
}

// Sanity: a successful stream still lands the full body and stays readable.
func TestLocalStorageUploadStreamAtomicSuccess(t *testing.T) {
	dir := t.TempDir()
	s := &LocalStorage{uploadDir: dir}
	key := "workspaces/ws/lark/ok"
	if _, err := s.UploadStream(context.Background(), key, strings.NewReader("payload"), 7, "text/plain", "p.txt"); err != nil {
		t.Fatalf("UploadStream: %v", err)
	}
	rc, err := s.GetReader(context.Background(), key)
	if err != nil {
		t.Fatalf("GetReader: %v", err)
	}
	defer rc.Close()
	body, _ := io.ReadAll(rc)
	if string(body) != "payload" {
		t.Fatalf("body = %q", body)
	}
}
