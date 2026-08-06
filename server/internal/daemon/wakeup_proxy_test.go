package daemon

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

const (
	// The child process re-runs this same test with the proxy environment in
	// place; the marker tells it which half of the test to execute.
	wakeupProxyChildEnv = "MULTICA_TEST_WAKEUP_PROXY_CHILD"

	// A non-loopback, permanently unresolvable host. Loopback targets are the
	// wrong choice here: net/http's proxy resolver bypasses proxies for
	// localhost/127.0.0.1, so a loopback target would be dialled direct even
	// with the proxy wired up correctly.
	wakeupProxyTargetHost = "wakeup.example.invalid"
)

// TestTaskWakeupDialUsesEnvironmentProxy pins that the daemon's wakeup
// WebSocket honours HTTPS_PROXY. Without it, a daemon behind a corporate
// egress proxy can never open the control connection and silently degrades to
// HTTP polling.
//
// The test runs the dial in a child process because net/http resolves the
// proxy environment exactly once per process (envProxyOnce) and caches the
// result: by the time this test runs inside the package suite, some earlier
// test has already primed that cache with "no proxy", and t.Setenv can no
// longer influence it. A fresh process reads the environment we hand it.
func TestTaskWakeupDialUsesEnvironmentProxy(t *testing.T) {
	if os.Getenv(wakeupProxyChildEnv) == "1" {
		dialWakeupThroughEnvProxy(t)
		return
	}

	// Stand-in CONNECT proxy: one line of the request is all we need to know
	// whether the dial was routed through it.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	requestLine := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		line, err := bufio.NewReader(conn).ReadString('\n')
		if err != nil {
			return
		}
		requestLine <- strings.TrimSpace(line)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestTaskWakeupDialUsesEnvironmentProxy$", "-test.v")
	cmd.Env = append(environWithoutProxyVars(),
		wakeupProxyChildEnv+"=1",
		"HTTPS_PROXY=http://"+ln.Addr().String(),
	)
	out, runErr := cmd.CombinedOutput()

	select {
	case line := <-requestLine:
		want := "CONNECT " + wakeupProxyTargetHost + ":443"
		if !strings.HasPrefix(line, want) {
			t.Fatalf("proxy received %q, want a request line starting with %q\nchild output:\n%s", line, want, out)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("proxy received no CONNECT: the wakeup dial ignored HTTPS_PROXY (child exit: %v)\nchild output:\n%s", runErr, out)
	}
}

// dialWakeupThroughEnvProxy is the child half: drive the real connection path
// at a wss:// target and let it fail. The fake proxy hangs up after the
// request line, so the handshake never completes — the parent asserts on what
// the proxy saw, not on the outcome here.
func dialWakeupThroughEnvProxy(t *testing.T) {
	d := New(Config{
		ServerBaseURL:  "https://" + wakeupProxyTargetHost,
		WorkspacesRoot: t.TempDir(),
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	_, err := d.runTaskWakeupConnection(ctx, nil, make(chan taskWakeup, 1), make(chan struct{}))
	t.Logf("wakeup dial returned: %v", err)
}

// environWithoutProxyVars copies the environment minus every *_PROXY setting,
// so a developer's own proxy configuration cannot decide the result.
func environWithoutProxyVars() []string {
	env := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		key, _, ok := strings.Cut(kv, "=")
		if ok && strings.HasSuffix(strings.ToLower(key), "_proxy") {
			continue
		}
		env = append(env, kv)
	}
	return env
}
