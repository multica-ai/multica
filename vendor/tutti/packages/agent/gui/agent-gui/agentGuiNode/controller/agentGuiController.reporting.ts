import type {
  AgentActivityDisplayStatus,
  AgentActivityMessage,
  AgentActivitySubmitDiagnostics,
  AgentActivityTurn,
  CanonicalAgentSession
} from "@tutti-os/agent-activity-core";
import { toast } from "@tutti-os/ui-system";
import { type AgentGUIRuntime } from "../../../agentActivityRuntime";
import type { AgentHostToastApi } from "../../../host/agentHostApi";
import type { AgentConversationVM } from "../../../shared/agentConversation/contracts/agentConversationVM";
import { createAgentClientId } from "../../../shared/agentConversation/agentClientIdentity";
import type { AgentSessionState } from "../../../shared/agentSessionTypes";
import type { AgentPromptContentBlock } from "../../../shared/contracts/dto";
import { agentPromptContentHasImage } from "../model/agentComposerDraft";
import {
  type AgentGUIApprovalRequest,
  type AgentGUIConversationSummary,
  type AgentGUIInteractivePrompt
} from "../model/agentGuiConversationModel";
import {
  agentGUIActivityTurnDiagnosticDetails,
  agentGUIConversationDiagnosticDetails,
  agentGUIRuntimeSessionDiagnosticDetails,
  agentGUISessionStateDiagnosticDetails,
  promptRequestId
} from "./agentGuiController.diagnostics";
import {
  getAgentGUIErrorCode,
  normalizeAgentGUIDiagnosticError
} from "./agentGuiController.errors";
export {
  normalizePermissionModeSemantic,
  permissionConfigFromComposerOptions,
  permissionModeDescription,
  permissionModeLabel,
  permissionModeOptions
} from "./agentGuiController.composerHelpers";
export * from "./agentGuiController.conversationHelpers";
export {
  agentGUIConversationDiagnosticDetails,
  agentGUIRuntimeSessionDiagnosticDetails,
  agentGUISessionStateDiagnosticDetails,
  agentGUIToolCallStatusIsWaiting,
  promptRequestId
} from "./agentGuiController.diagnostics";
export * from "./agentGuiController.draftMessageHelpers";
export * from "./agentGuiController.errors";
export {
  createAgentGUIConversationId,
  normalizeOptionalPrompt,
  normalizeOptionalText,
  projectAgentGUIMessagesToTimelineItems,
  recordValue,
  stringPayloadValue
} from "./agentGuiController.promptHelpers";
export * from "./agentGuiController.providerHelpers";
export {
  messageFromMessageUpdate,
  normalizeTimelineStatus,
  normalizedPositiveNumber,
  timelineItemTime
} from "./agentGuiController.sessionHelpers";
export * from "./agentGuiController.stableHelpers";
export {
  filterMessagesForDetailWindowOverlay,
  maxFiniteMessageVersion,
  minFiniteMessageVersion,
  sessionHasRenderableMessages,
  windowHasTurnMissingUserPrompt
} from "./useAgentConversationMessagePaging";
export type AgentGUIRuntimeErrorPhase =
  | "create_conversation"
  | "interrupt_current_turn"
  | "load_session_messages"
  | "load_session_state"
  | "synchronize_session"
  | "retry_activation"
  | "send_prompt"
  | "submit_interactive"
  | "toggle_conversation_pinned"
  | "rename_conversation"
  | "fork_conversation_through_turn"
  | "open_fork_source_conversation"
  | "delete_conversation"
  | "update_session_settings"
  | "warmup_openclaw_gateway";

export type AgentSubmitTraceState = {
  agentSessionId: string;
  blockCount: number;
  clientSubmitId: string;
  hasImage: boolean;
  promptLength: number;
  queued: boolean;
  startedAtUnixMs: number;
  turnId: string | null;
};

