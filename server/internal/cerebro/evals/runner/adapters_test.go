package runner

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/cerebro/evals"
)

// fakeCompleter returns a scripted reply and records the last request so tests
// can assert prompt construction. It never touches the network.
type fakeCompleter struct {
	reply CompletionResult
	err   error
	last  CompletionRequest
}

func (f *fakeCompleter) Complete(_ context.Context, req CompletionRequest) (CompletionResult, error) {
	f.last = req
	if f.err != nil {
		return CompletionResult{}, f.err
	}
	return f.reply, nil
}

func judgeTask() evals.TaskCase {
	return evals.TaskCase{ID: "a", Situation: "2+2?", Expected: "4", Critical: true}
}

func TestAIJudgePassAndFailAreParsed(t *testing.T) {
	pass := &fakeCompleter{reply: CompletionResult{Text: "VERDICT: PASS\nREASON: matches expected", CostCents: 3}}
	g, err := NewAIJudgeGrader(pass, evals.Grader{Type: evals.GraderAIJudge})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	ok, reason, cost, err := g.Grade(context.Background(), judgeTask(), "4")
	if err != nil || !ok {
		t.Fatalf("expected pass, got ok=%v err=%v", ok, err)
	}
	if reason != "matches expected" || cost != 3 {
		t.Fatalf("reason/cost wrong: %q %d", reason, cost)
	}

	fail := &fakeCompleter{reply: CompletionResult{Text: "VERDICT: FAIL\nREASON: wrong number"}}
	g2, _ := NewAIJudgeGrader(fail, evals.Grader{Type: evals.GraderAIJudge})
	ok, reason, _, _ = g2.Grade(context.Background(), judgeTask(), "5")
	if ok || reason != "wrong number" {
		t.Fatalf("expected fail with reason, got ok=%v reason=%q", ok, reason)
	}
}

func TestAIJudgeIsFailClosedOnGarbledReply(t *testing.T) {
	// A reply with no VERDICT line must fail, never pass by accident.
	garbled := &fakeCompleter{reply: CompletionResult{Text: "looks fine to me"}}
	g, _ := NewAIJudgeGrader(garbled, evals.Grader{Type: evals.GraderAIJudge})
	ok, reason, _, err := g.Grade(context.Background(), judgeTask(), "anything")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if ok {
		t.Fatal("garbled reply must not pass")
	}
	if !strings.Contains(reason, "did not return a PASS/FAIL verdict") {
		t.Fatalf("reason should flag missing verdict: %q", reason)
	}
}

func TestAIJudgeRubricReachesPrompt(t *testing.T) {
	fc := &fakeCompleter{reply: CompletionResult{Text: "VERDICT: PASS"}}
	g, _ := NewAIJudgeGrader(fc, evals.Grader{Type: evals.GraderAIJudge, Config: json.RawMessage(`{"rubric":"exact match only","model":"m1"}`)})
	if _, _, _, err := g.Grade(context.Background(), judgeTask(), "4"); err != nil {
		t.Fatalf("grade: %v", err)
	}
	if !strings.Contains(fc.last.Prompt, "exact match only") {
		t.Fatalf("rubric missing from prompt: %q", fc.last.Prompt)
	}
	if fc.last.Model != "m1" {
		t.Fatalf("model override lost: %q", fc.last.Model)
	}
}

func TestAIJudgePropagatesTransportError(t *testing.T) {
	boom := &fakeCompleter{err: errors.New("gateway down")}
	g, _ := NewAIJudgeGrader(boom, evals.Grader{Type: evals.GraderAIJudge})
	if _, _, _, err := g.Grade(context.Background(), judgeTask(), "4"); err == nil {
		t.Fatal("expected transport error to propagate")
	}
}

func TestPromptTargetSendsSituation(t *testing.T) {
	fc := &fakeCompleter{reply: CompletionResult{Text: "  4  ", CostCents: 2}}
	tgt, err := NewPromptTarget(fc, evals.TargetSpec{Kind: evals.TargetPrompt, System: "sys", Model: "m"})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	answer, cost, err := tgt.Run(context.Background(), judgeTask())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if answer != "  4  " || cost != 2 {
		t.Fatalf("answer/cost wrong: %q %d", answer, cost)
	}
	if fc.last.Prompt != "2+2?" || fc.last.System != "sys" || fc.last.Model != "m" {
		t.Fatalf("request not built from spec+task: %+v", fc.last)
	}
}

func TestNewTargetRouting(t *testing.T) {
	fc := &fakeCompleter{}
	if _, err := NewTarget(fc, evals.TargetSpec{Kind: evals.TargetPrompt}); err != nil {
		t.Fatalf("prompt target should build: %v", err)
	}
	for _, kind := range []string{evals.TargetAgent, evals.TargetSkill, evals.TargetWorkflow} {
		if _, err := NewTarget(fc, evals.TargetSpec{Kind: kind}); err == nil {
			t.Fatalf("kind %q should not yet be wired", kind)
		}
	}
	if _, err := NewTarget(fc, evals.TargetSpec{Kind: "bogus"}); err == nil {
		t.Fatal("unknown kind should error")
	}
}

func TestNewGraderRouting(t *testing.T) {
	fc := &fakeCompleter{}
	if _, err := NewGrader(fc, evals.Grader{Type: evals.GraderHardRule}); err != nil {
		t.Fatalf("hard_rule should build: %v", err)
	}
	if _, err := NewGrader(fc, evals.Grader{Type: evals.GraderAIJudge}); err != nil {
		t.Fatalf("ai_judge should build: %v", err)
	}
	if _, err := NewGrader(fc, evals.Grader{Type: "mystery"}); err == nil {
		t.Fatal("unknown grader should error")
	}
}

func TestGatewayCompleterRoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != gatewayMessagesPath {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer k" {
			t.Errorf("missing bearer auth: %q", r.Header.Get("Authorization"))
		}
		var req gatewayRequest
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("bad request body: %v", err)
		}
		if req.Model != "default-model" || len(req.Messages) != 1 || req.Messages[0].Content != "hi" {
			t.Errorf("request not built correctly: %+v", req)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"hel"},{"type":"text","text":"lo"}]}`))
	}))
	defer srv.Close()

	c, err := NewGatewayCompleter(srv.URL, "k", "default-model", srv.Client())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	res, err := c.Complete(context.Background(), CompletionRequest{Prompt: "hi"})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if res.Text != "hello" {
		t.Fatalf("text blocks not concatenated: %q", res.Text)
	}
}

func TestGatewayCompleterSurfacesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate limited"}`))
	}))
	defer srv.Close()

	c, _ := NewGatewayCompleter(srv.URL, "k", "m", srv.Client())
	_, err := c.Complete(context.Background(), CompletionRequest{Prompt: "hi"})
	if err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("expected 429 error, got %v", err)
	}
}

func TestNewGatewayCompleterValidates(t *testing.T) {
	if _, err := NewGatewayCompleter("", "k", "m", nil); err == nil {
		t.Fatal("empty base URL should error")
	}
	if _, err := NewGatewayCompleter("http://x", "", "m", nil); err == nil {
		t.Fatal("empty key should error")
	}
	if _, err := NewGatewayCompleter("http://x", "k", "", nil); err == nil {
		t.Fatal("empty model should error")
	}
}
