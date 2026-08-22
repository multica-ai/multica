package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewReturnsZeroclawBackend(t *testing.T) {
	t.Parallel()
	b, err := New("zeroclaw", Config{ExecutablePath: "/nonexistent/zeroclaw"})
	if err != nil {
		t.Fatalf("New(zeroclaw) error: %v", err)
	}
	if _, ok := b.(*zeroclawBackend); !ok {
		t.Fatalf("expected *zeroclawBackend, got %T", b)
	}
}

// fakeZeroclawACPScript impersonates `zeroclaw acp` for unit tests. Unlike a
// generic ACP fake, this one reproduces the frames a real ZeroClaw 0.8.4
// binary was observed to send, because several of the bugs this suite guards
// against were shipped by a fake that answered more generously than the
// runtime does:
//
//   - initialize carries the model id in `_meta.zeroclaw.defaultModel` and
//     advertises no remote MCP transports.
//   - session/new returns {sessionId, workspaceDir} and nothing else — no
//     model catalog, no currentModelId.
//   - session/resume returns a bare `{}`; ZeroClaw dispatches no
//     session/set_model at all, so that request must fall through to the
//     -32601 default arm exactly as it does on the wire.
//   - an unknown session is ZeroClaw's custom -32000 SESSION_NOT_FOUND, not a
//     JSON-RPC standard code.
//
// ZEROCLAW_STALE_REPLAY makes session/resume push a historical
// agent_message_chunk before answering, which is what session/load does for
// real and what the turn gate has to swallow.
func fakeZeroclawACPScript() string {
	return `#!/bin/sh
while IFS= read -r line; do
  if [ -n "$ZEROCLAW_REQUESTS_FILE" ]; then
    printf '%s\n' "$line" >> "$ZEROCLAW_REQUESTS_FILE"
  fi
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"_meta":{"zeroclaw":{"defaultModel":"llama3.2","maxSessions":10,"sessionTimeoutSecs":3600}},"agentInfo":{"name":"zeroclaw-acp","version":"0.8.4"},"authMethods":[],"agentCapabilities":{"loadSession":true,"mcpCapabilities":{"http":false,"sse":false},"sessionCapabilities":{"close":{},"resume":{}}}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      if [ -n "$ZEROCLAW_REQUIRE_AGENT_ALIAS" ]; then
        printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32602,"message":"session/new requires ` + "`agentAlias`" + ` (alias of a configured [agents.<alias>] entry)"}}\n' "$id"
        exit 0
      fi
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_zeroclaw_new","workspaceDir":"/tmp"}}\n' "$id"
      ;;
    *'"method":"session/resume"'*)
      if [ -n "$ZEROCLAW_SESSION_NOT_FOUND" ]; then
        printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32000,"message":"Session not found: ses_gone"}}\n' "$id"
        exit 0
      fi
      if [ -n "$ZEROCLAW_STALE_REPLAY" ]; then
        printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_existing","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"STALE PRIOR ANSWER"}}}}\n'
      fi
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_existing","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"CURRENT ANSWER"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn","usage":{"inputTokens":10,"outputTokens":20,"cacheReadTokens":3,"cacheWriteTokens":2,"costUsdTicks":900}}}\n' "$id"
      ;;
    *)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"Method not found"}}\n' "$id"
      ;;
  esac
done
`
}

func writeFakeZeroclawScript(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "zeroclaw")
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatalf("write fake zeroclaw: %v", err)
	}
	return bin
}

