package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// opencodeMCPLocal mirrors the McpLocalConfig slice of OpenCode's config
// schema (https://opencode.ai/config.json). Decoded with
// DisallowUnknownFields so any field outside this struct fails validation
// before the daemon hands the config to OpenCode — matching the schema's
// `additionalProperties: false`.
type opencodeMCPLocal struct {
	Type        string            `json:"type"`
	Command     []string          `json:"command"`
	Environment map[string]string `json:"environment,omitempty"`
	Enabled     *bool             `json:"enabled,omitempty"`
	Timeout     *int              `json:"timeout,omitempty"`
}

// opencodeMCPRemote mirrors the McpRemoteConfig slice. OAuth is held as
// json.RawMessage because the schema allows two shapes (an oauth-config
// object OR the literal `false`); the type discriminator is checked in
// validateOpenCodeOAuth.
type opencodeMCPRemote struct {
	Type    string            `json:"type"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	OAuth   json.RawMessage   `json:"oauth,omitempty"`
	Enabled *bool             `json:"enabled,omitempty"`
	Timeout *int              `json:"timeout,omitempty"`
}

// opencodeMCPEnabledOnly is the third shape OpenCode's schema accepts —
// a bare `{"enabled": true|false}` entry that toggles a server inherited
// from global / project config without redefining it. The discriminator
// is "no `type` field, but `enabled` is set".
type opencodeMCPEnabledOnly struct {
	Enabled *bool `json:"enabled"`
}

// opencodeMCPOAuth mirrors McpOAuthConfig. callbackPort is range-checked
// in validateOpenCodeOAuth (the schema requires 1..65535). It is held
// as `*int` so the absent / unset case (nil) and an explicit
// `"callbackPort": 0` (rejected as out-of-range) are distinguishable —
// Go's int zero value would otherwise collapse them.
type opencodeMCPOAuth struct {
	ClientID     string `json:"clientId,omitempty"`
	ClientSecret string `json:"clientSecret,omitempty"`
	Scope        string `json:"scope,omitempty"`
	CallbackPort *int   `json:"callbackPort,omitempty"`
	RedirectURI  string `json:"redirectUri,omitempty"`
}

// buildOpenCodeMCPConfigContent translates an agent.mcp_config payload into
// a JSON string carrying just the `mcp` slice of OpenCode's config schema,
// suitable for the OPENCODE_CONFIG_CONTENT environment variable. Returns
// ("", nil) when raw is empty so callers can skip the env entry entirely
// instead of injecting an empty config.
//
// OPENCODE_CONFIG_CONTENT is OpenCode's general inline-config injection
// mechanism — it accepts any subset of OpenCode's schema (model, agent,
// mode, plugin, mcp, …), not just MCP. This function is scoped to MCP
// because that's the agent.mcp_config field this PR plumbs through; if a
// future Multica field needs to project into the same env var (e.g. an
// agent-level model override), the assemble-and-inject step would move
// up a layer and merge multiple slices into one OPENCODE_CONFIG_CONTENT
// value. For now, MCP is the only consumer.
//
// Why env-var injection vs writing <workdir>/opencode.json: the task workdir
// is reused across turns for the same (agent, issue), and the agent itself
// (or the user) may have written model / tools / permission settings into
// <workdir>/opencode.json. Writing or removing that file as part of the
// mcp_config lifecycle would silently overwrite their state. The env-var
// approach avoids the workdir entirely — nothing is written to disk and no
// cleanup is needed because env dies with the spawned process.
//
// OPENCODE_CONFIG_CONTENT was added to OpenCode in v1.4.10 (2025-09) and is
// the same mechanism the official @opencode-ai/sdk uses to inject runtime
// config. OpenCode merges it AFTER the project-config loop at "local" scope,
// so it deep-merges with global + project config (same observable behaviour
// as writing into <workdir>/opencode.json), but its later merge position
// also gives daemon-injected entries precedence over any same-key entry
// the user happened to put in their project file — which matches the
// semantics of agent.mcp_config being the authoritative daemon-managed
// field.
func buildOpenCodeMCPConfigContent(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	servers, err := translateMCPConfigForOpenCode(raw)
	if err != nil {
		return "", err
	}
	// Empty result (no servers after translation) is observably the same as
	// raw being nil — return "" so the caller skips the env entry, and a
	// JSON null / empty object never clobbers what the user put in
	// agent.custom_env.OPENCODE_CONFIG_CONTENT.
	if len(servers) == 0 {
		return "", nil
	}
	data, err := json.Marshal(map[string]any{"mcp": servers})
	if err != nil {
		return "", fmt.Errorf("opencode mcp_config: marshal: %w", err)
	}
	return string(data), nil
}

// translateMCPConfigForOpenCode converts an agent.mcp_config payload into the
// shape OpenCode expects under its `mcp` key. Two input shapes are accepted:
//
//   - Claude-style `{"mcpServers": {name: {url|command, ...}}}` — translated
//     into OpenCode's `type: "local"|"remote"` form, command coerced to an
//     array, env renamed to environment, etc.
//   - Native OpenCode `{"mcp": {name: {type, ...}}}` — passed through after
//     validating each entry against OpenCode's schema. Without validation,
//     a malformed agent.mcp_config would be surfaced to OpenCode verbatim
//     and either silently disable the server or crash the CLI at startup.
func translateMCPConfigForOpenCode(raw json.RawMessage) (map[string]any, error) {
	var payload struct {
		MCPServers map[string]map[string]any  `json:"mcpServers"`
		MCP        map[string]json.RawMessage `json:"mcp"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("opencode mcp_config: parse mcp_config: %w", err)
	}
	if len(payload.MCPServers) == 0 {
		if payload.MCP == nil {
			return map[string]any{}, nil
		}
		return validateOpenCodeNativeMCPMap(payload.MCP)
	}

	servers := make(map[string]any, len(payload.MCPServers)+len(payload.MCP))
	for name, rawEntry := range payload.MCP {
		validated, err := validateOpenCodeNativeMCPEntry(name, rawEntry)
		if err != nil {
			return nil, err
		}
		servers[name] = validated
	}
	for name, server := range payload.MCPServers {
		translated, err := translateMCPServerForOpenCode(name, server)
		if err != nil {
			return nil, err
		}
		// Re-validate the translated entry through the native validator so
		// both input shapes — Claude-style `mcpServers` and OpenCode-native
		// `mcp` — are gated by the same schema rules. Without this re-check,
		// Claude-style inputs with malformed `headers`, `environment`,
		// `oauth`, or `timeout` values would bypass daemon validation and
		// surface as a confusing OpenCode startup error instead of a clear
		// daemon-side rejection. One validator, one source of truth.
		rawTranslated, err := json.Marshal(translated)
		if err != nil {
			return nil, fmt.Errorf("opencode mcp_config: server %q: marshal translated entry: %w", name, err)
		}
		validated, err := validateOpenCodeNativeMCPEntry(name, rawTranslated)
		if err != nil {
			return nil, err
		}
		servers[name] = validated
	}
	return servers, nil
}

