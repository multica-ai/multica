package runtime

import (
	"context"
	"fmt"
	"strings"

	"github.com/multica-ai/multica/server/internal/cerebro/taskmandate"
)

type gatewayAllowedToolsContextKey struct{}

func withGatewayAllowedTools(ctx context.Context, allowedTools []string) context.Context {
	return context.WithValue(ctx, gatewayAllowedToolsContextKey{}, append([]string(nil), allowedTools...))
}

func gatewayAllowedTools(ctx context.Context) []string {
	allowed, _ := ctx.Value(gatewayAllowedToolsContextKey{}).([]string)
	return append([]string(nil), allowed...)
}

type connectionNamedTool interface {
	ConnectionName() string
}

func compileGatewayAdmissionSurface(tools []Tool, allowedTools []string) ([]Tool, []string, error) {
	candidates := make([]taskmandate.AdmissionCandidate, 0, len(tools))
	for _, tool := range tools {
		if tool == nil {
			return nil, nil, fmt.Errorf("task mandate admission: nil Gateway tool")
		}
		callable := tool.Name()
		scopes := make([]string, 0, 2)
		if scoped, ok := tool.(connectionNamedTool); ok {
			if connection := strings.TrimSpace(scoped.ConnectionName()); connection != "" {
				scopes = append(scopes, "connection:"+connection)
			}
		}
		if _, rawMCP := tool.(*gatewayMCPTool); rawMCP {
			if wildcard := taskmandate.MCPServerWildcard(callable); wildcard != "" {
				scopes = append(scopes, wildcard)
			}
		}
		candidates = append(candidates, taskmandate.AdmissionCandidate{
			CallableIdentity:          callable,
			Aliases:                   taskmandate.CompatibilityAdmissionAliases(callable),
			ConnectionScopeIdentities: scopes,
		})
	}
	compiled, err := taskmandate.CompileAdmissionSurface(candidates, allowedTools)
	if err != nil {
		return nil, nil, err
	}
	filtered := make([]Tool, 0, len(compiled.SourceIndexes()))
	for _, index := range compiled.SourceIndexes() {
		filtered = append(filtered, tools[index])
	}
	return filtered, compiled.AuthorizationIdentities(), nil
}
