import {
  type AgentActivityInteraction,
  type AgentActivityTurn,
  type AgentSessionEngine
} from "@tutti-os/agent-activity-core";
import type { Dispatch, RefObject, SetStateAction } from "react";
import { useCallback, useEffect, useRef } from "react";
import type { AgentGUIRuntime } from "../../../agentActivityRuntime";
import { getActiveUiLanguage, translate } from "../../../i18n/index";
import { promptHasAgentMention, promptSubmissionError } from "@multica/tutti-agent-gui-host/prompt-submission-policy";
import type { AgentPromptContentBlock } from "../../../shared/contracts/dto";
import type { AgentInteractionResponseInput } from "../../../shared/agentConversation/contracts/agentConversationVM";
import type {
  AgentGUINodeData,
  AgentGUIInteractionReadinessSource
} from "../../../types";
import {
  agentPromptContentDisplayText,
  agentPromptContentHasImage,
  emptyAgentComposerDraft,
  normalizeAgentPromptContentBlocks,
  snapshotAgentComposerDraft
} from "../model/agentComposerDraft";
import type {
  AgentComposerDraft,
  SubmittedDraftSnapshot
} from "../model/agentGuiNodeTypes";
import { resolveAgentComposerDraftScopeKey } from "../model/agentComposerDraftScope";
import type { AgentGUIConversationSummary } from "../model/agentGuiConversationModel";
import type { AgentComposerSubmitOptions } from "../composer/AgentComposer.types";
import {
  clearSubmittedDraftIfUnchanged,
  deleteUnacceptedSubmittedDraftSnapshot,
  toRuntimeSendContent
} from "./agentGuiController.draftMessageHelpers";
import { clearSubmittedAgentGUIHomeDraft } from "./agentGuiController.homeDraftHelpers";
import {
  agentGUIInteractionDraftSettlementKey,
  AgentGUIEngineSettlementController,
  type AgentGUIInteractionDraftPendingSettlement
} from "./AgentGUIEngineSettlementController";
import {
  AGENT_RESUME_SESSION_NOT_LOCAL_ERROR,
  agentGUISubmitSettlementError,
  buildProviderSessionNotFoundActivationError,
  buildResumeSessionNotLocalActivationError,
  getAgentGUIErrorMessage,
  isNonRetryableResumeErrorCode
} from "./agentGuiController.errors";
import {
  agentSubmitTraceDiagnostics,
  createAgentSubmitTraceState,
  reportAgentGUISubmitRecoveredActiveConversation,
  reportAgentGUISubmitWithoutActiveConversation,
  reportAgentSubmitTraceDiagnostic,
  scheduleAgentSubmitTracePaint
} from "./agentGuiController.reporting";
import { resolveAgentGUIInteractionReadinessIdentity } from "./agentGuiController.interactionHelpers";
import { readAgentGUIInteractionReadiness } from "./useAgentGUIInteractionReadiness";
import {
  resolveConversationSummaryById,
  type ConversationIntent
} from "./useAgentConversationSelection";
import type { useAgentGUIActivation } from "./useAgentGUIActivation";
import type { AgentGUINewConversationActivationResult } from "./agentGuiNewConversationActivation.types";
import { resolvePendingQuestionComposerResponse } from "./pendingQuestionComposerResponse";

