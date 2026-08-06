package daemon

import (
	"context"
	"fmt"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
)

const runtimeHookTimeout = 5 * time.Second

func (d *Daemon) evaluateRuntimeHook(ctx context.Context, taskID string, event RuntimeHookEvent) (RuntimeHookResult, error) {
	hookCtx, cancel := context.WithTimeout(ctx, runtimeHookTimeout)
	defer cancel()
	return d.client.EmitRuntimeHookEvent(hookCtx, taskID, event)
}

func (d *Daemon) evaluateBlockingRuntimeHook(ctx context.Context, taskID string, event RuntimeHookEvent) (RuntimeHookResult, error) {
	result, err := d.evaluateRuntimeHook(ctx, taskID, event)
	if err != nil {
		return RuntimeHookResult{Decision: "allow", Warning: err.Error()}, nil
	}
	if result.Decision == "block" {
		return result, fmt.Errorf("workflow hook blocked %s", event.EventType)
	}
	return result, nil
}

func (d *Daemon) emitRuntimeHookAsync(taskID string, event RuntimeHookEvent) {
	go func() {
		_, _ = d.evaluateRuntimeHook(context.Background(), taskID, event)
	}()
}

func (d *Daemon) applyRuntimePromptHook(ctx context.Context, task Task, provider, prompt string) (string, error) {
	result, err := d.evaluateBlockingRuntimeHook(ctx, task.ID, RuntimeHookEvent{
		EventType:      "before.prompt.assemble",
		ProposedAction: map[string]any{"prompt": prompt},
		MutableFields:  []string{"prompt"},
		Context:        map[string]any{"prompt": prompt, "provider": provider},
	})
	if err != nil {
		return "", err
	}
	if modifiedPrompt, ok := result.Modifications["prompt"].(string); ok {
		return modifiedPrompt, nil
	}
	return prompt, nil
}

func (d *Daemon) beforeRuntimeSession(ctx context.Context, taskID, model, provider string, resuming bool) error {
	_, err := d.evaluateBlockingRuntimeHook(ctx, taskID, RuntimeHookEvent{
		EventType: "before.session.start",
		Model:     model,
		Context:   map[string]any{"provider": provider, "resuming": resuming},
	})
	return err
}

func (d *Daemon) emitRuntimeSessionStarted(taskID string, opts agent.ExecOptions) {
	d.emitRuntimeHookAsync(taskID, RuntimeHookEvent{
		EventType: "after.session.start",
		Model:     opts.Model,
		Context:   map[string]any{"resuming": opts.ResumeSessionID != ""},
	})
}

func (d *Daemon) emitRuntimeToolResult(taskID, toolName, callID, output string) {
	d.emitRuntimeHookAsync(taskID, RuntimeHookEvent{
		EventType: "after.tool.call",
		Context: map[string]any{
			"tool":    map[string]any{"name": toolName, "output": output},
			"call_id": callID,
		},
	})
}

func (d *Daemon) emitRuntimeError(taskID, message string) {
	d.emitRuntimeHookAsync(taskID, RuntimeHookEvent{
		EventType: "on.error",
		Context:   map[string]any{"error": message, "source": "agent"},
	})
}

func (d *Daemon) finishRuntimeSession(ctx context.Context, taskID string, result agent.Result, opts agent.ExecOptions) error {
	if _, err := d.evaluateBlockingRuntimeHook(ctx, taskID, RuntimeHookEvent{
		EventType: "before.session.end",
		SessionID: result.SessionID,
		Model:     opts.Model,
		Context:   map[string]any{"status": result.Status, "error": result.Error},
	}); err != nil {
		return err
	}
	d.emitRuntimeHookAsync(taskID, RuntimeHookEvent{
		EventType: "after.session.end",
		SessionID: result.SessionID,
		Model:     opts.Model,
		Context:   map[string]any{"status": result.Status, "error": result.Error},
	})
	return nil
}
