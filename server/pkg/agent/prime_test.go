//go:build !windows

package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestPrimeVersionExact(t *testing.T) {
	for _, ok := range []string{"0.7.2", "v0.7.2", "prime-agent 0.7.2"} {
		if err := CheckPrimeVersion(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "0.7.1", "0.7.3", "dev", "0.7.2-dev", "foo 0.7.2 9.9.9", "prime-agent 0.7.2 extra", "prefix0.7.2"} {
		if err := CheckPrimeVersion(bad); err == nil {
			t.Errorf("%q unexpectedly accepted", bad)
		}
	}
}

func TestDetectPrimeVersionReadsStderrAndRejectsNoise(t *testing.T) {
	for _, tc := range []struct {
		name    string
		body    string
		wantErr bool
	}{
		{name: "official stderr", body: "echo 0.7.2 >&2"},
		{name: "stdout noise", body: "echo diagnostic; echo 0.7.2 >&2", wantErr: true},
		{name: "stderr noise", body: "echo warning >&2; echo 0.7.2 >&2", wantErr: true},
		{name: "wrong version", body: "echo 0.7.3 >&2", wantErr: true},
		{name: "suffix", body: "echo 0.7.2-dev >&2", wantErr: true},
		{name: "oversized", body: "yes x | head -c 5000 >&2", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "prime-agent")
			if err := os.WriteFile(path, []byte("#!/bin/sh\n"+tc.body+"\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			got, err := DetectPrimeVersion(context.Background(), path)
			if (err != nil) != tc.wantErr {
				t.Fatalf("version=%q err=%v wantErr=%v", got, err, tc.wantErr)
			}
			if !tc.wantErr && got != "0.7.2" {
				t.Fatalf("version=%q, want 0.7.2", got)
			}
		})
	}
}

func TestPrimeAdmissionRequiresExplicitAttestation(t *testing.T) {
	t.Setenv("MULTICA_PRIME_AGENT_ISOLATED", "")
	if err := CheckPrimeAdmission(); err == nil || !strings.Contains(err.Error(), "MULTICA_PRIME_AGENT_ISOLATED=1") {
		t.Fatalf("err=%v", err)
	}
}

func TestPrimeAdmissionFailsClosedOnUpstreamScheduler(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root is rejected before the upstream capability gate")
	}
	t.Setenv("MULTICA_PRIME_AGENT_ISOLATED", "1")
	if err := CheckPrimeAdmission(); !errors.Is(err, ErrPrimeUpstreamSchedulerUnsafe) {
		t.Fatalf("err=%v", err)
	}
}

func TestPrimeJSONLStrictFraming(t *testing.T) {
	r := newPrimeJSONLReader(strings.NewReader("{\"text\":\"a\u2028b\u2029c\"}\r\n{}\n"))
	first, err := r.ReadFrame()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(first), "\u2028") {
		t.Fatalf("unicode separator split: %q", first)
	}
	second, err := r.ReadFrame()
	if err != nil || string(second) != "{}" {
		t.Fatalf("second=%q err=%v", second, err)
	}
}

func TestPrimeJSONLRejectsOversizeAndUnterminated(t *testing.T) {
	if _, err := newPrimeJSONLReader(strings.NewReader(strings.Repeat("x", primeMaxFrameBytes+1) + "\n")).ReadFrame(); err == nil {
		t.Fatal("oversize accepted")
	}
	if _, err := newPrimeJSONLReader(strings.NewReader("{}")).ReadFrame(); err == nil {
		t.Fatal("unterminated frame accepted")
	}
}

