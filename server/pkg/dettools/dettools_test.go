package dettools

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testEnv(workDir string) ToolEnv {
	return ToolEnv{
		WorkDir:     workDir,
		Timeout:     30 * time.Second,
		ArtifactDir: ".multica/artifacts",
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// initRepo creates a git repository in a temp dir with one commit on the given
// branch and returns its path. It skips the test if git is unavailable.
func initRepo(t *testing.T, branch string) string {
	t.Helper()
	if !gitAvailable() {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	// Disable commit signing locally so tests don't depend on a signing setup.
	run("config", "commit.gpgsign", "false")
	run("config", "tag.gpgsign", "false")
	run("checkout", "-q", "-b", branch)
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "README.md")
	run("commit", "-q", "-m", "init")
	return dir
}

func callHandler(t *testing.T, h Handler, workDir, argsJSON string) Result {
	t.Helper()
	return h(context.Background(), json.RawMessage(argsJSON), testEnv(workDir))
}

func TestRepoFacts(t *testing.T) {
	dir := initRepo(t, "feature/tool-plane")
	// Modify a tracked file (porcelain line " M README.md" starts with a space —
	// guards against the path-shift regression) plus an untracked npm lockfile.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\nchanged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res := callHandler(t, repoFactsHandler, dir, "{}")
	if res.Status != StatusOK {
		t.Fatalf("status = %q (%s), want ok", res.Status, res.Summary)
	}
	if got := res.MachineData["branch"]; got != "feature/tool-plane" {
		t.Errorf("branch = %v, want feature/tool-plane", got)
	}
	if df, _ := res.MachineData["dirty_files"].(int); df < 2 {
		t.Errorf("dirty_files = %v, want >= 2", res.MachineData["dirty_files"])
	}
	changed, _ := res.MachineData["changed_files"].([]string)
	if !containsStr(changed, "README.md") {
		t.Errorf("changed_files = %v, want intact path README.md", changed)
	}
	managers, _ := res.MachineData["package_managers"].([]string)
	if !containsStr(managers, "npm") {
		t.Errorf("package_managers = %v, want to contain npm", managers)
	}
}

func TestRepoFactsRejectsUnknownArgs(t *testing.T) {
	dir := initRepo(t, "main")
	res := callHandler(t, repoFactsHandler, dir, `{"unexpected": true}`)
	if res.Status != StatusError || res.ErrorCode != CodeInvalidInput {
		t.Fatalf("got status=%q code=%q, want error/INVALID_INPUT", res.Status, res.ErrorCode)
	}
}

func TestRepoFactsNotGitRepo(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	res := callHandler(t, repoFactsHandler, dir, "{}")
	if res.Status != StatusOK {
		t.Fatalf("status = %q, want ok", res.Status)
	}
	if res.MachineData["is_git_repo"] != false {
		t.Errorf("is_git_repo = %v, want false", res.MachineData["is_git_repo"])
	}
}

func TestPolicyCheckPasses(t *testing.T) {
	dir := initRepo(t, "feature/x")
	res := callHandler(t, policyCheckHandler, dir, `{
		"allowed_branch_prefixes": ["feature/", "fix/"],
		"required_files": ["README.md"]
	}`)
	if res.Status != StatusOK {
		t.Fatalf("status = %q (%s), want ok", res.Status, res.Summary)
	}
	if res.MachineData["passed"] != true {
		t.Errorf("passed = %v, want true", res.MachineData["passed"])
	}
}

func TestPolicyCheckViolations(t *testing.T) {
	dir := initRepo(t, "main")
	// Add a changed file under a forbidden path.
	if err := os.MkdirAll(filepath.Join(dir, "secrets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "secrets", "key.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := callHandler(t, policyCheckHandler, dir, `{
		"allowed_branch_prefixes": ["feature/"],
		"forbidden_paths": ["secrets/"],
		"required_files": ["CHANGELOG.md"]
	}`)
	if res.Status != StatusError || res.ErrorCode != CodePolicyFailure {
		t.Fatalf("got status=%q code=%q, want error/POLICY_FAILURE", res.Status, res.ErrorCode)
	}
	violations, _ := res.MachineData["violations"].([]string)
	if len(violations) != 3 {
		t.Fatalf("violations = %v, want 3 (branch, forbidden path, missing file)", violations)
	}
}

// TestServeRoundTrip drives the MCP stdio loop end to end: initialize, a skipped
// notification, tools/list, and a repo_facts tools/call.
func TestServeRoundTrip(t *testing.T) {
	dir := initRepo(t, "main")
	reg := NewRegistry(nil) // all tools

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"repo_facts","arguments":{}}}`,
	}, "\n") + "\n"

	var out strings.Builder
	err := Serve(context.Background(), strings.NewReader(input), &out, reg,
		ServerInfo{Name: "multica-tools", Version: "test"}, testEnv(dir),
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}

	responses := decodeResponses(t, out.String())
	if len(responses) != 3 {
		t.Fatalf("got %d responses, want 3 (notification must not reply)", len(responses))
	}

	// initialize echoes the protocol version.
	initResult := responses[0]["result"].(map[string]any)
	if initResult["protocolVersion"] != "2025-06-18" {
		t.Errorf("protocolVersion = %v", initResult["protocolVersion"])
	}

	// tools/list contains the full catalog.
	toolsList := responses[1]["result"].(map[string]any)["tools"].([]any)
	if len(toolsList) != len(allTools()) {
		t.Errorf("tools/list returned %d tools, want %d", len(toolsList), len(allTools()))
	}

	// tools/call returns a non-error result with structured content.
	callResult := responses[2]["result"].(map[string]any)
	if callResult["isError"] != false {
		t.Errorf("isError = %v, want false", callResult["isError"])
	}
	structured := callResult["structuredContent"].(map[string]any)
	if structured["status"] != StatusOK {
		t.Errorf("structured status = %v, want ok", structured["status"])
	}
}

func TestServeUnknownToolIsError(t *testing.T) {
	reg := NewRegistry(nil)
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"does_not_exist","arguments":{}}}` + "\n"
	var out strings.Builder
	if err := Serve(context.Background(), strings.NewReader(input), &out, reg,
		ServerInfo{Name: "multica-tools", Version: "test"}, testEnv(t.TempDir()),
		slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	res := decodeResponses(t, out.String())[0]["result"].(map[string]any)
	if res["isError"] != true {
		t.Errorf("isError = %v, want true for unknown tool", res["isError"])
	}
}

func TestServeParseError(t *testing.T) {
	reg := NewRegistry(nil)
	input := "this is not json\n"
	var out strings.Builder
	if err := Serve(context.Background(), strings.NewReader(input), &out, reg,
		ServerInfo{Name: "multica-tools", Version: "test"}, testEnv(t.TempDir()),
		slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	resp := decodeResponses(t, out.String())[0]
	rpcErr, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error response, got %v", resp)
	}
	if int(rpcErr["code"].(float64)) != rpcParseError {
		t.Errorf("error code = %v, want %d", rpcErr["code"], rpcParseError)
	}
}

func TestRegistryAllowlist(t *testing.T) {
	reg := NewRegistry([]string{"repo_facts"})
	if _, ok := reg.Lookup("repo_facts"); !ok {
		t.Error("repo_facts should be exposed")
	}
	if _, ok := reg.Lookup("policy_check"); ok {
		t.Error("policy_check should be filtered out by the allowlist")
	}
	if len(reg.Descriptors()) != 1 {
		t.Errorf("descriptors = %d, want 1", len(reg.Descriptors()))
	}
}

func decodeResponses(t *testing.T, raw string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("decode response %q: %v", line, err)
		}
		out = append(out, m)
	}
	return out
}

func containsStr(s []string, want string) bool {
	for _, v := range s {
		if v == want {
			return true
		}
	}
	return false
}
