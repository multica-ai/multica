import "@testing-library/jest-dom/vitest";
import { render } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type {
  AgentGUIAgentTarget,
  AgentGUINodeData,
  NodeFrame
} from "../../types";
import type { AgentGUIConversationSummary } from "./model/agentGuiConversationModel";
import type {
  AgentComposerDraft,
  AgentGUINodeViewModel
} from "./model/agentGuiNodeTypes";
import {
  groupAgentGUINodeViewModelFixture,
  type AgentGUINodeViewModelFixtureOverrides,
  type FlatAgentGUINodeViewModelFixture
} from "./model/AgentGUINodeViewModel.fixture";
import { AgentGUINode } from "./AgentGUINode";
import { createLocalAgentGUIAgentTarget } from "../../agentTargets";
import { buildAgentComposerDraft } from "./model/agentComposerDraft";
import { resolveNextAgentGUIConversationRailWidthPx } from "./model/agentGuiRailLayout";

const { agentGuiNodeViewSpy, translate } = vi.hoisted(() => ({
  agentGuiNodeViewSpy: vi.fn(),
  translate: (key: string, options?: Record<string, unknown>) => {
    if (key === "agentHost.workspaceAgentSessionDetailToolCalls") {
      return `${options?.count ?? 0} tool calls`;
    }
    if (key === "agentHost.agentGui.followupPlaceholder") {
      return `Request ${options?.provider ?? ""} to continue`;
    }
    return key;
  }
}));

let mockViewModel: AgentGUINodeViewModel;

vi.mock("../../i18n/index", () => ({
  useTranslation: () => ({
    t: translate
  })
}));

vi.mock("./controller/useAgentGUINodeController", () => ({
  useAgentGUINodeController: () => ({
    viewModel: mockViewModel,
    actions: {
      createConversation: vi.fn(),
      selectConversation: vi.fn(),
      submitPrompt: vi.fn(),
      submitGuidancePrompt: vi.fn(),
      showPromptImagesUnsupported: vi.fn(),
      submitApprovalOption: vi.fn(),
      submitInteractivePrompt: vi.fn(),
      interruptCurrentTurn: vi.fn(),
      updateDraftContent: vi.fn(),
      updateComposerSettings: vi.fn(),
      sendQueuedPromptNext: vi.fn(),
      removeQueuedPrompt: vi.fn(),
      editQueuedPrompt: vi.fn(),
      removeProject: vi.fn(),
      toggleProjectPinned: vi.fn(),
      confirmDeleteProjectConversations: vi.fn(),
      requestDeleteConversation: vi.fn(),
      retryActivation: vi.fn(),
      continueInNewConversation: vi.fn(),
      cancelDeleteConversation: vi.fn(),
      confirmDeleteConversation: vi.fn()
    }
  })
}));

vi.mock("./AgentGUINodeView", () => ({
  AgentGUINodeView: (props: unknown) => {
    agentGuiNodeViewSpy(props);
    return <div data-testid="agent-gui-view" />;
  }
}));

vi.mock("../shared/WorkspaceNodeWindow", () => ({
  WorkspaceNodeWindow: ({
    children,
    width,
    height
  }: {
    children: (frame: {
      size: { width: number; height: number };
    }) => React.ReactNode;
    width: number;
    height: number;
  }) => <div>{children({ size: { width, height } })}</div>
}));

vi.mock("../shared/CanvasNodeGhostIconButton", () => ({
  CanvasNodeGhostIconButton: ({ children }: { children: React.ReactNode }) => (
    <button>{children}</button>
  )
}));

vi.mock("../shared/canvasNodeChromeIcons", () => ({
  CanvasNodePanelLinedIcon: () => <span data-testid="panel-icon" />
}));

vi.mock("../workspaceDesktop/view/AgentProbeInfoPopover", () => ({
  AgentProbeInfoPopover: () => <div data-testid="agent-probe-popover" />
}));

