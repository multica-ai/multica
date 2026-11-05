import { act, renderHook } from "@testing-library/react";
import {
  type AgentActivityTurn,
  type PendingActivationIntentRecord
} from "@tutti-os/agent-activity-core";
import { useMemo } from "react";
import { describe, expect, it, vi } from "vitest";
import { useAgentObservationGap } from "../../../shared/agentConversation/AgentObservationGapContext";
import { createTestRuntimeState } from "../../../shared/testing/createTestRuntimeState";
import {
  buildSessionPresentation,
  resolveAgentGUISharingRevokedRecovery,
  type SessionPresentationInput
} from "../../../state/sessionPresentation";
import type {
  AgentGUIInteractionReadiness,
  AgentGUIInteractionReadinessSource,
  AgentGUIObservationGap,
  AgentGUIObservationGapSource,
  AgentGUITargetConnectionSource,
  AgentGUITargetConnectionState
} from "../../../types";
import { resolveAgentGUIInteractionReadinessIdentity } from "./agentGuiController.interactionHelpers";
import { useAgentGUIInteractionReadiness } from "./useAgentGUIInteractionReadiness";
import { useAgentGUITargetConnectionState } from "./useAgentGUITargetConnectionState";

class FakeTargetConnectionSource implements AgentGUITargetConnectionSource {
  private readonly listeners = new Set<() => void>();
  private state: AgentGUITargetConnectionState = {
    status: "connecting",
    retryAttempt: 0
  };

  getConnectionState(): AgentGUITargetConnectionState {
    return this.state;
  }

  subscribe(listener: () => void): () => void {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  }

  set(state: AgentGUITargetConnectionState): void {
    this.state = state;
    for (const listener of this.listeners) listener();
  }
}

class FakeObservationGapSource implements AgentGUIObservationGapSource {
  private readonly listeners = new Set<() => void>();
  private gap: AgentGUIObservationGap | null = {
    startedAtUnixMs: 10_000
  };

  getObservationGap(): AgentGUIObservationGap | null {
    return this.gap;
  }

  subscribe(listener: () => void): () => void {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  }

  clear(): void {
    this.gap = null;
    for (const listener of this.listeners) listener();
  }
}

class StaticInteractionSource implements AgentGUIInteractionReadinessSource {
  constructor(private readonly state: AgentGUIInteractionReadiness) {}

  getInteractionReadiness() {
    return this.state;
  }

  subscribe(): () => void {
    return () => undefined;
  }
}

