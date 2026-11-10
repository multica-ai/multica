package localenv

import (
	"context"
	"strings"
	"testing"
)

func TestParseLsofListeners(t *testing.T) {
	out := "p4210\ncnode\nf23\nn*:5173\nf24\nn[::1]:5173\np4300\ncpostgres\nf5\nn127.0.0.1:5432\np99\ncweird\nf1\nnlocalhost:http\n"
	got := parseLsofListeners(out)
	if len(got) != 2 {
		t.Fatalf("listeners = %+v, want node:5173 once and postgres:5432", got)
	}
	if got[0] != (Listener{PID: 4210, Port: 5173, Command: "node"}) || got[1] != (Listener{PID: 4300, Port: 5432, Command: "postgres"}) {
		t.Fatalf("listeners = %+v", got)
	}
	cwds := parseLsofCwd("p4210\nfcwd\nn/Users/me/multica_workspaces/ws/abc/workdir/app\np4300\nfcwd\nn/\n")
	if cwds[4210] != "/Users/me/multica_workspaces/ws/abc/workdir/app" || cwds[4300] != "/" {
		t.Fatalf("cwds = %v", cwds)
	}
}

func TestParseProcNetTCP(t *testing.T) {
	out := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000:1435 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 31337 1 0000000000000000 100 0 0 10 0
   1: 0100007F:A028 0100007F:1435 01 00000000:00000000 00:00000000 00000000  1000        0 31338 1 0000000000000000 20 4 30 10 -1
`
	inodes := map[string]int{}
	parseProcNetTCP(out, inodes)
	if len(inodes) != 1 || inodes["31337"] != 5173 {
		t.Fatalf("inodes = %v, want only the listening socket on 5173", inodes)
	}
}

func TestDetectorReportsServersInTaskDirectories(t *testing.T) {
	listeners := []Listener{
		{PID: 1, Port: 5173, Command: "node", Cwd: "/ws/task-a/workdir/app"},
		{PID: 1, Port: 5173, Command: "node", Cwd: "/ws/task-a/workdir/app"},
		{PID: 2, Port: 9000, Command: "claude", Cwd: "/ws/task-a/workdir"},
		{PID: 3, Port: 3000, Command: "node", Cwd: "/Users/me/own-project"},
	}
	d := NewDetector([]string{"Claude"})
	d.list = func(context.Context) ([]Listener, error) { return listeners, nil }
	resolve := func(cwd string) (Task, bool) {
		if strings.HasPrefix(cwd, "/ws/task-a/") {
			return Task{TaskID: "task-a", IssueID: "issue-a", RootDir: "/ws/task-a"}, true
		}
		return Task{}, false
	}
	owned := func(int) bool { return false }

	first, err := d.Detect(context.Background(), resolve, owned)
	if err != nil || len(first) != 0 {
		t.Fatalf("first scan = %+v, %v; want nothing until a server is seen twice", first, err)
	}
	second, _ := d.Detect(context.Background(), resolve, owned)
	if len(second) != 1 {
		t.Fatalf("second scan = %+v, want the one server in the task directory", second)
	}
	env := second[0]
	if env.Source != SourceDetected || env.TaskID != "task-a" || env.URL != "http://localhost:5173" || env.Name != "node" || env.State != StateRunning {
		t.Fatalf("detected = %+v", env)
	}
	if strings.Contains(env.Key, "/ws/") || env.Key != detectedKey("/ws/task-a", 5173) {
		t.Fatalf("key %q leaks the path or is unstable", env.Key)
	}

	listeners = nil
	if gone, _ := d.Detect(context.Background(), resolve, owned); len(gone) != 0 {
		t.Fatalf("after the server stopped = %+v", gone)
	}
}