describe("AgentGUINode memoization", () => {
  afterEach(() => {
    agentGuiNodeViewSpy.mockReset();
  });

  it("rerenders when its host status controller changes", () => {
    mockViewModel = createViewModel();
    const firstController = createStatusControllerStub();
    const props = createProps({
      runtimeRequests: {
        agentStatusController: firstController
      }
    });
    const { rerender } = render(<AgentGUINode {...props} />);

    expect(agentGuiNodeViewSpy).toHaveBeenCalledTimes(1);
    agentGuiNodeViewSpy.mockClear();

    rerender(
      <AgentGUINode
        {...props}
        runtimeRequests={{
          ...props.runtimeRequests,
          agentStatusController: createStatusControllerStub()
        }}
      />
    );

    expect(agentGuiNodeViewSpy).toHaveBeenCalledTimes(1);
  });

  it("rerenders when Session input history is enabled", () => {
    mockViewModel = createViewModel();
    const props = createProps();
    const { rerender } = render(<AgentGUINode {...props} />);

    expect(agentGuiNodeViewSpy).toHaveBeenLastCalledWith(
      expect.objectContaining({ sessionInputHistoryEnabled: false })
    );
    agentGuiNodeViewSpy.mockClear();

    rerender(
      <AgentGUINode
        {...props}
        hostCapabilities={{ sessionInputHistoryEnabled: true }}
      />
    );

    expect(agentGuiNodeViewSpy).toHaveBeenLastCalledWith(
      expect.objectContaining({ sessionInputHistoryEnabled: true })
    );
  });

  it("rerenders when Side conversations are enabled", () => {
    mockViewModel = createViewModel();
    const props = createProps();
    const { rerender } = render(<AgentGUINode {...props} />);

    expect(agentGuiNodeViewSpy).toHaveBeenLastCalledWith(
      expect.objectContaining({ sideConversationEnabled: false })
    );
    agentGuiNodeViewSpy.mockClear();

    rerender(
      <AgentGUINode
        {...props}
        hostCapabilities={{ sideConversationEnabled: true }}
      />
    );

    expect(agentGuiNodeViewSpy).toHaveBeenLastCalledWith(
      expect.objectContaining({ sideConversationEnabled: true })
    );
  });

  it("rerenders when per-target composer overrides change", () => {
    mockViewModel = createViewModel();
    const props = createProps({
      state: createState({
        agentTargetId: "target-a",
        composerOverridesByAgentTargetId: {
          "target-a": { model: "gpt-5" }
        }
      })
    });
    const { rerender } = render(<AgentGUINode {...props} />);

    expect(agentGuiNodeViewSpy).toHaveBeenCalledTimes(1);
    agentGuiNodeViewSpy.mockClear();

    rerender(
      <AgentGUINode
        {...props}
        state={createState({
          agentTargetId: "target-a",
          composerOverridesByAgentTargetId: {
            "target-a": { model: "gpt-5.1" }
          }
        })}
      />
    );

    expect(agentGuiNodeViewSpy).toHaveBeenCalledTimes(1);
  });

  it("rerenders when per-target session memory changes", () => {
    mockViewModel = createViewModel();
    const props = createProps({
      state: createState({
        lastActiveAgentSessionIdByAgentTargetId: {
          "local:codex": "session-1"
        }
      })
    });
    const { rerender } = render(<AgentGUINode {...props} />);

    expect(agentGuiNodeViewSpy).toHaveBeenCalledTimes(1);
    agentGuiNodeViewSpy.mockClear();

    rerender(
      <AgentGUINode
        {...props}
        state={createState({
          lastActiveAgentSessionIdByAgentTargetId: {
            "local:codex": "session-2"
          }
        })}
      />
    );

    expect(agentGuiNodeViewSpy).toHaveBeenCalledTimes(1);
  });

  it("passes reference content error recovery through the memo boundary", () => {
    mockViewModel = createViewModel();
    const firstResolver = vi.fn();
    const secondResolver = vi.fn();
    const initial = createProps();
    const props = {
      ...initial,
      workspace: {
        ...initial.workspace,
        resolveReferenceContentErrorAction: firstResolver
      }
    };
    const { rerender } = render(<AgentGUINode {...props} />);

    expect(
      agentGuiNodeViewSpy.mock.calls.at(-1)?.[0]
        .resolveReferenceContentErrorAction
    ).toBe(firstResolver);

    agentGuiNodeViewSpy.mockClear();
    rerender(
      <AgentGUINode
        {...props}
        workspace={{
          ...props.workspace,
          resolveReferenceContentErrorAction: secondResolver
        }}
      />
    );

    expect(agentGuiNodeViewSpy).toHaveBeenCalledTimes(1);
    expect(
      agentGuiNodeViewSpy.mock.calls.at(-1)?.[0]
        .resolveReferenceContentErrorAction
    ).toBe(secondResolver);
  });

  it("passes project directory picker header actions through the memo boundary", () => {
    mockViewModel = createViewModel();
    const firstRenderer = vi.fn();
    const secondRenderer = vi.fn();
    const initial = createProps();
    const props = {
      ...initial,
      renderSlots: {
        ...initial.renderSlots,
        projectDirectoryPickerHeaderActions: firstRenderer
      }
    };
    const { rerender } = render(<AgentGUINode {...props} />);

    expect(
      agentGuiNodeViewSpy.mock.calls.at(-1)?.[0]
        .renderProjectDirectoryPickerHeaderActions
    ).toBe(firstRenderer);

    agentGuiNodeViewSpy.mockClear();
    rerender(
      <AgentGUINode
        {...props}
        renderSlots={{
          ...props.renderSlots,
          projectDirectoryPickerHeaderActions: secondRenderer
        }}
      />
    );

    expect(agentGuiNodeViewSpy).toHaveBeenCalledTimes(1);
    expect(
      agentGuiNodeViewSpy.mock.calls.at(-1)?.[0]
        .renderProjectDirectoryPickerHeaderActions
    ).toBe(secondRenderer);
  });

  it("keeps rail labels stable when provider-facing labels change", () => {
    mockViewModel = createViewModel();
    const props = createProps();
    const { rerender } = render(<AgentGUINode {...props} />);
    const firstViewProps = agentGuiNodeViewSpy.mock.calls.at(-1)?.[0] as
      | {
          conversationRailLabels: unknown;
          labels: unknown;
        }
      | undefined;
    expect(firstViewProps).toBeDefined();

    mockViewModel = createViewModel({
      selectedAgentTarget: createLocalAgentGUIAgentTarget("claude-code"),
      agentTargets: [createLocalAgentGUIAgentTarget("claude-code")]
    });
    rerender(
      <AgentGUINode
        {...props}
        state={createState({ provider: "claude-code" })}
      />
    );
    const secondViewProps = agentGuiNodeViewSpy.mock.calls.at(-1)?.[0] as
      | {
          conversationRailLabels: unknown;
          labels: unknown;
        }
      | undefined;
    expect(secondViewProps).toBeDefined();

    expect(secondViewProps!.labels).not.toBe(firstViewProps!.labels);
    expect(secondViewProps!.conversationRailLabels).toBe(
      firstViewProps!.conversationRailLabels
    );
  });

  it("uses the exact active Agent target label in provider-facing copy", () => {
    const kimiTarget: AgentGUIAgentTarget = {
      agentTargetId: "extension:kimi-code",
      label: "Kimi Code",
      provider: "acp:kimi-code",
      ref: {
        kind: "agent_extension",
        provider: "acp:kimi-code"
      },
      targetId: "extension:kimi-code"
    };
    const activeConversation: AgentGUIConversationSummary = {
      agentTargetId: kimiTarget.agentTargetId,
      cwd: "/workspace",
      id: "session-1",
      provider: kimiTarget.provider,
      status: "ready",
      title: "Hello",
      updatedAtUnixMs: 1
    };
    mockViewModel = createViewModel({
      activeConversation,
      activeConversationId: activeConversation.id,
      agentTargets: [kimiTarget]
    });

    render(<AgentGUINode {...createProps()} />);

    const viewProps = agentGuiNodeViewSpy.mock.calls.at(-1)?.[0] as
      | {
          labels: {
            followupPlaceholder: string;
          };
        }
      | undefined;
    expect(viewProps?.labels.followupPlaceholder).toBe(
      "Request Kimi Code to continue"
    );
  });
});

