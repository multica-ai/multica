package agent

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func testSkillInvocation(t *testing.T) *SkillInvocation {
	t.Helper()
	return &SkillInvocation{
		Prompt:         "Platform context\nUser message:\n/compact keep this text\nAttachments: file-id",
		PlatformPrompt: "Platform context\nAttachments: file-id",
		UserMessage:    "/compact keep this text",
		Skills: []ExplicitSkill{
			{Name: "first-multica", Directory: filepath.Join(t.TempDir(), "first-multica"), Body: "FIRST REQUIREMENT"},
			{Name: "second", Directory: filepath.Join(t.TempDir(), "second"), Body: "SECOND REQUIREMENT"},
		},
	}
}

// TestExplicitSkillsIssueTurnClaudeBlocks pins the issue-comment shape: the
// daemon keeps the complete comment prompt as PlatformPrompt (issue context,
// thread metadata, --parent reply routing) and carries the stripped member
// comment as UserMessage, so the trailing slash block never drops the reply
// routing or ships an empty user ask.
func TestExplicitSkillsIssueTurnClaudeBlocks(t *testing.T) {
	s := &SkillInvocation{
		Prompt:      "Issue context\n[NEW COMMENT]\n> please review\nreply with --parent comment-trigger",
		UserMessage: "please review",
		Skills: []ExplicitSkill{
			{Name: "review-code", Directory: filepath.Join(t.TempDir(), "review-code"), Body: "FIRST BODY"},
			{Name: "second", Directory: filepath.Join(t.TempDir(), "second"), Body: "SECOND BODY"},
		},
	}
	s.PlatformPrompt = s.Prompt
	blocks := s.claudeTextBlocks(map[string]bool{"review-code": true})
	if len(blocks) != 2 || blocks[1] != "/review-code please review" {
		t.Fatalf("trailing block must carry the member ask: %q", blocks)
	}
	if !strings.Contains(blocks[0], "--parent comment-trigger") || !strings.Contains(blocks[0], "SECOND BODY") {
		t.Fatalf("platform block lost routing or injected skill: %q", blocks[0])
	}
}

// TestClaudeSkillCommandBlockSpacing pins the trailing block's shape for
// capsule-only turns: no trailing space once the member ask strips to
// nothing, and exactly one separating space for a real ask.
func TestClaudeSkillCommandBlockSpacing(t *testing.T) {
	s := &SkillInvocation{
		PlatformPrompt: "platform",
		Skills:         []ExplicitSkill{{Name: "review-code", Directory: filepath.Join(t.TempDir(), "review-code"), Body: "BODY"}},
	}
	available := map[string]bool{"review-code": true}
	s.UserMessage = " "
	if blocks := s.claudeTextBlocks(available); blocks[1] != "/review-code" {
		t.Fatalf("whitespace-only ask must not leave a trailing space: %q", blocks[1])
	}
	s.UserMessage = "  padded ask  "
	if blocks := s.claudeTextBlocks(available); blocks[1] != "/review-code padded ask" {
		t.Fatalf("ask must join the command with exactly one space: %q", blocks[1])
	}
}

