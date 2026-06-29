package runtime

// api_connection_tools.go is FIR-2166 "C" PR2: it turns any enabled API-type
// workspace connection into agent-callable tools — one tool per allowed endpoint
// (`<METHOD> <path>` from the connection's endpoint_permissions) — and dispatches
// those calls SERVER-SIDE from the Cerebro backend to the connection's URL with
// the connection's auth_config applied. The agent only ever sees the tool; the
// credential never leaves the backend, and because this code runs on the Cerebro
// backend (inside the internal network) it can reach `.internal` connection URLs
// directly without the daemon relay.
//
// The whole feature sits behind the workspace feature flag
// `cerebro_api_connection_tools`, default OFF (apiConnectionToolsEnabled). With
// the flag off no workspace is affected. Per-agent enforcement is OPT-IN
// (FIR-2166 "C" v2): even with the flag ON, an endpoint is exposed/callable only
// for an actor with an explicit Allow grant (toolpolicy.ConnectionEndpointEffective
// is default-deny) — every other and every NEW agent is denied. So turning the
// flag on does not, by itself, grant any agent access to any API connection.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/cerebro/connections"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

// apiConnectionToolsEnabled reports whether the cerebro_api_connection_tools
// workspace flag is on. Default OFF: a nil cerebro handle, a missing override, or
// a DB error all resolve to false, so this never switches itself on by accident
// (it mirrors policyCELEvaluator's fail-to-default-OFF, not the always-on gates).
func (e *FirtalGatewayExecutor) apiConnectionToolsEnabled(ctx context.Context, workspaceID pgtype.UUID) bool {
	if e == nil || e.cerebro == nil {
		return false
	}
	enabled, err := e.cerebro.GetCerebroFeatureFlag(ctx, cerebrodb.GetCerebroFeatureFlagParams{
		WorkspaceID: workspaceID,
		UserID:      pgtype.UUID{Valid: true}, // all-zero sentinel = workspace-level row
		FlagKey:     flagAPIConnectionTools,
	})
	if err != nil {
		return false
	}
	return enabled
}

// apiConnectionTools discovers the workspace's enabled API connections and
// returns one tool per allowed endpoint, or nil when the feature is off, no store
// is wired, or there are none. A discovery error is logged and swallowed — the
// feature is additive and flag-gated, so a transient blip must never break the
// tool loop for the rest of the agent's tools.
func (e *FirtalGatewayExecutor) apiConnectionTools(ctx context.Context, workspaceID pgtype.UUID) []Tool {
	if e == nil || e.apiConnStore == nil || !workspaceID.Valid {
		return nil
	}
	if !e.apiConnectionToolsEnabled(ctx, workspaceID) {
		return nil
	}
	conns, err := e.apiConnStore.ListEnabled(ctx, workspaceID)
	if err != nil {
		e.logger.Warn("firtal gateway: list enabled API connections failed",
			"workspace_id", util.UUIDToString(workspaceID), "error", err)
		return nil
	}
	return buildAPIConnectionTools(conns, &http.Client{Timeout: apiConnectionToolTimeout})
}

// flagAPIConnectionTools is the workspace feature flag that exposes API-connection
// endpoints as agent tools. Default OFF (see apiConnectionToolsEnabled).
const flagAPIConnectionTools = "cerebro_api_connection_tools"

// apiConnectionToolTimeout caps a single endpoint dispatch.
const apiConnectionToolTimeout = 45 * time.Second

// apiConnectionResponseLimit bounds how much of an endpoint response is read
// back into the tool result, so a large/streaming body can't blow up the
// transcript or memory.
const apiConnectionResponseLimit = 1 << 20 // 1 MiB

// pathParamRe matches a `{name}` path parameter placeholder in an endpoint path.
var pathParamRe = regexp.MustCompile(`\{([^/{}]+)\}`)

