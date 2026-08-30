package daemon

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/multica-ai/multica/server/pkg/remotemcp"
)

const (
	remoteMCPMaxRequestBytes = 1 << 20
	remoteMCPMaxCalls        = 256
	remoteMCPMaxConcurrency  = 8
)

type remoteMCPBrokerSet struct {
	servers   []*http.Server
	listeners []net.Listener
	once      sync.Once
}

type remoteMCPCredentialResolver func(context.Context, string) (http.Header, error)

func (set *remoteMCPBrokerSet) Close() {
	if set == nil {
		return
	}
	set.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		for _, server := range set.servers {
			_ = server.Shutdown(ctx)
		}
		for _, listener := range set.listeners {
			_ = listener.Close()
		}
	})
}

func startTaskRemoteMCPBrokers(setupCtx, lifetimeCtx context.Context, taskID, provider string, connections []remotemcp.Connection, resolveCredential remoteMCPCredentialResolver, logger *slog.Logger) (json.RawMessage, []string, *remoteMCPBrokerSet, error) {
	return startTaskRemoteMCPBrokersWithGate(setupCtx, lifetimeCtx, taskID, provider, connections, resolveCredential, nil, logger)
}

func startTaskRemoteMCPBrokersWithGate(setupCtx, lifetimeCtx context.Context, taskID, provider string, connections []remotemcp.Connection, resolveCredential remoteMCPCredentialResolver, invocationGate remoteMCPInvocationGate, logger *slog.Logger) (json.RawMessage, []string, *remoteMCPBrokerSet, error) {
	if len(connections) == 0 {
		return nil, nil, nil, nil
	}
	set := &remoteMCPBrokerSet{}
	servers := map[string]any{}
	diagnostics := make([]string, 0)
	for _, connection := range connections {
		if !providerSupportsRemoteMCPBroker(provider) {
			message := fmt.Sprintf("Remote MCP %s is incompatible with provider %s", connection.ContributionKey, provider)
			if connection.FailurePolicy == "optional" {
				diagnostics = append(diagnostics, message)
				continue
			}
			set.Close()
			return nil, diagnostics, nil, errors.New(message)
		}
		headers := make(http.Header)
		if connection.CredentialHeader != "" && resolveCredential != nil {
			resolvedHeaders, resolveErr := resolveCredential(setupCtx, connection.ContributionID)
			if resolveErr != nil {
				message := fmt.Sprintf("Remote MCP %s credential is unavailable", connection.ContributionKey)
				if connection.FailurePolicy == "optional" {
					diagnostics = append(diagnostics, message)
					continue
				}
				set.Close()
				return nil, diagnostics, nil, fmt.Errorf("%s: %w", message, resolveErr)
			}
			headers = resolvedHeaders
		} else if connection.CredentialHeader != "" {
			message := fmt.Sprintf("Remote MCP %s credential resolver is unavailable", connection.ContributionKey)
			if connection.FailurePolicy == "optional" {
				diagnostics = append(diagnostics, message)
				continue
			}
			set.Close()
			return nil, diagnostics, nil, errors.New(message)
		}
		discovered, _, err := remotemcp.Discover(setupCtx, connection.Endpoint, connection.EndpointAllowedHosts, connection.ProtocolVersions, headers)
		if err == nil {
			err = validatePinnedRemoteMCPTools(connection.ApprovedTools, discovered)
		}
		if err != nil {
			message := fmt.Sprintf("Remote MCP %s failed startup validation", connection.ContributionKey)
			if connection.FailurePolicy == "optional" {
				diagnostics = append(diagnostics, message)
				continue
			}
			set.Close()
			return nil, diagnostics, nil, fmt.Errorf("%s: %w", message, err)
		}
		endpoint, err := remotemcp.ValidatePublicHTTPSEndpoint(setupCtx, connection.Endpoint, connection.EndpointAllowedHosts, nil)
		if err != nil {
			set.Close()
			return nil, diagnostics, nil, err
		}
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			set.Close()
			return nil, diagnostics, nil, fmt.Errorf("listen for Remote MCP broker: %w", err)
		}
		pathToken, err := randomBrokerToken()
		if err != nil {
			_ = listener.Close()
			set.Close()
			return nil, diagnostics, nil, err
		}
		proxy := &remoteMCPProxy{
			taskID: taskID, provider: provider, connection: connection, endpoint: endpoint,
			client: remotemcp.NewSecureHTTPClient(endpoint), credentialHeaders: headers,
			resolveCredential: resolveCredential, path: "/" + pathToken,
			semaphore: make(chan struct{}, remoteMCPMaxConcurrency), logger: logger,
			invocationGate: invocationGate,
		}
		server := &http.Server{Handler: proxy, ReadHeaderTimeout: 5 * time.Second}
		set.servers = append(set.servers, server)
		set.listeners = append(set.listeners, listener)
		go func() {
			if serveErr := server.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) && logger != nil {
				logger.Warn("Remote MCP broker stopped unexpectedly", "task_id", taskID, "contribution", connection.ContributionKey, "error", serveErr)
			}
		}()
		name := remoteMCPServerName(connection)
		servers[name] = map[string]any{
			"type": "http",
			"url":  "http://" + listener.Addr().String() + proxy.path,
		}
	}
	if len(servers) == 0 {
		set.Close()
		return nil, diagnostics, nil, nil
	}
	go func() {
		<-lifetimeCtx.Done()
		set.Close()
	}()
	raw, err := json.Marshal(map[string]any{"mcpServers": servers})
	if err != nil {
		set.Close()
		return nil, diagnostics, nil, err
	}
	return raw, diagnostics, set, nil
}

