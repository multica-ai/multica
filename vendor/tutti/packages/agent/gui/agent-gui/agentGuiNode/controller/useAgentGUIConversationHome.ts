import type { PendingActivationIntentRecord } from "@tutti-os/agent-activity-core";
import {
  useCallback,
  useEffect,
  type Dispatch,
  type RefObject,
  type SetStateAction
} from "react";
import { resolveAgentGUIAgentTarget } from "../../../agentTargets";
import type { ConversationServices } from "../../../conversationServices";
import type {
  AgentGUIAgentTarget,
  AgentGUINodeData,
  AgentGUIProvider
} from "../../../types";
import { buildAgentComposerDraft } from "../model/agentComposerDraft";
import {
  normalizeAgentComposerDraftProjectPath,
  resolveAgentComposerDraftScopeKey
} from "../model/agentComposerDraftScope";
import type { AgentGUIConversationFilter } from "../model/agentGuiConversationFilter";
import type { AgentComposerDraft } from "../model/agentGuiNodeTypes";
import {
  buildNodeDefaultComposerSettings,
  nodeDataFromComposerSettings
} from "./agentGuiController.composerHelpers";
import type { AgentGUIComposerTargetData } from "./agentGuiController.composerPresentation";
import { isPendingNewConversationActivationForSession } from "./useAgentGUIActivation";
import {
  resolveAgentGUIComposerAppendRequest,
  type AgentGUIComposerAppendRequest
} from "./useAgentGUIComposerAppendRequest";

export interface AgentGUIPrefillPromptRequest {
  agentTargetId?: string | null;
  autoSubmit?: boolean;
  draftPrompt: string;
  model?: string | null;
  modelPlanId?: string | null;
  provider?: AgentGUIProvider;
  sequence: number;
  userProjectPath?: string | null;
}

export interface UseAgentGUIConversationHomeInput {
  activeConversationId: string | null;
  activeConversationIdRef: RefObject<string | null>;
  activePendingActivation: PendingActivationIntentRecord | null;
  agentActivityRuntime: ConversationServices;
  currentProvider: AgentGUIProvider;
  composerTargetDataFromProviderTarget: (input: {
    current: AgentGUINodeData;
    isExplicit: boolean;
    target: AgentGUIAgentTarget;
  }) => AgentGUIComposerTargetData;
  composerAppendRequest: AgentGUIComposerAppendRequest | null;
  conversationFilterRef: RefObject<AgentGUIConversationFilter>;
  dataRef: RefObject<AgentGUINodeData>;
  defaultAgentTargetId: string | null;
  handledPrefillPromptSequenceRef: RefObject<number | null>;
  handledComposerAppendSequenceRef: RefObject<number | null>;
  isComposerHomeRef: RefObject<boolean>;
  isExplicitAgentGUIAgentTarget: (
    target: AgentGUIAgentTarget,
    explicitTargets: readonly AgentGUIAgentTarget[]
  ) => boolean;
  loadDraftComposerOptions: () => void;
  normalizedExplicitProviderTargets: readonly AgentGUIAgentTarget[];
  normalizedProviderTargets: readonly AgentGUIAgentTarget[];
  onDataChangeRef: RefObject<
    (updater: (current: AgentGUINodeData) => AgentGUINodeData) => void
  >;
  onComposerAppendHandled?: (sequence: number) => void;
  submitPrefillPrompt: (prompt: string) => void;
  persistActiveConversation: (agentSessionId: string | null) => void;
  prefillPromptRequest: AgentGUIPrefillPromptRequest | null;
  reportActiveConversationCleared: (input: {
    details: Record<string, unknown>;
    previousAgentSessionId: string | null;
    reason: "create_conversation" | "prefill_prompt";
    runtime: ConversationServices;
    workspaceId: string;
  }) => void;
  selectedComposerTargetDataRef: RefObject<AgentGUIComposerTargetData>;
  selectedProjectPathRef: RefObject<string | null>;
  draftByScopeKeyRef: RefObject<Record<string, AgentComposerDraft>>;
  setActiveConversationId: Dispatch<SetStateAction<string | null>>;
  setConversationFilter: Dispatch<SetStateAction<AgentGUIConversationFilter>>;
  setDetailError: Dispatch<SetStateAction<string | null>>;
  setDraftByScopeKey: Dispatch<
    SetStateAction<Record<string, AgentComposerDraft>>
  >;
  setHomeComposerTargetOverride: Dispatch<
    SetStateAction<AgentGUIAgentTarget | null>
  >;
  setIntent: (intent: { tag: "home" }) => void;
  setIsComposerHome: Dispatch<SetStateAction<boolean>>;
  setIsLoadingMessages: Dispatch<SetStateAction<boolean>>;
  setSelectedProjectPath: Dispatch<SetStateAction<string | null>>;
  shouldUseStaticProviderTargets: boolean;
  unactivate: (agentSessionId: string) => Promise<unknown>;
  workspaceId: string;
}

