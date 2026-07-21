package agent

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withBrowserMcpTestHost(t *testing.T, goos string, env map[string]string, existing map[string]bool) {
	t.Helper()
	oldGOOS := browserMcpGOOS
	oldEnv := browserMcpEnv
	oldStat := browserMcpStat
	browserMcpGOOS = goos
	browserMcpEnv = func(key string) string { return env[key] }
	browserMcpStat = func(path string) (os.FileInfo, error) {
		if existing[path] {
			return nil, nil
		}
		return nil, os.ErrNotExist
	}
	t.Cleanup(func() {
		browserMcpGOOS = oldGOOS
		browserMcpEnv = oldEnv
		browserMcpStat = oldStat
	})
}

func TestHardenBrowserMcpConfigNoopOffWindows(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{"mcpServers":{"playwright":{"command":"node","args":["@playwright/mcp","--headless"]}}}`)
	got, err := hardenBrowserMcpConfig(raw, t.TempDir())
	if err != nil {
		t.Fatalf("hardenBrowserMcpConfig: %v", err)
	}
	if string(got) != string(raw) {
		t.Fatalf("non-Windows config changed:\n got %s\nwant %s", got, raw)
	}
}

func TestHardenWindowsBrowserMcpConfigAddsPlaywrightLaunchConfigAndEdgeFallback(t *testing.T) {
	edgePath := `C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`
	withBrowserMcpTestHost(t, "windows", map[string]string{
		"ProgramFiles(x86)": `C:\Program Files (x86)`,
	}, map[string]bool{edgePath: true})

	tempDir := t.TempDir()
	raw := json.RawMessage(`{"mcpServers":{
		"playwright":{"command":"node","args":["C:\\npm\\node_modules\\@playwright\\mcp\\cli.js","--headless","--isolated"]},
		"chrome-devtools":{"command":"node","args":["C:\\npm\\node_modules\\chrome-devtools-mcp\\dist\\index.js","--headless","--isolated"]}
	}}`)

	got, err := hardenBrowserMcpConfig(raw, tempDir)
	if err != nil {
		t.Fatalf("hardenBrowserMcpConfig: %v", err)
	}

	servers := decodeMcpServers(t, got)
	playwrightArgs := decodeArgs(t, servers["playwright"])
	configIndex := indexOfString(playwrightArgs, "--config")
	if configIndex < 0 || configIndex+1 >= len(playwrightArgs) {
		t.Fatalf("playwright args missing --config path: %v", playwrightArgs)
	}
	configPath := playwrightArgs[configIndex+1]
	if !strings.HasPrefix(configPath, tempDir) {
		t.Fatalf("playwright config path %q is not under temp dir %q", configPath, tempDir)
	}
	configData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read playwright config: %v", err)
	}
	if !strings.Contains(string(configData), "--disable-gpu") {
		t.Fatalf("playwright config missing --disable-gpu: %s", configData)
	}

	chromeArgs := decodeArgs(t, servers["chrome-devtools"])
	if !contains(chromeArgs, "--executablePath="+edgePath) {
		t.Fatalf("chrome-devtools args missing Edge executable fallback:\n%v", chromeArgs)
	}
}

func TestHardenWindowsBrowserMcpConfigRespectsExplicitBrowserArgs(t *testing.T) {
	withBrowserMcpTestHost(t, "windows", map[string]string{
		"MULTICA_CHROME_DEVTOOLS_EXECUTABLE_PATH": `D:\Browsers\Chrome\chrome.exe`,
	}, nil)

	tempDir := t.TempDir()
	raw := json.RawMessage(`{"mcpServers":{
		"playwright":{"command":"npx","args":["@playwright/mcp@latest","--config=custom.json"]},
		"chrome-devtools":{"command":"npx","args":["chrome-devtools-mcp@latest","-e","D:\\Browsers\\Chrome\\chrome.exe"]}
	}}`)

	got, err := hardenBrowserMcpConfig(raw, tempDir)
	if err != nil {
		t.Fatalf("hardenBrowserMcpConfig: %v", err)
	}
	if string(got) != string(raw) {
		t.Fatalf("explicit browser args should not be changed:\n got %s\nwant %s", got, raw)
	}
	if entries, err := os.ReadDir(tempDir); err != nil {
		t.Fatalf("read temp dir: %v", err)
	} else if len(entries) != 0 {
		t.Fatalf("explicit config should not create sidecar files: %v", entries)
	}
}

func TestHardenWindowsBrowserMcpConfigKeepsRawOnMalformedInput(t *testing.T) {
	withBrowserMcpTestHost(t, "windows", nil, nil)

	raw := json.RawMessage(`not json`)
	got, err := hardenBrowserMcpConfig(raw, t.TempDir())
	if err != nil {
		t.Fatalf("hardenBrowserMcpConfig: %v", err)
	}
	if string(got) != string(raw) {
		t.Fatalf("malformed config changed: got %s want %s", got, raw)
	}
}

func decodeMcpServers(t *testing.T, raw []byte) map[string]json.RawMessage {
	t.Helper()
	var top struct {
		McpServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatalf("unmarshal mcp config: %v\n%s", err, raw)
	}
	return top.McpServers
}

func decodeArgs(t *testing.T, raw json.RawMessage) []string {
	t.Helper()
	var entry struct {
		Args []string `json:"args"`
	}
	if err := json.Unmarshal(raw, &entry); err != nil {
		t.Fatalf("unmarshal mcp server: %v\n%s", err, raw)
	}
	return entry.Args
}

func indexOfString(items []string, item string) int {
	for i, candidate := range items {
		if candidate == item {
			return i
		}
	}
	return -1
}

func contains(items []string, item string) bool {
	return indexOfString(items, item) >= 0
}

func TestWindowsChromiumFallbackExecutableSkipsMissingInstallDirs(t *testing.T) {
	withBrowserMcpTestHost(t, "windows", map[string]string{
		"ProgramFiles(x86)": "",
		"ProgramFiles":      "",
		"LocalAppData":      "",
	}, nil)

	if got, ok := windowsChromiumFallbackExecutable(); ok {
		t.Fatalf("fallback executable = %q, want none", got)
	}
}

func TestWindowsChromiumFallbackExecutablePropagatesOverride(t *testing.T) {
	override := filepath.Clean(`D:\Browsers\Chromium\chrome.exe`)
	withBrowserMcpTestHost(t, "windows", map[string]string{
		"MULTICA_CHROME_DEVTOOLS_EXECUTABLE_PATH": override,
	}, map[string]bool{})

	got, ok := windowsChromiumFallbackExecutable()
	if !ok || got != override {
		t.Fatalf("fallback executable = %q, %v; want %q, true", got, ok, override)
	}
}

func TestWithBrowserMcpTestHostUsesMissingStat(t *testing.T) {
	withBrowserMcpTestHost(t, "windows", nil, nil)
	_, err := browserMcpStat("missing")
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("browserMcpStat missing error = %v, want os.ErrNotExist", err)
	}
}