func providerSupportsRemoteMCPBroker(provider string) bool {
	switch provider {
	case "codex", "claude", "hermes", "qoder", "mcode", "opencode-api", "opencode-zen", "opencode-go", "openrouter", "vercel-ai-gateway", "ollama", "lmstudio", "nvidia-nim":
		return true
	default:
		return false
	}
}

func validatePinnedRemoteMCPTools(approved, discovered []remotemcp.Tool) error {
	available := make(map[string]remotemcp.Tool, len(discovered))
	for _, tool := range discovered {
		available[tool.Name] = tool
	}
	for _, pinned := range approved {
		current, ok := available[pinned.Name]
		if !ok {
			return fmt.Errorf("approved tool %q is missing", pinned.Name)
		}
		if current.SchemaDigest != pinned.SchemaDigest {
			return fmt.Errorf("approved tool %q schema drifted", pinned.Name)
		}
	}
	return nil
}

func randomBrokerToken() (string, error) {
	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func remoteMCPServerName(connection remotemcp.Connection) string {
	name := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, strings.ToLower(connection.ContributionKey))
	suffix := strings.ReplaceAll(connection.ContributionID, "-", "")
	if len(suffix) > 8 {
		suffix = suffix[:8]
	}
	return "plugin-" + name + "-" + suffix
}

type remoteMCPProxy struct {
	taskID            string
	provider          string
	connection        remotemcp.Connection
	endpoint          *url.URL
	client            *http.Client
	credentialHeaders http.Header
	resolveCredential remoteMCPCredentialResolver
	path              string
	semaphore         chan struct{}
	calls             atomic.Int64
	logger            *slog.Logger
	invocationGate    remoteMCPInvocationGate
}

var (
	errRemoteMCPCapabilityUnsupported = errors.New("Remote MCP pre-transport capability is unavailable")
	errRemoteMCPPolicyDenied          = errors.New("Remote MCP invocation is denied by policy")
	errRemoteMCPSchemaDrift           = errors.New("Remote MCP tool schema changed and requires review")
	errRemoteMCPApprovalPending       = errors.New("Remote MCP invocation approval is pending")
	errRemoteMCPApprovalDenied        = errors.New("Remote MCP invocation approval was denied")
	errRemoteMCPApprovalExpired       = errors.New("Remote MCP invocation approval expired")
	errRemoteMCPApprovalCancelled     = errors.New("Remote MCP invocation approval was cancelled")
	errRemoteMCPApprovalConsumed      = errors.New("Remote MCP invocation approval was already consumed")
)