export function reportAgentGUIRuntimeError(input: {
  agentSessionId?: string | null;
  context?: Record<string, unknown>;
  error: unknown;
  phase: AgentGUIRuntimeErrorPhase;
  provider?: string | null;
  requestId?: number | string | null;
  runtime: AgentGUIRuntime;
  workspaceId: string;
}): void {
  const reportDiagnostic = input.runtime.reportDiagnostic;
  if (!reportDiagnostic) {
    return;
  }
  const details: Record<string, unknown> = {
    error: normalizeAgentGUIDiagnosticError(input.error),
    errorCode: getAgentGUIErrorCode(input.error),
    phase: input.phase,
    ...(input.agentSessionId ? { agentSessionId: input.agentSessionId } : {}),
    ...(input.provider ? { provider: input.provider } : {}),
    ...(input.requestId !== undefined && input.requestId !== null
      ? { requestId: input.requestId }
      : {}),
    ...(input.context ?? {})
  };
  try {
    void Promise.resolve(
      reportDiagnostic.call(input.runtime, {
        details,
        event: "agent.gui.caught_error",
        level: "error",
        source: "agent-gui",
        workspaceId: input.workspaceId
      })
    ).catch(() => {});
  } catch (reportError) {
    // Diagnostic logging must never affect the Agent GUI recovery path.
    console.error("[agent-gui] reportAgentGUIRuntimeError failed", reportError);
  }
}

export function showAgentGUIControllerErrorToast(
  hostToast: AgentHostToastApi | null | undefined,
  message: string
): void {
  if (hostToast?.error) {
    hostToast.error(message);
    return;
  }
  toast.error(message);
}

export function reportAgentGUIConversationFilterTargetUnresolved(input: {
  provider: string;
  agentTargetId: string | null;
  providerTargetCount: number;
  reason: "disabled" | "unresolved";
  runtime: AgentGUIRuntime;
  workspaceId: string;
}): void {
  const reportDiagnostic = input.runtime.reportDiagnostic;
  if (!reportDiagnostic) {
    return;
  }
  try {
    void Promise.resolve(
      reportDiagnostic.call(input.runtime, {
        details: {
          provider: input.provider,
          providerTargetCount: input.providerTargetCount,
          agentTargetId: input.agentTargetId,
          reason: input.reason
        },
        event: "agent.gui.conversation_filter.target_unresolved",
        level: "warn",
        source: "agent-gui",
        workspaceId: input.workspaceId
      })
    ).catch(() => {});
  } catch (reportError) {
    // Diagnostic logging must never affect conversation filter selection.
    console.error(
      "[agent-gui] reportAgentGUIConversationFilterTargetUnresolved failed",
      reportError
    );
  }
}

export function reportAgentGUIMessagePageDiagnostic(input: {
  agentSessionId: string;
  details?: Record<string, unknown>;
  event: string;
  level?: "debug" | "info" | "warn";
  messages?: readonly AgentActivityMessage[];
  runtime: AgentGUIRuntime;
  workspaceId: string;
}): void {
  const reportDiagnostic = input.runtime.reportDiagnostic;
  if (!reportDiagnostic) {
    return;
  }
  const versions = (input.messages ?? [])
    .map((message) => message.version)
    .filter((version) => Number.isFinite(version));
  try {
    void Promise.resolve(
      reportDiagnostic.call(input.runtime, {
        details: {
          agentSessionId: input.agentSessionId,
          ...(input.messages
            ? {
                firstVersion: versions.length ? Math.min(...versions) : null,
                lastVersion: versions.length ? Math.max(...versions) : null,
                messageCount: input.messages.length
              }
            : {}),
          ...(input.details ?? {})
        },
        event: input.event,
        level: input.level ?? "info",
        source: "agent-gui",
        workspaceId: input.workspaceId
      })
    ).catch(() => {});
  } catch (reportError) {
    // Diagnostic logging must never affect message loading.
    console.error(
      "[agent-gui] reportAgentGUIMessagePageDiagnostic failed",
      reportError
    );
  }
}

