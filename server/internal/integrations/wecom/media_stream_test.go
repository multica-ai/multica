package wecom

// media_stream_test.go — the streaming decrypt has to agree with the one it
// replaces, byte for byte, at every size that matters.
//
// The buffered path holds the whole ciphertext and the whole plaintext at
// once. Eight of those in flight — the engine's own concurrency cap, reached
// by four people sending two large files each — is an OOM on a self-hosted
// box, and the process it kills serves every other channel too.
//
// The trap the streaming version has to get right is the pad. WeCom pads
// plaintext to a 32-byte block while AES works in 16, so the pad spans TWO
// cipher blocks and a decoder that holds one block back unpads correctly for
// only about half of all files. These tests walk every pad length.

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

// sealMedia builds ciphertext the way WeCom does: PKCS#7 to a 32-byte block,
// AES-256-CBC, IV = the first 16 bytes of the key.
func sealMedia(t *testing.T, key []byte, plain []byte) string {
	t.Helper()
	pad := mediaPadBlock - len(plain)%mediaPadBlock
	padded := append(append([]byte(nil), plain...), bytes.Repeat([]byte{byte(pad)}, pad)...)

	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("aes.NewCipher: %v", err)
	}
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, key[:aes.BlockSize]).CryptBlocks(out, padded)
	return string(out)
}

func mediaTestKey(t *testing.T) ([]byte, string) {
	t.Helper()
	key := make([]byte, mediaAESKeyBytes)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return key, base64.StdEncoding.EncodeToString(key)
}

// TestStreamingDecryptMatchesTheBufferedOneAtEveryPadLength is the one that
// matters. A pad of 1..32 covers both cipher blocks the pad can occupy, and
// the sizes are chosen to land on each.
func TestStreamingDecryptMatchesTheBufferedOneAtEveryPadLength(t *testing.T) {
	key, keyB64 := mediaTestKey(t)

	// Every residue mod 32 — so every possible pad length — plus sizes that
	// cross the read-chunk boundary.
	sizes := []int{0, 1, 15, 16, 17, 31, 32, 33, 63, 64, 65, 1000}
	for i := 0; i < 32; i++ {
		sizes = append(sizes, mediaPadBlock*3+i)
	}
	sizes = append(sizes, mediaStreamChunk-1, mediaStreamChunk, mediaStreamChunk+1, mediaStreamChunk*2+7)

	for _, n := range sizes {
		plain := make([]byte, n)
		if _, err := rand.Read(plain); err != nil {
			t.Fatalf("rand: %v", err)
		}
		ct := sealMedia(t, key, plain)

		want, err := decryptMedia(keyB64, []byte(ct))
		if err != nil {
			t.Fatalf("buffered decrypt of %d bytes: %v", n, err)
		}

		f, size, err := decryptToFile(keyB64, strings.NewReader(ct), t.TempDir())
		if err != nil {
			t.Fatalf("streaming decrypt of %d bytes: %v", n, err)
		}
		got, readErr := io.ReadAll(f)
		f.Close()
		if readErr != nil {
			t.Fatalf("read back %d bytes: %v", n, readErr)
		}

		if size != int64(len(want)) {
			t.Fatalf("size for %d bytes = %d, want %d — an upload with the wrong ContentLength is a corrupt object",
				n, size, len(want))
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("streaming and buffered decrypt disagree at %d bytes (pad %d)",
				n, mediaPadBlock-n%mediaPadBlock)
		}
		if !bytes.Equal(got, plain) {
			t.Fatalf("the decrypt did not round-trip at %d bytes", n)
		}
	}
}

// A source that hands over a few bytes at a time must produce the same file —
// this is what an actual socket does, and a decoder that assumed whole blocks
// per Read would corrupt everything.
func TestStreamingDecryptSurvivesADribblingReader(t *testing.T) {
	key, keyB64 := mediaTestKey(t)
	plain := []byte(strings.Repeat("这是一个文件的内容。", 500))
	ct := sealMedia(t, key, plain)

	f, size, err := decryptToFile(keyB64, &iotest1ByteReader{s: ct}, t.TempDir())
	if err != nil {
		t.Fatalf("decryptToFile: %v", err)
	}
	defer f.Close()
	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if int64(len(got)) != size || !bytes.Equal(got, plain) {
		t.Fatalf("a one-byte-at-a-time source produced %d bytes, want %d", len(got), len(plain))
	}
}

// iotest1ByteReader hands over a single byte per Read.
type iotest1ByteReader struct {
	s string
	i int
}

