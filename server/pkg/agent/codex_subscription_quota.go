package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"syscall"
	"time"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ReadCodexSubscriptionQuota reads the default ChatGPT account bucket. The
// app-server inherits the daemon user's Codex config and auth (including
// CODEX_HOME and keychain access). It never starts a thread or a turn.
func ReadCodexSubscriptionQuota(ctx context.Context, runtimeCmd Command) (*protocol.SubscriptionQuotaSnapshot, error) {
	runtimeCmd, cleanup, err := runtimeCmd.withProbeDirectory()
	if err != nil {
		return nil, err
	}
	defer cleanup()

	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := runtimeCmd.exec(ctx, buildCodexArgs(ExecOptions{}, slog.Default())...)
	hideAgentWindow(cmd)
	cmd.Stderr = io.Discard

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	defer stdin.Close()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	defer stdout.Close()
	if err := startOwnedProcessTree(cmd, slog.Default()); err != nil {
		return nil, err
	}
	client := &codexClient{
		stdin:   stdin,
		pending: make(map[int]*pendingRPC),
	}
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 16*1024), 1024*1024)
		for scanner.Scan() {
			var msg map[string]json.RawMessage
			if json.Unmarshal(scanner.Bytes(), &msg) != nil {
				continue
			}
			// Only handle replies. The normal task client can approve reverse
			// requests, which an account probe must never do.
			if _, isRequest := msg["method"]; !isRequest {
				client.handleResponse(msg)
			}
		}
		client.markProcessExited(scanner.Err())
	}()
	defer func() {
		signalProcessGroup(cmd, syscall.SIGKILL)
		_ = stdout.Close()
		<-readDone
		_ = cmd.Wait()
		releaseProcessGroup(cmd)
	}()

	if _, err := client.request(ctx, "initialize", map[string]any{
		"clientInfo": map[string]string{"name": "multica-quota", "version": "0.1.0"},
	}); err != nil {
		return nil, err
	}
	client.notify("initialized")

	raw, err := client.request(ctx, "account/read", map[string]bool{"refreshToken": false})
	if err != nil {
		return nil, err
	}
	var account struct {
		Account *struct {
			Type string `json:"type"`
		} `json:"account"`
	}
	if err := json.Unmarshal(raw, &account); err != nil {
		return nil, err
	}
	if account.Account == nil || account.Account.Type != "chatgpt" {
		return nil, nil
	}

	raw, err = client.request(ctx, "account/rateLimits/read", nil)
	if err != nil {
		return nil, err
	}
	var limits struct {
		Default *protocol.SubscriptionQuotaSnapshot `json:"rateLimits"`
	}
	if err := json.Unmarshal(raw, &limits); err != nil {
		return nil, err
	}
	if limits.Default == nil || (limits.Default.Primary == nil && limits.Default.Secondary == nil) {
		return nil, nil
	}
	limits.Default.ObservedAt = time.Now().Unix()
	return limits.Default, nil
}
