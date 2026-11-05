import { type PendingActivationIntentRecord } from "@tutti-os/agent-activity-core";
import {
  useCallback,
  useEffect,
  useRef,
  type Dispatch,
  type SetStateAction
} from "react";
import {
  agentGUIAgentTargetRefsEqual,
  resolveAgentGUIAgentTarget
} from "../../../agentTargets";
import type { ConversationServices } from "../../../conversationServices";
import type { AgentConversationServices } from "../../../state/conversation-services";
import type {
  AgentGUIAgentTarget,
  AgentGUINodeData,
  AgentGUIProvider,
  AgentGUIProviderReadinessGate
} from "../../../types";
import {
  normalizeAgentGUIConversationFilter,
  type AgentGUIConversationFilter
} from "../model/agentGuiConversationFilter";
import type { AgentGUIConversationSummary } from "../model/agentGuiConversationModel";
import {
  forgetAgentGUISessionMemories,
  rememberAgentGUIActiveConversation,
  resolveAgentGUIRememberedSessionSelection,
  resolveAgentGUISessionMemoryTarget
} from "../model/agentGuiSessionNavigationMemory";
import { type AgentGUIComposerTargetData } from "./agentGuiController.composerPresentation";
import {
  agentGUINodeDataHasComposerTarget,
  composerTargetDataFromProviderTarget,
  resolveAgentGUIProviderRailTargetSelection
} from "./agentGuiController.providerHelpers";
import { reportAgentGUIConversationFilterTargetUnresolved } from "./agentGuiController.reporting";
import {
  resolveConversationSummaryById,
  type ConversationIntent
} from "./useAgentConversationSelection";
import { isPendingNewConversationActivationForSession } from "./useAgentGUIActivation";

interface CurrentValue<T> {
  current: T;
}

interface UseAgentGUIProviderHomeInput {
  activeConversationId: string | null;
  activeConversationIdRef: CurrentValue<string | null>;
  activePendingActivation: PendingActivationIntentRecord | null;
  agentActivityRuntime: ConversationServices;
  clearRailRevealRequest(): void;
  conversationFilter: AgentGUIConversationFilter;
  conversationFilterRef: CurrentValue<AgentGUIConversationFilter>;
  conversationsRef: CurrentValue<readonly AgentGUIConversationSummary[]>;
  data: AgentGUINodeData;
  dataRef: CurrentValue<AgentGUINodeData>;
  defaultAgentTargetId: string | null;
  effectiveSelectedProviderTarget: AgentGUIAgentTarget;
  firstReadyHomeComposerProviderTarget: AgentGUIAgentTarget | null;
  homeComposerTargetOverride: AgentGUIAgentTarget | null;
  isComposerHomeRef: CurrentValue<boolean>;
  isLoadingConversations: boolean;
  normalizedExplicitProviderTargets: readonly AgentGUIAgentTarget[];
  normalizedProviderTargets: readonly AgentGUIAgentTarget[];
  onDataChangeRef: CurrentValue<
    (updater: (current: AgentGUINodeData) => AgentGUINodeData) => void
  >;
  persistActiveConversation(agentSessionId: string | null): void;
  providerReadinessGates: Partial<
    Record<AgentGUIProvider, AgentGUIProviderReadinessGate | null>
  > | null;
  selectedComposerTargetDataRef: CurrentValue<AgentGUIComposerTargetData>;
  selectConversation(
    agentSessionId: string,
    options?: { reloadConversations?: boolean }
  ): void;
  conversationServices: AgentConversationServices;
  setActiveConversationId: Dispatch<SetStateAction<string | null>>;
  setConversationFilter: Dispatch<SetStateAction<AgentGUIConversationFilter>>;
  setDetailError: Dispatch<SetStateAction<string | null>>;
  setHomeComposerTargetOverride: Dispatch<
    SetStateAction<AgentGUIAgentTarget | null>
  >;
  setIntent: Dispatch<SetStateAction<ConversationIntent>>;
  setIsComposerHome: Dispatch<SetStateAction<boolean>>;
  setIsLoadingMessages: Dispatch<SetStateAction<boolean>>;
  shouldUseStaticProviderTargets: boolean;
  transientConversation: AgentGUIConversationSummary | null;
  unactivate(agentSessionId: string): Promise<void>;
  workspaceId: string;
}

