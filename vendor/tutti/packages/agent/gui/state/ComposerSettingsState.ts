import type {
  AgentActivityComposerOptions,
  AgentActivityComposerOptionsLoadStatus,
  AgentActivitySession
} from "@tutti-os/agent-activity-core";
import { areWorkspaceUserProjectPathsEqual } from "@tutti-os/workspace-user-project/core";
import { computed, makeObservable } from "mobx";
import {
  cloneComposerSettings,
  modelSelectionFromComposerOptions,
  normalizePermissionModeId,
  permissionConfigFromComposerOptions,
  permissionModeOptions,
  readNodeDefaultDraftSettings,
  reasoningSelectionFromComposerOptions,
  speedSelectionFromComposerOptions
} from "../agent-gui/agentGuiNode/controller/agentGuiController.composerHelpers";
import {
  composerModelChoiceHistoryTargetId,
  enforceComposerModelBindingForHomeDefaults,
  isForegroundModelOptionsLoading,
  resolveComposerSettingsPresentation,
  sanitizeComposerSettingsForTarget,
  type AgentGUIComposerTargetData
} from "../agent-gui/agentGuiNode/controller/agentGuiController.composerPresentation";
import { normalizeOptionalText } from "../agent-gui/agentGuiNode/controller/agentGuiController.promptHelpers";
import { overlayComposerDefaults } from "../agent-gui/agentGuiNode/controller/agentGuiController.providerHelpers";
import type { AgentGUIConversationSummary } from "../agent-gui/agentGuiNode/model/agentGuiConversationModel";
import type { AgentGUIConversationUserProject } from "../agent-gui/agentGuiNode/model/agentGuiConversationProjectResolver";
import type { AgentGUIComposerSettingsVM } from "../agent-gui/agentGuiNode/model/agentGuiNodeTypes";
import { slashCommandPolicyFromComposerOptions } from "../agent-gui/agentGuiNode/model/agentSlashCommandProviderPolicy";
import { composerSettingsSupportFromOptions } from "../agent-gui/agentGuiNode/model/composerSettingsSupport";
import type { ConversationServices } from "../conversationServices";
import type {
  AgentSessionComposerSettings,
  AgentSessionReasoningEffort,
  AgentSessionSpeed,
  AgentSessionState
} from "../shared/agentSessionTypes";
import type { AgentGUINodeData, AgentGUIProvider } from "../types";
export interface ComposerSettingsInput {
  activeConversation: AgentGUIConversationSummary | null;
  activeConversationId: string | null;
  activeCanonicalSession: Pick<AgentActivitySession, "settings"> | null;
  activeSessionState: AgentSessionState | null;
  agentActivityRuntime: ConversationServices;
  composerSupport: ReturnType<typeof composerSettingsSupportFromOptions>;
  composerOptionsLoadStatus?: AgentActivityComposerOptionsLoadStatus;
  composerOptionsLoading: boolean;
  connectorOptionsLoading: boolean;
  composerTargetProvider: AgentGUIProvider;
  codexSaverModeEntryEnabled?: boolean;
  rtkSaverModeEntryEnabled?: boolean;
  data: AgentGUINodeData;
  defaultReasoningEffort: AgentSessionReasoningEffort | null;
  draftSettingsBySessionId: Record<string, AgentSessionComposerSettings>;
  providerComposerOptions: AgentActivityComposerOptions | null;
  composerTargetData: AgentGUIComposerTargetData;
  selectedComposerTargetData: AgentGUIComposerTargetData;
  selectedProjectPath: string | null;
  shouldApplyPreparedProjectSelection: boolean;
  userProjects: readonly AgentGUIConversationUserProject[];
}
function resolveSelectedProjectSectionKey(
  selectedProjectPath: string | null,
  userProjects: readonly AgentGUIConversationUserProject[]
): string | null {
  const projectPath = selectedProjectPath?.trim() ?? "";
  if (!projectPath) {
    return "conversations";
  }
  return (
    userProjects
      .find((project) =>
        areWorkspaceUserProjectPathsEqual(project.path, projectPath)
      )
      ?.sectionKey?.trim() || null
  );
}
export class ComposerSettingsState {
  constructor(private readonly input: ComposerSettingsInput) {
    makeObservable(this, {
      sessionSettings: computed.struct,
      storedNodeDefaultSettings: computed.struct,
      targetSafeNodeDefaultSettings: computed.struct,
      homeComposerSettings: computed.struct,
      activeConversationDraftSettings: computed.struct,
      presentedDraftSettings: computed.struct,
      draftSettings: computed.struct,
      persistedDraftModel: computed.struct,
      usesPlaceholderDraftModel: computed.struct,
      liveConfigModel: computed.struct,
      draftModel: computed.struct,
      draftReasoningEffort: computed.struct,
      draftSpeed: computed.struct,
      activeSessionReasoningSelection: computed.struct,
      optionsReasoningEffort: computed.struct,
      activeSessionModelSelection: computed.struct,
      activeSessionSpeedSelection: computed.struct,
      composerSettings: computed.struct,
      stableComposerSettings: computed.struct
    });
  }
  get sessionSettings() {
    return cloneComposerSettings(
      this.input.activeSessionState?.settings ?? null
    );
  }

