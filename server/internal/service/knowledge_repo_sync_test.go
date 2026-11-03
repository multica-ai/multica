package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/knowledge"
)

// fakeRepoDocs stands in for the git client: it returns whatever docs holds.
type fakeRepoDocs struct {
	docs  []RepoDoc
	calls int
}

func (f *fakeRepoDocs) Snapshot(_ context.Context, _ RepoSyncSettings) ([]RepoDoc, error) {
	f.calls++
	return append([]RepoDoc(nil), f.docs...), nil
}

// syncedBody carries the one heading the reference kind requires.
func syncedBody(title string) string {
	return "# " + title + "\n\n## Summary\n\n" + title + " explained.\n"
}

func enableRepoSync(t *testing.T, e *knowledgeEnv, src RepoDocsSource) {
	t.Helper()
	e.svc.RepoDocs = src
	if _, err := e.svc.SetRepoSync(context.Background(), e.ws, RepoSyncSettings{
		Enabled: true, RepoURL: "https://example.test/acme/app.git",
	}); err != nil {
		t.Fatal(err)
	}
}

// engineeringWeKnoraDocs lists the documents WeKnora holds in the engineering base.
func (e *knowledgeEnv) engineeringWeKnoraDocs(t *testing.T) []knowledge.Document {
	t.Helper()
	ctx := context.Background()
	base, err := e.q.GetKnowledgeBaseByKey(ctx, dbBaseKey(e, "engineering"))
	if err != nil {
		t.Fatal(err)
	}
	page, err := e.weknoraWorkspace().ListDocuments(ctx, base.WeknoraKbID, knowledge.ListOptions{PageSize: 100})
	if err != nil {
		t.Fatal(err)
	}
	return page.Items
}

func (e *knowledgeEnv) syncedFile(t *testing.T, path string) (fileRecord, bool) {
	t.Helper()
	files, err := e.q.ListKnowledgeRepoFiles(context.Background(), e.ws)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f.Path == path {
			return fileRecord{hash: f.ContentHash, linked: f.KnowledgeLinkID.Valid, linkID: f.KnowledgeLinkID.String(), archived: f.ArchivedAt.Valid}, true
		}
	}
	return fileRecord{}, false
}

type fileRecord struct {
	hash     string
	linked   bool
	linkID   string
	archived bool
}

func TestRepoSyncCreatesANewFile(t *testing.T) {
	e := newKnowledgeEnv(t).enabled()
	ctx := context.Background()
	src := &fakeRepoDocs{docs: []RepoDoc{{Path: "docs/adr/0001-storage.md", Content: syncedBody("Storage")}}}
	enableRepoSync(t, e, src)

	if err := e.svc.SyncRepoDocs(ctx, e.ws); err != nil {
		t.Fatal(err)
	}
	docs := e.engineeringWeKnoraDocs(t)
	if len(docs) != 1 {
		t.Fatalf("engineering documents = %d, want 1", len(docs))
	}
	body, err := e.weknoraWorkspace().Read(ctx, docs[0].ID)
	if err != nil || !strings.Contains(body.Content, "Storage explained") {
		t.Fatalf("synced content = %q, %v", body.Content, err)
	}
	rec, ok := e.syncedFile(t, "docs/adr/0001-storage.md")
	if !ok || rec.archived || rec.hash == "" || !rec.linked {
		t.Fatalf("sync record = %+v, found %v", rec, ok)
	}
}

func TestRepoSyncUpdatesAChangedFile(t *testing.T) {
	e := newKnowledgeEnv(t).enabled()
	ctx := context.Background()
	src := &fakeRepoDocs{docs: []RepoDoc{{Path: "docs/runbook.md", Content: syncedBody("Restart")}}}
	enableRepoSync(t, e, src)
	if err := e.svc.SyncRepoDocs(ctx, e.ws); err != nil {
		t.Fatal(err)
	}
	before, _ := e.syncedFile(t, "docs/runbook.md")
	oldDocs := e.engineeringWeKnoraDocs(t)

	src.docs = []RepoDoc{{Path: "docs/runbook.md", Content: syncedBody("Restart v2")}}
	if err := e.svc.SyncRepoDocs(ctx, e.ws); err != nil {
		t.Fatal(err)
	}
	docs := e.engineeringWeKnoraDocs(t)
	if len(docs) != 1 || docs[0].ID == oldDocs[0].ID {
		t.Fatalf("after the change: %d documents, the old one kept: %v", len(docs), docs)
	}
	body, _ := e.weknoraWorkspace().Read(ctx, docs[0].ID)
	if !strings.Contains(body.Content, "Restart v2") {
		t.Fatalf("updated content = %q", body.Content)
	}
	after, _ := e.syncedFile(t, "docs/runbook.md")
	if after.hash == before.hash || after.linkID == before.linkID || after.archived {
		t.Fatalf("sync record did not move to the new version: before %+v, after %+v", before, after)
	}

	// An unchanged file is not written again.
	if err := e.svc.SyncRepoDocs(ctx, e.ws); err != nil {
		t.Fatal(err)
	}
	if e.fake.Calls(knowledge.OpCreateManual) != 2 {
		t.Fatalf("creates = %d, want 2 (the first version and the update)", e.fake.Calls(knowledge.OpCreateManual))
	}
}

