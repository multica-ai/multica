import { createTestAgentSessionEngine } from "../../../shared/testing/createTestAgentSessionEngine";
import { createTestPromptOperations } from "../../../shared/testing/createTestPromptOperations";
import { act, renderHook, waitFor } from "@testing-library/react";
import {
  type AgentActivityRailPlacement,
} from "@tutti-os/agent-activity-core";
import { describe, expect, it, vi } from "vitest";
import { createLocalAgentGUIAgentTarget } from "../../../agentTargets";
import type { AgentGUIRuntime } from "../../../agentActivityRuntime";
import type { AgentHostUserProject } from "../../../host/agentHostApi";
import type { AgentSessionComposerSettings } from "../../../shared/agentSessionTypes";
import type { AgentGUINodeData } from "../../../types";
import type { AgentGUIConversationSummary } from "../model/agentGuiConversationModel";
import type { AgentComposerDraft } from "../model/agentGuiNodeTypes";
import type { AgentGUIComposerTargetData } from "./agentGuiController.composerPresentation";
import { nodeDefaultDraftKey } from "./agentGuiController.composerHelpers";
import { requestAgentGUINewConversation } from "./agentGuiNewConversationRequest";
import { useAgentGUIActivation } from "./useAgentGUIActivation";
import { useAgentGUIConversationHome } from "./useAgentGUIConversationHome";
import { useAgentGUINewConversationActivation } from "./useAgentGUINewConversationActivation";
import { useAgentGUISubmitInteractionActions } from "./useAgentGUISubmitInteractionActions";

