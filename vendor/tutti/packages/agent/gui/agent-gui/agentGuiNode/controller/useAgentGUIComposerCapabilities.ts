import {
  resolveAgentActivityCapability,
  resolveAgentActivityUsage,
  type AgentActivityUsage,
  type CanonicalAgentSession
} from "@tutti-os/agent-activity-core";
import { useMemo, useRef } from "react";
import type {
  AgentSessionComposerSettings,
  AgentSessionReasoningEffort,
  AgentSessionState
} from "../../../shared/agentSessionTypes";
import type { AgentGUINodeData } from "../../../types";
import { composerSettingsSupportFromOptions } from "../model/composerSettingsSupport";
import { normalizeOptionalText } from "./agentGuiController.promptHelpers";
import {
  composerTargetDataForConversation,
  type AgentGUIComposerTargetData,
  type OptimisticComposerTarget
} from "./agentGuiController.composerPresentation";
import { resolvePromptImageSelectedModel } from "./agentGuiController.draftMessageHelpers";
import { useModelValue } from "../../../shared/engine/useEngineSelector";
import type { AgentGUIComposerOptionsReader } from "./agentGuiController.providerHelpers";

interface UseAgentGUIComposerCapabilitiesInput {
  activeConversationId: string | null;
  activeEngineSession: CanonicalAgentSession | null;
  activeSessionState: AgentSessionState | null;
  data: AgentGUINodeData;
  draftSettingsBySessionId: Record<string, AgentSessionComposerSettings>;
  optimisticComposerTarget?: OptimisticComposerTarget | null;
  selectedComposerTargetData: AgentGUIComposerTargetData;
  composer: AgentGUIComposerOptionsReader;
}

export function useAgentGUIComposerCapabilities(
  input: UseAgentGUIComposerCapabilitiesInput
) {
  const retainedUsageBySessionIdRef = useRef(
    new Map<string, AgentActivityUsage>()
  );
  const composerTargetData = composerTargetDataForConversation({
    activeConversationId: input.activeConversationId,
    activeSessionTarget: input.activeEngineSession,
    data: input.data,
    optimisticTarget: input.optimisticComposerTarget ?? null,
    selectedTarget: input.selectedComposerTargetData
  });
  const composerTargetKey = composerTargetData.agentTargetId?.trim() ?? "";
  const { composer } = input;
  const providerComposerOptions = useModelValue(
    () => composer.options(composerTargetKey),
    [composer, composerTargetKey]
  );
  const composerOptionsLoadStatus = useModelValue(
    () => composer.status(composerTargetKey),
    [composer, composerTargetKey]
  );
  const capabilitiesLoadStatus = useModelValue(
    () => composer.status(composerTargetKey, "capabilities"),
    [composer, composerTargetKey]
  );
  const connectorsLoadStatus = useModelValue(
    () => composer.status(composerTargetKey, "connectors"),
    [composer, composerTargetKey]
  );
  const composerOptionsLoading = Boolean(
    composerTargetKey &&
    (capabilitiesLoadStatus === "loading" ||
      (!providerComposerOptions && composerOptionsLoadStatus === "loading"))
  );
  const connectorOptionsLoading = Boolean(
    composerTargetKey && connectorsLoadStatus === "loading"
  );
  const defaultReasoningEffort: AgentSessionReasoningEffort | null = "high";
  const sessionCapabilities = input.activeEngineSession?.capabilities ?? null;
  const resolvedPromptImagesSupported =
    sessionCapabilities?.imageInput ??
    resolveAgentActivityCapability("imageInput", {
      composerOptions: providerComposerOptions,
      sessionCapabilities
    });
  const selectedModelForPromptImages =
    resolvePromptImageSelectedModel({
      activeConversationId: input.activeConversationId,
      activeSessionRuntimeContext: null,
      activeSessionSettings: input.activeSessionState?.settings ?? null,
      activeSessionPermissionModeId: input.activeSessionState?.permissionModeId,
      data: input.data,
      defaultReasoningEffort,
      draftSettingsBySessionId: input.draftSettingsBySessionId,
      providerComposerOptions,
      selectedComposerTargetData: input.selectedComposerTargetData
    }) ??
    normalizeOptionalText(providerComposerOptions?.effectiveSettings?.model);
  const modelImageInputRequired = Boolean(
    resolveAgentActivityCapability("modelImageInputRequired", {
      composerOptions: providerComposerOptions,
      sessionCapabilities
    })
  );
  const selectedModelImageInputSupported = !modelImageInputRequired
    ? true
    : selectedModelForPromptImages !== null &&
      (providerComposerOptions?.models.find(
        (option) => option.value === selectedModelForPromptImages
      )?.supportsImageInput ??
        false);
  const composerSupport = useMemo(() => {
    const fallback = composerSettingsSupportFromOptions(
      providerComposerOptions,
      sessionCapabilities
    );
    const targetSupport = composerSettingsSupportFromOptions(
      providerComposerOptions,
      null
    );
    return {
      ...fallback,
      browser:
        sessionCapabilities?.browserUse === true || targetSupport.browser,
      computer:
        sessionCapabilities?.computerUse === true || targetSupport.computer,
      permissionModeChangeDeferred:
        sessionCapabilities?.permissionModeChangeDeferred ??
        fallback.permissionModeChangeDeferred,
      permissionModeChangeDuringTurn:
        sessionCapabilities?.permissionModeChangeDuringTurn ??
        fallback.permissionModeChangeDuringTurn,
      plan: sessionCapabilities?.planMode ?? fallback.plan,
      planImplementation:
        sessionCapabilities?.planImplementation ?? fallback.planImplementation
    };
  }, [providerComposerOptions, sessionCapabilities]);

  const usageSource = input.activeEngineSession?.usage ?? null;
  const usage = useMemo(() => {
    const agentSessionId =
      input.activeEngineSession?.agentSessionId.trim() ?? "";
    if (!agentSessionId) {
      return null;
    }
    const resolved = resolveAgentActivityUsage({
      sessionUsage: usageSource
    });
    if (resolved) {
      retainedUsageBySessionIdRef.current.set(agentSessionId, resolved);
      return resolved;
    }
    return retainedUsageBySessionIdRef.current.get(agentSessionId) ?? null;
  }, [input.activeEngineSession?.agentSessionId, usageSource]);

  return {
    compactSupported:
      sessionCapabilities?.compact ??
      resolveAgentActivityCapability("compact", {
        composerOptions: providerComposerOptions,
        sessionCapabilities
      }),
    composerSupport,
    composerOptionsLoadStatus,
    composerOptionsLoading,
    connectorOptionsLoading,
    composerTargetData,
    defaultReasoningEffort,
    promptImagesSupported:
      (resolvedPromptImagesSupported ?? true) &&
      selectedModelImageInputSupported,
    providerComposerOptions,
    usage
  };
}
