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
// session id. initialize.defaultModel is deliberately not used for usage:
// it is the process-global first provider model, not the model selected by
// the agent alias bound during session/new.
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
	if _, ok := result.Usage["unknown"]; !ok {
		t.Fatalf("expected usage without a per-turn model id to stay unknown, got %+v", result.Usage)
	}
}

// TestZeroclawPromptModelMetadataControlsUsageAttribution proves the prompt
// result is authoritative when a future ZeroClaw reports a per-turn model id.
func TestZeroclawPromptModelMetadataControlsUsageAttribution(t *testing.T) {
	t.Parallel()
	script := strings.Replace(
		fakeZeroclawACPScript(),
		`"result":{"stopReason":"end_turn","usage":{`,
		`"result":{"stopReason":"end_turn","_meta":{"modelId":"selected-model"},"usage":{`,
		1,
	)
	if !strings.Contains(script, `"modelId":"selected-model"`) {
		t.Fatal("fake script rewrite failed: prompt model metadata not inserted")
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
	if _, ok := result.Usage["selected-model"]; !ok {
		t.Fatalf("expected usage attributed to prompt model metadata, got %+v", result.Usage)
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
	resumeFrame := findRecordedFrame(t, reqFile, "session/resume")
	resumeParams, _ := resumeFrame["params"].(map[string]any)
	if len(resumeParams) != 1 || resumeParams["sessionId"] != "ses_existing" {
		t.Fatalf("session/resume must send only the session id ZeroClaw reads, got %#v", resumeParams)
	}
	assertNoRecordedFrame(t, reqFile, "session/load")
	if strings.Contains(requests, `"method":"session/new"`) {
		t.Fatalf("resume must not call session/new, got requests:\n%s", requests)
	}
}

// TestZeroclawResumeCapabilityUnavailableReportsRejection covers ZeroClaw's
// read-only/unwritable persistence fallback. In that mode initialize omits
// sessionCapabilities.resume. Report positive rejection evidence so the
// daemon owns the fresh retry, continuity notice, prompt rebuild, runtime
// config rewrite, and retirement of the abandoned session id.
func TestZeroclawResumeCapabilityUnavailableReportsRejection(t *testing.T) {
	t.Parallel()
	script := strings.Replace(
		fakeZeroclawACPScript(),
		`"sessionCapabilities":{"close":{},"resume":{}}`,
		`"sessionCapabilities":{"close":{}}`,
		1,
	)
	if strings.Contains(script, `"resume":{}`) {
		t.Fatal("fake script rewrite failed: resume capability still present")
	}
	bin := writeFakeZeroclawScript(t, script)
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
		Cwd:             t.TempDir(),
		ResumeSessionID: "ses_existing",
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	for range session.Messages {
	}

	result := <-session.Result
	if result.Status != "failed" || !result.ResumeRejected || result.SessionID != "" {
		t.Fatalf("expected failed resume rejection without a replacement session, got status=%q session=%q rejected=%v error=%q", result.Status, result.SessionID, result.ResumeRejected, result.Error)
	}
	if !strings.Contains(result.Error, "session/resume unavailable") {
		t.Fatalf("expected an actionable resume-unavailable error, got %q", result.Error)
	}
	assertNoRecordedFrame(t, reqFile, "session/resume")
	assertNoRecordedFrame(t, reqFile, "session/new")
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

func TestSelectZeroclawPermissionOptionRejectsLegacyChoice(t *testing.T) {
	t.Parallel()

	question := json.RawMessage(`{"options":[{"optionId":"choice-0","kind":"allow_once"},{"optionId":"choice-1","kind":"allow_once"},{"optionId":"choice-2","kind":"reject_once"}]}`)
	optionID, grant, ok := selectZeroclawPermissionOption(question)
	if ok || grant || optionID != "" {
		t.Fatalf("legacy choices must return no selectable outcome, got option=%q grant=%v ok=%v", optionID, grant, ok)
	}
	questionWithoutReject := json.RawMessage(`{"options":[{"optionId":"choice-0","kind":"allow_once"},{"optionId":"choice-1","kind":"allow_once"}]}`)
	if optionID, grant, ok := selectZeroclawPermissionOption(questionWithoutReject); ok || grant || optionID != "" {
		t.Fatalf("legacy choices without an offered reject must return no selectable outcome, got option=%q grant=%v ok=%v", optionID, grant, ok)
	}

	toolApproval := json.RawMessage(`{"options":[{"optionId":"allow-once","kind":"allow_once"},{"optionId":"reject-once","kind":"reject_once"}]}`)
	optionID, grant, ok = selectZeroclawPermissionOption(toolApproval)
	if !ok || !grant || optionID != "allow-once" {
		t.Fatalf("ordinary tool approval must keep the shared single-use grant policy, got option=%q grant=%v ok=%v", optionID, grant, ok)
	}
}

// TestZeroclawLegacyChoiceReturnsProtocolErrorThroughClient pins the backend
// wiring, not just the selector helper. The fake waits for the response to the
// same session/request_permission shape ZeroClaw uses for structured ask_user,
// then mirrors ZeroClaw's prompt failure after the client returns -32603.
func TestZeroclawLegacyChoiceReturnsProtocolErrorThroughClient(t *testing.T) {
	t.Parallel()
	script := `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"sessionCapabilities":{"resume":{}}}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_choice","workspaceDir":"/tmp"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","id":"zc-out-0","method":"session/request_permission","params":{"sessionId":"ses_choice","options":[{"optionId":"choice-0","kind":"allow_once"},{"optionId":"choice-1","kind":"allow_once"},{"optionId":"choice-2","kind":"reject_once"}]}}\n'
      IFS= read -r answer
      case "$answer" in
        *'"code":-32603'*) ;;
        *) exit 2 ;;
      esac
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32000,"message":"ACP request_permission failed: no auto-selectable permission option offered"}}\n' "$id"
      ;;
    *)
      exit 3
      ;;
  esac
done
`
	bin := writeFakeZeroclawScript(t, script)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	b, err := New("zeroclaw", Config{ExecutablePath: bin, Logger: logger})
	if err != nil {
		t.Fatalf("New(zeroclaw) error: %v", err)
	}

	session, err := b.Execute(context.Background(), "choose", ExecOptions{Cwd: t.TempDir()})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	for range session.Messages {
	}
	result := <-session.Result
	if result.Status != "failed" || !strings.Contains(result.Error, "ACP request_permission failed") {
		t.Fatalf("expected the legacy choice to fail through the protocol error, got status=%q error=%q", result.Status, result.Error)
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
	// The requested model is ignored, and initialize's process-global default
	// may belong to another alias, so neither is safe for attribution.
	if _, ok := result.Usage["unknown"]; !ok {
		t.Fatalf("expected usage without a per-turn model id to stay unknown, got %+v", result.Usage)
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
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true,"sessionCapabilities":{"resume":{}}}}}\n' "$id"
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