func (r *iotest1ByteReader) Read(p []byte) (int, error) {
	if r.i >= len(r.s) {
		return 0, io.EOF
	}
	p[0] = r.s[r.i]
	r.i++
	return 1, nil
}

// A wrong key must be refused rather than producing a file of noise the agent
// would then be handed.
func TestStreamingDecryptRefusesAWrongKey(t *testing.T) {
	key, _ := mediaTestKey(t)
	_, otherB64 := mediaTestKey(t)
	ct := sealMedia(t, key, []byte("the real contents"))

	if _, _, err := decryptToFile(otherB64, strings.NewReader(ct), t.TempDir()); err == nil {
		t.Fatal("a wrong key produced a file")
	}
}

// A truncated body is the same class of failure and must not half-succeed.
func TestStreamingDecryptRefusesATruncatedBody(t *testing.T) {
	key, keyB64 := mediaTestKey(t)
	ct := sealMedia(t, key, []byte(strings.Repeat("x", 200)))

	// Cut mid-block: not a multiple of the cipher block at all.
	if _, _, err := decryptToFile(keyB64, strings.NewReader(ct[:len(ct)-5]), t.TempDir()); err == nil {
		t.Fatal("a body cut mid-block produced a file")
	}
	// And cut on a block boundary, which only the pad check can catch.
	if _, _, err := decryptToFile(keyB64, strings.NewReader(ct[:len(ct)-aes.BlockSize]), t.TempDir()); err == nil {
		t.Fatal("a body cut on a block boundary produced a file")
	}
}

func TestStreamingDecryptRefusesAnEmptyBody(t *testing.T) {
	_, keyB64 := mediaTestKey(t)
	if _, _, err := decryptToFile(keyB64, strings.NewReader(""), t.TempDir()); err == nil {
		t.Fatal("an empty body produced a file")
	}
}

// The plaintext is an attachment. Its temp file must not be readable by
// anyone else, and must not outlive the handle — including if the process
// dies holding it.
func TestTheTempFileIsPrivateAndUnlinked(t *testing.T) {
	key, keyB64 := mediaTestKey(t)
	dir := t.TempDir()
	ct := sealMedia(t, key, []byte("secret contents"))

	f, _, err := decryptToFile(keyB64, strings.NewReader(ct), dir)
	if err != nil {
		t.Fatalf("decryptToFile: %v", err)
	}
	defer f.Close()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("the temp directory still holds %d entries; the plaintext file was not unlinked", len(entries))
	}

	info, err := f.Stat()
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("the plaintext file is mode %v, want no group or other access", perm)
	}
}

// peekFile has to leave the file where it found it, or the upload sends a
// truncated object.
func TestPeekFileRewinds(t *testing.T) {
	key, keyB64 := mediaTestKey(t)
	plain := []byte("0123456789abcdefghij")
	f, size, err := decryptToFile(keyB64, strings.NewReader(sealMedia(t, key, plain)), t.TempDir())
	if err != nil {
		t.Fatalf("decryptToFile: %v", err)
	}
	defer f.Close()

	head, err := peekFile(f, 4)
	if err != nil {
		t.Fatalf("peekFile: %v", err)
	}
	if string(head) != "0123" {
		t.Fatalf("peek = %q", head)
	}
	all, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("read after peek: %v", err)
	}
	if int64(len(all)) != size || !bytes.Equal(all, plain) {
		t.Fatalf("after peeking, the file read back %d bytes, want %d", len(all), size)
	}
}

// A peek longer than the file is what a small attachment produces, and must
// not be an error.
func TestPeekFileHandlesAShortFile(t *testing.T) {
	key, keyB64 := mediaTestKey(t)
	f, _, err := decryptToFile(keyB64, strings.NewReader(sealMedia(t, key, []byte("hi"))), t.TempDir())
	if err != nil {
		t.Fatalf("decryptToFile: %v", err)
	}
	defer f.Close()

	head, err := peekFile(f, 512)
	if err != nil {
		t.Fatalf("peekFile: %v", err)
	}
	if string(head) != "hi" {
		t.Fatalf("peek = %q, want the whole short file", head)
	}
}

// streamingStorage records what UploadStream was handed, and how much of the
// heap it had to take to do it.
type streamingStorage struct {
	fakeMediaStorage
	streamed  int
	gotSize   int64
	gotBytes  []byte
	gotType   string
	streamErr error
}