  get storedNodeDefaultSettings() {
    return readNodeDefaultDraftSettings({
      data:
        this.input.activeConversationId === null
          ? this.input.selectedComposerTargetData.data
          : this.input.data,
      defaultReasoningEffort: this.input.defaultReasoningEffort,
      drafts: this.input.draftSettingsBySessionId
    });
  }

  get targetSafeNodeDefaultSettings() {
    return this.input.activeConversationId === null
      ? enforceComposerModelBindingForHomeDefaults(
          sanitizeComposerSettingsForTarget({
            settings: this.storedNodeDefaultSettings,
            target: this.input.selectedComposerTargetData,
            options: this.input.providerComposerOptions
          }),
          this.input.providerComposerOptions
        )
      : this.storedNodeDefaultSettings;
  }

  get homeComposerSettings() {
    return resolveComposerSettingsPresentation({
      active: false,
      homeSettings: this.targetSafeNodeDefaultSettings,
      options: this.input.providerComposerOptions
    });
  }

  get activeConversationDraftSettings() {
    return this.input.activeConversationId
      ? (this.input.draftSettingsBySessionId[this.input.activeConversationId] ??
          null)
      : null;
  }

  get presentedDraftSettings() {
    return resolveComposerSettingsPresentation({
      active: this.input.activeConversationId !== null,
      homeSettings: this.homeComposerSettings,
      optimisticSettings: this.activeConversationDraftSettings,
      options: this.input.providerComposerOptions,
      permissionModeId: this.input.activeSessionState?.permissionModeId,
      sessionSettings: this.sessionSettings
    });
  }

  get draftSettings() {
    return this.input.activeConversationId === null
      ? enforceComposerModelBindingForHomeDefaults(
          this.presentedDraftSettings,
          this.input.providerComposerOptions
        )
      : this.presentedDraftSettings;
  }

  get persistedDraftModel() {
    return normalizeOptionalText(this.draftSettings.model);
  }

  get usesPlaceholderDraftModel() {
    return (
      this.persistedDraftModel === null ||
      this.persistedDraftModel === "default"
    );
  }

  get liveConfigModel() {
    return this.input.activeConversationId !== null &&
      this.usesPlaceholderDraftModel
      ? normalizeOptionalText(
          this.input.activeCanonicalSession?.settings?.model
        )
      : null;
  }

  get draftModel() {
    return this.usesPlaceholderDraftModel
      ? (this.liveConfigModel ?? this.persistedDraftModel)
      : this.persistedDraftModel;
  }

  get draftReasoningEffort() {
    return normalizeOptionalText(
      this.draftSettings.reasoningEffort
    ) as AgentSessionReasoningEffort | null;
  }

  get draftSpeed() {
    return normalizeOptionalText(
      this.draftSettings.speed
    ) as AgentSessionSpeed | null;
  }

  get activeSessionReasoningSelection() {
    return reasoningSelectionFromComposerOptions(
      this.input.providerComposerOptions,
      this.draftReasoningEffort,
      this.draftModel
    );
  }

  get optionsReasoningEffort() {
    return this.activeSessionReasoningSelection
      ? this.activeSessionReasoningSelection.currentValue
      : this.draftReasoningEffort;
  }

  get activeSessionModelSelection() {
    return modelSelectionFromComposerOptions(
      this.input.providerComposerOptions,
      this.draftModel
    );
  }

