package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func translatePiMCPConfig(raw json.RawMessage) (json.RawMessage, error) {
	var doc map[string]json.RawMessage
	if json.Unmarshal(raw, &doc) != nil || doc == nil {
		return nil, errors.New("invalid MCP config object")
	}
	for key := range doc {
		if key != "mcpServers" {
			return nil, errors.New("unsupported MCP config field")
		}
	}
	servers := map[string]json.RawMessage{}
	if value, ok := doc["mcpServers"]; ok {
		if json.Unmarshal(value, &servers) != nil || servers == nil {
			return nil, errors.New("invalid mcpServers object")
		}
	}
	out := map[string]any{}
	for name, raw := range servers {
		var entry map[string]json.RawMessage
		if json.Unmarshal(raw, &entry) != nil || entry == nil {
			return nil, errors.New("invalid MCP server object")
		}
		disabled := false
		for _, key := range []string{"enabled", "disabled"} {
			if value, ok := entry[key]; ok {
				var b bool
				if string(value) == "null" || json.Unmarshal(value, &b) != nil {
					return nil, errors.New("invalid MCP enabled flag")
				}
				disabled = disabled || (key == "disabled" && b) || (key == "enabled" && !b)
			}
		}
		if disabled {
			continue
		}
		var server struct {
			Command       string            `json:"command,omitempty"`
			Args          []string          `json:"args,omitempty"`
			Env           map[string]string `json:"env,omitempty"`
			URL           string            `json:"url,omitempty"`
			Headers       map[string]string `json:"headers,omitempty"`
			Cwd           string            `json:"cwd,omitempty"`
			Auth          bool              `json:"auth"`
			OAuth         bool              `json:"oauth"`
			HTTPTransport string            `json:"httpTransport,omitempty"`
		}
		allowed := map[string]bool{"command": true, "args": true, "env": true, "url": true, "headers": true, "cwd": true, "enabled": true, "disabled": true, "type": true, "transport": true}
		for key := range entry {
			if !allowed[key] {
				return nil, errors.New("unsupported MCP server field")
			}
		}
		if json.Unmarshal(raw, &server) != nil {
			return nil, errors.New("invalid MCP server fields")
		}
		if strings.TrimSpace(name) == "" || (strings.TrimSpace(server.Command) == "") == (strings.TrimSpace(server.URL) == "") {
			return nil, errors.New("MCP server requires exactly one command or URL")
		}
		for _, key := range []string{"type", "transport"} {
			if value, ok := entry[key]; ok {
				var s string
				if json.Unmarshal(value, &s) != nil {
					return nil, errors.New("invalid MCP transport")
				}
				if server.URL != "" {
					if s == "sse" {
						server.HTTPTransport = "sse"
					} else {
						server.HTTPTransport = "streamable-http"
					}
				}
				if (server.URL != "" && s != "http" && s != "sse" && s != "streamable-http") || (server.Command != "" && s != "stdio") {
					return nil, errors.New("incompatible MCP transport")
				}
			}
		}
		if server.URL != "" && (len(server.Args) > 0 || server.Cwd != "") {
			return nil, errors.New("stdio fields supplied for HTTP MCP server")
		}
		// The adapter interprets a leading ! as a shell command. Multica values
		// are data; escape this adapter-only syntax without expanding any secrets.
		for _, values := range []map[string]string{server.Env, server.Headers} {
			for k, v := range values {
				if strings.HasPrefix(v, "!") {
					values[k] = "!" + v
				}
			}
		}
		out[name] = server
	}
	return json.Marshal(map[string]any{"mcpServers": out})
}

type piMCPRun struct {
	secrets []string
	dir     string
	args    []string
	env     []string
}

func (r *piMCPRun) cleanup() {
	if r != nil {
		_ = os.RemoveAll(r.dir)
	}
}

