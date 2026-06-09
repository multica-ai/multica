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

// TestBuildOpenCodeMCPConfigContent_Empty pins the early-return contract: an
// empty/nil mcp_config returns ("", nil) so the caller can skip the env
// entry entirely. Without this, an empty entry would unset whatever value
// the user had in agent.custom_env (Go's os/exec dedup keeps the last
// occurrence, including an empty string).
func TestBuildOpenCodeMCPConfigContent_Empty(t *testing.T) {
	t.Parallel()
	for _, raw := range []json.RawMessage{nil, json.RawMessage(""), json.RawMessage("null")} {
		got, err := buildOpenCodeMCPConfigContent(raw)
		if err != nil {
			t.Fatalf("err for %q: %v", string(raw), err)
		}
		if got != "" {
			t.Fatalf("expected empty content for %q, got %q", string(raw), got)
		}
	}
}

// TestBuildOpenCodeMCPConfigContent_Remote covers the Claude → OpenCode
// translation for HTTP MCP servers: the daemon receives the Claude shape
// (mcpServers with url + headers) and produces the OpenCode native shape
// (mcp with type:"remote" + url + headers).
func TestBuildOpenCodeMCPConfigContent_Remote(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{
	  "mcpServers": {
	    "mcpbase": {
	      "url": "https://mcpbase.example/multica-ai/mcp",
	      "headers": {"Authorization": "Bearer test-token"}
	    }
	  }
	}`)
	content, err := buildOpenCodeMCPConfigContent(raw)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	cfg := parseJSONString(t, content)
	mcpbase := cfg["mcp"].(map[string]any)["mcpbase"].(map[string]any)
	if got := mcpbase["type"]; got != "remote" {
		t.Fatalf("type = %v, want remote", got)
	}
	if got := mcpbase["url"]; got != "https://mcpbase.example/multica-ai/mcp" {
		t.Fatalf("url = %v", got)
	}
	if _, present := mcpbase["enabled"]; present {
		t.Fatalf("enabled should not be injected when not in source, got %v", mcpbase["enabled"])
	}
	if got := mcpbase["headers"].(map[string]any)["Authorization"]; got != "Bearer test-token" {
		t.Fatalf("Authorization header = %v", got)
	}
}

// TestBuildOpenCodeMCPConfigContent_Local pins the Claude → OpenCode
// translation for subprocess MCP servers: `command` is normalised to an
// array, `env` is renamed to `environment`, and `type: "local"` is added.
func TestBuildOpenCodeMCPConfigContent_Local(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{"mcpServers":{"local":{"command":"node","args":["server.js"],"env":{"TOKEN":"x"}}}}`)
	content, err := buildOpenCodeMCPConfigContent(raw)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	cfg := parseJSONString(t, content)
	local := cfg["mcp"].(map[string]any)["local"].(map[string]any)
	if got := local["type"]; got != "local" {
		t.Fatalf("type = %v, want local", got)
	}
	command, ok := local["command"].([]any)
	if !ok || len(command) != 2 || command[0] != "node" || command[1] != "server.js" {
		t.Fatalf("command = %#v, want [node server.js]", local["command"])
	}
	env, ok := local["environment"].(map[string]any)
	if !ok || env["TOKEN"] != "x" {
		t.Fatalf("environment = %#v, want {TOKEN:x}", local["environment"])
	}
	if _, present := local["env"]; present {
		t.Fatal("legacy `env` key should have been renamed to `environment`")
	}
}

