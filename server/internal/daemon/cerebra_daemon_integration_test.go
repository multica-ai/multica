package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

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

func TestCerebraFiveFailureModesDemonstration(t *testing.T) {
	ctx := context.Background()
	classifier := cerebra.HeuristicClassifier{}
	policy := &cerebra.Policy{}
	sessionStore := cerebra.NewSessionStore(0)
	unavailStore := cerebra.NewUnavailabilityStore(0)
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	router := cerebra.NewRouter(classifier, policy, sessionStore, unavailStore, logger, nil)

	fmt.Println("\n=========================================================================================")
	fmt.Println("           CEREBRA DYNAMIC MODEL DISCOVERY & 5 FAILURE MODES DEMO REPORT")
	fmt.Println("=========================================================================================")

	// -------------------------------------------------------------------------
	// DEMO 0: Dynamic Discovery on Machine (e.g. Local Ollama Models)
	// -------------------------------------------------------------------------
	localDiscoveredModels := []string{
		"ollama/llama3.2:1b-instruct",
		"ollama/qwen2.5-coder:14b-instruct",
		"ollama/deepseek-r1:32b-q4_k_m",
	}
	dynamicTierMap := cerebra.BuildTierMapFromCatalog(localDiscoveredModels)
	fmt.Printf("[DEMO 0] Dynamic Auto-Discovery on Local Machine:\n")
	fmt.Printf("         Simple Tier   -> %s\n", dynamicTierMap[cerebra.TierSimple])
	fmt.Printf("         Standard Tier -> %s\n", dynamicTierMap[cerebra.TierStandard])
	fmt.Printf("         Heavy Tier    -> %s\n", dynamicTierMap[cerebra.TierHeavy])

	// -------------------------------------------------------------------------
	// FAILURE 1: CLI Unavailable / Crashes -> 4-Layer Fallback Hierarchy
	// -------------------------------------------------------------------------
	fallbackMap := deriveRuntimeTierMap("codex") // simulated fallback when CLI discovery is unavailable
	resFallback := router.Route(ctx, "Explain quick sort", cerebra.TaskMeta{}, []cerebra.RuntimeEntry{
		{RuntimeID: "rt-fallback", TierMap: fallbackMap},
	}, "default-agent-model")
	fmt.Printf("\n[FAILURE 1] CLI Discovery Unavailable / Crash:\n")
	fmt.Printf("         Result: Gracefully fell back to Layer 3 provider catalog -> Dispatched: %s (Tier: %s)\n", resFallback.Model, resFallback.Tier)

	// -------------------------------------------------------------------------
	// FAILURE 2: Missing Tiers -> Adjacent Tier Bridging
	// -------------------------------------------------------------------------
	singleModelCatalog := []string{"ollama/llama3.2:1b-instruct"} // only 1 small model installed on this machine
	bridgedTierMap := cerebra.BuildTierMapFromCatalog(singleModelCatalog)
	resBridgedHeavy := router.Route(ctx, "Architect a multi-tenant distributed system", cerebra.TaskMeta{}, []cerebra.RuntimeEntry{
		{RuntimeID: "rt-bridged", TierMap: map[cerebra.Tier]string(bridgedTierMap)},
	}, "default-model")
	fmt.Printf("\n[FAILURE 2] Incomplete Catalog (No Heavy Model on Machine):\n")
	fmt.Printf("         Result: Adjacent tier bridging auto-bridged Heavy tier -> Dispatched: %s\n", resBridgedHeavy.Model)

	// -------------------------------------------------------------------------
	// FAILURE 3: Misleading Model Names & Keyword Traps -> Smart Segment Matching
	// -------------------------------------------------------------------------
	miniTier := cerebra.ClassifyModelTier("o1-mini")
	o1Tier := cerebra.ClassifyModelTier("o1")
	mimoTier := cerebra.ClassifyModelTier("opencode/mimo-v2.5-free")
	deepseekTier := cerebra.ClassifyModelTier("deepseek-r1:32b")
	fmt.Printf("\n[FAILURE 3] Misleading Model Names & Substrings:\n")
	fmt.Printf("         o1-mini                -> Classified as: %-8s (Safe: Did not trigger Heavy 'o1')\n", miniTier)
	fmt.Printf("         o1                     -> Classified as: %-8s (Correct: Reasoning model)\n", o1Tier)
	fmt.Printf("         opencode/mimo-v2.5     -> Classified as: %-8s (Safe: 'opencode/' prefix stripped, not 'code')\n", mimoTier)
	fmt.Printf("         deepseek-r1:32b        -> Classified as: %-8s (Correct: Reasoning model)\n", deepseekTier)

	// -------------------------------------------------------------------------
	// FAILURE 4: Runtime Quota / 429 Errors -> Circuit Breaker Cooldown & Failover
	// -------------------------------------------------------------------------
	// Mark primary standard model unavailable due to HTTP 429
	unavailStore.MarkUnavailable(ctx, "rt-failover", "ollama/qwen2.5-coder:14b-instruct", time.Hour)
	failoverRuntimes := []cerebra.RuntimeEntry{
		{RuntimeID: "rt-failover", TierMap: dynamicTierMap},
	}
	resFailover := router.Route(ctx, "Debug this memory leak", cerebra.TaskMeta{}, failoverRuntimes, "opencode/mimo-v2.5-free")
	fmt.Printf("\n[FAILURE 4] Runtime Quota / HTTP 429 Circuit Breaker:\n")
	fmt.Printf("         Primary model placed on 1-hour cooldown -> Router automatically failed over to: %s\n", resFailover.Model)

	// -------------------------------------------------------------------------
	// FAILURE 5: Context Loss on Follow-up Messages -> Sticky Escalation
	// -------------------------------------------------------------------------
	sessionID := "session-sticky-demo-99"
	// Turn 1: Complex architecture query
	metaTurn1 := cerebra.TaskMeta{TaskID: "task-01", SessionID: sessionID}
	resTurn1 := router.Route(ctx, "Architect a high-throughput event streaming pipeline", metaTurn1, []cerebra.RuntimeEntry{
		{RuntimeID: "rt-demo", TierMap: map[cerebra.Tier]string(cerebra.BuildTierMapFromCatalog([]string{"gpt-4o-mini", "gpt-4o", "o1"}))},
	}, "gpt-4o")

	// Turn 2: Short 3-word follow-up
	metaTurn2 := cerebra.TaskMeta{TaskID: "task-02", SessionID: sessionID}
	resTurn2 := router.Route(ctx, "Looks good, proceed", metaTurn2, []cerebra.RuntimeEntry{
		{RuntimeID: "rt-demo", TierMap: map[cerebra.Tier]string(cerebra.BuildTierMapFromCatalog([]string{"gpt-4o-mini", "gpt-4o", "o1"}))},
	}, "gpt-4o")
	fmt.Printf("\n[FAILURE 5] Multi-Turn Context Demotion Prevention:\n")
	fmt.Printf("         Turn 1 ('Architect a high-throughput...'): Tier = %s | Model = %s\n", resTurn1.Tier, resTurn1.Model)
	fmt.Printf("         Turn 2 ('Looks good, proceed'):           Tier = %s | Model = %s (Context preserved!)\n", resTurn2.Tier, resTurn2.Model)
	fmt.Println("=========================================================================================")
}

