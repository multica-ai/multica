// Package remotemcp implements the security boundary shared by Remote MCP
// Plugin discovery and the daemon's task-scoped broker.
package remotemcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	MaxResponseBytes = 4 << 20
	ConnectTimeout   = 10 * time.Second
	CallTimeout      = 30 * time.Second
)

type Resolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

func ValidatePublicHTTPSEndpoint(ctx context.Context, raw string, allowedHosts []string, resolver Resolver) (*url.URL, error) {
	endpoint, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("parse endpoint: %w", err)
	}
	// An operator-named dev origin skips the public-internet requirement and
	// nothing else: the net: scope below still applies, so a Plugin still
	// cannot reach a destination the administrator did not consent to.
	if isDevOrigin(endpoint) {
		devHost := strings.ToLower(strings.TrimSuffix(endpoint.Hostname(), "."))
		if len(allowedHosts) > 0 && !hostAllowed(devHost, allowedHosts) {
			return nil, errors.New("endpoint host is outside the Plugin endpoint policy")
		}
		return endpoint, nil
	}
	if endpoint.Scheme != "https" || endpoint.Hostname() == "" || endpoint.User != nil || endpoint.Fragment != "" || endpoint.RawQuery != "" {
		return nil, errors.New("endpoint must be a public HTTPS URL without userinfo, query, or fragment")
	}
	host := strings.ToLower(strings.TrimSuffix(endpoint.Hostname(), "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return nil, errors.New("endpoint host is not public")
	}
	if len(allowedHosts) > 0 && !hostAllowed(host, allowedHosts) {
		return nil, errors.New("endpoint host is outside the Plugin endpoint policy")
	}
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	addresses, err := resolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(addresses) == 0 {
		return nil, fmt.Errorf("resolve endpoint host: %w", err)
	}
	for _, address := range addresses {
		if !isPublicAddress(address.Unmap()) {
			return nil, fmt.Errorf("endpoint host resolves to non-public address %s", address)
		}
	}
	return endpoint, nil
}

func hostAllowed(host string, policies []string) bool {
	for _, policy := range policies {
		policy = strings.ToLower(strings.TrimSuffix(policy, "."))
		if host == policy {
			return true
		}
		if strings.HasPrefix(policy, "*.") {
			suffix := strings.TrimPrefix(policy, "*")
			if strings.HasSuffix(host, suffix) && host != strings.TrimPrefix(suffix, ".") {
				return true
			}
		}
	}
	return false
}

func isPublicAddress(address netip.Addr) bool {
	if !address.IsValid() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsMulticast() || address.IsUnspecified() {
		return false
	}
	// IANA special-purpose and metadata ranges not covered by netip's helpers.
	blocked := []netip.Prefix{
		netip.MustParsePrefix("100.64.0.0/10"),
		netip.MustParsePrefix("192.0.0.0/24"),
		netip.MustParsePrefix("192.0.2.0/24"),
		netip.MustParsePrefix("198.18.0.0/15"),
		netip.MustParsePrefix("198.51.100.0/24"),
		netip.MustParsePrefix("203.0.113.0/24"),
		netip.MustParsePrefix("224.0.0.0/4"),
		netip.MustParsePrefix("240.0.0.0/4"),
		netip.MustParsePrefix("2001:db8::/32"),
		netip.MustParsePrefix("fc00::/7"),
		netip.MustParsePrefix("fe80::/10"),
	}
	for _, prefix := range blocked {
		if prefix.Contains(address) {
			return false
		}
	}
	return address.IsGlobalUnicast()
}