type remoteMCPInvocation struct {
	TaskID         string
	ProviderFamily string
	TransportKind  string
	ServerKey      string
	ToolName       string
	SchemaDigest   string
	ArgumentBytes  int32
	IdempotencyKey string
}

type remoteMCPInvocationGrant struct {
	InvocationID      string
	ApprovalRequestID string
	PolicyRevision    int64
	Invocation        remoteMCPInvocation
}

type remoteMCPInvocationResult struct {
	OutcomeCode string
	ErrorClass  string
	ResultBytes int32
	DurationMS  int64
}

type remoteMCPInvocationGate interface {
	CheckCapability(providerFamily, transportKind string) error
	Begin(context.Context, remoteMCPInvocation) (remoteMCPInvocationGrant, error)
	Finish(context.Context, remoteMCPInvocationGrant, remoteMCPInvocationResult) error
}

type remoteMCPRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func (proxy *remoteMCPProxy) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	started := time.Now()
	toolName := ""
	resultClass := "rejected"
	defer func() {
		if proxy.logger != nil {
			proxy.logger.Info("Remote MCP broker call",
				"task_id", proxy.taskID,
				"installation_id", proxy.connection.InstallationID,
				"contribution", proxy.connection.ContributionKey,
				"tool", toolName,
				"duration_ms", time.Since(started).Milliseconds(),
				"result_class", resultClass,
			)
		}
	}()
	if request.URL.Path != proxy.path || request.Method != http.MethodPost {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if proxy.calls.Add(1) > remoteMCPMaxCalls {
		writeRemoteMCPError(w, nil, -32002, "Remote MCP task call limit exceeded")
		return
	}
	select {
	case proxy.semaphore <- struct{}{}:
		defer func() { <-proxy.semaphore }()
	default:
		writeRemoteMCPError(w, nil, -32003, "Remote MCP concurrency limit exceeded")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(request.Body, remoteMCPMaxRequestBytes+1))
	if err != nil || len(raw) > remoteMCPMaxRequestBytes {
		writeRemoteMCPError(w, nil, -32600, "Remote MCP request is invalid")
		return
	}
	var rpcRequest remoteMCPRequest
	if err := json.Unmarshal(raw, &rpcRequest); err != nil || rpcRequest.JSONRPC != "2.0" {
		writeRemoteMCPError(w, rpcRequest.ID, -32600, "Remote MCP request is invalid")
		return
	}
	if !allowedRemoteMCPMethod(rpcRequest.Method) {
		writeRemoteMCPError(w, rpcRequest.ID, -32601, "Only approved Remote MCP tools are available")
		return
	}
	var invocationGrant remoteMCPInvocationGrant
	invocationStarted := false
	finishInvocation := func(outcomeCode, errorClass string, resultBytes int32) error {
		if !invocationStarted {
			return nil
		}
		auditCtx, cancelAudit := context.WithTimeout(context.WithoutCancel(request.Context()), 5*time.Second)
		defer cancelAudit()
		return proxy.invocationGate.Finish(auditCtx, invocationGrant, remoteMCPInvocationResult{
			OutcomeCode: outcomeCode, ErrorClass: errorClass, ResultBytes: resultBytes,
			DurationMS: time.Since(started).Milliseconds(),
		})
	}
	if rpcRequest.Method == "tools/call" {
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(rpcRequest.Params, &params); err != nil {
			writeRemoteMCPError(w, rpcRequest.ID, -32602, "Remote MCP tool is not approved")
			return
		}
		toolName = params.Name
		if proxy.invocationGate != nil {
			if err := proxy.invocationGate.CheckCapability(proxy.provider, managedMCPTransportKind); err != nil {
				resultClass = remoteMCPGateResultClass(err)
				writeRemoteMCPError(w, rpcRequest.ID, -32007, remoteMCPGateMessage(err))
				return
			}
		}
		approvedTool, approved := proxy.approvedTool(params.Name)
		if !approved {
			writeRemoteMCPError(w, rpcRequest.ID, -32602, "Remote MCP tool is not approved")
			return
		}
		if proxy.invocationGate != nil {
			invocationGrant, err = proxy.invocationGate.Begin(request.Context(), remoteMCPInvocation{
				TaskID: proxy.taskID, ProviderFamily: proxy.provider,
				TransportKind: "managed_mcp", ServerKey: proxy.connection.ContributionKey,
				ToolName: approvedTool.Name, SchemaDigest: approvedTool.SchemaDigest,
				ArgumentBytes:  int32(len(params.Arguments)),
				IdempotencyKey: remoteMCPInvocationIdempotencyKey(proxy.taskID, proxy.connection.ContributionKey, approvedTool.Name, rpcRequest.ID),
			})
			if err != nil {
				resultClass = remoteMCPGateResultClass(err)
				writeRemoteMCPError(w, rpcRequest.ID, -32007, remoteMCPGateMessage(err))
				return
			}
			invocationStarted = true
		}
		if proxy.connection.Transport != "http" {
			if err := finishInvocation("failed", "unsupported", 0); err != nil {
				resultClass = "audit_failure"
				writeRemoteMCPError(w, rpcRequest.ID, -32008, "Remote MCP terminal audit commit failed")
				return
			}
			writeRemoteMCPError(w, rpcRequest.ID, -32006, "Remote MCP transport is not supported")
			return
		}
	}
	upstream, err := http.NewRequestWithContext(request.Context(), http.MethodPost, proxy.endpoint.String(), bytes.NewReader(raw))
	if err != nil {
		if finishErr := finishInvocation("failed", "internal", 0); finishErr != nil {
			resultClass = "audit_failure"
			writeRemoteMCPError(w, rpcRequest.ID, -32008, "Remote MCP terminal audit commit failed")
			return
		}
		writeRemoteMCPError(w, rpcRequest.ID, -32603, "Remote MCP request failed")
		return
	}
	upstream.Header.Set("Content-Type", "application/json")
	upstream.Header.Set("Accept", "application/json, text/event-stream")
	for _, header := range []string{"Mcp-Session-Id", "Mcp-Protocol-Version", "Last-Event-ID"} {
		if value := request.Header.Get(header); value != "" {
			upstream.Header.Set(header, value)
		}
	}
	credentialHeaders := proxy.credentialHeaders
	if proxy.connection.CredentialHeader != "" && proxy.resolveCredential != nil {
		credentialHeaders, err = proxy.resolveCredential(request.Context(), proxy.connection.ContributionID)
		if err != nil {
			resultClass = "credential_revoked"
			if finishErr := finishInvocation("failed", "provider", 0); finishErr != nil {
				resultClass = "audit_failure"
				writeRemoteMCPError(w, rpcRequest.ID, -32008, "Remote MCP terminal audit commit failed")
				return
			}
			writeRemoteMCPError(w, rpcRequest.ID, -32005, "Remote MCP credential is revoked or unavailable")
			return
		}
	}
	for key, values := range credentialHeaders {
		for _, value := range values {
			upstream.Header.Add(key, value)
		}
	}
	response, err := proxy.client.Do(upstream)
	if err != nil {
		resultClass = "remote_error"
		outcomeCode, errorClass := "failed", "transport"
		if request.Context().Err() != nil {
			outcomeCode, errorClass = "cancelled", "cancelled"
		}
		if finishErr := finishInvocation(outcomeCode, errorClass, 0); finishErr != nil {
			resultClass = "audit_failure"
			writeRemoteMCPError(w, rpcRequest.ID, -32008, "Remote MCP terminal audit commit failed")
			return
		}
		writeRemoteMCPError(w, rpcRequest.ID, -32000, "Remote MCP service is unavailable")
		return
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, remotemcp.MaxResponseBytes+1))
	if err != nil || len(responseBody) > remotemcp.MaxResponseBytes {
		resultClass = "remote_error"
		if finishErr := finishInvocation("failed", "transport", 0); finishErr != nil {
			resultClass = "audit_failure"
			writeRemoteMCPError(w, rpcRequest.ID, -32008, "Remote MCP terminal audit commit failed")
			return
		}
		writeRemoteMCPError(w, rpcRequest.ID, -32001, "Remote MCP response exceeded the allowed limit")
		return
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		resultClass = "remote_error"
		if finishErr := finishInvocation("failed", "transport", int32(len(responseBody))); finishErr != nil {
			resultClass = "audit_failure"
			writeRemoteMCPError(w, rpcRequest.ID, -32008, "Remote MCP terminal audit commit failed")
			return
		}
		writeRemoteMCPError(w, rpcRequest.ID, -32000, "Remote MCP service returned an error")
		return
	}
	if rpcRequest.Method == "tools/list" {
		responseBody, err = decodeRemoteMCPSSEData(response.Header.Get("Content-Type"), responseBody)
		if err != nil {
			resultClass = "remote_error"
			writeRemoteMCPError(w, rpcRequest.ID, -32000, "Remote MCP service returned an invalid response")
			return
		}
		responseBody, err = proxy.filterToolsListResponse(responseBody)
		if err != nil {
			resultClass = "schema_drift"
			writeRemoteMCPError(w, rpcRequest.ID, -32004, "Remote MCP tool schema changed and requires review")
			return
		}
		w.Header().Set("Content-Type", "application/json")
	} else if contentType := response.Header.Get("Content-Type"); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	for _, header := range []string{"Mcp-Session-Id", "Mcp-Protocol-Version"} {
		if value := response.Header.Get(header); value != "" {
			w.Header().Set(header, value)
		}
	}
	if invocationStarted {
		auditCtx, cancelAudit := context.WithTimeout(context.WithoutCancel(request.Context()), 5*time.Second)
		err := proxy.invocationGate.Finish(auditCtx, invocationGrant, remoteMCPInvocationResult{OutcomeCode: "succeeded", ResultBytes: int32(len(responseBody)), DurationMS: time.Since(started).Milliseconds()})
		cancelAudit()
		if err != nil {
			resultClass = "audit_failure"
			writeRemoteMCPError(w, rpcRequest.ID, -32008, "Remote MCP terminal audit commit failed")
			return
		}
		if invocationGrant.InvocationID != "" {
			w.Header().Set("X-Multica-Tool-Invocation-ID", invocationGrant.InvocationID)
		}
	}
	w.WriteHeader(response.StatusCode)
	_, _ = w.Write(responseBody)
	resultClass = "success"
}

