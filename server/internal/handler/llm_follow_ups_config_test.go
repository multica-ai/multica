package handler

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/analytics"
)

// TestNewWiresFollowUpQuestionsSwitch pins MULTICA_LLM_DISABLE_FOLLOW_UP_QUESTIONS
// (GitHub #9578): the switch removes only the follow-up questions generator,
// so the suggestion pass takes the same "unavailable" path as an unconfigured
// LLM layer, while the shared client — and with it chat auto-titling — stays
// enabled.
func TestNewWiresFollowUpQuestionsSwitch(t *testing.T) {
	configured := Config{LLMAPIKey: "test-key", LLMBaseURL: "https://llm.example.invalid/v1"}

	t.Run("default keeps follow-up questions on", func(t *testing.T) {
		h := New(nil, nil, nil, nil, nil, nil, nil, analytics.NoopClient{}, configured)
		if h.TaskService.QuickActions == nil || !h.TaskService.QuickActions.Enabled() {
			t.Fatal("QuickActions should be wired to the enabled LLM client by default")
		}
	})

	t.Run("switch turns off follow-up questions only", func(t *testing.T) {
		cfg := configured
		cfg.LLMDisableFollowUps = true
		h := New(nil, nil, nil, nil, nil, nil, nil, analytics.NoopClient{}, cfg)
		if h.TaskService.QuickActions != nil {
			t.Fatal("QuickActions should be nil when follow-up questions are disabled")
		}
		if h.LLM == nil || !h.LLM.Enabled() {
			t.Fatal("the LLM layer backing chat auto-titling must stay enabled")
		}
	})
}