func TestExplicitSkillsProviderInputs(t *testing.T) {
	s := testSkillInvocation(t)
	blocks := s.claudeTextBlocks(map[string]bool{"first-multica": true})
	wire, err := buildClaudeInput(blocks...)
	if err != nil {
		t.Fatal(err)
	}
	var input struct {
		Message struct{ Content []struct{ Type, Text string } }
	}
	if err := json.Unmarshal(wire, &input); err != nil {
		t.Fatal(err)
	}
	if len(input.Message.Content) != 2 || input.Message.Content[1].Text != "/first-multica /compact keep this text" {
		t.Fatalf("Claude command must start last block: %s", wire)
	}
	if !strings.Contains(blocks[0], "Attachments: file-id") || !strings.Contains(blocks[0], "SECOND REQUIREMENT") || strings.Contains(blocks[0], "FIRST REQUIREMENT") {
		t.Fatalf("Claude platform block=%q", blocks[0])
	}
	agy := s.antigravityPrompt(true)
	if !strings.HasPrefix(agy, "/first-multica\nPlatform context") || !strings.Contains(agy, "SECOND REQUIREMENT") || strings.Contains(agy, "FIRST REQUIREMENT") {
		t.Fatalf("agy prompt=%q", agy)
	}
	catalog, _ := json.Marshal(map[string]any{"data": []any{map[string]any{"skills": []any{
		map[string]any{"name": s.Skills[0].Name, "path": filepath.Join(s.Skills[0].Directory, "SKILL.md"), "enabled": true},
		map[string]any{"name": s.Skills[1].Name, "path": filepath.Join(s.Skills[1].Directory, "SKILL.md"), "enabled": true},
	}}}})
	codex := s.codexPrompt(catalog)
	if !strings.Contains(codex, "$first-multica\n") || !strings.Contains(codex, "$second\n") || strings.Contains(codex, "REQUIREMENT") || !strings.Contains(codex, s.UserMessage) {
		t.Fatalf("Codex prompt=%q", codex)
	}
}

func TestExplicitSkillsMissingCapabilitiesFallBack(t *testing.T) {
	s := testSkillInvocation(t)
	for _, catalog := range []string{`{}`, `{"data":[]}`, `{"data":[{"skills":[{"name":"first-multica","path":"/some-other/SKILL.md","enabled":true}]}]}`, `not json`} {
		if got := s.codexPrompt(json.RawMessage(catalog)); got != s.FallbackPrompt() {
			t.Fatalf("unsupported Codex: %q", got)
		}
	}
	for _, commands := range []map[string]bool{nil, {}, {"second": true}} {
		blocks := s.claudeTextBlocks(commands)
		if len(blocks) != 1 || blocks[0] != s.FallbackPrompt() {
			t.Fatalf("unsupported Claude: %v", blocks)
		}
	}
	if s.antigravityPrompt(false) != s.FallbackPrompt() {
		t.Fatal("unsupported agy lost fallback")
	}
	for _, want := range []string{s.UserMessage, "FIRST REQUIREMENT", "SECOND REQUIREMENT", s.Skills[0].Directory, "user explicitly selected"} {
		if !strings.Contains(s.FallbackPrompt(), want) {
			t.Errorf("fallback lost %q", want)
		}
	}
}

func TestCodexExplicitSkillDisabledOrMalformedEntryFallsBack(t *testing.T) {
	s := testSkillInvocation(t)
	for _, enabled := range []any{false, nil, "true"} {
		catalog, _ := json.Marshal(map[string]any{"data": []any{map[string]any{"skills": []any{map[string]any{"name": s.Skills[0].Name, "path": filepath.Join(s.Skills[0].Directory, "SKILL.md"), "enabled": enabled}}}}})
		if got := s.codexPrompt(catalog); got != s.FallbackPrompt() {
			t.Fatalf("disabled/malformed skill invoked: %q", got)
		}
	}
}

func TestAntigravityExplicitSkillCapabilityProbe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	for _, tc := range []struct {
		name, script   string
		disabled, want bool
	}{
		{"supported", "echo --disable-slash-commands", false, true},
		{"old CLI", "echo usage", false, false},
		{"failed help", "exit 2", false, false},
		{"explicitly disabled", "echo --disable-slash-commands", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "fake-agy")
			writeTestExecutable(t, path, []byte("#!/bin/sh\n"+tc.script+"\n"))
			opts := ExecOptions{}
			if tc.disabled {
				opts.CustomArgs = []string{"--disable-slash-commands"}
			}
			if got := supportsAntigravitySlashSkills(t.Context(), Config{Logger: slog.Default()}, path, opts); got != tc.want {
				t.Fatalf("capability=%v want=%v", got, tc.want)
			}
		})
	}
}