// TestBuildOpenCodeMCPConfigContent_Native pins that an mcp_config in
// OpenCode's native shape is passed through (after validation) without
// being treated like a Claude-shape payload.
func TestBuildOpenCodeMCPConfigContent_Native(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{
	  "mcp": {
	    "native": {
	      "type": "remote",
	      "url": "https://native.example/mcp",
	      "enabled": false
	    }
	  }
	}`)
	content, err := buildOpenCodeMCPConfigContent(raw)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	cfg := parseJSONString(t, content)
	native := cfg["mcp"].(map[string]any)["native"].(map[string]any)
	if got := native["enabled"]; got != false {
		t.Fatalf("enabled = %v, want false", got)
	}
	if got := native["url"]; got != "https://native.example/mcp" {
		t.Fatalf("url = %v", got)
	}
}

// TestBuildOpenCodeMCPConfigContent_NativeAcceptsAllSchemaFields pins that
// every field OpenCode's schema permits round-trips through validation
// unchanged. Each subtest exercises one of the three native variants:
// McpLocalConfig, McpRemoteConfig (with oauth as both an object and the
// `false` literal), and the bare `{enabled: bool}` override shape.
func TestBuildOpenCodeMCPConfigContent_NativeAcceptsAllSchemaFields(t *testing.T) {
	t.Parallel()

	t.Run("local with all optional fields", func(t *testing.T) {
		t.Parallel()
		raw := json.RawMessage(`{"mcp":{"x":{
			"type":"local",
			"command":["python","-m","my_mcp"],
			"environment":{"API_KEY":"secret","REGION":"us"},
			"enabled":true,
			"timeout":30000
		}}}`)
		content, err := buildOpenCodeMCPConfigContent(raw)
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		x := parseJSONString(t, content)["mcp"].(map[string]any)["x"].(map[string]any)
		if x["type"] != "local" || x["timeout"].(float64) != 30000 || x["enabled"].(bool) != true {
			t.Fatalf("local fields lost in round-trip: %#v", x)
		}
		cmd := x["command"].([]any)
		if len(cmd) != 3 || cmd[0] != "python" {
			t.Fatalf("command lost: %#v", cmd)
		}
		env := x["environment"].(map[string]any)
		if env["API_KEY"] != "secret" {
			t.Fatalf("environment lost: %#v", env)
		}
	})

	t.Run("remote with oauth object", func(t *testing.T) {
		t.Parallel()
		raw := json.RawMessage(`{"mcp":{"x":{
			"type":"remote",
			"url":"https://e.example/mcp",
			"headers":{"Authorization":"Bearer T","X-Trace":"abc"},
			"oauth":{"clientId":"cid","scope":"read","callbackPort":3000},
			"enabled":true,
			"timeout":5000
		}}}`)
		content, err := buildOpenCodeMCPConfigContent(raw)
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		x := parseJSONString(t, content)["mcp"].(map[string]any)["x"].(map[string]any)
		oauth := x["oauth"].(map[string]any)
		if oauth["clientId"] != "cid" || oauth["callbackPort"].(float64) != 3000 {
			t.Fatalf("oauth fields lost: %#v", oauth)
		}
	})

	t.Run("remote with oauth false", func(t *testing.T) {
		t.Parallel()
		raw := json.RawMessage(`{"mcp":{"x":{"type":"remote","url":"https://e/","oauth":false}}}`)
		content, err := buildOpenCodeMCPConfigContent(raw)
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		x := parseJSONString(t, content)["mcp"].(map[string]any)["x"].(map[string]any)
		// JSON `false` decodes to bool(false) when the receiver is map[string]any.
		// We just need to verify the literal survived (the validator's job is to
		// allow `false` and reject `true`; the schema-allowed-but-false case
		// falls through unchanged).
		if v, ok := x["oauth"].(bool); !ok || v {
			t.Fatalf("oauth literal `false` not preserved: %#v", x["oauth"])
		}
	})

	t.Run("bare enabled override", func(t *testing.T) {
		t.Parallel()
		// The third native variant: toggle a server inherited from
		// global/project config without redefining it. Required field
		// is `enabled`; no `type` field allowed.
		raw := json.RawMessage(`{"mcp":{"inherited":{"enabled":false}}}`)
		content, err := buildOpenCodeMCPConfigContent(raw)
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		x := parseJSONString(t, content)["mcp"].(map[string]any)["inherited"].(map[string]any)
		if v, ok := x["enabled"].(bool); !ok || v {
			t.Fatalf("override `enabled:false` lost: %#v", x)
		}
		if _, hasType := x["type"]; hasType {
			t.Fatalf("override should not have a type field: %#v", x)
		}
	})

	t.Run("bare enabled override rejects extra fields", func(t *testing.T) {
		t.Parallel()
		// The override shape is also schema-strict: extra fields without
		// type are rejected via the friendlier "missing type" message.
		raw := json.RawMessage(`{"mcp":{"x":{"enabled":true,"foo":"bar"}}}`)
		_, err := buildOpenCodeMCPConfigContent(raw)
		if err == nil {
			t.Fatal("expected validation failure for extra fields without type")
		}
		if !strings.Contains(err.Error(), "missing required field `type`") {
			t.Fatalf("expected friendly missing-type error, got %q", err.Error())
		}
	})
}

// TestBuildOpenCodeMCPConfigContent_RejectsMalformedNative covers the
// schema check: a native-shape entry without a recognised type / required
// field must be rejected before the env var is built, so OpenCode never
// receives malformed input that would silently disable the server or
// crash at startup.
func TestBuildOpenCodeMCPConfigContent_RejectsMalformedNative(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		raw  string
		want string
	}{
		// Discriminator + required fields
		{"missing type", `{"mcp":{"x":{"url":"https://e.example/mcp"}}}`, "missing required field `type`"},
		{"invalid type", `{"mcp":{"x":{"type":"bogus","url":"https://e.example/mcp"}}}`, "invalid type"},
		{"remote missing url", `{"mcp":{"x":{"type":"remote"}}}`, "remote server missing required field `url`"},
		{"local missing command", `{"mcp":{"x":{"type":"local"}}}`, "local server missing required field `command`"},
		{"entry not an object (string)", `{"mcp":{"x":"not-an-object"}}`, "entry must be a JSON object"},
		{"entry not an object (number)", `{"mcp":{"x":42}}`, "entry must be a JSON object"},
		{"entry not an object (array)", `{"mcp":{"x":["a","b"]}}`, "entry must be a JSON object"},
		{"entry not an object (null)", `{"mcp":{"x":null}}`, "entry must be a JSON object"},
		{"type field is not a string", `{"mcp":{"x":{"type":42}}}`, "`type` must be a string"},

		// command: must be []string (the headline case Bohan-J flagged)
		{"local command is string", `{"mcp":{"x":{"type":"local","command":"node"}}}`, "json: cannot unmarshal string"},
		{"local command has non-string element", `{"mcp":{"x":{"type":"local","command":["node",5]}}}`, "json: cannot unmarshal number"},
		{"local command is object", `{"mcp":{"x":{"type":"local","command":{"foo":"bar"}}}}`, "json: cannot unmarshal object"},

		// environment / headers: values must be strings
		{"local env value is number", `{"mcp":{"x":{"type":"local","command":["node"],"environment":{"PORT":3000}}}}`, "json: cannot unmarshal number"},
		{"local env value is array", `{"mcp":{"x":{"type":"local","command":["node"],"environment":{"FOO":["a"]}}}}`, "json: cannot unmarshal array"},
		{"remote header value is number", `{"mcp":{"x":{"type":"remote","url":"https://e/","headers":{"X-Limit":10}}}}`, "json: cannot unmarshal number"},
		{"remote header value is bool", `{"mcp":{"x":{"type":"remote","url":"https://e/","headers":{"X-Auth":true}}}}`, "json: cannot unmarshal bool"},

		// oauth: must be object | false (true is rejected as ambiguous)
		{"oauth is true", `{"mcp":{"x":{"type":"remote","url":"https://e/","oauth":true}}}`, "must be an object or `false`"},
		{"oauth is string", `{"mcp":{"x":{"type":"remote","url":"https://e/","oauth":"yes"}}}`, "must be an object or `false`"},
		{"oauth is number", `{"mcp":{"x":{"type":"remote","url":"https://e/","oauth":1}}}`, "must be an object or `false`"},
		{"oauth has unknown field", `{"mcp":{"x":{"type":"remote","url":"https://e/","oauth":{"foo":"bar"}}}}`, `json: unknown field "foo"`},
		{"oauth callbackPort out of range (high)", `{"mcp":{"x":{"type":"remote","url":"https://e/","oauth":{"callbackPort":70000}}}}`, "`callbackPort` must be in 1..65535"},
		{"oauth callbackPort out of range (negative)", `{"mcp":{"x":{"type":"remote","url":"https://e/","oauth":{"callbackPort":-1}}}}`, "`callbackPort` must be in 1..65535"},
		// Explicit zero must be rejected too — `*int` lets us tell
		// "absent" from "explicit 0" so the schema's `minimum: 1` is
		// honoured. Without the pointer change, Go's int zero value
		// would mask this case as "unset" and let it through.
		{"oauth callbackPort explicit zero", `{"mcp":{"x":{"type":"remote","url":"https://e/","oauth":{"callbackPort":0}}}}`, "`callbackPort` must be in 1..65535"},

		// timeout: positive integer
		{"timeout zero", `{"mcp":{"x":{"type":"local","command":["node"],"timeout":0}}}`, "`timeout` must be a positive integer"},
		{"timeout negative", `{"mcp":{"x":{"type":"remote","url":"https://e/","timeout":-1}}}`, "`timeout` must be a positive integer"},
		{"timeout fractional", `{"mcp":{"x":{"type":"local","command":["node"],"timeout":60.5}}}`, "json: cannot unmarshal number"},
		{"timeout string", `{"mcp":{"x":{"type":"remote","url":"https://e/","timeout":"60"}}}`, "json: cannot unmarshal string"},

		// additionalProperties: false (the schema-strict requirement)
		{"local has unknown field", `{"mcp":{"x":{"type":"local","command":["node"],"unknown":"x"}}}`, `json: unknown field "unknown"`},
		{"local has remote-only field", `{"mcp":{"x":{"type":"local","command":["node"],"url":"https://e/"}}}`, `json: unknown field "url"`},
		{"remote has local-only field", `{"mcp":{"x":{"type":"remote","url":"https://e/","command":["node"]}}}`, `json: unknown field "command"`},
		{"remote has unknown field", `{"mcp":{"x":{"type":"remote","url":"https://e/","extra":1}}}`, `json: unknown field "extra"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			content, err := buildOpenCodeMCPConfigContent(json.RawMessage(tc.raw))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil (content=%q)", tc.want, content)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %q", tc.want, err.Error())
			}
			if content != "" {
				t.Fatalf("content should be empty on validation failure, got %q", content)
			}
		})
	}
}

