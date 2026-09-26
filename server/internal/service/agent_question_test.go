package service

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseAgentQuestionsAcceptsClaudeCamelCase(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`[{
		"question": " Which flag name? ",
		"header": "Flag",
		"multiSelect": true,
		"options": [
			{"label": "--dry-run", "description": "Conventional"},
			{"label": "--preview"}
		]
	}]`)
	got, err := ParseAgentQuestions(raw)
	if err != nil {
		t.Fatalf("ParseAgentQuestions: %v", err)
	}
	if len(got.Questions) != 1 {
		t.Fatalf("questions = %d, want 1", len(got.Questions))
	}
	q := got.Questions[0]
	if q.Question != "Which flag name?" || q.Header != "Flag" || !q.MultiSelect {
		t.Fatalf("question = %+v", q)
	}
	if len(q.Options) != 2 || q.Options[0].Label != "--dry-run" || q.Options[0].Description != "Conventional" || q.Options[1].Description != "" {
		t.Fatalf("options = %+v", q.Options)
	}

	// The stored form is snake_case, whatever the daemon sent.
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(encoded), `"multi_select":true`) || strings.Contains(string(encoded), "multiSelect") {
		t.Fatalf("stored payload must be snake_case: %s", encoded)
	}

	// And the stored form round-trips.
	var stored AgentQuestionPayload
	if err := json.Unmarshal(encoded, &stored); err != nil {
		t.Fatalf("unmarshal stored: %v", err)
	}
	again, err := ParseAgentQuestions(json.RawMessage(mustQuestionsArray(t, encoded)))
	if err != nil || !again.Questions[0].MultiSelect {
		t.Fatalf("snake_case multi_select not honoured: %+v, %v", again, err)
	}
}

func mustQuestionsArray(t *testing.T, payload []byte) []byte {
	t.Helper()
	var p struct {
		Questions json.RawMessage `json:"questions"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	return p.Questions
}

func TestParseAgentQuestionsRejectsMalformedInput(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"empty":            ``,
		"empty array":      `[]`,
		"not an array":     `{"question": "x"}`,
		"no text":          `[{"question": "  ", "options": [{"label": "a"}]}]`,
		"option no label":  `[{"question": "q", "options": [{"label": ""}]}]`,
		"too many options": `[{"question": "q", "options": [` + strings.Repeat(`{"label":"a"},`, 12) + `{"label":"a"}]}]`,
	}
	for name, raw := range cases {
		if _, err := ParseAgentQuestions(json.RawMessage(raw)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}

	var many []string
	for i := 0; i < agentQuestionMaxQuestions+1; i++ {
		many = append(many, `{"question":"q","options":[{"label":"a"}]}`)
	}
	if _, err := ParseAgentQuestions(json.RawMessage("[" + strings.Join(many, ",") + "]")); err == nil {
		t.Error("too many questions: expected an error")
	}
}

func TestParseAgentQuestionsBoundsAndScrubsText(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("x", agentQuestionMaxQuestionRunes+50)
	raw, _ := json.Marshal([]map[string]any{{
		"question": long,
		"header":   "he\x00ader",
		"options":  []map[string]any{{"label": "a\x00b"}},
	}})
	got, err := ParseAgentQuestions(raw)
	if err != nil {
		t.Fatalf("ParseAgentQuestions: %v", err)
	}
	q := got.Questions[0]
	if len([]rune(q.Question)) != agentQuestionMaxQuestionRunes {
		t.Fatalf("question not bounded: %d runes", len([]rune(q.Question)))
	}
	if strings.ContainsRune(q.Header, 0) || strings.ContainsRune(q.Options[0].Label, 0) {
		t.Fatalf("NUL survived scrubbing: %+v", q)
	}
}

func TestAgentQuestionPayloadMarkdownIsReadableOnItsOwn(t *testing.T) {
	t.Parallel()

	p := AgentQuestionPayload{Questions: []AgentQuestion{
		{Question: "Which flag name?", Header: "Flag", Options: []AgentQuestionOption{
			{Label: "--dry-run", Description: "Conventional"},
			{Label: "--preview"},
		}},
		{Question: "Which sections?", MultiSelect: true, Options: []AgentQuestionOption{{Label: "Intro"}, {Label: "Outro"}}},
	}}
	md := p.Markdown()
	for _, want := range []string{
		"**Flag** — Which flag name?",
		"- **--dry-run** — Conventional",
		"- **--preview**\n",
		"**Which sections?**",
		"_(choose any that apply)_",
		"Answer in this thread",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q:\n%s", want, md)
		}
	}
}