func preparePiMCP(raw json.RawMessage, env []string) (_ *piMCPRun, err error) {
	config, err := translatePiMCPConfig(raw)
	if err != nil {
		return nil, err
	}
	original, err := piAgentDir(env)
	if err != nil {
		return nil, err
	}
	adapter, err := findPiMCPAdapter(original)
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "multica-pi-mcp-")
	if err != nil {
		return nil, errors.New("cannot create private Pi directory")
	}
	run := &piMCPRun{dir: dir, secrets: piMCPSecrets(raw, env)}
	defer func() {
		if err != nil {
			run.cleanup()
		}
	}()
	for _, name := range []string{"auth.json", "models.json"} {
		data, readErr := os.ReadFile(filepath.Join(original, name))
		if os.IsNotExist(readErr) {
			continue
		}
		if readErr != nil {
			return nil, fmt.Errorf("cannot read Pi %s", name)
		}
		if err = os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			return nil, fmt.Errorf("cannot copy Pi %s", name)
		}
	}
	data, err := os.ReadFile(filepath.Join(original, "settings.json"))
	if err != nil {
		return nil, errors.New("cannot read Pi settings")
	}
	var settings map[string]json.RawMessage
	if json.Unmarshal(data, &settings) != nil {
		return nil, errors.New("invalid Pi settings")
	}
	// Resource paths are relative to the original agent dir. Installed packages
	// retain their resources, while extension discovery remains disabled.
	if value, ok := settings["packages"]; ok {
		packages, packageErr := piMCPResourcePackages(original, value)
		if packageErr != nil {
			return nil, packageErr
		}
		settings["packages"], _ = json.Marshal(packages)
	}
	delete(settings, "extensions")
	for _, key := range []string{"skills", "prompts", "themes"} {
		if value, ok := settings[key]; ok {
			var paths []string
			if json.Unmarshal(value, &paths) != nil {
				return nil, errors.New("invalid Pi resource paths")
			}
			rebased := make([]string, 0, len(paths)*2)
			for _, p := range paths {
				prefix := ""
				if len(p) > 0 && strings.ContainsAny(p[:1], "!+-") {
					prefix = p[:1]
					p = p[1:]
					if !filepath.IsAbs(p) && !strings.HasPrefix(p, "~") {
						rebased = append(rebased, prefix+p)
					}
				}
				if !filepath.IsAbs(p) && !strings.HasPrefix(p, "~") {
					p = filepath.Join(original, p)
				}
				p = prefix + p
				rebased = append(rebased, p)
			}
			settings[key], _ = json.Marshal(rebased)
		}
	}
	if info, statErr := os.Stat(filepath.Join(original, "skills")); statErr == nil && info.IsDir() {
		var skills []string
		if value, ok := settings["skills"]; ok {
			_ = json.Unmarshal(value, &skills)
		}
		skills = append([]string{filepath.Join(original, "skills")}, skills...)
		settings["skills"], _ = json.Marshal(skills)
	}
	data, _ = json.Marshal(settings)
	if err = os.WriteFile(filepath.Join(dir, "settings.json"), data, 0600); err != nil {
		return nil, errors.New("cannot write private Pi settings")
	}
	if err = os.WriteFile(filepath.Join(dir, "multica-managed-mcp.json"), config, 0600); err != nil {
		return nil, errors.New("cannot write private MCP config")
	}
	entryJSON, _ := json.Marshal(filepath.ToSlash(adapter.entryPath))
	configJSON, _ := json.Marshal(filepath.ToSlash(filepath.Join(dir, "multica-managed-mcp.json")))
	bridge := fmt.Sprintf(`import { readFileSync } from "node:fs";
import { createMcpAdapter } from %s;
export default function(pi) {
  const config = JSON.parse(readFileSync(%s, "utf8"));
  const expected = JSON.stringify(Object.keys(config.mcpServers).sort());
  let resolveReady;
  const ready = new Promise(resolve => { resolveReady = resolve; });
  pi.events.on("pi-mcp-adapter/status/v1", snapshot => {
    if (snapshot && Array.isArray(snapshot.servers)) {
      resolveReady(JSON.stringify(snapshot.servers.map(server => server.name).sort()) === expected && snapshot.servers.every(server => ["connected", "cached", "not-connected"].includes(server.status)));
    }
  });
  createMcpAdapter({ config })(pi);
  pi.on("session_start", async () => {
    let timer;
    const initialized = await Promise.race([
      ready,
      new Promise(resolve => { timer = setTimeout(() => resolve(false), 30000); }),
    ]);
    clearTimeout(timer);
    process.stdout.write(JSON.stringify({type: initialized ? "multica_managed_mcp_ready" : "multica_managed_mcp_failed"}) + "\n");
  });
}
`, entryJSON, configJSON)
	path := filepath.Join(dir, "multica-mcp.ts")
	if err = os.WriteFile(path, []byte(bridge), 0600); err != nil {
		return nil, errors.New("cannot write Pi MCP extension")
	}
	run.args = []string{"--no-extensions", "--extension", path}

	run.env = append(append([]string{}, env...), "PI_CODING_AGENT_DIR="+dir)
	// A rebranded Pi uses a different agent-dir variable. Do not pretend its
	// storage and extension-loading behavior was validated by this adapter.
	if piEnvValue(env, "PI_PACKAGE_DIR") != "" {
		return nil, errors.New("custom Pi package directories are unsupported for managed MCP")
	}
	return run, nil
}

// Managed mode must not allow a second MCP extension/config to bypass the
// assignment. Native runs retain the existing custom_args behavior.
func validatePiMCPArgs(args []string) error {
	for _, arg := range args {
		flag, _, _ := strings.Cut(arg, "=")
		switch flag {
		case "-e", "--extension", "--mcp-config", "--no-extensions", "-ne":
			return errors.New("managed Pi MCP owns extension and MCP config arguments")
		}
	}
	return nil
}

// Keep installed package resources (including skills), but resolve packages
// to existing local directories so the isolated home never downloads them.
func piMCPResourcePackages(agentDir string, raw json.RawMessage) ([]map[string]any, error) {
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return nil, errors.New("invalid Pi packages")
	}
	packages := make([]map[string]any, 0, len(items))
	for _, item := range items {
		p := map[string]any{}
		var source string
		if json.Unmarshal(item, &source) != nil {
			if json.Unmarshal(item, &p) != nil {
				return nil, errors.New("invalid Pi package")
			}
			source, _ = p["source"].(string)
		}
		root := source
		if strings.HasPrefix(source, "npm:") {
			name := strings.TrimPrefix(source, "npm:")
			if i := strings.LastIndex(name, "@"); i > 0 {
				name = name[:i]
			}
			root = filepath.Join(agentDir, "npm", "node_modules", filepath.FromSlash(name))
		} else if !filepath.IsAbs(source) {
			return nil, errors.New("managed Pi MCP requires installed npm packages or absolute local package paths")
		}
		if info, err := os.Stat(root); err != nil || !info.IsDir() {
			return nil, errors.New("Pi package resources are not installed locally")
		}
		p["source"] = root
		p["extensions"] = []string{}
		packages = append(packages, p)
	}
	return packages, nil
}