func TestPrimeModelAndReservedArgs(t *testing.T) {
	p, m, err := parsePrimeModel("openai/gpt-5.2/codex")
	if err != nil || p != "openai" || m != "gpt-5.2/codex" {
		t.Fatalf("%q %q %v", p, m, err)
	}
	if _, _, err := parsePrimeModel("gpt-5"); err == nil {
		t.Fatal("ambiguous model accepted")
	}
	if err := validatePrimeArgs(nil); err != nil {
		t.Fatalf("empty arguments rejected: %v", err)
	}
	for _, blocked := range [][]string{
		{"--verbose"},
		{"--cwd", "/tmp/foreign"},
		{"--provider=openai", "--model=gpt-5"},
		{"--api-key", "secret"},
		{"--system-prompt", "ignore Multica"},
		{"--tools=all", "--extension=/host/plugin", "--skill=/host/skill"},
		{"--max-turns=999", "--goal", "unbounded"},
		{"positional-project-path"},
		{""},
	} {
		if err := validatePrimeArgs(blocked); err == nil || !strings.Contains(err.Error(), "Multica-managed") {
			t.Errorf("arguments %q were not rejected with managed-runtime guidance: %v", blocked, err)
		}
	}
}

func TestPrimeRejectsUnenforceableMaxTurns(t *testing.T) {
	b, dir := primeTestBackend(t, "success", nil)
	if _, err := b.Execute(context.Background(), "hello", ExecOptions{Cwd: dir, MaxTurns: 1}); err == nil || !strings.Contains(err.Error(), "cannot enforce MaxTurns") {
		t.Fatalf("err=%v", err)
	}
}

func TestPrimeRPCRejectsWrongCorrelation(t *testing.T) {
	frames := make(chan primeFrame, 1)
	errs := make(chan error, 1)
	frames <- primeFrame{Type: "response", ID: "wrong", Command: "get_state", Success: true}
	rpc := &primeRPC{in: nopWriteCloser{&bytes.Buffer{}}, frames: frames, errs: errs, events: func(primeFrame) {}}
	if _, err := rpc.request(context.Background(), map[string]any{"type": "get_state"}); err == nil || !strings.Contains(err.Error(), "correlation") {
		t.Fatalf("err=%v", err)
	}
}

