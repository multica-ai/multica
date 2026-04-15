package daemon

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
)

func TestScanLocalSkillsFiltersAndSorts(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	writeTestFile(t, filepath.Join(root, "zeta", "SKILL.md"), "# Zeta\n")
	writeTestFile(t, filepath.Join(root, "zeta", "notes.txt"), "notes")

	writeTestFile(t, filepath.Join(root, "alpha", "SKILL.md"), "# Alpha\n")
	writeTestFile(t, filepath.Join(root, "alpha", "docs", "guide.md"), "guide")
	writeTestFile(t, filepath.Join(root, "alpha", "docs", "a.txt"), "A")
	writeTestFile(t, filepath.Join(root, "alpha", ".DS_Store"), "junk")
	writeTestFile(t, filepath.Join(root, "alpha", ".hidden", "ignored.txt"), "ignored")
	writeTestFile(t, filepath.Join(root, "alpha", "__pycache__", "ignored.pyc"), "ignored")
	writeTestFile(t, filepath.Join(root, "alpha", "node_modules", "pkg", "index.js"), "ignored")
	writeTestFile(t, filepath.Join(root, "alpha", "dist", "bundle.js"), "ignored")
	writeTestFile(t, filepath.Join(root, "alpha", "build", "artifact.txt"), "ignored")
	writeTestFile(t, filepath.Join(root, "alpha", ".next", "cache.txt"), "ignored")
	writeTestFile(t, filepath.Join(root, "alpha", "coverage", "report.txt"), "ignored")
	writeTestFile(t, filepath.Join(root, "alpha", "invalid.txt"), string([]byte{0xff, 0xfe, 0xfd}))
	writeTestFile(t, filepath.Join(root, "alpha", "binary.bin"), string([]byte{'a', 0x00, 'b'}))
	writeTestFile(t, filepath.Join(root, "alpha", "control-heavy.bin"), string([]byte{0x01, 0x02, 0x03, 'A', 'B'}))

	writeTestFile(t, filepath.Join(root, "plain-dir", "readme.txt"), "not a skill")
	writeTestFile(t, filepath.Join(root, ".dot-skill", "SKILL.md"), "# Hidden\n")

	skills, err := ScanLocalSkills(root)
	if err != nil {
		t.Fatalf("ScanLocalSkills returned error: %v", err)
	}

	if len(skills) != 2 {
		t.Fatalf("got %d skills, want 2", len(skills))
	}

	if got := []string{skills[0].Name, skills[1].Name}; !reflect.DeepEqual(got, []string{"alpha", "zeta"}) {
		t.Fatalf("got skill order %v, want [alpha zeta]", got)
	}

	alpha := skills[0]
	if alpha.Content != "# Alpha\n" {
		t.Fatalf("got alpha content %q", alpha.Content)
	}

	gotFiles := make([]string, 0, len(alpha.Files))
	for _, file := range alpha.Files {
		gotFiles = append(gotFiles, file.Path)
	}
	wantFiles := []string{"docs/a.txt", "docs/guide.md"}
	if !reflect.DeepEqual(gotFiles, wantFiles) {
		t.Fatalf("got alpha files %v, want %v", gotFiles, wantFiles)
	}

	for _, skill := range skills {
		if skill.Hash == "" {
			t.Fatalf("skill %q hash should not be empty", skill.Name)
		}
	}
}

func TestScanLocalSkillsManifestHashStable(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "scanner", "nested", "b.md"), "B")
	writeTestFile(t, filepath.Join(root, "scanner", "SKILL.md"), "# Skill\n")
	writeTestFile(t, filepath.Join(root, "scanner", "nested", "a.md"), "A")

	first, err := ScanLocalSkills(root)
	if err != nil {
		t.Fatalf("first ScanLocalSkills returned error: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("got %d skills, want 1", len(first))
	}

	writeTestFile(t, filepath.Join(root, "scanner", ".DS_Store"), "ignored")
	writeTestFile(t, filepath.Join(root, "scanner", "ignored.bin"), string([]byte{'x', 0x00, 'y'}))

	second, err := ScanLocalSkills(root)
	if err != nil {
		t.Fatalf("second ScanLocalSkills returned error: %v", err)
	}
	if len(second) != 1 {
		t.Fatalf("got %d skills, want 1", len(second))
	}

	if first[0].Hash != second[0].Hash {
		t.Fatalf("hash changed after adding ignored files: %s != %s", first[0].Hash, second[0].Hash)
	}

	if !reflect.DeepEqual(first[0].Files, second[0].Files) {
		t.Fatalf("files changed after adding ignored files: %#v != %#v", first[0].Files, second[0].Files)
	}
}