interface UseAgentGUISubmitInteractionActionsInput {
  activation: ReturnType<typeof useAgentGUIActivation>;
  activeConversationId: string | null;
  activeConversationIdRef: RefObject<string | null>;
  activeEngineActiveTurn: AgentActivityTurn | null;
  activeEnginePendingInteractions: readonly AgentActivityInteraction[];
  agentActivityRuntime: AgentGUIRuntime;
  conversationListQuery: unknown | null;
  conversationsRef: RefObject<AgentGUIConversationSummary[]>;
  dataRef: RefObject<AgentGUINodeData>;
  draftByScopeKeyRef: RefObject<Record<string, AgentComposerDraft>>;
  executePromptRef: RefObject<
    (
      agentSessionId: string,
      content: AgentPromptContentBlock[],
      displayPrompt?: string,
      options?: {
        immediate?: boolean;
        requiredSettingsPatch?: AgentComposerSubmitOptions["requiredSettingsPatch"];
        sendNow?: boolean;
        submittedDraft?: AgentComposerSubmitOptions["submittedDraft"];
        targetTurnId?: AgentComposerSubmitOptions["targetTurnId"];
        sourceScopeKey?: string;
        trackDraft?: boolean;
      }
    ) => void
  >;
  isComposerHomeRef: RefObject<boolean>;
  isCurrentConversation(agentSessionId: string): boolean;
  isRespondingToInteraction: boolean;
  interactionReadinessSource?: AgentGUIInteractionReadinessSource | null;
  isSessionMarkedNonResumable(agentSessionId: string): boolean;
  persistActiveConversation(agentSessionId: string | null): void;
  promptImagesSupported: boolean;
  sessionEngine: AgentSessionEngine;
  setActiveConversationId: Dispatch<SetStateAction<string | null>>;
  setDetailError: Dispatch<SetStateAction<string | null>>;
  setDraftByScopeKey: Dispatch<
    SetStateAction<Record<string, AgentComposerDraft>>
  >;
  setIntent: Dispatch<SetStateAction<ConversationIntent>>;
  submittedDraftSnapshotsRef: RefObject<Record<string, SubmittedDraftSnapshot>>;
  startConversation(
    content: AgentPromptContentBlock[],
    displayPrompt?: string,
    options?: AgentComposerSubmitOptions,
    initialTurnExpected?: boolean
  ): AgentGUINewConversationActivationResult | null;
  submitPromptRef: RefObject<
    (
      content: AgentPromptContentBlock[],
      displayPrompt?: string,
      options?: AgentComposerSubmitOptions
    ) => void
  >;
  transientConversation: AgentGUIConversationSummary | null;
  workspaceId: string;
}

