// Package mcpvalidate provides schema validation for MCP (Model Context
// Protocol) server configurations stored on agents. It is called during
// agent create/update to reject malformed configs early instead of letting
// them bubble up as opaque runtime errors inside the daemon.
//
// The accepted shape mirrors the de-facto standard used by Claude, Codex,
// Hermes, and other MCP-aware CLIs:
//
//	{
//	  "mcpServers": {
//	    "name": {
//	      "command": "npx",             // stdio transport
//	      "args": ["-y", "@scope/server"],
//	      "env": {"KEY": "val"}
//	    },
//	    "other": {
//	      "type": "sse",                // HTTP/SSE transport
//	      "url": "https://example.com/mcp",
//	      "headers": {"Auth": "Bearer …"}
//	    }
//	  }
//	}
package mcpvalidate

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Validate checks raw MCP config JSON. Returns nil when raw is empty or
// nil (clearing the config is always valid). Returns a descriptive error
// on schema violations.
func Validate(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}

	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		return fmt.Errorf("mcp_config must be a JSON object: %w", err)
	}

	serversRaw, ok := root["mcpServers"]
	if !ok {
		return fmt.Errorf("mcp_config must contain a top-level \"mcpServers\" key")
	}

	// Allow an empty mcpServers object (effectively clears all servers).
	if strings.TrimSpace(string(serversRaw)) == "{}" {
		return nil
	}

	var servers map[string]json.RawMessage
	if err := json.Unmarshal(serversRaw, &servers); err != nil {
		return fmt.Errorf("\"mcpServers\" must be a JSON object: %w", err)
	}

	for name, entryRaw := range servers {
		if err := validateEntry(name, entryRaw); err != nil {
			return err
		}
	}
	return nil
}

// validateEntry checks a single MCP server entry. Two shapes are accepted:
//   - stdio: has "command" (string), optional "args" ([]string), optional "env" (object)
//   - http/sse: has "type" ("sse" or "streamable-http"), "url" (string),
//     optional "headers" (object)
//
// An entry with neither "command" nor "type" is rejected.
func validateEntry(name string, raw json.RawMessage) error {
	var entry map[string]json.RawMessage
	if err := json.Unmarshal(raw, &entry); err != nil {
		return fmt.Errorf("mcpServers.%s: must be a JSON object: %w", name, err)
	}

	hasCommand := entryHasString(entry, "command")
	hasType := entryHasString(entry, "type")

	switch {
	case hasCommand:
		return validateStdioEntry(name, entry)
	case hasType:
		return validateHTTPEntry(name, entry)
	default:
		return fmt.Errorf("mcpServers.%s: must have either \"command\" (stdio) or \"type\" (HTTP/SSE)", name)
	}
}

func validateStdioEntry(name string, entry map[string]json.RawMessage) error {
	cmd, _ := entry["command"]
	var cmdStr string
	if err := json.Unmarshal(cmd, &cmdStr); err != nil || cmdStr == "" {
		return fmt.Errorf("mcpServers.%s: \"command\" must be a non-empty string", name)
	}

	if argsRaw, ok := entry["args"]; ok {
		var args []json.RawMessage
		if err := json.Unmarshal(argsRaw, &args); err != nil {
			return fmt.Errorf("mcpServers.%s: \"args\" must be an array", name)
		}
		for i, arg := range args {
			var s string
			if err := json.Unmarshal(arg, &s); err != nil {
				return fmt.Errorf("mcpServers.%s: args[%d] must be a string", name, i)
			}
		}
	}

	if envRaw, ok := entry["env"]; ok {
		if err := validateStringMap(name, "env", envRaw); err != nil {
			return err
		}
	}
	return nil
}

func validateHTTPEntry(name string, entry map[string]json.RawMessage) error {
	var typ string
	if err := json.Unmarshal(entry["type"], &typ); err != nil {
		return fmt.Errorf("mcpServers.%s: \"type\" must be a string", name)
	}
	if typ != "sse" && typ != "streamable-http" {
		return fmt.Errorf("mcpServers.%s: \"type\" must be \"sse\" or \"streamable-http\", got %q", name, typ)
	}

	urlRaw, ok := entry["url"]
	if !ok {
		return fmt.Errorf("mcpServers.%s: \"url\" is required for HTTP/SSE transport", name)
	}
	var urlStr string
	if err := json.Unmarshal(urlRaw, &urlStr); err != nil || urlStr == "" {
		return fmt.Errorf("mcpServers.%s: \"url\" must be a non-empty string", name)
	}

	if headersRaw, ok := entry["headers"]; ok {
		if err := validateStringMap(name, "headers", headersRaw); err != nil {
			return err
		}
	}
	return nil
}

func validateStringMap(entryName, field string, raw json.RawMessage) error {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return fmt.Errorf("mcpServers.%s: %q must be a JSON object", entryName, field)
	}
	for k, v := range m {
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			return fmt.Errorf("mcpServers.%s: %s.%s must be a string", entryName, field, k)
		}
	}
	return nil
}

func entryHasString(entry map[string]json.RawMessage, key string) bool {
	raw, ok := entry[key]
	if !ok {
		return false
	}
	var s string
	return json.Unmarshal(raw, &s) == nil
}
