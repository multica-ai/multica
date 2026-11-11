package localenv

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
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
	d := fakeDetector(&listeners, nil, nil)
	d.ignore = NewDetector([]string{"Claude"}).ignore

	first, err := d.Detect(context.Background(), resolveTaskA, notOwned)
	if err != nil || len(first) != 0 {
		t.Fatalf("first scan = %+v, %v; want nothing until a server is seen twice", first, err)
	}
	second, _ := d.Detect(context.Background(), resolveTaskA, notOwned)
	if len(second) != 1 {
		t.Fatalf("second scan = %+v, want the one server in the task directory", second)
	}
	env := second[0]
	if env.Source != SourceDetected || env.Kind != KindServer || env.TaskID != "task-a" || env.URL != "http://localhost:5173" || env.Name != "node" || env.State != StateRunning {
		t.Fatalf("detected = %+v", env)
	}
	if strings.Contains(env.Key, "/ws/") || env.Key != detectedKey("/ws/task-a", 5173) {
		t.Fatalf("key %q leaks the path or is unstable", env.Key)
	}

	listeners = nil
	if gone, _ := d.Detect(context.Background(), resolveTaskA, notOwned); len(gone) != 0 {
		t.Fatalf("after the server stopped = %+v", gone)
	}
}

func TestDetectorLeavesOutADeclaredEnvironmentsServersAndKeepsItsApp(t *testing.T) {
	listeners := []Listener{
		{PID: 10, Port: 9724, Command: "Electron", Cwd: "/ws/task-a/workdir/apps/desktop"},
		{PID: 11, Port: 5173, Command: "node", Cwd: "/ws/task-a/workdir/apps/desktop"},
		{PID: 12, Port: 8080, Command: "server", Cwd: "/ws/task-a/workdir/server"},
		{PID: 13, Port: 6006, Command: "node", Cwd: "/ws/task-a/workdir"},
	}
	procs := map[int]Process{
		10: {Args: electronBundle + "/Contents/MacOS/Electron . --remote-debugging-port=9724", App: "Multica Canary", Bundle: electronBundle},
	}
	owner := func(pid int) string {
		if pid == 13 {
			return ""
		}
		return "config:abc"
	}
	d := fakeDetector(&listeners, procs, nil)

	d.Detect(context.Background(), resolveTaskA, owner)
	envs, _ := d.Detect(context.Background(), resolveTaskA, owner)
	if len(envs) != 2 {
		t.Fatalf("detected = %+v, want the environment's app and the one server nobody declared", envs)
	}
	app, other := byKind(envs, KindApp), byKind(envs, KindServer)
	if app.Key != "config:abc" || app.Source != SourceConfig || app.pid != 10 || app.bundle != electronBundle {
		t.Fatalf("app = %+v, want it under the environment's key with the config source", app)
	}
	if other.Source != SourceDetected || other.Port != 6006 {
		t.Fatalf("other = %+v, want the server on 6006 as detected", other)
	}
}

const electronBundle = "/ws/task-a/workdir/node_modules/electron/dist/Electron.app"

