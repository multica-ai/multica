package main

import (
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/pkg/llm"
)

// TestParseLLMMaxRetriesAccepted pins the states an operator is allowed to
// configure. The nil-versus-override distinction is the whole point: it is what
// lets pkg/llm tell "unset, use the default" apart from "explicitly disabled".
func TestParseLLMMaxRetriesAccepted(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want *int // nil means "unset"
	}{
		{name: "unset", raw: "", want: nil},
		{name: "whitespace is unset", raw: "   ", want: nil},
		{name: "zero disables retries", raw: "0", want: ptr(0)},
		{name: "positive value", raw: "3", want: ptr(3)},
		{name: "surrounding whitespace is tolerated", raw: " 3 ", want: ptr(3)},
		{name: "the ceiling itself is valid", raw: "5", want: ptr(maxLLMRetriesLimit)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseLLMMaxRetries(tc.raw)
			if err != nil {
				t.Fatalf("parseLLMMaxRetries(%q) failed: %v", tc.raw, err)
			}
			switch {
			case tc.want == nil && got != nil:
				t.Fatalf("parseLLMMaxRetries(%q) = %d, want unset (nil)", tc.raw, got.Value())
			case tc.want != nil && got == nil:
				t.Fatalf("parseLLMMaxRetries(%q) = nil, want %d", tc.raw, *tc.want)
			case tc.want != nil && got.Value() != *tc.want:
				t.Fatalf("parseLLMMaxRetries(%q) = %d, want %d", tc.raw, got.Value(), *tc.want)
			}
		})
	}
}

// TestParseLLMMaxRetriesRejected is the half the original report asked for:
// every invalid value must fail configuration validation instead of being
// silently coerced back to a default that looks configured.
func TestParseLLMMaxRetriesRejected(t *testing.T) {
	for _, tc := range []struct {
		name        string
		raw         string
		wantMessage string
	}{
		{name: "non-numeric", raw: "two", wantMessage: "must be an integer"},
		{name: "float", raw: "1.5", wantMessage: "must be an integer"},
		{name: "trailing garbage", raw: "3x", wantMessage: "must be an integer"},
		{name: "negative", raw: "-1", wantMessage: "must not be negative"},
		{name: "above the ceiling", raw: "6", wantMessage: "must be at most 5"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseLLMMaxRetries(tc.raw)
			if err == nil {
				t.Fatalf("parseLLMMaxRetries(%q) accepted the value (= %v), want a validation error", tc.raw, got)
			}
			if got != nil {
				t.Fatalf("parseLLMMaxRetries(%q) returned %d alongside an error; a rejected value must not be usable", tc.raw, got.Value())
			}
			if !strings.Contains(err.Error(), tc.wantMessage) {
				t.Fatalf("parseLLMMaxRetries(%q) error = %q, want it to mention %q", tc.raw, err, tc.wantMessage)
			}
			if !strings.Contains(err.Error(), strings.TrimSpace(tc.raw)) {
				t.Fatalf("parseLLMMaxRetries(%q) error = %q, want it to echo the offending value", tc.raw, err)
			}
		})
	}
}

// TestParsedLLMMaxRetriesReachesTheClient closes the loop from env string to
// the budget the SDK will actually enforce — the wiring that did not exist at
// all before MUL-6364, which is why the field was configurable in name only.
func TestParsedLLMMaxRetriesReachesTheClient(t *testing.T) {
	parsed, err := parseLLMMaxRetries("0")
	if err != nil {
		t.Fatalf("parseLLMMaxRetries failed: %v", err)
	}

	budget := llm.New(llm.Config{APIKey: "k", MaxRetries: parsed}).RetryBudget()
	if budget.MaxRetries != 0 || budget.Source != llm.RetrySourceConfig {
		t.Fatalf("RetryBudget() = %+v, want retries disabled and sourced from config", budget)
	}

	unset, err := parseLLMMaxRetries("")
	if err != nil {
		t.Fatalf("parseLLMMaxRetries failed: %v", err)
	}
	budget = llm.New(llm.Config{APIKey: "k", MaxRetries: unset}).RetryBudget()
	if budget.MaxRetries != llm.DefaultMaxRetries || budget.Source != llm.RetrySourceDefault {
		t.Fatalf("RetryBudget() = %+v, want the default budget", budget)
	}
}

func ptr(n int) *int { return &n }