describe("P0 new-conversation placement scenarios", () => {
  it("does not persist a provisional session before canonical confirmation", async () => {
    const scenario = renderNewConversationScenario({
      activeConversation: null,
      initialHomeProjectPath: null,
      userProjects: []
    });

    act(() => scenario.requestNewConversation());
    scenario.persistActiveConversation.mockClear();
    act(() =>
      scenario.submitPrompt([{ type: "text", text: "start a new chat" }])
    );

    await scenario.waitForActivation();
    expect(scenario.persistActiveConversation).not.toHaveBeenCalled();
  });

  it("creates a conversations-scoped activation from an active Chats session", async () => {
    const previousSessionCwd = "/Users/example/Documents/tutti/session-current";
    const scenario = renderNewConversationScenario({
      activeConversation: conversationSummary({
        cwd: previousSessionCwd,
        railSectionKey: "conversations"
      }),
      initialHomeProjectPath: previousSessionCwd,
      userProjects: []
    });

    act(() => scenario.requestNewConversation());
    act(() =>
      scenario.submitPrompt([{ type: "text", text: "start a new chat" }])
    );

    const activation = await scenario.waitForActivation();
    expect(scenario.activateSession).toHaveBeenCalledTimes(1);
    expect(activation).toMatchObject({
      cwd: "",
      initialContent: [{ type: "text", text: "start a new chat" }],
      railPlacement: {
        version: 1,
        kind: "conversations",
        sectionKey: "conversations"
      }
    });
    expect(activation.cwd).not.toBe(previousSessionCwd);
  });

  it("keeps canonical project placement when starting from a project session", async () => {
    const projectPath = "/workspace/project-a";
    const sectionKey = "project:workspace-1:/workspace/project-a";
    const scenario = renderNewConversationScenario({
      activeConversation: conversationSummary({
        cwd: projectPath,
        railSectionKey: sectionKey
      }),
      initialHomeProjectPath: null,
      userProjects: [
        {
          id: "project-a",
          label: "Project A",
          path: projectPath,
          pinnedAtUnixMs: 0,
          sectionKey
        }
      ]
    });

    act(() => scenario.requestNewConversation());
    act(() =>
      scenario.submitPrompt([{ type: "text", text: "continue in project" }])
    );

    expect(await scenario.waitForActivation()).toMatchObject({
      cwd: projectPath,
      initialContent: [{ type: "text", text: "continue in project" }],
      railPlacement: {
        version: 1,
        kind: "project",
        projectPath,
        sectionKey
      }
    });
  });

  it("fails closed when a remembered saver mode outlives the developer flag", async () => {
    const scenario = renderNewConversationScenario({
      activeConversation: null,
      codexSaverModeEntryEnabled: false,
      rtkSaverModeEntryEnabled: false,
      initialHomeProjectPath: null,
      rememberedSettings: { codexSaverMode: true, rtkSaverMode: true },
      userProjects: []
    });

    act(() => scenario.requestNewConversation());
    act(() => scenario.submitPrompt([{ type: "text", text: "start safely" }]));

    expect(await scenario.waitForActivation()).toMatchObject({
      settings: { codexSaverMode: false, rtkSaverMode: false }
    });
  });

  it("keeps remembered saver mode when both entry and target support it", async () => {
    const scenario = renderNewConversationScenario({
      activeConversation: null,
      codexSaverModeEntryEnabled: true,
      initialHomeProjectPath: null,
      rememberedSettings: { codexSaverMode: true, rtkSaverMode: true },
      userProjects: []
    });

    act(() => scenario.requestNewConversation());
    act(() =>
      scenario.submitPrompt([{ type: "text", text: "delegate when useful" }])
    );

    expect(await scenario.waitForActivation()).toMatchObject({
      settings: { codexSaverMode: true, rtkSaverMode: true }
    });
  });

  it("uses the authoritative remembered saver mode when no local draft exists", async () => {
    const scenario = renderNewConversationScenario({
      activeConversation: null,
      authoritativeSettings: { codexSaverMode: true },
      codexSaverModeEntryEnabled: true,
      initialHomeProjectPath: null,
      userProjects: []
    });

    act(() => scenario.requestNewConversation());
    act(() =>
      scenario.submitPrompt([{ type: "text", text: "use remembered mode" }])
    );

    expect(await scenario.waitForActivation()).toMatchObject({
      settings: { codexSaverMode: true }
    });
  });

  it("keeps an explicit local saver opt-out above authoritative defaults", async () => {
    const scenario = renderNewConversationScenario({
      activeConversation: null,
      authoritativeSettings: { codexSaverMode: true },
      codexSaverModeEntryEnabled: true,
      initialHomeProjectPath: null,
      rememberedSettings: { codexSaverMode: false },
      userProjects: []
    });

    act(() => scenario.requestNewConversation());
    act(() =>
      scenario.submitPrompt([{ type: "text", text: "keep saver disabled" }])
    );

    expect(await scenario.waitForActivation()).toMatchObject({
      settings: { codexSaverMode: false }
    });
  });

  it("keeps the logical project when the active session runs in an isolated worktree", async () => {
    const projectPath = "/workspace/project-a";
    const sectionKey = "project:workspace-1:/workspace/project-a";
    const scenario = renderNewConversationScenario({
      activeConversation: conversationSummary({
        cwd: "/state/task-worktrees/issue/task-run",
        railSectionKey: sectionKey
      }),
      initialHomeProjectPath: null,
      userProjects: [
        {
          id: "project-a",
          label: "Project A",
          path: projectPath,
          pinnedAtUnixMs: 0,
          sectionKey
        }
      ]
    });

    act(() => scenario.requestNewConversation());
    act(() =>
      scenario.submitPrompt([{ type: "text", text: "continue from worktree" }])
    );

    expect(await scenario.waitForActivation()).toMatchObject({
      cwd: projectPath,
      railPlacement: {
        version: 1,
        kind: "project",
        projectPath,
        sectionKey
      }
    });
  });

  it("preserves an explicit project selection already made on Home", async () => {
    const projectPath = "/workspace/project-a";
    const sectionKey = "project:workspace-1:/workspace/project-a";
    const scenario = renderNewConversationScenario({
      activeConversation: null,
      initialHomeProjectPath: projectPath,
      userProjects: [
        {
          id: "project-a",
          label: "Project A",
          path: projectPath,
          pinnedAtUnixMs: 0,
          sectionKey
        }
      ]
    });

    act(() => scenario.requestNewConversation());
    act(() =>
      scenario.submitPrompt([{ type: "text", text: "start in my project" }])
    );

    expect(await scenario.waitForActivation()).toMatchObject({
      cwd: projectPath,
      initialContent: [{ type: "text", text: "start in my project" }],
      railPlacement: {
        version: 1,
        kind: "project",
        projectPath,
        sectionKey
      }
    });
  });
});

