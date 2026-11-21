package wecom

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

func assertMediaTempDirEmpty(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("media processing left %d temporary files", len(entries))
	}
}

func assertMediaTempFileClosed(t *testing.T, f *os.File) {
	t.Helper()
	if f == nil {
		t.Fatal("streaming upload did not receive a plaintext file")
	}
	// Stat can return ERROR_INVALID_HANDLE after Close on Windows.
	// Read checks Go's closed-file state consistently across platforms.
	var buf [1]byte
	if _, err := f.Read(buf[:]); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("plaintext file is still open: %v", err)
	}
}

func setMediaTempDir(t *testing.T, dir string) {
	t.Helper()
	for _, name := range []string{"TMPDIR", "TMP", "TEMP", "SystemTemp"} {
		t.Setenv(name, dir)
	}
}

func TestMediaTempFileRemovedAfterClose(t *testing.T) {
	dir := t.TempDir()
	key, keyB64 := mediaTestKey(t)
	f, _, err := decryptToFile(keyB64, strings.NewReader(sealMedia(t, key, []byte("private attachment"))), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	assertMediaTempDirEmpty(t, dir)
}

func TestMediaTempFileRemovedAfterProcessExit(t *testing.T) {
	if os.Getenv("MULTICA_TEST_MEDIA_TEMP_CHILD") == "1" {
		key, keyB64 := mediaTestKey(t)
		f, _, err := decryptToFile(keyB64, strings.NewReader(sealMedia(t, key, []byte("private attachment"))), os.Getenv("MULTICA_TEST_MEDIA_TEMP_DIR"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		os.Exit(43) // Bypass deferred closes, as an abnormal process exit does.
	}
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestMediaTempFileRemovedAfterProcessExit$")
	cmd.Env = append(os.Environ(), "MULTICA_TEST_MEDIA_TEMP_CHILD=1", "MULTICA_TEST_MEDIA_TEMP_DIR="+dir)
	output, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 43 {
		t.Fatalf("child did not reach the abrupt exit: %v\n%s", err, output)
	}
	assertMediaTempDirEmpty(t, dir)
}

func TestMediaTempFileCreationFailureClassified(t *testing.T) {
	dir := t.TempDir()
	_, keyB64 := mediaTestKey(t)
	f, _, err := decryptToFile(keyB64, strings.NewReader(""), filepath.Join(dir, "missing"))
	if f != nil || !errors.Is(err, errMediaTempFile) || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temp creation failure lost its classification or cause: file=%v err=%v", f, err)
	}
	assertMediaTempDirEmpty(t, dir)
}

type mediaErrorReader struct{ err error }

func (r mediaErrorReader) Read([]byte) (int, error) { return 0, r.err }

func TestStreamingDecryptReadErrorCleansTempFile(t *testing.T) {
	key, keyB64 := mediaTestKey(t)
	ct := sealMedia(t, key, []byte(strings.Repeat("x", mediaStreamChunk*2)))
	for _, readErr := range []error{io.ErrUnexpectedEOF, context.Canceled} {
		t.Run(readErr.Error(), func(t *testing.T) {
			dir := t.TempDir()
			src := io.MultiReader(strings.NewReader(ct[:mediaStreamChunk]), mediaErrorReader{readErr})
			f, _, err := decryptToFile(keyB64, src, dir)
			if f != nil || !errors.Is(err, readErr) || errors.Is(err, errMediaTempFile) {
				t.Fatalf("read failure was misclassified: file=%v err=%v", f, err)
			}
			assertMediaTempDirEmpty(t, dir)
		})
	}
}

func TestIngestStreamingFailureClosesTempFile(t *testing.T) {
	for _, failure := range []string{"upload", "cancel"} {
		t.Run(failure, func(t *testing.T) {
			dir := t.TempDir()
			setMediaTempDir(t, dir)
			key, keyB64 := mediaTestKey(t)
			ct := sealMedia(t, key, []byte("private attachment"))
			var requests atomic.Int64
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				w.Write([]byte(ct))
			}))
			defer srv.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			wantErr := errors.New("upload failed")
			storage := &streamingStorage{streamErr: wantErr}
			if failure == "cancel" {
				wantErr = context.Canceled
				storage.onStream = cancel
			}
			r := newTestResolver(storage, newFakeMediaLedger(nil), nil)
			msg := mediaMessage(t, "file", map[string]any{"file": map[string]any{"url": srv.URL, "aeskey": keyB64}})
			wm, err := wecomMsgFromRaw(msg)
			if err != nil {
				t.Fatal(err)
			}
			_, err = r.ingestOne(ctx, mediaInstallation(), uuidOf(5), wm, 0, wm.Media[0])
			if !errors.Is(err, wantErr) {
				t.Fatalf("ingest error=%v, want %v", err, wantErr)
			}
			if requests.Load() != 1 || len(storage.stored()) != 0 {
				t.Fatalf("failed upload was retried: requests=%d buffered=%d", requests.Load(), len(storage.stored()))
			}
			assertMediaTempFileClosed(t, storage.gotFile)
			assertMediaTempDirEmpty(t, dir)
		})
	}
}