func NewSecureHTTPClient(endpoint *url.URL) *http.Client {
	// Decided once, here, rather than inside the dialer: the endpoint is fixed
	// for this client's lifetime, so a per-dial lookup could only differ if the
	// environment changed mid-task, and honouring that would be a way to widen
	// the guard after the connection was approved.
	devOrigin := isDevOrigin(endpoint)
	dialer := &net.Dialer{Timeout: ConnectTimeout, KeepAlive: 30 * time.Second}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	if devOrigin {
		// Trust an extra CA for this origin, never skip verification: a plugin
		// author's local MCP server still presents a certificate.
		if config := devTLSConfig(); config != nil {
			transport.TLSClientConfig = config
		}
	}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		if !strings.EqualFold(strings.TrimSuffix(host, "."), strings.TrimSuffix(endpoint.Hostname(), ".")) {
			return nil, errors.New("remote MCP redirect changed endpoint host")
		}
		addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil || len(addresses) == 0 {
			return nil, fmt.Errorf("resolve endpoint host: %w", err)
		}
		for _, candidate := range addresses {
			candidate = candidate.Unmap()
			// The host pin above still holds for a dev origin, so a redirect
			// pointing somewhere else is refused either way. What this skips is
			// only "the address must be on the public internet".
			if !devOrigin && !isPublicAddress(candidate) {
				return nil, errors.New("remote MCP endpoint resolved to a non-public address")
			}
			connection, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(candidate.String(), port))
			if dialErr == nil {
				return connection, nil
			}
		}
		return nil, errors.New("connect to remote MCP endpoint failed")
	}
	return &http.Client{
		Timeout:   CallTimeout,
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("remote MCP redirects are not allowed")
		},
	}
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type discoveredTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// SupportedProtocolVersions is the MCP protocol revisions this build speaks,
// most preferred first. Discover offers the first and accepts any of them.
func SupportedProtocolVersions() []string {
	return []string{"2025-03-26", "2024-11-05"}
}

