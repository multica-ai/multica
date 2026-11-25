package agent

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Hook-based steering (Cursor, Antigravity) shares one transport: the agent
// CLI runs the multica binary as a hook command, and that relay forwards the
// ownership fields of the hook payload to the run's token-protected loopback
// endpoint, then prints the endpoint's reply as the hook output.
const (
	hookRelayTimeout       = 5 * time.Second
	hookRelayResponseLimit = 1 << 20
)

// forwardHookRequest posts req to the run's endpoint and returns the reply
// object it should print. Every failure — no steering for this run, a stale
// hooks file, an unreachable endpoint — returns empty, so the hook never
// changes what the agent does.
func forwardHookRequest(endpoint, token string, req any, empty string) string {
	if endpoint == "" || token == "" || !loopbackHTTPURL(endpoint) {
		return empty
	}
	body, err := json.Marshal(req)
	if err != nil {
		return empty
	}
	ctx, cancel := context.WithTimeout(context.Background(), hookRelayTimeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return empty
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")
	client := &http.Client{Transport: &http.Transport{Proxy: nil}}
	resp, err := client.Do(httpReq)
	if err != nil {
		return empty
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, hookRelayResponseLimit))
	if err != nil || resp.StatusCode != http.StatusOK {
		return empty
	}
	var output map[string]any
	if json.Unmarshal(data, &output) != nil || output == nil {
		return empty
	}
	return string(bytes.TrimSpace(data))
}

// hookShellCommand quotes argv for the POSIX shell agent CLIs run command
// hooks through.
func hookShellCommand(argv []string) string {
	quoted := make([]string, len(argv))
	for i, arg := range argv {
		quoted[i] = "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
	}
	return strings.Join(quoted, " ")
}

func loopbackHTTPURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" {
		return false
	}
	ip := net.ParseIP(u.Hostname())
	return ip != nil && ip.IsLoopback()
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate random token: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