// TestZeroclawSessionNew covers the fresh-session happy path: initialize,
// session/new, session/prompt, and a completed result carrying the new
// session id and usage attributed to the model ZeroClaw reported on
// initialize.
func TestZeroclawSessionNew(t *testing.T) {
	t.Parallel()
	bin := writeFakeZeroclawScript(t, fakeZeroclawACPScript())

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	b, err := New("zeroclaw", Config{
		ExecutablePath: bin,
		Logger:         logger,
	})
	if err != nil {
		t.Fatalf("New(zeroclaw) error: %v", err)
	}

	ctx := context.Background()
	session, err := b.Execute(ctx, "test prompt", ExecOptions{
		Cwd: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	for range session.Messages {
	}

	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("expected completed, got status=%q error=%q", result.Status, result.Error)
	}
	if result.SessionID != "ses_zeroclaw_new" {
		t.Fatalf("expected sessionID ses_zeroclaw_new, got %q", result.SessionID)
	}
	// `initialize._meta.zeroclaw.defaultModel` is the only model identity
	// ZeroClaw's ACP surface exposes, so it is what usage must be keyed on.
	if _, ok := result.Usage["llama3.2"]; !ok {
		t.Fatalf("expected usage attributed to the initialize default model, got %+v", result.Usage)
	}
}

// TestZeroclawInitializeWithoutDefaultModel proves a handshake that omits
// `_meta` degrades to an unlabelled run instead of failing it. ZeroClaw leaves
// defaultModel out whenever no provider entry is configured.
func TestZeroclawInitializeWithoutDefaultModel(t *testing.T) {
	t.Parallel()
	script := strings.Replace(
		fakeZeroclawACPScript(),
		`"_meta":{"zeroclaw":{"defaultModel":"llama3.2","maxSessions":10,"sessionTimeoutSecs":3600}},`,
		`"_meta":{"zeroclaw":{"maxSessions":10}},`,
		1,
	)
	if strings.Contains(script, "defaultModel") {
		t.Fatal("fake script rewrite failed: defaultModel still present")
	}
	bin := writeFakeZeroclawScript(t, script)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	b, err := New("zeroclaw", Config{ExecutablePath: bin, Logger: logger})
	if err != nil {
		t.Fatalf("New(zeroclaw) error: %v", err)
	}

	session, err := b.Execute(context.Background(), "test prompt", ExecOptions{Cwd: t.TempDir()})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	for range session.Messages {
	}

	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("expected completed, got status=%q error=%q", result.Status, result.Error)
	}
	if _, ok := result.Usage["unknown"]; !ok {
		t.Fatalf("expected usage under the unknown-model key, got %+v", result.Usage)
	}
}

