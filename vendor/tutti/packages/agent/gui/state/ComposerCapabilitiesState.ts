import {
  resolveAgentActivityCapability,
  resolveAgentActivityUsage,
  type AgentActivityUsage,
  type CanonicalAgentSession
} from "@tutti-os/agent-activity-core";
import { comparer, computed, makeObservable } from "mobx";
import {
  composerTargetDataForConversation,
  type AgentGUIComposerTargetData,
  type OptimisticComposerTarget
} from "../agent-gui/agentGuiNode/controller/agentGuiController.composerPresentation";
import { resolvePromptImageSelectedModel } from "../agent-gui/agentGuiNode/controller/agentGuiController.draftMessageHelpers";
import { normalizeOptionalText } from "../agent-gui/agentGuiNode/controller/agentGuiController.promptHelpers";
import type { AgentGUIComposerOptionsReader } from "../agent-gui/agentGuiNode/controller/agentGuiController.providerHelpers";
import { composerSettingsSupportFromOptions } from "../agent-gui/agentGuiNode/model/composerSettingsSupport";
import type {
  AgentSessionComposerSettings,
  AgentSessionState
} from "../shared/agentSessionTypes";
import type { AgentGUINodeData } from "../types";
export interface ComposerCapabilitiesInput {
  activeConversationId: string | null;
  activeCanonicalSession: CanonicalAgentSession | null;
  activeSessionState: AgentSessionState | null;
  data: AgentGUINodeData;
  draftSettingsBySessionId: Record<string, AgentSessionComposerSettings>;
  optimisticComposerTarget?: OptimisticComposerTarget | null;
  selectedComposerTargetData: AgentGUIComposerTargetData;
  composer: AgentGUIComposerOptionsReader;
}
export class ComposerCapabilitiesState {
  constructor(
    private readonly input: ComposerCapabilitiesInput,
    usageHistory = new Map<string, AgentActivityUsage>()
  ) {
    this.retainedUsageBySessionIdRef.current = usageHistory;
    makeObservable(this, {
      optimisticComposerTarget: computed.struct,
      composerTargetData: computed.struct,
      composerTargetKey: computed.struct,
      providerComposerOptions: computed.struct,
      composerOptionsLoadStatus: computed.struct,
      capabilitiesLoadStatus: computed.struct,
      connectorsLoadStatus: computed.struct,
      composerOptionsLoading: computed.struct,
      connectorOptionsLoading: computed.struct,
      defaultReasoningEffort: computed.struct,
      sessionCapabilities: computed.struct,
      resolvedPromptImagesSupported: computed.struct,
      selectedModelForPromptImages: computed.struct,
      modelImageInputRequired: computed.struct,
      selectedModelImageInputSupported: computed.struct,
      composerSupport: computed.struct,
      usageSource: computed.struct,
      usage: computed.struct,
      compactSupported: computed.struct,
      promptImagesSupported: computed.struct
    });
  }
  private readonly retainedUsageBySessionIdRef = {
    current: new Map<string, AgentActivityUsage>()
  };

  get optimisticComposerTarget() {
    return this.input.optimisticComposerTarget ?? null;
  }

  get composerTargetData() {
    return composerTargetDataForConversation({
      activeConversationId: this.input.activeConversationId,
      activeSessionTarget: this.input.activeCanonicalSession,
      data: this.input.data,
      optimisticTarget: this.input.optimisticComposerTarget ?? null,
      selectedTarget: this.input.selectedComposerTargetData
    });
  }

  get composerTargetKey() {
    return this.composerTargetData.agentTargetId?.trim() ?? "";
  }

  get providerComposerOptions() {
    return this.input.composer.options(this.composerTargetKey);
  }

  get composerOptionsLoadStatus() {
    return this.input.composer.status(this.composerTargetKey);
  }

  get capabilitiesLoadStatus() {
    return this.input.composer.status(this.composerTargetKey, "capabilities");
  }

  get connectorsLoadStatus() {
    return this.input.composer.status(this.composerTargetKey, "connectors");
  }

  get composerOptionsLoading() {
    return Boolean(
      this.composerTargetKey &&
        (this.capabilitiesLoadStatus === "loading" ||
          (!this.providerComposerOptions &&
            this.composerOptionsLoadStatus === "loading"))
    );
  }