func TestScanLocalSkillsIgnoresGeneratedDirectories(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "scanner", "SKILL.md"), "# Skill\n")
	writeTestFile(t, filepath.Join(root, "scanner", "docs", "guide.md"), "guide")
	writeTestFile(t, filepath.Join(root, "scanner", "node_modules", "left-pad", "index.js"), "module.exports = 1;")
	writeTestFile(t, filepath.Join(root, "scanner", "dist", "bundle.js"), "bundle")
	writeTestFile(t, filepath.Join(root, "scanner", "build", "artifact.txt"), "artifact")
	writeTestFile(t, filepath.Join(root, "scanner", ".next", "server.js"), "next")
	writeTestFile(t, filepath.Join(root, "scanner", "coverage", "index.html"), "coverage")

	skills, err := ScanLocalSkills(root)
	if err != nil {
		t.Fatalf("ScanLocalSkills returned error: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("got %d skills, want 1", len(skills))
	}

	gotFiles := make([]string, 0, len(skills[0].Files))
	for _, file := range skills[0].Files {
		gotFiles = append(gotFiles, file.Path)
	}
	wantFiles := []string{"docs/guide.md"}
	if !reflect.DeepEqual(gotFiles, wantFiles) {
		t.Fatalf("got files %v, want %v", gotFiles, wantFiles)
	}
}

func TestScanLocalSkillsRejectsBinaryLikeUTF8Files(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "scanner", "SKILL.md"), "# Skill\n")
	writeTestFile(t, filepath.Join(root, "scanner", "notes.txt"), "plain text")
	writeTestFile(t, filepath.Join(root, "scanner", "binary-ish.dat"), string([]byte{0x01, 0x02, 0x03, 'A', 'B'}))

	skills, err := ScanLocalSkills(root)
	if err != nil {
		t.Fatalf("ScanLocalSkills returned error: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("got %d skills, want 1", len(skills))
	}

	gotFiles := make([]string, 0, len(skills[0].Files))
	for _, file := range skills[0].Files {
		gotFiles = append(gotFiles, file.Path)
	}
	wantFiles := []string{"notes.txt"}
	if !reflect.DeepEqual(gotFiles, wantFiles) {
		t.Fatalf("got files %v, want %v", gotFiles, wantFiles)
	}
}

func TestReconcileWorkspaceSkillsCreatesLocalOnlySkill(t *testing.T) {
	t.Parallel()

	client := &fakeWorkspaceSkillService{}
	req := WorkspaceSkillSyncRequest{
		WorkspaceID: "ws-1",
		SyncDir:     "/skills",
		DaemonID:    "daemon-1",
		Profile:     "default",
		LocalSkills: []ScannedSkill{
			testScannedSkill("alpha", "hash-alpha"),
		},
	}

	result, err := ReconcileWorkspaceSkills(context.Background(), client, req)
	if err != nil {
		t.Fatalf("ReconcileWorkspaceSkills returned error: %v", err)
	}

	if got, want := result.Created, []string{"alpha"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("created = %v, want %v", got, want)
	}
	if len(client.created) != 1 || client.created[0].Name != "alpha" {
		t.Fatalf("unexpected create calls: %#v", client.created)
	}
	if client.created[0].Config != buildDaemonSkillSyncConfig(req, "hash-alpha") {
		t.Fatalf("unexpected config: %#v", client.created[0].Config)
	}
}