func Discover(ctx context.Context, rawEndpoint string, allowedHosts, protocolVersions []string, headers http.Header) ([]Tool, string, error) {
	endpoint, err := ValidatePublicHTTPSEndpoint(ctx, rawEndpoint, allowedHosts, nil)
	if err != nil {
		return nil, "", err
	}
	client := NewSecureHTTPClient(endpoint)
	sessionID := ""
	// An empty list means "whatever this build supports", not "nothing is
	// acceptable". The response is checked against this same slice further
	// down, so leaving it empty rejected every server that answered correctly —
	// which is exactly what it did to the first plugin that used this path.
	if len(protocolVersions) == 0 {
		protocolVersions = SupportedProtocolVersions()
	}
	protocolVersion := protocolVersions[0]
	initialize := map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "multica-plugin-review", "version": "1"},
		},
	}
	initializeResponse, responseHeaders, err := call(ctx, client, endpoint, headers, sessionID, initialize)
	if err != nil {
		return nil, "", fmt.Errorf("initialize remote MCP: %w", err)
	}
	sessionID = responseHeaders.Get("Mcp-Session-Id")
	var initialized struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := json.Unmarshal(initializeResponse.Result, &initialized); err != nil || !containsString(protocolVersions, initialized.ProtocolVersion) {
		return nil, "", fmt.Errorf("remote MCP negotiated unsupported protocol version %q", initialized.ProtocolVersion)
	}
	if err := notify(ctx, client, endpoint, headers, sessionID, map[string]any{
		"jsonrpc": "2.0", "method": "notifications/initialized", "params": map[string]any{},
	}); err != nil {
		return nil, "", fmt.Errorf("confirm remote MCP initialization: %w", err)
	}
	toolsResponse, _, err := call(ctx, client, endpoint, headers, sessionID, map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": map[string]any{},
	})
	if err != nil {
		return nil, "", fmt.Errorf("list remote MCP tools: %w", err)
	}
	var result struct {
		Tools []discoveredTool `json:"tools"`
	}
	if err := json.Unmarshal(toolsResponse.Result, &result); err != nil {
		return nil, "", fmt.Errorf("decode tools/list result: %w", err)
	}
	tools := make([]Tool, 0, len(result.Tools))
	seen := map[string]bool{}
	for _, tool := range result.Tools {
		if strings.TrimSpace(tool.Name) == "" || seen[tool.Name] {
			return nil, "", errors.New("remote MCP returned an invalid or duplicate tool name")
		}
		seen[tool.Name] = true
		canonical, err := canonicalJSON(tool.InputSchema)
		if err != nil {
			return nil, "", fmt.Errorf("tool %q input schema: %w", tool.Name, err)
		}
		tools = append(tools, Tool{
			Name: tool.Name, Description: tool.Description, InputSchema: canonical,
			SchemaDigest: DigestBytes(canonical),
		})
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	digest, err := ToolSetDigest(tools)
	if err != nil {
		return nil, "", err
	}
	return tools, digest, nil
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func notify(ctx context.Context, client *http.Client, endpoint *url.URL, headers http.Header, sessionID string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	for key, values := range headers {
		for _, value := range values {
			request.Header.Add(key, value)
		}
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	if sessionID != "" {
		request.Header.Set("Mcp-Session-Id", sessionID)
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("remote MCP returned HTTP %d", response.StatusCode)
	}
	written, err := io.Copy(io.Discard, io.LimitReader(response.Body, MaxResponseBytes+1))
	if err != nil {
		return err
	}
	if written > MaxResponseBytes {
		return errors.New("remote MCP response exceeds size limit")
	}
	return nil
}

func call(ctx context.Context, client *http.Client, endpoint *url.URL, headers http.Header, sessionID string, payload any) (rpcResponse, http.Header, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return rpcResponse{}, nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return rpcResponse{}, nil, err
	}
	for key, values := range headers {
		for _, value := range values {
			request.Header.Add(key, value)
		}
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	if sessionID != "" {
		request.Header.Set("Mcp-Session-Id", sessionID)
	}
	response, err := client.Do(request)
	if err != nil {
		return rpcResponse{}, nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return rpcResponse{}, response.Header, fmt.Errorf("remote MCP returned HTTP %d", response.StatusCode)
	}
	raw, err := readResponse(response)
	if err != nil {
		return rpcResponse{}, response.Header, err
	}
	var decoded rpcResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return rpcResponse{}, response.Header, fmt.Errorf("decode JSON-RPC response: %w", err)
	}
	if decoded.Error != nil {
		return rpcResponse{}, response.Header, fmt.Errorf("remote MCP error %d: %s", decoded.Error.Code, decoded.Error.Message)
	}
	return decoded, response.Header, nil
}

func readResponse(response *http.Response) ([]byte, error) {
	limited := io.LimitReader(response.Body, MaxResponseBytes+1)
	if strings.HasPrefix(strings.ToLower(response.Header.Get("Content-Type")), "text/event-stream") {
		scanner := bufio.NewScanner(limited)
		scanner.Buffer(make([]byte, 4096), MaxResponseBytes+1)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data:") {
				data := bytes.TrimSpace([]byte(strings.TrimPrefix(line, "data:")))
				if len(data) > 0 {
					return data, nil
				}
			}
		}
		if err := scanner.Err(); err != nil {
			return nil, err
		}
		return nil, errors.New("remote MCP SSE response contained no data")
	}
	raw, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if len(raw) > MaxResponseBytes {
		return nil, errors.New("remote MCP response exceeds size limit")
	}
	return raw, nil
}

func canonicalJSON(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		raw = json.RawMessage(`{"type":"object"}`)
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(value)
	return json.RawMessage(canonical), err
}

func ToolSetDigest(tools []Tool) (string, error) {
	copyTools := append([]Tool(nil), tools...)
	for i := range copyTools {
		canonical, err := canonicalJSON(copyTools[i].InputSchema)
		if err != nil {
			return "", fmt.Errorf("tool %q input schema: %w", copyTools[i].Name, err)
		}
		copyTools[i].InputSchema = canonical
	}
	sort.Slice(copyTools, func(i, j int) bool { return copyTools[i].Name < copyTools[j].Name })
	raw, err := json.Marshal(copyTools)
	if err != nil {
		return "", err
	}
	return DigestBytes(raw), nil
}