func TestPrimeDaemonShutdownWireIsTaskLocal(t *testing.T) {
	for _, tc := range []struct {
		name       string
		version    int
		correlate  bool
		graceful   bool
		forgedPID  bool
		wantErr    bool
		wantKilled bool
	}{
		{name: "graceful", version: 7, correlate: true, graceful: true},
		{name: "forced after correlation failure", version: 7, correlate: false, wantErr: true, wantKilled: true},
		{name: "reject incompatible hello", version: 6, correlate: true, wantErr: true, wantKilled: true},
		{name: "forged hello pid targets peer only", version: 7, correlate: true, forgedPID: true, wantErr: true, wantKilled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir, err := os.MkdirTemp("", "prime-wire-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })
			_ = os.Chmod(tmpDir, 0o700)
			socketPath := filepath.Join(tmpDir, "daemon.sock")
			listener, err := net.Listen("unix", socketPath)
			if err != nil {
				t.Fatal(err)
			}
			listener.(*net.UnixListener).SetUnlinkOnClose(false)
			_ = os.Chmod(socketPath, 0o600)
			sentinelPath := filepath.Join(tmpDir, "sentinel.sock")
			sentinelListener, err := net.Listen("unix", sentinelPath)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = sentinelListener.Close(); _ = os.Remove(sentinelPath) })

			supervisor := exec.Command("sleep", "30")
			configureProcessGroup(supervisor)
			if err := supervisor.Start(); err != nil {
				t.Fatal(err)
			}
			supervisorStartToken, err := primeProcessStartToken(supervisor.Process.Pid)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = supervisor.Process.Kill(); _, _ = supervisor.Process.Wait() })
			sentinelProcess := exec.Command("sleep", "30")
			configureProcessGroup(sentinelProcess)
			if err := sentinelProcess.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = sentinelProcess.Process.Kill(); _, _ = sentinelProcess.Process.Wait() })
			wire := make(chan map[string]any, 1)
			go func() {
				conn, acceptErr := listener.Accept()
				if acceptErr != nil {
					return
				}
				defer conn.Close()
				defer listener.Close()
				defer os.Remove(socketPath)
				enc := json.NewEncoder(conn)
				helloPID := supervisor.Process.Pid
				if tc.forgedPID {
					helloPID = sentinelProcess.Process.Pid
				}
				_ = enc.Encode(map[string]any{
					"type": "daemon_hello", "socketPath": socketPath,
					"protocol": map[string]any{"name": "prime-agent.daemon", "version": tc.version},
					"schemaId": primeDaemonSchemaID, "appVersion": PrimeAgentVersion,
					"supervisorPid": helloPID, "supervisorProcessStartId": supervisorStartToken, "clientId": "fixture-client",
				})
				if tc.version != primeDaemonProtocolVersion || tc.forgedPID {
					_ = listener.Close()
					return
				}
				var command map[string]any
				if json.NewDecoder(conn).Decode(&command) != nil {
					return
				}
				wire <- command
				id, _ := command["id"].(string)
				if !tc.correlate {
					id = "wrong-id"
				}
				_ = enc.Encode(map[string]any{"type": "response", "id": id, "command": "shutdown", "success": true})
				_ = listener.Close()
				_ = os.Remove(socketPath)
				if tc.graceful {
					_ = supervisor.Process.Signal(syscall.SIGTERM)
					_, _ = supervisor.Process.Wait()
				}
			}()

			pgid, pgidErr := primeSupervisorIdentity(supervisor.Process.Pid)
			if pgidErr != nil {
				t.Fatal(pgidErr)
			}
			err = shutdownPrimeTaskDaemon(tmpDir, socketPath, &primeDaemonIdentity{PID: supervisor.Process.Pid, PGID: pgid, StartToken: supervisorStartToken}, func(net.Conn) (int, int, error) {
				return supervisor.Process.Pid, os.Geteuid(), nil
			})
			if (err != nil) != tc.wantErr {
				t.Fatalf("shutdown error=%v wantErr=%v", err, tc.wantErr)
			}
			if tc.version == 7 && !tc.forgedPID {
				command := <-wire
				protocol := command["protocol"].(map[string]any)
				inner := command["command"].(map[string]any)
				if command["type"] != "command" || command["clientId"] != "fixture-client" || protocol["name"] != "prime-agent.daemon" || protocol["version"] != float64(7) || inner["type"] != "shutdown" || inner["force"] != true || inner["id"] != command["id"] {
					t.Fatalf("unexpected wire command: %#v", command)
				}
			}
			if tc.wantKilled && !primeSupervisorGone(supervisor.Process.Pid, supervisor.Process.Pid, supervisorStartToken) {
				t.Fatal("verified supervisor survived forced targeted cleanup")
			}
			if tc.wantErr && !tc.wantKilled && primeSupervisorGone(supervisor.Process.Pid, supervisor.Process.Pid, supervisorStartToken) {
				t.Fatal("unverified supervisor was terminated")
			}
			if err := syscall.Kill(sentinelProcess.Process.Pid, 0); err != nil {
				t.Fatalf("sentinel process was touched: %v", err)
			}
			if _, err := os.Lstat(sentinelPath); err != nil {
				t.Fatalf("sentinel socket was touched: %v", err)
			}
		})
	}
}

func TestPrimeKernelPeerIdentityOnRealUnixSocket(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "prime-peer-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })
	socketPath := filepath.Join(tmpDir, "peer.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			accepted <- conn
		}
	}()
	client, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server := <-accepted
	defer server.Close()
	pid, uid, err := kernelPrimePeerIdentity(client)
	if err != nil {
		t.Fatal(err)
	}
	if pid != os.Getpid() || uid != os.Geteuid() {
		t.Fatalf("kernel peer identity pid=%d uid=%d, want pid=%d uid=%d", pid, uid, os.Getpid(), os.Geteuid())
	}
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