// validateOpenCodeNativeMCPMap validates every entry in a native-shape
// mcp map and returns a parallel map[string]any of validated entries
// (each entry round-tripped through json so the output is the verbatim
// representation OpenCode would observe in its config).
func validateOpenCodeNativeMCPMap(mcp map[string]json.RawMessage) (map[string]any, error) {
	out := make(map[string]any, len(mcp))
	for name, raw := range mcp {
		validated, err := validateOpenCodeNativeMCPEntry(name, raw)
		if err != nil {
			return nil, err
		}
		out[name] = validated
	}
	return out, nil
}

// validateOpenCodeNativeMCPEntry strict-decodes one native-shape entry
// against OpenCode's schema and returns the equivalent map[string]any
// representation. The decode is intentionally strict
// (DisallowUnknownFields) — any field outside the McpLocalConfig /
// McpRemoteConfig / `{enabled: bool}` shapes is rejected, matching the
// schema's `additionalProperties: false` and surfacing user typos as
// errors before they reach OpenCode.
func validateOpenCodeNativeMCPEntry(name string, raw json.RawMessage) (map[string]any, error) {
	wrap := func(err error) error {
		return fmt.Errorf("opencode mcp_config: server %q: %w", name, err)
	}

	// JSON-object guard: the discriminator probe and strict decoders
	// below assume an object; without this guard a primitive (string,
	// number, array, null) would surface a confusing decoder error.
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, wrap(errors.New("entry must be a JSON object"))
	}

	// Discriminator probe: peek `type` to choose the right strict
	// decode target. This first decode is intentionally permissive so
	// "type: 5" surfaces a clear "type must be a string" error rather
	// than the strict-decode generic "json: cannot unmarshal number".
	var probe struct {
		Type *json.RawMessage `json:"type,omitempty"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, wrap(fmt.Errorf("parse: %w", err))
	}
	var typeStr string
	if probe.Type != nil {
		if err := json.Unmarshal(*probe.Type, &typeStr); err != nil {
			return nil, wrap(fmt.Errorf("`type` must be a string, got %s", strings.TrimSpace(string(*probe.Type))))
		}
	}

	switch typeStr {
	case "local":
		var entry opencodeMCPLocal
		if err := strictDecode(raw, &entry); err != nil {
			return nil, wrap(err)
		}
		if len(entry.Command) == 0 {
			return nil, wrap(errors.New("local server missing required field `command`"))
		}
		if entry.Timeout != nil && *entry.Timeout <= 0 {
			return nil, wrap(fmt.Errorf("`timeout` must be a positive integer, got %d", *entry.Timeout))
		}
	case "remote":
		var entry opencodeMCPRemote
		if err := strictDecode(raw, &entry); err != nil {
			return nil, wrap(err)
		}
		if entry.URL == "" {
			return nil, wrap(errors.New("remote server missing required field `url`"))
		}
		if entry.Timeout != nil && *entry.Timeout <= 0 {
			return nil, wrap(fmt.Errorf("`timeout` must be a positive integer, got %d", *entry.Timeout))
		}
		if len(entry.OAuth) > 0 {
			if err := validateOpenCodeOAuth(entry.OAuth); err != nil {
				return nil, wrap(fmt.Errorf("`oauth`: %w", err))
			}
		}
	case "":
		// No `type` field. The bare `{"enabled": bool}` override shape
		// is OpenCode's third native variant; anything else without a
		// type is a malformed local/remote attempt. Surface a single
		// friendly "missing type" error instead of the strict-decode
		// "json: unknown field" leak — the user usually didn't realise
		// they were mis-using the override shape.
		var entry opencodeMCPEnabledOnly
		if err := strictDecode(raw, &entry); err != nil || entry.Enabled == nil {
			return nil, wrap(errors.New("missing required field `type` (must be \"local\" or \"remote\", or use bare {\"enabled\": bool} to override an inherited server)"))
		}
	default:
		return nil, wrap(fmt.Errorf("invalid type %q (must be \"local\" or \"remote\")", typeStr))
	}

	// Validation passed; re-decode the raw bytes into map[string]any for
	// the output. Identical observable representation, just typed as a
	// generic map for the caller.
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, wrap(fmt.Errorf("parse: %w", err))
	}
	return out, nil
}

// strictDecode runs a json.Decoder with DisallowUnknownFields so any
// field outside the target struct's tags is rejected, enforcing the
// schema's `additionalProperties: false`.
func strictDecode(raw json.RawMessage, target any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(target)
}

// validateOpenCodeOAuth enforces the `oauth: McpOAuthConfig | false`
// union from OpenCode's schema. The literal `false` disables OAuth
// entirely (overriding the auto-detection default); any other primitive
// or `true` is rejected as ambiguous.
func validateOpenCodeOAuth(raw json.RawMessage) error {
	trimmed := bytes.TrimSpace(raw)
	if bytes.Equal(trimmed, []byte("false")) {
		return nil
	}
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return fmt.Errorf("must be an object or `false`, got %s", string(trimmed))
	}
	var oauth opencodeMCPOAuth
	if err := strictDecode(raw, &oauth); err != nil {
		return err
	}
	// callbackPort: nil means absent (legal); any concrete value must be
	// in 1..65535 per the schema. The pointer type lets us reject an
	// explicit `"callbackPort": 0` instead of silently accepting it as
	// the Go int zero value.
	if oauth.CallbackPort != nil && (*oauth.CallbackPort < 1 || *oauth.CallbackPort > 65535) {
		return fmt.Errorf("`callbackPort` must be in 1..65535, got %d", *oauth.CallbackPort)
	}
	return nil
}

// translateMCPServerForOpenCode converts one Claude-style mcpServers entry
// into an OpenCode native entry. The `enabled` field is only emitted when
// the source explicitly sets it: OpenCode defaults to enabled when absent,
// so hard-injecting `enabled: true` would only add noise to the merged
// config.
func translateMCPServerForOpenCode(name string, server map[string]any) (map[string]any, error) {
	if url, ok := stringField(server, "url"); ok && url != "" {
		out := map[string]any{
			"type": "remote",
			"url":  url,
		}
		if v, ok := server["enabled"].(bool); ok {
			out["enabled"] = v
		}
		copyIfPresent(out, server, "headers")
		copyIfPresent(out, server, "oauth")
		copyIfPresent(out, server, "timeout")
		return out, nil
	}

	command, err := openCodeCommand(server)
	if err != nil {
		return nil, fmt.Errorf("server %q: %w", name, err)
	}
	if len(command) == 0 {
		return nil, fmt.Errorf("server %q has neither url nor command", name)
	}
	out := map[string]any{
		"type":    "local",
		"command": command,
	}
	if v, ok := server["enabled"].(bool); ok {
		out["enabled"] = v
	}
	if env, ok := server["env"]; ok {
		out["environment"] = env
	} else {
		copyIfPresent(out, server, "environment")
	}
	copyIfPresent(out, server, "timeout")
	return out, nil
}

// openCodeCommand normalises the `command` field into a string slice.
// Claude's mcpServers accepts a single string with separate `args`; OpenCode
// expects one combined array. A pre-existing array (used by some MCP
// generators) is also passed through after a type check.
func openCodeCommand(server map[string]any) ([]string, error) {
	raw, ok := server["command"]
	if !ok {
		return nil, nil
	}
	switch v := raw.(type) {
	case string:
		cmd := []string{v}
		args, err := stringSliceField(server, "args")
		if err != nil {
			return nil, err
		}
		return append(cmd, args...), nil
	case []any:
		cmd := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("command array must contain only strings")
			}
			cmd = append(cmd, s)
		}
		return cmd, nil
	default:
		return nil, fmt.Errorf("command must be a string or string array")
	}
}

func stringField(m map[string]any, key string) (string, bool) {
	v, ok := m[key].(string)
	return v, ok
}

func stringSliceField(m map[string]any, key string) ([]string, error) {
	raw, ok := m[key]
	if !ok {
		return nil, nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an array", key)
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("%s must contain only strings", key)
		}
		out = append(out, s)
	}
	return out, nil
}

func copyIfPresent(dst, src map[string]any, key string) {
	if v, ok := src[key]; ok {
		dst[key] = v
	}
}
