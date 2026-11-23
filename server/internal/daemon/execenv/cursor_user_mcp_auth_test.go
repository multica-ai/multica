package execenv

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

const (
	testCursorChallenge = `{"mobbin":{"clientInfo":{"client_id":"c"}}}`
	testCursorLogin     = `{"mobbin":{"clientInfo":{"client_id":"c"},"tokens":{"access_token":"a","refresh_token":"r"}}}`
)

func newCursorUserTaskRoot(t *testing.T, parent, name string) (string, string) {
	t.Helper()
	envRoot := filepath.Join(parent, name)
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", name, err)
	}
	return envRoot, workDir
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestCursorUserMcpAuthLoginCarriesAcrossTasks(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	userData := filepath.Join(root, "home", ".cursor")
	store := filepath.Join(root, "profile", cursorMcpAuthStoreRoot, "agent-1", cursorMcpAuthFile)

	firstRoot, firstWork := newCursorUserTaskRoot(t, root, "task-1")
	got, firstMirror := linkCursorUserMcpAuthMirror(userData, firstRoot, firstWork, "", store, nil)
	if got != userData || firstMirror == nil {
		t.Fatalf("link first task = %q, %v; want %q and a mirror", got, firstMirror, userData)
	}
	cursorSave(t, CursorMcpAuthPath(userData, firstWork), testCursorChallenge)
	if err := firstMirror.Sync(); err != nil {
		t.Fatalf("sync challenge: %v", err)
	}
	if got := CursorMcpServersNeedingLogin(userData, firstWork); !reflect.DeepEqual(got, []string{"mobbin"}) {
		t.Fatalf("after challenge = %v, want [mobbin]", got)
	}

	secondRoot, secondWork := newCursorUserTaskRoot(t, root, "task-2")
	if got := linkCursorUserMcpAuth(userData, secondRoot, secondWork, "", store, nil); got != userData {
		t.Fatalf("link second task = %q, want %q", got, userData)
	}
	if got := CursorMcpServersNeedingLogin(userData, secondWork); !reflect.DeepEqual(got, []string{"mobbin"}) {
		t.Fatalf("next task before login = %v, want [mobbin]", got)
	}

	cursorSave(t, CursorMcpAuthPath(userData, firstWork), testCursorLogin)
	if err := firstMirror.Sync(); err != nil {
		t.Fatalf("sync login: %v", err)
	}
	assertCursorAuthJSON(t, store, testCursorLogin)
	if got := linkCursorUserMcpAuth(userData, secondRoot, secondWork, "", store, nil); got != userData {
		t.Fatalf("relink second task = %q, want %q", got, userData)
	}
	if got := CursorMcpServersNeedingLogin(userData, secondWork); got != nil {
		t.Fatalf("next task after login = %v, want nothing to report", got)
	}
}

func TestCursorUserMcpAuthLeavesUserProjectsAlone(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	userData := filepath.Join(root, "home", ".cursor")
	store := filepath.Join(root, "profile", cursorMcpAuthStoreRoot, "agent-1", cursorMcpAuthFile)
	userLogin := `{"mobbin":{"tokens":{"access_token":"mine"}}}`

	// In place: the task runs in the user's own repository.
	userRepo := newTestRepo(t)
	userAuth := CursorMcpAuthPath(userData, userRepo)
	writeTestFile(t, userAuth, userLogin)
	envRoot, _ := newCursorUserTaskRoot(t, root, "in-place")
	if got := linkCursorUserMcpAuth(userData, envRoot, userRepo, "", store, nil); got != "" {
		t.Fatalf("in-place link = %q, want none", got)
	}
	if info, err := os.Lstat(userAuth); err != nil || !info.Mode().IsRegular() || readFile(t, userAuth) != userLogin {
		t.Fatalf("user's own login was touched: info=%v err=%v", info, err)
	}

	// A git repository enclosing the task, like a dotfiles repo at $HOME.
	enclosing := newTestRepo(t)
	nestedRoot, nestedWork := newCursorUserTaskRoot(t, filepath.Join(enclosing, "multica_workspaces"), "task-1")
	enclosingAuth := CursorMcpAuthPath(userData, nestedWork)
	writeTestFile(t, enclosingAuth, userLogin)
	if got := linkCursorUserMcpAuth(userData, nestedRoot, nestedWork, "", store, nil); got != "" {
		t.Fatalf("enclosed task link = %q, want none", got)
	}
	if readFile(t, enclosingAuth) != userLogin {
		t.Fatal("enclosing repository's login was touched")
	}
	if _, err := os.Stat(filepath.Join(nestedRoot, cursorMcpAuthLinkRecordFile)); !os.IsNotExist(err) {
		t.Fatalf("skipped task should record no link, stat err=%v", err)
	}
	if _, err := os.Stat(store); !os.IsNotExist(err) {
		t.Fatalf("skipped tasks should not create the store, stat err=%v", err)
	}
}

