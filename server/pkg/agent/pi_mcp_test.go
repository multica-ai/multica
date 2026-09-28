package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func piMCPFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	pkg := filepath.Join(dir, "npm", "node_modules", "pi-mcp-adapter")
	if err := os.MkdirAll(pkg, 0700); err != nil {
		t.Fatal(err)
	}
	for path, data := range map[string]string{
		filepath.Join(dir, "settings.json"): `{"packages":["npm:pi-mcp-adapter"],"defaultProvider":"anthropic","defaultModel":"test-model"}`,
		filepath.Join(pkg, "package.json"):  `{"name":"pi-mcp-adapter","version":"2.37.0","pi":{"extensions":["./index.ts"]}}`,
		filepath.Join(pkg, "index.ts"):      `export function createMcpAdapter(options) { return function(pi) {} }`,
		filepath.Join(dir, "auth.json"):     `{"test":{"type":"api_key","key":"private-key"}}`,
		filepath.Join(dir, "models.json"):   `{"providers":{}}`,
	} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestPiMCPProbeRequiresEnabledCompatibleAdapter(t *testing.T) {
	dir := piMCPFixture(t)
	if _, err := findPiMCPAdapter(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := findPiMCPAdapter(t.TempDir()); err == nil {
		t.Fatal("missing plugin reported capable")
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"packages":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := findPiMCPAdapter(dir); err == nil {
		t.Fatal("disabled plugin reported capable")
	}
	dir = piMCPFixture(t)
	if err := os.WriteFile(filepath.Join(dir, "npm/node_modules/pi-mcp-adapter/package.json"), []byte(`{"name":"pi-mcp-adapter","version":"1.0.0"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := findPiMCPAdapter(dir); err == nil {
		t.Fatal("incompatible plugin reported capable")
	}
}

func TestPiMCPTranslation(t *testing.T) {
	raw := json.RawMessage(`{"mcpServers":{"http":{"url":"http://localhost:3000/mcp","headers":{"Authorization":"!literal"}},"stdio":{"command":"node","args":["server.js"],"env":{"TOKEN":"!literal"}},"off":{"disabled":true},"off2":{"enabled":false}}}`)
	got, err := translatePiMCPConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Servers map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(got, &config); err != nil {
		t.Fatal(err)
	}
	if len(config.Servers) != 2 || config.Servers["http"]["url"] != "http://localhost:3000/mcp" {
		t.Fatalf("unexpected servers: %s", got)
	}
	if config.Servers["stdio"]["env"].(map[string]any)["TOKEN"] != "!!literal" {
		t.Fatal("literal secret became plugin command")
	}
	for _, raw := range []string{`{}`, `{"mcpServers":{}}`} {
		got, err := translatePiMCPConfig(json.RawMessage(raw))
		if err != nil || string(got) != `{"mcpServers":{}}` {
			t.Fatalf("empty: %s %v", got, err)
		}
	}
	for _, raw := range []string{`[1]`, `{"mcpServers":null}`, `{"mcpServers":{"bad":{}}}`, `{"mcpServers":{"bad":{"url":1}}}`, `{"mcpServers":{"bad":{"command":"node","url":"http://test"}}}`, `{"mcpServers":{"bad":{"command":"node","args":"secret"}}}`, `{"mcpServers":{"bad":{"url":"http://test","requestHeadersCommand":"secret"}}}`} {
		if _, err := translatePiMCPConfig(json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted malformed config: %s", raw)
		} else if strings.Contains(err.Error(), "secret") {
			t.Fatal("error leaked config")
		}
	}
}

func TestPiMCPPrepareIsolatesRunsAndCleans(t *testing.T) {
	dir := piMCPFixture(t)
	original, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	env := []string{"PI_CODING_AGENT_DIR=" + dir, "HOME=/unchanged"}
	a, err := preparePiMCP(json.RawMessage(`{"mcpServers":{"a":{"command":"a"}}}`), env)
	if err != nil {
		t.Fatal(err)
	}
	defer a.cleanup()
	b, err := preparePiMCP(json.RawMessage(`{"mcpServers":{"b":{"url":"http://localhost/mcp"}}}`), env)
	if err != nil {
		t.Fatal(err)
	}
	defer b.cleanup()
	if a.dir == b.dir {
		t.Fatal("shared run directory")
	}
	for _, run := range []*piMCPRun{a, b} {
		stat, err := os.Stat(filepath.Join(run.dir, "multica-managed-mcp.json"))
		if err != nil {
			t.Fatal(err)
		}
		if runtime.GOOS != "windows" && stat.Mode().Perm()&0077 != 0 {
			t.Fatal("config readable by other users")
		}
		auth, _ := os.ReadFile(filepath.Join(run.dir, "auth.json"))
		if !strings.Contains(string(auth), "private-key") {
			t.Fatal("authentication not preserved")
		}
		settings, _ := os.ReadFile(filepath.Join(run.dir, "settings.json"))
		if strings.Contains(string(settings), "npm:pi-mcp-adapter") {
			t.Fatal("ambient package retained")
		}
		if !strings.Contains(string(settings), "test-model") {
			t.Fatal("model preference lost")
		}
	}
	ac, _ := os.ReadFile(filepath.Join(a.dir, "multica-managed-mcp.json"))
	bc, _ := os.ReadFile(filepath.Join(b.dir, "multica-managed-mcp.json"))
	if strings.Contains(string(ac), `"b"`) || strings.Contains(string(bc), `"a"`) {
		t.Fatal("cross-agent MCP leak")
	}
	a.cleanup()
	if _, err := os.Stat(a.dir); !os.IsNotExist(err) {
		t.Fatal("run directory survived cleanup")
	}
	after, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	if string(after) != string(original) {
		t.Fatal("global configuration changed")
	}
}

func TestPiMCPPreparePreservesSkillsWithoutAutoInstallingPackages(t *testing.T) {
	dir := piMCPFixture(t)
	pkg := filepath.Join(dir, "npm", "node_modules", "my-skills")
	if err := os.MkdirAll(pkg, 0700); err != nil {
		t.Fatal(err)
	}
	data := `{"packages":["npm:pi-mcp-adapter","npm:my-skills@1.0.0"],"skills":["./custom-skills"],"extensions":["./unsafe.ts"]}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	run, err := preparePiMCP(json.RawMessage(`{}`), []string{"PI_CODING_AGENT_DIR=" + dir})
	if err != nil {
		t.Fatal(err)
	}
	defer run.cleanup()
	raw, _ := os.ReadFile(filepath.Join(run.dir, "settings.json"))
	var s struct {
		Packages []struct {
			Source     string   `json:"source"`
			Extensions []string `json:"extensions"`
		} `json:"packages"`
		Skills []string `json:"skills"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range s.Packages {
		if p.Source == pkg {
			found = true
		}
		if !filepath.IsAbs(p.Source) || len(p.Extensions) != 0 {
			t.Fatal("package can install or autoload extensions")
		}
	}
	if !found || len(s.Skills) != 1 || s.Skills[0] != filepath.Join(dir, "custom-skills") {
		t.Fatalf("skills missing: %s", raw)
	}
}

func TestPiMCPHTTPDoesNotAdoptGlobalOAuthAndPreservesTransport(t *testing.T) {
	raw, err := translatePiMCPConfig(json.RawMessage(`{"mcpServers":{"s":{"url":"https://example.test/mcp","type":"sse"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	var d map[string]map[string]map[string]any
	_ = json.Unmarshal(raw, &d)
	s := d["mcpServers"]["s"]
	if s["auth"] != false || s["oauth"] != false || s["httpTransport"] != "sse" {
		t.Fatalf("HTTP policy lost: %s", raw)
	}
}

func TestPiMCPProbeRespectsPackageFilters(t *testing.T) {
	for _, pkg := range []string{`{"source":"npm:pi-mcp-adapter","autoload":false}`, `{"source":"npm:pi-mcp-adapter","extensions":["*","!{index.ts,other.ts}"]}`, `{"source":"npm:pi-mcp-adapter","extensions":["*","!index.ts"]}`, `{"source":"npm:pi-mcp-adapter","extensions":["*","-index.ts"]}`} {
		dir := piMCPFixture(t)
		_ = os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"packages":[`+pkg+`]}`), 0600)
		if _, err := findPiMCPAdapter(dir); err == nil {
			t.Fatalf("disabled adapter enabled: %s", pkg)
		}
	}
}

func TestPiMCPRedactsInterpolatedCredentialsAndToolResultFields(t *testing.T) {
	r := &piMCPRun{secrets: piMCPSecrets(json.RawMessage(`{"mcpServers":{"a":{"url":"https://example.test/mcp?key=${TOKEN}","headers":{"Authorization":"Bearer ${TOKEN}"},"command":"node","args":["--key","${ARG_TOKEN}"]}}}`), []string{"TOKEN=private-token", "ARG_TOKEN=argument-secret"})}
	got := r.redactJSON(`{"type":"tool_execution_end","result":{"status":"private-token","type":"argument-secret","content":[{"text":"https://example.test/mcp?key=private-token"}]}}`)
	if strings.Contains(got, "private-token") || strings.Contains(got, "argument-secret") {
		t.Fatalf("credential leaked: %s", got)
	}
}

func TestPiMCPPreservesSkillOverridesInSettings(t *testing.T) {
	dir := piMCPFixture(t)
	_ = os.MkdirAll(filepath.Join(dir, "skills"), 0700)
	_ = os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"packages":["npm:pi-mcp-adapter"],"skills":["-skills/disabled","+custom/SKILL.md","!skills/hidden*"]}`), 0600)
	run, err := preparePiMCP(json.RawMessage(`{}`), []string{"PI_CODING_AGENT_DIR=" + dir})
	if err != nil {
		t.Fatal(err)
	}
	defer run.cleanup()
	raw, _ := os.ReadFile(filepath.Join(run.dir, "settings.json"))
	var s struct {
		Skills []string `json:"skills"`
	}
	_ = json.Unmarshal(raw, &s)
	want := []string{filepath.Join(dir, "skills"), "-skills/disabled", "-" + filepath.Join(dir, "skills/disabled"), "+custom/SKILL.md", "+" + filepath.Join(dir, "custom/SKILL.md"), "!skills/hidden*", "!" + filepath.Join(dir, "skills/hidden*")}
	if len(s.Skills) != len(want) {
		t.Fatalf("skills: %v", s.Skills)
	}
	for i, p := range want {
		if s.Skills[i] != p {
			t.Fatalf("skill %d = %q want %q", i, s.Skills[i], p)
		}
	}
	for _, arg := range run.args {
		if arg == "--skill" {
			t.Fatal("CLI skill paths bypass settings exclusions")
		}
	}
}

func TestPiMCPRedactionPreservesErrorControlFlow(t *testing.T) {
	r := &piMCPRun{secrets: []string{"error"}}
	var event piStreamEvent
	raw := r.redactJSON(`{"type":"turn_end","message":{"role":"assistant","stopReason":"error","errorMessage":"provider failed"}}`)
	_ = json.Unmarshal([]byte(raw), &event)
	msg := decodePiMessage(event.Message)
	if msg == nil || msg.StopReason != "error" {
		t.Fatalf("redaction erased failure: %s", raw)
	}
}