func TestDetectorReportsADesktopAppInsteadOfItsDebugPort(t *testing.T) {
	listeners := []Listener{
		{PID: 10, Port: 9724, Command: "Electron", Cwd: "/ws/task-a/workdir/apps/desktop"},
		{PID: 11, Port: 5173, Command: "node", Cwd: "/ws/task-a/workdir/apps/desktop"},
	}
	procs := map[int]Process{
		10: {Args: electronBundle + "/Contents/MacOS/Electron . --remote-debugging-port=9724", App: "Multica Canary multica-501", Bundle: electronBundle},
		11: {Args: "node node_modules/.bin/electron-vite dev"},
	}
	probed := 0
	d := fakeDetector(&listeners, procs, func(int) bool { probed++; return false })

	d.Detect(context.Background(), resolveTaskA, notOwned)
	envs, _ := d.Detect(context.Background(), resolveTaskA, notOwned)
	if len(envs) != 2 {
		t.Fatalf("detected = %+v, want the app and the Vite server", envs)
	}
	app, vite := byKind(envs, KindApp), byKind(envs, KindServer)
	if app.Name != "Multica Canary multica-501" || app.URL != "" || app.Port != 0 || app.Detail != "Remote debugging on localhost:9724" {
		t.Fatalf("app = %+v, want the app's own name, no URL and the debug port as detail", app)
	}
	if app.pid != 10 || app.bundle != electronBundle {
		t.Fatalf("app pid/bundle = %d/%q, want the listening process", app.pid, app.bundle)
	}
	if strings.Contains(app.Key, "/ws/") || app.Key != detectedAppKey("/ws/task-a", procs[10]) {
		t.Fatalf("app key %q leaks the path or is unstable", app.Key)
	}
	if vite.URL != "http://localhost:5173" || vite.Name != "node" {
		t.Fatalf("an ordinary server next to the app = %+v, want it unchanged", vite)
	}
	if probed != 0 {
		t.Fatalf("probed %d ports; a debug port named in the arguments needs no probe", probed)
	}

	// The same app restarted on another debug port is still the same row.
	listeners[0] = Listener{PID: 20, Port: 9800, Command: "Electron", Cwd: "/ws/task-a/workdir/apps/desktop"}
	procs[20] = Process{Args: electronBundle + "/Contents/MacOS/Electron . --remote-debugging-port 9800", App: "Multica Canary multica-501", Bundle: electronBundle}
	envs, _ = d.Detect(context.Background(), resolveTaskA, notOwned)
	if restarted := byKind(envs, KindApp); restarted.Key != app.Key || restarted.pid != 20 || restarted.Detail != "Remote debugging on localhost:9800" {
		t.Fatalf("restarted app = %+v, want the same key with the new process", restarted)
	}
}

func TestDetectorFindsADebugPortTheAppOpenedItself(t *testing.T) {
	listeners := []Listener{
		{PID: 10, Port: 9222, Command: "Electron", Cwd: "/ws/task-a/workdir"},
		{PID: 10, Port: 3000, Command: "Electron", Cwd: "/ws/task-a/workdir"},
	}
	procs := map[int]Process{10: {Args: electronBundle + "/Contents/MacOS/Electron .", App: "My App", Bundle: electronBundle}}
	probes := map[int]int{}
	d := fakeDetector(&listeners, procs, func(port int) bool { probes[port]++; return port == 9222 })

	d.Detect(context.Background(), resolveTaskA, notOwned)
	envs, _ := d.Detect(context.Background(), resolveTaskA, notOwned)
	if len(envs) != 2 {
		t.Fatalf("detected = %+v, want the app and the server it runs", envs)
	}
	if app := byKind(envs, KindApp); app.Name != "My App" || app.Detail != "Remote debugging on localhost:9222" {
		t.Fatalf("app = %+v", app)
	}
	if server := byKind(envs, KindServer); server.URL != "http://localhost:3000" {
		t.Fatalf("the app's own HTTP server = %+v, want it still opened in a browser", server)
	}
	if probes[9222] != 1 {
		t.Fatalf("probes = %v, want a port that answered as DevTools probed once across scans", probes)
	}
}

func TestDetectorNoticesAnAppThatWasStillStarting(t *testing.T) {
	listeners := []Listener{{PID: 10, Port: 9724, Command: "Electron", Cwd: "/ws/task-a/workdir"}}
	args := electronBundle + "/Contents/MacOS/Electron . --remote-debugging-port=9724"
	procs := map[int]Process{10: {Args: args, Unsettled: true}}
	d := fakeDetector(&listeners, procs, nil)

	d.Detect(context.Background(), resolveTaskA, notOwned)
	if envs, _ := d.Detect(context.Background(), resolveTaskA, notOwned); len(envs) != 0 {
		t.Fatalf("before the app shows itself = %+v, want its debug port left out", envs)
	}
	procs[10] = Process{Args: args, App: "My App", Bundle: electronBundle}
	var envs []Environment
	for i := 0; i < 2; i++ {
		envs, _ = d.Detect(context.Background(), resolveTaskA, notOwned)
	}
	if len(envs) != 1 || envs[0].Kind != KindApp || envs[0].Name != "My App" || envs[0].pid != 10 {
		t.Fatalf("after the app showed itself = %+v, want the app row", envs)
	}
	if len(d.procChecks) != 0 {
		t.Fatalf("procChecks = %v, want a settled process no longer rechecked", d.procChecks)
	}
}