func TestIngestFallsBackWhenTempFileUnavailable(t *testing.T) {
	dir := t.TempDir()
	setMediaTempDir(t, filepath.Join(dir, "missing"))
	key, keyB64 := mediaTestKey(t)
	ct := sealMedia(t, key, []byte("private attachment"))
	var requests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Write([]byte(ct))
	}))
	defer srv.Close()
	storage := &streamingStorage{}
	r := newTestResolver(storage, newFakeMediaLedger(nil), nil)
	msg := mediaMessage(t, "file", map[string]any{"file": map[string]any{"url": srv.URL, "aeskey": keyB64}})
	out := r.ResolveMedia(context.Background(), mediaInstallation(), engine.ResolvedIdentity{}, uuidOf(6), uuidOf(5), msg)
	r.notices.Wait()
	if len(out.MediaRefs) != 1 || requests.Load() != 2 || len(storage.stored()) != 1 || storage.gotFile != nil {
		t.Fatalf("temp-file failure did not use buffered fallback: refs=%d requests=%d buffered=%d file=%v", len(out.MediaRefs), requests.Load(), len(storage.stored()), storage.gotFile)
	}
	assertMediaTempDirEmpty(t, dir)
}

type mediaReadErrorTransport struct {
	http.RoundTripper
	ciphertext string
	err        error
}

func (tr mediaReadErrorTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := tr.RoundTripper.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	resp.Body.Close()
	resp.Body = io.NopCloser(io.MultiReader(strings.NewReader(tr.ciphertext), mediaErrorReader{tr.err}))
	resp.ContentLength = -1
	return resp, nil
}

func TestIngestReadFailureDoesNotRetryAsTempFailure(t *testing.T) {
	dir := t.TempDir()
	setMediaTempDir(t, dir)
	key, keyB64 := mediaTestKey(t)
	ct := sealMedia(t, key, []byte("private attachment"))
	var requests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Write([]byte(ct))
	}))
	defer srv.Close()
	storage := &streamingStorage{}
	r := newTestResolver(storage, newFakeMediaLedger(nil), nil)
	readErr := errors.New("media temp file: error text from the source")
	r.http.Transport = mediaReadErrorTransport{r.http.Transport, ct, readErr}
	msg := mediaMessage(t, "file", map[string]any{"file": map[string]any{"url": srv.URL, "aeskey": keyB64}})
	wm, err := wecomMsgFromRaw(msg)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.ingestOne(context.Background(), mediaInstallation(), uuidOf(5), wm, 0, wm.Media[0])
	if !errors.Is(err, readErr) || requests.Load() != 1 {
		t.Fatalf("source failure was retried as a temp-file failure: err=%v requests=%d", err, requests.Load())
	}
	if storage.gotFile != nil || len(storage.stored()) != 0 {
		t.Fatal("a failed download reached storage")
	}
	assertMediaTempDirEmpty(t, dir)
}