function createStatusControllerStub() {
  const snapshot = {
    query: null,
    value: null,
    phase: "idle" as const,
    isRefreshing: false,
    errorCode: null
  };
  return {
    close: vi.fn(),
    getSnapshot: () => snapshot,
    invalidate: vi.fn(),
    open: vi.fn(),
    subscribe: () => () => {}
  };
}

function createProps(
  overrides: Partial<Parameters<typeof AgentGUINode>[0]> = {}
): Parameters<typeof AgentGUINode>[0] {
  return {
    identity: {
      nodeId: "agent-gui-1",
      workspaceId: "room-1",
      currentUserId: "user-1",
      title: "Codex"
    },
    workspace: {
      path: "/workspace",
      agentSettings: { avoidGroupingEdits: false }
    },
    frame: {
      position: { x: 80, y: 56 },
      width: 880,
      height: 520,
      desktopSize: { width: 1280, height: 720 },
      isActive: true
    },
    state: createState(),
    runtimeRequests: {},
    hostCapabilities: {},
    hostActions: {
      onClose: vi.fn(),
      onResize: vi.fn<(frame: NodeFrame) => void>(),
      onUpdateNode: vi.fn()
    },
    renderSlots: {},
    ...overrides
  };
}

function createState(
  overrides: Partial<AgentGUINodeData> = {}
): AgentGUINodeData {
  return {
    provider: "codex",
    lastActiveAgentSessionId: null,
    conversationRailWidthPx: null,
    conversationRailCollapsed: false,
    ...overrides
  };
}