// TestZeroclawResumeUsesSessionResume covers the resume happy path: a
// follow-up run with ResumeSessionID set uses session/resume and reports
// ResumeRejected=false.
//
// session/load is explicitly forbidden here. Both methods restore the
// transcript into the agent, but load also replays every retained message back
// to the client as session/update notifications, so resuming through it would
// feed the previous answer into this turn's deliverable.
func TestZeroclawResumeUsesSessionResume(t *testing.T) {
	t.Parallel()
	bin := writeFakeZeroclawScript(t, fakeZeroclawACPScript())
	reqFile := filepath.Join(t.TempDir(), "requests.txt")

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	b, err := New("zeroclaw", Config{
		ExecutablePath: bin,
		Logger:         logger,
		Env:            map[string]string{"ZEROCLAW_REQUESTS_FILE": reqFile},
	})
	if err != nil {
		t.Fatalf("New(zeroclaw) error: %v", err)
	}

	ctx := context.Background()
	session, err := b.Execute(ctx, "test prompt", ExecOptions{
		Cwd:             t.TempDir(),
		ResumeSessionID: "ses_existing",
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	for range session.Messages {
	}

	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("expected completed, got status=%q error=%q", result.Status, result.Error)
	}
	// session/resume returns a bare {}, so resolveResumedSessionID falls back
	// to the requested id.
	if result.SessionID != "ses_existing" {
		t.Fatalf("expected sessionID ses_existing (fallback from resume), got %q", result.SessionID)
	}
	if result.ResumeRejected {
		t.Fatal("expected ResumeRejected=false on successful resume")
	}

	raw, err := os.ReadFile(reqFile)
	if err != nil {
		t.Fatalf("read requests file: %v", err)
	}
	requests := string(raw)
	if !strings.Contains(requests, `"method":"session/resume"`) {
		t.Fatalf("expected session/resume on resume, got requests:\n%s", requests)
	}
	assertNoRecordedFrame(t, reqFile, "session/load")
	if strings.Contains(requests, `"method":"session/new"`) {
		t.Fatalf("resume must not call session/new, got requests:\n%s", requests)
	}
}

// TestZeroclawResumeDropsReplayedHistory pins the reason resume switched off
// session/load. Even when the runtime pushes a historical
// agent_message_chunk before answering the resume request, the turn gate must
// swallow it: it belongs to a prior turn, and appending it would republish the
// previous answer as this run's output and re-emit it to the UI.
func TestZeroclawResumeDropsReplayedHistory(t *testing.T) {
	t.Parallel()
	bin := writeFakeZeroclawScript(t, fakeZeroclawACPScript())

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	b, err := New("zeroclaw", Config{
		ExecutablePath: bin,
		Logger:         logger,
		Env:            map[string]string{"ZEROCLAW_STALE_REPLAY": "1"},
	})
	if err != nil {
		t.Fatalf("New(zeroclaw) error: %v", err)
	}

	session, err := b.Execute(context.Background(), "test prompt", ExecOptions{
		Cwd:             t.TempDir(),
		ResumeSessionID: "ses_existing",
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var streamed []string
	for msg := range session.Messages {
		if msg.Type == MessageText {
			streamed = append(streamed, msg.Content)
		}
	}

	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("expected completed, got status=%q error=%q", result.Status, result.Error)
	}
	if strings.Contains(result.Output, "STALE PRIOR ANSWER") {
		t.Fatalf("replayed history leaked into Result.Output: %q", result.Output)
	}
	if !strings.Contains(result.Output, "CURRENT ANSWER") {
		t.Fatalf("expected the current turn's answer in Result.Output, got %q", result.Output)
	}
	for _, content := range streamed {
		if strings.Contains(content, "STALE PRIOR ANSWER") {
			t.Fatalf("replayed history was re-sent to the UI: %v", streamed)
		}
	}
}

// TestZeroclawResumeNotFound covers the resume-not-found path: session/resume
// fails with ZeroClaw's custom -32000 SESSION_NOT_FOUND and the backend
// reports ResumeRejected=true so the daemon retries fresh.
func TestZeroclawResumeNotFound(t *testing.T) {
	t.Parallel()
	bin := writeFakeZeroclawScript(t, fakeZeroclawACPScript())

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	b, err := New("zeroclaw", Config{
		ExecutablePath: bin,
		Logger:         logger,
		Env:            map[string]string{"ZEROCLAW_SESSION_NOT_FOUND": "1"},
	})
	if err != nil {
		t.Fatalf("New(zeroclaw) error: %v", err)
	}

	ctx := context.Background()
	session, err := b.Execute(ctx, "test prompt", ExecOptions{
		Cwd:             t.TempDir(),
		ResumeSessionID: "ses_gone",
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	for range session.Messages {
	}

	result := <-session.Result
	if result.Status != "failed" {
		t.Fatalf("expected failed, got status=%q error=%q", result.Status, result.Error)
	}
	if !strings.Contains(result.Error, "session/resume failed") {
		t.Fatalf("expected session/resume failed error, got %q", result.Error)
	}
	if !result.ResumeRejected {
		t.Fatal("expected ResumeRejected=true on session not found — ZeroClaw reports it as -32000, which isACPSessionNotFound must recognise")
	}
}

func TestZeroclawBlockedArgs(t *testing.T) {
	t.Parallel()
	if _, ok := zeroclawBlockedArgs["acp"]; !ok {
		t.Fatal("expected acp to be in zeroclawBlockedArgs")
	}
	if zeroclawBlockedArgs["acp"] != blockedStandalone {
		t.Fatalf("expected acp to be blockedStandalone, got %v", zeroclawBlockedArgs["acp"])
	}
	for _, flag := range []string{"--help", "-h", "login", "--login", "--auth"} {
		if _, ok := zeroclawBlockedArgs[flag]; !ok {
			t.Fatalf("expected %s to be in zeroclawBlockedArgs", flag)
		}
	}
}

func TestZeroclawListModels(t *testing.T) {
	t.Parallel()
	// ZeroClaw's session/new advertises no catalog and it has no
	// session-scoped model selection to consume one, so ListModels must
	// return an empty catalog WITHOUT spawning a discovery subprocess.
	//
	// Point it at a real, executable fake that records its own invocation: a
	// non-existent path cannot guard this, because the removed discovery
	// helper also returned an empty catalog when the binary was missing.
	dir := t.TempDir()
	marker := filepath.Join(dir, "invoked")
	bin := writeFakeZeroclawScript(t, "#!/bin/sh\ntouch '"+marker+"'\nexit 0\n")

	cat, err := ListModels(context.Background(), "zeroclaw", Command{Path: bin})
	if err != nil {
		t.Fatalf("zeroclaw ListModels should not error, got: %v", err)
	}
	if len(cat.Models) != 0 {
		t.Fatalf("zeroclaw ListModels should return empty catalog, got %d models", len(cat.Models))
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("zeroclaw ListModels executed the CLI; it must return an empty catalog without spawning a discovery subprocess")
	}
}

func TestZeroclawModelSelectionUnsupported(t *testing.T) {
	t.Parallel()
	if ModelSelectionSupported("zeroclaw") {
		t.Fatal("ModelSelectionSupported(zeroclaw) should return false — its ACP server has no session/set_model and no handler reads a model param, so the model comes from the ZeroClaw agent profile")
	}
	// Other providers should remain supported.
	if !ModelSelectionSupported("claude") {
		t.Fatal("ModelSelectionSupported(claude) should remain true")
	}
}

// TestZeroclawDoesNotAttemptModelSelection is the regression for the shipped
// bug: the backend used to send session/set_model whenever opts.Model was set
// and fail the whole run on its error. ZeroClaw dispatches no such method —
// the real binary answers -32601, which the fake reproduces through its
// default arm — so a configured model turned every run into a hard failure
// before session/prompt was ever sent.
func TestZeroclawDoesNotAttemptModelSelection(t *testing.T) {
	t.Parallel()
	bin := writeFakeZeroclawScript(t, fakeZeroclawACPScript())
	reqFile := filepath.Join(t.TempDir(), "requests.txt")

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	b, err := New("zeroclaw", Config{
		ExecutablePath: bin,
		Logger:         logger,
		Env:            map[string]string{"ZEROCLAW_REQUESTS_FILE": reqFile},
	})
	if err != nil {
		t.Fatalf("New(zeroclaw) error: %v", err)
	}

	session, err := b.Execute(context.Background(), "test prompt", ExecOptions{
		Cwd:   t.TempDir(),
		Model: "zeroclaw-large",
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	for range session.Messages {
	}

	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("a configured model must not fail the run, got status=%q error=%q", result.Status, result.Error)
	}
	assertNoRecordedFrame(t, reqFile, "session/set_model")
	// The model actually used is ZeroClaw's own, so usage is attributed to
	// what initialize reported rather than to the ignored request.
	if _, ok := result.Usage["llama3.2"]; !ok {
		t.Fatalf("expected usage attributed to the runtime's model, got %+v", result.Usage)
	}
}

// TestZeroclawDoesNotForwardMcpServers pins that a saved mcp_config never
// reaches the wire. ZeroClaw reads MCP only from its own config-dir — no ACP
// handler looks at `params.mcpServers` — so forwarding would be theatre, and
// the failure must stay non-fatal because a stale value must not brick a task.
func TestZeroclawDoesNotForwardMcpServers(t *testing.T) {
	t.Parallel()
	bin := writeFakeZeroclawScript(t, fakeZeroclawACPScript())
	reqFile := filepath.Join(t.TempDir(), "requests.txt")

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	b, err := New("zeroclaw", Config{
		ExecutablePath: bin,
		Logger:         logger,
		Env:            map[string]string{"ZEROCLAW_REQUESTS_FILE": reqFile},
	})
	if err != nil {
		t.Fatalf("New(zeroclaw) error: %v", err)
	}

	session, err := b.Execute(context.Background(), "test prompt", ExecOptions{
		Cwd: t.TempDir(),
		McpConfig: json.RawMessage(`{"mcpServers":{"probe-stdio":{"command":"/bin/sh","args":["-c","true"]},` +
			`"probe-http":{"type":"http","url":"http://127.0.0.1:59999/mcp"}}}`),
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	for range session.Messages {
	}

	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("a stale mcp_config must not fail the run, got status=%q error=%q", result.Status, result.Error)
	}

	frame := findRecordedFrame(t, reqFile, "session/new")
	params, _ := frame["params"].(map[string]any)
	servers, ok := params["mcpServers"].([]any)
	if !ok {
		t.Fatalf("session/new must carry a spec-shaped mcpServers array, got %#v", params["mcpServers"])
	}
	if len(servers) != 0 {
		t.Fatalf("session/new must send an empty mcpServers array, got %#v", servers)
	}
	raw, err := os.ReadFile(reqFile)
	if err != nil {
		t.Fatalf("read requests file: %v", err)
	}
	for _, name := range []string{"probe-stdio", "probe-http"} {
		if strings.Contains(string(raw), name) {
			t.Fatalf("MCP server %q leaked onto the wire:\n%s", name, raw)
		}
	}
}

// TestZeroclawSessionNewOmitsAgentAliasWhenUnset guards ZeroClaw's
// sole-agent auto-select. When the operator configured no alias the key must
// be absent entirely: a hardcoded guess such as "default" turns a working
// single-agent install into `Unknown agent \`default\“.
func TestZeroclawSessionNewOmitsAgentAliasWhenUnset(t *testing.T) {
	t.Parallel()
	bin := writeFakeZeroclawScript(t, fakeZeroclawACPScript())
	reqFile := filepath.Join(t.TempDir(), "requests.txt")

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	b, err := New("zeroclaw", Config{
		ExecutablePath: bin,
		Logger:         logger,
		Env:            map[string]string{"ZEROCLAW_REQUESTS_FILE": reqFile},
	})
	if err != nil {
		t.Fatalf("New(zeroclaw) error: %v", err)
	}

	// A whitespace-only alias counts as unset, matching ZeroClaw's own
	// trim-and-drop-empty handling of the param.
	session, err := b.Execute(context.Background(), "test prompt", ExecOptions{
		Cwd:        t.TempDir(),
		CustomArgs: []string{"--agent", "   "},
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	for range session.Messages {
	}
	if result := <-session.Result; result.Status != "completed" {
		t.Fatalf("expected completed, got status=%q error=%q", result.Status, result.Error)
	}

	frame := findRecordedFrame(t, reqFile, "session/new")
	params, _ := frame["params"].(map[string]any)
	if _, ok := params["agentAlias"]; ok {
		t.Fatalf("session/new must omit agentAlias when none is configured, got %#v", params)
	}
}

// TestZeroclawSessionNewSendsConfiguredAgentAlias covers the multi-agent
// case. `zeroclaw acp` has no --agent flag — clap aborts on one — so the alias
// is lifted out of custom_args and travels as a session/new param instead.
func TestZeroclawSessionNewSendsConfiguredAgentAlias(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "separate value", args: []string{"--agent", "myagent"}},
		{name: "inline value", args: []string{"--agent=myagent"}},
		{name: "long spelling", args: []string{"--agent-alias", "myagent"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			bin := writeFakeZeroclawScript(t, fakeZeroclawACPScript())
			reqFile := filepath.Join(t.TempDir(), "requests.txt")

			logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
			b, err := New("zeroclaw", Config{
				ExecutablePath: bin,
				Logger:         logger,
				Env:            map[string]string{"ZEROCLAW_REQUESTS_FILE": reqFile},
			})
			if err != nil {
				t.Fatalf("New(zeroclaw) error: %v", err)
			}

			session, err := b.Execute(context.Background(), "test prompt", ExecOptions{
				Cwd:        t.TempDir(),
				CustomArgs: tc.args,
			})
			if err != nil {
				t.Fatalf("Execute error: %v", err)
			}
			for range session.Messages {
			}
			if result := <-session.Result; result.Status != "completed" {
				t.Fatalf("expected completed, got status=%q error=%q", result.Status, result.Error)
			}

			frame := findRecordedFrame(t, reqFile, "session/new")
			params, _ := frame["params"].(map[string]any)
			if got := params["agentAlias"]; got != "myagent" {
				t.Fatalf("expected agentAlias=myagent on session/new, got %#v", params)
			}
		})
	}
}

// TestZeroclawAgentAliasNeverReachesArgv is the other half of the alias
// plumbing: `zeroclaw acp --agent x` dies at clap argument parsing, so the
// tokens must be consumed, not forwarded.
func TestZeroclawAgentAliasNeverReachesArgv(t *testing.T) {
	t.Parallel()
	alias, rest := takeZeroclawAgentAlias([]string{"--log-level", "debug", "--agent", "myagent", "--verbose"})
	if alias != "myagent" {
		t.Fatalf("expected alias myagent, got %q", alias)
	}
	want := []string{"--log-level", "debug", "--verbose"}
	if strings.Join(rest, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("expected the alias tokens to be consumed, got %v want %v", rest, want)
	}

	// A copy arriving through any other path must still be stripped rather
	// than crashing the CLI.
	for _, flag := range []string{"--agent", "--agent-alias"} {
		if zeroclawBlockedArgs[flag] != blockedWithValue {
			t.Fatalf("expected %s to be blockedWithValue in zeroclawBlockedArgs", flag)
		}
	}
}

// TestZeroclawSessionNewMissingAliasErrorIsActionable covers the install
// ZeroClaw refuses outright: 2+ agents (or none) with no [acp].default_agent.
// The bare RPC error names a param that has no CLI flag, so the backend has to
// say where the alias can actually come from.
func TestZeroclawSessionNewMissingAliasErrorIsActionable(t *testing.T) {
	t.Parallel()
	bin := writeFakeZeroclawScript(t, fakeZeroclawACPScript())

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	b, err := New("zeroclaw", Config{
		ExecutablePath: bin,
		Logger:         logger,
		Env:            map[string]string{"ZEROCLAW_REQUIRE_AGENT_ALIAS": "1"},
	})
	if err != nil {
		t.Fatalf("New(zeroclaw) error: %v", err)
	}

	session, err := b.Execute(context.Background(), "test prompt", ExecOptions{Cwd: t.TempDir()})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	for range session.Messages {
	}

	result := <-session.Result
	if result.Status != "failed" {
		t.Fatalf("expected failed, got status=%q error=%q", result.Status, result.Error)
	}
	for _, want := range []string{"--agent", "acp].default_agent"} {
		if !strings.Contains(result.Error, want) {
			t.Fatalf("expected the error to mention %q, got %q", want, result.Error)
		}
	}
}

// TestZeroclawTimeout tests that a context timeout during session/new is
// reported as status=timeout. The fake script responds to initialize
// immediately, then sleeps 30s on session/new so the 5s context deadline
// expires during the session/new RPC.
func TestZeroclawTimeout(t *testing.T) {
	t.Parallel()

	script := `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      sleep 30
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_late"}}\n' "$id"
      ;;
    *)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"method not found"}}\n' "$id"
      ;;
  esac
done`

	bin := writeFakeZeroclawScript(t, script)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	b, err := New("zeroclaw", Config{
		ExecutablePath: bin,
		Logger:         logger,
	})
	if err != nil {
		t.Fatalf("New(zeroclaw) error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	session, err := b.Execute(ctx, "test prompt", ExecOptions{
		Cwd: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	for range session.Messages {
	}

	result := <-session.Result
	if result.Status != "timeout" {
		t.Fatalf("expected timeout, got status=%q error=%q", result.Status, result.Error)
	}
}

// TestZeroclawSessionResumeTransientError verifies that a transient
// network/handshake error on session/resume does NOT set ResumeRejected=true,
// matching the invariant documented in grok.go and qwenpaw_test.go.
//
// The code here is -32000, the same one ZeroClaw uses for SESSION_NOT_FOUND,
// so this also pins that isACPSessionNotFound still decides on the wording:
// recognising the code alone would throw away a live session on a rate limit.
func TestZeroclawSessionResumeTransientError(t *testing.T) {
	t.Parallel()

	script := `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true}}}\n' "$id"
      ;;
    *'"method":"session/resume"'*)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32000,"message":"rate limit exceeded"}}\n' "$id"
      exit 0
      ;;
    *)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"method not found"}}\n' "$id"
      ;;
  esac
done
`
	bin := writeFakeZeroclawScript(t, script)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	b, err := New("zeroclaw", Config{
		ExecutablePath: bin,
		Logger:         logger,
	})
	if err != nil {
		t.Fatalf("New(zeroclaw) error: %v", err)
	}

	ctx := context.Background()
	session, err := b.Execute(ctx, "test prompt", ExecOptions{
		Cwd:             t.TempDir(),
		ResumeSessionID: "ses_transient",
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	for range session.Messages {
	}

	result := <-session.Result
	if result.Status != "failed" {
		t.Fatalf("expected failed, got status=%q error=%q", result.Status, result.Error)
	}
	if !strings.Contains(result.Error, "session/resume failed") {
		t.Fatalf("expected session/resume failed error, got %q", result.Error)
	}
	if result.ResumeRejected {
		t.Fatal("expected ResumeRejected=false on transient resume error")
	}
}
