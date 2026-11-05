import { useEffect, useMemo } from "react";
import { useTranslation } from "../../../i18n";
import { SessionPresentationHostService } from "../../../state/SessionPresentationHostService";
import {
  SessionPresentationState,
  type SessionPresentationStateInput
} from "../../../state/SessionPresentationState";
export function useAgentGUISessionPresentation(
  input: SessionPresentationStateInput
) {
  const { locale } = useTranslation();
  const {
    native,
    capabilities,
    conversation,
    selectedTarget,
    targets,
    detailError,
    agentActivityRuntime,
    agentTargetsLoading,
    currentUserId,
    isLoadingMessages,
    providerReadinessGate,
    selectedAgentTargetUnavailable,
    selectedAgentTargetUnavailableReason,
    selectedAgentTargetOwnerLabel,
    targetConnectionSource,
    interactionReadinessSource,
    observationGapSource,
    workspaceId,
    workspacePath,
    avoidGroupingEdits
  } = input;
  const binding = useMemo(() => {
    const config: SessionPresentationStateInput = {
      native,
      capabilities,
      conversation,
      selectedTarget,
      targets,
      detailError,
      agentActivityRuntime,
      agentTargetsLoading,
      currentUserId,
      isLoadingMessages,
      providerReadinessGate,
      selectedAgentTargetUnavailable,
      selectedAgentTargetUnavailableReason,
      selectedAgentTargetOwnerLabel,
      targetConnectionSource,
      interactionReadinessSource,
      observationGapSource,
      workspaceId,
      workspacePath,
      avoidGroupingEdits,
      locale
    };
    const host = new SessionPresentationHostService(config);
    return { state: new SessionPresentationState(config, host), host };
  }, [
    native,
    capabilities,
    conversation,
    selectedTarget,
    targets,
    detailError,
    agentActivityRuntime,
    agentTargetsLoading,
    currentUserId,
    isLoadingMessages,
    providerReadinessGate,
    selectedAgentTargetUnavailable,
    selectedAgentTargetUnavailableReason,
    selectedAgentTargetOwnerLabel,
    targetConnectionSource,
    interactionReadinessSource,
    observationGapSource,
    workspaceId,
    workspacePath,
    avoidGroupingEdits,
    locale
  ]);
  const { state, host } = binding;
  useEffect(() => host.startDiagnostics(state), [state, host]);
  return state;
}
