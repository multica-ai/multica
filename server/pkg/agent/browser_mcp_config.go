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
	browserMcpGOOS = runtime.GOOS
	browserMcpStat = os.Stat
	browserMcpEnv  = os.Getenv
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
	return append(args, "--config", configPath), nil
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
