import {
  selectEngineTurnsForSession,
  type AgentActivityInteraction,
  type AgentActivityMessage,
  type AgentSessionFamilySnapshot,
  type AgentActivityTurn,
  type AgentSessionEngine,
  type EngineQueuedPrompt,
  type PromptQueueInFlightCommand
} from "@tutti-os/agent-activity-core";
import { useEffect, useMemo, useRef } from "react";
import type { AgentGUIRuntime } from "../../../agentActivityRuntime";
import type { AgentConversationVM } from "../../../shared/agentConversation/contracts/agentConversationVM";
import type {
  AgentPromptContentBlock,
  AppErrorCode
} from "../../../shared/contracts/dto";
import { useEngineSelector } from "../../../shared/engine/useEngineSelector";
import type { AgentGUIConversationSummary } from "../model/agentGuiConversationModel";
import {
  buildAgentGUIConversationModels,
  type AgentGUIConversationProjectionSource,
  type AgentGUIInteractivePrompt
} from "../model/agentGuiConversationModel";
import type {
  AgentComposerDraft,
  AgentGUIQueueStatus,
  AgentGUIQueuedPromptVM
} from "../model/agentGuiNodeTypes";
import type { AgentGUIComposerTargetData } from "./agentGuiController.composerPresentation";
import { providerSkillsFromComposerOptions } from "./agentGuiController.composerHelpers";
import {
  interactiveApprovalFromInteraction,
  interactivePromptFromInteraction
} from "./agentGuiController.interactiveHelpers";
import { isNonRetryableResumeErrorCode } from "./agentGuiController.errors";
import { reportAgentGUIMessagePageDiagnostic } from "./agentGuiController.reporting";
import {
  useStableConversationDetail,
  useStableProviderSkillOptions
} from "./agentGuiController.stableHelpers";
import {
  EMPTY_AGENT_COMPOSER_DRAFT,
  readAgentComposerDraftContent
} from "./agentGuiController.draftMessageHelpers";
import { minFiniteMessageVersion } from "./useAgentConversationMessagePaging";
import { resolveAgentComposerDraftScopeKey } from "../model/agentComposerDraftScope";
import { agentComposerDraftPrompt } from "../model/agentComposerDraft";
import { resolvePendingQuestionComposerTarget } from "./pendingQuestionComposerResponse";

interface UseAgentGUIConversationDetailInput {
  activeCancelStatus: string | null;
  activeConversation: AgentGUIConversationSummary | null;
  activeConversationId: string | null;
  activeConversationLiveState: "inactive" | "activating" | "active" | "failed";
  activeEngineError: string | null;
  activeMessages: readonly AgentActivityMessage[];
  activePendingInteractions: readonly AgentActivityInteraction[];
  activeQueuedPromptInFlight: PromptQueueInFlightCommand | null;
  activeQueuedPrompts: readonly EngineQueuedPrompt[];
  activeQueueStatus: AgentGUIQueueStatus;
  activeSessionFamily: AgentSessionFamilySnapshot;
  activeSessionReconcileError: string | null;
  activeSessionView: {
    hasOlderMessages: boolean;
    isLoadingOlderMessages: boolean;
    olderMessageCount: number;
    oldestLoadedVersion: number | null;
  } | null;
  activeTimelineItems: Parameters<
    typeof buildAgentGUIConversationModels
  >[0]["timelineItems"];
  activeTurn: AgentActivityTurn | null;
  agentActivityRuntime: AgentGUIRuntime;
  avoidGroupingEdits: boolean;
  codeFor(agentSessionId: string | null): AppErrorCode | null;
  detailError: string | null;
  draftByScopeKey: Record<string, AgentComposerDraft>;
  errorFor(agentSessionId: string | null): string | null;
  providerComposerOptions: Parameters<
    typeof providerSkillsFromComposerOptions
  >[0];
  selectedComposerTargetData: AgentGUIComposerTargetData;
  selectedProjectPath: string | null;
  projectedSessionMessagesById: Readonly<
    Record<string, readonly AgentActivityMessage[]>
  >;
  sessionEngine: AgentSessionEngine;
  workspaceId: string;
  workspacePath: string;
}