export function reportAgentGUIRenderStateDiagnostic(input: {
  activeActivityDisplayStatus: AgentActivityDisplayStatus | null;
  activeConversation: AgentGUIConversationSummary | null;
  activeConversationBusy: boolean;
  activeConversationId: string | null;
  activeEngineActiveTurn: AgentActivityTurn | null;
  activeEngineAvailability: "available" | "blocked" | "missing";
  activeEngineLatestTurn: AgentActivityTurn | null;
  activeEngineRuntimeActivity: "idle" | "running";
  isAwaitingTurnStart: boolean;
  activeLiveState: "inactive" | "activating" | "active" | "failed";
  activeRuntimeSession: CanonicalAgentSession | null;
  activeSessionState: AgentSessionState | null;
  activeSubmitBlocked: boolean;
  canQueueWhileBusy: boolean;
  canSubmit: boolean;
  conversation: AgentConversationVM | null;
  isCreatingConversation: boolean;
  isLoadingMessages: boolean;
  isSubmitting: boolean;
  pendingApproval: AgentGUIApprovalRequest | null;
  pendingInteractivePrompt: AgentGUIInteractivePrompt | null;
  runtime: AgentGUIRuntime;
  workspaceId: string;
}): void {
  const reportDiagnostic = input.runtime.reportDiagnostic;
  if (!reportDiagnostic) {
    return;
  }
  try {
    void Promise.resolve(
      reportDiagnostic.call(input.runtime, {
        details: {
          activeActivityDisplayStatus: input.activeActivityDisplayStatus,
          activeConversationBusy: input.activeConversationBusy,
          activeConversationId: input.activeConversationId,
          activeConversationStatus: input.activeConversation?.status ?? null,
          activeEngineActiveTurn: agentGUIActivityTurnDiagnosticDetails(
            input.activeEngineActiveTurn
          ),
          activeEngineAvailability: input.activeEngineAvailability,
          activeEngineLatestTurn: agentGUIActivityTurnDiagnosticDetails(
            input.activeEngineLatestTurn
          ),
          activeEngineRuntimeActivity: input.activeEngineRuntimeActivity,
          isAwaitingTurnStart: input.isAwaitingTurnStart,
          activeLiveState: input.activeLiveState,
          activeSubmitBlocked: input.activeSubmitBlocked,
          canQueueWhileBusy: input.canQueueWhileBusy,
          canSubmit: input.canSubmit,
          conversation: agentGUIConversationDiagnosticDetails(
            input.conversation
          ),
          isCreatingConversation: input.isCreatingConversation,
          isLoadingMessages: input.isLoadingMessages,
          isSubmitting: input.isSubmitting,
          pendingApprovalRequestId: input.pendingApproval?.requestId ?? null,
          pendingInteractivePromptKind:
            input.pendingInteractivePrompt?.kind ?? null,
          pendingInteractivePromptRequestId: promptRequestId(
            input.pendingInteractivePrompt
          ),
          runtimeSession: agentGUIRuntimeSessionDiagnosticDetails(
            input.activeRuntimeSession
          ),
          sessionState: agentGUISessionStateDiagnosticDetails(
            input.activeSessionState
          )
        },
        event: "agent.gui.node.render_state_changed",
        level: "info",
        source: "agent-gui",
        workspaceId: input.workspaceId
      })
    ).catch(() => {});
  } catch (reportError) {
    // Diagnostic logging must never affect Agent GUI rendering.
    console.error(
      "[agent-gui] reportAgentGUIRenderStateDiagnostic failed",
      reportError
    );
  }
}

export function reportAgentGUIActiveConversationCleared(input: {
  details?: Record<string, unknown>;
  previousAgentSessionId: string | null;
  reason: string;
  runtime: AgentGUIRuntime;
  workspaceId: string;
}): void {
  const reportDiagnostic = input.runtime.reportDiagnostic;
  if (!reportDiagnostic) {
    return;
  }
  try {
    void Promise.resolve(
      reportDiagnostic.call(input.runtime, {
        details: {
          previousAgentSessionId: input.previousAgentSessionId,
          reason: input.reason,
          ...(input.details ?? {})
        },
        event: "agent.gui.active_conversation.cleared",
        level: "info",
        source: "agent-gui",
        workspaceId: input.workspaceId
      })
    ).catch(() => {});
  } catch (reportError) {
    // Diagnostic logging must never affect active conversation routing.
    console.error(
      "[agent-gui] reportAgentGUIActiveConversationCleared failed",
      reportError
    );
  }
}

