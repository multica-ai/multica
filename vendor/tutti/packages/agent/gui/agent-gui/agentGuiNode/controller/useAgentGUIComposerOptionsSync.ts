import type { RefObject } from "react";
import { useCallback, useEffect, useRef } from "react";
import type { ConversationServices } from "../../../conversationServices";
import { subscribe, subscribeCoalesced } from "../../../host/agentHostEventBus";
import type {
  AgentSessionComposerSettings,
  AgentSessionReasoningEffort
} from "../../../shared/agentSessionTypes";
import type { AgentGUINodeData } from "../../../types";
import type { AgentGUIComposerDefaultsAuthorityReconciler } from "./agentGuiComposerDefaultsReconciliation";
import { readNodeDefaultDraftSettings } from "./agentGuiController.composerHelpers";
import {
  composerTargetDataForConversation,
  type AgentGUIActiveSessionTarget,
  type AgentGUIComposerTargetData,
  type OptimisticComposerTarget
} from "./agentGuiController.composerPresentation";
import {
  composerDefaultsPatchFromSettings,
  composerOptionsForTarget,
  mergeAgentModelCatalogInvalidationEvents,
  withoutAcknowledgedComposerDefaults
} from "./agentGuiController.providerHelpers";

export function useAgentGUIComposerOptionsSync(input: {
  activeConversationId: string | null;
  activeConversationIdRef: RefObject<string | null>;
  activeSessionTarget: AgentGUIActiveSessionTarget | null;
  agentActivityRuntime: ConversationServices;
  composerTargetData: AgentGUIComposerTargetData;
  conversationFilter: unknown;
  currentUserId: string | null | undefined;
  data: AgentGUINodeData;
  dataRef: RefObject<AgentGUINodeData>;
  defaultReasoningEffort: AgentSessionReasoningEffort | null;
  draftSettingsBySessionIdRef: RefObject<
    Record<string, AgentSessionComposerSettings>
  >;
  isComposerHome: boolean;
  isComposerHomeRef: RefObject<boolean>;
  isCreatingConversation: boolean;
  loadDraftComposerOptionsRef: RefObject<
    (options?: {
      force?: boolean;
      section?: "core" | "capabilities" | "connectors";
      waitForFreshModelCatalog?: boolean;
    }) => void
  >;
  loadSessionState(agentSessionId: string): void;
  onComposerDefaultsAuthorityReloadedRef: RefObject<AgentGUIComposerDefaultsAuthorityReconciler>;
  optimisticComposerTarget?: OptimisticComposerTarget | null;
  providerComposerOptions:
    | { behavior?: { prewarmDraftSession?: boolean } | null }
    | null
    | undefined;
  selectedComposerTargetDataRef: RefObject<AgentGUIComposerTargetData>;
  selectedProjectPath: string | null;
  selectedProjectPathRef: RefObject<string | null>;
  syncConversationListProjection(agentSessionId?: string | null): Promise<void>;
  workspaceId: string;
  workspacePath: string;
}) {
  const previousIsCreatingConversationRef = useRef(
    input.isCreatingConversation
  );
  const loadComposerOptionsForTarget = useCallback(
    (
      targetData: AgentGUIComposerTargetData,
      options?: {
        allowWhileCreating?: boolean;
        excludePersistentDefaults?: boolean;
        force?: boolean;
        section?: "core" | "capabilities" | "connectors";
        waitForFreshModelCatalog?: boolean;
        reconcileAcknowledgedDefaults?: boolean;
        settings?: AgentSessionComposerSettings;
      }
    ): Promise<void> => {
      if (
        (input.isCreatingConversation && !options?.allowWhileCreating) ||
        !targetData.agentTargetId
      ) {
        return Promise.resolve();
      }
      const localSettings =
        options?.settings ??
        readNodeDefaultDraftSettings({
          data: targetData.data,
          defaultReasoningEffort: input.defaultReasoningEffort,
          drafts: input.draftSettingsBySessionIdRef.current
        });
      const authorityRead = options?.reconcileAcknowledgedDefaults
        ? input.onComposerDefaultsAuthorityReloadedRef.current.prepareRead(
            targetData,
            localSettings
          )
        : { force: false, receipt: null, settings: localSettings };
      const localDefaults = options?.excludePersistentDefaults
        ? composerDefaultsPatchFromSettings(localSettings, localSettings)
        : null;
      const requestSettings = localDefaults
        ? withoutAcknowledgedComposerDefaults(
            authorityRead.settings,
            localDefaults
          )
        : authorityRead.settings;
      const cwd =
        input.selectedProjectPathRef.current?.trim() ||
        input.workspacePath.trim() ||
        "";
      const request = {
        agentSessionId: input.activeConversationIdRef.current,
        cwd,
        force: options?.force || authorityRead.force ? true : undefined,
        waitForFreshModelCatalog: options?.waitForFreshModelCatalog,
        provider: targetData.provider,
        targetKey: targetData.agentTargetId,
        settings: requestSettings
      } as const;
      const section = options?.section ?? "core";
      const composerOptions = input.agentActivityRuntime.composer.load({
        ...request,
        section
      });
      return Promise.resolve(composerOptions).then((returnedOptions) => {
        if (section === "capabilities" || section === "connectors") {
          return;
        }
        const loadedOptions =
          composerOptionsForTarget({
            composer: input.agentActivityRuntime.composer,
            target: targetData
          }) ?? returnedOptions;
        input.onComposerDefaultsAuthorityReloadedRef.current.reconcileHomeDefaults(
          targetData,
          loadedOptions
        );
        if (options?.reconcileAcknowledgedDefaults) {
          input.onComposerDefaultsAuthorityReloadedRef.current.reloaded(
            authorityRead.receipt,
            returnedOptions
          );
        }
      });
    },
    [
      input.agentActivityRuntime,
      input.defaultReasoningEffort,
      input.isCreatingConversation,
      input.workspaceId,
      input.workspacePath
    ]
  );
  const loadDraftComposerOptions = useCallback(
    (options?: {
      force?: boolean;
      section?: "core" | "capabilities" | "connectors";
      waitForFreshModelCatalog?: boolean;
    }) => {
      void loadComposerOptionsForTarget(
        composerTargetDataForConversation({
          activeConversationId: input.activeConversationIdRef.current,
          activeSessionTarget: input.activeSessionTarget,
          data: input.dataRef.current,
          optimisticTarget: input.optimisticComposerTarget ?? null,
          selectedTarget: input.selectedComposerTargetDataRef.current
        }),
        {
          ...options,
          reconcileAcknowledgedDefaults:
            input.activeConversationIdRef.current === null &&
            input.isComposerHomeRef.current
        }
      ).catch(() => undefined);
    },
    [
      input.activeConversationIdRef,
      input.activeSessionTarget?.agentTargetId,
      input.activeSessionTarget?.agentSessionId,
      input.activeSessionTarget?.provider,
      input.dataRef,
      input.isComposerHomeRef,
      input.optimisticComposerTarget,
      input.selectedComposerTargetDataRef,
      loadComposerOptionsForTarget
    ]
  );
  const reloadComposerOptionsForTarget = useCallback(
    (reloadInput: {
      settings: AgentSessionComposerSettings;
      target: AgentGUIComposerTargetData;
    }): Promise<void> =>
      loadComposerOptionsForTarget(reloadInput.target, {
        allowWhileCreating: true,
        force: true,
        reconcileAcknowledgedDefaults: true,
        settings: reloadInput.settings
      }),
    [loadComposerOptionsForTarget]
  );
  input.loadDraftComposerOptionsRef.current = loadDraftComposerOptions;

  useEffect(() => {
    const disposeModelCatalog = subscribeCoalesced(
      "agent-model-catalog-invalidated",
      {
        delayMs: 150,
        key: () => "agent-model-catalog-invalidated",
        merge: mergeAgentModelCatalogInvalidationEvents
      },
      (event) => {
        const provider = composerTargetDataForConversation({
          activeConversationId: input.activeConversationIdRef.current,
          activeSessionTarget: input.activeSessionTarget,
          data: input.dataRef.current,
          optimisticTarget: input.optimisticComposerTarget ?? null,
          selectedTarget: input.selectedComposerTargetDataRef.current
        }).provider;
        const activeId = input.activeConversationIdRef.current;
        if (!event.providers.some((candidate) => candidate === provider))
          return;
        loadDraftComposerOptions({ force: true });
        if (
          !activeId ||
          (activeId === null && input.isComposerHomeRef.current)
        ) {
          return;
        }
        input.loadSessionState(activeId);
      }
    );
    const disposeConnectorCatalog = subscribe(
      "agent-connector-catalog-invalidated",
      () => loadDraftComposerOptions({ force: true, section: "connectors" })
    );
    return () => {
      disposeModelCatalog();
      disposeConnectorCatalog();
    };
  }, [input.loadSessionState, loadDraftComposerOptions]);

  useEffect(() => {
    return subscribe("agent-composer-defaults-invalidated", (event) => {
      const selectedTarget = composerTargetDataForConversation({
        activeConversationId: input.activeConversationIdRef.current,
        activeSessionTarget: input.activeSessionTarget,
        data: input.dataRef.current,
        optimisticTarget: input.optimisticComposerTarget ?? null,
        selectedTarget: input.selectedComposerTargetDataRef.current
      });
      if (selectedTarget.agentTargetId !== event.agentTargetId) {
        return;
      }
      const localIntent = readNodeDefaultDraftSettings({
        data: selectedTarget.data,
        defaultReasoningEffort: input.defaultReasoningEffort,
        drafts: input.draftSettingsBySessionIdRef.current
      });
      // The target-only event is always a reread signal. Exclude local
      // persistent intent from the request so effectiveSettings can reflect
      // daemon authority, but do not retire that intent without its own ack.
      void loadComposerOptionsForTarget(selectedTarget, {
        allowWhileCreating: true,
        excludePersistentDefaults: true,
        force: true,
        reconcileAcknowledgedDefaults: true,
        settings: localIntent
      }).catch(() => undefined);
    });
  }, [
    input.defaultReasoningEffort,
    input.activeSessionTarget?.agentTargetId,
    input.activeSessionTarget?.agentSessionId,
    input.activeSessionTarget?.provider,
    input.onComposerDefaultsAuthorityReloadedRef,
    input.optimisticComposerTarget,
    loadComposerOptionsForTarget
  ]);

  useEffect(() => {
    // Session creation can finish after an earlier request cached the
    // provider's selected-model-only fallback. Once creation settles, bypass
    // request-signature deduplication so runtime-discovered
    // model options replace that bootstrap snapshot.
    const conversationCreationSettled =
      previousIsCreatingConversationRef.current &&
      !input.isCreatingConversation;
    const shouldPrewarmDraftSession =
      input.providerComposerOptions?.behavior?.prewarmDraftSession === true &&
      input.isComposerHome;
    previousIsCreatingConversationRef.current = input.isCreatingConversation;
    loadDraftComposerOptions(
      conversationCreationSettled
        ? { force: true }
        : shouldPrewarmDraftSession
          ? { force: true, waitForFreshModelCatalog: true }
          : undefined
    );
  }, [
    input.activeConversationId,
    input.composerTargetData.agentTargetId,
    input.composerTargetData.provider,
    input.isComposerHome,
    input.isCreatingConversation,
    input.providerComposerOptions?.behavior?.prewarmDraftSession,
    input.selectedProjectPath,
    loadDraftComposerOptions
  ]);

  useEffect(() => {
    {
      void input.syncConversationListProjection(
        input.dataRef.current.lastActiveAgentSessionId
      );
    }
  }, [
    input.conversationFilter,
    input.currentUserId,
    input.data.provider,
    input.syncConversationListProjection
  ]);

  return { loadDraftComposerOptions, reloadComposerOptionsForTarget };
}
