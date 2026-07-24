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

func cerebroProviderCapabilities(providerType, executable string) map[string]any {
	fallback := capabilities.LegacyProviderMap(providerType)
	if providerType != "claude" {
		return fallback
	}
	cacheKey := providerType + "\x00" + executable
	if cached, ok := providerCapabilityProbeCache.Load(cacheKey); ok {
		return cached.(map[string]any)
	}
	tools, err := probeClaudeTools(executable)
	if err != nil || len(tools) == 0 {
		providerCapabilityProbeCache.Store(cacheKey, fallback)
		return fallback
	}
	fallback["tools"] = tools
	fallback["discovery_method"] = "probed"
	providerCapabilityProbeCache.Store(cacheKey, fallback)
	return fallback
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
