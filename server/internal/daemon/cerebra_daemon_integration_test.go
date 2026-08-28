package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"

	"github.com/multica-ai/multica/server/internal/cerebra"
	"github.com/multica-ai/multica/server/pkg/agent"
)

func TestCLIRoutingSimulation(t *testing.T) {
	classifier := cerebra.HeuristicClassifier{}
	policy := &cerebra.Policy{}
	session := cerebra.NewSessionStore(0)
	unavail := cerebra.NewUnavailabilityStore(0)
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	router := cerebra.NewRouter(classifier, policy, session, unavail, logger, nil)
	ctx := context.Background()

	// Available discovered OpenCode catalog
	openCodeCatalog := deriveRuntimeTierMap("opencode")
	runtimes := []cerebra.RuntimeEntry{
		{
			RuntimeID: "runtime-opencode-01",
			TierMap:   openCodeCatalog,
		},
	}

	testCases := []struct {
		Name            string
		Prompt          string
		WillUseMCPTools bool
		ExpectedTier    cerebra.Tier
		ExpectedModel   string
	}{
		{
			Name:            "1. Simple Question",
			Prompt:          "What is the structure of this project?",
			WillUseMCPTools: false,
			ExpectedTier:    cerebra.TierSimple,
			ExpectedModel:   "opencode/mimo-v2.5-free",
		},
		{
			Name:            "2. Debug / Coding Task",
			Prompt:          "Debug the database connection and fix the race condition.",
			WillUseMCPTools: false,
			ExpectedTier:    cerebra.TierStandard,
			ExpectedModel:   "opencode/nemotron-3.5-lightning-free",
		},
		{
			Name:            "3. Complex Architecture Task",
			Prompt:          "Architect and design a new multi-tenant sharding and migration engine.",
			WillUseMCPTools: false,
			ExpectedTier:    cerebra.TierHeavy,
			ExpectedModel:   "opencode/nemotron-3-ultra-free",
		},
		{
			Name:            "4. Simple Prompt with Active MCP Tools (Tool Floor Policy)",
			Prompt:          "Say hello in 3 words.",
			WillUseMCPTools: true,
			ExpectedTier:    cerebra.TierStandard,
			ExpectedModel:   "opencode/nemotron-3.5-lightning-free",
		},
	}

	fmt.Println("\n=========================================================================================")
	fmt.Printf("%-35s | %-10s | %-38s | %-12s\n", "TEST SCENARIO", "TIER", "DYNAMICALLY SELECTED MODEL", "RULE")
	fmt.Println("-----------------------------------------------------------------------------------------")

	for i, tc := range testCases {
		meta := cerebra.TaskMeta{
			TaskID:          fmt.Sprintf("task-cli-test-%02d", i+1),
			WillUseMCPTools: tc.WillUseMCPTools,
			IssueID:         fmt.Sprintf("issue-cli-test-%d", i+1),
			SessionID:       fmt.Sprintf("session-cli-test-%d", i+1),
		}

		result := router.Route(ctx, tc.Prompt, meta, runtimes, "default-fallback-model")
		dispatchedModel := routeBeforeDispatch(ctx, router, tc.Prompt, meta, runtimes, "default-fallback-model")

		fmt.Printf("%-35s | %-10s | %-38s | %-12s\n", tc.Name, result.Tier, dispatchedModel, result.MatchedRule)

		if result.Tier != tc.ExpectedTier {
			t.Errorf("[%s] Expected tier %s, got %s", tc.Name, tc.ExpectedTier, result.Tier)
		}
		if dispatchedModel != tc.ExpectedModel {
			t.Errorf("[%s] Expected model %s, got %s", tc.Name, tc.ExpectedModel, dispatchedModel)
		}
	}
	fmt.Println("=========================================================================================")

	// Test Codex catalog derivation
	codexMap := deriveRuntimeTierMap("codex")
	if codexMap[cerebra.TierSimple] == "" || codexMap[cerebra.TierStandard] == "" || codexMap[cerebra.TierHeavy] == "" {
		t.Errorf("expected complete tier map for codex, got %v", codexMap)
	}

	// Test Claude catalog derivation
	claudeMap := deriveRuntimeTierMap("claude")
	if claudeMap[cerebra.TierSimple] != "claude-3-5-haiku" || claudeMap[cerebra.TierStandard] != "claude-3-5-sonnet" || claudeMap[cerebra.TierHeavy] != "claude-3-opus" {
		t.Errorf("expected complete tier map for claude, got %v", claudeMap)
	}

	// Test Gemini catalog derivation
	geminiMap := deriveRuntimeTierMap("gemini")
	if geminiMap[cerebra.TierSimple] == "" || geminiMap[cerebra.TierStandard] == "" || geminiMap[cerebra.TierHeavy] == "" {
		t.Errorf("expected complete tier map for gemini, got %v", geminiMap)
	}

	// Test Ollama / local machine models derivation
	ollamaMap := deriveRuntimeTierMap("ollama")
	if ollamaMap[cerebra.TierSimple] == "" || ollamaMap[cerebra.TierStandard] == "" || ollamaMap[cerebra.TierHeavy] == "" {
		t.Errorf("expected complete tier map for ollama, got %v", ollamaMap)
	}

	// Test Dynamic Runtime Model Discovery (Simulating custom developer machine models)
	origListModels := listModels
	defer func() { listModels = origListModels }()

	listModels = func(ctx context.Context, providerType string, runtimeCmd agent.Command) (agent.Catalog, error) {
		return agent.Catalog{
			Models: []agent.Model{
				{ID: "ollama/llama3.2:1b-instruct-q4_0"},
				{ID: "ollama/qwen2.5-coder:14b-instruct-q4_k_m"},
				{ID: "ollama/deepseek-r1:32b-q4_k_m"},
			},
		}, nil
	}

	dynMap := deriveDynamicRuntimeTierMap(ctx, "ollama", agent.Command{})
	if dynMap[cerebra.TierSimple] != "ollama/llama3.2:1b-instruct-q4_0" {
		t.Errorf("expected dynamic Simple tier to pick llama3.2, got %q", dynMap[cerebra.TierSimple])
	}
	if dynMap[cerebra.TierStandard] != "ollama/qwen2.5-coder:14b-instruct-q4_k_m" {
		t.Errorf("expected dynamic Standard tier to pick qwen2.5-coder, got %q", dynMap[cerebra.TierStandard])
	}
	if dynMap[cerebra.TierHeavy] != "ollama/deepseek-r1:32b-q4_k_m" {
		t.Errorf("expected dynamic Heavy tier to pick deepseek-r1, got %q", dynMap[cerebra.TierHeavy])
	}
}
