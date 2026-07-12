package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var (
	browserMcpGOOS      = runtime.GOOS
	browserMcpStat      = os.Stat
	browserMcpEnv       = os.Getenv
	browserMcpMkdirTemp = os.MkdirTemp
)

func hardenBrowserMcpConfig(raw json.RawMessage, tempDir string) ([]byte, error) {
	if browserMcpGOOS != "windows" {
		return raw, nil
	}
	return hardenWindowsBrowserMcpConfig(raw, tempDir)
}

func hardenWindowsBrowserMcpConfig(raw json.RawMessage, tempDir string) ([]byte, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return raw, nil
	}
	serversRaw, ok := top["mcpServers"]
	if !ok {
		return raw, nil
	}
	var servers map[string]json.RawMessage
	if err := json.Unmarshal(serversRaw, &servers); err != nil {
		return raw, nil
	}

	changed := false
	for name, serverRaw := range servers {
		var entry map[string]any
		if err := json.Unmarshal(serverRaw, &entry); err != nil {
			continue
		}
		args, ok := stringSlice(entry["args"])
		if !ok {
			continue
		}

		lowerName := strings.ToLower(name)
		switch {
		case lowerName == "playwright" || argsContain(args, "@playwright/mcp") || argsContain(args, `@playwright\mcp`):
			nextArgs, err := hardenWindowsPlaywrightMcpArgs(args, tempDir)
			if err != nil {
				return nil, err
			}
			if !sameStringSlice(args, nextArgs) {
				entry["args"] = nextArgs
				servers[name], changed = mustMarshalRaw(entry), true
			}
		case lowerName == "chrome-devtools" || argsContain(args, "chrome-devtools-mcp"):
			if path, ok := windowsChromiumFallbackExecutable(); ok && shouldPinChromeDevToolsExecutable(args) {
				entry["args"] = append(args, "--executablePath="+path)
				servers[name], changed = mustMarshalRaw(entry), true
			}
		case lowerName == "agent-browser" || argsContain(args, "agent-browser"):
			if shouldPinAgentBrowserArgs(args, entry["env"]) {
				entry["env"] = withEnvVar(entry["env"], "AGENT_BROWSER_ARGS", "--disable-gpu")
				servers[name], changed = mustMarshalRaw(entry), true
			}
		}
	}
	if !changed {
		return raw, nil
	}

	top["mcpServers"] = mustMarshalRaw(servers)
	data, err := json.Marshal(top)
	if err != nil {
		return nil, fmt.Errorf("marshal hardened mcp config: %w", err)
	}
	return data, nil
}

func hardenWindowsPlaywrightMcpArgs(args []string, tempDir string) ([]string, error) {
	if hasFlag(args, "--config") || hasFlag(args, "--cdp-endpoint") || hasFlag(args, "--extension") {
		return args, nil
	}
	configPath := filepath.Join(tempDir, "playwright-windows-browser.json")
	config := map[string]any{
		"browser": map[string]any{
			"launchOptions": map[string]any{
				"args": []string{"--disable-gpu"},
			},
		},
	}
	data, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("marshal playwright mcp browser config: %w", err)
	}
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		return nil, fmt.Errorf("write playwright mcp browser config: %w", err)
	}
	nextArgs := append(args, "--config", configPath)
	// Without an explicit --browser/--executable-path/--channel, playwright
	// falls back to its own downloaded/managed Chromium build (surfaces as
	// "Chrome for Testing") instead of a system browser. Pin it to system
	// Edge when present, mirroring the --executablePath pin already applied
	// to chrome-devtools-mcp below, so Windows users get one consistent
	// browser identity across both tools rather than an unmanaged download.
	if _, ok := windowsChromiumFallbackExecutable(); ok && shouldPinPlaywrightBrowser(args) {
		nextArgs = append(nextArgs, "--browser", "msedge")
	}
	return nextArgs, nil
}

// shouldPinPlaywrightBrowser reports whether it's safe to force playwright
// onto system Edge — false if the caller already made an explicit browser
// choice, matching shouldPinChromeDevToolsExecutable's "never override an
// explicit choice" contract.
func shouldPinPlaywrightBrowser(args []string) bool {
	for _, flag := range []string{
		"--browser",
		"--executable-path",
		"--executablePath",
		"--channel",
	} {
		if hasFlag(args, flag) {
			return false
		}
	}
	return true
}

