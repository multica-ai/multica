package agentvault

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// Relay is an authenticating forward-proxy that chains to the Agent Vault MITM
// proxy over the internal path. Agents set HTTPS_PROXY to this relay and
// authenticate with their OWN Multica agent token (Proxy-Authorization). The
// relay resolves the agent's scoped Agent Vault token server-side and chains
// the CONNECT to agent-vault.internal, which swaps in the real credential.
//
// Net effect of "brokering in Multica": the agent machine never reaches the
// internal path, never holds the Agent Vault token, and never sees the
// underlying secret. It only ever carries its own Multica token.
type Relay struct {
	// proxyAddr is the Agent Vault MITM proxy on the internal path, host:port.
	proxyAddr string
	resolver  TokenResolver
	// dial is overridable in tests; defaults to a net.Dialer.
	dial func(ctx context.Context, network, addr string) (net.Conn, error)
}

// TokenResolver maps an agent's Multica proxy credential to that agent's minted,
// vault-scoped Agent Vault token. Returning ok=false denies the request.
type TokenResolver interface {
	ResolveAgentVaultToken(ctx context.Context, agentProxyCred string) (avToken string, ok bool)
}

// NewRelay builds a Relay. proxyAddr is the internal host:port of the Agent
// Vault MITM proxy (e.g. "agent-vault.internal:14322").
func NewRelay(proxyAddr string, resolver TokenResolver) *Relay {
	d := &net.Dialer{Timeout: 10 * time.Second}
	return &Relay{proxyAddr: proxyAddr, resolver: resolver, dial: d.DialContext}
}

// ServeHTTP handles the CONNECT tunnel: authenticate the agent, then chain to
// the Agent Vault proxy carrying the agent's scoped Agent Vault token.
func (r *Relay) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodConnect {
		http.Error(w, "only CONNECT is supported by the Agent Vault relay", http.StatusMethodNotAllowed)
		return
	}
	cred, ok := ParseProxyCredential(req.Header.Get("Proxy-Authorization"))
	if !ok {
		w.Header().Set("Proxy-Authenticate", "Basic realm=\"agentvault\"")
		http.Error(w, "proxy authentication required", http.StatusProxyAuthRequired)
		return
	}
	avToken, ok := r.resolver.ResolveAgentVaultToken(req.Context(), cred)
	if !ok {
		http.Error(w, "agent has no Agent Vault access", http.StatusForbidden)
		return
	}

	upstream, err := r.dial(req.Context(), "tcp", r.proxyAddr)
	if err != nil {
		http.Error(w, "agent vault unreachable", http.StatusBadGateway)
		return
	}
	defer upstream.Close()

	// Chain the CONNECT to the Agent Vault proxy, authenticating with the
	// agent's scoped Agent Vault token. Agent Vault MITMs from here.
	connectReq := fmt.Sprintf(
		"CONNECT %s HTTP/1.1\r\nHost: %s\r\nProxy-Authorization: Bearer %s\r\n\r\n",
		req.Host, req.Host, avToken,
	)
	if _, err := upstream.Write([]byte(connectReq)); err != nil {
		http.Error(w, "agent vault connect failed", http.StatusBadGateway)
		return
	}
	br := bufio.NewReader(upstream)
	resp, err := http.ReadResponse(br, req)
	if err != nil || resp.StatusCode != http.StatusOK {
		http.Error(w, "agent vault refused tunnel", http.StatusBadGateway)
		return
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "relay requires a hijackable connection", http.StatusInternalServerError)
		return
	}
	client, _, err := hj.Hijack()
	if err != nil {
		return
	}
	defer client.Close()
	if _, err := client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		return
	}
	// Flush any bytes Agent Vault already buffered, then pipe both ways.
	if n := br.Buffered(); n > 0 {
		if peek, perr := br.Peek(n); perr == nil {
			_, _ = client.Write(peek)
		}
	}
	pipe(client, upstream)
}

// pipe copies bytes bidirectionally until either side closes.
func pipe(a, b net.Conn) {
	done := make(chan struct{}, 2)
	cp := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		done <- struct{}{}
	}
	go cp(a, b)
	go cp(b, a)
	<-done
}

// ParseProxyCredential extracts the agent's credential from a Proxy-Authorization
// header. Supports "Bearer <token>" and "Basic <base64>" (credential is the
// username part, matching how Agent Vault's own clients pass token:vault).
// Returns ok=false when absent or malformed.
func ParseProxyCredential(header string) (string, bool) {
	header = strings.TrimSpace(header)
	if header == "" {
		return "", false
	}
	scheme, rest, found := strings.Cut(header, " ")
	if !found {
		return "", false
	}
	rest = strings.TrimSpace(rest)
	switch strings.ToLower(scheme) {
	case "bearer":
		if rest == "" {
			return "", false
		}
		return rest, true
	case "basic":
		dec, err := base64.StdEncoding.DecodeString(rest)
		if err != nil {
			return "", false
		}
		user, _, _ := strings.Cut(string(dec), ":")
		if user == "" {
			return "", false
		}
		return user, true
	default:
		return "", false
	}
}