func TestPrimeExecuteFakeRPC(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("admission correctly rejects root")
	}
	t.Setenv("MULTICA_PRIME_AGENT_ISOLATED", "1")
	dir := t.TempDir()
	tmpDir, err := os.MkdirTemp("", "multica-prime-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })
	if err := os.Chmod(tmpDir, 0o700); err != nil {
		t.Fatal(err)
	}
	wrapper := filepath.Join(dir, "prime-agent")
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = --version ]; then echo 0.7.2; exit 0; fi\nexec %q -test.run=TestPrimeRPCProcess -- \"$@\"\n", os.Args[0])
	if err := os.WriteFile(wrapper, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	b := &primeBackend{cfg: Config{ExecutablePath: wrapper, Logger: slog.Default(), primeTestBypassSafetyAdmission: true, Env: map[string]string{
		"GO_WANT_PRIME_HELPER": "1", "TMPDIR": tmpDir, "PRIME_TEST_SHUTDOWN_FILE": filepath.Join(dir, "prime-shutdown"),
	}}}
	session, err := b.Execute(context.Background(), "hello", ExecOptions{Cwd: dir, Model: "openai/gpt-test", ThinkingLevel: "high"})
	if err != nil {
		t.Fatal(err)
	}
	var sawText, sawThinking, sawTool bool
	for msg := range session.Messages {
		switch msg.Type {
		case MessageText:
			sawText = true
		case MessageThinking:
			sawThinking = true
		case MessageToolUse:
			sawTool = true
		}
	}
	res := <-session.Result
	if res.Status != "completed" || res.Output != "final answer" || res.SessionID != "prime-session" {
		t.Fatalf("result=%+v", res)
	}
	if !sawText || !sawThinking || !sawTool {
		t.Fatalf("events text=%v thinking=%v tool=%v", sawText, sawThinking, sawTool)
	}
	if got := res.Usage["openai/gpt-test"]; got.InputTokens != 10 || got.OutputTokens != 4 {
		t.Fatalf("usage=%+v", got)
	}
	assertPrimeShutdown(t, dir)
}

func primeTestBackend(t *testing.T, mode string, extraEnv map[string]string) (*primeBackend, string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("admission correctly rejects root")
	}
	t.Setenv("MULTICA_PRIME_AGENT_ISOLATED", "1")
	t.Setenv("PRIME_HOST_SENTINEL_SECRET", "must-not-leak")
	dir := t.TempDir()
	tmpDir, err := os.MkdirTemp("", "multica-prime-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })
	if err := os.Chmod(tmpDir, 0o700); err != nil {
		t.Fatal(err)
	}
	wrapper := filepath.Join(dir, "prime-agent")
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = --version ]; then echo 0.7.2; exit 0; fi\nexec %q -test.run=TestPrimeRPCProcess -- \"$@\"\n", os.Args[0])
	if err := os.WriteFile(wrapper, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{
		"GO_WANT_PRIME_HELPER": "1", "PRIME_TEST_MODE": mode, "TMPDIR": tmpDir,
		"PRIME_TEST_SHUTDOWN_FILE": filepath.Join(dir, "prime-shutdown"),
	}
	for key, value := range extraEnv {
		env[key] = value
	}
	return &primeBackend{cfg: Config{ExecutablePath: wrapper, Logger: slog.Default(), Env: env, primeTestBypassSafetyAdmission: true}}, dir
}

func assertPrimeShutdown(t *testing.T, dir string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "prime-shutdown"))
	if err != nil {
		t.Fatalf("Prime shutdown was not invoked: %v", err)
	}
	if !strings.Contains(string(b), "targeted:") || strings.Contains(string(b), "shutdown --force --json") {
		t.Fatalf("unexpected shutdown invocation: %q", b)
	}
}

func TestPrimeRequiresPrivateTaskTMPDIR(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("admission correctly rejects root")
	}
	t.Setenv("MULTICA_PRIME_AGENT_ISOLATED", "1")
	b := &primeBackend{cfg: Config{ExecutablePath: "does-not-matter", Logger: slog.Default(), primeTestBypassSafetyAdmission: true}}
	if _, err := b.Execute(context.Background(), "hello", ExecOptions{}); err == nil || !strings.Contains(err.Error(), "task-private TMPDIR") {
		t.Fatalf("missing TMPDIR err=%v", err)
	}
	public := filepath.Join(t.TempDir(), "public")
	if err := os.Mkdir(public, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(public, 0o755); err != nil {
		t.Fatal(err)
	}
	b.cfg.Env = map[string]string{"TMPDIR": public}
	if _, err := b.Execute(context.Background(), "hello", ExecOptions{}); err == nil || !strings.Contains(err.Error(), "not private") {
		t.Fatalf("public TMPDIR err=%v", err)
	}
}