func TestCursorUserMcpAuthLinksWorktreeTask(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	userData := filepath.Join(root, "home", ".cursor")
	store := filepath.Join(root, "profile", cursorMcpAuthStoreRoot, "agent-1", cursorMcpAuthFile)
	repo := newTestRepo(t)
	envRoot := filepath.Join(root, "task-1")
	worktree := filepath.Join(envRoot, "worktree")
	gitRun(t, repo, "worktree", "add", "-b", "agent/task-1", worktree)

	writeTestFile(t, store, testCursorLogin)
	if got := linkCursorUserMcpAuth(userData, envRoot, worktree, "", store, nil); got != userData {
		t.Fatalf("worktree link = %q, want %q", got, userData)
	}
	taskAuth := CursorMcpAuthPath(userData, worktree)
	assertCursorUserTaskCopy(t, taskAuth, testCursorLogin)

	// The worktree is removed at finalize, before the GC removes the env root.
	gitRun(t, repo, "worktree", "remove", "--force", worktree)
	if err := removeCursorMcpAuthLink(envRoot, userData, filepath.Dir(filepath.Dir(store))); err != nil {
		t.Fatalf("removeCursorMcpAuthLink: %v", err)
	}
	if _, err := os.Lstat(taskAuth); !os.IsNotExist(err) {
		t.Fatalf("worktree task's login copy should be removed, lstat err=%v", err)
	}
	assertCursorAuthJSON(t, store, testCursorLogin)
}

func assertCursorUserTaskCopy(t *testing.T, path, want string) {
	t.Helper()
	if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("task project auth = %v, %v; want a copy of the store", info, err)
	}
	assertCursorAuthJSON(t, path, want)
}

func TestCursorUserMcpAuthAdoptsExistingTaskLogin(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	userData := filepath.Join(root, "home", ".cursor")
	store := filepath.Join(root, "profile", cursorMcpAuthStoreRoot, "agent-1", cursorMcpAuthFile)

	// A login Cursor saved in this task's own project before it was linked.
	firstRoot, firstWork := newCursorUserTaskRoot(t, root, "task-1")
	writeTestFile(t, CursorMcpAuthPath(userData, firstWork), testCursorLogin)
	if got := linkCursorUserMcpAuth(userData, firstRoot, firstWork, "", store, nil); got != userData {
		t.Fatalf("link = %q, want %q", got, userData)
	}
	if got := readFile(t, store); got != testCursorLogin {
		t.Fatalf("store = %s, want the adopted login", got)
	}

	// Once the store holds a login, a task's own file no longer replaces it.
	secondRoot, secondWork := newCursorUserTaskRoot(t, root, "task-2")
	writeTestFile(t, CursorMcpAuthPath(userData, secondWork), `{"mobbin":{"tokens":{"access_token":"stale"}}}`)
	if got := linkCursorUserMcpAuth(userData, secondRoot, secondWork, "", store, nil); got != userData {
		t.Fatalf("link = %q, want %q", got, userData)
	}
	if got := readFile(t, store); got != testCursorLogin {
		t.Fatalf("store = %s, want the agent's login kept", got)
	}
	if got := readFile(t, CursorMcpAuthPath(userData, secondWork)); got != testCursorLogin {
		t.Fatalf("second task auth = %s, want the agent's login", got)
	}
}