function createViewModel(
  overrides: AgentGUINodeViewModelFixtureOverrides = {}
): AgentGUINodeViewModel {
  const draftContent: AgentComposerDraft = buildAgentComposerDraft({
    prompt: ""
  });
  return groupAgentGUINodeViewModelFixture({
    workspaceId: "room-1",
    data: createState(),
    activeConversationId: null,
    activeConversation: null,
    conversations: [],
    userProjects: [],
    conversation: null,
    conversationDetail: null,
    selectedAgentTarget: createLocalAgentGUIAgentTarget("codex"),
    agentTargets: [createLocalAgentGUIAgentTarget("codex")],
    agentTargetsLoading: false,
    conversationFilter: { kind: "all" },
    draftPrompt: "",
    draftContent,
    sessionChrome: {
      auth: null,
      approval: null,
      recovery: null,
      rawState: null
    },
    pendingInteractivePrompt: null,
    queuedPrompts: [],
    queueStatus: "active",
    gate: {
      conversationBusy: false,
      isAwaitingTurnStart: false,
      runtime: {
        status: "ready",
        reason: null,
        sessionRuntimeReason: null
      },
      editor: { status: "editable", reason: null },
      submission: { status: "ready", reason: null }
    },
    isSubmitting: false,
    isInterrupting: false,
    hasPendingSubmitStopTarget: false,
    promptImagesSupported: true,
    availability: "ready",
    listError: null,
    isCreatingConversation: false,
    isLoadingConversations: false,
    isLoadingMessages: false,
    detailError: null,
    deletingConversationId: null,
    deletingConversationTitle: null,
    composerSettings: {
      sessionSettings: null,
      draftSettings: {
        model: null,
        reasoningEffort: null,
        planMode: false,
        permissionModeId: "full-access"
      },
      defaultModel: null,
      defaultReasoningEffort: null,
      supportsModel: false,
      supportsReasoningEffort: true,
      supportsPlanMode: true,
      isSettingsLoading: false,
      modelUnavailable: false,
      reasoningUnavailable: false,
      availableModels: [],
      availableReasoningEfforts: []
    },
    availableCommands: [],
    availableSkills: [],
    ...overrides
  } as FlatAgentGUINodeViewModelFixture);
}