func (s *streamingStorage) UploadStream(_ context.Context, key string, data io.Reader, size int64, contentType, filename string) (string, error) {
	if s.streamErr != nil {
		return "", s.streamErr
	}
	s.streamed++
	s.gotSize = size
	s.gotType = contentType
	b, err := io.ReadAll(data)
	if err != nil {
		return "", err
	}
	s.gotBytes = b
	return "https://objects.example/" + key, nil
}

// TestIngestPrefersTheStreamingPathAndUploadsTheRightBytes — the whole point
// of the temp file. A backend that can stream must be handed the plaintext
// with its exact length, and the object must be byte-identical to what the
// buffered path would have produced.
func TestIngestPrefersTheStreamingPathAndUploadsTheRightBytes(t *testing.T) {
	key, keyB64 := mediaTestKey(t)
	plain := []byte(strings.Repeat("报告正文。", 4000))
	ct := sealMedia(t, key, plain)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="report.txt"`)
		w.Write([]byte(ct))
	}))
	defer srv.Close()

	storage := &streamingStorage{}
	r := &wecomMediaResolver{
		storage: storage,
		ledger:  newFakeMediaLedger(nil),
		http:    testMediaClient(),
		logger:  testLogger(),
	}

	msg := mediaMessage(t, "file", map[string]any{
		"file": map[string]any{"url": srv.URL + "/object", "aeskey": keyB64},
	})
	out := r.ResolveMedia(context.Background(), mediaInstallation(), engine.ResolvedIdentity{}, uuidOf(6), uuidOf(5), msg)

	if storage.streamed != 1 {
		t.Fatalf("UploadStream was called %d times, want the streaming path taken", storage.streamed)
	}
	if storage.gotSize != int64(len(plain)) {
		t.Fatalf("ContentLength = %d, want %d — a wrong length is a corrupt object", storage.gotSize, len(plain))
	}
	if !bytes.Equal(storage.gotBytes, plain) {
		t.Fatalf("the uploaded object is %d bytes, want the %d-byte plaintext", len(storage.gotBytes), len(plain))
	}
	if len(out.MediaRefs) != 1 {
		t.Fatalf("MediaRefs = %d, want the attachment bound", len(out.MediaRefs))
	}
	if out.MediaRefs[0].SizeBytes != int64(len(plain)) {
		t.Errorf("the ref carries size %d, want %d", out.MediaRefs[0].SizeBytes, len(plain))
	}
	if out.MediaRefs[0].Filename != "report.txt" {
		t.Errorf("filename = %q", out.MediaRefs[0].Filename)
	}
}

// A backend with no streaming half still works — the buffered path is the
// fallback, not a dead branch.
func TestIngestFallsBackWhenTheBackendCannotStream(t *testing.T) {
	key, keyB64 := mediaTestKey(t)
	plain := []byte("a short attachment")
	ct := sealMedia(t, key, plain)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(ct))
	}))
	defer srv.Close()

	storage := &fakeMediaStorage{}
	r := &wecomMediaResolver{
		storage: storage,
		ledger:  newFakeMediaLedger(storage),
		http:    testMediaClient(),
		logger:  testLogger(),
	}

	msg := mediaMessage(t, "file", map[string]any{
		"file": map[string]any{"url": srv.URL + "/object", "aeskey": keyB64},
	})
	out := r.ResolveMedia(context.Background(), mediaInstallation(), engine.ResolvedIdentity{}, uuidOf(6), uuidOf(5), msg)
	if len(out.MediaRefs) != 1 {
		t.Fatalf("MediaRefs = %d, want the buffered path to have carried it", len(out.MediaRefs))
	}
}

// The ceiling still holds on the streaming path. A body that keeps going past
// it must be cut off rather than allowed to fill the disk the way it used to
// be able to fill the heap.
func TestTheStreamingPathStillRefusesAnOversizeBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// No Content-Length, and more bytes than the cap.
		chunk := bytes.Repeat([]byte("x"), 1<<20)
		for written := 0; written <= maxMediaBytes; written += len(chunk) {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	body, _, err := openMedia(context.Background(), testMediaClient(), srv.URL+"/object")
	if err != nil {
		t.Fatalf("openMedia: %v", err)
	}
	defer body.Close()

	n, err := io.Copy(io.Discard, body)
	if !errors.Is(err, errMediaTooLarge) {
		t.Fatalf("read %d bytes with err %v, want the size refusal", n, err)
	}
	if n > maxMediaBytes+1 {
		t.Fatalf("read %d bytes before stopping, want the cap to hold", n)
	}
}