func TestPrimeRejectsOverlongDaemonSocketPath(t *testing.T) {
	tmpDir := "/" + strings.Repeat("a", primeUnixSocketPathLimit)
	err := validateUnusedPrimeSocketPath(tmpDir, filepath.Join(tmpDir, "multica-prime-daemon.sock"))
	if err == nil || !strings.Contains(err.Error(), "too long") {
		t.Fatalf("err=%v", err)
	}
}

func TestPrimeEnvironmentIsMinimizedAndExplicit(t *testing.T) {
	t.Setenv("PRIME_HOST_SENTINEL_SECRET", "must-not-leak")
	t.Setenv("PATH", "/safe/bin")
	t.Setenv("HOME", "/safe/home")
	t.Setenv("LANG", "C.UTF-8")
	t.Setenv("LC_ALL", "C")
	env := buildPrimeEnv(map[string]string{
		"HOME": "/task/home", "EXPLICIT_PROVIDER_KEY": "task-secret",
		"PRIME_AGENT_CODING_AGENT_DIR": "/private/prime-agent-state/runtime/agent",
		"PRIME_AGENT_TELEMETRY":        "1", "DO_NOT_TRACK": "0", "PI_SKIP_VERSION_CHECK": "0",
	})
	got := map[string]string{}
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			got[key] = value
		}
	}
	if _, ok := got["PRIME_HOST_SENTINEL_SECRET"]; ok {
		t.Fatal("unrelated host secret leaked into Prime environment")
	}
	if got["PATH"] != "/safe/bin" || got["LANG"] != "C.UTF-8" || got["LC_ALL"] != "C" {
		t.Fatalf("essential environment missing: %v", got)
	}
	if got["HOME"] != "/task/home" || got["EXPLICIT_PROVIDER_KEY"] != "task-secret" {
		t.Fatalf("explicit cfg environment missing or lost precedence: %v", got)
	}
	if got["PRIME_AGENT_CODING_AGENT_DIR"] != "/private/prime-agent-state/runtime/agent" {
		t.Fatalf("daemon-owned Prime state directory was not passed to backend: %v", got)
	}
	if got["PRIME_AGENT_TELEMETRY"] != "0" || got["DO_NOT_TRACK"] != "1" || got["PI_SKIP_VERSION_CHECK"] != "1" {
		t.Fatalf("telemetry was not forced off: %v", got)
	}
}

func TestPrimeTerminalErrorAndResumeMismatch(t *testing.T) {
	t.Run("terminal error", func(t *testing.T) {
		b, dir := primeTestBackend(t, "terminal_error", nil)
		s, err := b.Execute(context.Background(), "hello", ExecOptions{Cwd: dir})
		if err != nil {
			t.Fatal(err)
		}
		for range s.Messages {
		}
		res := <-s.Result
		if res.Status != "failed" || !strings.Contains(res.Error, "provider exploded") {
			t.Fatalf("result=%+v", res)
		}
		if res.Usage["prime/default"].InputTokens != 10 {
			t.Fatalf("terminal failure lost usage: %+v", res.Usage)
		}
		assertPrimeShutdown(t, dir)
	})
	t.Run("resume mismatch", func(t *testing.T) {
		b, dir := primeTestBackend(t, "resume_mismatch", nil)
		s, err := b.Execute(context.Background(), "hello", ExecOptions{Cwd: dir, ResumeSessionID: "wanted"})
		if err != nil {
			t.Fatal(err)
		}
		for range s.Messages {
		}
		res := <-s.Result
		if res.Status != "failed" || !res.ResumeRejected || !strings.Contains(res.Error, "resume session mismatch") {
			t.Fatalf("result=%+v", res)
		}
		assertPrimeShutdown(t, dir)
	})
}