export function reportAgentGUIConversationListProjectionSkipped(input: {
  activeConversationId: string | null;
  currentUserIdPresent: boolean;
  dataLastActiveAgentSessionId: string | null;
  isComposerHome: boolean;
  provider: string | null;
  reason: string;
  runtime: AgentGUIRuntime;
  workspaceId: string;
  workspaceIdPresent: boolean;
}): void {
  const reportDiagnostic = input.runtime.reportDiagnostic;
  if (!reportDiagnostic) {
    return;
  }
  try {
    void Promise.resolve(
      reportDiagnostic.call(input.runtime, {
        details: {
          activeConversationId: input.activeConversationId,
          currentUserIdPresent: input.currentUserIdPresent,
          dataLastActiveAgentSessionId: input.dataLastActiveAgentSessionId,
          isComposerHome: input.isComposerHome,
          provider: input.provider,
          reason: input.reason,
          workspaceIdPresent: input.workspaceIdPresent
        },
        event: "agent.gui.conversation_list_projection.skipped",
        level: "info",
        source: "agent-gui",
        workspaceId: input.workspaceId
      })
    ).catch(() => {});
  } catch (reportError) {
    // Diagnostic logging must never affect active conversation routing.
    console.error(
      "[agent-gui] reportAgentGUIConversationListProjectionSkipped failed",
      reportError
    );
  }
}

export function reportAgentGUISubmitWithoutActiveConversation(input: {
  blockCount: number;
  conversationCount: number;
  conversationListQueryReady: boolean;
  dataLastActiveAgentSessionId: string | null;
  isComposerHome: boolean;
  promptLength: number;
  provider: string | null;
  runtime: AgentGUIRuntime;
  workspaceId: string;
}): void {
  const reportDiagnostic = input.runtime.reportDiagnostic;
  if (!reportDiagnostic) {
    return;
  }
  try {
    void Promise.resolve(
      reportDiagnostic.call(input.runtime, {
        details: {
          blockCount: input.blockCount,
          conversationCount: input.conversationCount,
          conversationListQueryReady: input.conversationListQueryReady,
          dataLastActiveAgentSessionId: input.dataLastActiveAgentSessionId,
          isComposerHome: input.isComposerHome,
          promptLength: input.promptLength,
          provider: input.provider
        },
        event: "agent.gui.submit.without_active_conversation",
        level: "warn",
        source: "agent-gui",
        workspaceId: input.workspaceId
      })
    ).catch(() => {});
  } catch (reportError) {
    // Diagnostic logging must never affect active conversation routing.
    console.error(
      "[agent-gui] reportAgentGUISubmitWithoutActiveConversation failed",
      reportError
    );
  }
}

export function reportAgentGUISubmitRecoveredActiveConversation(input: {
  blockCount: number;
  conversationCount: number;
  conversationListQueryReady: boolean;
  promptLength: number;
  provider: string | null;
  recoveredAgentSessionId: string;
  runtime: AgentGUIRuntime;
  workspaceId: string;
}): void {
  const reportDiagnostic = input.runtime.reportDiagnostic;
  if (!reportDiagnostic) {
    return;
  }
  try {
    void Promise.resolve(
      reportDiagnostic.call(input.runtime, {
        details: {
          blockCount: input.blockCount,
          conversationCount: input.conversationCount,
          conversationListQueryReady: input.conversationListQueryReady,
          promptLength: input.promptLength,
          provider: input.provider,
          recoveredAgentSessionId: input.recoveredAgentSessionId
        },
        event: "agent.gui.submit.recovered_active_conversation",
        level: "info",
        source: "agent-gui",
        workspaceId: input.workspaceId
      })
    ).catch(() => {});
  } catch (reportError) {
    // Diagnostic logging must never affect active conversation routing.
    console.error(
      "[agent-gui] reportAgentGUISubmitRecoveredActiveConversation failed",
      reportError
    );
  }
}