func TestReconcileWorkspaceSkillsUpdatesChangedManagedSkill(t *testing.T) {
	t.Parallel()

	client := &fakeWorkspaceSkillService{
		remote: []WorkspaceSkill{
			testRemoteManagedSkill("skill-1", "alpha", "/skills", "old-hash", "default"),
		},
	}
	req := WorkspaceSkillSyncRequest{
		WorkspaceID: "ws-1",
		SyncDir:     "/skills",
		DaemonID:    "daemon-1",
		Profile:     "default",
		LocalSkills: []ScannedSkill{
			testScannedSkill("alpha", "new-hash"),
		},
	}

	result, err := ReconcileWorkspaceSkills(context.Background(), client, req)
	if err != nil {
		t.Fatalf("ReconcileWorkspaceSkills returned error: %v", err)
	}

	if got, want := result.Updated, []string{"alpha"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("updated = %v, want %v", got, want)
	}
	if len(client.updated) != 1 || client.updated[0].skillID != "skill-1" {
		t.Fatalf("unexpected update calls: %#v", client.updated)
	}
	if got := client.updated[0].req.Files; !reflect.DeepEqual(got, buildWorkspaceSkillFiles(req.LocalSkills[0].Files)) {
		t.Fatalf("updated files = %#v", got)
	}
}

func TestReconcileWorkspaceSkillsLeavesSameHashManagedSkillUnchanged(t *testing.T) {
	t.Parallel()

	client := &fakeWorkspaceSkillService{
		remote: []WorkspaceSkill{
			testRemoteManagedSkill("skill-1", "alpha", "/skills", "hash-alpha", "default"),
		},
	}
	req := WorkspaceSkillSyncRequest{
		WorkspaceID: "ws-1",
		SyncDir:     "/skills",
		DaemonID:    "daemon-1",
		Profile:     "default",
		LocalSkills: []ScannedSkill{
			testScannedSkill("alpha", "hash-alpha"),
		},
	}

	result, err := ReconcileWorkspaceSkills(context.Background(), client, req)
	if err != nil {
		t.Fatalf("ReconcileWorkspaceSkills returned error: %v", err)
	}

	if got, want := result.Unchanged, []string{"alpha"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unchanged = %v, want %v", got, want)
	}
	if len(client.updated) != 0 {
		t.Fatalf("expected no update calls, got %#v", client.updated)
	}
}

func TestReconcileWorkspaceSkillsDeletesMissingManagedSkillWhenEnabled(t *testing.T) {
	t.Parallel()

	client := &fakeWorkspaceSkillService{
		remote: []WorkspaceSkill{
			testRemoteManagedSkill("skill-1", "alpha", "/skills", "hash-alpha", "default"),
		},
	}
	req := WorkspaceSkillSyncRequest{
		WorkspaceID:   "ws-1",
		SyncDir:       "/skills",
		DaemonID:      "daemon-1",
		Profile:       "default",
		DeleteManaged: true,
	}

	result, err := ReconcileWorkspaceSkills(context.Background(), client, req)
	if err != nil {
		t.Fatalf("ReconcileWorkspaceSkills returned error: %v", err)
	}

	if got, want := result.Deleted, []string{"alpha"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("deleted = %v, want %v", got, want)
	}
	if got, want := client.deleted, []string{"skill-1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("delete calls = %v, want %v", got, want)
	}
}

func TestReconcileWorkspaceSkillsPreservesMissingManagedSkillWhenDeleteDisabled(t *testing.T) {
	t.Parallel()

	client := &fakeWorkspaceSkillService{
		remote: []WorkspaceSkill{
			testRemoteManagedSkill("skill-1", "alpha", "/skills", "hash-alpha", "default"),
		},
	}
	req := WorkspaceSkillSyncRequest{
		WorkspaceID:   "ws-1",
		SyncDir:       "/skills",
		DaemonID:      "daemon-1",
		Profile:       "default",
		DeleteManaged: false,
	}

	result, err := ReconcileWorkspaceSkills(context.Background(), client, req)
	if err != nil {
		t.Fatalf("ReconcileWorkspaceSkills returned error: %v", err)
	}

	if got, want := result.Unchanged, []string{"alpha"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unchanged = %v, want %v", got, want)
	}
	if len(client.deleted) != 0 {
		t.Fatalf("expected no delete calls, got %#v", client.deleted)
	}
}