func remoteMCPInvocationIdempotencyKey(taskID, serverKey, toolName string, rpcID json.RawMessage) string {
	hash := sha256.New()
	for _, value := range [][]byte{[]byte(taskID), []byte(serverKey), []byte(toolName), rpcID} {
		_, _ = hash.Write(value)
		_, _ = hash.Write([]byte{0})
	}
	return "mcp:sha256:" + hex.EncodeToString(hash.Sum(nil))
}

func decodeRemoteMCPSSEData(contentType string, raw []byte) ([]byte, error) {
	if !strings.HasPrefix(strings.ToLower(contentType), "text/event-stream") {
		return raw, nil
	}
	for _, line := range bytes.Split(raw, []byte("\n")) {
		if bytes.HasPrefix(line, []byte("data:")) {
			data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
			if len(data) > 0 {
				return data, nil
			}
		}
	}
	return nil, errors.New("Remote MCP SSE response contained no data")
}

func allowedRemoteMCPMethod(method string) bool {
	switch method {
	case "initialize", "notifications/initialized", "notifications/cancelled", "ping", "tools/list", "tools/call":
		return true
	default:
		return false
	}
}

func (proxy *remoteMCPProxy) approvedTool(name string) (remotemcp.Tool, bool) {
	for _, tool := range proxy.connection.ApprovedTools {
		if tool.Name == name {
			return tool, true
		}
	}
	return remotemcp.Tool{}, false
}

