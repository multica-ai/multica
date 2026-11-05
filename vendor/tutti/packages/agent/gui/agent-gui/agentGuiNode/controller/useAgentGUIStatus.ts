import { useCallback, useMemo } from "react";
import type { TranslateFn } from "../../../i18n/index";
import { useExternalStoreSelector } from "../../../shared/useExternalStoreSelector";
import { buildDockAgentProbeTooltipLines } from "../../workspaceDesktop/view/desktopDockAgentProbeTooltipModel";
import type { AgentComposerSlashStatus } from "../AgentComposer";
import type { AgentGUINodeProps } from "../AgentGUINode.types";
import {
  resolveAgentGUIRailStatusTarget,
  resolveAgentGUIStatusTarget,
  slashStatusLimitsFromQuotas,
  slashStatusUsageErrorMessage
} from "../AgentGUINode.usage";
import type {
  AgentGUIComposerViewModel,
  AgentGUIDetailViewModel,
  AgentGUIInteractionViewModel,
  AgentGUIOperationsViewModel,
  AgentGUIRailViewModel,
  AgentGUIReadinessViewModel,
  AgentGUIShellViewModel
} from "../model/agentGuiNodeTypes";
import {
  selectAgentStatusControllerSnapshot,
  type AgentStatusControllerSnapshot,
  type AgentStatusRequestReason
} from "./AgentStatusController";
import type { useAgentGUIPage } from "./useAgentGUIPage";

type AgentGUIViewModel = Pick<
  ReturnType<typeof useAgentGUIPage>,
  | "shell"
  | "rail"
  | "detail"
  | "composer"
  | "interaction"
  | "readiness"
  | "operations"
>;
type RuntimeRequests = AgentGUINodeProps["runtimeRequests"];

const EMPTY_STATUS_SNAPSHOT: AgentStatusControllerSnapshot = {
  query: null,
  value: null,
  phase: "idle",
  isRefreshing: false,
  errorCode: null
};

const EMPTY_STATUS_STORE = {
  getSnapshot: () => EMPTY_STATUS_SNAPSHOT,
  subscribe: () => () => {}
};

const selectStatusSnapshot = (snapshot: AgentStatusControllerSnapshot) =>
  snapshot;

/**
 * Owns the three AgentGUI status entry points and their projections. Hosts
 * provide one controller; target resolution, transport and probing remain
 * outside the package.
 */