describe("useAgentGUISessionPresentation", () => {
  it("projects a revoked selected Home target into the shared status chrome", () => {
    expect(
      resolveAgentGUISharingRevokedRecovery({
        activeConversationId: null,
        selectedAgentTargetOwnerLabel: "Jackson",
        selectedAgentTargetUnavailable: true,
        selectedAgentTargetUnavailableReason: "agent_sharing_revoked",
        sessionRuntimeBlock: null
      })
    ).toEqual({
      kind: "agent-sharing-revoked",
      message: "Jackson stopped sharing this agent",
      canRetry: false
    });
  });

  it("separates Turn-start feedback from canonical execution busy", () => {
    const conversationServices = createTestRuntimeState("workspace-1");
    const input = {
      activeConversation: null,
      activeConversationId: "session-1",
      activeEngineActiveTurn: null,
      activeEngineAvailability: "available",
      activeEngineHasPendingInteractions: false,
      activeHasPendingSubmitStopTarget: false,
      activeEngineLatestTurn: null as AgentActivityTurn | null,
      activeEngineRuntimeAvailability: null,
      activeEngineRuntimeActivity: "idle" as "idle" | "running",
      activeCanonicalSession: {
        agentSessionId: "session-1",
        resumable: true
      },
      activeEngineSettingsUpdate: null,
      activeLatestPendingSubmitTurnId: null as string | null,
      activeLiveState: "active",
      activeMessages: [],
      activePendingActivation: null as PendingActivationIntentRecord | null,
      activeSessionState: null,
      activeTimelineItems: [],
      activationError: null,
      activationErrorCode: null,
      activationState: "active",
      activityDisplayStatus: null,
      agentActivityRuntime: {},
      agentTargetsLoading: false,
      composerSupport: {
        model: false,
        reasoningEffort: false,
        permissionMode: false,
        planMode: false,
        planImplementation: false,
        plan: false
      },
      conversation: null,
      currentUserId: "user-1",
      hasUnconfirmedSubmit: true,
      isCreatingConversation: false,
      isInterrupting: false,
      isLoadingMessages: false,
      isRespondingToInteraction: false,
      isSubmitting: false,
      lastRenderStateDiagnosticKeyRef: { current: null },
      pendingApproval: null,
      planImplementationTurnIdRef: { current: null },
      providerReadinessGate: null,
      selectedAgentTargetOwnerLabel: null,
      selectedAgentTargetUnavailable: false,
      selectedAgentTargetUnavailableReason: null,
      serverInteractivePrompt: null,
      conversationServices,
      targetConnectionAgentTargetId: null,
      workspaceId: "workspace-1"
    } as unknown as Parameters<typeof useAgentGUISessionPresentation>[0];

    const rendered = renderHook(() => useAgentGUISessionPresentation(input));

    expect(rendered.result.current.activeConversationBusy).toBe(false);
    expect(rendered.result.current.isAwaitingTurnStart).toBe(true);
    expect(rendered.result.current.composerGate.submission).toEqual({
      status: "blocked",
      reason: "submitting"
    });

    input.activeLatestPendingSubmitTurnId = "turn-1";
    input.hasUnconfirmedSubmit = false;
    rendered.rerender();

    expect(rendered.result.current.activeConversationBusy).toBe(false);
    expect(rendered.result.current.isAwaitingTurnStart).toBe(false);

    input.activeLiveState = "activating";
    rendered.rerender();

    expect(rendered.result.current.activeConversationBusy).toBe(false);
    expect(rendered.result.current.isAwaitingTurnStart).toBe(false);
    expect(rendered.result.current.composerGate.submission.status).not.toBe(
      "queue"
    );

    input.activeLiveState = "active";
    rendered.rerender();

    input.activePendingActivation = {
      agentSessionId: "session-1",
      agentTargetId: "local:claude-code",
      clientSubmitId: "submit-1",
      commandOutcome: "pending",
      commandSettledAtUnixMs: null,
      content: [{ type: "text", text: "hello" }],
      cwd: "/workspace",
      errorCode: null,
      errorMessage: null,
      expiresAtUnixMs: Number.MAX_SAFE_INTEGER,
      initialPromptRetracted: false,
      initialTurnExpected: true,
      lastObservedStage: "requested",
      mode: "new",
      requestedAtUnixMs: 1,
      requestId: "request-1",
      snapshotObservedAtUnixMs: null,
      snapshotOutcome: "not_observed",
      status: "requested",
      title: null,
      workspaceId: "workspace-1"
    };
    rendered.rerender();
    expect(rendered.result.current.activeConversationBusy).toBe(false);
    expect(rendered.result.current.isAwaitingTurnStart).toBe(true);

    input.activeEngineLatestTurn = {
      agentSessionId: "session-1",
      origin: "user_prompt",
      outcome: "completed",
      phase: "settled",
      settledAtUnixMs: 3,
      startedAtUnixMs: 2,
      turnId: "turn-1",
      updatedAtUnixMs: 3
    };
    rendered.rerender();

    expect(rendered.result.current.activeConversationBusy).toBe(false);

    input.activePendingActivation = null;
    input.activeEngineLatestTurn = null;
    input.activeEngineRuntimeActivity = "running";
    input.activityDisplayStatus = "idle";
    rendered.rerender();
    expect(rendered.result.current.activeConversationBusy).toBe(false);

    input.activeEngineRuntimeActivity = "idle";
    input.activityDisplayStatus = "idle";
    rendered.rerender();
    expect(rendered.result.current.activeConversationBusy).toBe(false);

    input.activeEngineActiveTurn = {
      agentSessionId: "session-1",
      origin: "user_prompt",
      phase: "running",
      startedAtUnixMs: 5,
      turnId: "turn-2",
      updatedAtUnixMs: 5
    };
    rendered.rerender();
    expect(rendered.result.current.activeConversationBusy).toBe(true);
    expect(rendered.result.current.isAwaitingTurnStart).toBe(false);

    input.activeEngineActiveTurn = null;
    rendered.rerender();
    expect(rendered.result.current.activeConversationBusy).toBe(false);
  });

  it("does not expose manual retry for failed activation", () => {
    const conversationServices = createTestRuntimeState("workspace-1");
    const input = {
      activeConversation: null,
      activeConversationId: "session-rejected",
      activeEngineActiveTurn: null,
      activeEngineAvailability: "available",
      activeEngineHasPendingInteractions: false,
      activeEngineLatestTurn: null,
      activeEngineRuntimeAvailability: null,
      activeCanonicalSession: null,
      activeLatestPendingSubmitTurnId: null,
      activeLiveState: "failed",
      activeMessages: [],
      activePendingActivation: {
        agentSessionId: "session-rejected",
        agentTargetId: "local:claude-code",
        clientSubmitId: "submit-rejected",
        content: [{ type: "text", text: "hello" }],
        cwd: "/workspace",
        errorCode: "auth_required",
        errorMessage: "Claude Code needs authentication",
        expiresAtUnixMs: Number.MAX_SAFE_INTEGER,
        initialPromptRetracted: false,
        initialTurnExpected: true,
        mode: "new",
        requestedAtUnixMs: 1,
        requestId: "request-rejected",
        status: "failed",
        title: null,
        workspaceId: "workspace-1"
      },
      activeSessionState: null,
      activeTimelineItems: [],
      activationError: "Claude Code needs authentication",
      activationErrorCode: "auth_required",
      activationState: "failed",
      activityDisplayStatus: null,
      agentActivityRuntime: {},
      agentTargetsLoading: false,
      composerSupport: {
        model: false,
        reasoningEffort: false,
        permissionMode: false,
        planMode: false,
        planImplementation: false,
        plan: false
      },
      conversation: null,
      currentUserId: "user-1",
      isCreatingConversation: false,
      isInterrupting: false,
      isLoadingMessages: false,
      isRespondingToInteraction: false,
      isSubmitting: false,
      lastRenderStateDiagnosticKeyRef: { current: null },
      pendingApproval: null,
      planImplementationTurnIdRef: { current: null },
      providerReadinessGate: null,
      selectedAgentTargetOwnerLabel: null,
      selectedAgentTargetUnavailable: false,
      selectedAgentTargetUnavailableReason: null,
      serverInteractivePrompt: null,
      conversationServices,
      targetConnectionAgentTargetId: null,
      workspaceId: "workspace-1"
    } as unknown as Parameters<typeof useAgentGUISessionPresentation>[0];

    const rendered = renderHook(() => useAgentGUISessionPresentation(input));

    expect(rendered.result.current.sessionChrome.auth).toEqual({
      message: "Claude Code needs authentication"
    });

    input.activationError = "Provider rejected the initial request";
    input.activationErrorCode = null;
    rendered.rerender();

    expect(rendered.result.current.sessionChrome.auth).toBeNull();
    expect(rendered.result.current.sessionChrome.recovery).toEqual({
      kind: "failed",
      message: "Provider rejected the initial request",
      canRetry: false
    });
  });

  it("makes a shared Agent composer editable in the same snapshot that its target connects", () => {
    const targetConnectionSource = new FakeTargetConnectionSource();
    const conversationServices = createTestRuntimeState("workspace-1");
    const input = {
      activeConversation: null,
      activeConversationId: null,
      activeEngineActiveTurn: null,
      activeEngineAvailability: "available",
      activeEngineHasPendingInteractions: false,
      activeEngineLatestTurn: null,
      activeEngineRuntimeAvailability: null,
      activeCanonicalSession: null,
      activeEngineSettingsUpdate: null,
      activeLatestPendingSubmitTurnId: null,
      activeLiveState: "inactive",
      activeMessages: [],
      activePendingActivation: null,
      activeSessionState: null,
      activeTimelineItems: [],
      activationError: null,
      activationErrorCode: null,
      activationState: null,
      activityDisplayStatus: null,
      agentActivityRuntime: {},
      agentTargetsLoading: false,
      composerSupport: {
        model: false,
        reasoningEffort: false,
        permissionMode: false,
        planMode: false,
        planImplementation: false,
        plan: false
      },
      conversation: null,
      currentUserId: "user-1",
      isCreatingConversation: false,
      isInterrupting: false,
      isLoadingMessages: false,
      isRespondingToInteraction: false,
      isSubmitting: false,
      lastRenderStateDiagnosticKeyRef: { current: null },
      pendingApproval: null,
      planImplementationTurnIdRef: { current: null },
      providerReadinessGate: null,
      selectedAgentTargetOwnerLabel: null,
      selectedAgentTargetUnavailable: false,
      selectedAgentTargetUnavailableReason: null,
      serverInteractivePrompt: null,
      conversationServices,
      targetConnectionAgentTargetId: "shared-agent:shared-1",
      targetConnectionSource,
      workspaceId: "workspace-1"
    } as unknown as Parameters<typeof useAgentGUISessionPresentation>[0];
    const rendered = renderHook(
      ({ renderRevision }) => {
        void renderRevision;
        return useAgentGUISessionPresentation(input);
      },
      { initialProps: { renderRevision: 0 } }
    );

    expect(rendered.result.current.composerGate).toMatchObject({
      runtime: { status: "blocked", reason: "target_connection" },
      editor: { status: "blocked", reason: "runtime_blocked" },
      submission: { status: "blocked", reason: "runtime_blocked" }
    });

    act(() => {
      targetConnectionSource.set({ status: "connected", retryAttempt: 0 });
    });

    expect(rendered.result.current.composerGate).toMatchObject({
      runtime: { status: "ready", reason: null },
      editor: { status: "editable", reason: null },
      submission: { status: "ready", reason: null }
    });
    const readyGate = rendered.result.current.composerGate;

    rendered.rerender({ renderRevision: 1 });

    expect(rendered.result.current.composerGate).toEqual(readyGate);
  });

  it("keeps target and Turn recovery outside exact Interaction readiness authority", () => {
    const targetConnectionSource = new FakeTargetConnectionSource();
    targetConnectionSource.set({ status: "connected", retryAttempt: 0 });
    const observationGapSource = new FakeObservationGapSource();
    const conversationServices = createTestRuntimeState("workspace-1");
    const input = {
      activeConversation: null,
      activeConversationId: "session-1",
      activeEngineActiveTurn: {
        agentSessionId: "session-1",
        origin: "user_prompt",
        phase: "running",
        startedAtUnixMs: 1,
        turnId: "turn-1",
        updatedAtUnixMs: 1
      },
      activeEngineAvailability: "available",
      activeEngineHasPendingInteractions: false,
      activeEngineLatestTurn: null,
      activeEngineRuntimeAvailability: null,
      activeCanonicalSession: {
        agentSessionId: "session-1",
        resumable: true
      },
      activeEngineSettingsUpdate: null,
      activeLatestPendingSubmitTurnId: null,
      activeLiveState: "active",
      activeMessages: [],
      activePendingActivation: null,
      activeSessionState: null,
      activeTimelineItems: [],
      activationError: null,
      activationErrorCode: null,
      activationState: "active",
      activityDisplayStatus: null,
      agentActivityRuntime: {},
      agentTargetsLoading: false,
      composerSupport: {
        model: false,
        reasoningEffort: false,
        permissionMode: false,
        planMode: false,
        planImplementation: false,
        plan: false
      },
      conversation: null,
      currentUserId: "user-1",
      isCreatingConversation: false,
      isInterrupting: false,
      isLoadingMessages: false,
      isRespondingToInteraction: false,
      isSubmitting: false,
      lastRenderStateDiagnosticKeyRef: { current: null },
      observationGapSource,
      pendingApproval: null,
      planImplementationTurnIdRef: { current: null },
      providerReadinessGate: null,
      selectedAgentTargetOwnerLabel: null,
      selectedAgentTargetUnavailable: false,
      selectedAgentTargetUnavailableReason: null,
      serverInteractivePrompt: null,
      conversationServices,
      targetConnectionAgentTargetId: "shared-agent:shared-1",
      targetConnectionSource,
      workspaceId: "workspace-1"
    } as unknown as Parameters<typeof useAgentGUISessionPresentation>[0];
    const rendered = renderHook(() => useAgentGUISessionPresentation(input));

    expect(rendered.result.current.sessionChrome.recovery).toMatchObject({
      kind: "transport-connecting",
      message: "Synchronizing the latest task progress…"
    });
    expect(rendered.result.current.composerGate.runtime).toMatchObject({
      status: "blocked",
      reason: "target_connection"
    });

    act(() => {
      targetConnectionSource.set({ status: "unavailable", retryAttempt: 1 });
    });
    expect(rendered.result.current.sessionChrome.recovery).toMatchObject({
      kind: "transport-unavailable"
    });

    input.interactionReadinessSource = new StaticInteractionSource({
      status: "ready"
    });
    input.pendingApproval = {
      kind: "approval",
      id: "approval-1",
      agentSessionId: "session-1",
      turnId: "turn-1",
      requestId: "request-1",
      callId: "call-1",
      title: "Allow command?",
      toolName: "shell",
      status: "pending",
      input: null,
      options: [],
      occurredAtUnixMs: 1
    };
    rendered.rerender();

    expect(rendered.result.current.sessionChrome).toMatchObject({
      approval: { requestId: "request-1" },
      recovery: null
    });
    expect(rendered.result.current.composerGate.runtime).toMatchObject({
      status: "blocked",
      reason: "target_connection"
    });
    expect(rendered.result.current.isRespondingApproval).toBe(false);

    const blockedInteractionCases = [
      {
        reason: "synchronizing",
        recoveryKind: "transport-connecting",
        preservesApproval: true
      },
      {
        reason: "owner_offline",
        recoveryKind: "transport-unavailable",
        preservesApproval: true
      },
      {
        reason: "binding_revoked",
        recoveryKind: "agent-sharing-revoked",
        preservesApproval: false
      }
    ] as const;
    for (const blockedCase of blockedInteractionCases) {
      input.interactionReadinessSource = new StaticInteractionSource({
        status: "blocked",
        reason: blockedCase.reason
      });
      rendered.rerender();

      expect(rendered.result.current.sessionChrome.recovery).toMatchObject({
        kind: blockedCase.recoveryKind
      });
      expect(rendered.result.current.sessionChrome.approval).toEqual(
        blockedCase.preservesApproval
          ? expect.objectContaining({ requestId: "request-1" })
          : null
      );
      expect(rendered.result.current.composerGate.runtime).toMatchObject({
        status: "blocked",
        reason: "target_connection"
      });
      expect(rendered.result.current.isRespondingApproval).toBe(true);
    }

    const promptAwareReadinessSource = {
      getInteractionReadiness: vi.fn((identity: { requestId: string }) =>
        identity.requestId === "question-1"
          ? ({ status: "ready" } as const)
          : ({ status: "blocked", reason: "synchronizing" } as const)
      ),
      subscribe: vi.fn(() => () => undefined)
    };
    input.interactionReadinessSource = promptAwareReadinessSource;
    input.serverInteractivePrompt = {
      kind: "ask-user",
      agentSessionId: "session-1",
      turnId: "turn-2",
      requestId: "question-1",
      title: "Choose a deployment",
      questions: []
    };
    rendered.rerender();

    expect(
      promptAwareReadinessSource.getInteractionReadiness
    ).toHaveBeenCalledWith({
      agentSessionId: "session-1",
      requestId: "question-1",
      turnId: "turn-2",
      workspaceId: "workspace-1"
    });
    expect(
      promptAwareReadinessSource.getInteractionReadiness
    ).toHaveBeenCalledWith({
      agentSessionId: "session-1",
      requestId: "request-1",
      turnId: "turn-1",
      workspaceId: "workspace-1"
    });
    expect(rendered.result.current.pendingInteractivePrompt).toMatchObject({
      requestId: "question-1"
    });
    expect(rendered.result.current.pendingApproval).toMatchObject({
      requestId: "request-1"
    });
    expect(rendered.result.current.sessionChrome.recovery).toBeNull();
    expect(rendered.result.current.composerGate.runtime).toMatchObject({
      status: "blocked",
      reason: "target_connection"
    });
    expect(rendered.result.current.isRespondingApproval).toBe(true);
    expect(rendered.result.current.isRespondingInteractivePrompt).toBe(false);

    input.interactionReadinessSource = {
      getInteractionReadiness: vi.fn((identity: { requestId: string }) =>
        identity.requestId === "question-1"
          ? ({ status: "blocked", reason: "synchronizing" } as const)
          : ({ status: "ready" } as const)
      ),
      subscribe: vi.fn(() => () => undefined)
    };
    rendered.rerender();

    expect(rendered.result.current.sessionChrome.recovery).toMatchObject({
      kind: "transport-connecting",
      interactionScoped: true
    });
    expect(rendered.result.current.isRespondingApproval).toBe(false);
    expect(rendered.result.current.isRespondingInteractivePrompt).toBe(true);

    input.interactionReadinessSource = new StaticInteractionSource({
      status: "ready"
    });
    input.serverInteractivePrompt = null;
    rendered.rerender();
    act(() => {
      targetConnectionSource.set({ status: "connected", retryAttempt: 0 });
      observationGapSource.clear();
    });

    expect(rendered.result.current.sessionChrome.recovery).toBeNull();
    expect(rendered.result.current.composerGate.runtime).toMatchObject({
      status: "ready",
      reason: null
    });
  });

  it("keeps shared history visible while revoked sharing blocks commands with owner copy", () => {
    const targetConnectionSource = new FakeTargetConnectionSource();
    targetConnectionSource.set({ status: "unavailable", retryAttempt: 4 });
    const conversationServices = createTestRuntimeState("workspace-1");
    const input = {
      activeConversation: null,
      activeConversationId: "session-1",
      activeEngineActiveTurn: null,
      activeEngineAvailability: "available",
      activeEngineHasPendingInteractions: false,
      activeEngineLatestTurn: null,
      activeEngineRuntimeAvailability: {
        state: "blocked",
        reason: "agent_sharing_revoked",
        ownerLabel: "riceballmama"
      },
      activeCanonicalSession: {
        agentSessionId: "session-1",
        resumable: true
      },
      activeEngineSettingsUpdate: null,
      activeLatestPendingSubmitTurnId: null,
      activeLiveState: "active",
      activeMessages: [],
      activePendingActivation: null,
      activeSessionState: null,
      activeTimelineItems: [{ role: "user" }],
      activationError: null,
      activationErrorCode: null,
      activationState: "active",
      activityDisplayStatus: null,
      agentActivityRuntime: {},
      agentTargetsLoading: false,
      composerSupport: {
        model: false,
        reasoningEffort: false,
        permissionMode: false,
        planMode: false,
        planImplementation: false,
        plan: false
      },
      conversation: null,
      currentUserId: "user-1",
      isCreatingConversation: false,
      isInterrupting: false,
      isLoadingMessages: false,
      isRespondingToInteraction: false,
      isSubmitting: false,
      lastRenderStateDiagnosticKeyRef: { current: null },
      pendingApproval: null,
      planImplementationTurnIdRef: { current: null },
      providerReadinessGate: null,
      selectedAgentTargetOwnerLabel: null,
      selectedAgentTargetUnavailable: false,
      selectedAgentTargetUnavailableReason: null,
      serverInteractivePrompt: null,
      conversationServices,
      targetConnectionAgentTargetId: "shared-agent:shared-1",
      targetConnectionSource,
      workspaceId: "workspace-1"
    } as unknown as Parameters<typeof useAgentGUISessionPresentation>[0];

    const rendered = renderHook(() => useAgentGUISessionPresentation(input));

    expect(rendered.result.current.sessionChrome.recovery).toEqual({
      kind: "agent-sharing-revoked",
      message: "riceballmama stopped sharing this agent",
      canRetry: false
    });
    expect(rendered.result.current.hasSentUserMessage).toBe(true);
    expect(rendered.result.current.composerGate).toMatchObject({
      runtime: {
        status: "blocked",
        reason: "session_runtime",
        sessionRuntimeReason: "agent_sharing_revoked"
      },
      editor: { status: "blocked", reason: "runtime_blocked" },
      submission: { status: "blocked", reason: "runtime_blocked" }
    });
  });
});

