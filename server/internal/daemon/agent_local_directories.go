package daemon

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/multica-ai/multica/server/internal/util"
)

const agentLocalDirectoriesEnv = "MULTICA_AGENT_LOCAL_DIRECTORIES"

func parseAgentLocalDirectories(raw string) (map[string]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var entries map[string]string
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return nil, fmt.Errorf("%s: expected a JSON object mapping agent UUIDs to absolute directories: %w", agentLocalDirectoriesEnv, err)
	}
	if entries == nil {
		return nil, fmt.Errorf("%s: expected a JSON object, not null", agentLocalDirectoriesEnv)
	}
	directories := make(map[string]string, len(entries))
	for id, path := range entries {
		parsed, err := util.ParseUUID(id)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", agentLocalDirectoriesEnv, err)
		}
		canonicalID := util.UUIDToString(parsed)
		if _, exists := directories[canonicalID]; exists {
			return nil, fmt.Errorf("%s: duplicate agent UUID %q", agentLocalDirectoriesEnv, canonicalID)
		}
		normalized, err := normalizeLocalPath(path)
		if err != nil {
			return nil, fmt.Errorf("%s: agent %s: %w", agentLocalDirectoriesEnv, canonicalID, err)
		}
		directories[canonicalID] = normalized
	}
	return directories, nil
}