func TestClaudeExplicitSkillsExecute(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	for _, tc := range []struct {
		name, description string
		rejected, native  bool
	}{
		{name: "project command", description: "Bound skill (project)", native: true},
		{name: "personal skill shadows project", description: "Personal review (user)"},
		{name: "plugin command", description: "Plugin review (plugin)"},
		{name: "unknown command origin", description: "Review code"},
		{name: "missing command origin"},
		{name: "unsupported initialization", rejected: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := testSkillInvocation(t)
			dir := t.TempDir()
			capture := filepath.Join(dir, "input.json")
			path := filepath.Join(dir, "fake-claude")
			response := `{"subtype":"error","request_id":"multica-supplement-initialize","error":"unsupported"}`
			if !tc.rejected {
				response = fmt.Sprintf(`{"subtype":"success","request_id":"multica-supplement-initialize","response":{"commands":[{"name":"first-multica","description":%q}]}}`, tc.description)
			}
			script := "#!/bin/sh\nIFS= read -r init\nprintf '%s\\n' \"$init\" > \"$INIT_CAPTURE\"\nprintf '%s\\n' '{\"type\":\"control_response\",\"response\":" + response + "}'\nIFS= read -r input\nprintf '%s\\n' \"$input\" > \"$CAPTURE\"\nprintf '%s\\n' '{\"type\":\"result\",\"subtype\":\"success\",\"result\":\"done\"}'\ncat >/dev/null\n"
			writeTestExecutable(t, path, []byte(script))
			initCapture := filepath.Join(dir, "initialize.json")
			backend := &claudeBackend{cfg: Config{ExecutablePath: path, Logger: slog.Default(), Env: map[string]string{"CAPTURE": capture, "INIT_CAPTURE": initCapture, "IS_SANDBOX": "1"}}}
			session, err := backend.Execute(t.Context(), s.FallbackPrompt(), ExecOptions{SkillInvocation: s, Timeout: 10 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			for range session.Messages {
			}
			if result := <-session.Result; result.Status != "completed" {
				t.Fatalf("result=%+v", result)
			}
			wire, err := os.ReadFile(capture)
			if err != nil {
				t.Fatal(err)
			}
			var input struct {
				Message struct{ Content []struct{ Text string } }
			}
			if err := json.Unmarshal(wire, &input); err != nil {
				t.Fatal(err)
			}
			initWire, err := os.ReadFile(initCapture)
			if err != nil {
				t.Fatal(err)
			}
			var init struct {
				Request struct{ Hooks map[string]json.RawMessage }
			}
			if err := json.Unmarshal(initWire, &init); err != nil {
				t.Fatal(err)
			}
			if len(init.Request.Hooks) != 0 {
				t.Errorf("skill discovery enabled supplement hooks: %s", initWire)
			}
			if tc.native {
				if len(input.Message.Content) != 2 || !strings.HasPrefix(input.Message.Content[1].Text, "/first-multica ") {
					t.Fatalf("native input=%s", wire)
				}
			} else if len(input.Message.Content) != 1 || input.Message.Content[0].Text != s.FallbackPrompt() {
				t.Fatalf("fallback input=%s", wire)
			}
		})
	}
}

func TestCodexExplicitSkillsExecuteCapabilityFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	for _, supported := range []bool{true, false} {
		t.Run(fmt.Sprintf("supported=%t", supported), func(t *testing.T) {
			s := testSkillInvocation(t)
			response := `{"jsonrpc":"2.0","id":3,"error":{"code":-32601,"message":"Method not found"}}`
			if supported {
				catalog, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 3, "result": map[string]any{"data": []any{map[string]any{"skills": []any{map[string]any{"name": s.Skills[0].Name, "path": filepath.Join(s.Skills[0].Directory, "SKILL.md"), "enabled": true}}}}}})
				if err != nil {
					t.Fatal(err)
				}
				response = string(catalog)
			}
			path := writeFakeCodexAppServer(t, `read line
echo '{"jsonrpc":"2.0","id":1,"result":{}}'
read line
read line
echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"skill-thread"}}}'
read line
echo '`+response+`'
IFS= read -r line
printf '%s\n' "$line" > "$CAPTURE"
echo '{"jsonrpc":"2.0","id":4,"result":{"turn":{"id":"skill-turn"}}}'
echo '{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"skill-thread","turn":{"id":"skill-turn"}}}'
echo '{"jsonrpc":"2.0","method":"item/completed","params":{"threadId":"skill-thread","item":{"type":"agentMessage","id":"response","text":"Done"}}}'
echo '{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"skill-thread","turn":{"id":"skill-turn","status":"completed"}}}'
`)
			capture := filepath.Join(t.TempDir(), "input.json")
			backend, err := New("codex", Config{ExecutablePath: path, Logger: slog.Default(), Env: map[string]string{"CAPTURE": capture}})
			if err != nil {
				t.Fatal(err)
			}
			session, err := backend.Execute(t.Context(), s.FallbackPrompt(), ExecOptions{SkillInvocation: s, Cwd: t.TempDir(), Timeout: 10 * time.Second, ResumeExpected: true, ResumeContinuityNotice: "History unavailable.\n"})
			if err != nil {
				t.Fatal(err)
			}
			for range session.Messages {
			}
			if result := <-session.Result; result.Status != "completed" {
				t.Fatalf("result=%+v", result)
			}
			wire, err := os.ReadFile(capture)
			if err != nil {
				t.Fatal(err)
			}
			var request struct {
				Method string
				Params struct{ Input []struct{ Text string } }
			}
			if err := json.Unmarshal(wire, &request); err != nil {
				t.Fatal(err)
			}
			if request.Method != "turn/start" || len(request.Params.Input) != 1 {
				t.Fatalf("wrong input: %s", wire)
			}
			got := request.Params.Input[0].Text
			for _, want := range []string{s.UserMessage, "History unavailable.", "SECOND REQUIREMENT"} {
				if !strings.Contains(got, want) {
					t.Fatalf("lost %q: %s", want, got)
				}
			}
			if supported {
				if !strings.Contains(got, "$first-multica") || strings.Contains(got, "FIRST REQUIREMENT") {
					t.Fatalf("missing native input: %s", got)
				}
			} else if !strings.Contains(got, "FIRST REQUIREMENT") || strings.Contains(got, "$first-multica") {
				t.Fatalf("missing fallback: %s", got)
			}
		})
	}
}

