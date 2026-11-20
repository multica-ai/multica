package execenv

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
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
	if got := linkCursorUserMcpAuth(userData, firstRoot, firstWork, "", store, nil); got != userData {
		t.Fatalf("link first task = %q, want %q", got, userData)
	}
	writeTestFile(t, CursorMcpAuthPath(userData, firstWork), testCursorChallenge)
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

	writeTestFile(t, CursorMcpAuthPath(userData, firstWork), testCursorLogin)
	if got := CursorMcpServersNeedingLogin(userData, secondWork); got != nil {
		t.Fatalf("next task after login = %v, want nothing to report", got)
	}
	if got := readFile(t, store); got != testCursorLogin {
		t.Fatalf("store = %s, want the login", got)
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
	worktree := filepath.Join(envRoot, "workdir")
	gitRun(t, repo, "worktree", "add", "-b", "agent/task-1", worktree)

	if got := linkCursorUserMcpAuth(userData, envRoot, worktree, "", store, nil); got != userData {
		t.Fatalf("worktree link = %q, want %q", got, userData)
	}
	if got, err := os.Readlink(CursorMcpAuthPath(userData, worktree)); err != nil || got != store {
		t.Fatalf("worktree project auth links to %q (err %v), want the store", got, err)
	}
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
	if got := linkCursorUserMcpAuth(userData, envRoot, workDir, "", store, nil); got != userData {
		t.Fatalf("link = %q, want %q", got, userData)
	}
	writeTestFile(t, CursorMcpAuthPath(userData, workDir), testCursorLogin)
	if err := removeCursorMcpAuthLink(envRoot, userData, storeRoot); err != nil {
		t.Fatalf("removeCursorMcpAuthLink: %v", err)
	}
	if _, err := os.Lstat(CursorMcpAuthPath(userData, workDir)); !os.IsNotExist(err) {
		t.Fatalf("task link should be removed, lstat err=%v", err)
	}
	if got := readFile(t, store); got != testCursorLogin {
		t.Fatalf("store = %s, want it kept", got)
	}

	// A hard link to the store (the Windows fallback) is removed too.
	hardRoot, hardWork := newCursorUserTaskRoot(t, root, "task-2")
	hardLink := CursorMcpAuthPath(userData, hardWork)
	if err := os.MkdirAll(filepath.Dir(hardLink), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Link(store, hardLink); err != nil {
		t.Fatalf("hard link: %v", err)
	}
	if err := writeCursorMcpAuthLinkRecord(hardRoot, hardLink, store); err != nil {
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
	if err := writeCursorMcpAuthLinkRecord(tamperedRoot, userAuth, userAuth); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := removeCursorMcpAuthLink(tamperedRoot, userData, storeRoot); err != nil {
		t.Fatalf("removeCursorMcpAuthLink tampered: %v", err)
	}
	if readFile(t, userAuth) != `{"mine":true}` {
		t.Fatal("user's own login was removed")
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
	writeTestFile(t, CursorMcpAuthPath(userData, loginWork), testCursorLogin)

	envRoot, workDir := newCursorUserTaskRoot(t, root, "task-1")
	if got := linkCursorUserMcpAuth(userData, envRoot, workDir, "", store, nil); got != userData {
		t.Fatalf("link = %q, want %q", got, userData)
	}
	if got := CursorMcpServersNeedingLogin(userData, workDir); got != nil {
		t.Fatalf("task after login = %v, want nothing to report", got)
	}
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
	if got, err := os.Readlink(CursorMcpAuthPath(userData, env.WorkDir)); err != nil || got != store {
		t.Fatalf("task project auth links to %q (err %v), want the store", got, err)
	}

	inPlace := prepare("bbbbbbbb-bbbb-cccc-dddd-ffffffffffff", newTestRepo(t))
	if inPlace.CursorUserDataDir != "" {
		t.Fatalf("in-place CursorUserDataDir = %q, want empty", inPlace.CursorUserDataDir)
	}
}