// APIConnectionTool exposes one allowed endpoint (`<METHOD> <path>`) of an
// enabled API-type connection as a Tool. Dispatch is server-side: Call issues the
// HTTP request to baseURL+path with auth applied and returns the response body.
type APIConnectionTool struct {
	toolName    string
	description string
	schema      map[string]any

	connName string // workspace_connection.name — the policy key suffix (PR3)
	baseURL  string // connection URL, no trailing slash
	method   string // upper-case HTTP method
	path     string // endpoint path, may contain {param} placeholders
	auth     connections.AuthConfig

	client *http.Client
}

func (t *APIConnectionTool) Name() string                { return t.toolName }
func (t *APIConnectionTool) Description() string         { return t.description }
func (t *APIConnectionTool) InputSchema() map[string]any { return t.schema }

// ConnectionName / Method / Path expose the endpoint identity so the PR3
// call-time guard can resolve toolpolicy.ConnectionEndpointEffective without
// re-parsing the synthetic tool name.
func (t *APIConnectionTool) ConnectionName() string { return t.connName }
func (t *APIConnectionTool) Method() string         { return t.method }
func (t *APIConnectionTool) Path() string           { return t.path }

// BuildAPIConnectionTools is the exported wrapper around buildAPIConnectionTools
// (FIR-2273). It lets sibling cerebro packages — notably the connectiontools
// HTTP handler that fronts api-type connections for the Multica MCP server —
// build the exact same per-endpoint *APIConnectionTool set the Firtal Gateway
// builds, so the two surfaces can never drift. Identity, the feature flag, and
// the per-agent endpoint gate are the caller's responsibility; this only turns a
// connection list into tools.
func BuildAPIConnectionTools(conns []connections.Connection, client *http.Client) []Tool {
	return buildAPIConnectionTools(conns, client)
}

// buildAPIConnectionTools turns a set of (already enabled) connections into one
// tool per allowed endpoint+method on every API-type connection. MCP connections
// and connections without a usable URL are skipped. It is pure (no DB / network)
// so it is unit-testable; the caller supplies the connection list and the
// dispatch http.Client.
func buildAPIConnectionTools(conns []connections.Connection, client *http.Client) []Tool {
	var tools []Tool
	seen := map[string]struct{}{}
	for _, c := range conns {
		if c.Type != connections.TypeAPI || !c.Enabled {
			continue
		}
		baseURL := strings.TrimRight(strings.TrimSpace(c.URL), "/")
		if baseURL == "" {
			continue
		}
		for _, ep := range c.EndpointPermissions {
			path := normalizeEndpointPath(ep.Path)
			if path == "" {
				continue
			}
			for _, m := range ep.Methods {
				method := strings.ToUpper(strings.TrimSpace(m))
				if method == "" {
					continue
				}
				name := apiToolName(c.Name, method, path)
				if name == "" {
					continue
				}
				// Guard against a name collision (two endpoints sanitising to the
				// same tool name); first one wins, the rest are dropped rather than
				// silently shadowing dispatch targets.
				if _, dup := seen[name]; dup {
					continue
				}
				seen[name] = struct{}{}
				tools = append(tools, &APIConnectionTool{
					toolName:    name,
					description: apiToolDescription(c, method, path),
					schema:      apiToolSchema(method, path),
					connName:    c.Name,
					baseURL:     baseURL,
					method:      method,
					path:        path,
					auth:        c.AuthConfig,
					client:      client,
				})
			}
		}
	}
	// Stable order so the tool list is deterministic across runs.
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name() < tools[j].Name() })
	return tools
}

// normalizeEndpointPath ensures a leading slash and trims surrounding space.
func normalizeEndpointPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return p
}

// apiToolName builds a deterministic, model-safe tool identifier for a
// connection endpoint: "<connection>__<method>_<path>", lower-cased with every
// run of non-alphanumeric characters collapsed to a single underscore. The
// connection name is kept as a distinct prefix (double underscore) so two
// connections that expose the same path never collide.
func apiToolName(connName, method, path string) string {
	conn := sanitizeToolSegment(connName)
	tail := sanitizeToolSegment(method + " " + path)
	if conn == "" || tail == "" {
		return ""
	}
	return conn + "__" + tail
}

