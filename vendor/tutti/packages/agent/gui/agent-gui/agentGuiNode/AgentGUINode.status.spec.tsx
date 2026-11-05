import "@testing-library/jest-dom/vitest";
import { act, render } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  createLocalAgentGUIAgentTarget,
  createSharedAgentGUIAgentTarget
} from "../../agentTargets";
import type { AgentGUINodeData, NodeFrame } from "../../types";
import { AgentGUINode } from "./AgentGUINode";
import {
  createAgentStatusController,
  type AgentStatusController,
  type AgentStatusControllerSnapshot,
  type AgentStatusQuery,
  type AgentStatusStreamObserver
} from "./controller/AgentStatusController";
import { buildAgentComposerDraft } from "./model/agentComposerDraft";
import {
  groupAgentGUIViewFixture,
  type AgentGUIViewFixtureOverrides,
  type FlatAgentGUIViewFixture
} from "./model/agentGUIView.fixture";
import type {
  AgentComposerDraft,
  AgentGUIComposerViewModel,
  AgentGUIDetailViewModel,
  AgentGUIInteractionViewModel,
  AgentGUIOperationsViewModel,
  AgentGUIRailViewModel,
  AgentGUIReadinessViewModel,
  AgentGUIShellViewModel
} from "./model/agentGuiNodeTypes";
type AgentGUIViewFixture = {
  shell: AgentGUIShellViewModel;
  rail: AgentGUIRailViewModel;
  detail: AgentGUIDetailViewModel;
  composer: AgentGUIComposerViewModel;
  interaction: AgentGUIInteractionViewModel;
  readiness: AgentGUIReadinessViewModel;
  operations: AgentGUIOperationsViewModel;
};

const { agentGuiNodeViewSpy, agentProbeInfoPopoverSpy } = vi.hoisted(() => ({
  agentGuiNodeViewSpy: vi.fn(),
  agentProbeInfoPopoverSpy: vi.fn()
}));

let mockViewModel: AgentGUIViewFixture;

vi.mock("../../i18n/index", () => ({
  useTranslation: () => ({ t: (key: string) => key })
}));

vi.mock("./controller/useAgentGUIPage", () => ({
  useAgentGUIPage: () => ({
    ...mockViewModel,
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
    height,
    titleAccessory
  }: {
    children: (frame: {
      size: { width: number; height: number };
    }) => React.ReactNode;
    width: number;
    height: number;
    titleAccessory?: React.ReactNode;
  }) => (
    <div>
      {titleAccessory}
      {children({ size: { width, height } })}
    </div>
  )
}));

vi.mock("../shared/CanvasNodeGhostIconButton", () => ({
  CanvasNodeGhostIconButton: ({ children }: { children: React.ReactNode }) => (
    <button>{children}</button>
  )
}));

vi.mock("../shared/canvasNodeChromeIcons", () => ({
  CanvasNodePanelLinedIcon: () => <span />
}));

vi.mock("../workspaceDesktop/view/AgentProbeInfoPopover", () => ({
  AgentProbeInfoPopover: (props: unknown) => {
    agentProbeInfoPopoverSpy(props);
    return <div />;
  }
}));

