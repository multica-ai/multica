package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/url"
	"strings"
	"time"
)

// v2.0.18 waits for session idle and reconciles text/tools from projected
// messages, but stops consuming step events before draining step_finish:
// https://github.com/anomalyco/opencode/blob/v2.0.18/packages/cli/src/run/noninteractive.ts
// Its persisted idle message is stronger evidence than exit 0 (which can also
// follow interrupted runs). Keep this workaround within the verified major.
func opencodeCanVerifyV2Completion(cfg Config) bool {
	if !cfg.BuiltinRuntime {
		return false
	}
	v, err := parseSemver(strings.TrimSpace(cfg.CLIVersion))
	return err == nil && v.Major == 2 && !v.lessThan(semver{Major: 2, Minor: 0, Patch: 18})
}

// Evidence is scoped to the last step observed on this invocation, never just
// the session (which may contain successful turns from earlier resumed runs).
type opencodeV2Completion struct {
	sessionID string
	messageID string
	text      strings.Builder
	tools     map[string]string
}

func (c *opencodeV2Completion) observe(e opencodeEvent) bool {
	if c.sessionID == "" || c.messageID == "" || e.SessionID != c.sessionID {
		return false
	}
	switch e.Type {
	case "step_start", "step_finish", "text", "reasoning", "tool_use":
		if e.Part.MessageID != c.messageID || (e.Part.SessionID != "" && e.Part.SessionID != c.sessionID) {
			return false
		}
	}
	switch e.Type {
	case "text":
		c.text.WriteString(e.Part.Text)
	case "tool_use":
		// v2 emits terminal tools with part.id, unlike v1's callID.
		if e.Part.ID == "" || e.Part.State == nil {
			return false
		}
		status := e.Part.State.Status
		if status != "completed" && status != "error" {
			return false
		}
		c.tools[e.Part.ID] = status
	}
	return true
}

// opencodeConfirmV2Completion reads only the newest idle/assistant pair. If a
// concurrent prompt advanced the session, or the expected records aren't at
// the tail, refuse recovery rather than borrowing another turn's success.
// No pagination/retry is needed: ambiguity retains the original failure.
func opencodeConfirmV2Completion(ctx context.Context, conn opencodeRunConnection, c *opencodeV2Completion, logger *slog.Logger) bool {
	if conn.standalone || c.sessionID == "" || c.messageID == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	args := []string{"api", "GET", "/api/session/" + url.PathEscape(c.sessionID) + "/message?order=desc&limit=2"}
	if conn.server != "" {
		args = append(args, "--server", conn.server)
	}
	cmd := conn.cmd.exec(ctx, args...)
	hideAgentWindow(cmd)
	cmd.Env, cmd.Dir = conn.env, conn.dir
	data, err := outputOwned(cmd, logger)
	if err != nil || ctx.Err() != nil {
		return false
	}
	return c.matches(data)
}

type opencodeV2ProjectedMessage struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Outcome string          `json:"outcome"`
	Finish  string          `json:"finish"`
	Error   json.RawMessage `json:"error"`
	Time    struct {
		Completed int64 `json:"completed"`
	} `json:"time"`
	Content []struct {
		ID    string `json:"id"`
		Type  string `json:"type"`
		Text  string `json:"text"`
		State struct {
			Status string `json:"status"`
		} `json:"state"`
	} `json:"content"`
}

func (c *opencodeV2Completion) matches(data []byte) bool {
	var page struct {
		Data []opencodeV2ProjectedMessage `json:"data"`
	}
	if json.Unmarshal(data, &page) != nil || len(page.Data) != 2 {
		return false
	}
	idle, reply := page.Data[0], page.Data[1]
	if idle.Type != "idle" || idle.Outcome != "succeeded" || idle.ID == "" ||
		reply.Type != "assistant" || reply.ID != c.messageID || reply.Time.Completed <= 0 ||
		reply.Finish != "stop" || (len(reply.Error) != 0 && string(reply.Error) != "null") {
		return false
	}
	var text strings.Builder
	tools := make(map[string]string)
	for _, part := range reply.Content {
		switch part.Type {
		case "text":
			text.WriteString(part.Text)
		case "reasoning": // Reasoning need not be enabled in the stdout stream.
		case "tool":
			if part.ID == "" || (part.State.Status != "completed" && part.State.Status != "error") {
				return false
			}
			tools[part.ID] = part.State.Status
		default:
			return false
		}
	}
	if text.String() != c.text.String() || len(tools) != len(c.tools) || (text.Len() == 0 && len(tools) == 0) {
		return false
	}
	for id, status := range tools {
		if c.tools[id] != status {
			return false
		}
	}
	// Usage remains whatever step_finish actually reported; never manufacture
	// counters for the recovered step from absent or partial accounting.
	return true
}
