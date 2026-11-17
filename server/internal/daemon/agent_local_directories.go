package daemon

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/util"
)

const agentLocalDirectoriesEnv = "MULTICA_AGENT_LOCAL_DIRECTORIES"

func parseAgentLocalDirectories(raw string) (map[string]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, fmt.Errorf("%s: expected a JSON object mapping agent UUIDs to absolute directories", agentLocalDirectoriesEnv)
	}
	directories := make(map[string]string)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("%s: read agent UUID: %w", agentLocalDirectoriesEnv, err)
		}
		id, ok := token.(string)
		if !ok {
			return nil, fmt.Errorf("%s: expected an agent UUID key", agentLocalDirectoriesEnv)
		}
		// pgx accepts arbitrary UUID separators; validate syntax before its scan.
		if err := uuid.Validate(id); err != nil {
			return nil, fmt.Errorf("%s: invalid agent UUID %q: %w", agentLocalDirectoriesEnv, id, err)
		}
		parsed, err := util.ParseUUID(id)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", agentLocalDirectoriesEnv, err)
		}
		canonicalID := util.UUIDToString(parsed)
		if _, exists := directories[canonicalID]; exists {
			return nil, fmt.Errorf("%s: duplicate agent UUID %q", agentLocalDirectoriesEnv, canonicalID)
		}
		var path string
		if err := decoder.Decode(&path); err != nil {
			return nil, fmt.Errorf("%s: agent %s directory must be a string: %w", agentLocalDirectoriesEnv, canonicalID, err)
		}
		normalized, err := normalizeLocalPath(path)
		if err != nil {
			return nil, fmt.Errorf("%s: agent %s: %w", agentLocalDirectoriesEnv, canonicalID, err)
		}
		directories[canonicalID] = normalized
	}
	if _, err := decoder.Token(); err != nil {
		return nil, fmt.Errorf("%s: read directory mapping: %w", agentLocalDirectoriesEnv, err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("%s: expected exactly one JSON object", agentLocalDirectoriesEnv)
	}
	return directories, nil
}