describe("AgentGUINode status controller integration", () => {
  afterEach(() => {
    agentGuiNodeViewSpy.mockReset();
    agentProbeInfoPopoverSpy.mockReset();
  });

  it("routes info, config, and slash status through exact-scope requests", () => {
    mockViewModel = createViewModel();
    const queries: AgentStatusQuery[] = [];
    const unsubscribes: ReturnType<typeof vi.fn>[] = [];
    const controller = createAgentStatusController({
      source: {
        open: (query) => {
          queries.push(query);
          const unsubscribe = vi.fn();
          unsubscribes.push(unsubscribe);
          return unsubscribe;
        }
      }
    });
    render(
      <AgentGUINode
        {...createProps({
          runtimeRequests: { agentStatusController: controller }
        })}
      />
    );

    const infoProps = agentProbeInfoPopoverSpy.mock.calls.at(-1)?.[0] as {
      onOpen?: () => void;
    };
    act(() => infoProps.onOpen?.());
    let viewProps = latestViewProps();
    act(() => viewProps.onAgentConfigMenuOpen?.());
    viewProps = latestViewProps();
    act(() => viewProps.onSlashStatusOpen?.());
    viewProps = latestViewProps();
    act(() => viewProps.onSlashStatusClose?.());

    expect(queries).toEqual([
      statusQuery("agent-info"),
      statusQuery("agent-config"),
      statusQuery("slash-status")
    ]);
    expect(unsubscribes).toHaveLength(3);
    expect(
      unsubscribes.every((unsubscribe) => unsubscribe.mock.calls.length === 1)
    ).toBe(true);
  });

  it("gives Host account chrome the same exact target on config open", () => {
    const target = {
      ...createLocalAgentGUIAgentTarget("tutti-agent"),
      ownership: "self" as const
    };
    mockViewModel = createViewModel({
      selectedAgentTarget: target,
      agentTargets: [target],
      conversationFilter: {
        kind: "agentTarget",
        agentTargetId: target.agentTargetId ?? target.targetId
      }
    });
    const onAgentConfigMenuOpen = vi.fn();
    const renderAgentConfigAccount = vi.fn(() => (
      <div data-testid="host-account">Host account</div>
    ));
    const props = createProps();
    render(
      <AgentGUINode
        {...props}
        hostActions={{
          ...props.hostActions,
          onAgentConfigMenuOpen
        }}
        renderSlots={{
          agentConfigAccount: renderAgentConfigAccount
        }}
      />
    );

    expect(renderAgentConfigAccount).toHaveBeenCalledWith({
      agentTargetId: "local:tutti-agent",
      provider: "tutti-agent",
      label: target.label,
      ownership: "self",
      presentation: "menu"
    });
    expect(latestViewProps().agentConfigAccountContent).toBeTruthy();

    act(() => latestViewProps().onAgentConfigMenuOpen?.());

    expect(onAgentConfigMenuOpen).toHaveBeenCalledOnce();
    expect(onAgentConfigMenuOpen).toHaveBeenCalledWith({
      agentTargetId: "local:tutti-agent",
      provider: "tutti-agent",
      label: target.label,
      ownership: "self",
      presentation: "menu"
    });
  });

  it("renders Host system actions in the all-provider config menu", () => {
    mockViewModel = createViewModel({
      conversationFilter: { kind: "all" }
    });
    const renderAgentConfigSystemActions = vi.fn(() => (
      <div data-testid="host-system-actions">Host system actions</div>
    ));

    render(
      <AgentGUINode
        {...createProps()}
        renderSlots={{
          agentConfigSystemActions: renderAgentConfigSystemActions
        }}
      />
    );

    expect(renderAgentConfigSystemActions).toHaveBeenCalledOnce();
    expect(renderAgentConfigSystemActions).toHaveBeenCalledWith({
      presentation: "menu"
    });
    expect(latestViewProps().agentConfigSystemActionsContent).toBeTruthy();
  });

  it("does not project status from the previous target after a target switch", () => {
    mockViewModel = createViewModel();
    let observer: AgentStatusStreamObserver | null = null;
    const controller = createAgentStatusController({
      source: {
        open: (_query, nextObserver) => {
          observer = nextObserver;
          return vi.fn();
        }
      }
    });
    const props = createProps({
      runtimeRequests: { agentStatusController: controller }
    });
    const { rerender } = render(<AgentGUINode {...props} />);
    let viewProps = latestViewProps();
    act(() => viewProps.onSlashStatusOpen?.());
    act(() => {
      observer?.onFrame({
        kind: "refreshed",
        value: {
          contextState: "unavailable",
          limitsState: "available",
          quotas: [{ quotaType: "weekly", percentRemaining: 80 }]
        }
      });
    });
    expect(latestViewProps().slashStatusOverride?.limits).toHaveLength(1);

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
    viewProps = latestViewProps();
    expect(viewProps.slashStatusOverride?.limits).toEqual([]);
  });

  it("projects structured usage errors as localized config messages", () => {
    mockViewModel = createViewModel({
      conversationFilter: {
        kind: "agentTarget",
        agentTargetId: "local:codex"
      }
    });
    let observer: AgentStatusStreamObserver | null = null;
    const controller = createAgentStatusController({
      source: {
        open: (_query, nextObserver) => {
          observer = nextObserver;
          return vi.fn();
        }
      }
    });
    render(
      <AgentGUINode
        {...createProps({
          runtimeRequests: { agentStatusController: controller }
        })}
      />
    );

    act(() => latestViewProps().onAgentConfigMenuOpen?.());
    act(() => {
      observer?.onFrame({
        kind: "refreshed",
        value: {
          contextState: "unavailable",
          limitsErrorCode: "subscription_required",
          limitsState: "error",
          quotas: []
        }
      });
    });

    expect(latestViewProps().slashStatusUsageErrorMessage).toBe(
      "agentHost.agentGui.slashStatusUsageSubscriptionRequired"
    );
  });

  it("disables provider-account usage refresh for a Model Plan target", () => {
    const target = {
      ...createLocalAgentGUIAgentTarget("codex"),
      agentTargetId: "workspace-agent:kimi-code",
      targetId: "workspace-agent:kimi-code",
      providerAccountUsageApplicable: false
    };
    mockViewModel = createViewModel({
      selectedAgentTarget: target,
      agentTargets: [target],
      conversationFilter: {
        kind: "agentTarget",
        agentTargetId: "workspace-agent:kimi-code"
      }
    });
    render(
      <AgentGUINode
        {...createProps({
          runtimeRequests: {
            agentStatusController: createAgentStatusController({
              source: { open: vi.fn(() => vi.fn()) }
            })
          }
        })}
      />
    );

    expect(latestViewProps().onAgentUsageRefresh).toBeUndefined();
    expect(latestViewProps().onSlashStatusRefresh).toBeUndefined();
  });

  it("projects the host billing label into the rail config menu", () => {
    mockViewModel = createViewModel({
      conversationFilter: {
        kind: "agentTarget",
        agentTargetId: "local:codex"
      }
    });
    let observer: AgentStatusStreamObserver | null = null;
    const controller = createAgentStatusController({
      source: {
        open: (_query, nextObserver) => {
          observer = nextObserver;
          return vi.fn();
        }
      }
    });
    render(
      <AgentGUINode
        {...createProps({
          runtimeRequests: { agentStatusController: controller }
        })}
      />
    );

    act(() => latestViewProps().onAgentConfigMenuOpen?.());
    act(() => {
      observer?.onFrame({
        kind: "refreshed",
        value: {
          accountLabel: "API Usage Billing",
          contextState: "unavailable",
          limitsState: "available",
          quotas: []
        }
      });
    });

    expect(latestViewProps().providerAuthAccountLabels?.codex).toBe(
      "API Usage Billing"
    );
  });

  it("switches status controllers synchronously when their query keys match", () => {
    mockViewModel = createViewModel();
    const props = createProps({
      runtimeRequests: {
        agentStatusController: statusControllerWithUsedTokens(10)
      }
    });
    const { rerender } = render(<AgentGUINode {...props} />);

    expect(
      latestViewProps().slashStatusOverride?.contextWindow?.usedTokens
    ).toBe(10);

    rerender(
      <AgentGUINode
        {...props}
        runtimeRequests={{
          agentStatusController: statusControllerWithUsedTokens(20)
        }}
      />
    );

    expect(
      latestViewProps().slashStatusOverride?.contextWindow?.usedTokens
    ).toBe(20);
  });

  it("uses the active conversation binding before raw session hydration", () => {
    const target = createSharedAgentGUIAgentTarget({
      provider: "codex",
      sharedAgentId: "shared-1",
      label: "Shared Codex"
    });
    mockViewModel = createViewModel({
      activeConversationId: "binding-1",
      activeConversation: {
        id: "binding-1",
        agentTargetId: target.agentTargetId ?? target.targetId,
        provider: "codex",
        title: "Shared session",
        status: "completed",
        cwd: "/workspace",
        updatedAtUnixMs: 1
      },
      conversations: [],
      selectedAgentTarget: target,
      agentTargets: [target],
      sessionChrome: {
        auth: null,
        approval: null,
        recovery: null,
        rawState: null
      }
    });
    const open = vi.fn(() => vi.fn());
    const controller = createAgentStatusController({ source: { open } });
    render(
      <AgentGUINode
        {...createProps({
          runtimeRequests: { agentStatusController: controller }
        })}
      />
    );

    act(() => latestViewProps().onSlashStatusOpen?.());

    expect(open).toHaveBeenCalledWith(
      {
        scopeKey: "shared-agent:shared-1",
        agentSessionId: "binding-1",
        reason: "slash-status",
        forceRefresh: false
      },
      expect.any(Object)
    );
  });

  it("closes only the request owned by the surface that is closing", () => {
    mockViewModel = createViewModel();
    const unsubscribes: ReturnType<typeof vi.fn>[] = [];
    const controller = createAgentStatusController({
      source: {
        open: () => {
          const unsubscribe = vi.fn();
          unsubscribes.push(unsubscribe);
          return unsubscribe;
        }
      }
    });
    render(
      <AgentGUINode
        {...createProps({
          runtimeRequests: { agentStatusController: controller }
        })}
      />
    );

    const infoProps = agentProbeInfoPopoverSpy.mock.calls.at(-1)?.[0] as {
      onOpen?: () => void;
      onClose?: () => void;
    };
    act(() => infoProps.onOpen?.());
    act(() => latestViewProps().onSlashStatusOpen?.());
    expect(unsubscribes[0]).toHaveBeenCalledOnce();

    act(() => infoProps.onClose?.());
    expect(unsubscribes[1]).not.toHaveBeenCalled();

    act(() => latestViewProps().onSlashStatusClose?.());
    expect(unsubscribes[1]).toHaveBeenCalledOnce();

    act(() => latestViewProps().onAgentConfigMenuOpen?.());
    act(() => latestViewProps().onAgentConfigMenuClose?.());
    expect(unsubscribes[2]).toHaveBeenCalledOnce();
  });
});