export function useAgentGUIStatus(input: {
  activeProvider: string;
  agentStatusController: RuntimeRequests["agentStatusController"];
  t: TranslateFn;
  shell: AgentGUIShellViewModel;
  rail: AgentGUIRailViewModel;
  detail: AgentGUIDetailViewModel;
  composer: AgentGUIComposerViewModel;
  interaction: AgentGUIInteractionViewModel;
  readiness: AgentGUIReadinessViewModel;
  operations: AgentGUIOperationsViewModel;
}) {
  const {
    agentStatusController,
    t,
    shell,
    rail,
    detail,
    composer,
    interaction,
    readiness,
    operations
  } = input;
  const agentStatusSnapshot = useExternalStoreSelector(
    agentStatusController ?? EMPTY_STATUS_STORE,
    selectStatusSnapshot
  );
  const activeStatusScopeKey =
    rail.activeConversation?.agentTargetId?.trim() ||
    rail.selectedAgentTarget.agentTargetId?.trim() ||
    rail.selectedAgentTarget.targetId.trim();
  // Conversation identity is available before detail/raw-state hydration and
  // is the caller-visible binding for shared sessions.
  const activeStatusSessionId =
    rail.activeConversationId?.trim() ||
    interaction.sessionChrome.rawState?.agentSessionId?.trim() ||
    null;
  const activeStatusTarget = useMemo(
    () =>
      resolveAgentGUIStatusTarget({
        agentTargets: rail.agentTargets,
        scopeKey: activeStatusScopeKey
      }),
    [activeStatusScopeKey, rail.agentTargets]
  );
  const railStatusTarget = useMemo(
    () =>
      resolveAgentGUIRailStatusTarget({
        conversationFilter: rail.conversationFilter,
        agentTargets: rail.agentTargets
      }),
    [rail.conversationFilter, rail.agentTargets]
  );
  const railStatusProvider = railStatusTarget?.provider ?? null;
  const railStatusScopeKey =
    rail.conversationFilter.kind === "agentTarget"
      ? rail.conversationFilter.agentTargetId.trim()
      : activeStatusScopeKey;
  const activeAgentStatusSnapshot = selectAgentStatusControllerSnapshot(
    agentStatusSnapshot,
    {
      scopeKey: activeStatusScopeKey,
      agentSessionId: activeStatusSessionId,
      reasons: ["slash-status", "agent-info"]
    }
  );
  const railAgentStatusSnapshot = selectAgentStatusControllerSnapshot(
    agentStatusSnapshot,
    {
      scopeKey: railStatusScopeKey,
      agentSessionId: null,
      reasons: ["agent-config"]
    }
  );

  const agentInfoProbe = useMemo(() => {
    const value = activeAgentStatusSnapshot.value;
    return {
      provider: input.activeProvider,
      availability: {
        status: "unknown" as const,
        detailsVisible: false,
        checks: []
      },
      ...(value &&
      (value.limitsState === "available" || value.quotas.length > 0)
        ? {
            usage: {
              accountTier: value.accountLabel ?? undefined,
              quotas: [...value.quotas],
              capturedAtUnixMs: value.limitsCapturedAtUnixMs ?? 0
            }
          }
        : {}),
      ...(value?.limitsState === "error"
        ? {
            lastError: {
              code: value.limitsErrorCode?.trim() || "runtime_unavailable"
            }
          }
        : {})
    };
  }, [activeAgentStatusSnapshot.value, input.activeProvider]);
  const agentProbeLines = useMemo(
    () =>
      buildDockAgentProbeTooltipLines(
        agentInfoProbe,
        activeAgentStatusSnapshot.phase === "loading" &&
          activeAgentStatusSnapshot.value === null,
        t,
        {
          includeUsageLines: true,
          isLoadingUsage:
            activeAgentStatusSnapshot.isRefreshing &&
            activeAgentStatusSnapshot.value === null
        }
      ),
    [
      activeAgentStatusSnapshot.isRefreshing,
      activeAgentStatusSnapshot.phase,
      activeAgentStatusSnapshot.value,
      agentInfoProbe,
      t
    ]
  );

  const requestStatus = useCallback(
    (request: {
      scopeKey: string;
      agentSessionId?: string | null;
      reason: AgentStatusRequestReason;
      forceRefresh?: boolean;
    }): void => {
      {
        agentStatusController?.open(request);
      }
    },
    [agentStatusController]
  );
  const closeStatus = useCallback(
    (reason: AgentStatusRequestReason): void => {
      if (agentStatusSnapshot.query?.reason === reason) {
        agentStatusController?.close();
      }
    },
    [agentStatusController, agentStatusSnapshot.query?.reason]
  );
  const handleAgentProbeInfoOpen = useCallback(() => {
    requestStatus({
      scopeKey: activeStatusScopeKey,
      agentSessionId: activeStatusSessionId,
      reason: "agent-info"
    });
  }, [activeStatusScopeKey, activeStatusSessionId, requestStatus]);
  const handleAgentProbeInfoClose = useCallback(
    () => closeStatus("agent-info"),
    [closeStatus]
  );
  const handleAgentConfigMenuOpen = useCallback(() => {
    requestStatus({
      scopeKey: railStatusScopeKey,
      agentSessionId: null,
      reason: "agent-config"
    });
  }, [railStatusScopeKey, requestStatus]);
  const handleAgentConfigMenuClose = useCallback(
    () => closeStatus("agent-config"),
    [closeStatus]
  );
  const handleAgentUsageRefresh = useCallback(() => {
    requestStatus({
      scopeKey: railStatusScopeKey,
      agentSessionId: null,
      reason: "agent-config",
      forceRefresh: true
    });
  }, [railStatusScopeKey, requestStatus]);
  const handleSlashStatusClose = useCallback(
    () => closeStatus("slash-status"),
    [closeStatus]
  );
  const handleSlashStatusOpen = useCallback(() => {
    requestStatus({
      scopeKey: activeStatusScopeKey,
      agentSessionId: activeStatusSessionId,
      reason: "slash-status"
    });
  }, [activeStatusScopeKey, activeStatusSessionId, requestStatus]);
  const handleSlashStatusRefresh = useCallback(() => {
    requestStatus({
      scopeKey: activeStatusScopeKey,
      agentSessionId: activeStatusSessionId,
      reason: "slash-status",
      forceRefresh: true
    });
  }, [activeStatusScopeKey, activeStatusSessionId, requestStatus]);

  const agentStatusLimits = useMemo(
    () =>
      slashStatusLimitsFromQuotas(
        activeAgentStatusSnapshot.value?.quotas,
        composer.state.composerSettings.selectedModelValue ??
          composer.state.composerSettings.draftSettings.model,
        t
      ),
    [
      activeAgentStatusSnapshot.value?.quotas,
      t,
      composer.state.composerSettings.draftSettings.model,
      composer.state.composerSettings.selectedModelValue
    ]
  );
  const railAgentStatusLimits = useMemo(
    () =>
      slashStatusLimitsFromQuotas(
        railAgentStatusSnapshot.value?.quotas,
        null,
        t
      ),
    [railAgentStatusSnapshot.value?.quotas, t]
  );
  const slashStatusOverride = useMemo<AgentComposerSlashStatus | null>(() => {
    if (!agentStatusController) return null;
    const value = activeAgentStatusSnapshot.value;
    return {
      agentSessionId: value?.agentSessionId ?? activeStatusSessionId,
      contextWindow: value?.contextWindow ?? null,
      limits: agentStatusLimits,
      limitsLoading:
        activeAgentStatusSnapshot.phase === "loading" && value === null,
      limitsUnavailable:
        activeAgentStatusSnapshot.phase === "error" ||
        (value !== null && value.limitsState !== "available"),
      limitsResolvedEmpty:
        value?.limitsState === "available" && agentStatusLimits.length === 0,
      limitsCapturedAtUnixMs: value?.limitsCapturedAtUnixMs ?? null,
      limitsErrorMessage: slashStatusUsageErrorMessage(
        value?.limitsErrorCode ?? activeAgentStatusSnapshot.errorCode,
        t
      ),
      refreshFailed:
        activeAgentStatusSnapshot.errorCode !== null ||
        value?.limitsState === "error",
      isRefreshing: activeAgentStatusSnapshot.isRefreshing
    };
  }, [
    activeAgentStatusSnapshot.errorCode,
    activeAgentStatusSnapshot.isRefreshing,
    activeAgentStatusSnapshot.phase,
    activeAgentStatusSnapshot.value,
    activeStatusSessionId,
    agentStatusController,
    agentStatusLimits,
    t
  ]);
  const controllerRailStatus = agentStatusController
    ? {
        accountLabel:
          railAgentStatusSnapshot.value?.accountLabel?.trim() || null,
        limits: railAgentStatusLimits,
        loading:
          railAgentStatusSnapshot.phase === "loading" &&
          railAgentStatusSnapshot.value === null,
        capturedAtUnixMs:
          railAgentStatusSnapshot.value?.limitsCapturedAtUnixMs ?? null,
        didFail:
          railAgentStatusSnapshot.errorCode !== null ||
          railAgentStatusSnapshot.value?.limitsState === "error",
        errorMessage: slashStatusUsageErrorMessage(
          railAgentStatusSnapshot.value?.limitsErrorCode ??
            railAgentStatusSnapshot.errorCode,
          t
        ),
        attempted:
          railAgentStatusSnapshot.value !== null ||
          railAgentStatusSnapshot.errorCode !== null,
        resolvedEmpty:
          railAgentStatusSnapshot.value?.limitsState === "available" &&
          railAgentStatusLimits.length === 0
      }
    : null;

  return {
    agentProbeLines,
    controllerRailStatus,
    handleAgentConfigMenuClose,
    handleAgentConfigMenuOpen,
    handleAgentProbeInfoClose,
    handleAgentProbeInfoOpen,
    handleAgentUsageRefresh:
      railStatusTarget?.providerAccountUsageApplicable === false
        ? undefined
        : handleAgentUsageRefresh,
    handleSlashStatusClose,
    handleSlashStatusOpen,
    handleSlashStatusRefresh:
      activeStatusTarget?.providerAccountUsageApplicable === false
        ? undefined
        : handleSlashStatusRefresh,
    railStatusProvider,
    slashStatusLimits: agentStatusLimits,
    slashStatusLimitsUnavailable:
      activeAgentStatusSnapshot.phase === "error" ||
      (activeAgentStatusSnapshot.value !== null &&
        activeAgentStatusSnapshot.value.limitsState !== "available"),
    slashStatusOverride
  };
}