  get connectorOptionsLoading() {
    return Boolean(
      this.composerTargetKey && this.connectorsLoadStatus === "loading"
    );
  }

  get defaultReasoningEffort() {
    return "high" as const;
  }

  get sessionCapabilities() {
    return this.input.activeCanonicalSession?.capabilities ?? null;
  }

  get resolvedPromptImagesSupported() {
    return (
      this.sessionCapabilities?.imageInput ??
      resolveAgentActivityCapability("imageInput", {
        composerOptions: this.providerComposerOptions,
        sessionCapabilities: this.sessionCapabilities
      })
    );
  }

  get selectedModelForPromptImages() {
    return (
      resolvePromptImageSelectedModel({
        activeConversationId: this.input.activeConversationId,
        activeSessionRuntimeContext: null,
        activeSessionSettings: this.input.activeSessionState?.settings ?? null,
        activeSessionPermissionModeId:
          this.input.activeSessionState?.permissionModeId,
        data: this.input.data,
        defaultReasoningEffort: this.defaultReasoningEffort,
        draftSettingsBySessionId: this.input.draftSettingsBySessionId,
        providerComposerOptions: this.providerComposerOptions,
        selectedComposerTargetData: this.input.selectedComposerTargetData
      }) ??
      normalizeOptionalText(
        this.providerComposerOptions?.effectiveSettings?.model
      )
    );
  }

  get modelImageInputRequired() {
    return Boolean(
      resolveAgentActivityCapability("modelImageInputRequired", {
        composerOptions: this.providerComposerOptions,
        sessionCapabilities: this.sessionCapabilities
      })
    );
  }

  get selectedModelImageInputSupported() {
    return !this.modelImageInputRequired
      ? true
      : this.selectedModelForPromptImages !== null &&
          (this.providerComposerOptions?.models.find(
            (option) => option.value === this.selectedModelForPromptImages
          )?.supportsImageInput ??
            false);
  }

  get composerSupport() {
    const fallback = composerSettingsSupportFromOptions(
      this.providerComposerOptions,
      this.sessionCapabilities
    );
    const targetSupport = composerSettingsSupportFromOptions(
      this.providerComposerOptions,
      null
    );
    return {
      ...fallback,
      browser:
        this.sessionCapabilities?.browserUse === true || targetSupport.browser,
      computer:
        this.sessionCapabilities?.computerUse === true ||
        targetSupport.computer,
      permissionModeChangeDeferred:
        this.sessionCapabilities?.permissionModeChangeDeferred ??
        fallback.permissionModeChangeDeferred,
      permissionModeChangeDuringTurn:
        this.sessionCapabilities?.permissionModeChangeDuringTurn ??
        fallback.permissionModeChangeDuringTurn,
      plan: this.sessionCapabilities?.planMode ?? fallback.plan,
      planImplementation:
        this.sessionCapabilities?.planImplementation ??
        fallback.planImplementation
    };
  }

  get usageSource() {
    return this.input.activeCanonicalSession?.usage ?? null;
  }

  get usage() {
    const agentSessionId =
      this.input.activeCanonicalSession?.agentSessionId.trim() ?? "";
    if (!agentSessionId) {
      return null;
    }
    const resolved = resolveAgentActivityUsage({
      sessionUsage: this.usageSource
    });
    if (resolved) {
      const retained =
        this.retainedUsageBySessionIdRef.current.get(agentSessionId);
      if (retained && comparer.structural(retained, resolved)) return retained;
      this.retainedUsageBySessionIdRef.current.set(agentSessionId, resolved);
      return resolved;
    }
    return this.retainedUsageBySessionIdRef.current.get(agentSessionId) ?? null;
  }
  get compactSupported() {
    return (
      this.sessionCapabilities?.compact ??
      resolveAgentActivityCapability("compact", {
        composerOptions: this.providerComposerOptions,
        sessionCapabilities: this.sessionCapabilities
      })
    );
  }

  get promptImagesSupported() {
    return (
      (this.resolvedPromptImagesSupported ?? true) &&
      this.selectedModelImageInputSupported
    );
  }
}
