import {
  selectEngineSession,
  type AgentActivityComposerOptions,
  type AgentActivityTurn,
  type AgentSessionEngine
} from "@tutti-os/agent-activity-core";
import type { Dispatch, RefObject, SetStateAction } from "react";
import { useCallback, useRef } from "react";
import type { AgentGUIRuntime } from "../../../agentActivityRuntime";
import { translate } from "../../../i18n/index";
import type {
  AgentSessionComposerSettings,
  AgentSessionReasoningEffort
} from "../../../shared/agentSessionTypes";
import type { AgentGUINodeData } from "../../../types";
import {
  normalizePlanIssueBudgetPreset,
  planIssueBudgetPresetsEqual,
  type PlanIssueBudgetPreset
} from "../../../shared/agentConversation/planImplementationPresentation";
import { useStableControllerEventCallback } from "./agentGuiController.stableHelpers";
import {
  cloneComposerSettings,
  nodeDataFromComposerSettings,
  nodeDefaultDraftKey,
  normalizePermissionModeId,
  readNodeDefaultDraftSettings,
  resolveEffectiveComposerSettings,
  sameComposerSettings
} from "./agentGuiController.composerHelpers";
import {
  enforceComposerModelBindingForHomeDefaults,
  effectiveComposerSettingsFromOptions,
  nodeDataMatchesComposerTarget,
  sanitizeComposerSettingsForTarget,
  type AgentGUIComposerTargetData
} from "./agentGuiController.composerPresentation";
import {
  acknowledgeAgentGUIComposerDefaultsMutation,
  createAgentGUIComposerDefaultsLedger,
  prepareAcknowledgedComposerDefaultsAuthorityRead,
  preserveAcknowledgedComposerDefaultsForReconciliation,
  registerAgentGUIComposerDefaultsMutation,
  removeRetiredComposerDefaults,
  retireAcknowledgedComposerDefaultsForRead,
  type AgentGUIComposerDefaultsLedger,
  type AgentGUIComposerDefaultsAuthorityReconciler,
  type AgentGUIComposerDefaultsAuthorityReadReceipt,
  type AgentGUIComposerDefaultsMutation,
  type AgentGUIRetiredComposerDefault
} from "./agentGuiComposerDefaultsReconciliation";
import { normalizeOptionalText } from "./agentGuiController.promptHelpers";
import {
  composerDefaultsPatchFromSettings,
  composerOptionsForTarget,
  rememberComposerDefaultsFields,
  type AgentGUIRememberComposerDefaultsInput,
  type AgentGUIRememberComposerDefaultsResult
} from "./agentGuiController.providerHelpers";
import type { useAgentGUIActivation } from "./useAgentGUIActivation";

interface UseAgentGUIComposerSettingsActionsInput {
  activation: ReturnType<typeof useAgentGUIActivation>;
  activeCanonicalComposerSettings: AgentSessionComposerSettings;
  activeConversationIdRef: RefObject<string | null>;
  activeEngineActiveTurn: AgentActivityTurn | null;
  agentActivityRuntime: AgentGUIRuntime;
  composerSupportPermissionModeChangeDeferred: boolean;
  dataRef: RefObject<AgentGUINodeData>;
  defaultReasoningEffort: AgentSessionReasoningEffort | null;
  draftSettingsBySessionIdRef: RefObject<
    Record<string, AgentSessionComposerSettings>
  >;
  isMountedRef: RefObject<boolean>;
  loadDraftComposerOptions(options?: {
    force?: boolean;
    section?: "core" | "capabilities" | "connectors";
    waitForFreshModelCatalog?: boolean;
  }): void;
  onDataChangeRef: RefObject<
    (updater: (current: AgentGUINodeData) => AgentGUINodeData) => void
  >;
  onComposerDefaultsAuthorityReloadedRef: RefObject<AgentGUIComposerDefaultsAuthorityReconciler>;
  onRememberComposerDefaultsRef: RefObject<
    | ((
        input: AgentGUIRememberComposerDefaultsInput
      ) => void | Promise<AgentGUIRememberComposerDefaultsResult>)
    | undefined
  >;
  onShowMessageRef: RefObject<
    ((message: string, tone?: "info" | "warning" | "error") => void) | undefined
  >;
  reloadComposerOptionsForTarget(input: {
    settings: AgentSessionComposerSettings;
    target: AgentGUIComposerTargetData;
  }): Promise<void>;
  selectedComposerTargetDataRef: RefObject<AgentGUIComposerTargetData>;
  sessionEngine: AgentSessionEngine;
  setDraftSettingsBySessionId: Dispatch<
    SetStateAction<Record<string, AgentSessionComposerSettings>>
  >;
  setDetailError?: Dispatch<SetStateAction<string | null>>;
  updateComposerSettingsRef: RefObject<
    (settings: Partial<AgentSessionComposerSettings>) => void
  >;
  workspaceId: string;
}