func remoteMCPGateMessage(err error) string {
	switch {
	case errors.Is(err, errRemoteMCPCapabilityUnsupported):
		return "Remote MCP pre-transport capability is unavailable"
	case errors.Is(err, errRemoteMCPSchemaDrift):
		return "Remote MCP tool schema changed and requires review"
	case errors.Is(err, errRemoteMCPApprovalPending):
		return "Remote MCP invocation approval is pending"
	case errors.Is(err, errRemoteMCPApprovalDenied):
		return "Remote MCP invocation approval was denied"
	case errors.Is(err, errRemoteMCPApprovalExpired):
		return "Remote MCP invocation approval expired"
	case errors.Is(err, errRemoteMCPApprovalCancelled):
		return "Remote MCP invocation approval was cancelled"
	case errors.Is(err, errRemoteMCPApprovalConsumed):
		return "Remote MCP invocation approval was already consumed"
	case errors.Is(err, errRemoteMCPAuditFailure):
		return "Remote MCP audit commit failed"
	default:
		return "Remote MCP invocation is denied by policy"
	}
}

func remoteMCPGateResultClass(err error) string {
	switch {
	case errors.Is(err, errRemoteMCPCapabilityUnsupported):
		return "unsupported"
	case errors.Is(err, errRemoteMCPSchemaDrift):
		return "schema_drift"
	case errors.Is(err, errRemoteMCPApprovalPending):
		return "approval_pending"
	case errors.Is(err, errRemoteMCPApprovalDenied):
		return "approval_denied"
	case errors.Is(err, errRemoteMCPApprovalExpired):
		return "approval_expired"
	case errors.Is(err, errRemoteMCPApprovalCancelled):
		return "approval_cancelled"
	case errors.Is(err, errRemoteMCPApprovalConsumed):
		return "approval_consumed"
	case errors.Is(err, errRemoteMCPAuditFailure):
		return "audit_failure"
	default:
		return "policy_denied"
	}
}

