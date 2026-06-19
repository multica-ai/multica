package dettools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildProbe(t *testing.T) {
	dir := t.TempDir()
	// A Go manifest so the probe detects the go toolchain (go is always present
	// in the test environment).
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := callHandler(t, buildProbeHandler, dir, "{}")
	if res.Status != StatusOK {
		t.Fatalf("status = %q (%s), want ok", res.Status, res.Summary)
	}
	toolchains, _ := res.MachineData["toolchains"].([]map[string]any)
	var foundGo bool
	for _, tc := range toolchains {
		if tc["toolchain"] == "go" {
			foundGo = true
			if tc["available"] != true {
				t.Errorf("go toolchain reported unavailable: %v", tc)
			}
		}
	}
	if !foundGo {
		t.Errorf("expected go toolchain to be detected, got %v", toolchains)
	}
}

func TestTestGatePass(t *testing.T) {
	res := callHandler(t, testGateHandler, t.TempDir(), `{"commands": ["true", "echo hi"]}`)
	if res.Status != StatusOK {
		t.Fatalf("status = %q (%s), want ok", res.Status, res.Summary)
	}
	if res.MachineData["all_passed"] != true {
		t.Errorf("all_passed = %v, want true", res.MachineData["all_passed"])
	}
}

func TestTestGateFail(t *testing.T) {
	res := callHandler(t, testGateHandler, t.TempDir(), `{"commands": ["true", "false"]}`)
	if res.Status != StatusError || res.ErrorCode != CodePolicyFailure {
		t.Fatalf("got status=%q code=%q, want error/POLICY_FAILURE", res.Status, res.ErrorCode)
	}
	if f, _ := res.MachineData["failed"].(int); f != 1 {
		t.Errorf("failed = %v, want 1", res.MachineData["failed"])
	}
}

func TestTestGateRequiresCommands(t *testing.T) {
	res := callHandler(t, testGateHandler, t.TempDir(), `{"commands": []}`)
	if res.Status != StatusError || res.ErrorCode != CodeInvalidInput {
		t.Fatalf("got status=%q code=%q, want error/INVALID_INPUT", res.Status, res.ErrorCode)
	}
}

func TestDiffSummarizeWorkingTree(t *testing.T) {
	dir := initRepo(t, "main")
	// Modify the committed README so it shows as a working-tree change.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\nmore\nlines\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := callHandler(t, diffSummarizeHandler, dir, "{}")
	if res.Status != StatusOK {
		t.Fatalf("status = %q (%s), want ok", res.Status, res.Summary)
	}
	files, _ := res.MachineData["files"].([]map[string]any)
	if len(files) != 1 {
		t.Fatalf("files = %v, want 1", files)
	}
	if files[0]["path"] != "README.md" || files[0]["status"] != "modified" {
		t.Errorf("file entry = %v, want README.md/modified", files[0])
	}
}

func TestArtifactEmitMarkdown(t *testing.T) {
	dir := t.TempDir()
	res := callHandler(t, artifactEmitHandler, dir, `{"filename": "report.md", "format": "markdown", "content": "# Hello\n"}`)
	if res.Status != StatusOK {
		t.Fatalf("status = %q (%s), want ok", res.Status, res.Summary)
	}
	if len(res.Artifacts) != 1 {
		t.Fatalf("artifacts = %v, want 1", res.Artifacts)
	}
	want := filepath.Join(".multica", "artifacts", "report.md")
	if res.Artifacts[0].Path != want {
		t.Errorf("artifact path = %q, want %q", res.Artifacts[0].Path, want)
	}
	body, err := os.ReadFile(filepath.Join(dir, want))
	if err != nil {
		t.Fatalf("artifact not written: %v", err)
	}
	if string(body) != "# Hello\n" {
		t.Errorf("artifact content = %q", string(body))
	}
}

func TestArtifactEmitJSON(t *testing.T) {
	dir := t.TempDir()
	res := callHandler(t, artifactEmitHandler, dir, `{"filename": "data.json", "format": "json", "content": {"a": 1, "b": [2, 3]}}`)
	if res.Status != StatusOK {
		t.Fatalf("status = %q (%s), want ok", res.Status, res.Summary)
	}
	body, err := os.ReadFile(filepath.Join(dir, ".multica", "artifacts", "data.json"))
	if err != nil {
		t.Fatalf("artifact not written: %v", err)
	}
	if len(body) == 0 || body[len(body)-1] != '\n' {
		t.Errorf("expected pretty JSON ending in newline, got %q", string(body))
	}
}

func TestArtifactEmitRejectsPathEscape(t *testing.T) {
	dir := t.TempDir()
	res := callHandler(t, artifactEmitHandler, dir, `{"filename": "../../evil.txt", "format": "text", "content": "x"}`)
	if res.Status != StatusError || res.ErrorCode != CodeInvalidInput {
		t.Fatalf("got status=%q code=%q, want error/INVALID_INPUT", res.Status, res.ErrorCode)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "evil.txt")); err == nil {
		t.Error("path escape wrote a file outside the work dir")
	}
}

func TestArtifactEmitTextRejectsNonString(t *testing.T) {
	res := callHandler(t, artifactEmitHandler, t.TempDir(), `{"filename": "x.txt", "format": "text", "content": {"not": "a string"}}`)
	if res.Status != StatusError || res.ErrorCode != CodeInvalidInput {
		t.Fatalf("got status=%q code=%q, want error/INVALID_INPUT", res.Status, res.ErrorCode)
	}
}