export function useAgentGUISubmitInteractionActions(
  input: UseAgentGUISubmitInteractionActionsInput
) {
  const {
    activation,
    activeConversationIdRef,
    activeEngineActiveTurn,
    activeEnginePendingInteractions,
    agentActivityRuntime,
    conversationListQuery,
    conversationsRef,
    dataRef,
    draftByScopeKeyRef,
    executePromptRef,
    isComposerHomeRef,
    isCurrentConversation,
    isRespondingToInteraction,
    interactionReadinessSource,
    isSessionMarkedNonResumable,
    persistActiveConversation,
    promptImagesSupported,
    sessionEngine,
    setActiveConversationId,
    setDetailError,
    setDraftByScopeKey,
    setIntent,
    submittedDraftSnapshotsRef,
    startConversation,
    submitPromptRef,
    transientConversation,
    workspaceId
  } = input;
  const interactionDraftSettlementsRef = useRef<
    Record<string, AgentGUIInteractionDraftPendingSettlement>
  >({});
  const retryActivation = useCallback(() => {
    const agentSessionId = activeConversationIdRef.current;
    if (!agentSessionId) {
      return;
    }
    if (isSessionMarkedNonResumable(agentSessionId)) {
      return;
    }
    if (isNonRetryableResumeErrorCode(activation.codeFor(agentSessionId))) {
      return;
    }
    setDetailError(null);
    activation.activate({ mode: "existing", agentSessionId });
  }, [
    agentActivityRuntime,
    activation,
    isCurrentConversation,
    isSessionMarkedNonResumable,
    workspaceId
  ]);

  const executePrompt = useCallback(
    (
      agentSessionId: string,
      content: AgentPromptContentBlock[],
      displayPrompt?: string,
      options?: {
        capabilityRefs?: AgentComposerSubmitOptions["capabilityRefs"];
        immediate?: boolean;
        requiredSettingsPatch?: AgentComposerSubmitOptions["requiredSettingsPatch"];
        sendNow?: boolean;
        submittedDraft?: AgentComposerSubmitOptions["submittedDraft"];
        targetTurnId?: AgentComposerSubmitOptions["targetTurnId"];
        sourceScopeKey?: string;
        trackDraft?: boolean;
      }
    ) => {
      const normalizedContent = normalizeAgentPromptContentBlocks(content);
      if (!agentSessionId || normalizedContent.length === 0) {
        return;
      }
      const submissionError = promptSubmissionError(options ?? {}, getActiveUiLanguage());
      if (submissionError) {
        setDetailError(submissionError);
        return;
      }
      const targetIsActiveConversation =
        activeConversationIdRef.current === agentSessionId;
      // displayPrompt(如 bundle 折叠成单 chip)优先用于回显;否则回退到 content 派生文本。
      const submittedPromptText =
        displayPrompt && displayPrompt.trim()
          ? displayPrompt
          : agentPromptContentDisplayText(normalizedContent);
      const submittedAtUnixMs = Date.now();
      const submitTrace = createAgentSubmitTraceState({
        agentSessionId,
        content: normalizedContent,
        prompt: submittedPromptText,
        queued: false,
        startedAtUnixMs: submittedAtUnixMs
      });
      if (options?.trackDraft === true) {
        const sourceScopeKey =
          options.sourceScopeKey ??
          resolveAgentComposerDraftScopeKey({ agentSessionId });
        const submittedDraft =
          options?.submittedDraft ??
          draftByScopeKeyRef.current[sourceScopeKey] ??
          emptyAgentComposerDraft();
        submittedDraftSnapshotsRef.current[submitTrace.clientSubmitId] = {
          sourceScopeKey,
          content: snapshotAgentComposerDraft(submittedDraft),
          targetAgentSessionId: agentSessionId
        };
      }
      const targetConversation = resolveConversationSummaryById(
        conversationsRef.current,
        agentSessionId,
        transientConversation
      );
      reportAgentSubmitTraceDiagnostic({
        event: "submit.begin",
        runtime: agentActivityRuntime,
        trace: submitTrace,
        workspaceId,
        fields: {
          activeConversationId: activeConversationIdRef.current,
          conversationKnown: targetConversation !== null,
          conversationStatus: targetConversation?.status ?? null,
          isComposerHome: isComposerHomeRef.current,
          targetIsActiveConversation,
          targetMode: "existing"
        }
      });
      const { accepted, queued } = sessionEngine.submitPrompt({
        agentSessionId,
        ...(options?.capabilityRefs?.length
          ? { capabilityRefs: options.capabilityRefs }
          : {}),
        clientSubmitId: submitTrace.clientSubmitId,
        content: normalizedContent,
        ...(displayPrompt && displayPrompt.trim() ? { displayPrompt } : {}),
        submitDiagnostics: agentSubmitTraceDiagnostics(submitTrace),
        ...(options?.requiredSettingsPatch
          ? {
              requiredSettingsPatch: {
                ...options.requiredSettingsPatch
              }
            }
          : {}),
        ...(options?.targetTurnId?.trim()
          ? { targetTurnId: options.targetTurnId.trim() }
          : {}),
        ...(options?.immediate === true
          ? { routing: "immediate" as const }
          : options?.sendNow === true
            ? { routing: "send_now" as const }
            : {}),
        runtimeContent: toRuntimeSendContent(normalizedContent)
      });
      submitTrace.queued = queued;
      setDetailError(null);
      // Clear the composer optimistically the instant the engine takes the
      // prompt — whether it was queued behind a busy turn or accepted straight
      // into an idle session. The snapshot is retained so
      // AgentGUIEngineSettlementController can restore it if the send is
      // later rejected. A submit the engine never accepted is left untouched so
      // its text is not lost (deleteUnacceptedSubmittedDraftSnapshot cleans up).
      const submittedSnapshot =
        submittedDraftSnapshotsRef.current[submitTrace.clientSubmitId];
      if ((accepted || queued) && submittedSnapshot) {
        setDraftByScopeKey((current) => {
          const next = clearSubmittedDraftIfUnchanged({
            drafts: current,
            snapshot: submittedSnapshot
          });
          draftByScopeKeyRef.current = next;
          return next;
        });
      }
      deleteUnacceptedSubmittedDraftSnapshot({
        snapshots: submittedDraftSnapshotsRef.current,
        clientSubmitId: submitTrace.clientSubmitId,
        accepted,
        queued
      });
      reportAgentSubmitTraceDiagnostic({
        event: "send_input.requested",
        runtime: agentActivityRuntime,
        trace: submitTrace,
        workspaceId
      });
      scheduleAgentSubmitTracePaint({
        runtime: agentActivityRuntime,
        trace: submitTrace,
        workspaceId
      });
    },
    [agentActivityRuntime, sessionEngine, setDraftByScopeKey, workspaceId]
  );

  useEffect(() => {
    executePromptRef.current = executePrompt;
  }, [executePrompt]);

  useEffect(() => {
    const controller = new AgentGUIEngineSettlementController({
      applyDraftUpdate: (update) => {
        setDraftByScopeKey((current) => {
          const next = update(current);
          draftByScopeKeyRef.current = next;
          return next;
        });
      },
      engine: sessionEngine,
      prompts: agentActivityRuntime.prompts,
      interactionDraftSettlements: interactionDraftSettlementsRef.current,
      isCurrentConversation,
      onSubmitFailed: (submit) => {
        setDetailError(
          getAgentGUIErrorMessage(agentGUISubmitSettlementError(submit))
        );
      },
      snapshots: submittedDraftSnapshotsRef.current
    });
    return controller.attach();
  }, [
    agentActivityRuntime,
    draftByScopeKeyRef,
    isCurrentConversation,
    sessionEngine,
    setDetailError,
    setDraftByScopeKey,
    submittedDraftSnapshotsRef
  ]);

  const submitExistingPrompt = useCallback(
    (
      agentSessionId: string,
      normalizedContent: AgentPromptContentBlock[],
      displayPromptText?: string,
      options?: {
        capabilityRefs?: AgentComposerSubmitOptions["capabilityRefs"];
        requiredSettingsPatch?: AgentComposerSubmitOptions["requiredSettingsPatch"];
        sendNow?: boolean;
        submittedDraft?: AgentComposerSubmitOptions["submittedDraft"];
        targetTurnId?: AgentComposerSubmitOptions["targetTurnId"];
        sourceScopeKey?: string;
        trackDraft?: boolean;
      }
    ) => {
      if (isSessionMarkedNonResumable(agentSessionId)) {
        setDetailError(
          getAgentGUIErrorMessage(buildResumeSessionNotLocalActivationError())
        );
        return;
      }
      if (isNonRetryableResumeErrorCode(activation.codeFor(agentSessionId))) {
        setDetailError(
          getAgentGUIErrorMessage(
            activation.codeFor(agentSessionId) ===
              AGENT_RESUME_SESSION_NOT_LOCAL_ERROR
              ? buildResumeSessionNotLocalActivationError(
                  activation.errorFor(agentSessionId)
                )
              : buildProviderSessionNotFoundActivationError(
                  activation.errorFor(agentSessionId)
                )
          )
        );
        return;
      }
      executePrompt(agentSessionId, normalizedContent, displayPromptText, {
        capabilityRefs: options?.capabilityRefs,
        requiredSettingsPatch: options?.requiredSettingsPatch,
        targetTurnId: options?.targetTurnId,
        sendNow: options?.sendNow === true,
        submittedDraft: options?.submittedDraft,
        sourceScopeKey: options?.sourceScopeKey,
        trackDraft: options?.trackDraft === true
      });
    },
    [activation, executePrompt, isSessionMarkedNonResumable, workspaceId]
  );

  const submitInteractivePrompt = useCallback(
    (input: AgentInteractionResponseInput): boolean => {
      const normalizedOptionId = input.optionId?.trim() ?? "";
      const target = resolveAgentGUIInteractionReadinessIdentity({
        agentSessionId: input.agentSessionId,
        requestId: input.requestId,
        turnId: input.turnId,
        workspaceId
      });
      const exactPendingInteraction =
        target !== null &&
        activeEnginePendingInteractions.some(
          (interaction) =>
            interaction.status === "pending" &&
            interaction.agentSessionId.trim() === target.agentSessionId &&
            interaction.turnId.trim() === target.turnId &&
            interaction.requestId.trim() === target.requestId
        );
      if (!target || !exactPendingInteraction || isRespondingToInteraction) {
        return false;
      }
      if (
        readAgentGUIInteractionReadiness({
          identity: target,
          source: interactionReadinessSource
        })?.status === "blocked"
      ) {
        return false;
      }
      setDetailError(null);
      return agentActivityRuntime.prompts.respondToInteraction({
        ...(input.action?.trim() ? { action: input.action.trim() } : {}),
        agentSessionId: target.agentSessionId,
        ...(normalizedOptionId ? { optionId: normalizedOptionId } : {}),
        ...(input.payload ? { payload: { ...input.payload } } : {}),
        requestId: target.requestId,
        turnId: target.turnId
      });
    },
    [
      activeEnginePendingInteractions,
      agentActivityRuntime,
      interactionReadinessSource,
      isRespondingToInteraction,
      workspaceId
    ]
  );

  const submitPrompt = useCallback(
    (
      content: AgentPromptContentBlock[],
      displayPrompt?: string,
      options?: AgentComposerSubmitOptions
    ) => {
      const agentSessionId = activeConversationIdRef.current;
      const normalizedContent = normalizeAgentPromptContentBlocks(content);
      if (normalizedContent.length === 0) {
        return;
      }
      const displayPromptText =
        displayPrompt && displayPrompt.trim() ? displayPrompt : undefined;
      if (
        !promptImagesSupported &&
        agentPromptContentHasImage(normalizedContent)
      ) {
        setDetailError(translate("agentHost.agentGui.promptImagesUnsupported"));
        return;
      }
      if (!agentSessionId) {
        if (!isComposerHomeRef.current) {
          const promptLength =
            agentPromptContentDisplayText(normalizedContent).length;
          reportAgentGUISubmitWithoutActiveConversation({
            blockCount: normalizedContent.length,
            conversationCount: conversationsRef.current.length,
            conversationListQueryReady: conversationListQuery !== null,
            dataLastActiveAgentSessionId:
              dataRef.current.lastActiveAgentSessionId ?? null,
            isComposerHome: isComposerHomeRef.current,
            promptLength,
            provider: dataRef.current.provider ?? null,
            runtime: agentActivityRuntime,
            workspaceId
          });
          const recoveredAgentSessionId =
            dataRef.current.lastActiveAgentSessionId?.trim() ?? "";
          if (recoveredAgentSessionId) {
            reportAgentGUISubmitRecoveredActiveConversation({
              blockCount: normalizedContent.length,
              conversationCount: conversationsRef.current.length,
              conversationListQueryReady: conversationListQuery !== null,
              promptLength,
              provider: dataRef.current.provider ?? null,
              recoveredAgentSessionId,
              runtime: agentActivityRuntime,
              workspaceId
            });
            activeConversationIdRef.current = recoveredAgentSessionId;
            setActiveConversationId(recoveredAgentSessionId);
            setIntent({ tag: "active", id: recoveredAgentSessionId });
            persistActiveConversation(recoveredAgentSessionId);
            submitExistingPrompt(
              recoveredAgentSessionId,
              normalizedContent,
              displayPromptText,
              {
                capabilityRefs: options?.capabilityRefs,
                requiredSettingsPatch: options?.requiredSettingsPatch,
                submittedDraft: options?.submittedDraft,
                sourceScopeKey: resolveAgentComposerDraftScopeKey({}),
                trackDraft: true
              }
            );
            return;
          }
        }
        const homeDraftKey = resolveAgentComposerDraftScopeKey({});
        const submittedHomeDraft = snapshotAgentComposerDraft(
          options?.submittedDraft ??
            draftByScopeKeyRef.current[homeDraftKey] ??
            emptyAgentComposerDraft()
        );
        const activationResult = startConversation(
          normalizedContent,
          displayPromptText,
          options
        );
        if (activationResult) {
          draftByScopeKeyRef.current = clearSubmittedAgentGUIHomeDraft({
            draftKey: homeDraftKey,
            drafts: draftByScopeKeyRef.current,
            submittedDraft: submittedHomeDraft
          });
          setDraftByScopeKey((current) =>
            clearSubmittedAgentGUIHomeDraft({
              draftKey: homeDraftKey,
              drafts: current,
              submittedDraft: submittedHomeDraft
            })
          );
        }
        return;
      }
      const activeTurnId = activeEngineActiveTurn?.turnId.trim() ?? "";
      const pendingQuestionResponse = promptHasAgentMention(normalizedContent) ? null : resolvePendingQuestionComposerResponse({
        activeTurnId,
        agentSessionId,
        content: normalizedContent,
        pendingInteractions: activeEnginePendingInteractions,
        submitOptions: options
      });
      if (pendingQuestionResponse) {
        const sourceScopeKey = resolveAgentComposerDraftScopeKey({
          agentSessionId
        });
        const submittedDraftSnapshot: SubmittedDraftSnapshot = {
          content: snapshotAgentComposerDraft(
            options?.submittedDraft ??
              draftByScopeKeyRef.current[sourceScopeKey] ??
              emptyAgentComposerDraft()
          ),
          sourceScopeKey,
          targetAgentSessionId: agentSessionId
        };
        const settlementKey = agentGUIInteractionDraftSettlementKey({
          agentSessionId,
          requestId: pendingQuestionResponse.requestId,
          turnId: activeTurnId
        });
        interactionDraftSettlementsRef.current[settlementKey] = {
          agentSessionId,
          requestId: pendingQuestionResponse.requestId,
          submittedDraftSnapshot,
          turnId: activeTurnId
        };
        if (!submitInteractivePrompt(pendingQuestionResponse)) {
          delete interactionDraftSettlementsRef.current[settlementKey];
          return;
        }
        setDraftByScopeKey((current) => {
          const next = clearSubmittedDraftIfUnchanged({
            drafts: current,
            snapshot: submittedDraftSnapshot
          });
          draftByScopeKeyRef.current = next;
          return next;
        });
        return;
      }
      submitExistingPrompt(
        agentSessionId,
        normalizedContent,
        displayPromptText,
        {
          capabilityRefs: options?.capabilityRefs,
          requiredSettingsPatch: options?.requiredSettingsPatch,
          submittedDraft: options?.submittedDraft,
          trackDraft: true
        }
      );
    },
    [
      activeEngineActiveTurn,
      activeEnginePendingInteractions,
      agentActivityRuntime,
      conversationListQuery,
      persistActiveConversation,
      promptImagesSupported,
      startConversation,
      submitExistingPrompt,
      submitInteractivePrompt,
      workspaceId
    ]
  );

  useEffect(() => {
    submitPromptRef.current = submitPrompt;
  }, [submitPrompt]);

  const submitGuidancePrompt = useCallback(
    (
      content: AgentPromptContentBlock[],
      displayPrompt?: string,
      options?: AgentComposerSubmitOptions
    ) => {
      const agentSessionId = activeConversationIdRef.current;
      const normalizedContent = normalizeAgentPromptContentBlocks(content);
      if (!agentSessionId || normalizedContent.length === 0) {
        return;
      }
      if (
        !promptImagesSupported &&
        agentPromptContentHasImage(normalizedContent)
      ) {
        setDetailError(translate("agentHost.agentGui.promptImagesUnsupported"));
        return;
      }
      const activeTurnId = activeEngineActiveTurn?.turnId.trim() ?? "";
      if (activeTurnId === "") {
        return;
      }
      const displayPromptText =
        displayPrompt && displayPrompt.trim() ? displayPrompt : undefined;
      submitExistingPrompt(
        agentSessionId,
        normalizedContent,
        displayPromptText,
        {
          capabilityRefs: options?.capabilityRefs,
          sendNow: true,
          submittedDraft: options?.submittedDraft,
          targetTurnId: activeTurnId,
          trackDraft: true
        }
      );
    },
    [
      activeEngineActiveTurn,
      promptImagesSupported,
      submitExistingPrompt,
      translate
    ]
  );

  const showPromptImagesUnsupported = useCallback(() => {
    setDetailError(translate("agentHost.agentGui.promptImagesUnsupported"));
  }, []);

  const submitApprovalOption = useCallback(
    (input: AgentInteractionResponseInput): boolean =>
      submitInteractivePrompt(input),
    [submitInteractivePrompt]
  );

  const interruptCurrentTurn = useCallback(
    (noRunningResponseMessage: string) => {
      const agentSessionId = activeConversationIdRef.current;
      if (!agentSessionId) return;
      void noRunningResponseMessage;
      setDetailError(null);
      agentActivityRuntime.prompts.stop(agentSessionId);
    },
    [agentActivityRuntime]
  );

  const updateDraftContent = useCallback(
    (draftContent: AgentComposerDraft, sourceScopeKey?: string) => {
      const agentSessionId = activeConversationIdRef.current;
      const draftKey =
        sourceScopeKey ??
        resolveAgentComposerDraftScopeKey({
          agentSessionId
        });
      draftByScopeKeyRef.current = {
        ...draftByScopeKeyRef.current,
        [draftKey]: draftContent
      };
      setDraftByScopeKey((current) => ({
        ...current,
        [draftKey]: draftContent
      }));
    },
    []
  );

  return {
    interruptCurrentTurn,
    retryActivation,
    showPromptImagesUnsupported,
    submitApprovalOption,
    submitGuidancePrompt,
    submitInteractivePrompt,
    submitPrompt,
    updateDraftContent
  };
}
