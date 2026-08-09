package commentquality

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/cerebro/duplicatecheck"
)

func newJudger(t *testing.T, handler http.HandlerFunc) *Judger {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &Judger{
		Config: duplicatecheck.GatewayConfig{BaseURL: server.URL, APIKey: "test", Model: "claude-haiku-4-5"},
		Client: server.Client(),
	}
}

func replyJSON(t *testing.T, w http.ResponseWriter, content string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	body := `{"choices":[{"message":{"content":` + mustJSONString(content) + `}}]}`
	_, _ = w.Write([]byte(body))
}

func mustJSONString(s string) string {
	// minimal JSON string encoding for the test payload
	out := []byte{'"'}
	for _, r := range s {
		switch r {
		case '"':
			out = append(out, '\\', '"')
		case '\\':
			out = append(out, '\\', '\\')
		case '\n':
			out = append(out, '\\', 'n')
		default:
			out = append(out, []byte(string(r))...)
		}
	}
	out = append(out, '"')
	return string(out)
}

func TestJudgePass(t *testing.T) {
	j := newJudger(t, func(w http.ResponseWriter, _ *http.Request) {
		replyJSON(t, w, `{"pass":true}`)
	})
	res, err := j.Judge(context.Background(), "rubric", "En klar konklusion. PR: https://x", "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Pass || res.Requirement != "" {
		t.Fatalf("res = %#v, want pass", res)
	}
}

func TestJudgeRejectCarriesRequirement(t *testing.T) {
	j := newJudger(t, func(w http.ResponseWriter, _ *http.Request) {
		// model sometimes wraps JSON in prose/fences — parseVerdict must still read it
		replyJSON(t, w, "```json\n{\"pass\":false,\"requirement\":\"Start med en konklusion.\"}\n```")
	})
	res, err := j.Judge(context.Background(), "rubric", "Her er status", "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	if res.Pass || res.Requirement != "Start med en konklusion." {
		t.Fatalf("res = %#v, want reject with requirement", res)
	}
}

func TestJudgeGatewayErrorIsError(t *testing.T) {
	j := newJudger(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	if _, err := j.Judge(context.Background(), "rubric", "text", "agent-1"); err == nil {
		t.Fatal("expected error on gateway 500")
	}
}

func TestJudgeUnconfiguredIsError(t *testing.T) {
	t.Setenv("FIRTAL_REGISTRY_URL", "")
	t.Setenv("FIRTAL_REGISTRY_KEY", "")
	j := &Judger{}
	if _, err := j.Judge(context.Background(), "rubric", "text", "agent-1"); err == nil {
		t.Fatal("expected error when gateway is not configured")
	}
}