export function useAgentGUIProviderHome(input: UseAgentGUIProviderHomeInput) {
  const inputRef = useRef(input);
  inputRef.current = input;
  const resolveDefaultHomeComposerTarget = useCallback(() => {
    const current = inputRef.current;
    const defaultTargetId = current.defaultAgentTargetId?.trim() ?? "";
    const explicitDefaultTarget = defaultTargetId
      ? (current.normalizedProviderTargets.find(
          (target) =>
            target.targetId === defaultTargetId ||
            target.agentTargetId === defaultTargetId
        ) ?? null)
      : null;
    return (
      explicitDefaultTarget ??
      current.normalizedProviderTargets.find(
        (target) => target.disabled !== true
      ) ??
      current.normalizedProviderTargets[0] ??
      null
    );
  }, []);

  const resetHomeComposerAgentTargetToDefault = useCallback(() => {
    const currentInput = inputRef.current;

    const nextTarget = resolveDefaultHomeComposerTarget();
    if (!nextTarget) return;
    const nextTargetIsExplicit =
      currentInput.normalizedExplicitProviderTargets.some(
        (target) =>
          target.provider === nextTarget.provider &&
          target.targetId === nextTarget.targetId &&
          agentGUIAgentTargetRefsEqual(target.ref, nextTarget.ref)
      );
    const nextTargetData = composerTargetDataFromProviderTarget({
      current: currentInput.dataRef.current,
      isExplicit: nextTargetIsExplicit,
      target: nextTarget
    });
    currentInput.setHomeComposerTargetOverride(nextTarget);
    currentInput.setIntent({ tag: "home" });
    currentInput.isComposerHomeRef.current = true;
    currentInput.setIsComposerHome(true);
    currentInput.onDataChangeRef.current((current) => {
      const currentNextTargetData = composerTargetDataFromProviderTarget({
        current,
        isExplicit: nextTargetIsExplicit,
        target: nextTarget
      });
      const nextData = {
        ...currentNextTargetData.data,
        lastActiveAgentSessionId: null
      };
      currentInput.dataRef.current = nextData;
      return nextData;
    });
    currentInput.dataRef.current = {
      ...nextTargetData.data,
      lastActiveAgentSessionId: null
    };
  }, [resolveDefaultHomeComposerTarget]);

  const updateConversationFilter = useCallback(
    (filter: AgentGUIConversationFilter) => {
      const current = inputRef.current;
      const nextFilter = normalizeAgentGUIConversationFilter(filter);
      current.setConversationFilter(nextFilter);
      if (
        nextFilter.kind === "all" &&
        current.activeConversationIdRef.current === null
      ) {
        resetHomeComposerAgentTargetToDefault();
      }
    },
    [resetHomeComposerAgentTargetToDefault]
  );

  const selectHomeComposerAgentTarget = useCallback(
    (selection: {
      provider: AgentGUIProvider;
      agentTargetId?: string | null;
    }) => {
      const currentInput = inputRef.current;

      const nextTarget = resolveAgentGUIAgentTarget({
        agentTargetId: selection.agentTargetId,
        defaultAgentTargetId: currentInput.defaultAgentTargetId,
        provider: selection.provider,
        agentTargets: currentInput.normalizedProviderTargets,
        useStaticCatalog: currentInput.shouldUseStaticProviderTargets
      });
      if (!nextTarget) return;
      currentInput.clearRailRevealRequest();
      const nextTargetIsExplicit =
        currentInput.normalizedExplicitProviderTargets.some(
          (target) =>
            target.provider === nextTarget.provider &&
            target.targetId === nextTarget.targetId &&
            agentGUIAgentTargetRefsEqual(target.ref, nextTarget.ref)
        );
      const shouldSyncScopedRailFilter =
        currentInput.conversationFilterRef.current.kind === "agentTarget";
      currentInput.setHomeComposerTargetOverride(nextTarget);
      if (shouldSyncScopedRailFilter) {
        const nextAgentTargetId = nextTarget.agentTargetId?.trim() ?? "";
        currentInput.setConversationFilter(
          nextAgentTargetId
            ? { kind: "agentTarget", agentTargetId: nextAgentTargetId }
            : { kind: "all" }
        );
      }
      const previous = currentInput.activeConversationIdRef.current;
      const previousSummary = resolveConversationSummaryById(
        currentInput.conversationsRef.current,
        previous,
        currentInput.transientConversation
      );
      const previousAgentTargetId = resolveAgentGUISessionMemoryTarget({
        agentSessionId: previous,
        canonicalAgentTargetId: previous
          ? currentInput.conversationServices.sessions.session(previous)
              ?.agentTargetId
          : null,
        pendingActivation: currentInput.activePendingActivation,
        projectedAgentTargetId: previousSummary?.agentTargetId
      });
      if (
        previous &&
        !isPendingNewConversationActivationForSession(
          currentInput.activePendingActivation,
          previous
        )
      ) {
        void currentInput.unactivate(previous);
      }
      currentInput.setIntent({ tag: "home" });
      currentInput.isComposerHomeRef.current = true;
      currentInput.setIsComposerHome(true);
      currentInput.activeConversationIdRef.current = null;
      currentInput.setActiveConversationId(null);
      currentInput.setIsLoadingMessages(false);
      currentInput.setDetailError(null);
      currentInput.persistActiveConversation(null);
      currentInput.onDataChangeRef.current((current) => {
        const rememberedCurrent = previous
          ? rememberAgentGUIActiveConversation(
              current,
              previous,
              previousAgentTargetId
            )
          : current;
        const currentNextTargetData = composerTargetDataFromProviderTarget({
          current: rememberedCurrent,
          isExplicit: nextTargetIsExplicit,
          target: nextTarget
        });
        const nextAgentTargetId = currentNextTargetData.agentTargetId;
        const currentTargetId = current.agentTargetId ?? null;
        const nextTargetId = nextAgentTargetId ?? nextTarget.targetId;
        const providerTargetChanged =
          current.provider !== selection.provider ||
          ((currentTargetId !== null || nextAgentTargetId !== null) &&
            currentTargetId !== nextTargetId);
        const nextData: AgentGUINodeData = {
          ...rememberedCurrent,
          provider: currentNextTargetData.provider,
          agentTargetId: currentNextTargetData.agentTargetId,
          lastActiveAgentSessionId: null,
          composerOverrides: providerTargetChanged
            ? null
            : rememberedCurrent.composerOverrides
        };
        currentInput.dataRef.current = nextData;
        return nextData;
      });
    },
    []
  );

  useEffect(() => {
    const effectiveAgentTargetId =
      input.effectiveSelectedProviderTarget.agentTargetId?.trim() ?? "";
    if (
      input.activeConversationId === null &&
      input.conversationFilter.kind === "agentTarget" &&
      effectiveAgentTargetId &&
      effectiveAgentTargetId !== input.conversationFilter.agentTargetId.trim()
    ) {
      input.setConversationFilter({
        kind: "agentTarget",
        agentTargetId: effectiveAgentTargetId
      });
    }
    if (
      input.activeConversationId !== null ||
      input.conversationFilter.kind !== "all" ||
      input.homeComposerTargetOverride !== null ||
      agentGUINodeDataHasComposerTarget(input.data) ||
      !input.providerReadinessGates ||
      !input.firstReadyHomeComposerProviderTarget
    ) {
      return;
    }
    const readyTarget = input.firstReadyHomeComposerProviderTarget;
    if (
      readyTarget.provider === input.effectiveSelectedProviderTarget.provider &&
      readyTarget.targetId === input.effectiveSelectedProviderTarget.targetId &&
      agentGUIAgentTargetRefsEqual(
        readyTarget.ref,
        input.effectiveSelectedProviderTarget.ref
      )
    ) {
      return;
    }
    if (
      !Object.prototype.hasOwnProperty.call(
        input.providerReadinessGates,
        input.effectiveSelectedProviderTarget.provider
      ) ||
      !input.providerReadinessGates[
        input.effectiveSelectedProviderTarget.provider
      ]
    ) {
      return;
    }
    selectHomeComposerAgentTarget({
      provider: readyTarget.provider,
      agentTargetId: readyTarget.targetId
    });
  }, [
    input.activeConversationId,
    input.conversationFilter,
    input.data,
    input.effectiveSelectedProviderTarget,
    input.firstReadyHomeComposerProviderTarget,
    input.homeComposerTargetOverride,
    input.providerReadinessGates,
    input.setConversationFilter,
    selectHomeComposerAgentTarget
  ]);

  const selectConversationFilterTarget = useCallback(
    (selection: { provider: AgentGUIProvider; agentTargetId: string }) => {
      const current = inputRef.current;
      const agentTargetId = selection.agentTargetId.trim();
      if (!agentTargetId) {
        reportAgentGUIConversationFilterTargetUnresolved({
          provider: selection.provider,
          agentTargetId: null,
          providerTargetCount: current.normalizedProviderTargets.length,
          reason: "unresolved",
          runtime: current.agentActivityRuntime,
          workspaceId: current.workspaceId
        });
        return;
      }
      const nextTarget = resolveAgentGUIAgentTarget({
        agentTargetId,
        defaultAgentTargetId: current.defaultAgentTargetId,
        provider: selection.provider,
        agentTargets: current.normalizedProviderTargets,
        useStaticCatalog: current.shouldUseStaticProviderTargets
      });
      if (!nextTarget) {
        reportAgentGUIConversationFilterTargetUnresolved({
          provider: selection.provider,
          agentTargetId,
          providerTargetCount: current.normalizedProviderTargets.length,
          reason: "unresolved",
          runtime: current.agentActivityRuntime,
          workspaceId: current.workspaceId
        });
        return;
      }
      current.clearRailRevealRequest();
      const nextFilter = { kind: "agentTarget" as const, agentTargetId };
      current.setConversationFilter(nextFilter);
      const activeId = current.activeConversationIdRef.current;
      const activeSummary = resolveConversationSummaryById(
        current.conversationsRef.current,
        activeId,
        current.transientConversation
      );
      if (
        resolveAgentGUIProviderRailTargetSelection({
          activeConversation: activeSummary,
          nextFilter
        }) === "keep-active-conversation"
      ) {
        return;
      }
      const rememberedAgentSessionId =
        agentTargetId &&
        current.dataRef.current.lastActiveAgentSessionIdByAgentTargetId?.[
          agentTargetId
        ];
      const rememberedSummary = resolveConversationSummaryById(
        current.conversationsRef.current,
        rememberedAgentSessionId,
        current.transientConversation
      );
      const rememberedEngineSession =
        current.conversationServices.sessions.session(rememberedAgentSessionId);
      const rememberedSelection = resolveAgentGUIRememberedSessionSelection({
        data: current.dataRef.current,
        deleted: current.conversationServices.sessions.isDeleted(
          rememberedAgentSessionId
        ),
        knownAgentTargetId:
          rememberedEngineSession?.agentTargetId ??
          rememberedSummary?.agentTargetId ??
          null,
        targetAgentTargetId: agentTargetId || null
      });
      if (rememberedSelection.kind === "restore") {
        selectHomeComposerAgentTarget({
          provider: nextTarget.provider,
          agentTargetId
        });
        current.selectConversation(rememberedSelection.agentSessionId, {
          reloadConversations: false
        });
        return;
      }
      if (rememberedSelection.kind === "stale") {
        const staleIds = new Set([rememberedSelection.agentSessionId]);
        current.onDataChangeRef.current((data) =>
          forgetAgentGUISessionMemories(data, staleIds)
        );
        current.dataRef.current = forgetAgentGUISessionMemories(
          current.dataRef.current,
          staleIds
        );
      }
      selectHomeComposerAgentTarget({
        provider: nextTarget.provider,
        agentTargetId
      });
    },
    [selectHomeComposerAgentTarget]
  );

  return {
    resetHomeComposerAgentTargetToDefault,
    selectConversationFilterTarget,
    selectHomeComposerAgentTarget,
    updateConversationFilter
  };
}