export function reportAgentSubmitTraceDiagnostic(input: {
  event: string;
  runtime: AgentGUIRuntime;
  trace: AgentSubmitTraceState;
  workspaceId: string;
  fields?: Record<string, unknown>;
}): void {
  const reportDiagnostic = input.runtime.reportDiagnostic;
  if (!reportDiagnostic) {
    return;
  }
  const nowUnixMs = Date.now();
  try {
    void Promise.resolve(
      reportDiagnostic.call(input.runtime, {
        details: {
          agentSessionId: input.trace.agentSessionId,
          blockCount: input.trace.blockCount,
          clientSubmitId: input.trace.clientSubmitId,
          elapsedMs: Math.max(0, nowUnixMs - input.trace.startedAtUnixMs),
          hasImage: input.trace.hasImage,
          promptLength: input.trace.promptLength,
          queued: input.trace.queued,
          startedAtUnixMs: input.trace.startedAtUnixMs,
          traceEvent: input.event,
          turnId: input.trace.turnId,
          ...(input.fields ?? {})
        },
        event: "agent.submit.trace",
        level: "info",
        source: "agent-gui",
        workspaceId: input.workspaceId
      })
    ).catch(() => {});
  } catch (reportError) {
    // Diagnostic logging must never affect the Agent GUI submit path.
    console.error(
      "[agent-gui] reportAgentSubmitTraceDiagnostic failed",
      reportError
    );
  }
}

export function scheduleAgentSubmitTracePaint(input: {
  event?: string;
  runtime: AgentGUIRuntime;
  trace: AgentSubmitTraceState;
  workspaceId: string;
}): void {
  const logPaint = () =>
    reportAgentSubmitTraceDiagnostic({
      event: input.event ?? "optimistic_user_message_painted",
      runtime: input.runtime,
      trace: input.trace,
      workspaceId: input.workspaceId
    });
  const requestFrame = globalThis.requestAnimationFrame;
  if (typeof requestFrame !== "function") {
    // timing: requestAnimationFrame fallback for non-browser runtimes; defer to next tick
    setTimeout(logPaint, 0);
    return;
  }
  requestFrame(() => requestFrame(logPaint));
}

export function createAgentSubmitTraceState(input: {
  agentSessionId: string;
  content: readonly AgentPromptContentBlock[];
  prompt: string;
  queued: boolean;
  startedAtUnixMs: number;
}): AgentSubmitTraceState {
  return {
    agentSessionId: input.agentSessionId,
    blockCount: input.content.length,
    clientSubmitId: createAgentSubmitTraceId(),
    hasImage: agentPromptContentHasImage(input.content),
    promptLength: input.prompt.length,
    queued: input.queued,
    startedAtUnixMs: input.startedAtUnixMs,
    turnId: null
  };
}

export function agentSubmitTraceDiagnostics(
  trace: AgentSubmitTraceState
): AgentActivitySubmitDiagnostics {
  return {
    submittedAtUnixMs: trace.startedAtUnixMs,
    blockCount: trace.blockCount,
    hasImage: trace.hasImage,
    promptLength: trace.promptLength,
    queued: trace.queued,
    source: "agent-gui"
  };
}

export function createAgentSubmitTraceId(): string {
  return createAgentClientId();
}

/**
 * Rewrites content for handoff to the runtime: pasted-text file blocks become a
 * codex-style "read this file" instruction (path embedded). Applied at every
 * runtime send boundary (new-session createSession + existing-session sendInput)
 * so no `file` block reaches the desktop tuttid pipeline, while the draft/queue
 * keep the structured block for the chip and edit-restore. The translated copy
 * is resolved here (controller layer), never in the model.
 */