interface CapturedViewProps {
  agentConfigAccountContent?: React.ReactNode;
  agentConfigSystemActionsContent?: React.ReactNode;
  onAgentConfigMenuOpen?: () => void;
  onAgentConfigMenuClose?: () => void;
  onAgentUsageRefresh?: () => void;
  providerAuthAccountLabels?: Partial<Record<string, string>>;
  onSlashStatusOpen?: () => void;
  onSlashStatusClose?: () => void;
  onSlashStatusRefresh?: () => void;
  slashStatusUsageErrorMessage?: string | null;
  slashStatusOverride?: {
    contextWindow?: { usedTokens?: number | null } | null;
    limits?: readonly unknown[];
  } | null;
}

function latestViewProps(): CapturedViewProps {
  return agentGuiNodeViewSpy.mock.calls.at(-1)?.[0] as CapturedViewProps;
}

function statusQuery(reason: AgentStatusQuery["reason"]): AgentStatusQuery {
  return {
    scopeKey: "local:codex",
    agentSessionId: null,
    reason,
    forceRefresh: false
  };
}

function statusControllerWithUsedTokens(
  usedTokens: number
): AgentStatusController {
  const snapshot: AgentStatusControllerSnapshot = {
    query: statusQuery("slash-status"),
    value: {
      agentSessionId: null,
      contextState: "available",
      contextWindow: { usedTokens, totalTokens: 100 },
      limitsState: "available",
      quotas: []
    },
    phase: "ready",
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
  overrides: AgentGUIViewFixtureOverrides = {}
): AgentGUIViewFixture {
  const draftContent: AgentComposerDraft = buildAgentComposerDraft({
    prompt: ""
  });
  return groupAgentGUIViewFixture({
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
  } as FlatAgentGUIViewFixture);
}