  get activeSessionSpeedSelection() {
    return speedSelectionFromComposerOptions(
      this.input.providerComposerOptions,
      this.draftSpeed
    );
  }

  get composerSettings(): AgentGUIComposerSettingsVM {
    const permissionConfig = permissionConfigFromComposerOptions(
      this.input.providerComposerOptions
    );
    const supportsPermissionMode = Boolean(
      permissionConfig?.configurable && permissionConfig.modes.length > 0
    );
    const hasOptionsSource = this.input.providerComposerOptions !== null;
    const hasACPSettings =
      hasOptionsSource &&
      (!this.input.composerSupport.model ||
        this.activeSessionModelSelection !== null) &&
      (!this.input.composerSupport.reasoning ||
        this.activeSessionReasoningSelection !== null);
    const composerOptionsError =
      this.input.composerOptionsLoadStatus === "error" && !hasOptionsSource;
    const selectedPermissionModeValue =
      normalizePermissionModeId(this.draftSettings.permissionModeId) ??
      normalizePermissionModeId(permissionConfig?.defaultValue);
    const protectedSettings = overlayComposerDefaults(
      {
        codexSaverMode:
          this.input.providerComposerOptions?.effectiveSettings
            ?.codexSaverMode === true,
        rtkSaverMode:
          this.input.providerComposerOptions?.effectiveSettings
            ?.rtkSaverMode === true,
        model: this.draftModel,
        permissionModeId: selectedPermissionModeValue,
        reasoningEffort: this.optionsReasoningEffort,
        speed: this.draftSpeed
      },
      this.input.activeConversationId === null
        ? this.input.selectedComposerTargetData.agentTargetId
          ? this.storedNodeDefaultSettings
          : null
        : this.sessionSettings
    );
    const presentedModel = normalizeOptionalText(protectedSettings.model);
    const presentedReasoningEffort = normalizeOptionalText(
      protectedSettings.reasoningEffort
    ) as AgentSessionReasoningEffort | null;
    const presentedSpeed = normalizeOptionalText(
      protectedSettings.speed
    ) as AgentSessionSpeed | null;
    const presentedPermissionMode = normalizePermissionModeId(
      protectedSettings.permissionModeId
    );
    return {
      sessionSettings: this.sessionSettings,
      draftSettings: {
        codexSaverMode:
          this.input.codexSaverModeEntryEnabled === true &&
          this.input.providerComposerOptions?.codexSaverModeSupported ===
            true &&
          protectedSettings.codexSaverMode === true,
        rtkSaverMode:
          this.input.rtkSaverModeEntryEnabled === true &&
          this.input.providerComposerOptions?.rtkSaverModeSupported === true &&
          protectedSettings.rtkSaverMode === true,
        model: presentedModel,
        reasoningEffort: presentedReasoningEffort,
        speed: presentedSpeed,
        planMode: Boolean(this.draftSettings.planMode),
        browserUse: this.draftSettings.browserUse ?? true,
        computerUse: this.draftSettings.computerUse ?? true,
        permissionModeId: presentedPermissionMode
      },
      supportsModel: this.input.composerSupport.model,
      supportsCodexSaverMode:
        this.input.codexSaverModeEntryEnabled &&
        this.input.providerComposerOptions?.codexSaverModeSupported === true,
      supportsRTKSaverMode:
        this.input.rtkSaverModeEntryEnabled &&
        this.input.providerComposerOptions?.rtkSaverModeSupported === true,
      supportsReasoningEffort: this.input.composerSupport.reasoning,
      supportsSpeed: this.input.composerSupport.speed,
      supportsBrowser: this.input.composerSupport.browser,
      supportsComputerUse: this.input.composerSupport.computer,
      permissionModeChangeDuringTurn:
        this.input.composerSupport.permissionModeChangeDuringTurn,
      slashCommandPolicy: slashCommandPolicyFromComposerOptions(
        this.input.providerComposerOptions
      ),
      supportsPermissionMode,
      supportsPlanMode: this.input.composerSupport.plan,
      planExclusiveWithPermissionMode:
        this.input.providerComposerOptions?.behavior
          ?.planModeExclusiveWithPermissionMode === true,
      composerOptionsError,
      composerOptionsLoadStatus: this.input.composerOptionsLoadStatus,
      isSettingsLoading: !hasACPSettings && !composerOptionsError,
      isCapabilityOptionsLoading: this.input.composerOptionsLoading,
      isConnectorOptionsLoading: this.input.connectorOptionsLoading,
      isModelOptionsLoading: isForegroundModelOptionsLoading({
        modelOptionsLoading:
          this.input.providerComposerOptions?.modelOptionsLoading,
        selection: this.activeSessionModelSelection,
        supportsModel: this.input.composerSupport.model
      }),
      modelChoiceHistory: {
        targetId: composerModelChoiceHistoryTargetId({
          activeConversationId: this.input.activeConversationId,
          target: this.input.composerTargetData
        }),
        catalog: this.input.providerComposerOptions
          ? {
              authoritative:
                this.input.providerComposerOptions.behavior
                  .modelOptionsAuthoritative === true,
              loading:
                this.input.providerComposerOptions.modelOptionsLoading === true,
              effectiveModel: normalizeOptionalText(
                this.input.providerComposerOptions.effectiveSettings?.model
              ),
              models: this.input.providerComposerOptions.models.map(
                (option) => ({
                  value: option.value,
                  ...(option.requested === true ? { requested: true } : {})
                })
              )
            }
          : null
      },
      modelUnavailable:
        this.input.activeConversationId !== null &&
        this.sessionSettings === null &&
        this.input.composerSupport.model &&
        this.draftModel === null,
      reasoningUnavailable:
        this.input.activeConversationId !== null &&
        this.sessionSettings === null &&
        this.input.composerSupport.reasoning &&
        this.draftReasoningEffort === null,
      speedUnavailable:
        this.input.activeConversationId !== null &&
        this.sessionSettings === null &&
        this.input.composerSupport.speed &&
        this.draftSpeed === null,
      permissionModeUnavailable:
        this.input.activeConversationId !== null &&
        this.sessionSettings === null &&
        supportsPermissionMode &&
        selectedPermissionModeValue === null,
      selectedModelValue: presentedModel,
      effectiveModelValue:
        this.input.providerComposerOptions?.effectiveModel ?? null,
      selectedReasoningEffortValue: presentedReasoningEffort,
      selectedSpeedValue: presentedSpeed,
      selectedPermissionModeValue: presentedPermissionMode,
      permissionConfig,
      selectedProjectPath:
        this.input.activeConversationId !== null
          ? (this.input.activeConversation?.cwd ?? null)
          : this.input.selectedProjectPath,
      selectedProjectSectionKey:
        this.input.activeConversationId !== null
          ? (this.input.activeConversation?.railSectionKey ?? null)
          : resolveSelectedProjectSectionKey(
              this.input.selectedProjectPath,
              this.input.userProjects
            ),
      shouldApplyPreparedProjectSelection:
        this.input.activeConversationId === null &&
        this.input.shouldApplyPreparedProjectSelection,
      projectLocked: this.input.activeConversationId !== null,
      projectPathIsRemote:
        this.input.agentActivityRuntime.environment?.projectPathIsRemote,
      collapseModelOptionsToLatest:
        this.input.providerComposerOptions?.behavior
          .collapseModelOptionsToLatest === true,
      modelPlan: this.input.providerComposerOptions?.modelPlan ?? null,
      modelSwitchTakesEffectNextTurn:
        this.input.activeConversationId !== null &&
        this.input.composerSupport.modelSwitch,
      availableModels:
        this.input.composerSupport.model &&
        hasOptionsSource &&
        this.activeSessionModelSelection !== null
          ? this.activeSessionModelSelection.options
          : [],
      availableReasoningEfforts:
        this.input.composerSupport.reasoning &&
        hasOptionsSource &&
        this.activeSessionReasoningSelection !== null
          ? this.activeSessionReasoningSelection.options
          : [],
      availableSpeeds:
        this.input.composerSupport.speed &&
        hasOptionsSource &&
        this.activeSessionSpeedSelection !== null
          ? this.activeSessionSpeedSelection.options
          : [],
      availablePermissionModes: supportsPermissionMode
        ? permissionModeOptions(
            this.input.composerTargetProvider,
            permissionConfig
          )
        : []
    };
  }

  get stableComposerSettings() {
    return this.composerSettings;
  }
}
