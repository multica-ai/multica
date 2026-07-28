package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/cerebro/capabilities"
)

var providerCapabilityProbeCache sync.Map

// discoveryNotMeasured marks a snapshot whose tool inventory could not be
// measured on this host. It is deliberately distinct from "static" (a curated
// list we stand behind) and from "unmapped" (no provider row at all): an empty
// inventory silently becomes a total lockout at claim time, so the state has to
// be nameable before anything can surface it to an operator.
const discoveryNotMeasured = "not_measured"

// cerebroProviderCapabilities reports the runtime's capability snapshot. Every
// provider is measured when a probe exists for it (see providerInventories in
// cerebro_runtime_tool_probe.go) and otherwise falls back to the curated static
// row, flagged as not measured when that row carries no tools.
//
// Only successful probes are cached. A failed probe costs one spawn on the next
// register or version-drift refresh, which is rare, and re-probing is what makes
// a transient failure self-heal instead of pinning the runtime to an empty
// inventory until the daemon restarts.
func cerebroProviderCapabilities(providerType, executable string) map[string]any {
	fallback := capabilities.LegacyProviderMap(providerType)
	cacheKey := providerType + "\x00" + executable
	if cached, ok := providerCapabilityProbeCache.Load(cacheKey); ok {
		return cached.(map[string]any)
	}
	tools, measured := probeProviderTools(providerType, executable)
	if !measured {
		return markUnmeasuredCapabilities(fallback)
	}
	fallback["tools"] = tools
	fallback["discovery_method"] = "probed"
	providerCapabilityProbeCache.Store(cacheKey, fallback)
	return fallback
}

// markUnmeasuredCapabilities flags a snapshot that carries no tools, so an
// operator sees "we could not measure this runtime" instead of an empty table
// that looks like a permission decision.
func markUnmeasuredCapabilities(snapshot map[string]any) map[string]any {
	if existing, ok := snapshot["tools"].([]string); ok && len(existing) > 0 {
		return snapshot
	}
	snapshot["discovery_method"] = discoveryNotMeasured
	return snapshot
}

func probeClaudeTools(executable string) ([]string, error) {
	if strings.TrimSpace(executable) == "" {
		executable = "claude"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "-p", "OK", "--output-format", "stream-json", "--verbose", "--max-turns", "0")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), 10<<20)
	for scanner.Scan() {
		var init struct {
			Type    string   `json:"type"`
			Subtype string   `json:"subtype"`
			Tools   []string `json:"tools"`
		}
		if json.Unmarshal(scanner.Bytes(), &init) != nil || init.Type != "system" || init.Subtype != "init" {
			continue
		}
		tools := make([]string, 0, len(init.Tools))
		for _, tool := range init.Tools {
			if tool = strings.TrimSpace(tool); tool != "" {
				tools = append(tools, tool)
			}
		}
		return tools, nil
	}
	return nil, scanner.Err()
}