export function useAgentGUIComposerSettingsActions(
  input: UseAgentGUIComposerSettingsActionsInput
) {
  const {
    activation,
    activeCanonicalComposerSettings,
    activeConversationIdRef,
    activeEngineActiveTurn,
    agentActivityRuntime,
    dataRef,
    defaultReasoningEffort,
    draftSettingsBySessionIdRef,
    isMountedRef,
    loadDraftComposerOptions,
    onComposerDefaultsAuthorityReloadedRef,
    onDataChangeRef,
    onRememberComposerDefaultsRef,
    onShowMessageRef,
    reloadComposerOptionsForTarget,
    selectedComposerTargetDataRef,
    sessionEngine,
    setDraftSettingsBySessionId,
    setDetailError,
    updateComposerSettingsRef,
    workspaceId
  } = input;
  const composerSupport = {
    permissionModeChangeDeferred:
      input.composerSupportPermissionModeChangeDeferred
  };
  const composerDefaultsLedgerRef = useRef(
    createAgentGUIComposerDefaultsLedger()
  );
  const retireAcknowledgedDefaultsForRead = useCallback(
    (
      receipt: AgentGUIComposerDefaultsAuthorityReadReceipt | null,
      options: AgentActivityComposerOptions
    ) => {
      if (!isMountedRef.current || !receipt) return;
      const currentDraft =
        draftSettingsBySessionIdRef.current[receipt.draftKey];
      if (!currentDraft) return;
      const authoritativeSettings =
        effectiveComposerSettingsFromOptions(options) ?? {};
      const retired = retireAcknowledgedComposerDefaultsForRead(
        composerDefaultsLedgerRef.current,
        receipt,
        currentDraft,
        authoritativeSettings
      );
      if (retired.length === 0) return;
      draftSettingsBySessionIdRef.current = reconcileRetiredDraftMap(
        draftSettingsBySessionIdRef.current,
        receipt.draftKey,
        retired
      );
      setDraftSettingsBySessionId((current) =>
        reconcileRetiredDraftMap(current, receipt.draftKey, retired)
      );
    },
    [draftSettingsBySessionIdRef, isMountedRef, setDraftSettingsBySessionId]
  );
  const prepareComposerDefaultsAuthorityRead = useCallback(
    (
      target: AgentGUIComposerTargetData,
      settings: AgentSessionComposerSettings
    ) =>
      prepareAcknowledgedComposerDefaultsAuthorityRead(
        composerDefaultsLedgerRef.current,
        nodeDefaultDraftKey(target.provider, target.agentTargetId),
        settings
      ),
    []
  );
  onComposerDefaultsAuthorityReloadedRef.current = {
    prepareRead: prepareComposerDefaultsAuthorityRead,
    reconcileHomeDefaults: (target, options) => {
      if (
        !isMountedRef.current ||
        activeConversationIdRef.current !== null ||
        !nodeDataMatchesComposerTarget(
          selectedComposerTargetDataRef.current.data,
          target
        )
      ) {
        return;
      }
      const draftKey = nodeDefaultDraftKey(
        target.provider,
        target.agentTargetId
      );
      const currentDraft = draftSettingsBySessionIdRef.current[draftKey];
      if (!currentDraft) {
        return;
      }
      const reconciledDraft =
        preserveAcknowledgedComposerDefaultsForReconciliation(
          composerDefaultsLedgerRef.current,
          draftKey,
          currentDraft,
          enforceComposerModelBindingForHomeDefaults(
            sanitizeComposerSettingsForTarget({
              settings: currentDraft,
              target,
              options
            }),
            options
          )
        );
      if (sameComposerSettings(currentDraft, reconciledDraft)) {
        return;
      }
      draftSettingsBySessionIdRef.current = {
        ...draftSettingsBySessionIdRef.current,
        [draftKey]: reconciledDraft
      };
      setDraftSettingsBySessionId((current) => ({
        ...current,
        [draftKey]: reconciledDraft
      }));
      onDataChangeRef.current((current) =>
        nodeDataFromComposerSettings(
          {
            ...current,
            provider: target.provider,
            agentTargetId: target.agentTargetId
          },
          reconciledDraft
        )
      );
    },
    reloaded: retireAcknowledgedDefaultsForRead
  };
  const updateComposerSettings = useCallback(
    (nextSettings: Partial<AgentSessionComposerSettings>) => {
      // A model validation failure belongs to the model that was submitted.
      // Once the user selects another model, retaining that failure makes the
      // new valid selection look broken before it has even been submitted.
      if (normalizeOptionalText(nextSettings.model) !== null) {
        setDetailError?.(null);
      }
      // Values pass through unclamped: the toggle visibility is capability
      // gated and the daemon clamps persisted settings per provider.
      const supportedNextSettings: Partial<AgentSessionComposerSettings> = {
        ...nextSettings
      };
      // Persistent selections only originate from rendered menu values. A
      // transient empty select value during options refresh is not a user
      // intent and must not clear either the optimistic draft or defaults.
      for (const field of rememberComposerDefaultsFields) {
        if (field === "codexSaverMode" || field === "rtkSaverMode") {
          continue;
        }
        if (
          supportedNextSettings[field] !== undefined &&
          normalizeOptionalText(supportedNextSettings[field]) === null
        ) {
          delete supportedNextSettings[field];
        }
      }
      const agentSessionId = activeConversationIdRef.current;
      if (!agentSessionId) {
        const targetData = selectedComposerTargetDataRef.current;
        const defaultDraftKey = nodeDefaultDraftKey(
          targetData.provider,
          targetData.agentTargetId
        );
        const storedIntent = readNodeDefaultDraftSettings({
          data: targetData.data,
          defaultReasoningEffort,
          drafts: draftSettingsBySessionIdRef.current
        });
        const previousSettings = resolveEffectiveComposerSettings({
          settings: storedIntent
        });
        const mergedIntent: AgentSessionComposerSettings = {
          ...storedIntent,
          ...supportedNextSettings
        };
        for (const field of rememberComposerDefaultsFields) {
          if (supportedNextSettings[field] !== undefined) {
            Object.assign(mergedIntent, {
              [field]:
                field === "codexSaverMode" || field === "rtkSaverMode"
                  ? supportedNextSettings[field] === true
                  : normalizeOptionalText(supportedNextSettings[field])
            });
          }
        }
        const snapshotComposerOptions = composerOptionsForTarget({
          composer: agentActivityRuntime.composer,
          target: targetData
        });
        draftSettingsBySessionIdRef.current = {
          ...draftSettingsBySessionIdRef.current,
          [defaultDraftKey]: mergedIntent
        };
        setDraftSettingsBySessionId((current) => ({
          ...current,
          [defaultDraftKey]: mergedIntent
        }));
        const rememberedDefaultsPatch = composerDefaultsPatchFromSettings(
          supportedNextSettings,
          mergedIntent
        );
        if (rememberedDefaultsPatch) {
          const mutation = registerAgentGUIComposerDefaultsMutation(
            composerDefaultsLedgerRef.current,
            defaultDraftKey,
            rememberedDefaultsPatch
          );
          const acknowledgement = invokeRememberComposerDefaults(
            onRememberComposerDefaultsRef.current,
            {
              agentTargetId: targetData.agentTargetId,
              provider: targetData.provider,
              defaults: rememberedDefaultsPatch
            }
          );
          if (targetData.agentTargetId && acknowledgement) {
            void reconcileAcknowledgedHomeDefaults({
              acknowledgement,
              draftKey: defaultDraftKey,
              draftSettingsBySessionIdRef,
              isMountedRef,
              ledger: composerDefaultsLedgerRef.current,
              mutation,
              reloadComposerOptionsForTarget,
              target: targetData
            }).catch(() => undefined);
          }
        }
        void agentActivityRuntime.trackDraftComposerSettingsChange?.({
          workspaceId,
          provider: targetData.provider,
          previousSettings,
          nextSettings: resolveEffectiveComposerSettings({
            settings: mergedIntent
          })
        });
        loadDraftComposerOptions(
          snapshotComposerOptions?.behavior
            ?.refreshModelOptionsAfterSettings === true
            ? { force: true }
            : undefined
        );
        return;
      }
      const canonicalSession = selectEngineSession(
        sessionEngine.getSnapshot(),
        agentSessionId
      );
      // The optimistic pre-activation window (see startConversation): the id
      // is already the active conversation but the backend session has not
      // attached yet, so there is no control state to read settings from or
      // send an update RPC against. Composer changes here are still applied
      // to the local view (so the control reflects the click immediately)
      // and queued for the flush once activation resolves.
      const isPreActivationSession =
        canonicalSession === null &&
        activation.stateFor(agentSessionId) === "activating";
      const sessionSettings = cloneComposerSettings(
        canonicalSession ? activeCanonicalComposerSettings : null
      );
      const nextPermission =
        supportedNextSettings.permissionModeId !== undefined
          ? normalizeOptionalText(supportedNextSettings.permissionModeId)
          : undefined;
      const currentPermission = normalizeOptionalText(
        sessionSettings?.permissionModeId
      );
      const nextModel =
        supportedNextSettings.model !== undefined
          ? normalizeOptionalText(supportedNextSettings.model)
          : undefined;
      const currentModel = normalizeOptionalText(sessionSettings?.model);
      const nextReasoningEffort =
        supportedNextSettings.reasoningEffort !== undefined
          ? (supportedNextSettings.reasoningEffort ?? null)
          : undefined;
      const currentReasoningEffort = sessionSettings?.reasoningEffort ?? null;
      const nextSpeed =
        supportedNextSettings.speed !== undefined
          ? (supportedNextSettings.speed ?? null)
          : undefined;
      const currentSpeed = sessionSettings?.speed ?? null;
      const nextPlanMode = supportedNextSettings.planMode;
      const currentPlanMode = sessionSettings?.planMode ?? false;
      const nextBrowserUse = supportedNextSettings.browserUse;
      const currentBrowserUse = sessionSettings?.browserUse ?? true;
      const nextComputerUse = supportedNextSettings.computerUse;
      const currentComputerUse = sessionSettings?.computerUse ?? true;
      const sessionSettingsPatch: AgentSessionComposerSettings = {};

      const rememberedDefaultsPatch = composerDefaultsPatchFromSettings(
        supportedNextSettings,
        supportedNextSettings as AgentSessionComposerSettings
      );
      if (rememberedDefaultsPatch) {
        const defaultAgentTargetId =
          normalizeOptionalText(canonicalSession?.agentTargetId) ??
          normalizeOptionalText(dataRef.current.agentTargetId);
        const defaultProvider =
          canonicalSession?.provider ?? dataRef.current.provider;
        const saving = invokeRememberComposerDefaults(
          onRememberComposerDefaultsRef.current,
          {
            agentTargetId: defaultAgentTargetId,
            provider: defaultProvider,
            defaults: rememberedDefaultsPatch
          }
        );
        if (saving) {
          // Defaults persistence is independent from the active-session
          // command and must remain silent on both sync and async failures.
          void saving.catch(() => undefined);
        }
      }

      if (nextModel !== undefined && nextModel !== currentModel) {
        sessionSettingsPatch.model = nextModel;
      }
      if (
        nextReasoningEffort !== undefined &&
        nextReasoningEffort !== currentReasoningEffort
      ) {
        sessionSettingsPatch.reasoningEffort = nextReasoningEffort;
      }
      if (nextSpeed !== undefined && nextSpeed !== currentSpeed) {
        sessionSettingsPatch.speed = nextSpeed;
      }
      if (nextPlanMode !== undefined && nextPlanMode !== currentPlanMode) {
        sessionSettingsPatch.planMode = nextPlanMode;
      }
      if (
        nextBrowserUse !== undefined &&
        nextBrowserUse !== currentBrowserUse
      ) {
        sessionSettingsPatch.browserUse = nextBrowserUse;
      }
      if (
        nextComputerUse !== undefined &&
        nextComputerUse !== currentComputerUse
      ) {
        sessionSettingsPatch.computerUse = nextComputerUse;
      }
      if (
        nextPermission !== undefined &&
        nextPermission &&
        nextPermission !== currentPermission &&
        (canonicalSession !== null || isPreActivationSession)
      ) {
        sessionSettingsPatch.permissionModeId =
          normalizePermissionModeId(nextPermission);
        // Descriptor capability data decides whether an in-flight change is
        // deferred until the next turn. Pre-activation has no turn to defer.
        const turnPhase = activeEngineActiveTurn?.phase;
        const isTurnInFlight =
          turnPhase === "running" || turnPhase === "submitted";
        if (composerSupport.permissionModeChangeDeferred && isTurnInFlight) {
          onShowMessageRef.current?.(
            translate("messages.agentPermissionModeAppliesNextTurn"),
            "info"
          );
        }
      }
      if (
        Object.keys(sessionSettingsPatch).length > 0 &&
        (canonicalSession !== null || isPreActivationSession)
      ) {
        if (isPreActivationSession) {
          sessionEngine.dispatch({
            type: "activation/settingsPatched",
            agentSessionId,
            settings: { ...sessionSettingsPatch }
          });
        } else {
          agentActivityRuntime.composer.updateSettings({
            agentSessionId,
            settings: { ...sessionSettingsPatch }
          });
        }
        return;
      }
    },
    [
      activation,
      activeCanonicalComposerSettings,
      defaultReasoningEffort,
      composerSupport.permissionModeChangeDeferred,
      loadDraftComposerOptions,
      reloadComposerOptionsForTarget,
      sessionEngine,
      workspaceId,
      setDetailError
    ]
  );
  updateComposerSettingsRef.current = updateComposerSettings;

  const updatePlanIssueBudgetPreset = useStableControllerEventCallback(
    (preset: PlanIssueBudgetPreset) => {
      const normalized = normalizePlanIssueBudgetPreset(preset);
      if (!normalized) return;
      onDataChangeRef.current((current) =>
        planIssueBudgetPresetsEqual(current.planIssueBudgetPreset, normalized)
          ? current
          : { ...current, planIssueBudgetPreset: normalized }
      );
    }
  );

  // Recovery entry for the composer-options terminal error state. Leave the
  // request non-forced so Activity Core joins an already-running load when a
  // user double-clicks the retry control instead of superseding it.
  const retryComposerOptions = useStableControllerEventCallback(
    (options?: {
      force?: boolean;
      section?: "core" | "capabilities" | "connectors";
      waitForFreshModelCatalog?: boolean;
    }) => {
      if (options === undefined) {
        loadDraftComposerOptions();
        return;
      }
      loadDraftComposerOptions(options);
    }
  );

  return {
    retryComposerOptions,
    updateComposerSettings,
    updatePlanIssueBudgetPreset
  };
}

