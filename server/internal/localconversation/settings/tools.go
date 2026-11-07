// Package settings gives local executions Multica's Settings operations:
// the registry the GUI and CLI share, exported from
// packages/core/settings/operations.ts. Calls execute without a separate
// user approval; token creation stays on Desktop to keep secrets out of MCP.
package settings

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/multica-ai/multica/server/internal/localconversation/contextmcp"
)

//go:embed operations.json
var operationsJSON []byte

// Operation is one registry entry.
type Operation struct {
	Name        string          `json:"name"`
	Domain      string          `json:"domain"`
	Description string          `json:"description"`
	Permission  string          `json:"permission"`
	Write       bool            `json:"write"`
	Confirm     bool            `json:"confirm"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// Operations is the registry.
func Operations() []Operation {
	var ops []Operation
	if err := json.Unmarshal(operationsJSON, &ops); err != nil {
		panic(err)
	}
	return ops
}

// Instructions describe the Settings tools to a provider.
const Instructions = "Multica Settings tools are available through the built-in MCP. Begin with settings_capabilities and read the relevant settings before changes. Use exact IDs and current status keys. Use settings_preview_workflow and dry_run=true for project workflow changes before applying; preserve the server's status_mapping and affected-task plan. Never invent success: distinguish applied, opened, cancelled and unknown results. Desktopd MCP calls execute directly without a separate user approval. Do not ask for confirmation before calling these tools. OAuth is completed in the settings UI. Never ask the user to paste credentials into chat; when the user supplies one for an MCP server config, pass it only in that config. Use the same request_id for retries. Use settings_* tools for Settings changes."

// Requester performs authenticated workspace API calls.
type Requester interface {
	Request(ctx context.Context, method, path string, body any) (any, error)
}

// Host asks Desktop to act: "settings.confirm" applies a write and displays
// a created token without a prior approval; "settings.host" performs a device operation;
// "settings.refresh" reloads the Settings UI.
type Host func(ctx context.Context, kind string, payload any) (json.RawMessage, error)

// Receipts persist write outcomes across executions.
type Receipts interface {
	// Claim records a pending write and returns the prior record's
	// fingerprint and result when the key was claimed before.
	Claim(ctx context.Context, key, fingerprint string) (prior *Receipt, err error)
	Finish(ctx context.Context, key, result string) error
}

// Receipt is a recorded write.
type Receipt struct {
	Fingerprint string
	Result      string
}

// Options configures one execution's Settings tools.
type Options struct {
	Scope    Scope
	Request  Requester
	Host     Host
	Receipts Receipts
}

// OperationError is a Settings failure with a stable code.
type OperationError struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Status  int            `json:"http_status,omitempty"`
	Details map[string]any `json:"details,omitempty"`
}

func (e *OperationError) Error() string { return e.Message }

func opErr(code, message string) error { return &OperationError{Code: code, Message: message} }

type tools struct {
	opts Options
}

// Tools returns settings_capabilities and the operations this execution
// can perform: API operations need cloud access, device operations and
// token creation need Desktop.
func Tools(opts Options) []contextmcp.Tool {
	t := &tools{opts: opts}
	var available []Operation
	for _, op := range Operations() {
		_, api := routes[op.Name]
		if (api && opts.Request == nil) || (!api && opts.Host == nil) || (op.Name == "settings_create_token" && opts.Host == nil) {
			continue
		}
		available = append(available, op)
	}
	out := []contextmcp.Tool{{
		Name: "settings_capabilities", ReadOnly: true,
		Description: "Discover Settings operations for this execution, account, workspace and device. Permission hints are not authorization; the server evaluates resource ownership and feature gates on every call.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		Call: func(ctx context.Context, _ json.RawMessage) (contextmcp.Result, error) {
			v, err := t.capabilities(ctx, available)
			return contextmcp.Result{Value: v}, err
		},
	}}
	for _, op := range available {
		out = append(out, t.tool(op))
	}
	return out
}

var domains = []string{"workspace", "members", "workflows", "statuses", "wakeups", "labels", "properties", "quick-actions", "code", "channels", "mcp", "profile", "preferences", "notifications", "shortcuts", "tokens", "daemon", "updates"}

func (t *tools) capabilities(ctx context.Context, available []Operation) (any, error) {
	role := "unknown"
	if t.opts.Request != nil {
		v, err := t.opts.Request.Request(ctx, http.MethodGet, ws(t.opts.Scope)+"/members", nil)
		if err != nil {
			return nil, err
		}
		if err := t.checkWorkspace(v); err != nil {
			return nil, err
		}
		members, _ := v.([]any)
		for _, m := range members {
			if m, ok := m.(map[string]any); ok && m["user_id"] == t.opts.Scope.UserID {
				if r, ok := m["role"].(string); ok {
					role = r
				}
			}
		}
	}
	ops := make([]map[string]any, len(available))
	for i, op := range available {
		auth := "server_or_host_evaluated"
		if op.Permission == "admin" && role != "unknown" {
			auth = "denied"
			if role == "owner" || role == "admin" {
				auth = "eligible"
			}
		}
		ops[i] = map[string]any{"name": op.Name, "domain": op.Domain, "write": op.Write, "human_confirmation": false, "permission": op.Permission, "authorization": auth}
	}
	return map[string]any{
		"workspace_id": t.opts.Scope.WorkspaceID, "user_id": t.opts.Scope.UserID, "role": role,
		"cloud_available": t.opts.Request != nil, "confirmation_available": false,
		"feature_gates": map[string]any{"project_workflows_v1": "server_evaluated"}, "domains": domains, "operations": ops,
	}, nil
}

var requestIDPattern = `^[A-Za-z0-9][A-Za-z0-9_-]*$`

func (t *tools) tool(op Operation) contextmcp.Tool {
	var doc map[string]any
	_ = json.Unmarshal(op.InputSchema, &doc)
	delete(doc, "$schema")
	if op.Write {
		props, _ := doc["properties"].(map[string]any)
		if props == nil {
			props = map[string]any{}
			doc["properties"] = props
		}
		props["request_id"] = map[string]any{"type": "string", "minLength": 1, "maxLength": 128, "pattern": requestIDPattern}
	}
	portablePatterns(doc)
	raw, _ := json.Marshal(doc)
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	parsed, _ := jsonschema.UnmarshalJSON(strings.NewReader(string(raw)))
	if err := compiler.AddResource(op.Name+".json", parsed); err != nil {
		panic(err)
	}
	schema := compiler.MustCompile(op.Name + ".json")
	defaults := map[string]any{}
	if props, ok := doc["properties"].(map[string]any); ok {
		for key, p := range props {
			if p, ok := p.(map[string]any); ok {
				if d, ok := p["default"]; ok {
					defaults[key] = d
				}
			}
		}
	}
	return contextmcp.Tool{
		Name: op.Name, Description: op.Description, InputSchema: raw, ReadOnly: !op.Write,
		Call: func(ctx context.Context, args json.RawMessage) (contextmcp.Result, error) {
			if len(args) == 0 || string(args) == "null" {
				args = json.RawMessage(`{}`)
			}
			inst, err := jsonschema.UnmarshalJSON(strings.NewReader(string(args)))
			if err != nil {
				return contextmcp.Result{}, opErr("invalid_input", "invalid tool arguments")
			}
			if err := schema.Validate(inst); err != nil {
				return contextmcp.Result{}, opErr("invalid_input", fmt.Sprintf("invalid tool arguments: %v", err))
			}
			var input map[string]any
			_ = json.Unmarshal(args, &input)
			for k, v := range defaults {
				if _, ok := input[k]; !ok {
					input[k] = v
				}
			}
			if err := checkTimezone(op.Name, input); err != nil {
				return contextmcp.Result{}, err
			}
			requestID, _ := input["request_id"].(string)
			delete(input, "request_id")
			preview := input["dry_run"] == true || (op.Name == "settings_device_action" && input["action"] == "check_update")
			var v any
			if !op.Write || preview {
				v, err = t.execute(ctx, op, input)
			} else {
				if requestID == "" {
					return contextmcp.Result{}, opErr("invalid_input", "request_id is required")
				}
				v, err = t.writeOnce(ctx, op, requestID, input)
			}
			if err != nil {
				return contextmcp.Result{}, err
			}
			return contextmcp.Result{Value: v}, nil
		},
	}
}

// portablePatterns drops patterns Go cannot compile, such as zod's email
// lookaheads; those nodes keep their asserted format.
func portablePatterns(node any) {
	switch n := node.(type) {
	case map[string]any:
		if p, ok := n["pattern"].(string); ok {
			if _, err := regexp.Compile(p); err != nil {
				if _, hasFormat := n["format"]; !hasFormat {
					panic(fmt.Sprintf("settings: pattern %q needs a format", p))
				}
				delete(n, "pattern")
			}
		}
		for _, v := range n {
			portablePatterns(v)
		}
	case []any:
		for _, v := range n {
			portablePatterns(v)
		}
	}
}

func checkTimezone(name string, input map[string]any) error {
	if name != "settings_update_profile" {
		return nil
	}
	fields, _ := input["fields"].(map[string]any)
	if tz, ok := fields["timezone"].(string); ok && tz != "" {
		if _, err := time.LoadLocation(tz); err != nil {
			return opErr("invalid_input", "timezone must be an IANA time zone")
		}
	}
	return nil
}

// stored is a persisted write outcome.
type stored struct {
	Status string          `json:"status"`
	Result any             `json:"result,omitempty"`
	Error  *OperationError `json:"error,omitempty"`
}

// writeOnce runs a write at most once per request_id: a completed write
// returns its result, a server rejection its error, and an attempt whose
// outcome is unknown is never replayed.
func (t *tools) writeOnce(ctx context.Context, op Operation, requestID string, input map[string]any) (any, error) {
	canonical, _ := json.Marshal(map[string]any{"operation": op.Name, "input": input})
	sum := sha256.Sum256(canonical)
	fingerprint := hex.EncodeToString(sum[:])
	key := "settings:" + op.Name + ":" + requestID
	if t.opts.Receipts == nil {
		return t.execute(ctx, op, input)
	}
	prior, err := t.opts.Receipts.Claim(ctx, key, fingerprint)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		if prior.Fingerprint != fingerprint {
			return nil, opErr("request_id_conflict", "This request_id belongs to different input")
		}
		var saved stored
		_ = json.Unmarshal([]byte(prior.Result), &saved)
		switch saved.Status {
		case "completed":
			return saved.Result, nil
		case "rejected":
			return nil, saved.Error
		}
		return nil, opErr("operation_outcome_unknown", "This operation was already attempted. Read current settings before choosing a new request_id; the previous write will not be replayed.")
	}
	v, err := t.execute(ctx, op, input)
	var rejected *OperationError
	switch {
	case err == nil:
		data, _ := json.Marshal(stored{Status: "completed", Result: v})
		_ = t.opts.Receipts.Finish(ctx, key, string(data))
	case errors.As(err, &rejected) && rejected.Status >= 400 && rejected.Status < 500:
		data, _ := json.Marshal(stored{Status: "rejected", Error: rejected})
		_ = t.opts.Receipts.Finish(ctx, key, string(data))
	}
	return v, err
}

var createOps = map[string]bool{"settings_create_workflow": true, "settings_create_status": true, "settings_create_label": true,
	"settings_create_property": true, "settings_create_quick_action": true, "settings_create_mcp_server": true}

func (t *tools) execute(ctx context.Context, op Operation, input map[string]any) (any, error) {
	scope := t.opts.Scope
	hostRequest := map[string]any{"operation": op.Name, "scope": scope, "input": input}
	r, api := routes[op.Name]
	var before any
	if api && r.before != nil {
		call := r.before(scope, input)
		v, err := t.call(ctx, *call)
		if err != nil {
			return nil, err
		}
		before = v
		if op.Domain != "profile" {
			if err := t.checkWorkspace(before); err != nil {
				return nil, err
			}
		}
		obj, _ := before.(map[string]any)
		if (op.Name == "settings_update_workspace" || op.Name == "settings_update_code_settings") && obj["id"] != scope.WorkspaceID {
			return nil, opErr("workspace_mismatch", "The setting belongs to another workspace")
		}
		if op.Domain == "profile" && obj["id"] != scope.UserID {
			return nil, opErr("account_mismatch", "The profile belongs to another account")
		}
		if id, ok := input["id"]; ok && !includesID(before, id) {
			return nil, opErr("resource_not_found", "The setting is outside this workspace or is no longer available")
		}
	}
	if op.Name == "settings_update_workflow" {
		call := *r.request(scope, input)
		body := merged(map[string]any{}, input["fields"])
		body["dry_run"] = true
		call.Body = body
		if _, err := t.call(ctx, call); err != nil {
			return nil, err
		}
	}
	if op.Name == "settings_set_project_workflow" && input["dry_run"] == false {
		call := *r.request(scope, input)
		call.Body = map[string]any{"workflow_id": input["workflow_id"], "status_mapping": input["status_mapping"], "dry_run": true}
		if _, err := t.call(ctx, call); err != nil {
			return nil, err
		}
	}
	preview := input["dry_run"] == true || (op.Name == "settings_device_action" && input["action"] == "check_update")
	var call *Call
	if api {
		call = r.request(scope, input)
		if op.Name == "settings_update_code_settings" {
			settings := merged(map[string]any{}, asObject(before)["settings"])
			for k, v := range asObject(input["fields"]) {
				settings[k] = v
			}
			call.Body = map[string]any{"settings": settings}
		}
	}

	var raw any
	hostApplied := false
	// Token creation must stay on Desktop so the raw secret never enters MCP.
	// Keep the existing host message for installed-client compatibility.
	if op.Name == "settings_create_token" {
		if t.opts.Host == nil {
			return nil, opErr("secret_display_unavailable", "Open Desktop to display the created token")
		}
		payload := map[string]any{"operation": op.Name, "scope": scope, "input": input, "apply": call}
		reply, err := t.opts.Host(ctx, "settings.confirm", payload)
		if err != nil {
			return nil, err
		}
		var outcome struct {
			Approved bool `json:"approved"`
			Raw      any  `json:"raw"`
		}
		if json.Unmarshal(reply, &outcome) != nil {
			return nil, opErr("invalid_response", "Desktop returned an invalid token creation result")
		}
		if !outcome.Approved {
			return map[string]any{"status": "cancelled", "operation": op.Name, "workspace_id": scope.WorkspaceID}, nil
		}
		hostApplied, raw = true, outcome.Raw
	}
	if call == nil {
		reply, err := t.opts.Host(ctx, "settings.host", hostRequest)
		if err != nil {
			return nil, err
		}
		var v any
		_ = json.Unmarshal(reply, &v)
		return v, nil
	}
	if !hostApplied {
		v, err := t.call(ctx, *call)
		if err != nil {
			return nil, err
		}
		raw = v
	}
	if op.Domain != "profile" && op.Domain != "tokens" {
		if err := t.checkWorkspace(raw); err != nil {
			return nil, err
		}
	}
	obj := asObject(raw)
	if op.Name == "settings_read_workspace" && obj["id"] != scope.WorkspaceID {
		return nil, opErr("workspace_mismatch", "The setting belongs to another workspace")
	}
	if op.Domain == "profile" && obj["id"] != scope.UserID {
		return nil, opErr("account_mismatch", "The profile belongs to another account")
	}
	if createOps[op.Name] {
		id, _ := obj["id"].(string)
		if id == "" || (op.Domain != "mcp" && obj["workspace_id"] != scope.WorkspaceID) {
			return nil, opErr("result_unconfirmed", "The server did not confirm a created resource in this workspace. Read current settings before another creation.")
		}
	}
	// Desktop displayed the created token and removed the secret from its reply.
	if op.Name == "settings_create_token" {
		return map[string]any{"status": "applied", "operation": op.Name, "id": obj["id"], "secret_displayed_to_user": true}, nil
	}
	if op.Name == "settings_list_tokens" {
		values, ok := raw.([]any)
		if !ok {
			values, ok = obj["tokens"].([]any)
		}
		if !ok {
			return nil, opErr("invalid_response", "The token metadata response is invalid")
		}
		out := make([]any, len(values))
		for i, v := range values {
			row := asObject(v)
			out[i] = map[string]any{"id": row["id"], "name": row["name"], "prefix": row["token_prefix"], "expires_at": row["expires_at"], "last_used_at": row["last_used_at"], "created_at": row["created_at"]}
		}
		return out, nil
	}
	value := public(raw)
	if op.Domain == "mcp" {
		summary := func(v any) any {
			row := asObject(v)
			return map[string]any{"id": row["id"], "name": row["name"], "transport": row["transport"]}
		}
		if list, ok := raw.([]any); ok {
			out := make([]any, len(list))
			for i, v := range list {
				out[i] = summary(v)
			}
			value = out
		} else {
			value = summary(raw)
		}
	}
	if !op.Write || preview {
		return value, nil
	}
	result := map[string]any{"status": "applied", "operation": op.Name, "workspace_id": scope.WorkspaceID, "data": value, "settings_tab": op.Domain}
	if t.opts.Host != nil {
		result["ui_refresh"] = "refreshed"
		if _, err := t.opts.Host(ctx, "settings.refresh", map[string]any{"scope": scope}); err != nil {
			result["ui_refresh"] = "pending"
		}
	}
	return result, nil
}

func (t *tools) call(ctx context.Context, c Call) (any, error) {
	v, err := t.opts.Request.Request(ctx, c.Method, c.Path, c.Body)
	var op *contextmcp.OperationError
	if errors.As(err, &op) {
		return nil, &OperationError{Code: op.Code, Message: op.Message, Status: op.Status, Details: op.Details}
	}
	return v, err
}

func (t *tools) checkWorkspace(v any) error {
	switch v := v.(type) {
	case []any:
		for _, e := range v {
			if err := t.checkWorkspace(e); err != nil {
				return err
			}
		}
	case map[string]any:
		if ws, ok := v["workspace_id"].(string); ok && ws != "" && ws != t.opts.Scope.WorkspaceID {
			return opErr("workspace_mismatch", "The response belongs to another workspace")
		}
		for _, e := range v {
			if err := t.checkWorkspace(e); err != nil {
				return err
			}
		}
	}
	return nil
}

func includesID(v any, id any) bool {
	switch v := v.(type) {
	case []any:
		for _, e := range v {
			if includesID(e, id) {
				return true
			}
		}
	case map[string]any:
		if v["id"] == id {
			return true
		}
		for _, e := range v {
			if list, ok := e.([]any); ok && includesID(list, id) {
				return true
			}
		}
	}
	return false
}

var publicHidden = regexp.MustCompile(`(?i)(?:token|secret|password|credential|authorization|private_key|headers|^env$|mcp_config|runtime_config|custom_args|webhook_url|webhook_path)`)

// public drops credentials and private configuration from a value.
func public(v any) any {
	switch v := v.(type) {
	case []any:
		out := make([]any, len(v))
		for i, e := range v {
			out[i] = public(e)
		}
		return out
	case map[string]any:
		out := map[string]any{}
		for k, e := range v {
			if !publicHidden.MatchString(k) {
				out[k] = public(e)
			}
		}
		return out
	}
	return v
}

func asObject(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}
