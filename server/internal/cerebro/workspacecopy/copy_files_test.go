package workspacecopy

import (
	"bytes"
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// memStorage is an in-memory storage.Storage for the materialize test. Keys are
// "workspaces/<wsid>/..."; URLs are "/uploads/" + key, so KeyFromURL round-trips
// and a key's workspace segment is detectable by substring (the idempotency
// signal MaterializeCopiedFiles relies on).
type memStorage struct {
	mu    sync.Mutex
	blobs map[string][]byte
}

func newMemStorage() *memStorage { return &memStorage{blobs: map[string][]byte{}} }

func (m *memStorage) Upload(_ context.Context, key string, data []byte, _ string, _ string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]byte, len(data))
	copy(cp, data)
	m.blobs[key] = cp
	return "/uploads/" + key, nil
}

func (m *memStorage) Delete(_ context.Context, key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.blobs, key)
}

func (m *memStorage) DeleteKeys(ctx context.Context, keys []string) {
	for _, k := range keys {
		m.Delete(ctx, k)
	}
}

func (m *memStorage) KeyFromURL(rawURL string) string {
	return strings.TrimPrefix(rawURL, "/uploads/")
}

func (m *memStorage) CdnDomain() string { return "" }

func (m *memStorage) GetReader(_ context.Context, key string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.blobs[key]
	if !ok {
		return nil, io.ErrUnexpectedEOF
	}
	cp := make([]byte, len(data))
	copy(cp, data)
	return io.NopCloser(bytes.NewReader(cp)), nil
}

// TECH-3766: MaterializeCopiedFiles gives every shared blob its own physical
// copy in the target workspace and rewrites the row URL, so the source can be
// deleted. A blob already owned by the target is left untouched, and the pass is
// idempotent.
func TestMaterializeCopiedFiles(t *testing.T) {
	if testPool == nil {
		t.Skip("no test database")
	}
	ctx := context.Background()
	mem := newMemStorage()
	s := New(testPool).WithStorage(mem)

	tgtWS := isolatedWS(t, ctx, "wscopy-mat-tgt", "MAT")
	otherWS := uuid.UUID(newID().Bytes).String() // stands in for the copy source

	// Seed three target rows whose blobs all exist in storage.
	sharedArtKey := "workspaces/" + otherWS + "/artifacts/" + uuid.NewString() + ".pdf"
	ownArtKey := "workspaces/" + uuid.UUID(tgtWS.Bytes).String() + "/artifacts/" + uuid.NewString() + ".pdf"
	sharedAttKey := "workspaces/" + otherWS + "/attachments/" + uuid.NewString() + ".png"
	wantArt := []byte("shared-artifact-bytes")
	wantAtt := []byte("shared-attachment-bytes")
	if _, err := mem.Upload(ctx, sharedArtKey, wantArt, "application/pdf", "doc.pdf"); err != nil {
		t.Fatalf("seed shared artifact blob: %v", err)
	}
	if _, err := mem.Upload(ctx, ownArtKey, []byte("already-local"), "application/pdf", "local.pdf"); err != nil {
		t.Fatalf("seed local artifact blob: %v", err)
	}
	if _, err := mem.Upload(ctx, sharedAttKey, wantAtt, "image/png", "pic.png"); err != nil {
		t.Fatalf("seed shared attachment blob: %v", err)
	}

	sharedArt := newID()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO artifact (id, workspace_id, kind, title, body, author_type, author_id, format, file_url, file_size_bytes)
		VALUES ($1,$2,'report','Shared doc','','member',$3,'pdf',$4,$5)`,
		sharedArt, tgtWS, testUser, "/uploads/"+sharedArtKey, int64(len(wantArt))); err != nil {
		t.Fatalf("seed shared artifact: %v", err)
	}
	localArt := newID()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO artifact (id, workspace_id, kind, title, body, author_type, author_id, format, file_url)
		VALUES ($1,$2,'report','Local doc','','member',$3,'pdf',$4)`,
		localArt, tgtWS, testUser, "/uploads/"+ownArtKey); err != nil {
		t.Fatalf("seed local artifact: %v", err)
	}
	sharedAtt := newID()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO attachment (id, workspace_id, uploader_type, uploader_id, filename, url, content_type, size_bytes, created_at)
		VALUES ($1,$2,'member',$3,'pic.png',$4,'image/png',$5,$6)`,
		sharedAtt, tgtWS, testUser, "/uploads/"+sharedAttKey, int64(len(wantAtt)), time.Now()); err != nil {
		t.Fatalf("seed shared attachment: %v", err)
	}

	res, err := s.MaterializeCopiedFiles(ctx, tgtWS)
	if err != nil {
		t.Fatalf("MaterializeCopiedFiles: %v", err)
	}
	if res.ArtifactFiles != 1 {
		t.Fatalf("artifact files materialized = %d, want 1 (the shared one; the local one is skipped)", res.ArtifactFiles)
	}
	if res.Attachments != 1 {
		t.Fatalf("attachments materialized = %d, want 1", res.Attachments)
	}
	if res.FilesSkipped != 1 {
		t.Fatalf("files skipped = %d, want 1 (the already-local artifact)", res.FilesSkipped)
	}

	// The shared artifact URL now points at a target-owned blob with identical bytes.
	var newArtURL string
	if err := testPool.QueryRow(ctx, `SELECT file_url FROM artifact WHERE id = $1`, sharedArt).Scan(&newArtURL); err != nil {
		t.Fatalf("reload artifact: %v", err)
	}
	if !strings.Contains(newArtURL, uuid.UUID(tgtWS.Bytes).String()) {
		t.Fatalf("artifact URL %q not rewritten under target workspace", newArtURL)
	}
	rc, err := mem.GetReader(ctx, mem.KeyFromURL(newArtURL))
	if err != nil {
		t.Fatalf("open new artifact blob: %v", err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if !bytes.Equal(got, wantArt) {
		t.Fatalf("new artifact blob = %q, want %q", got, wantArt)
	}

	// The local artifact was not touched.
	var localURL string
	if err := testPool.QueryRow(ctx, `SELECT file_url FROM artifact WHERE id = $1`, localArt).Scan(&localURL); err != nil {
		t.Fatalf("reload local artifact: %v", err)
	}
	if localURL != "/uploads/"+ownArtKey {
		t.Fatalf("local artifact URL changed to %q, want unchanged", localURL)
	}

	// Idempotent: a second pass finds nothing left to materialize.
	res2, err := s.MaterializeCopiedFiles(ctx, tgtWS)
	if err != nil {
		t.Fatalf("second MaterializeCopiedFiles: %v", err)
	}
	if res2.ArtifactFiles != 0 || res2.Attachments != 0 {
		t.Fatalf("second pass materialized art=%d att=%d, want 0/0 (idempotent)", res2.ArtifactFiles, res2.Attachments)
	}
}