func TestReconcileWorkspaceSkillsDoesNotDeleteManagedSkillFromDifferentProfileWhenLocalProfileEmpty(t *testing.T) {
	t.Parallel()

	client := &fakeWorkspaceSkillService{
		remote: []WorkspaceSkill{
			testRemoteManagedSkill("skill-1", "alpha", "/skills", "hash-alpha", "team-a"),
		},
	}
	req := WorkspaceSkillSyncRequest{
		WorkspaceID:   "ws-1",
		SyncDir:       "/skills",
		DaemonID:      "daemon-1",
		Profile:       "",
		DeleteManaged: true,
	}

	result, err := ReconcileWorkspaceSkills(context.Background(), client, req)
	if err != nil {
		t.Fatalf("ReconcileWorkspaceSkills returned error: %v", err)
	}

	if len(result.Deleted) != 0 {
		t.Fatalf("expected no deleted skills, got %v", result.Deleted)
	}
	if len(client.deleted) != 0 {
		t.Fatalf("expected no delete calls, got %#v", client.deleted)
	}
}

func TestReconcileWorkspaceSkillsPreservesUnmanagedRemoteSkill(t *testing.T) {
	t.Parallel()

	client := &fakeWorkspaceSkillService{
		remote: []WorkspaceSkill{
			{ID: "skill-1", Name: "manual", Content: "# manual", Config: map[string]any{}},
		},
	}
	req := WorkspaceSkillSyncRequest{
		WorkspaceID:   "ws-1",
		SyncDir:       "/skills",
		DaemonID:      "daemon-1",
		Profile:       "default",
		DeleteManaged: true,
	}

	result, err := ReconcileWorkspaceSkills(context.Background(), client, req)
	if err != nil {
		t.Fatalf("ReconcileWorkspaceSkills returned error: %v", err)
	}

	if len(result.Deleted) != 0 {
		t.Fatalf("expected no deleted skills, got %v", result.Deleted)
	}
	if len(client.deleted) != 0 {
		t.Fatalf("expected no delete calls, got %#v", client.deleted)
	}
}

func TestReconcileWorkspaceSkillsDoesNotOverwriteUnmanagedNameCollision(t *testing.T) {
	t.Parallel()

	client := &fakeWorkspaceSkillService{
		remote: []WorkspaceSkill{
			{ID: "skill-1", Name: "alpha", Content: "# manual", Config: map[string]any{}},
		},
	}
	req := WorkspaceSkillSyncRequest{
		WorkspaceID: "ws-1",
		SyncDir:     "/skills",
		DaemonID:    "daemon-1",
		Profile:     "default",
		LocalSkills: []ScannedSkill{
			testScannedSkill("alpha", "hash-alpha"),
		},
	}

	result, err := ReconcileWorkspaceSkills(context.Background(), client, req)
	if err != nil {
		t.Fatalf("ReconcileWorkspaceSkills returned error: %v", err)
	}

	if got, want := result.Conflicts, []string{"alpha"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("conflicts = %v, want %v", got, want)
	}
	if len(client.updated) != 0 || len(client.created) != 0 {
		t.Fatalf("expected no create/update calls, got created=%#v updated=%#v", client.created, client.updated)
	}
}

func TestReconcileWorkspaceSkillsTreatsDifferentProfileNameCollisionAsConflict(t *testing.T) {
	t.Parallel()

	client := &fakeWorkspaceSkillService{
		remote: []WorkspaceSkill{
			testRemoteManagedSkill("skill-1", "alpha", "/skills", "old-hash", "team-a"),
		},
	}
	req := WorkspaceSkillSyncRequest{
		WorkspaceID: "ws-1",
		SyncDir:     "/skills",
		DaemonID:    "daemon-1",
		Profile:     "",
		LocalSkills: []ScannedSkill{
			testScannedSkill("alpha", "new-hash"),
		},
	}

	result, err := ReconcileWorkspaceSkills(context.Background(), client, req)
	if err != nil {
		t.Fatalf("ReconcileWorkspaceSkills returned error: %v", err)
	}

	if got, want := result.Conflicts, []string{"alpha"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("conflicts = %v, want %v", got, want)
	}
	if len(client.updated) != 0 || len(client.created) != 0 {
		t.Fatalf("expected no create/update calls, got created=%#v updated=%#v", client.created, client.updated)
	}
}

