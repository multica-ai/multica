package computer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// This opt-in uses only the built JavaScript runtime, never a model or desktop.
func TestBuiltRuntimeSandbox(t *testing.T) {
	exe, entry := os.Getenv("MULTICA_TEST_COMPUTER_EXECUTABLE"), os.Getenv("MULTICA_TEST_COMPUTER_ENTRY")
	if runtime.GOOS != "darwin" || exe == "" || entry == "" {
		t.Skip("supply the built runtime paths to verify the macOS sandbox")
	}
	m := &Manager{Command: Command{Executable: exe, Entry: entry}}
	defer m.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r, err := m.Execute(ctx, "c", "one", "let retained = await Promise.resolve(42); retained", nil)
	if err != nil || r.Value != "42" {
		t.Fatalf("startup=%+v err=%v", r, err)
	}
	path := filepath.Join(t.TempDir(), "private.txt")
	if err := os.WriteFile(path, []byte("private-marker"), 0600); err != nil {
		t.Fatal(err)
	}
	code := `let denied; try { denied = require("node:fs").readFileSync(` + strconv.Quote(path) + `,"utf8") } catch (e) { denied = e.code }; denied`
	r, err = m.Execute(ctx, "c", "two", code, nil)
	if err != nil || strings.Contains(r.Value, "private-marker") || !(strings.Contains(r.Value, "EPERM") || strings.Contains(r.Value, "EACCES")) {
		t.Fatalf("file sandbox=%+v err=%v", r, err)
	}
	r, err = m.Execute(ctx, "c", "three", `retained`, nil)
	if err != nil || r.Value != "42" {
		t.Fatalf("persistence=%+v err=%v", r, err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("network-marker")) }))
	defer server.Close()
	code = `let network; try { network=await(await fetch(` + strconv.Quote(server.URL) + `)).text() } catch(e) {network=e.cause?.code || e.code || "denied"}; network`
	r, err = m.Execute(ctx, "c", "four", code, nil)
	if err != nil || strings.Contains(r.Value, "network-marker") || !(strings.Contains(r.Value, "EPERM") || strings.Contains(r.Value, "EACCES")) {
		t.Fatalf("network sandbox=%+v err=%v", r, err)
	}
	code = `let child; try {child=require("node:child_process").execFileSync("/bin/echo",["child-marker"]).toString()} catch(e){child=e.code}; child`
	r, err = m.Execute(ctx, "c", "five", code, nil)
	if err != nil || strings.Contains(r.Value, "child-marker") || !(strings.Contains(r.Value, "EPERM") || strings.Contains(r.Value, "EACCES")) {
		t.Fatalf("child sandbox=%+v err=%v", r, err)
	}
	code = `let write; try {require("node:fs").writeFileSync(` + strconv.Quote(path) + `,"changed");write="written"} catch(e){write=e.code};write`
	r, err = m.Execute(ctx, "c", "six", code, nil)
	if err != nil || !(strings.Contains(r.Value, "EPERM") || strings.Contains(r.Value, "EACCES")) {
		t.Fatalf("write sandbox=%+v err=%v", r, err)
	}
}