func TestRepoSyncArchivesARemovedFile(t *testing.T) {
	e := newKnowledgeEnv(t).enabled()
	ctx := context.Background()
	src := &fakeRepoDocs{docs: []RepoDoc{
		{Path: "docs/keep.md", Content: syncedBody("Keep")},
		{Path: "docs/gone.md", Content: syncedBody("Gone")},
	}}
	enableRepoSync(t, e, src)
	if err := e.svc.SyncRepoDocs(ctx, e.ws); err != nil {
		t.Fatal(err)
	}
	src.docs = src.docs[:1]
	if err := e.svc.SyncRepoDocs(ctx, e.ws); err != nil {
		t.Fatal(err)
	}
	if docs := e.engineeringWeKnoraDocs(t); len(docs) != 1 {
		t.Fatalf("after the removal: %d documents, want only the kept one", len(docs))
	}
	rec, ok := e.syncedFile(t, "docs/gone.md")
	if !ok || !rec.archived || rec.linked {
		t.Fatalf("removed file's record = %+v, found %v; want archived and unlinked", rec, ok)
	}
	if n := e.linkCount(); n != 1 {
		t.Fatalf("registry links after the archive = %d, want 1", n)
	}
}

func TestRepoSyncDoesNothingWhenDisabled(t *testing.T) {
	e := newKnowledgeEnv(t).enabled()
	ctx := context.Background()
	src := &fakeRepoDocs{docs: []RepoDoc{{Path: "docs/a.md", Content: syncedBody("A")}}}
	e.svc.RepoDocs = src
	if _, err := e.svc.SetRepoSync(ctx, e.ws, RepoSyncSettings{Enabled: false, RepoURL: "https://example.test/acme/app.git"}); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.SyncRepoDocs(ctx, e.ws); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.RunRepoDocsSync(ctx); err != nil {
		t.Fatalf("pass over a disabled workspace: %v", err)
	}
	if src.calls != 0 || len(e.engineeringWeKnoraDocs(t)) != 0 {
		t.Fatalf("a disabled sync read the repository (%d calls) or wrote documents", src.calls)
	}
	// A workspace that never set the sync has no row and no work either.
	status, err := e.svc.GetRepoSync(ctx, e.ws)
	if err != nil || status.Enabled || status.KBKey != "engineering" || status.DocsPath != "docs" {
		t.Fatalf("default setting = %+v, %v", status, err)
	}
}

func TestRepoSyncRefusesANonHTTPSRepository(t *testing.T) {
	e := newKnowledgeEnv(t).enabled()
	for _, repo := range []string{"http://example.test/acme/app.git", "git@example.test:acme/app.git", "file:///srv/app", "https://"} {
		_, err := e.svc.SetRepoSync(context.Background(), e.ws, RepoSyncSettings{Enabled: true, RepoURL: repo})
		var invalid *KnowledgeInvalidError
		if !errors.As(err, &invalid) {
			t.Fatalf("repo %q: err = %v, want an invalid error", repo, err)
		}
	}
}

func TestRepoSyncRecordsAFailedFileAndKeepsTheRest(t *testing.T) {
	e := newKnowledgeEnv(t).enabled()
	ctx := context.Background()
	src := &fakeRepoDocs{docs: []RepoDoc{
		{Path: "docs/bare.md", Content: "# No summary here\n\ntext\n"},
		{Path: "docs/good.md", Content: syncedBody("Good")},
	}}
	enableRepoSync(t, e, src)
	if err := e.svc.SyncRepoDocs(ctx, e.ws); err != nil {
		t.Fatal(err)
	}
	if docs := e.engineeringWeKnoraDocs(t); len(docs) != 1 {
		t.Fatalf("documents = %d, want the good file only", len(docs))
	}
	status, err := e.svc.GetRepoSync(ctx, e.ws)
	if err != nil || !strings.Contains(status.LastError, "docs/bare.md") || strings.Contains(status.LastError, "good.md") {
		t.Fatalf("last error = %q, %v", status.LastError, err)
	}
}

func TestReadRepoDocsTakesMarkdownUnderTheDocsPath(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("docs/b/second.md", "# Second\n")
	write("docs/a.md", "# First\n")
	write("docs/notes.txt", "not markdown")
	write("docs/blank.md", "   \n")
	write("src/outside.md", "# Outside the docs path\n")
	if err := os.Symlink(filepath.Join(root, "src", "outside.md"), filepath.Join(root, "docs", "link.md")); err != nil {
		t.Fatal(err)
	}

	docs, err := readRepoDocs(root, "docs")
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, d := range docs {
		paths = append(paths, d.Path)
	}
	if want := []string{"docs/a.md", "docs/b/second.md"}; !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths = %v, want %v", paths, want)
	}
	if _, err := readRepoDocs(root, "missing"); err == nil {
		t.Fatal("a missing docs path was not reported")
	}
}