func windowsChromiumFallbackExecutable() (string, bool) {
	if path := strings.TrimSpace(browserMcpEnv("MULTICA_CHROME_DEVTOOLS_EXECUTABLE_PATH")); path != "" {
		return path, true
	}
	for _, root := range []string{
		browserMcpEnv("ProgramFiles(x86)"),
		browserMcpEnv("ProgramFiles"),
		browserMcpEnv("LocalAppData"),
	} {
		if strings.TrimSpace(root) == "" {
			continue
		}
		path := windowsPathJoin(root, "Microsoft", "Edge", "Application", "msedge.exe")
		if _, err := browserMcpStat(path); err == nil {
			return path, true
		}
	}
	return "", false
}

func windowsPathJoin(root string, elems ...string) string {
	root = strings.TrimRight(root, `\/`)
	if root == "" {
		return ""
	}
	return root + `\` + strings.Join(elems, `\`)
}

func shouldPinChromeDevToolsExecutable(args []string) bool {
	for _, flag := range []string{
		"--executablePath",
		"--executable-path",
		"-e",
		"--channel",
		"--browserUrl",
		"--browser-url",
		"-u",
		"--wsEndpoint",
		"--ws-endpoint",
		"-w",
		"--autoConnect",
		"--auto-connect",
	} {
		if hasFlag(args, flag) {
			return false
		}
	}
	return true
}

// shouldPinAgentBrowserArgs reports whether it's safe to inject
// AGENT_BROWSER_ARGS=--disable-gpu — false if the caller already set an
// explicit --args CLI flag or an AGENT_BROWSER_ARGS env var, matching the
// "never override an explicit choice" contract used by the other browser
// tools above.
func shouldPinAgentBrowserArgs(args []string, env any) bool {
	if hasFlag(args, "--args") {
		return false
	}
	return !envHasKey(env, "AGENT_BROWSER_ARGS")
}

func envHasKey(env any, key string) bool {
	envMap, ok := env.(map[string]any)
	if !ok {
		return false
	}
	_, ok = envMap[key]
	return ok
}

func withEnvVar(env any, key, value string) map[string]any {
	envMap, ok := env.(map[string]any)
	if !ok {
		envMap = map[string]any{}
	}
	envMap[key] = value
	return envMap
}

func stringSlice(v any) ([]string, bool) {
	raw, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		s, ok := item.(string)
		if !ok {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

func argsContain(args []string, needle string) bool {
	needle = strings.ToLower(needle)
	for _, arg := range args {
		if strings.Contains(strings.ToLower(arg), needle) {
			return true
		}
	}
	return false
}

func hasFlag(args []string, flag string) bool {
	for _, arg := range args {
		if arg == flag || strings.HasPrefix(arg, flag+"=") {
			return true
		}
	}
	return false
}

func sameStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func mustMarshalRaw(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}

// hardenBrowserMcpConfigTemp is the counterpart to writeMcpConfigToTemp for
// callers that don't write mcp_config to a top-level file of their own (ACP
// backends send it in-memory; OpenCode injects it via an env var). The
// Windows hardening pass still needs a directory to write the playwright
// launchOptions sidecar file to, so this allocates a throwaway one.
//
// The returned cleanup func must not run until the child process this
// config was handed to has exited — not sooner. The child may not launch
// the playwright/chrome-devtools MCP subprocess (and read the sidecar file)
// until partway through the run, so cleaning up right after Execute()
// returns would delete the sidecar out from under it. Callers should
// schedule cleanup with context.AfterFunc(runCtx, cleanup) using the same
// runCtx passed to exec.CommandContext.
//
// Returns raw unchanged with a no-op cleanup when raw is empty or the host
// isn't Windows, mirroring hardenBrowserMcpConfig's own no-op contract —
// off Windows hardenBrowserMcpConfig never touches tempDir, so allocating
// one would be pure filesystem churn on every non-empty mcp_config, on
// every task, on the Linux/macOS hosts the daemon actually runs on in
// production.
func hardenBrowserMcpConfigTemp(raw json.RawMessage) (json.RawMessage, func(), error) {
	noop := func() {}
	if len(raw) == 0 || browserMcpGOOS != "windows" {
		return raw, noop, nil
	}
	dir, err := browserMcpMkdirTemp("", "multica-mcp-harden-*")
	if err != nil {
		return nil, noop, fmt.Errorf("create mcp harden temp dir: %w", err)
	}
	hardened, err := hardenBrowserMcpConfig(raw, dir)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, noop, err
	}
	return hardened, func() { _ = os.RemoveAll(dir) }, nil
}