/** Owns transitions from an active conversation back to the home composer. */
export function useAgentGUIConversationHome({
  activeConversationId,
  activeConversationIdRef,
  activePendingActivation,
  agentActivityRuntime,
  composerTargetDataFromProviderTarget,
  composerAppendRequest,
  conversationFilterRef,
  currentProvider,
  dataRef,
  defaultAgentTargetId,
  handledPrefillPromptSequenceRef,
  handledComposerAppendSequenceRef,
  isComposerHomeRef,
  isExplicitAgentGUIAgentTarget,
  loadDraftComposerOptions,
  normalizedExplicitProviderTargets,
  normalizedProviderTargets,
  onDataChangeRef,
  onComposerAppendHandled,
  submitPrefillPrompt,
  persistActiveConversation,
  prefillPromptRequest,
  reportActiveConversationCleared,
  selectedComposerTargetDataRef,
  selectedProjectPathRef,
  draftByScopeKeyRef,
  setActiveConversationId,
  setConversationFilter,
  setDetailError,
  setDraftByScopeKey,
  setHomeComposerTargetOverride,
  setIntent,
  setIsComposerHome,
  setIsLoadingMessages,
  setSelectedProjectPath,
  shouldUseStaticProviderTargets,
  unactivate,
  workspaceId
}: UseAgentGUIConversationHomeInput) {
  const enterHome = useCallback(
    (
      reason: "create_conversation" | "prefill_prompt",
      details: Record<string, unknown>
    ) => {
      const previous = activeConversationIdRef.current;
      reportActiveConversationCleared({
        details,
        previousAgentSessionId: previous,
        reason,
        runtime: agentActivityRuntime,
        workspaceId
      });
      if (
        previous &&
        !isPendingNewConversationActivationForSession(
          activePendingActivation,
          previous
        )
      ) {
        void unactivate(previous);
      }
      setIntent({ tag: "home" });
      isComposerHomeRef.current = true;
      setIsComposerHome(true);
      activeConversationIdRef.current = null;
      setActiveConversationId(null);
      setIsLoadingMessages(false);
      setDetailError(null);
    },
    [
      activeConversationIdRef,
      activePendingActivation,
      agentActivityRuntime,
      isComposerHomeRef,
      reportActiveConversationCleared,
      setActiveConversationId,
      setDetailError,
      setIntent,
      setIsComposerHome,
      setIsLoadingMessages,
      unactivate,
      workspaceId
    ]
  );

  const createConversation = useCallback(
    (options?: { projectPath?: string | null; source?: string }) => {
      if (options && "projectPath" in options) {
        const projectPath = normalizeAgentComposerDraftProjectPath(
          options.projectPath
        );
        selectedProjectPathRef.current = projectPath;
        setSelectedProjectPath(projectPath);
      }
      enterHome("create_conversation", {
        hasProjectPathOption: Boolean(options && "projectPath" in options),
        isComposerHome: isComposerHomeRef.current,
        projectPathPresent: Boolean(
          options &&
            "projectPath" in options &&
            normalizeAgentComposerDraftProjectPath(options.projectPath)
        ),
        source: options?.source ?? "controller"
      });
      persistActiveConversation(null);
      const filter = conversationFilterRef.current;
      if (filter.kind === "agentTarget") {
        const target = resolveAgentGUIAgentTarget({
          agentTargetId: filter.agentTargetId,
          defaultAgentTargetId,
          provider: currentProvider,
          agentTargets: normalizedProviderTargets,
          useStaticCatalog: false
        });
        if (
          target &&
          target.disabled !== true &&
          (target.agentTargetId?.trim() ?? "") === filter.agentTargetId
        ) {
          setHomeComposerTargetOverride(target);
        }
      }
      loadDraftComposerOptions();
    },
    [
      currentProvider,
      conversationFilterRef,
      dataRef,
      defaultAgentTargetId,
      enterHome,
      isComposerHomeRef,
      loadDraftComposerOptions,
      normalizedProviderTargets,
      persistActiveConversation,
      selectedProjectPathRef,
      setHomeComposerTargetOverride,
      setSelectedProjectPath
    ]
  );

  useEffect(() => {
    const resolvedAppendRequest = resolveAgentGUIComposerAppendRequest({
      activeConversationId,
      draftByScopeKey: draftByScopeKeyRef.current,
      handledSequence: handledComposerAppendSequenceRef.current,
      request: composerAppendRequest
    });
    if (resolvedAppendRequest) {
      handledComposerAppendSequenceRef.current = resolvedAppendRequest.sequence;
      draftByScopeKeyRef.current = {
        ...draftByScopeKeyRef.current,
        [resolvedAppendRequest.draftKey]: resolvedAppendRequest.nextDraft
      };
      setDraftByScopeKey((current) => ({
        ...current,
        [resolvedAppendRequest.draftKey]: resolvedAppendRequest.nextDraft
      }));
      onComposerAppendHandled?.(resolvedAppendRequest.sequence);
    }
    if (
      !prefillPromptRequest ||
      handledPrefillPromptSequenceRef.current === prefillPromptRequest.sequence
    ) {
      return;
    }
    handledPrefillPromptSequenceRef.current = prefillPromptRequest.sequence;
    const draftPrompt = prefillPromptRequest.draftPrompt.trim();
    if (!draftPrompt) return;
    const projectPath = normalizeAgentComposerDraftProjectPath(
      prefillPromptRequest.userProjectPath
    );
    selectedProjectPathRef.current = projectPath;
    setSelectedProjectPath(projectPath);
    enterHome("prefill_prompt", {
      autoSubmit: prefillPromptRequest.autoSubmit === true,
      sequence: prefillPromptRequest.sequence
    });
    const selectedTargetData = selectedComposerTargetDataRef.current;
    const target =
      prefillPromptRequest.provider || prefillPromptRequest.agentTargetId
        ? resolveAgentGUIAgentTarget({
            agentTargetId: prefillPromptRequest.agentTargetId,
            defaultAgentTargetId,
            provider:
              prefillPromptRequest.provider ?? selectedTargetData.provider,
            agentTargets: normalizedProviderTargets,
            useStaticCatalog: shouldUseStaticProviderTargets
          })
        : null;
    const targetData = target
      ? composerTargetDataFromProviderTarget({
          current: dataRef.current,
          isExplicit: isExplicitAgentGUIAgentTarget(
            target,
            normalizedExplicitProviderTargets
          ),
          target
        })
      : selectedTargetData;
    if (target) {
      setHomeComposerTargetOverride(target);
      setConversationFilter(
        targetData.agentTargetId
          ? { kind: "agentTarget", agentTargetId: targetData.agentTargetId }
          : { kind: "all" }
      );
      onDataChangeRef.current((current) => {
        const nextTargetData = composerTargetDataFromProviderTarget({
          current,
          isExplicit: isExplicitAgentGUIAgentTarget(
            target,
            normalizedExplicitProviderTargets
          ),
          target
        });
        const nextTargetNodeData = {
          ...nextTargetData.data,
          lastActiveAgentSessionId: null
        };
        const nextData = applyAgentGUIPrefillComposerAssignment(
          nextTargetNodeData,
          prefillPromptRequest
        );
        dataRef.current = nextData;
        return nextData;
      });
    } else if (prefillPromptRequest.model || prefillPromptRequest.modelPlanId) {
      onDataChangeRef.current((current) => {
        const nextData = applyAgentGUIPrefillComposerAssignment(
          { ...current, lastActiveAgentSessionId: null },
          prefillPromptRequest
        );
        dataRef.current = nextData;
        return nextData;
      });
    }
    const sourceScopeKey = resolveAgentComposerDraftScopeKey({});
    const prefilledDraft = buildAgentComposerDraft({ prompt: draftPrompt });
    draftByScopeKeyRef.current = {
      ...draftByScopeKeyRef.current,
      [sourceScopeKey]: prefilledDraft
    };
    setDraftByScopeKey((current) => ({
      ...current,
      [sourceScopeKey]: prefilledDraft
    }));
    if (prefillPromptRequest.autoSubmit) {
      submitPrefillPrompt(draftPrompt);
    }
    persistActiveConversation(null);
    loadDraftComposerOptions();
  }, [
    activeConversationId,
    dataRef,
    draftByScopeKeyRef,
    composerAppendRequest,
    defaultAgentTargetId,
    enterHome,
    handledPrefillPromptSequenceRef,
    handledComposerAppendSequenceRef,
    loadDraftComposerOptions,
    normalizedExplicitProviderTargets,
    normalizedProviderTargets,
    onComposerAppendHandled,
    onDataChangeRef,
    persistActiveConversation,
    prefillPromptRequest,
    selectedComposerTargetDataRef,
    selectedProjectPathRef,
    setConversationFilter,
    setDraftByScopeKey,
    setHomeComposerTargetOverride,
    setSelectedProjectPath,
    submitPrefillPrompt,
    shouldUseStaticProviderTargets
  ]);

  return { createConversation, enterHome };
}

export function applyAgentGUIPrefillComposerAssignment(
  data: AgentGUINodeData,
  request: Pick<AgentGUIPrefillPromptRequest, "model" | "modelPlanId">
): AgentGUINodeData {
  const model = request.model?.trim() || null;
  const modelPlanId = request.modelPlanId?.trim() || null;
  if (!model && !modelPlanId) {
    return data;
  }
  return nodeDataFromComposerSettings(data, {
    ...buildNodeDefaultComposerSettings(data),
    model,
    modelPlanId
  });
}