func (proxy *remoteMCPProxy) filterToolsListResponse(raw []byte) ([]byte, error) {
	var response map[string]json.RawMessage
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, err
	}
	var result map[string]json.RawMessage
	if err := json.Unmarshal(response["result"], &result); err != nil {
		return nil, err
	}
	var tools []struct {
		Name        string          `json:"name"`
		Description string          `json:"description,omitempty"`
		InputSchema json.RawMessage `json:"inputSchema"`
	}
	if err := json.Unmarshal(result["tools"], &tools); err != nil {
		return nil, err
	}
	byName := make(map[string]json.RawMessage, len(tools))
	for _, tool := range tools {
		canonical, err := canonicalRemoteMCPJSON(tool.InputSchema)
		if err != nil {
			return nil, err
		}
		byName[tool.Name] = canonical
	}
	pinned := append([]remotemcp.Tool(nil), proxy.connection.ApprovedTools...)
	sort.Slice(pinned, func(i, j int) bool { return pinned[i].Name < pinned[j].Name })
	filtered := make([]map[string]any, 0, len(pinned))
	for _, tool := range pinned {
		current, ok := byName[tool.Name]
		if !ok || remotemcp.DigestBytes(current) != tool.SchemaDigest {
			return nil, errors.New("tool schema drift")
		}
		filtered = append(filtered, map[string]any{
			"name": tool.Name, "description": tool.Description, "inputSchema": json.RawMessage(current),
		})
	}
	result["tools"], _ = json.Marshal(filtered)
	response["result"], _ = json.Marshal(result)
	return json.Marshal(response)
}

func canonicalRemoteMCPJSON(raw json.RawMessage) (json.RawMessage, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(value)
	return json.RawMessage(canonical), err
}

func writeRemoteMCPError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0", "id": id,
		"error": map[string]any{"code": code, "message": message},
	})
}

func mergeTaskRemoteMCPConfig(base, overlay json.RawMessage) (json.RawMessage, error) {
	if len(overlay) == 0 {
		return base, nil
	}
	var overlayDocument struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(overlay, &overlayDocument); err != nil {
		return nil, err
	}
	baseDocument := struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}{MCPServers: map[string]json.RawMessage{}}
	trimmed := bytes.TrimSpace(base)
	if len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null")) {
		if err := json.Unmarshal(trimmed, &baseDocument); err != nil {
			return nil, err
		}
		if baseDocument.MCPServers == nil {
			baseDocument.MCPServers = map[string]json.RawMessage{}
		}
	}
	for name, server := range overlayDocument.MCPServers {
		baseDocument.MCPServers[name] = server
	}
	return json.Marshal(baseDocument)
}
