package internalbrowserqa

import (
	"os"
	"strings"
	"testing"
)

func TestBackendImageContainsPinnedInternalBrowserRuntime(t *testing.T) {
	raw, err := os.ReadFile("../../../../Dockerfile")
	if err != nil {
		t.Fatalf("read backend Dockerfile: %v", err)
	}
	content := string(raw)
	for _, required := range []string{
		"chromium",
		"nodejs",
		"npm install --global agent-browser@0.26.0",
		"AGENT_BROWSER_EXECUTABLE_PATH=/usr/bin/chromium-browser",
		"HOME=/home/app",
	} {
		if !strings.Contains(content, required) {
			t.Errorf("backend Dockerfile is missing %q", required)
		}
	}
}

func TestDedicatedVerifierImageRunsUnprivilegedBesideInternalTargets(t *testing.T) {
	raw, err := os.ReadFile("../../../../Dockerfile.browser-verifier")
	if err != nil {
		t.Fatalf("read verifier Dockerfile: %v", err)
	}
	content := string(raw)
	for _, required := range []string{
		"./cmd/browser-verifier-runner",
		"agent-browser@0.26.0",
		"AGENT_BROWSER_EXECUTABLE_PATH=/usr/bin/chromium-browser",
		"USER app",
	} {
		if !strings.Contains(content, required) {
			t.Errorf("verifier Dockerfile is missing %q", required)
		}
	}
}