var nonToolChar = regexp.MustCompile(`[^a-z0-9]+`)

func sanitizeToolSegment(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = nonToolChar.ReplaceAllString(s, "_")
	return strings.Trim(s, "_")
}

// apiToolDescription is the human/model-facing one-liner for an endpoint tool.
func apiToolDescription(c connections.Connection, method, path string) string {
	label := c.DisplayName
	if label == "" {
		label = c.Name
	}
	return fmt.Sprintf("Call the %s API: %s %s. Dispatched server-side with the connection's credentials.", label, method, path)
}

// apiToolSchema derives a JSON-Schema input shape for an endpoint from its method
// and path. The endpoint definition carries no parameter schema (endpoint_permissions
// is just path+methods), so the schema is structural: one required string property
// per `{param}` path placeholder, a free-form `query` object for query-string
// parameters, and — for body methods — a free-form `body` object for the JSON
// request payload.
func apiToolSchema(method, path string) map[string]any {
	props := map[string]any{}
	var required []string
	for _, name := range pathParamNames(path) {
		props[name] = map[string]any{
			"type":        "string",
			"description": fmt.Sprintf("Value for the {%s} path parameter.", name),
		}
		required = append(required, name)
	}
	props["query"] = map[string]any{
		"type":                 "object",
		"description":          "Optional query-string parameters as a flat key/value object.",
		"additionalProperties": true,
	}
	if methodHasBody(method) {
		props["body"] = map[string]any{
			"type":                 "object",
			"description":          "JSON request body.",
			"additionalProperties": true,
		}
	}
	schema := map[string]any{
		"type":       "object",
		"properties": props,
	}
	if len(required) > 0 {
		sort.Strings(required)
		schema["required"] = required
	}
	return schema
}

func pathParamNames(path string) []string {
	matches := pathParamRe.FindAllStringSubmatch(path, -1)
	var names []string
	for _, m := range matches {
		names = append(names, m[1])
	}
	return names
}