function invokeRememberComposerDefaults(
  callback:
    | ((
        input: AgentGUIRememberComposerDefaultsInput
      ) => void | Promise<AgentGUIRememberComposerDefaultsResult>)
    | undefined,
  input: AgentGUIRememberComposerDefaultsInput
): Promise<AgentGUIRememberComposerDefaultsResult> | undefined {
  if (!callback) return undefined;
  try {
    const result = callback(input);
    return result === undefined ? undefined : Promise.resolve(result);
  } catch (error) {
    return Promise.reject(error);
  }
}

async function reconcileAcknowledgedHomeDefaults(input: {
  acknowledgement: Promise<AgentGUIRememberComposerDefaultsResult>;
  draftKey: string;
  draftSettingsBySessionIdRef: RefObject<
    Record<string, AgentSessionComposerSettings>
  >;
  isMountedRef: RefObject<boolean>;
  ledger: AgentGUIComposerDefaultsLedger;
  mutation: AgentGUIComposerDefaultsMutation;
  reloadComposerOptionsForTarget(input: {
    settings: AgentSessionComposerSettings;
    target: AgentGUIComposerTargetData;
  }): Promise<void>;
  target: AgentGUIComposerTargetData;
}): Promise<void> {
  const result = await input.acknowledgement;
  if (!input.isMountedRef.current) return;

  const currentDraft =
    input.draftSettingsBySessionIdRef.current[input.draftKey];
  if (!currentDraft) {
    return;
  }
  if (
    !acknowledgeAgentGUIComposerDefaultsMutation(
      input.ledger,
      input.mutation,
      result
    )
  ) {
    return;
  }
  await input.reloadComposerOptionsForTarget({
    settings: currentDraft,
    target: input.target
  });
}

function reconcileRetiredDraftMap(
  current: Record<string, AgentSessionComposerSettings>,
  draftKey: string,
  retired: readonly AgentGUIRetiredComposerDefault[]
): Record<string, AgentSessionComposerSettings> {
  const draft = current[draftKey];
  if (!draft) return current;
  const nextDraft = removeRetiredComposerDefaults(draft, retired);
  if (Object.keys(draft).length === Object.keys(nextDraft).length)
    return current;
  const next = { ...current };
  if (Object.keys(nextDraft).length === 0) {
    delete next[draftKey];
  } else {
    next[draftKey] = nextDraft;
  }
  return next;
}