func TestDaemonSkillSyncCreatesSkillForEnabledWorkspace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	skillsDir := filepath.Join(home, "skills")
	writeTestFile(t, filepath.Join(skillsDir, "alpha", "SKILL.md"), "# Alpha\n")
	writeTestFile(t, filepath.Join(skillsDir, "alpha", "notes.txt"), "local notes")

	cfg := cli.CLIConfig{
		WatchedWorkspaces: []cli.WatchedWorkspace{
			{
				ID:   "ws-1",
				Name: "Workspace 1",
				SkillSync: &cli.WorkspaceSkillSync{
					Dir:     skillsDir,
					Enabled: true,
				},
			},
		},
	}
	if err := cli.SaveCLIConfigForProfile(cfg, ""); err != nil {
		t.Fatalf("SaveCLIConfigForProfile: %v", err)
	}

	var getCalls int
	var postCalls int
	var created CreateWorkspaceSkillRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("Authorization header = %q, want Bearer test-token", got)
		}
		if got := r.Header.Get("X-Workspace-ID"); got != "ws-1" {
			t.Fatalf("X-Workspace-ID = %q, want ws-1", got)
		}

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/skills":
			getCalls++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/skills":
			postCalls++
			defer r.Body.Close()
			if err := json.NewDecoder(r.Body).Decode(&created); err != nil {
				t.Fatalf("decode create request: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"skill-1","name":"alpha","content":"# Alpha\n","config":{},"files":[]}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	d := New(Config{
		ServerBaseURL:     server.URL,
		DaemonID:          "daemon-1",
		Profile:           "",
		WorkspacesRoot:    home,
		SkillSyncInterval: DefaultSkillSyncInterval,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	d.client.SetToken("test-token")

	d.syncWorkspaceSkills(context.Background())

	if getCalls != 1 {
		t.Fatalf("GET /api/skills calls = %d, want 1", getCalls)
	}
	if postCalls != 1 {
		t.Fatalf("POST /api/skills calls = %d, want 1", postCalls)
	}
	if created.Name != "alpha" {
		t.Fatalf("created name = %q, want alpha", created.Name)
	}
	if created.Content != "# Alpha\n" {
		t.Fatalf("created content = %q", created.Content)
	}
	if len(created.Files) != 1 || created.Files[0].Path != "notes.txt" {
		t.Fatalf("created files = %#v", created.Files)
	}

	configMap, ok := created.Config.(map[string]any)
	if !ok {
		t.Fatalf("created config type = %T, want map[string]any", created.Config)
	}
	if got := configMap["managed_by"]; got != daemonSkillSyncManagedBy {
		t.Fatalf("managed_by = %#v, want %q", got, daemonSkillSyncManagedBy)
	}

	reloaded, err := cli.LoadCLIConfigForProfile("")
	if err != nil {
		t.Fatalf("LoadCLIConfigForProfile: %v", err)
	}
	if len(reloaded.WatchedWorkspaces) != 1 || reloaded.WatchedWorkspaces[0].SkillSync == nil {
		t.Fatalf("unexpected reloaded config: %+v", reloaded.WatchedWorkspaces)
	}
	if reloaded.WatchedWorkspaces[0].SkillSync.LastSyncAt == "" {
		t.Fatal("expected last_sync_at to be persisted on success")
	}
	if reloaded.WatchedWorkspaces[0].SkillSync.LastSyncError != "" {
		t.Fatalf("expected cleared last_sync_error, got %q", reloaded.WatchedWorkspaces[0].SkillSync.LastSyncError)
	}
}

func TestDaemonSkillSyncContinuesAfterWorkspaceScanError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	goodDir := filepath.Join(home, "good-skills")
	writeTestFile(t, filepath.Join(goodDir, "alpha", "SKILL.md"), "# Alpha\n")

	cfg := cli.CLIConfig{
		WatchedWorkspaces: []cli.WatchedWorkspace{
			{
				ID:   "ws-bad",
				Name: "Bad Workspace",
				SkillSync: &cli.WorkspaceSkillSync{
					Dir:     filepath.Join(home, "missing-dir"),
					Enabled: true,
				},
			},
			{
				ID:   "ws-good",
				Name: "Good Workspace",
				SkillSync: &cli.WorkspaceSkillSync{
					Dir:     goodDir,
					Enabled: true,
				},
			},
		},
	}
	if err := cli.SaveCLIConfigForProfile(cfg, ""); err != nil {
		t.Fatalf("SaveCLIConfigForProfile: %v", err)
	}

	var createdWorkspaceIDs []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("Authorization header = %q, want Bearer test-token", got)
		}

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/skills":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/skills":
			createdWorkspaceIDs = append(createdWorkspaceIDs, r.Header.Get("X-Workspace-ID"))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"skill-1","name":"alpha","content":"# Alpha\n","config":{},"files":[]}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	d := New(Config{
		ServerBaseURL:     server.URL,
		DaemonID:          "daemon-1",
		Profile:           "",
		WorkspacesRoot:    home,
		SkillSyncInterval: DefaultSkillSyncInterval,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	d.client.SetToken("test-token")

	d.syncWorkspaceSkills(context.Background())

	if got, want := createdWorkspaceIDs, []string{"ws-good"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("created workspace IDs = %v, want %v", got, want)
	}

	reloaded, err := cli.LoadCLIConfigForProfile("")
	if err != nil {
		t.Fatalf("LoadCLIConfigForProfile: %v", err)
	}
	if len(reloaded.WatchedWorkspaces) != 2 {
		t.Fatalf("expected 2 watched workspaces, got %d", len(reloaded.WatchedWorkspaces))
	}

	stateByID := make(map[string]*cli.WorkspaceSkillSync, len(reloaded.WatchedWorkspaces))
	for _, ws := range reloaded.WatchedWorkspaces {
		stateByID[ws.ID] = ws.SkillSync
	}
	if stateByID["ws-bad"] == nil {
		t.Fatal("expected ws-bad skill sync state")
	}
	if stateByID["ws-bad"].LastSyncError == "" {
		t.Fatal("expected ws-bad last_sync_error to be persisted")
	}
	if stateByID["ws-bad"].LastSyncAt != "" {
		t.Fatalf("expected ws-bad last_sync_at to remain empty, got %q", stateByID["ws-bad"].LastSyncAt)
	}
	if stateByID["ws-good"] == nil {
		t.Fatal("expected ws-good skill sync state")
	}
	if stateByID["ws-good"].LastSyncAt == "" {
		t.Fatal("expected ws-good last_sync_at to be persisted")
	}
	if stateByID["ws-good"].LastSyncError != "" {
		t.Fatalf("expected ws-good last_sync_error to be cleared, got %q", stateByID["ws-good"].LastSyncError)
	}
}

type fakeWorkspaceSkillService struct {
	remote  []WorkspaceSkill
	created []CreateWorkspaceSkillRequest
	updated []fakeWorkspaceSkillUpdate
	deleted []string
}

type fakeWorkspaceSkillUpdate struct {
	skillID string
	req     UpdateWorkspaceSkillRequest
}

func (f *fakeWorkspaceSkillService) ListWorkspaceSkills(_ context.Context, _ string) ([]WorkspaceSkill, error) {
	return append([]WorkspaceSkill(nil), f.remote...), nil
}

func (f *fakeWorkspaceSkillService) CreateWorkspaceSkill(_ context.Context, _ string, req CreateWorkspaceSkillRequest) (*WorkspaceSkill, error) {
	f.created = append(f.created, req)
	return &WorkspaceSkill{ID: "created", Name: req.Name, Content: req.Content, Config: req.Config, Files: req.Files}, nil
}

func (f *fakeWorkspaceSkillService) UpdateWorkspaceSkill(_ context.Context, _ string, skillID string, req UpdateWorkspaceSkillRequest) (*WorkspaceSkill, error) {
	f.updated = append(f.updated, fakeWorkspaceSkillUpdate{skillID: skillID, req: req})
	return &WorkspaceSkill{ID: skillID, Config: req.Config, Files: req.Files}, nil
}

func (f *fakeWorkspaceSkillService) DeleteWorkspaceSkill(_ context.Context, _ string, skillID string) error {
	f.deleted = append(f.deleted, skillID)
	return nil
}

func testScannedSkill(name, hash string) ScannedSkill {
	return ScannedSkill{
		Name:    name,
		Content: "# " + name,
		Hash:    hash,
		Files: []ScannedSkillFile{
			{Path: "notes.txt", Content: "content for " + name},
		},
	}
}

func testRemoteManagedSkill(id, name, dir, hash, profile string) WorkspaceSkill {
	return WorkspaceSkill{
		ID:      id,
		Name:    name,
		Content: "# " + name,
		Config: map[string]any{
			"managed_by": daemonSkillSyncManagedBy,
			"workspace_skill_sync": map[string]any{
				"dir":       dir,
				"hash":      hash,
				"daemon_id": "other-daemon",
				"profile":   profile,
			},
		},
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}