func methodHasBody(method string) bool {
	switch strings.ToUpper(method) {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

// Call dispatches the endpoint server-side and returns the response body. Path
// `{param}` placeholders are filled from args, a `query` object is appended to
// the query string, and for body methods a `body` object is sent as the JSON
// request payload. Auth (bearer / api-key header / Cloudflare Access service
// token) is applied from the connection's auth_config — it never reaches the
// agent.
func (t *APIConnectionTool) Call(ctx context.Context, args map[string]any) (string, error) {
	if t.baseURL == "" {
		return "", fmt.Errorf("api connection %q has no URL configured", t.connName)
	}
	filledPath, err := fillPathParams(t.path, args)
	if err != nil {
		return "", err
	}

	reqURL := t.baseURL + filledPath
	if q := buildQueryValues(args); len(q) > 0 {
		sep := "?"
		if strings.Contains(reqURL, "?") {
			sep = "&"
		}
		reqURL += sep + q.Encode()
	}

	var bodyReader io.Reader
	hasBody := false
	if methodHasBody(t.method) {
		if raw, ok := args["body"]; ok && raw != nil {
			payload, err := json.Marshal(raw)
			if err != nil {
				return "", fmt.Errorf("api connection %q: marshal body: %w", t.connName, err)
			}
			bodyReader = bytes.NewReader(payload)
			hasBody = true
		}
	}

	req, err := http.NewRequestWithContext(ctx, t.method, reqURL, bodyReader)
	if err != nil {
		return "", fmt.Errorf("api connection %q: build request: %w", t.connName, err)
	}
	if hasBody {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	applyConnectionAuth(req, t.auth)

	client := t.client
	if client == nil {
		client = &http.Client{Timeout: apiConnectionToolTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("api connection %q: call %s %s: %w", t.connName, t.method, filledPath, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, apiConnectionResponseLimit))
	if err != nil {
		return "", fmt.Errorf("api connection %q: read response: %w", t.connName, err)
	}
	// Strip the connection's own secrets from the body before it reaches the
	// agent. Server-side dispatch keeps the credential out of the request the
	// agent constructs, but a misbehaving endpoint can echo its auth back in the
	// response; without this an echoed token would leak straight to the model.
	// Applied before BOTH the error and success returns below. (FIR-2166 C review.)
	text := redactCredentials(strings.TrimSpace(string(body)), t.auth)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("api connection %q: %s %s returned HTTP %d: %s", t.connName, t.method, filledPath, resp.StatusCode, text)
	}
	if text == "" {
		return fmt.Sprintf("HTTP %d (empty body)", resp.StatusCode), nil
	}
	return text, nil
}

// fillPathParams substitutes every `{param}` in path from args; a missing or
// non-string param value is an error (fail closed rather than call a wrong URL).
// Values are path-escaped so a value can't break out of its path segment.
func fillPathParams(path string, args map[string]any) (string, error) {
	var missing []string
	out := pathParamRe.ReplaceAllStringFunc(path, func(token string) string {
		name := strings.Trim(token, "{}")
		v, ok := args[name]
		if !ok || v == nil {
			missing = append(missing, name)
			return token
		}
		s, ok := v.(string)
		if !ok {
			s = fmt.Sprintf("%v", v)
		}
		if strings.TrimSpace(s) == "" {
			missing = append(missing, name)
			return token
		}
		return url.PathEscape(s)
	})
	if len(missing) > 0 {
		return "", fmt.Errorf("missing required path parameter(s): %s", strings.Join(missing, ", "))
	}
	return out, nil
}

// buildQueryValues flattens the optional `query` object argument into URL values.
func buildQueryValues(args map[string]any) url.Values {
	raw, ok := args["query"]
	if !ok || raw == nil {
		return nil
	}
	m, ok := raw.(map[string]any)
	if !ok || len(m) == 0 {
		return nil
	}
	values := url.Values{}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := m[k]
		if v == nil {
			continue
		}
		values.Set(k, fmt.Sprintf("%v", v))
	}
	return values
}

// applyConnectionAuth mirrors the header logic the MCP relay uses (store.go):
// bearer token, a custom API-key header (default X-API-Key), and a Cloudflare
// Access service-token pair. All are optional.
func applyConnectionAuth(req *http.Request, auth connections.AuthConfig) {
	if auth.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+auth.BearerToken)
	}
	if auth.APIKey != "" {
		header := auth.APIKeyHeader
		if strings.TrimSpace(header) == "" {
			header = "X-API-Key"
		}
		req.Header.Set(header, auth.APIKey)
	}
	if auth.CFAccessID != "" && auth.CFAccessSecret != "" {
		req.Header.Set("CF-Access-Client-Id", auth.CFAccessID)
		req.Header.Set("CF-Access-Client-Secret", auth.CFAccessSecret)
	}
}

// redactCredentials replaces any occurrence of the connection's own secret
// values in an endpoint response with a fixed placeholder, so an endpoint that
// echoes its auth back cannot leak the credential to the agent. Every non-empty
// secret half from the auth config is covered (bearer token, API key, Cloudflare
// Access id + secret). Values shorter than 4 chars are skipped to avoid
// over-redacting trivial/empty config; real credentials are far longer.
func redactCredentials(text string, auth connections.AuthConfig) string {
	if text == "" {
		return text
	}
	for _, secret := range []string{auth.BearerToken, auth.APIKey, auth.CFAccessSecret, auth.CFAccessID} {
		if len(secret) < 4 {
			continue
		}
		text = strings.ReplaceAll(text, secret, "[REDACTED]")
	}
	return text
}
