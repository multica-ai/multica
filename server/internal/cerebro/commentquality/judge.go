// Package commentquality powers the live effective-comments gate (FIR-4727):
// a synchronous LLM judge that scores a proposed agent comment against a rubric
// at before.message.send time. It is the sustainable counterpart to hard-coded
// phrase heuristics — one rubric decides, in any language, on the actual text.
//
// The gateway plumbing (endpoint, auth, BigQuery cost labels, response shape)
// is shared with the duplicate-check judge via duplicatecheck.GatewayConfig.ChatText;
// this package owns only the rubric prompt and the pass/reject verdict.
//
// It is NOT best-effort like duplicatecheck: a gateway/parse failure returns an
// error so the calling hook action can let the policy's fail_mode decide
// (fail_mode: warn keeps comments flowing during an outage; closed blocks them).
package commentquality

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/cerebro/duplicatecheck"
)

// judgeTimeout caps the gateway call. This sits on the critical path of every
// comment a bound agent posts, so it is short: a slow judge must not hold the
// send open for long. On timeout the action surfaces an error and fail_mode
// decides.
const judgeTimeout = 6 * time.Second

// Result is the verdict for one proposed comment. Requirement is the concrete,
// one-line rewrite instruction the agent receives when Pass is false; it is
// surfaced verbatim in the 422 so the agent can fix and retry in the same turn.
type Result struct {
	Pass        bool
	Requirement string
}

// Judger scores a comment against a rubric via the Firtal AI Gateway. A zero
// Judger reads env-resolved config (shared with duplicatecheck) and uses
// http.DefaultClient.
type Judger struct {
	Config duplicatecheck.GatewayConfig
	Client *http.Client
}

// Judge returns the verdict for content against rubric. userLabel tags the
// gateway call for BigQuery cost-tracking (it is not sent to the model).
// Returns an error when the gateway is unconfigured, unreachable, or replies
// with something unparseable — the caller maps that onto the policy fail_mode.
func (j *Judger) Judge(parent context.Context, rubric, content, userLabel string) (Result, error) {
	cfg := j.Config
	if cfg.BaseURL == "" || cfg.APIKey == "" {
		cfg = duplicatecheck.GatewayConfigFromEnv()
	}
	if cfg.Model == "" {
		cfg.Model = duplicatecheck.JudgeModel
	}

	ctx, cancel := context.WithTimeout(parent, judgeTimeout)
	defer cancel()

	text, err := cfg.ChatText(ctx, j.Client, systemPrompt, buildPrompt(rubric, content), 400, "comment-quality", userLabel)
	if err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(text) == "" {
		return Result{}, fmt.Errorf("commentquality: empty gateway response")
	}
	return parseVerdict(text)
}

// systemPrompt frames the judge. It must reply with strict JSON so the verdict
// is machine-readable; the requirement is written back to the agent, so it is
// phrased as a direct instruction in the comment's own language.
const systemPrompt = `You gate an agent's issue comment before it is posted. ` +
	`Judge ONLY the comment text against the RUBRIC. Output valid JSON, no prose. ` +
	`If it satisfies the rubric: {"pass":true}. ` +
	`If it violates the rubric: {"pass":false,"requirement":"<one short imperative sentence telling the author exactly what to change>"}. ` +
	`Write the requirement in the comment's own language (Danish or English). Be strict but do not invent rules beyond the rubric.`

func buildPrompt(rubric, content string) string {
	var b strings.Builder
	b.WriteString("RUBRIC:\n")
	b.WriteString(strings.TrimSpace(rubric))
	b.WriteString("\n\nCOMMENT TO JUDGE:\n")
	b.WriteString(strings.TrimSpace(content))
	b.WriteString("\n\nReturn the JSON verdict now.")
	return b.String()
}

// parseVerdict extracts the verdict from the model reply. The model occasionally
// fences the JSON or prepends a stray token, so we slice from the first '{' to
// the last '}'. A malformed reply is an error (the gate cannot guess).
func parseVerdict(text string) (Result, error) {
	text = strings.TrimSpace(text)
	start := strings.IndexByte(text, '{')
	end := strings.LastIndexByte(text, '}')
	if start < 0 || end <= start {
		return Result{}, fmt.Errorf("commentquality: no JSON object in verdict")
	}
	var wire struct {
		Pass        bool   `json:"pass"`
		Requirement string `json:"requirement"`
	}
	if err := json.Unmarshal([]byte(text[start:end+1]), &wire); err != nil {
		return Result{}, err
	}
	return Result{Pass: wire.Pass, Requirement: strings.TrimSpace(wire.Requirement)}, nil
}
