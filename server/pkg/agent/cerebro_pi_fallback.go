package agent

import (
	"context"
	"strings"
)

const defaultPiGatewayFallbackModel = "claude-sonnet-5"

func (b *piBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	primary, err := b.executeOnce(ctx, prompt, opts)
	if err != nil || !piGatewayFallbackConfigured(b.cfg.Env) {
		return primary, err
	}

	messages := make(chan Message, 256)
	results := make(chan Result, 1)
	go b.bridgePiGatewayFallback(ctx, prompt, opts, primary, messages, results)
	return &Session{Messages: messages, Result: results}, nil
}

func (b *piBackend) bridgePiGatewayFallback(
	ctx context.Context,
	prompt string,
	opts ExecOptions,
	primary *Session,
	messages chan Message,
	results chan Result,
) {
	defer close(messages)
	defer close(results)

	safeToRetry := true
	var pendingPrimary []Message
	flushPrimary := func() {
		for _, message := range pendingPrimary {
			trySend(messages, message)
		}
		pendingPrimary = nil
	}
	for message := range primary.Messages {
		if message.Type == MessageToolUse || message.Type == MessageToolResult || message.Type == MessageText {
			safeToRetry = false
			flushPrimary()
		}
		if safeToRetry && (message.Type == MessageStatus || message.Type == MessageError) {
			pendingPrimary = append(pendingPrimary, message)
			continue
		}
		trySend(messages, message)
	}
	primaryResult, ok := <-primary.Result
	if !ok {
		return
	}
	if !safeToRetry || !piGatewayFallbackEligible(primaryResult) {
		flushPrimary()
		results <- primaryResult
		return
	}

	fallbackOpts := opts
	fallbackOpts.Model = "firtal-gateway/" + piGatewayFallbackModel(b.cfg.Env)
	fallbackOpts.ResumeSessionID = ""
	fallback, err := b.executeOnce(ctx, prompt, fallbackOpts)
	if err != nil {
		flushPrimary()
		results <- primaryResult
		return
	}
	for message := range fallback.Messages {
		trySend(messages, message)
	}
	fallbackResult, ok := <-fallback.Result
	if !ok {
		results <- primaryResult
		return
	}
	results <- fallbackResult
}

func piGatewayFallbackConfigured(env map[string]string) bool {
	return firstEnv(env, "FIRTAL_REGISTRY_URL") != "" &&
		firstEnv(env, "FIRTAL_REGISTRY_KEY") != ""
}

func piGatewayFallbackModel(env map[string]string) string {
	if model := strings.TrimSpace(firstEnv(env, "FIRTAL_REGISTRY_MODEL")); model != "" {
		return model
	}
	return defaultPiGatewayFallbackModel
}

func piGatewayFallbackEligible(result Result) bool {
	if result.Status != "failed" {
		return false
	}
	errorText := strings.ToLower(result.Error)
	return containsAny(errorText,
		"authentication rejected",
		"rate limited",
		"subscription or quota limit",
		"network unavailable",
	)
}
