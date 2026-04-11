package sandbox

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	e2bAPIBase    = "https://api.e2b.app"
	e2bEnvdPort   = "49983" // default envd port for process/file operations
)

// E2BProvider implements SandboxProvider using the E2B REST + Connect API.
// Sandbox lifecycle (create/delete) uses the E2B management API at api.e2b.app.
// In-sandbox operations (exec, files) use the envd API at {port}-{sandboxID}.e2b.app.
type E2BProvider struct {
	apiKey string
	client *http.Client
}

// NewE2BProvider creates a new E2B sandbox provider.
func NewE2BProvider(apiKey string) *E2BProvider {
	return &E2BProvider{
		apiKey: apiKey,
		client: &http.Client{Timeout: 120 * time.Second},
	}
}

func (p *E2BProvider) CreateOrConnect(ctx context.Context, sandboxID string, opts CreateOpts) (*Sandbox, error) {
	if sandboxID != "" {
		// Try to reconnect to existing sandbox
		sb, err := p.getSandbox(ctx, sandboxID)
		if err == nil {
			return sb, nil
		}
		// Fall through to create
	}

	templateID := opts.TemplateID
	if templateID == "" {
		templateID = "base"
	}

	timeout := 300 // 5 minutes default
	if opts.Timeout > 0 {
		timeout = int(opts.Timeout.Seconds())
	}

	body := map[string]any{
		"templateID": templateID,
		"timeout":    timeout,
	}
	if len(opts.EnvVars) > 0 {
		body["envVars"] = opts.EnvVars
	}

	reqBody, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", e2bAPIBase+"/sandboxes", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("e2b: create request: %w", err)
	}
	req.Header.Set("X-API-Key", p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("e2b: create sandbox: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 201 {
		return nil, fmt.Errorf("e2b: create sandbox failed (%d): %s", resp.StatusCode, string(respBody))
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("e2b: parse create response: %w", err)
	}

	newID, _ := result["sandboxID"].(string)
	clientID, _ := result["clientID"].(string)
	envdToken, _ := result["envdAccessToken"].(string)

	if newID == "" {
		return nil, fmt.Errorf("e2b: no sandboxID in response: %s", string(respBody))
	}

	return &Sandbox{
		ID:       newID,
		Status:   "running",
		Provider: "e2b",
		metadata: map[string]string{
			"clientID":        clientID,
			"envdAccessToken": envdToken,
		},
	}, nil
}

// Exec runs a command in the sandbox via the E2B Connect RPC process API.
// The Connect protocol uses binary framing over HTTP: each frame is
// flags(1) + length(4 big-endian) + payload(JSON).
func (p *E2BProvider) Exec(ctx context.Context, sb *Sandbox, cmd []string) (string, error) {
	if len(cmd) == 0 {
		return "", fmt.Errorf("e2b: empty command")
	}

	// Use envdAccessToken if available, otherwise fall back to the provider API key
	accessToken := sb.metadata["envdAccessToken"]
	if accessToken == "" {
		accessToken = p.apiKey
	}

	// Build the process start request — pass cmd[0] as executable, cmd[1:] as args.
	// This preserves argument boundaries (no shell expansion issues).
	processConfig := map[string]any{
		"cmd": cmd[0],
	}
	if len(cmd) > 1 {
		processConfig["args"] = cmd[1:]
	}
	processReq := map[string]any{
		"process": processConfig,
		"stdin":   false,
	}

	jsonPayload, _ := json.Marshal(processReq)

	// Connect server-streaming: request body must be enveloped
	// Frame format: flags(1) + length(4 big-endian) + payload
	enveloped := connectEnvelope(jsonPayload)

	endpoint := fmt.Sprintf("https://%s-%s.e2b.app/process.Process/Start", e2bEnvdPort, sb.ID)
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(enveloped))
	if err != nil {
		return "", fmt.Errorf("e2b: exec request: %w", err)
	}
	req.Header.Set("Content-Type", "application/connect+json")
	req.Header.Set("Connect-Protocol-Version", "1")
	req.Header.Set("X-Access-Token", accessToken)

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("e2b: exec: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("e2b: exec failed (%d): %s", resp.StatusCode, string(body))
	}

	// Read Connect streaming response frames
	return p.readConnectStream(resp.Body)
}