export function useAgentGUIConversationDetail(
  input: UseAgentGUIConversationDetailInput
) {
  const projectionConversationRef =
    useRef<AgentGUIConversationProjectionSource | null>(null);
  const activeCanonicalLiveTurn = Boolean(
    input.activeTurn && input.activeTurn.phase !== "settled"
  );
  const sessionTurns = useEngineSelector(
    input.sessionEngine,
    (state) => selectEngineTurnsForSession(state, input.activeConversationId),
    referenceArrayEqual
  );
  const projectionConversation =
    useMemo<AgentGUIConversationProjectionSource | null>(() => {
      if (!input.activeConversation) {
        projectionConversationRef.current = null;
        return null;
      }
      const current = input.activeConversation;
      const previous = projectionConversationRef.current;
      if (
        previous &&
        previous.id === current.id &&
        previous.userId === current.userId &&
        previous.provider === current.provider &&
        previous.title === current.title &&
        previous.titleFallback === current.titleFallback &&
        previous.status === current.status &&
        previous.cwd === current.cwd &&
        previous.activeTurn === input.activeTurn &&
        previous.sessionTurns === sessionTurns
      ) {
        return previous;
      }
      const next = {
        id: current.id,
        userId: current.userId,
        provider: current.provider,
        title: current.title,
        titleFallback: current.titleFallback,
        status: current.status,
        cwd: current.cwd,
        updatedAtUnixMs: current.updatedAtUnixMs,
        activeTurn: input.activeTurn,
        sessionTurns
      };
      projectionConversationRef.current = next;
      return next;
    }, [input.activeConversation, input.activeTurn, sessionTurns]);

  const draftContent = input.activeConversationId
    ? (input.draftByScopeKey[
        resolveAgentComposerDraftScopeKey({
          agentSessionId: input.activeConversationId
        })
      ] ?? EMPTY_AGENT_COMPOSER_DRAFT)
    : readAgentComposerDraftContent({
        drafts: input.draftByScopeKey
      });
  const availableCommands = useMemo(
    () => (input.providerComposerOptions?.commands ?? []).map((command) => ({ ...command })),
    [input.providerComposerOptions?.commands]
  );
  const availableSkills = useStableProviderSkillOptions(
    useMemo(
      () => providerSkillsFromComposerOptions(input.providerComposerOptions),
      [input.providerComposerOptions]
    )
  );
  const conversationModels = useMemo(() => {
    if (!projectionConversation) {
      return { conversation: null, detail: null };
    }
    return buildAgentGUIConversationModels({
      timelineItems: input.activeTimelineItems,
      conversation: projectionConversation,
      canonicalSession: input.activeSessionFamily.rootSession,
      childSessions: input.activeSessionFamily.childSessions,
      childMessagesBySessionId: input.projectedSessionMessagesById,
      workspaceRoot: input.workspacePath,
      avoidGroupingEdits: input.avoidGroupingEdits
    });
  }, [
    input.activeTimelineItems,
    input.activeSessionFamily.childSessions,
    input.activeSessionFamily.rootSession,
    input.avoidGroupingEdits,
    input.projectedSessionMessagesById,
    input.workspacePath,
    projectionConversation
  ]);
  const conversationDetail = useStableConversationDetail(
    conversationModels.detail
  );
  const conversation = useMemo<AgentConversationVM | null>(() => {
    if (!conversationModels.conversation) return null;
    if (
      conversationDetail &&
      (conversationModels.conversation.sourceDetail !== conversationDetail ||
        conversationModels.conversation.activity !==
          conversationDetail.activity)
    ) {
      return {
        ...conversationModels.conversation,
        activity: conversationDetail.activity,
        sourceDetail: conversationDetail
      };
    }
    return conversationModels.conversation;
  }, [conversationDetail, conversationModels.conversation]);

  const diagnosticKeyRef = useRef<string | null>(null);
  useEffect(() => {
    if (!input.activeConversationId || !conversation) {
      diagnosticKeyRef.current = null;
      return;
    }
    const firstVersion = minFiniteMessageVersion(input.activeMessages);
    const diagnosticKey = [
      input.activeConversationId,
      input.activeMessages.length,
      input.activeTimelineItems.length,
      conversation.sourceDetail.turns.length,
      conversation.rows.length,
      firstVersion ?? "",
      input.activeSessionView?.hasOlderMessages ? "1" : "0",
      input.activeSessionView?.isLoadingOlderMessages ? "1" : "0"
    ].join(":");
    if (diagnosticKeyRef.current === diagnosticKey) return;
    diagnosticKeyRef.current = diagnosticKey;
    reportAgentGUIMessagePageDiagnostic({
      agentSessionId: input.activeConversationId,
      details: {
        detailMessageCount: input.activeSessionView?.olderMessageCount ?? 0,
        hasOlderMessages: input.activeSessionView?.hasOlderMessages ?? false,
        isLoadingOlderMessages:
          input.activeSessionView?.isLoadingOlderMessages ?? false,
        oldestLoadedVersion:
          input.activeSessionView?.oldestLoadedVersion ?? null,
        rowCount: conversation.rows.length,
        timelineItemCount: input.activeTimelineItems.length,
        turnCount: conversation.sourceDetail.turns.length
      },
      event: "agent.gui.conversation.projection.resolved",
      level: "debug",
      messages: input.activeMessages,
      runtime: input.agentActivityRuntime,
      workspaceId: input.workspaceId
    });
  }, [
    input.activeConversationId,
    input.activeMessages,
    input.activeSessionView,
    input.activeTimelineItems,
    input.agentActivityRuntime,
    input.workspaceId,
    conversation
  ]);

  const activeLiveState =
    input.activeConversationLiveState === "inactive" && activeCanonicalLiveTurn
      ? "active"
      : input.activeConversationLiveState;
  const activationError = input.errorFor(input.activeConversationId);
  const activationErrorCode = input.codeFor(input.activeConversationId);
  const hasProviderSessionNotFoundError =
    isNonRetryableResumeErrorCode(activationErrorCode);
  const rawPendingApproval = useMemo(() => {
    const interaction =
      [...input.activePendingInteractions]
        .reverse()
        .find((candidate) => candidate.kind === "approval") ?? null;
    return interactiveApprovalFromInteraction(interaction);
  }, [input.activePendingInteractions]);
  const rawPendingInteractivePrompt =
    useMemo<AgentGUIInteractivePrompt | null>(() => {
      const interaction =
        [...input.activePendingInteractions]
          .reverse()
          .find((candidate) => candidate.kind !== "approval") ?? null;
      return interactivePromptFromInteraction(interaction);
    }, [input.activePendingInteractions]);
  const pendingQuestionComposerTarget = useMemo(
    () =>
      resolvePendingQuestionComposerTarget({
        activeTurnId: input.activeTurn?.turnId ?? "",
        agentSessionId: input.activeConversationId ?? "",
        pendingInteractions: input.activePendingInteractions
      }),
    [
      input.activeConversationId,
      input.activePendingInteractions,
      input.activeTurn?.turnId
    ]
  );
  const queuedPrompts = useMemo<AgentGUIQueuedPromptVM[]>(
    () =>
      input.activeConversationId
        ? input.activeQueuedPrompts.map((prompt) => ({
            id: prompt.id,
            content: [...prompt.content] as AgentPromptContentBlock[],
            ...(prompt.displayPrompt
              ? { displayPrompt: prompt.displayPrompt }
              : {}),
            createdAtUnixMs: prompt.createdAtUnixMs
          }))
        : [],
    [input.activeConversationId, input.activeQueuedPrompts]
  );

  return {
    activeLiveState,
    activationError,
    activationErrorCode,
    availableCommands,
    availableSkills,
    conversation,
    conversationDetail,
    draftContent,
    draftPrompt: agentComposerDraftPrompt(draftContent),
    drainingQueuedPromptId:
      input.activeQueuedPromptInFlight?.kind === "send"
        ? input.activeQueuedPromptInFlight.promptId
        : null,
    effectiveDetailError:
      input.detailError ??
      input.activeSessionReconcileError ??
      (input.activeConversationId !== null ? input.activeEngineError : null),
    hasProviderSessionNotFoundError,
    isCancelPending: input.activeCancelStatus === "awaitingTurn",
    isInterrupting:
      input.activeCancelStatus === "requested" ||
      input.activeCancelStatus === "accepted",
    pendingApproval: hasProviderSessionNotFoundError
      ? null
      : rawPendingApproval,
    queueStatus: input.activeQueueStatus,
    queuedPrompts,
    pendingQuestionComposerTarget: hasProviderSessionNotFoundError
      ? null
      : pendingQuestionComposerTarget,
    serverInteractivePrompt: hasProviderSessionNotFoundError
      ? null
      : rawPendingInteractivePrompt
  };
}

function referenceArrayEqual<T>(
  left: readonly T[],
  right: readonly T[]
): boolean {
  return (
    left.length === right.length &&
    left.every((value, index) => value === right[index])
  );
}