func TestInjectedSkillsReachACPWithRuntimeBrief(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	for _, provider := range []string{"kimi", "hermes"} {
		t.Run(provider, func(t *testing.T) {
			s := testSkillInvocation(t)
			dir := t.TempDir()
			path, capture := filepath.Join(dir, "fake-acp"), filepath.Join(dir, "prompt.json")
			writeTestExecutable(t, path, []byte(`#!/bin/sh
read line
echo '{"jsonrpc":"2.0","id":0,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":false}}}'
read line
echo '{"jsonrpc":"2.0","id":1,"result":{"sessionId":"skills-acp"}}'
IFS= read -r line
printf '%s\n' "$line" > "$CAPTURE"
echo '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"skills-acp","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Done"}}}}'
echo '{"jsonrpc":"2.0","id":2,"result":{"stopReason":"end_turn"}}'
`))
			backend, err := New(provider, Config{ExecutablePath: path, Logger: slog.Default(), Env: map[string]string{"CAPTURE": capture}})
			if err != nil {
				t.Fatal(err)
			}
			session, err := backend.Execute(t.Context(), s.FallbackPrompt(), ExecOptions{SkillInvocation: s, Cwd: dir, SystemPrompt: "RUNTIME BRIEF", Timeout: 8 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			for range session.Messages {
			}
			if result := <-session.Result; result.Status != "completed" {
				t.Fatalf("result=%+v", result)
			}
			wire, err := os.ReadFile(capture)
			if err != nil {
				t.Fatal(err)
			}
			var request struct {
				Method string
				Params struct{ Prompt []struct{ Type, Text string } }
			}
			if err := json.Unmarshal(wire, &request); err != nil {
				t.Fatal(err)
			}
			if request.Method != "session/prompt" || len(request.Params.Prompt) != 1 || request.Params.Prompt[0].Type != "text" {
				t.Fatalf("wrong ACP input: %s", wire)
			}
			want := s.FallbackPrompt()
			if provider == "kimi" {
				want = "RUNTIME BRIEF\n\n---\n\n" + want
			}
			if request.Params.Prompt[0].Text != want {
				t.Fatalf("lost injected body/brief: %s", wire)
			}
		})
	}
}

func TestAntigravityExplicitSkillsExecuteFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	for _, supported := range []bool{true, false} {
		t.Run(fmt.Sprintf("supported=%t", supported), func(t *testing.T) {
			s := testSkillInvocation(t)
			dir := t.TempDir()
			path, capture := filepath.Join(dir, "fake-agy"), filepath.Join(dir, "prompt.txt")
			help := "usage"
			if supported {
				help = "--disable-slash-commands"
			}
			writeTestExecutable(t, path, []byte("#!/bin/sh\nif [ \"$1\" = '--help' ]; then echo '"+help+"'; exit 0; fi\nprintf '%s' \"$2\" > \"$CAPTURE\"\necho Done\n"))
			backend, err := New("antigravity", Config{ExecutablePath: path, Logger: slog.Default(), Env: map[string]string{"CAPTURE": capture}})
			if err != nil {
				t.Fatal(err)
			}
			session, err := backend.Execute(t.Context(), s.FallbackPrompt(), ExecOptions{SkillInvocation: s, Cwd: dir, Timeout: 8 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			for range session.Messages {
			}
			if result := <-session.Result; result.Status != "completed" {
				t.Fatalf("result=%+v", result)
			}
			prompt, err := os.ReadFile(capture)
			if err != nil {
				t.Fatal(err)
			}
			if string(prompt) != s.antigravityPrompt(supported) {
				t.Fatalf("wrong agy input: %s", prompt)
			}
		})
	}
}

func TestClaudeSkillInitializationUsesHandshakeTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	path := filepath.Join(t.TempDir(), "fake-claude")
	// Deliberately never answer initialize. The configured budget must release
	// skill discovery without waiting for the whole execution timeout.
	writeTestExecutable(t, path, []byte(`#!/bin/sh
IFS= read -r init
IFS= read -r input
printf '%s\n' '{"type":"result","subtype":"success","result":"fallback completed"}'
cat >/dev/null
`))
	backend := &claudeBackend{cfg: Config{ExecutablePath: path, Logger: slog.Default(), Env: map[string]string{"IS_SANDBOX": "1"}}}
	skill := testSkillInvocation(t)
	session, err := backend.Execute(t.Context(), skill.FallbackPrompt(), ExecOptions{
		SkillInvocation: skill, HandshakeTimeout: 30 * time.Millisecond, Timeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	for range session.Messages {
	}
	if result := <-session.Result; result.Status != "completed" || result.Output != "fallback completed" {
		t.Fatalf("skill-only handshake did not honor its budget: %+v", result)
	}
}

func TestClaudeSkillInitializationReleasesOnEarlyExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	path := filepath.Join(t.TempDir(), "fake-claude")
	// Exit before answering the initialize request: the shutdown path must
	// release the command-discovery handshake, not the timeout budget.
	writeTestExecutable(t, path, []byte("#!/bin/sh\nexit 1\n"))
	backend := &claudeBackend{cfg: Config{ExecutablePath: path, Logger: slog.Default(), Env: map[string]string{"IS_SANDBOX": "1"}}}
	skill := testSkillInvocation(t)
	started := time.Now()
	session, err := backend.Execute(t.Context(), skill.FallbackPrompt(), ExecOptions{
		SkillInvocation: skill, HandshakeTimeout: 5 * time.Second, Timeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	for range session.Messages {
	}
	result := <-session.Result
	if result.Status == "completed" {
		t.Fatalf("early exit must fail the run: %+v", result)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("early exit waited for the handshake budget: %s (%+v)", elapsed, result)
	}
}