interface ExactInteractionPromptIdentity {
  agentSessionId?: string | null;
  requestId?: string | null;
  turnId?: string | null;
}
function useExactInteractionReadiness(input: {
  prompt: ExactInteractionPromptIdentity | null;
  source?: AgentGUIInteractionReadinessSource | null;
  workspaceId: string;
}) {
  const identity = useMemo(
    () =>
      resolveAgentGUIInteractionReadinessIdentity({
        agentSessionId: input.prompt?.agentSessionId,
        requestId: input.prompt?.requestId,
        turnId: input.prompt?.turnId,
        workspaceId: input.workspaceId
      }),
    [
      input.prompt?.agentSessionId,
      input.prompt?.requestId,
      input.prompt?.turnId,
      input.workspaceId
    ]
  );
  const readiness = useAgentGUIInteractionReadiness({
    identity,
    required: input.prompt !== null,
    source: input.source
  });
  return {
    block: readiness?.status === "blocked" ? readiness : null,
    ownsInteraction: input.source != null && input.prompt !== null
  };
}
function useAgentGUISessionPresentation(input: SessionPresentationInput) {
  return buildSessionPresentation(input, {
    interactiveReadiness: useExactInteractionReadiness({
      prompt: input.serverInteractivePrompt,
      source: input.interactionReadinessSource,
      workspaceId: input.workspaceId
    }),
    approvalReadiness: useExactInteractionReadiness({
      prompt: input.pendingApproval,
      source: input.interactionReadinessSource,
      workspaceId: input.workspaceId
    }),
    targetConnection: useAgentGUITargetConnectionState({
      agentTargetId: input.targetConnectionAgentTargetId,
      source: input.targetConnectionSource
    }),
    observationGap: useAgentObservationGap(
      input.activeCanonicalSession?.agentSessionId ??
        input.activeConversationId,
      input.activeEngineActiveTurn?.turnId,
      input.observationGapSource
    )
  });
}
