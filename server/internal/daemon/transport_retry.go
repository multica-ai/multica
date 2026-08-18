package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync/atomic"

	"github.com/multica-ai/multica/server/internal/daemon/transportretry"
	"github.com/multica-ai/multica/server/pkg/agent"
)

// transportRetryReceipt is optional task-run metadata when in-turn transport retry occurred.
type transportRetryReceipt struct {
	PolicyID                 string   `json:"policy_id,omitempty"`
	Attempts                 int      `json:"attempts,omitempty"`
	RecoveredOnAttempt       int      `json:"recovered_on_attempt,omitempty"`
	SessionModes             []string `json:"session_modes,omitempty"`
	CacheReadTokensFirst     int64    `json:"cache_read_tokens_first,omitempty"`
	CacheReadTokensRecovered int64    `json:"cache_read_tokens_recovered,omitempty"`
	SurfacedToServer         bool     `json:"surfaced_to_server,omitempty"`
}

func (d *Daemon) executeWithTransportRetry(
	ctx context.Context,
	cfg transportretry.Config,
	backend agent.Backend,
	prompt string,
	execOpts agent.ExecOptions,
	taskLog *slog.Logger,
	taskID, codexHome string,
	msgSeq *atomic.Int32,
	provider string,
	priorSessionID string,
	onFreshSession func(*agent.ExecOptions) string,
) (agent.Result, int32, transportretry.Stats, string, json.RawMessage, error) {
	viewOpts := transportretry.ExecOptionsView{ResumeSessionID: execOpts.ResumeSessionID}
	observer := transportretry.MetricsObserver{Metrics: transportretry.GlobalMetrics()}

	hooks := transportretry.RetryHooks{
		OnFreshSession: func(view *transportretry.ExecOptionsView) (string, string) {
			retired := view.ResumeSessionID
			if retired == "" {
				retired = priorSessionID
			}
			execOpts.ResumeSessionID = ""
			view.ResumeSessionID = ""
			freshPrompt := ""
			if onFreshSession != nil {
				freshPrompt = onFreshSession(&execOpts)
			}
			return freshPrompt, retired
		},
	}

	execute := func(runPrompt string, opts transportretry.ExecOptionsView) (transportretry.ResultView, int32, error) {
		execOpts.ResumeSessionID = opts.ResumeSessionID
		result, tools, err := d.executeAndDrain(ctx, backend, runPrompt, execOpts, taskLog, taskID, codexHome, msgSeq)
		return agentResultView(result), tools, err
	}

	result, tools, stats, retired, err := transportretry.ExecuteWithRetry(
		ctx,
		cfg,
		provider,
		priorSessionID,
		hooks,
		observer,
		taskLog,
		execute,
		prompt,
		viewOpts,
	)

	receiptJSON := marshalTransportRetryReceipt(stats)
	return agentResultFromView(result), tools, stats, retired, receiptJSON, err
}

func agentResultView(r agent.Result) transportretry.ResultView {
	usage := make(map[string]transportretry.TokenUsageView, len(r.Usage))
	for model, u := range r.Usage {
		usage[model] = transportretry.TokenUsageView{CacheReadTokens: u.CacheReadTokens}
	}
	return transportretry.ResultView{
		Status:    r.Status,
		Output:    r.Output,
		Error:     r.Error,
		SessionID: r.SessionID,
		Usage:     usage,
	}
}

func agentResultFromView(v transportretry.ResultView) agent.Result {
	usage := make(map[string]agent.TokenUsage, len(v.Usage))
	for model, u := range v.Usage {
		usage[model] = agent.TokenUsage{CacheReadTokens: u.CacheReadTokens}
	}
	return agent.Result{
		Status:    v.Status,
		Output:    v.Output,
		Error:     v.Error,
		SessionID: v.SessionID,
		Usage:     usage,
	}
}

func marshalTransportRetryReceipt(stats transportretry.Stats) json.RawMessage {
	if stats.Attempts <= 1 && stats.PolicyID == "" {
		return nil
	}
	modes := make([]string, len(stats.SessionModes))
	for i, m := range stats.SessionModes {
		modes[i] = m.String()
	}
	receipt := transportRetryReceipt{
		PolicyID:                 stats.PolicyID,
		Attempts:                 stats.Attempts,
		RecoveredOnAttempt:       stats.RecoveredOnAttempt,
		SessionModes:             modes,
		CacheReadTokensFirst:     stats.CacheReadTokensFirst,
		CacheReadTokensRecovered: stats.CacheReadTokensRecovered,
		SurfacedToServer:         stats.SurfacedToServer,
	}
	b, err := json.Marshal(receipt)
	if err != nil {
		return nil
	}
	return b
}