func TestRemoveCursorMcpAuthLinkRemovesOnlyTheTaskLink(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	userData := filepath.Join(root, "home", ".cursor")
	storeRoot := filepath.Join(root, "profile", cursorMcpAuthStoreRoot)
	store := filepath.Join(storeRoot, "agent-1", cursorMcpAuthFile)

	envRoot, workDir := newCursorUserTaskRoot(t, root, "task-1")
	got, mirror := linkCursorUserMcpAuthMirror(userData, envRoot, workDir, "", store, nil)
	if got != userData {
		t.Fatalf("link = %q, want %q", got, userData)
	}
	cursorSave(t, CursorMcpAuthPath(userData, workDir), testCursorLogin)
	if err := mirror.Sync(); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if err := removeCursorMcpAuthLink(envRoot, userData, storeRoot); err != nil {
		t.Fatalf("removeCursorMcpAuthLink: %v", err)
	}
	if _, err := os.Lstat(CursorMcpAuthPath(userData, workDir)); !os.IsNotExist(err) {
		t.Fatalf("task's login copy should be removed, lstat err=%v", err)
	}
	assertCursorAuthJSON(t, store, testCursorLogin)

	// A hard link to the store, left by a daemon that linked the login on
	// Windows, is removed too.
	hardRoot, hardWork := newCursorUserTaskRoot(t, root, "task-2")
	hardLink := CursorMcpAuthPath(userData, hardWork)
	if err := os.MkdirAll(filepath.Dir(hardLink), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Link(store, hardLink); err != nil {
		t.Fatalf("hard link: %v", err)
	}
	if err := writeCursorMcpAuthLinkRecord(hardRoot, hardLink, store, ""); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := removeCursorMcpAuthLink(hardRoot, userData, storeRoot); err != nil {
		t.Fatalf("removeCursorMcpAuthLink hard link: %v", err)
	}
	if _, err := os.Lstat(hardLink); !os.IsNotExist(err) {
		t.Fatalf("hard link should be removed, lstat err=%v", err)
	}

	// A record pointing at a file Cursor wrote for the user is ignored.
	userRepo := newTestRepo(t)
	userAuth := CursorMcpAuthPath(userData, userRepo)
	writeTestFile(t, userAuth, `{"mine":true}`)
	tamperedRoot, _ := newCursorUserTaskRoot(t, root, "task-3")
	for _, project := range []string{"", userRepo, "workdir"} {
		if err := writeCursorMcpAuthLinkRecord(tamperedRoot, userAuth, userAuth, project); err != nil {
			t.Fatalf("record: %v", err)
		}
		if err := removeCursorMcpAuthLink(tamperedRoot, userData, storeRoot); err != nil {
			t.Fatalf("removeCursorMcpAuthLink tampered with project %q: %v", project, err)
		}
		if readFile(t, userAuth) != `{"mine":true}` {
			t.Fatalf("user's own login was removed with recorded project %q", project)
		}
	}
}

// A user login workspace an older daemon set up holds a link to the store and
// no base. A login made there after the upgrade replaces the link, and must
// reach the store before the workspace is seeded again, although the store
// still holds the spent login.
func TestCursorMcpUserLoginWorkspaceFromOlderDaemonKeepsNewLogin(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	userData := filepath.Join(root, "home", ".cursor")
	store := filepath.Join(root, "profile", cursorMcpAuthStoreRoot, "agent-1", cursorMcpAuthFile)
	writeTestFile(t, store, `{"mobbin":{"clientInfo":{"client_id":"c"},"tokens":{"access_token":"old","refresh_token":"spent"}}}`)

	loginWork, err := PrepareCursorMcpUserLoginWorkspace(userData, "", store)
	if err != nil {
		t.Fatalf("PrepareCursorMcpUserLoginWorkspace: %v", err)
	}
	makeCursorLoginWorkspaceLegacy(t, store, cursorMcpUserLoginWorkspaceDir)
	cursorSave(t, CursorMcpAuthPath(userData, loginWork), testCursorLogin)

	if _, err := PrepareCursorMcpUserLoginWorkspace(userData, "", store); err != nil {
		t.Fatalf("rebuild user login workspace: %v", err)
	}
	assertCursorAuthJSON(t, store, testCursorLogin)
	assertCursorUserTaskCopy(t, CursorMcpAuthPath(userData, loginWork), testCursorLogin)
}

// makeCursorLoginWorkspaceLegacy leaves an agent's login workspace the way an
// older daemon set it up, with no base, and its store written before any
// login made there.
func makeCursorLoginWorkspaceLegacy(t *testing.T, store, workspace string) {
	t.Helper()
	if err := os.Remove(filepath.Join(filepath.Dir(store), workspace, cursorMcpAuthBaseFile)); err != nil {
		t.Fatalf("remove base: %v", err)
	}
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(store, past, past); err != nil {
		t.Fatalf("age store: %v", err)
	}
}

func TestCursorMcpUserLoginWorkspaceLogsInForNextTask(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	userData := filepath.Join(root, "home", ".cursor")
	store := filepath.Join(root, "profile", cursorMcpAuthStoreRoot, "agent-1", cursorMcpAuthFile)

	loginWork, err := PrepareCursorMcpUserLoginWorkspace(userData, "", store)
	if err != nil {
		t.Fatalf("PrepareCursorMcpUserLoginWorkspace: %v", err)
	}
	if _, err := os.Stat(filepath.Join(loginWork, ".cursor")); !os.IsNotExist(err) {
		t.Fatalf("user login workspace should carry no project config, stat err=%v", err)
	}
	cursorSave(t, CursorMcpAuthPath(userData, loginWork), testCursorLogin)

	envRoot, workDir := newCursorUserTaskRoot(t, root, "task-1")
	env := &Environment{}
	if err := prepareCursorMcp(env, envRoot, workDir, nil, "", store, userData, nil, nil); err != nil {
		t.Fatalf("prepareCursorMcp: %v", err)
	}
	if env.CursorUserDataDir != userData {
		t.Fatalf("CursorUserDataDir = %q, want %q", env.CursorUserDataDir, userData)
	}
	if got := CursorMcpServersNeedingLogin(userData, workDir); got != nil {
		t.Fatalf("task after login = %v, want nothing to report", got)
	}
	assertCursorAuthJSON(t, store, testCursorLogin)
}

func TestPrepareCursorWithoutMcpConfigLinksUserDataDir(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	userData := filepath.Join(root, "home", ".cursor")
	store := filepath.Join(root, "profile", cursorMcpAuthStoreRoot, "agent-1", cursorMcpAuthFile)
	prepare := func(taskID, localWorkDir string) *Environment {
		t.Helper()
		env, err := Prepare(PrepareParams{
			WorkspacesRoot:     filepath.Join(root, "workspaces"),
			WorkspaceID:        "ws-cursor-user",
			TaskID:             taskID,
			AgentName:          "Test Agent",
			Provider:           "cursor",
			LocalWorkDir:       localWorkDir,
			CursorMcpAuthStore: store,
			CursorUserDataDir:  userData,
			Task: TaskContextForEnv{
				IssueID: "11111111-2222-3333-4444-555555555555",
				AgentID: "99999999-8888-7777-6666-555555555555",
			},
		}, testLogger())
		if err != nil {
			t.Fatalf("Prepare: %v", err)
		}
		return env
	}

	env := prepare("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", "")
	if env.CursorDataDir != "" || env.CursorUserDataDir != userData {
		t.Fatalf("CursorDataDir=%q CursorUserDataDir=%q, want only the user data dir", env.CursorDataDir, env.CursorUserDataDir)
	}
	assertCursorUserTaskCopy(t, CursorMcpAuthPath(userData, env.WorkDir), "{}")
	if env.CursorMcpAuthMirror == nil {
		t.Fatal("CursorMcpAuthMirror = nil, want the task's login mirrored")
	}

	inPlace := prepare("bbbbbbbb-bbbb-cccc-dddd-ffffffffffff", newTestRepo(t))
	if inPlace.CursorUserDataDir != "" {
		t.Fatalf("in-place CursorUserDataDir = %q, want empty", inPlace.CursorUserDataDir)
	}
}