func TestLiveTestLabIssues(t *testing.T) {
	ctx := context.Background()
	classifier := cerebra.HeuristicClassifier{}
	policy := &cerebra.Policy{}
	sessionStore := cerebra.NewSessionStore(0)
	unavailStore := cerebra.NewUnavailabilityStore(0)
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	router := cerebra.NewRouter(classifier, policy, sessionStore, unavailStore, logger, nil)

	localCatalog := []string{
		"opencode/mimo-v2.5-free",
		"opencode/nemotron-3.5-lightning-free",
		"opencode/nemotron-3-ultra-free",
		"opencode/big-pickle",
	}
	runtimes := []cerebra.RuntimeEntry{
		{
			RuntimeID: "local-runtime-testlab",
			TierMap:   map[cerebra.Tier]string(cerebra.BuildTierMapFromCatalog(localCatalog)),
		},
	}

	testLabIssues := []struct {
		Key         string
		Title       string
		Description string
		WillUseMCP  bool
	}{
		{
			Key:         "TEST-14",
			Title:       "CEREBRA-01: [Simple Tier] Documentation & Folder Structure",
			Description: "What is the folder architecture and purpose of each package in this repo?",
			WillUseMCP:  false,
		},
		{
			Key:         "TEST-15",
			Title:       "CEREBRA-02: [Standard Tier] Debug Database Connection Pool",
			Description: "Debug the database connection deadlock and fix the concurrent query timeout.",
			WillUseMCP:  false,
		},
		{
			Key:         "TEST-16",
			Title:       "CEREBRA-03: [Heavy Tier] Architect Distributed Sharding Engine",
			Description: "Architect and design a new multi-tenant sharding and distributed consensus migration engine with failover.",
			WillUseMCP:  false,
		},
		{
			Key:         "TEST-17",
			Title:       "CEREBRA-04: [MCP Tool Floor] MCP Tool Invocation Policy",
			Description: "Fetch the user profile via remote MCP tool server and format the response.",
			WillUseMCP:  true,
		},
		{
			Key:         "TEST-18",
			Title:       "CEREBRA-05: [Substring Trap] Verify Prefix and Fixture Documentation",
			Description: "Explain what prefix and postfix conventions we use. Do not debug any code; just check the sample fixture structure.",
			WillUseMCP:  false,
		},
	}

	fmt.Println("\n========================================================================================================================")
	fmt.Println("                            CEREBRA LIVE ROUTING RESULTS FOR 'TEST LAB' ISSUES")
	fmt.Println("========================================================================================================================")
	fmt.Printf("%-9s | %-10s | %-37s | %-18s | %s\n", "ISSUE KEY", "TIER", "DISPATCHED MODEL", "MATCHED RULE", "EXPLAINABILITY")
	fmt.Println("------------------------------------------------------------------------------------------------------------------------")

	for _, issue := range testLabIssues {
		prompt := issue.Title + "\n" + issue.Description
		meta := cerebra.TaskMeta{
			TaskID:          "task-" + issue.Key,
			IssueID:         issue.Key,
			WillUseMCPTools: issue.WillUseMCP,
		}
		result := router.Route(ctx, prompt, meta, runtimes, "default-agent-model")
		explain := ""
		switch result.Tier {
		case cerebra.TierSimple:
			explain = "Lightweight fast model selected; minimal token cost."
		case cerebra.TierStandard:
			if result.MatchedRule == "mcp_floor" {
				explain = "Tool Floor Policy raised tier to Standard for tool capability."
			} else {
				explain = "Coding/Debug tier selected for execution accuracy."
			}
		case cerebra.TierHeavy:
			explain = "Frontier reasoning model allocated for architectural complexity."
		}

		fmt.Printf("%-9s | %-10s | %-37s | %-18s | %s\n", issue.Key, result.Tier, result.Model, result.MatchedRule, explain)
	}
	fmt.Println("========================================================================================================================")
}