func TestDetectorNoticesADebugPortThatWasNotServingYet(t *testing.T) {
	listeners := []Listener{{PID: 10, Port: 9222, Command: "Electron", Cwd: "/ws/task-a/workdir"}}
	procs := map[int]Process{10: {Args: electronBundle + "/Contents/MacOS/Electron .", App: "My App", Bundle: electronBundle}}
	ready, probes := false, 0
	d := fakeDetector(&listeners, procs, func(int) bool { probes++; return ready })

	d.Detect(context.Background(), resolveTaskA, notOwned)
	envs, _ := d.Detect(context.Background(), resolveTaskA, notOwned)
	if server := byKind(envs, KindServer); server.URL != "http://localhost:9222" {
		t.Fatalf("while the port does not answer as DevTools = %+v, want it shown as a server", envs)
	}
	ready = true
	envs, _ = d.Detect(context.Background(), resolveTaskA, notOwned)
	if len(envs) != 1 || envs[0].Kind != KindApp || envs[0].Detail != "Remote debugging on localhost:9222" {
		t.Fatalf("once it answers as DevTools = %+v, want only the app with the port as detail", envs)
	}
	settled := probes
	d.Detect(context.Background(), resolveTaskA, notOwned)
	if probes != settled {
		t.Fatalf("probed %d more times after the port was recognized", probes-settled)
	}
}

func TestDetectorSpacesOutRechecksThatNeverSettle(t *testing.T) {
	listeners := []Listener{{PID: 10, Port: 3000, Command: "Electron", Cwd: "/ws/task-a/workdir"}}
	procs := map[int]Process{10: {Args: electronBundle + "/Contents/MacOS/Electron .", Unsettled: true}}
	inspected, probes := 0, 0
	d := fakeDetector(&listeners, procs, func(int) bool { probes++; return false })
	inner := d.inspect
	d.inspect = func(ctx context.Context, pids []int) map[int]Process { inspected++; return inner(ctx, pids) }

	for i := 0; i < 45; i++ {
		d.Detect(context.Background(), resolveTaskA, notOwned)
	}
	// Five scans in a row, then every tenth.
	if inspected != 9 || probes != 9 {
		t.Fatalf("inspected %d times and probed %d times over 45 scans, want 9 each", inspected, probes)
	}
	listeners = nil
	d.Detect(context.Background(), resolveTaskA, notOwned)
	if len(d.procChecks) != 0 || len(d.probeChecks) != 0 {
		t.Fatal("rechecks outlived the process")
	}
}

func TestDetectorDropsTheDebugPortOfAProcessWithoutWindows(t *testing.T) {
	listeners := []Listener{{PID: 10, Port: 9333, Command: "chrome", Cwd: "/ws/task-a/workdir"}}
	procs := map[int]Process{10: {Args: "/opt/chrome/chrome --headless --remote-debugging-port=9333"}}
	d := fakeDetector(&listeners, procs, func(int) bool { t.Fatal("a process that is not an app is never probed"); return false })

	d.Detect(context.Background(), resolveTaskA, notOwned)
	if envs, _ := d.Detect(context.Background(), resolveTaskA, notOwned); len(envs) != 0 {
		t.Fatalf("detected = %+v, want a bare debug port left out", envs)
	}
}

