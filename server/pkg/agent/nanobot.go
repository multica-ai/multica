package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const nanobotSessionFile = ".nanobot_session"

const defaultNanobotGatewayURL = "ws://127.0.0.1:8765/ws"

// nanobotBackend implements Backend by connecting to a running nanobot
// gateway via its WebSocket channel protocol. Unlike CLI-based backends
// (claude, opencode), nanobot runs as a long-lived gateway server and
// the daemon communicates over WebSocket — sending message envelopes
// and receiving streaming events (delta, message, turn_end).
type nanobotBackend struct {
	cfg Config
}

func (b *nanobotBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)

		startTime := time.Now()
		finalStatus := "completed"
		var finalError string
		var chatID string

		gatewayURL, err := b.resolveGatewayURL(runCtx)
		if err != nil {
			finalStatus = "failed"
			finalError = fmt.Sprintf("nanobot gateway URL resolve failed: %v", err)
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
			return
		}
		b.cfg.Logger.Info("connecting to nanobot gateway", "url", gatewayURL)

		dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
		conn, _, err := dialer.DialContext(runCtx, gatewayURL, nil)
		if err != nil {
			finalStatus = "failed"
			finalError = fmt.Sprintf("nanobot gateway connect failed: %v", err)
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
			return
		}
		defer conn.Close()

		// Read the "ready" event to get the default chat_id.
		if err := conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
			finalStatus = "failed"
			finalError = fmt.Sprintf("nanobot set deadline failed: %v", err)
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
			return
		}
		_, raw, err := conn.ReadMessage()
		if err != nil {
			finalStatus = "failed"
			finalError = fmt.Sprintf("nanobot gateway ready read failed: %v", err)
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
			return
		}
		var ready struct {
			Event  string `json:"event"`
			ChatID string `json:"chat_id"`
		}
		if err := json.Unmarshal(raw, &ready); err != nil || ready.ChatID == "" {
			finalStatus = "failed"
			finalError = "nanobot gateway returned no chat_id in ready event"
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
			return
		}
		chatID = ready.ChatID

		// Resolve the session to resume: server-provided ID takes priority,
		// then fall back to the chat_id cached in the workdir.
		resumeID := opts.ResumeSessionID
		if resumeID == "" && opts.Cwd != "" {
			if data, err := os.ReadFile(filepath.Join(opts.Cwd, nanobotSessionFile)); err == nil {
				if id := strings.TrimSpace(string(data)); id != "" {
					resumeID = id
				}
			}
		}

		// If resuming a previous session, attach to the existing chat_id
		// so the agent retains conversation context.
		if resumeID != "" {
			attachEnvelope := map[string]any{
				"type":    "attach",
				"chat_id": resumeID,
			}
			if err := conn.WriteJSON(attachEnvelope); err != nil {
				finalStatus = "failed"
				finalError = fmt.Sprintf("nanobot gateway attach failed: %v", err)
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			// Read the "attached" confirmation event.
			_, raw, err = conn.ReadMessage()
			if err != nil {
				finalStatus = "failed"
				finalError = fmt.Sprintf("nanobot gateway attach confirm read failed: %v", err)
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			chatID = resumeID
		}

		b.cfg.Logger.Info("nanobot gateway connected", "chat_id", chatID, "resume", resumeID != "")

		_ = conn.SetReadDeadline(time.Time{})

		userText := prompt
		if opts.SystemPrompt != "" {
			userText = opts.SystemPrompt + "\n\n---\n\n" + prompt
		}

		envelope := map[string]any{
			"type":    "message",
			"chat_id": chatID,
			"content": userText,
		}

		if err := conn.WriteJSON(envelope); err != nil {
			finalStatus = "failed"
			finalError = fmt.Sprintf("nanobot gateway send failed: %v", err)
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
			return
		}

		b.cfg.Logger.Info("nanobot prompt sent", "chat_id", chatID)

		var output strings.Builder

	eventLoop:
		for {
			type readResult struct {
				msgType int
				data    []byte
				err     error
			}
			readCh := make(chan readResult, 1)
			go func() {
				mt, d, e := conn.ReadMessage()
				readCh <- readResult{mt, d, e}
			}()

			select {
			case <-runCtx.Done():
				if runCtx.Err() == context.DeadlineExceeded {
					finalStatus = "timeout"
					finalError = fmt.Sprintf("nanobot timed out after %s", timeout)
				} else {
					finalStatus = "aborted"
					finalError = "execution cancelled"
				}
				break eventLoop

			case result := <-readCh:
				if result.err != nil {
					if finalStatus == "completed" && output.Len() > 0 {
						break eventLoop
					}
					if websocket.IsCloseError(result.err, websocket.CloseNormalClosure) {
						break eventLoop
					}
					if websocket.IsUnexpectedCloseError(result.err) {
						if output.Len() > 0 {
							break eventLoop
						}
						finalStatus = "failed"
						finalError = fmt.Sprintf("nanobot connection closed: %v", result.err)
						break eventLoop
					}
					finalStatus = "failed"
					finalError = fmt.Sprintf("nanobot read error: %v", result.err)
					break eventLoop
				}

				msgType, content, status := processNanobotEvent(result.data)
				switch msgType {
				case MessageText:
					output.WriteString(content)
					trySend(msgCh, Message{Type: MessageText, Content: content})
				case MessageThinking:
					trySend(msgCh, Message{Type: MessageThinking, Content: content})
				case MessageToolUse:
					trySend(msgCh, Message{Type: MessageToolUse, Tool: content})
				case MessageStatus:
					if status == "turn_end" {
						break eventLoop
					}
					trySend(msgCh, Message{Type: MessageStatus, Status: status})
				case MessageError:
					trySend(msgCh, Message{Type: MessageError, Content: content})
				}
			}
		}

		duration := time.Since(startTime)
		b.cfg.Logger.Info("nanobot finished", "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		// Persist chat_id to workdir so the next task on the same issue
		// can resume this session even after a daemon restart.
		if opts.Cwd != "" && chatID != "" && finalStatus != "failed" {
			_ = os.WriteFile(filepath.Join(opts.Cwd, nanobotSessionFile), []byte(chatID+"\n"), 0o644)
		}

		resCh <- Result{
			Status:     finalStatus,
			Output:     output.String(),
			Error:      finalError,
			DurationMs: duration.Milliseconds(),
			SessionID:  chatID,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

// gatewayURL returns the raw base URL (for tests).
func (b *nanobotBackend) gatewayURL() string {
	if u := b.cfg.Env["NANOBOT_GATEWAY_URL"]; u != "" {
		return u
	}
	return defaultNanobotGatewayURL
}

// resolveGatewayURL returns the full WebSocket URL, fetching a short-lived
// token via /auth/token if NANOBOT_GATEWAY_AUTH_SECRET is configured.
func (b *nanobotBackend) resolveGatewayURL(ctx context.Context) (string, error) {
	base := b.cfg.Env["NANOBOT_GATEWAY_URL"]
	if base == "" {
		base = defaultNanobotGatewayURL
	}

	secret := b.cfg.Env["NANOBOT_GATEWAY_AUTH_SECRET"]
	if secret == "" {
		return base, nil
	}

	u, err := url.Parse(base)
	if err != nil {
		return base, nil
	}

	tokenURL := url.URL{Scheme: "http", Host: u.Host, Path: "/auth/token"}
	if u.Scheme == "wss" {
		tokenURL.Scheme = "https"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tokenURL.String(), nil)
	if err != nil {
		return "", fmt.Errorf("build token request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+secret)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch gateway token from %s: %w", tokenURL.String(), err)
	}
	defer resp.Body.Close()

	var tokenResp struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil || tokenResp.Token == "" {
		return "", fmt.Errorf("gateway token response missing token (status %d)", resp.StatusCode)
	}

	q := u.Query()
	q.Set("token", tokenResp.Token)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// processNanobotEvent parses a single WebSocket frame from the nanobot
// gateway and returns the message type, content, and status.
func processNanobotEvent(raw []byte) (MessageType, string, string) {
	var event struct {
		Event string `json:"event"`
		Text  string `json:"text"`
		Kind  string `json:"kind"`
	}
	if err := json.Unmarshal(raw, &event); err != nil {
		return "", "", ""
	}
	switch event.Event {
	case "delta":
		if event.Text != "" {
			return MessageText, event.Text, ""
		}
	case "message":
		switch event.Kind {
		case "tool_hint":
			return MessageToolUse, event.Text, ""
		case "progress":
			return MessageStatus, "", event.Text
		default:
			if event.Text != "" {
				return MessageText, event.Text, ""
			}
		}
	case "reasoning_delta":
		if event.Text != "" {
			return MessageThinking, event.Text, ""
		}
	case "turn_end":
		return MessageStatus, "", "turn_end"
	case "error":
		return MessageError, event.Text, ""
	}
	return "", "", ""
}