// readConnectStream reads a Connect server-streaming response.
// Each frame: flags(1 byte) + length(4 bytes big-endian) + JSON payload.
// We collect stdout data from DataEvents and return the exit code from EndEvent.
func (p *E2BProvider) readConnectStream(r io.Reader) (string, error) {
	var stdout strings.Builder
	var exitCode int
	finished := false

	for {
		// Read frame header: 1 byte flags + 4 bytes length
		header := make([]byte, 5)
		if _, err := io.ReadFull(r, header); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return stdout.String(), fmt.Errorf("e2b: read frame header: %w", err)
		}

		flags := header[0]
		length := binary.BigEndian.Uint32(header[1:5])

		if length == 0 {
			continue
		}

		// Read frame payload
		payload := make([]byte, length)
		if _, err := io.ReadFull(r, payload); err != nil {
			return stdout.String(), fmt.Errorf("e2b: read frame payload: %w", err)
		}

		// flags & 0x02 = end-of-stream (trailers frame)
		if flags&0x02 != 0 {
			break
		}

		// Parse the JSON payload as a StartResponse event
		var event connectProcessEvent
		if err := json.Unmarshal(payload, &event); err != nil {
			continue // skip unparseable frames
		}

		if event.Event.Data != nil {
			if event.Event.Data.Stdout != "" {
				// stdout is base64 encoded in E2B Connect API
				if decoded, err := base64.StdEncoding.DecodeString(event.Event.Data.Stdout); err == nil {
					stdout.Write(decoded)
				} else {
					stdout.WriteString(event.Event.Data.Stdout) // fallback to raw
				}
			}
			if event.Event.Data.Stderr != "" {
				if decoded, err := base64.StdEncoding.DecodeString(event.Event.Data.Stderr); err == nil {
					// Append stderr too (useful for error output)
					stdout.Write(decoded)
				}
			}
		}
		if event.Event.End != nil {
			finished = true
			// Parse exit code from status string like "exit status 42"
			if event.Event.End.Status != "" && !strings.Contains(event.Event.End.Status, "status 0") {
				exitCode = 1 // non-zero exit
			}
		}
	}

	if finished && exitCode != 0 {
		return stdout.String(), fmt.Errorf("e2b: command exited with code %d", exitCode)
	}

	return stdout.String(), nil
}

func (p *E2BProvider) ReadFile(ctx context.Context, sb *Sandbox, path string) ([]byte, error) {
	accessToken := sb.metadata["envdAccessToken"]
	if accessToken == "" {
		accessToken = p.apiKey
	}

	endpoint := fmt.Sprintf("https://%s-%s.e2b.app/files?path=%s",
		e2bEnvdPort, sb.ID, url.QueryEscape(path))

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("e2b: read file request: %w", err)
	}
	req.Header.Set("X-Access-Token", accessToken)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("e2b: read file: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("e2b: read file %s (%d): %s", path, resp.StatusCode, string(body))
	}

	return body, nil
}

func (p *E2BProvider) WriteFile(ctx context.Context, sb *Sandbox, path string, content []byte) error {
	accessToken := sb.metadata["envdAccessToken"]
	if accessToken == "" {
		accessToken = p.apiKey
	}

	endpoint := fmt.Sprintf("https://%s-%s.e2b.app/files?path=%s",
		e2bEnvdPort, sb.ID, url.QueryEscape(path))

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(content))
	if err != nil {
		return fmt.Errorf("e2b: write file request: %w", err)
	}
	req.Header.Set("X-Access-Token", accessToken)
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("e2b: write file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("e2b: write file %s (%d): %s", path, resp.StatusCode, string(body))
	}

	return nil
}

func (p *E2BProvider) Destroy(ctx context.Context, sandboxID string) error {
	req, err := http.NewRequestWithContext(ctx, "DELETE", e2bAPIBase+"/sandboxes/"+sandboxID, nil)
	if err != nil {
		return fmt.Errorf("e2b: destroy request: %w", err)
	}
	req.Header.Set("X-API-Key", p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("e2b: destroy: %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode != 204 && resp.StatusCode != 200 {
		return fmt.Errorf("e2b: destroy sandbox %s failed (%d)", sandboxID, resp.StatusCode)
	}
	return nil
}

func (p *E2BProvider) getSandbox(ctx context.Context, sandboxID string) (*Sandbox, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", e2bAPIBase+"/sandboxes/"+sandboxID, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-API-Key", p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("e2b: get sandbox (%d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		SandboxID       string `json:"sandboxID"`
		ClientID        string `json:"clientID"`
		EnvdAccessToken string `json:"envdAccessToken"`
	}
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	return &Sandbox{
		ID:       result.SandboxID,
		Status:   "running",
		Provider: "e2b",
		metadata: map[string]string{
			"clientID":        result.ClientID,
			"envdAccessToken": result.EnvdAccessToken,
		},
	}, nil
}

// connectEnvelope wraps a JSON payload in Connect protocol envelope framing.
// Frame format: flags(1 byte, 0 = data) + length(4 bytes big-endian) + payload.
func connectEnvelope(payload []byte) []byte {
	buf := make([]byte, 5+len(payload))
	buf[0] = 0 // flags: data frame
	binary.BigEndian.PutUint32(buf[1:5], uint32(len(payload)))
	copy(buf[5:], payload)
	return buf
}

// --- Connect RPC response types ---

type connectProcessEvent struct {
	Event processEventWrapper `json:"event"`
}

type processEventWrapper struct {
	Start *processStartEvent `json:"start,omitempty"`
	Data  *processDataEvent  `json:"data,omitempty"`
	End   *processEndEvent   `json:"end,omitempty"`
}

type processStartEvent struct {
	PID int `json:"pid"`
}

type processDataEvent struct {
	Stdout string `json:"stdout,omitempty"`
	Stderr string `json:"stderr,omitempty"`
}

type processEndEvent struct {
	Exited bool   `json:"exited"`
	Status string `json:"status"` // e.g. "exit status 0", "exit status 42"
}