func TestPrimeAccountingAndClientExitFailClosed(t *testing.T) {
	for _, tc := range []struct {
		mode string
		want string
	}{
		{mode: "stats_error", want: "stats"},
		{mode: "stats_negative", want: "session-stats"},
		{mode: "stats_overflow", want: "session-stats"},
		{mode: "client_exit_error", want: "exited unsuccessfully"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			b, dir := primeTestBackend(t, tc.mode, nil)
			s, err := b.Execute(context.Background(), "hello", ExecOptions{Cwd: dir})
			if err != nil {
				t.Fatal(err)
			}
			for range s.Messages {
			}
			res := <-s.Result
			if res.Status == "completed" || !strings.Contains(res.Error, tc.want) {
				t.Fatalf("result=%+v", res)
			}
		})
	}
}

func TestPrimeUnlinkedSocketStillTerminatesCapturedSupervisor(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "prime-unlinked-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })
	_ = os.Chmod(tmpDir, 0o700)
	supervisor := exec.Command("sleep", "30")
	configureProcessGroup(supervisor)
	if err := supervisor.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = supervisor.Process.Kill(); _, _ = supervisor.Process.Wait() })
	sentinel := exec.Command("sleep", "30")
	configureProcessGroup(sentinel)
	if err := sentinel.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sentinel.Process.Kill(); _, _ = sentinel.Process.Wait() })
	pgid, err := primeSupervisorIdentity(supervisor.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	startToken, err := primeProcessStartToken(supervisor.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	identity := &primeDaemonIdentity{PID: supervisor.Process.Pid, PGID: pgid, StartToken: startToken}
	if err := shutdownPrimeTaskDaemon(tmpDir, filepath.Join(tmpDir, "already-unlinked.sock"), identity, kernelPrimePeerIdentity); err != nil {
		t.Fatal(err)
	}
	if !primeSupervisorGone(identity.PID, identity.PGID, identity.StartToken) {
		t.Fatal("captured supervisor survived after unlinking its socket")
	}
	if err := syscall.Kill(sentinel.Process.Pid, 0); err != nil {
		t.Fatalf("sentinel was touched: %v", err)
	}
}

func TestPrimeRefusesSignalAfterProcessStartIdentityMismatch(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "prime-reused-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })
	_ = os.Chmod(tmpDir, 0o700)
	sentinel := exec.Command("sleep", "30")
	configureProcessGroup(sentinel)
	if err := sentinel.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sentinel.Process.Kill(); _, _ = sentinel.Process.Wait() })
	pgid, err := primeSupervisorIdentity(sentinel.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	identity := &primeDaemonIdentity{PID: sentinel.Process.Pid, PGID: pgid, StartToken: "forged-old-process-start"}
	err = shutdownPrimeTaskDaemon(tmpDir, filepath.Join(tmpDir, "unlinked.sock"), identity, kernelPrimePeerIdentity)
	if err == nil || !strings.Contains(err.Error(), "start identity changed") {
		t.Fatalf("err=%v", err)
	}
	if err := syscall.Kill(sentinel.Process.Pid, 0); err != nil {
		t.Fatalf("PID-reuse sentinel was signalled: %v", err)
	}
}

func TestPrimeMalformedFirstRPCStillReapsObservedProcess(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	b, dir := primeTestBackend(t, "malformed_first_rpc", map[string]string{"PRIME_TEST_PID_FILE": pidFile})
	s, err := b.Execute(context.Background(), "hello", ExecOptions{Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	for range s.Messages {
	}
	res := <-s.Result
	if res.Status != "failed" || !strings.Contains(res.Error, "malformed Prime RPC") {
		t.Fatalf("result=%+v", res)
	}
	pidRaw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(pidRaw)))
	if err := syscall.Kill(pid, 0); err == nil {
		t.Fatalf("observed Prime process %d survived malformed first RPC", pid)
	}
	assertPrimeShutdown(t, dir)
}

