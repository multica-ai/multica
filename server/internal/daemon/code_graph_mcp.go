package daemon

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"github.com/multica-ai/multica/server/pkg/codegraph"
)

// Keep aligned with packages/core/agents/mcp-support.ts. Unlike the remote
// broker, this server needs no upstream discovery or provider-specific pinning.
func providerSupportsCodeGraphMCP(provider string) bool {
	return codegraph.SupportsLocalTools("local", provider)
}

// The task identity is fixed; repository grants are resolved on every server call.
func codeGraphMCPConfig(baseURL, taskToken string) (json.RawMessage, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
		return nil, errors.New("invalid CodeGraph server URL")
	}
	if !strings.HasPrefix(taskToken, "mat_") {
		return nil, errors.New("CodeGraph requires a task token")
	}
	return json.Marshal(map[string]any{"mcpServers": map[string]any{
		"multica-codegraph": map[string]any{"type": "http", "url": strings.TrimRight(baseURL, "/") + "/api/codegraph/mcp", "headers": map[string]string{"Authorization": "Bearer " + taskToken}},
	}})
}
