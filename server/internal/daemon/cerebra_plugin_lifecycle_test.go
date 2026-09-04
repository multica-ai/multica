package daemon

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/cerebra"
)

// TestCerebraPluginLifecycle_BeforeAndAfterInstall tests the exact user scenario:
// 1. Initial State (Before Plugin): Agent is created with default model "claude-3-5-sonnet"
//    and runs an issue task. Because Cerebra plugin is NOT installed, model routing does NOT touch it.
// 2. Plugin Installed: Workspace installs "cerebra" (adding "cerebra-routing" skill to agents).
// 3. Post-Install State:
//    - A simple task routes to TierSimple (e.g. haiku/mini/mimo).
//    - An architecture task routes to TierHeavy (e.g. opus/ultra/o1).
//    - Multi-turn chat follow-up retains TierHeavy (sticky session pinning).
//    - Routing decisions are recorded for auditability.
func TestCerebraPluginLifecycle_BeforeAndAfterInstall(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	classifier := cerebra.HeuristicClassifier{}
	policy := &cerebra.Policy{}
	sessionStore := cerebra.NewSessionStore(2 * time.Hour)
	unavailStore := cerebra.NewUnavailabilityStore(time.Hour)

	var mu sync.Mutex
	var recordedLogs []cerebra.RoutingLogEntry
	logFn := func(_ context.Context, entry cerebra.RoutingLogEntry) {
		mu.Lock()
		defer mu.Unlock()
		recordedLogs = append(recordedLogs, entry)
	}

	router := cerebra.NewRouter(classifier, policy, sessionStore, unavailStore, logger, logFn)

	// Available runtime models for the agent (e.g. Anthropic Claude models)
	runtimeCatalog := []string{
		"claude-3-5-haiku",
		"claude-3-5-sonnet",
		"claude-3-opus",
	}
	tierMap := cerebra.BuildTierMapFromCatalog(runtimeCatalog)
	runtimes := []cerebra.RuntimeEntry{
		{RuntimeID: "rt-claude-prod", TierMap: tierMap},
	}

	// -------------------------------------------------------------------------
	// Phase 1: BEFORE Cerebra Plugin Install
	// -------------------------------------------------------------------------
	// Agent has default model "claude-3-5-sonnet" and only standard skills.
	taskBeforeInstall := Task{
		ID:          "task-01-before-install",
		WorkspaceID: "ws-lifecycle-demo",
		IssueID:     "issue-101",
		RuntimeID:   "rt-claude-prod",
		Agent: &AgentData{
			Name: "DevBot",
			Skills: []SkillData{
				{Name: "git-commit"},
				{Name: "code-review"},
			},
		},
	}

	prompt1 := "What is the project folder structure?"
	defaultModel := "claude-3-5-sonnet"

	// Helper function replicating daemon check:
	isCerebraActive := func(task Task) bool {
		if task.Agent != nil {
			for _, sk := range task.Agent.Skills {
				if sk.Name == "cerebra-routing" {
					return true
				}
			}
		}
		return false
	}

	model1 := defaultModel
	if isCerebraActive(taskBeforeInstall) {
		meta := cerebra.TaskMeta{TaskID: taskBeforeInstall.ID, IssueID: taskBeforeInstall.IssueID}
		model1 = routeBeforeDispatch(ctx, router, prompt1, meta, runtimes, defaultModel)
	}

	if isCerebraActive(taskBeforeInstall) {
		t.Fatalf("expected Cerebra to be INACTIVE before plugin install")
	}
	if model1 != "claude-3-5-sonnet" {
		t.Fatalf("expected model to remain default 'claude-3-5-sonnet' before plugin install, got %s", model1)
	}
	if len(recordedLogs) != 0 {
		t.Fatalf("expected 0 routing logs before plugin install, got %d", len(recordedLogs))
	}
	t.Log("✅ Phase 1 Passed: Before plugin install, agent uses default model without Cerebra override")

	// -------------------------------------------------------------------------
	// Phase 2: INSTALL PLUGIN (multica install cerebra)
	// -------------------------------------------------------------------------
	// Installing cerebra adds the 'cerebra-routing' skill to the workspace & agent.
	taskAfterInstallSimple := Task{
		ID:          "task-02-after-install",
		WorkspaceID: "ws-lifecycle-demo",
		IssueID:     "issue-102",
		RuntimeID:   "rt-claude-prod",
		Agent: &AgentData{
			Name: "DevBot",
			Skills: []SkillData{
				{Name: "git-commit"},
				{Name: "code-review"},
				{Name: "cerebra-routing"}, // added by multica install cerebra
			},
		},
	}

	if !isCerebraActive(taskAfterInstallSimple) {
		t.Fatalf("expected Cerebra to be ACTIVE after plugin install")
	}

	// -------------------------------------------------------------------------
	// Phase 3: AFTER Plugin Install - Simple Issue
	// -------------------------------------------------------------------------
	metaSimple := cerebra.TaskMeta{
		TaskID:  taskAfterInstallSimple.ID,
		IssueID: taskAfterInstallSimple.IssueID,
	}
	model2 := defaultModel
	if isCerebraActive(taskAfterInstallSimple) {
		model2 = routeBeforeDispatch(ctx, router, prompt1, metaSimple, runtimes, defaultModel)
	}

	if model2 != "claude-3-5-haiku" {
		t.Fatalf("expected simple prompt to route to 'claude-3-5-haiku', got %s", model2)
	}
	t.Logf("✅ Phase 2 Passed: Simple task dynamically routed to lightweight model: %s", model2)

	// -------------------------------------------------------------------------
	// Phase 4: AFTER Plugin Install - Architecture Issue
	// -------------------------------------------------------------------------
	sessionID := "session-arch-401"
	taskAfterInstallArch := Task{
		ID:            "task-03-arch",
		WorkspaceID:   "ws-lifecycle-demo",
		IssueID:       "issue-103",
		ChatSessionID: sessionID,
		RuntimeID:     "rt-claude-prod",
		Agent: &AgentData{
			Name: "DevBot",
			Skills: []SkillData{
				{Name: "cerebra-routing"},
			},
		},
	}

	promptArch := "Architect a distributed multi-region consensus engine with partition balancing."
	metaArch := cerebra.TaskMeta{
		TaskID:    taskAfterInstallArch.ID,
		IssueID:   taskAfterInstallArch.IssueID,
		SessionID: taskAfterInstallArch.ChatSessionID,
	}

	model3 := defaultModel
	if isCerebraActive(taskAfterInstallArch) {
		model3 = routeBeforeDispatch(ctx, router, promptArch, metaArch, runtimes, defaultModel)
	}

	if model3 != "claude-3-opus" {
		t.Fatalf("expected architectural prompt to route to 'claude-3-opus', got %s", model3)
	}
	t.Logf("✅ Phase 3 Passed: Architecture task dynamically routed to heavy frontier model: %s", model3)

	// -------------------------------------------------------------------------
	// Phase 5: Multi-Turn Chat on Same Issue (Sticky Escalation)
	// -------------------------------------------------------------------------
	promptFollowup := "Looks good, proceed with implementation."
	taskFollowup := taskAfterInstallArch
	taskFollowup.ID = "task-04-followup"

	metaFollowup := cerebra.TaskMeta{
		TaskID:    taskFollowup.ID,
		IssueID:   taskFollowup.IssueID,
		SessionID: taskFollowup.ChatSessionID,
	}

	model4 := defaultModel
	if isCerebraActive(taskFollowup) {
		model4 = routeBeforeDispatch(ctx, router, promptFollowup, metaFollowup, runtimes, defaultModel)
	}

	if model4 != "claude-3-opus" {
		t.Fatalf("expected follow-up to retain pinned heavy model 'claude-3-opus', got %s", model4)
	}
	t.Logf("✅ Phase 4 Passed: Multi-turn chat session retained heavy tier: %s", model4)

	// Wait up to 1 second for asynchronous log writes
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		count := len(recordedLogs)
		mu.Unlock()
		if count >= 3 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	finalCount := len(recordedLogs)
	mu.Unlock()

	if finalCount < 3 {
		t.Fatalf("expected at least 3 routing log records, got %d", finalCount)
	}
	t.Logf("✅ Phase 5 Passed: Routing audit logs written asynchronously (%d entries recorded)", finalCount)
}
