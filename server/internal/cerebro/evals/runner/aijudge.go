package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/multica-ai/multica/server/internal/cerebro/evals"
)

// aijudge.go is the ai_judge grader: it asks a model whether a produced answer
// satisfies the task, given the situation, the expected facit, and an optional
// author rubric. The verdict is parsed fail-closed — anything that is not an
// explicit PASS counts as a fail — so a malformed or evasive judge reply can
// never turn a bad answer green.

type aiJudgeConfig struct {
	// Model overrides the completer's default judge model when set.
	Model string `json:"model"`
	// Rubric is the author's extra grading guidance, appended to the prompt.
	Rubric string `json:"rubric"`
}

// AIJudgeGrader grades answers with a model call via the injected Completer.
type AIJudgeGrader struct {
	completer Completer
	config    aiJudgeConfig
}

// NewAIJudgeGrader builds the grader from an ai_judge grader definition. The
// completer is required; without it there is no way to reach a model.
func NewAIJudgeGrader(completer Completer, g evals.Grader) (*AIJudgeGrader, error) {
	if completer == nil {
		return nil, errors.New("ai_judge grader needs a completer")
	}
	cfg := aiJudgeConfig{}
	if len(g.Config) > 0 {
		if err := json.Unmarshal(g.Config, &cfg); err != nil {
			return nil, fmt.Errorf("ai_judge config: %w", err)
		}
	}
	cfg.Model = strings.TrimSpace(cfg.Model)
	cfg.Rubric = strings.TrimSpace(cfg.Rubric)
	return &AIJudgeGrader{completer: completer, config: cfg}, nil
}

const aiJudgeSystem = `You are a strict grader for an automated evaluation.
Decide whether the ANSWER satisfies the TASK given the EXPECTED result and any RUBRIC.
Be conservative: if the answer is wrong, incomplete, or you are unsure, fail it.
Reply with exactly one line starting "VERDICT: PASS" or "VERDICT: FAIL", then a second line "REASON: <one short sentence>".`

// Grade asks the model for a verdict and parses it fail-closed. A transport
// error propagates so the runner records the task as failed; a malformed reply
// is treated as a fail with the raw reply surfaced as the reason.
func (a *AIJudgeGrader) Grade(ctx context.Context, task evals.TaskCase, answer string) (bool, string, int64, error) {
	req := CompletionRequest{
		System: aiJudgeSystem,
		Prompt: buildJudgePrompt(task, answer, a.config.Rubric),
		Model:  a.config.Model,
	}
	res, err := a.completer.Complete(ctx, req)
	if err != nil {
		return false, "", 0, err
	}
	passed, reason := parseVerdict(res.Text)
	return passed, reason, res.CostCents, nil
}

func buildJudgePrompt(task evals.TaskCase, answer, rubric string) string {
	var b strings.Builder
	b.WriteString("TASK:\n")
	b.WriteString(task.Situation)
	b.WriteString("\n\nEXPECTED:\n")
	b.WriteString(task.Expected)
	b.WriteString("\n\nANSWER:\n")
	b.WriteString(answer)
	if rubric != "" {
		b.WriteString("\n\nRUBRIC:\n")
		b.WriteString(rubric)
	}
	return b.String()
}

// parseVerdict is fail-closed: only an explicit PASS verdict passes; every other
// reply (FAIL, missing verdict, empty, garbled) fails. The REASON line, if any,
// becomes the human-readable reason; otherwise the trimmed reply is used.
func parseVerdict(text string) (bool, string) {
	var verdictLine, reasonLine string
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		upper := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(upper, "VERDICT:"):
			verdictLine = strings.ToUpper(strings.TrimSpace(line[len("VERDICT:"):]))
		case strings.HasPrefix(upper, "REASON:"):
			reasonLine = strings.TrimSpace(line[len("REASON:"):])
		}
	}
	passed := strings.HasPrefix(verdictLine, "PASS")
	reason := reasonLine
	if reason == "" {
		reason = strings.TrimSpace(text)
	}
	if reason == "" {
		reason = "judge returned an empty reply"
	}
	if !passed && verdictLine == "" {
		reason = "judge did not return a PASS/FAIL verdict: " + reason
	}
	return passed, reason
}
