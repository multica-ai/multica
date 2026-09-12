package main

import (
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/pkg/llm"
)

// TestParseLLMDisableThinkingAccepted pins the only values an operator may
// configure: unset/whitespace -> false, "true" -> true, "false" -> false.
func TestParseLLMDisableThinkingAccepted(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want bool
	}{
		{name: "unset", raw: "", want: false},
		{name: "whitespace is unset", raw: "   ", want: false},
		{name: "true enables", raw: "true", want: true},
		{name: "false is explicit off", raw: "false", want: false},
		{name: "surrounding whitespace is tolerated", raw: "  true  ", want: true},
		{name: "whitespace around false", raw: " false ", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseLLMDisableThinking(tc.raw)
			if err != nil {
				t.Fatalf("parseLLMDisableThinking(%q) failed: %v", tc.raw, err)
			}
			if got != tc.want {
				t.Fatalf("parseLLMDisableThinking(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

// TestParseLLMDisableThinkingRejected: strconv.ParseBool extras (1, t, TRUE,
// yes, ...) must NOT be accepted — only true/false, else fail the boot.
func TestParseLLMDisableThinkingRejected(t *testing.T) {
	for _, raw := range []string{"1", "0", "yes", "no", "TRUE", "True", "FALSE", "t", "f", "on", "x"} {
		t.Run("reject "+raw, func(t *testing.T) {
			got, err := parseLLMDisableThinking(raw)
			if err == nil {
				t.Fatalf("parseLLMDisableThinking(%q) accepted the value (= %v), want a validation error", raw, got)
			}
			if got {
				t.Fatalf("parseLLMDisableThinking(%q) returned true alongside an error", raw)
			}
			if !strings.Contains(err.Error(), strings.TrimSpace(raw)) {
				t.Fatalf("parseLLMDisableThinking(%q) error = %q, want it to echo the offending value", raw, err)
			}
		})
	}
}

// TestParsedLLMDisableThinkingReachesTheClient closes the loop from env string
// to the effective policy the client reports (and the startup log reads).
func TestParsedLLMDisableThinkingReachesTheClient(t *testing.T) {
	parsed, err := parseLLMDisableThinking("true")
	if err != nil {
		t.Fatalf("parseLLMDisableThinking failed: %v", err)
	}
	if got := llm.New(llm.Config{APIKey: "k", DisableThinking: parsed}).DisableThinking(); !got {
		t.Fatal("parsed true must reach the client as effective true")
	}

	unset, err := parseLLMDisableThinking("")
	if err != nil {
		t.Fatalf("parseLLMDisableThinking failed: %v", err)
	}
	if got := llm.New(llm.Config{APIKey: "k", DisableThinking: unset}).DisableThinking(); got {
		t.Fatal("unset must reach the client as effective false")
	}
}
