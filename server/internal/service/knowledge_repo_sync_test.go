package service

import (
	"context"
	"errors"
	"os"
	"os/exec"
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

// makeGitRepo commits files, and symlinks as symbolic links, to a new repository
// on branch main in a test temp directory. It returns the directory.
func makeGitRepo(t *testing.T, files, links map[string]string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	for p, body := range files {
		writeRepoFile(t, dir, p, body)
	}
	for p, target := range links {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, full); err != nil {
			t.Fatal(err)
		}
	}
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "-m", "docs")
	return dir
}

// writeRepoFile writes one file of a repository; the next commit records it.
func writeRepoFile(t *testing.T, dir, p, body string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(p))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// commitRepo stages and commits everything in dir, removals included.
func commitRepo(t *testing.T, dir, message string) {
	t.Helper()
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "-m", message)
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.test",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.test")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
}

// localGitDocs is the production git client aimed at a repository on disk: the
// setting's address is ignored and the clone reads dir instead.
type localGitDocs struct{ dir string }

func (l localGitDocs) Snapshot(ctx context.Context, cfg RepoSyncSettings) ([]RepoDoc, error) {
	return cloneRepoDocs(ctx, l.dir, cfg.Branch, cfg.DocsPath)
}

func TestCloneRepoDocsReadsOnlyMarkdownUnderTheDocsPath(t *testing.T) {
	dir := makeGitRepo(t, map[string]string{
		"docs/adr/0001-storage.md": syncedBody("Storage"),
		"docs/empty.md":            "   \n",
		"docs/notes.txt":           "not markdown",
		"README.md":                syncedBody("Readme"),
	}, map[string]string{
		"docs/link.md": "../README.md",
	})

	docs, err := cloneRepoDocs(context.Background(), dir, "main", "docs")
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || docs[0].Path != "docs/adr/0001-storage.md" {
		t.Fatalf("docs = %+v, want only docs/adr/0001-storage.md (blank, non-Markdown, outside and symlinked files skipped)", docs)
	}
	if !strings.Contains(docs[0].Content, "Storage explained") {
		t.Fatalf("content = %q", docs[0].Content)
	}
}

func TestCloneRepoDocsRefusesAMissingDocsDirectory(t *testing.T) {
	dir := makeGitRepo(t, map[string]string{"README.md": syncedBody("Readme")}, nil)
	_, err := cloneRepoDocs(context.Background(), dir, "main", "docs")
	var invalid *KnowledgeInvalidError
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want a KnowledgeInvalidError naming the missing docs directory", err)
	}
}

func TestCloneRepoDocsFailsForAnUnknownBranch(t *testing.T) {
	dir := makeGitRepo(t, map[string]string{"docs/a.md": syncedBody("A")}, nil)
	if _, err := cloneRepoDocs(context.Background(), dir, "no-such-branch", "docs"); err == nil || !strings.Contains(err.Error(), "git clone failed") {
		t.Fatalf("err = %v, want the clone to fail on the missing branch", err)
	}
}

// A local git repository drives the whole sync: a new file is created, an edited
// one replaced, and a removed one archived, one commit at a time.
func TestRepoSyncFollowsALocalGitRepository(t *testing.T) {
	e := newKnowledgeEnv(t).enabled()
	ctx := context.Background()
	dir := makeGitRepo(t, map[string]string{
		"docs/adr/0001-storage.md": syncedBody("Storage"),
		"docs/runbooks/deploy.md":  syncedBody("Deploy"),
	}, nil)
	enableRepoSync(t, e, localGitDocs{dir: dir})

	if err := e.svc.SyncRepoDocs(ctx, e.ws); err != nil {
		t.Fatal(err)
	}
	if n := len(e.engineeringWeKnoraDocs(t)); n != 2 {
		t.Fatalf("after the first commit: engineering documents = %d, want 2", n)
	}

	writeRepoFile(t, dir, "docs/adr/0001-storage.md", syncedBody("Storage v2"))
	commitRepo(t, dir, "edit storage")
	if err := e.svc.SyncRepoDocs(ctx, e.ws); err != nil {
		t.Fatal(err)
	}
	foundV2 := false
	for _, d := range e.engineeringWeKnoraDocs(t) {
		body, err := e.weknoraWorkspace().Read(ctx, d.ID)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(body.Content, "Storage v2 explained") {
			foundV2 = true
		}
	}
	if !foundV2 {
		t.Fatal("the edited file was not replaced in the engineering base")
	}

	if err := os.Remove(filepath.Join(dir, "docs", "runbooks", "deploy.md")); err != nil {
		t.Fatal(err)
	}
	commitRepo(t, dir, "remove deploy runbook")
	if err := e.svc.SyncRepoDocs(ctx, e.ws); err != nil {
		t.Fatal(err)
	}
	if rec, ok := e.syncedFile(t, "docs/runbooks/deploy.md"); !ok || !rec.archived {
		t.Fatalf("the removed file's record = %+v, found %v; want it archived", rec, ok)
	}
	if n := len(e.engineeringWeKnoraDocs(t)); n != 1 {
		t.Fatalf("after the removal: engineering documents = %d, want the 1 live file", n)
	}
}
