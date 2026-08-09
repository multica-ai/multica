package workflows

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/multica-ai/multica/server/internal/cerebro/commentquality"
)

// QualityGateActionType is the typed hook action that judges a proposed comment
// against a rubric live, at before.message.send time (FIR-4727). It is the
// sustainable effective-comments gate: unlike judge.gate (async, enqueues a
// separate judge run) it returns PASS/REJECT inline, so a good comment passes
// and a bad one is rejected with a concrete rewrite the agent retries in the
// same turn.
//
// Wiring: the handler decision stays `allow`. A REJECT is a SUCCESSFUL judgment,
// so the action returns success with {"decision":"require","requirement":…}; the
// engine honors that verdict unconditionally (a bad comment is blocked even
// under fail_mode: warn) and surfaces the requirement in the 422. Only a judge
// FAILURE (gateway unreachable/timeout) returns an error — that is what
// fail_mode governs: warn lets comments through during an outage, closed blocks.
const QualityGateActionType = "quality.gate"

// qualityGateRunner is the seam to the LLM judge. It is a package var so tests
// run the action without a gateway; production uses a zero Judger (env config).
var qualityGateRunner = func(ctx context.Context, rubric, content, userLabel string) (commentquality.Result, error) {
	return (&commentquality.Judger{}).Judge(ctx, rubric, content, userLabel)
}

// qualityGate judges event.Proposed["content"] against the config rubric.
//   - empty content            -> pass (nothing to judge)
//   - judge error (gateway/…)  -> error, so fail_mode decides
//   - PASS                     -> {"pass": true} (decision stays allow)
//   - REJECT                   -> {"decision":"require","requirement":…}, no error;
//     the engine blocks unconditionally and shows the requirement in the 422.
func (e *PostgresHookActionExecutor) qualityGate(ctx context.Context, event HookEvent, config map[string]any) (map[string]any, error) {
	rubric := strings.TrimSpace(hookConfigString(config, "rubric", ""))
	if rubric == "" {
		return nil, errors.New("quality.gate requires a rubric")
	}
	content := strings.TrimSpace(proposedContent(event))
	if content == "" {
		return map[string]any{"pass": true}, nil
	}
	res, err := qualityGateRunner(ctx, rubric, content, event.AgentID)
	if err != nil {
		return nil, fmt.Errorf("quality.gate judge: %w", err)
	}
	if res.Pass {
		return map[string]any{"pass": true}, nil
	}
	requirement := strings.TrimSpace(res.Requirement)
	if requirement == "" {
		requirement = "Rewrite the comment to satisfy effective-comments before sending."
	}
	return map[string]any{"decision": string(HookRequire), "requirement": requirement}, nil
}

func proposedContent(event HookEvent) string {
	if content, ok := event.Proposed["content"].(string); ok {
		return content
	}
	return ""
}