// TestBuildOpenCodeMCPConfigContent_ClaudeStyleOAuthRoundTrip is a
// protective test: a well-formed Claude-style payload that includes an
// `oauth` object MUST survive translation + the unified validator + the
// final round-trip into OPENCODE_CONFIG_CONTENT with every oauth field
// preserved. If a future refactor accidentally tightens the validator or
// drops fields during translation, this test fails immediately instead
// of letting a regression reach users whose mcp_config was previously
// accepted. Pairs with TestBuildOpenCodeMCPConfigContent_RejectsMalformedClaudeStyle
// — that one pins what we reject; this one pins what we must keep accepting.
func TestBuildOpenCodeMCPConfigContent_ClaudeStyleOAuthRoundTrip(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{"mcpServers":{"x":{
		"url":"https://oauth.example/mcp",
		"headers":{"Authorization":"Bearer T"},
		"oauth":{"clientId":"cid","clientSecret":"sec","scope":"read write","callbackPort":3000,"redirectUri":"https://example/cb"},
		"timeout":5000
	}}}`)
	content, err := buildOpenCodeMCPConfigContent(raw)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	x := parseJSONString(t, content)["mcp"].(map[string]any)["x"].(map[string]any)
	if x["type"] != "remote" {
		t.Fatalf("type = %v, want remote", x["type"])
	}
	if x["timeout"].(float64) != 5000 {
		t.Fatalf("timeout = %v, want 5000", x["timeout"])
	}
	oauth, ok := x["oauth"].(map[string]any)
	if !ok {
		t.Fatalf("oauth not preserved as object: %#v", x["oauth"])
	}
	if oauth["clientId"] != "cid" || oauth["clientSecret"] != "sec" || oauth["scope"] != "read write" {
		t.Fatalf("oauth string fields lost: %#v", oauth)
	}
	if oauth["callbackPort"].(float64) != 3000 {
		t.Fatalf("oauth.callbackPort = %v, want 3000", oauth["callbackPort"])
	}
	if oauth["redirectUri"] != "https://example/cb" {
		t.Fatalf("oauth.redirectUri = %v", oauth["redirectUri"])
	}
}

// TestBuildOpenCodeMCPConfigContent_RejectsMalformedClaudeStyle covers the
// unified-validator contract: Claude-style `mcpServers` input is translated
// to OpenCode's native shape and then re-validated through the same
// `validateOpenCodeNativeMCPEntry` that gates the native path. Without that
// re-validation step, a Claude-style payload with malformed `headers`,
// `env`, `oauth`, or `timeout` would slip past daemon validation and surface
// as a confusing OpenCode startup error. This test pins each of those four
// bypass cases the reviewer flagged.
func TestBuildOpenCodeMCPConfigContent_RejectsMalformedClaudeStyle(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		raw  string
		want string
	}{
		// headers: values must be strings (translation kept the bad
		// value as-is; the unified validator catches it on the way out).
		{"remote header value is number", `{"mcpServers":{"x":{"url":"https://e/","headers":{"Authorization":123}}}}`, "json: cannot unmarshal number"},
		{"remote header value is bool", `{"mcpServers":{"x":{"url":"https://e/","headers":{"X-Auth":true}}}}`, "json: cannot unmarshal bool"},
		// env (renamed to environment during translation): values must be strings.
		{"local env value is number", `{"mcpServers":{"x":{"command":"node","env":{"FOO":42}}}}`, "json: cannot unmarshal number"},
		{"local env value is array", `{"mcpServers":{"x":{"command":"node","env":{"FOO":["a"]}}}}`, "json: cannot unmarshal array"},
		// oauth: must be object | false. `true` is rejected as ambiguous.
		{"oauth is true", `{"mcpServers":{"x":{"url":"https://e/","oauth":true}}}`, "must be an object or `false`"},
		// timeout: positive integer.
		{"timeout negative", `{"mcpServers":{"x":{"command":"node","timeout":-1}}}`, "`timeout` must be a positive integer"},
		{"timeout zero", `{"mcpServers":{"x":{"command":"node","timeout":0}}}`, "`timeout` must be a positive integer"},
		// callbackPort: explicit 0 must be rejected (verifies the
		// pointer-typed CallbackPort flows through translation too).
		{"oauth callbackPort explicit zero (claude-style)", `{"mcpServers":{"x":{"url":"https://e/","oauth":{"callbackPort":0}}}}`, "`callbackPort` must be in 1..65535"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			content, err := buildOpenCodeMCPConfigContent(json.RawMessage(tc.raw))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil (content=%q)", tc.want, content)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %q", tc.want, err.Error())
			}
			if content != "" {
				t.Fatalf("content should be empty on validation failure, got %q", content)
			}
		})
	}
}

// TestOpencodeBackendInjectsMCPConfigViaEnv is the end-to-end happy path:
// dispatch a task with mcp_config, and assert the spawned process saw the
// translated config in $OPENCODE_CONFIG_CONTENT. Crucially also asserts
// no <workdir>/opencode.json was written — that file is owned by the
// agent / user across turns, and the daemon must never touch it.
func TestOpencodeBackendInjectsMCPConfigViaEnv(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	fakePath := filepath.Join(tempDir, "opencode")
	captureFile := filepath.Join(tempDir, "env-capture.txt")
	writeTestExecutable(t, fakePath, []byte(fakeOpencodeScriptCapturingEnv()))

	workDir := t.TempDir()
	backend, err := New("opencode", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env: map[string]string{
			"OPENCODE_CAPTURE_FILE": captureFile,
		},
	})
	if err != nil {
		t.Fatalf("new backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Cwd:       workDir,
		Timeout:   5 * time.Second,
		McpConfig: json.RawMessage(`{"mcpServers":{"mcpbase":{"url":"https://mcpbase.example/mcp"}}}`),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("status = %q, error = %q; want completed", result.Status, result.Error)
	}

	// (1) Child saw the translated mcp config in OPENCODE_CONFIG_CONTENT.
	captured := readCapturedEnv(t, captureFile)
	got := captured["OPENCODE_CONFIG_CONTENT"]
	if !strings.Contains(got, "https://mcpbase.example/mcp") {
		t.Fatalf("OPENCODE_CONFIG_CONTENT did not include managed url:\n%s", got)
	}
	if !strings.Contains(got, `"type":"remote"`) {
		t.Fatalf("OPENCODE_CONFIG_CONTENT missing translated type=remote:\n%s", got)
	}

	// (2) <workdir>/opencode.json was not touched. The agent / user owns
	// that file across turns; the daemon must never write or remove it.
	if _, statErr := os.Stat(filepath.Join(workDir, "opencode.json")); !os.IsNotExist(statErr) {
		body, _ := os.ReadFile(filepath.Join(workDir, "opencode.json"))
		t.Fatalf("daemon must not write <workdir>/opencode.json; found:\n%s", string(body))
	}
}

// TestOpencodeBackendOmitsMCPEnvWhenEmpty asserts the no-mcp_config path
// does NOT inject OPENCODE_CONFIG_CONTENT, so any value the user set in
// agent.custom_env is preserved untouched. Without this, an empty
// mcp_config would silently clobber the user's escape hatch.
func TestOpencodeBackendOmitsMCPEnvWhenEmpty(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	fakePath := filepath.Join(tempDir, "opencode")
	captureFile := filepath.Join(tempDir, "env-capture.txt")
	writeTestExecutable(t, fakePath, []byte(fakeOpencodeScriptCapturingEnv()))

	const userContent = `{"mcp":{"user_only":{"type":"remote","url":"https://user.example/mcp"}}}`
	workDir := t.TempDir()
	backend, err := New("opencode", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env: map[string]string{
			"OPENCODE_CAPTURE_FILE":   captureFile,
			"OPENCODE_CONFIG_CONTENT": userContent,
		},
	})
	if err != nil {
		t.Fatalf("new backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Cwd:       workDir,
		Timeout:   5 * time.Second,
		McpConfig: nil, // explicit no-mcp path
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	if r := <-session.Result; r.Status != "completed" {
		t.Fatalf("status = %q, error = %q; want completed", r.Status, r.Error)
	}

	captured := readCapturedEnv(t, captureFile)
	if got := captured["OPENCODE_CONFIG_CONTENT"]; got != userContent {
		t.Fatalf("user OPENCODE_CONFIG_CONTENT was not preserved:\n  want %q\n  got  %q", userContent, got)
	}
}

// TestOpencodeBackendOverridesUserOpenCodeConfigContent asserts the
// daemon-level mcp_config wins over a user-supplied OPENCODE_CONFIG_CONTENT
// in agent.custom_env. This is the behaviour Go's os/exec dedup gives us
// (last occurrence wins) and the warning log is the documented signal of
// the override.
func TestOpencodeBackendOverridesUserOpenCodeConfigContent(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	fakePath := filepath.Join(tempDir, "opencode")
	captureFile := filepath.Join(tempDir, "env-capture.txt")
	writeTestExecutable(t, fakePath, []byte(fakeOpencodeScriptCapturingEnv()))

	const userBogus = `{"this-should-not-survive":true}`
	workDir := t.TempDir()
	backend, err := New("opencode", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env: map[string]string{
			"OPENCODE_CAPTURE_FILE":   captureFile,
			"OPENCODE_CONFIG_CONTENT": userBogus,
		},
	})
	if err != nil {
		t.Fatalf("new backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Cwd:       workDir,
		Timeout:   5 * time.Second,
		McpConfig: json.RawMessage(`{"mcpServers":{"daemon":{"url":"https://daemon.example/mcp"}}}`),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	if r := <-session.Result; r.Status != "completed" {
		t.Fatalf("status = %q, error = %q; want completed", r.Status, r.Error)
	}

	captured := readCapturedEnv(t, captureFile)
	got := captured["OPENCODE_CONFIG_CONTENT"]
	if strings.Contains(got, "this-should-not-survive") {
		t.Fatalf("user-set OPENCODE_CONFIG_CONTENT survived dedup; daemon mcp_config did not win:\n%s", got)
	}
	if !strings.Contains(got, "https://daemon.example/mcp") {
		t.Fatalf("daemon mcp_config did not reach the child process:\n%s", got)
	}
}

// fakeOpencodeScriptCapturingEnv returns a POSIX-sh script that writes the
// child process's relevant env vars to $OPENCODE_CAPTURE_FILE before
// emitting a minimal-valid stream and exiting. Lets a test observe what
// the child saw at exec time.
func fakeOpencodeScriptCapturingEnv() string {
	return `#!/bin/sh
if [ -n "$OPENCODE_CAPTURE_FILE" ]; then
  {
    printf 'OPENCODE_CONFIG_CONTENT=%s\n' "${OPENCODE_CONFIG_CONTENT-<unset>}"
  } > "$OPENCODE_CAPTURE_FILE"
fi
printf '{"type":"step_start","timestamp":1,"sessionID":"ses_fake","part":{"type":"step-start"}}\n'
printf '{"type":"text","timestamp":2,"sessionID":"ses_fake","part":{"type":"text","text":"ok"}}\n'
printf '{"type":"step_finish","timestamp":3,"sessionID":"ses_fake","part":{"type":"step-finish"}}\n'
`
}

// readCapturedEnv parses the env-dump file emitted by
// fakeOpencodeScriptCapturingEnv. Each line is `KEY=VALUE`. The single
// known sentinel `<unset>` flags an env var that was not set in the
// child's environment (vs. an explicitly empty string).
func readCapturedEnv(t *testing.T, path string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read capture %s: %v", path, err)
	}
	out := make(map[string]string)
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if v == "<unset>" {
			continue
		}
		out[k] = v
	}
	return out
}

// parseJSONString is a tiny helper for tests that work with the JSON the
// daemon will hand to OpenCode via OPENCODE_CONFIG_CONTENT.
func parseJSONString(t *testing.T, s string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		t.Fatalf("parse content: %v\n%s", err, s)
	}
	return out
}
