package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"sort"
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// managedMcpSnapshot belongs to a single task and dispatch generation. Keeping
// it outside task context prevents retries from inheriting their parent's data.
type managedMcpSnapshot struct {
	Version      int                `json:"version"`
	TaskID       string             `json:"task_id"`
	Attempt      int32              `json:"attempt"`
	DispatchedAt time.Time          `json:"dispatched_at"`
	Servers      []ManagedMcpSource `json:"servers"`
}

// Read the effective container, matching canonical-first runtime consumption.
// Workspace resolution normalizes both spellings before reaching this point.
func managedMcpConfigSources(raw json.RawMessage, source string) (map[string]string, error) {
	sources := make(map[string]string)
	if !hasManagedJSON(raw) {
		return sources, nil
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	serversRaw, canonical := doc["mcpServers"]
	if !canonical {
		serversRaw = doc["mcp"]
	}
	servers, err := unmarshalServerMap(serversRaw)
	if err != nil {
		return nil, err
	}
	for name := range servers {
		sources[name] = source
	}
	return sources, nil
}

func sortedManagedMcpSources(sources map[string]string) []ManagedMcpSource {
	result := make([]ManagedMcpSource, 0, len(sources))
	for name, source := range sources {
		result = append(result, ManagedMcpSource{Name: name, Source: source})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func encodeManagedMcpSnapshot(task db.AgentTaskQueue, sources *[]ManagedMcpSource) ([]byte, error) {
	if sources == nil {
		return nil, nil
	}
	return json.Marshal(managedMcpSnapshot{
		Version: 1, TaskID: uuidToString(task.ID), Attempt: task.Attempt,
		DispatchedAt: task.DispatchedAt.Time, Servers: *sources,
	})
}

func decodeManagedMcpSnapshot(task db.AgentTaskQueue) *[]ManagedMcpSource {
	var snapshot managedMcpSnapshot
	decoder := json.NewDecoder(bytes.NewReader(task.ManagedMcpSnapshot))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&snapshot); err != nil {
		return nil
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil
	}
	if snapshot.Version != 1 || snapshot.TaskID != uuidToString(task.ID) || snapshot.Attempt != task.Attempt ||
		!task.DispatchedAt.Valid || !snapshot.DispatchedAt.Equal(task.DispatchedAt.Time) || snapshot.Servers == nil {
		return nil
	}
	sources := make(map[string]string, len(snapshot.Servers))
	for _, server := range snapshot.Servers {
		if server.Name == "" {
			return nil
		}
		switch server.Source {
		case "workspace", "agent", "task":
		default:
			return nil
		}
		if _, duplicate := sources[server.Name]; duplicate {
			return nil
		}
		sources[server.Name] = server.Source
	}
	result := sortedManagedMcpSources(sources)
	return &result
}