func TestPrimeShutdownFailurePreventsCompletedStatus(t *testing.T) {
	b, dir := primeTestBackend(t, "shutdown_fail", nil)
	s, err := b.Execute(context.Background(), "hello", ExecOptions{Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	for range s.Messages {
	}
	res := <-s.Result
	if res.Status == "completed" || !strings.Contains(res.Error, "shutdown") {
		t.Fatalf("result=%+v", res)
	}
	assertPrimeShutdown(t, dir)
}

func TestPrimeCancellationReapsProcessAndRedactsStderr(t *testing.T) {
	t.Run("timeout reaps", func(t *testing.T) {
		pidFile := filepath.Join(t.TempDir(), "pid")
		b, dir := primeTestBackend(t, "hang", map[string]string{"PRIME_TEST_PID_FILE": pidFile})
		s, err := b.Execute(context.Background(), "hello", ExecOptions{Cwd: dir, Timeout: 150 * time.Millisecond})
		if err != nil {
			t.Fatal(err)
		}
		for range s.Messages {
		}
		res := <-s.Result
		if res.Status != "timeout" {
			t.Fatalf("result=%+v", res)
		}
		if res.Usage["prime/default"].InputTokens != 10 {
			t.Fatalf("timeout lost usage: %+v", res.Usage)
		}
		pidRaw, err := os.ReadFile(pidFile)
		if err != nil {
			t.Fatal(err)
		}
		pid, _ := strconv.Atoi(strings.TrimSpace(string(pidRaw)))
		if err := syscall.Kill(pid, 0); err == nil {
			t.Fatalf("Prime helper pid %d still alive", pid)
		}
		assertPrimeShutdown(t, dir)
	})
	t.Run("stderr secret redacted", func(t *testing.T) {
		b, dir := primeTestBackend(t, "stderr_error", nil)
		s, err := b.Execute(context.Background(), "hello", ExecOptions{Cwd: dir})
		if err != nil {
			t.Fatal(err)
		}
		for range s.Messages {
		}
		res := <-s.Result
		if strings.Contains(res.Error, "supersecret") || !strings.Contains(res.Error, "REDACTED") {
			t.Fatalf("result=%+v", res)
		}
		assertPrimeShutdown(t, dir)
	})
}

func TestPrimeRPCProcess(t *testing.T) {
	if os.Getenv("GO_WANT_PRIME_HELPER") != "1" {
		return
	}
	if os.Getenv("PRIME_AGENT_TELEMETRY") != "0" || os.Getenv("DO_NOT_TRACK") != "1" || os.Getenv("PI_SKIP_VERSION_CHECK") != "1" || os.Getenv("RLM_MAX_DEPTH") != "0" {
		os.Exit(91)
	}
	joinedArgs := strings.Join(os.Args, " ")
	mode := os.Getenv("PRIME_TEST_MODE")
	for _, required := range []string{"--mode rpc", "--no-extensions", "--no-skills", "--no-prompt-templates", "--no-themes"} {
		if !strings.Contains(joinedArgs, required) {
			os.Exit(93)
		}
	}
	socketPath := ""
	for i, arg := range os.Args {
		if arg == "--daemon-socket" && i+1 < len(os.Args) {
			socketPath = os.Args[i+1]
		}
	}
	if socketPath == "" || filepath.Dir(socketPath) != os.Getenv("TMPDIR") {
		os.Exit(95)
	}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		os.Exit(96)
	}
	if unixListener, ok := listener.(*net.UnixListener); ok {
		unixListener.SetUnlinkOnClose(false)
	}
	_ = os.Chmod(socketPath, 0o600)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				startToken, _ := primeProcessStartToken(os.Getpid())
				_ = json.NewEncoder(conn).Encode(map[string]any{
					"type": "daemon_hello", "socketPath": socketPath,
					"protocol": map[string]any{"name": "prime-agent.daemon", "version": primeDaemonProtocolVersion},
					"schemaId": primeDaemonSchemaID, "appVersion": PrimeAgentVersion,
					"supervisorPid": os.Getpid(), "supervisorProcessStartId": startToken, "clientId": "fake-client",
				})
				var envelope map[string]any
				if json.NewDecoder(conn).Decode(&envelope) == nil {
					id, _ := envelope["id"].(string)
					_ = json.NewEncoder(conn).Encode(map[string]any{"type": "response", "id": id, "command": "shutdown", "success": true})
				}
			}(conn)
		}
	}()
	recordTargetedCleanup := func(remove bool) {
		_ = listener.Close()
		if remove {
			_ = os.Remove(socketPath)
		}
		if path := os.Getenv("PRIME_TEST_SHUTDOWN_FILE"); path != "" {
			_ = os.WriteFile(path, []byte("targeted:"+socketPath), 0o600)
		}
	}
	if path := os.Getenv("PRIME_TEST_PID_FILE"); path != "" {
		_ = os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o600)
	}
	if mode == "stderr_error" {
		_, _ = fmt.Fprintln(os.Stderr, "api_key=supersecret")
		_, _ = fmt.Fprintln(os.Stdout, "not-json")
		recordTargetedCleanup(true)
		time.Sleep(time.Second)
		os.Exit(2)
	}
	s := bufio.NewScanner(os.Stdin)
	enc := json.NewEncoder(os.Stdout)
	for s.Scan() {
		var cmd map[string]any
		if json.Unmarshal(s.Bytes(), &cmd) != nil {
			os.Exit(92)
		}
		id, typ := cmd["id"], cmd["type"].(string)
		switch typ {
		case "get_state":
			if mode == "malformed_first_rpc" {
				_, _ = fmt.Fprintln(os.Stdout, "not-json")
				continue
			}
			sessionID := "prime-session"
			if mode == "resume_mismatch" {
				sessionID = "other-session"
			}
			enc.Encode(map[string]any{"type": "response", "id": id, "command": typ, "success": true, "data": map[string]any{"sessionId": sessionID, "isStreaming": false, "messageCount": 0}})
		case "set_model", "set_thinking_level":
			enc.Encode(map[string]any{"type": "response", "id": id, "command": typ, "success": true})
		case "prompt":
			enc.Encode(map[string]any{"type": "response", "id": id, "command": typ, "success": true})
			if mode == "hang" {
				continue
			}
			if mode == "terminal_error" {
				enc.Encode(map[string]any{"type": "agent_end", "messages": []any{map[string]any{"role": "assistant", "stopReason": "error", "errorMessage": "provider exploded"}}})
				continue
			}
			enc.Encode(map[string]any{"type": "message_update", "assistantMessageEvent": map[string]any{"type": "thinking_delta", "delta": "hmm"}})
			enc.Encode(map[string]any{"type": "message_update", "assistantMessageEvent": map[string]any{"type": "text_delta", "delta": "answer"}})
			enc.Encode(map[string]any{"type": "tool_execution_start", "toolCallId": "t1", "toolName": "read", "args": map[string]any{"path": "x"}})
			enc.Encode(map[string]any{"type": "tool_execution_end", "toolCallId": "t1", "toolName": "read", "result": map[string]any{"content": "ok"}})
			enc.Encode(map[string]any{"type": "agent_end", "messages": []any{map[string]any{"role": "assistant", "stopReason": "stop"}}})
		case "get_last_assistant_text":
			enc.Encode(map[string]any{"type": "response", "id": id, "command": typ, "success": true, "data": map[string]any{"text": "final answer"}})
		case "get_session_stats":
			if mode == "stats_error" {
				enc.Encode(map[string]any{"type": "response", "id": id, "command": typ, "success": false, "error": "stats unavailable"})
				continue
			}
			if mode == "stats_negative" {
				enc.Encode(map[string]any{"type": "response", "id": id, "command": typ, "success": true, "data": map[string]any{"sessionId": "prime-session", "tokens": map[string]any{"input": -1}, "cost": -1}})
				continue
			}
			if mode == "stats_overflow" {
				enc.Encode(map[string]any{"type": "response", "id": id, "command": typ, "success": true, "data": map[string]any{"sessionId": "prime-session", "tokens": map[string]any{}, "cost": 1e100}})
				continue
			}
			enc.Encode(map[string]any{"type": "response", "id": id, "command": typ, "success": true, "data": map[string]any{"sessionId": "prime-session", "tokens": map[string]any{"input": 10, "output": 4, "cacheRead": 2, "cacheWrite": 1}, "cost": 0.01}})
		case "abort":
			enc.Encode(map[string]any{"type": "response", "id": id, "command": typ, "success": true})
			recordTargetedCleanup(mode != "shutdown_fail")
			if mode == "client_exit_error" {
				os.Exit(7)
			}
			return
		}
	}
	os.Exit(0)
}