func TestDetectorInspectsEachProcessOnce(t *testing.T) {
	listeners := []Listener{{PID: 10, Port: 5173, Command: "node", Cwd: "/ws/task-a/workdir"}}
	inspected := map[int]int{}
	d := fakeDetector(&listeners, map[int]Process{10: {Args: "node vite"}, 11: {Args: "node vite"}}, nil)
	inner := d.inspect
	d.inspect = func(ctx context.Context, pids []int) map[int]Process {
		for _, pid := range pids {
			inspected[pid]++
		}
		return inner(ctx, pids)
	}
	d.Detect(context.Background(), resolveTaskA, notOwned)
	d.Detect(context.Background(), resolveTaskA, notOwned)
	listeners[0].PID = 11
	d.Detect(context.Background(), resolveTaskA, notOwned)
	if inspected[10] != 1 || inspected[11] != 1 {
		t.Fatalf("inspected = %v, want each pid once", inspected)
	}
	if _, kept := d.procs[10]; kept {
		t.Fatal("a pid that stopped listening is still remembered")
	}
}

func TestParsePsArgs(t *testing.T) {
	out := "  4210 /Users/me/My App.app/Contents/MacOS/My App . --remote-debugging-port=9724\n  99 node vite\nbogus\n"
	got := parsePsArgs(out)
	if len(got) != 2 || got[4210].Args != "/Users/me/My App.app/Contents/MacOS/My App . --remote-debugging-port=9724" || got[99].Args != "node vite" {
		t.Fatalf("parsePsArgs = %+v", got)
	}
	if !got[4210].debugs(9724) || got[4210].debugs(972) || got[99].debugs(9724) {
		t.Fatal("debugs matched the wrong ports")
	}
}

func TestProbeCDP(t *testing.T) {
	cdp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/version" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"Browser":"Chrome/152","Protocol-Version":"1.3","webSocketDebuggerUrl":"ws://127.0.0.1/devtools/browser/x"}`))
	}))
	defer cdp.Close()
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>app</html>`))
	}))
	defer web.Close()
	if !probeCDP(context.Background(), cdp.Listener.Addr().(*net.TCPAddr).Port) {
		t.Fatal("a DevTools endpoint was not recognized")
	}
	if probeCDP(context.Background(), web.Listener.Addr().(*net.TCPAddr).Port) {
		t.Fatal("an ordinary web server was taken for a DevTools endpoint")
	}
}

func TestFocusNeedsADetectedApp(t *testing.T) {
	if err := (Environment{Kind: KindServer, pid: 10}).Focus(context.Background()); !errors.Is(err, ErrAppGone) {
		t.Fatalf("focusing a server = %v, want ErrAppGone", err)
	}
	if err := (Environment{Kind: KindApp}).Focus(context.Background()); !errors.Is(err, ErrAppGone) {
		t.Fatalf("focusing an app without a process = %v, want ErrAppGone", err)
	}
}

func fakeDetector(listeners *[]Listener, procs map[int]Process, isCDP func(port int) bool) *Detector {
	d := NewDetector(nil)
	d.list = func(context.Context) ([]Listener, error) { return *listeners, nil }
	d.inspect = func(_ context.Context, pids []int) map[int]Process {
		out := make(map[int]Process)
		for _, pid := range pids {
			if p, ok := procs[pid]; ok {
				out[pid] = p
			}
		}
		return out
	}
	d.isCDP = func(_ context.Context, port int) bool { return isCDP != nil && isCDP(port) }
	return d
}

func resolveTaskA(cwd string) (Task, bool) {
	if strings.HasPrefix(cwd, "/ws/task-a/") {
		return Task{TaskID: "task-a", IssueID: "issue-a", RootDir: "/ws/task-a"}, true
	}
	return Task{}, false
}

func notOwned(int) string { return "" }

func byKind(envs []Environment, kind string) Environment {
	for _, env := range envs {
		if env.Kind == kind {
			return env
		}
	}
	return Environment{}
}