function renderNewConversationScenario(input: {
  activeConversation: AgentGUIConversationSummary | null;
  authoritativeSettings?: AgentSessionComposerSettings;
  codexSaverModeEntryEnabled?: boolean;
  rtkSaverModeEntryEnabled?: boolean;
  initialHomeProjectPath: string | null;
  rememberedSettings?: AgentSessionComposerSettings;
  userProjects: AgentHostUserProject[];
}) {
  const sessionEngine = createTestAgentSessionEngine("workspace-1");
  const activateSession = vi.spyOn(sessionEngine, "activateSession");
  const target = createLocalAgentGUIAgentTarget("codex");
  const dataRef: { current: AgentGUINodeData } = {
    current: {
      agentTargetId: target.agentTargetId,
      lastActiveAgentSessionId: input.activeConversation?.id ?? null,
      provider: "codex" as const
    }
  };
  const targetData: AgentGUIComposerTargetData = {
    agentTargetId: target.agentTargetId ?? null,
    data: dataRef.current,
    provider: "codex",
    targetId: target.targetId
  };
  const activeConversationIdRef = {
    current: input.activeConversation?.id ?? null
  };
  const isComposerHomeRef = { current: input.activeConversation === null };
  const selectedProjectPathRef = {
    current: input.initialHomeProjectPath
  };
  const draftByScopeKeyRef = {
    current: {} as Record<string, AgentComposerDraft>
  };
  const submittedDraftSnapshotsRef = { current: {} };
  const agentActivityRuntime = {
    prompts: createTestPromptOperations()
  } as unknown as AgentGUIRuntime;
  const setDraftByScopeKey = vi.fn();
  const persistActiveConversation = vi.fn();
  const conversationsRef = {
    current: input.activeConversation ? [input.activeConversation] : []
  };
  const conversationListQuery = {
    provider: "codex" as const,
    sessionOrigin: "local",
    userId: "user-1",
    workspaceId: "workspace-1"
  };

  const { result } = renderHook(() => {
    const activation = useAgentGUIActivation({
      engine: sessionEngine,
      getErrorMessage: (error) => String(error),
      workspaceId: "workspace-1"
    });
    const { createConversation } = useAgentGUIConversationHome({
      activeConversationId: activeConversationIdRef.current,
      activeConversationIdRef,
      activePendingActivation: null,
      agentActivityRuntime,
      composerAppendRequest: null,
      composerTargetDataFromProviderTarget: () => targetData,
      conversationFilterRef: { current: { kind: "all" } },
      currentProvider: "codex",
      dataRef,
      defaultAgentTargetId: target.agentTargetId ?? null,
      draftByScopeKeyRef,
      handledComposerAppendSequenceRef: { current: null },
      handledPrefillPromptSequenceRef: { current: null },
      isComposerHomeRef,
      isExplicitAgentGUIAgentTarget: () => true,
      loadDraftComposerOptions: vi.fn(),
      normalizedExplicitProviderTargets: [target],
      normalizedProviderTargets: [target],
      onDataChangeRef: {
        current: (updater) => {
          dataRef.current = updater(dataRef.current);
        }
      },
      persistActiveConversation,
      prefillPromptRequest: null,
      reportActiveConversationCleared: vi.fn(),
      selectedComposerTargetDataRef: { current: targetData },
      selectedProjectPathRef,
      setActiveConversationId: vi.fn(),
      setConversationFilter: vi.fn(),
      setDetailError: vi.fn(),
      setDraftByScopeKey,
      setHomeComposerTargetOverride: vi.fn(),
      setIntent: vi.fn(),
      setIsComposerHome: vi.fn(),
      setIsLoadingMessages: vi.fn(),
      setSelectedProjectPath: vi.fn(),
      shouldUseStaticProviderTargets: true,
      submitPrefillPrompt: vi.fn(),
      unactivate: async () => undefined,
      workspaceId: "workspace-1"
    });
    const startConversation = useAgentGUINewConversationActivation({
      activation,
      activeConversationIdRef,
      activeSessionState: null,
      agentActivityRuntime,
      agentTargetsProvidedRef: { current: true },
      conversationListQuery,
      conversationsRef,
      codexSaverModeEntryEnabled: input.codexSaverModeEntryEnabled ?? true,
      rtkSaverModeEntryEnabled: input.rtkSaverModeEntryEnabled ?? true,
      currentUserId: "user-1",
      data: dataRef.current,
      defaultReasoningEffort: "high",
      draftByScopeKeyRef,
      draftSettingsBySessionIdRef: {
        current: input.rememberedSettings
          ? {
              [nodeDefaultDraftKey("codex", target.agentTargetId)]:
                input.rememberedSettings
            }
          : {}
      },
      getCachedComposerOptions: () => ({
        behavior: {
          collapseModelOptionsToLatest: false,
          modelOptionsAuthoritative: false,
          planModeExclusiveWithPermissionMode: false,
          prewarmDraftSession: false,
          refreshModelOptionsAfterSettings: false
        },
        capabilities: null,
        codexSaverModeSupported: true,
        rtkSaverModeSupported: true,
        effectiveSettings: input.authoritativeSettings,
        loadedAtUnixMs: 1,
        models: [],
        provider: "codex",
        reasoningEfforts: [],
        skills: [],
        speeds: []
      }),
      isComposerHomeRef,
      isConversationStale: () => false,
      isCreatingConversationRef: { current: false },
      isCurrentConversation: () => false,
      onDataChangeRef: {
        current: (updater) => {
          dataRef.current = updater(dataRef.current);
        }
      },
      requestRailReveal: vi.fn(),
      selectedAgentTargetIsExplicitRef: { current: true },
      selectedAgentTargetRef: { current: target },
      selectedComposerTargetDataRef: { current: targetData },
      selectedProjectPathRef,
      sessionEngine,
      setActiveConversationId: vi.fn(),
      setDetailError: vi.fn(),
      setIntent: vi.fn(),
      setIsComposerHome: vi.fn(),
      setIsLoadingMessages: vi.fn(),
      submittedDraftSnapshotsRef,
      userProjectsRef: { current: input.userProjects },
      workspaceId: "workspace-1"
    });
    const { submitPrompt } = useAgentGUISubmitInteractionActions({
      activation,
      activeConversationId: activeConversationIdRef.current,
      activeConversationIdRef,
      activeEngineActiveTurn: null,
      activeEnginePendingInteractions: [],
      agentActivityRuntime,
      conversationListQuery,
      conversationsRef,
      dataRef,
      draftByScopeKeyRef,
      executePromptRef: { current: vi.fn() },
      isComposerHomeRef,
      isCurrentConversation: () => false,
      isRespondingToInteraction: false,
      isSessionMarkedNonResumable: () => false,
      persistActiveConversation: vi.fn(),
      promptImagesSupported: true,
      sessionEngine,
      setActiveConversationId: vi.fn(),
      setDetailError: vi.fn(),
      setDraftByScopeKey,
      setIntent: vi.fn(),
      startConversation,
      submitPromptRef: { current: vi.fn() },
      submittedDraftSnapshotsRef,
      transientConversation: null,
      workspaceId: "workspace-1"
    });
    return { createConversation, submitPrompt };
  });

  return {
    activateSession,
    requestNewConversation() {
      requestAgentGUINewConversation({
        activeConversationId: activeConversationIdRef.current,
        conversations: conversationsRef.current,
        createConversation: result.current.createConversation,
        transientConversation: null,
        userProjects: input.userProjects
      });
    },
    persistActiveConversation,
    submitPrompt: result.current.submitPrompt,
    async waitForActivation() {
      let activation: EngineExternalCommand | undefined;
      await waitFor(() => {
        const matches = activateSession.mock.calls.map(([input]) => input);
        expect(matches).toHaveLength(1);
        activation = matches[0];
      });
      return activation as EngineExternalCommand & {
        cwd: string;
        railPlacement: AgentActivityRailPlacement;
      };
    }
  };
}

function conversationSummary(input: {
  cwd: string;
  railSectionKey: string;
}): AgentGUIConversationSummary {
  return {
    id: "session-current",
    provider: "codex",
    title: "Current conversation",
    status: "ready",
    cwd: input.cwd,
    railSectionKey: input.railSectionKey,
    updatedAtUnixMs: 1
  };
}
