// Agent GUI controller — composer settings, drafts, and permission labels.

import type {
  AgentActivityComposerOptions,
  AgentActivitySlashCommandPolicy
} from "@tutti-os/agent-activity-core";
import type { AgentPromptContentBlock } from "../../../shared/contracts/dto";
import type {
  AgentSessionComposerSettings,
  AgentSessionPermissionConfig,
  AgentSessionPermissionModeOption,
  AgentSessionReasoningEffort,
  AgentSessionSpeed
} from "../../../shared/agentSessionTypes";
import type { AgentGUINodeData } from "../../../types";
import { translate } from "../../../i18n/index";
import type {
  AgentGUIComposerSettingOption,
  AgentGUIProviderSkillOption
} from "../model/agentGuiNodeTypes";
import type { ACPConfigOptionSelection } from "./agentGuiController.types";
import { normalizeOptionalText } from "./agentGuiController.promptHelpers";

export function normalizeConfigOptionValue(value: unknown): string | null {
  return typeof value === "string" && value.trim() ? value.trim() : null;
}

export function composerSettingOptionsFromActivity(
  options: readonly AgentActivityComposerOptions["models"][number][]
): AgentGUIComposerSettingOption[] {
  return options.map((option) => ({ ...option }));
}

function reasoningOptionsFromRuntimeConfig(
  runtimeContext: Record<string, unknown> | null | undefined
): {
  currentValue: string | null;
  options: AgentGUIComposerSettingOption[];
} | null {
  const configOptions = runtimeContext?.configOptions;
  if (!Array.isArray(configOptions)) {
    return null;
  }
  for (const rawOption of configOptions) {
    const option = recordValue(rawOption);
    const id = normalizeConfigOptionValue(option?.id);
    if (
      !option ||
      !id ||
      !["reasoning_effort", "model_reasoning_effort", "effort"].includes(id)
    ) {
      continue;
    }
    const rawEntries = option.options;
    if (!Array.isArray(rawEntries)) {
      return null;
    }
    const options: AgentGUIComposerSettingOption[] = [];
    for (const rawEntry of rawEntries) {
      const entry = recordValue(rawEntry);
      const value = normalizeConfigOptionValue(entry?.value);
      if (
        !entry ||
        !value ||
        options.some((candidate) => candidate.value === value)
      ) {
        continue;
      }
      const label =
        normalizeConfigOptionValue(entry.name) ??
        normalizeConfigOptionValue(entry.label) ??
        value;
      const description = normalizeConfigOptionValue(entry.description);
      options.push({
        value,
        label,
        ...(description ? { description } : {})
      });
    }
    return {
      currentValue: normalizeConfigOptionValue(
        option.currentValue ?? option.current_value
      ),
      options
    };
  }
  return null;
}

function recordValue(value: unknown): Record<string, unknown> | null {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : null;
}

function reasoningProfileForModel(
  options: AgentActivityComposerOptions | null,
  model: string | null
): {
  defaultValue: string | null;
  options: AgentGUIComposerSettingOption[];
} | null {
  if (!options || !model) {
    return null;
  }
  const profile = options.reasoningOptionsByModel?.[model];
  if (!profile || !Array.isArray(profile.options)) {
    return null;
  }
  return {
    defaultValue: normalizeConfigOptionValue(profile.defaultValue),
    options: profile.options.map((option) => ({ ...option }))
  };
}

export function reasoningSelectionForModelFromComposerOptions(
  options: AgentActivityComposerOptions | null,
  currentValue: AgentSessionReasoningEffort | null,
  selectedModel: string | null
): ACPConfigOptionSelection | null {
  const modelProfile = reasoningProfileForModel(options, selectedModel);
  if (!modelProfile) {
    return null;
  }
  const supportedValues = new Set(
    modelProfile.options.map((option) => option.value)
  );
  return {
    options: modelProfile.options,
    currentValue: ((currentValue && supportedValues.has(currentValue)
      ? currentValue
      : null) ??
      modelProfile.defaultValue ??
      modelProfile.options[0]?.value ??
      null) as AgentSessionReasoningEffort | null
  };
}

export function modelSelectionFromComposerOptions(
  options: AgentActivityComposerOptions | null,
  currentValue: string | null
): ACPConfigOptionSelection | null {
  if (!options) {
    return null;
  }
  return {
    options: composerSettingOptionsFromActivity(options.models),
    currentValue
  };
}

export function reasoningSelectionFromComposerOptions(
  options: AgentActivityComposerOptions | null,
  currentValue: AgentSessionReasoningEffort | null,
  selectedModel: string | null = null,
  sessionRuntimeContext: Record<string, unknown> | null = null
): ACPConfigOptionSelection | null {
  const liveConfig = reasoningOptionsFromRuntimeConfig(sessionRuntimeContext);
  const modelSelection = reasoningSelectionForModelFromComposerOptions(
    options,
    currentValue,
    selectedModel
  );
  const sourceOptions = liveConfig
    ? liveConfig.options
    : modelSelection
      ? modelSelection.options
      : options
        ? composerSettingOptionsFromActivity(options.reasoningEfforts)
        : [];
  if (!options && !liveConfig && !modelSelection) {
    return null;
  }
  const supportedValues = new Set(sourceOptions.map((option) => option.value));
  const supportedValue = (
    value: AgentSessionReasoningEffort | string | null | undefined
  ): AgentSessionReasoningEffort | null =>
    value && supportedValues.has(value)
      ? (value as AgentSessionReasoningEffort)
      : null;
  const resolvedCurrentValue =
    supportedValue(currentValue) ??
    supportedValue(liveConfig?.currentValue) ??
    supportedValue(modelSelection?.currentValue) ??
    (sourceOptions[0]?.value as AgentSessionReasoningEffort | undefined) ??
    null;
  return {
    options: sourceOptions,
    currentValue: resolvedCurrentValue as AgentSessionReasoningEffort | null
  };
}

export function speedSelectionFromComposerOptions(
  options: AgentActivityComposerOptions | null,
  currentValue: AgentSessionSpeed | null
): ACPConfigOptionSelection | null {
  if (!options) {
    return null;
  }
  return {
    options: composerSettingOptionsFromActivity(options.speeds ?? []),
    currentValue
  };
}

export function providerSkillsFromComposerOptions(
  options: AgentActivityComposerOptions | null
): AgentGUIProviderSkillOption[] {
  if (!options) {
    return [];
  }
  const invocationByTrigger = new Map(
    (options.capabilityCatalog ?? []).flatMap((capability) =>
      capability.trigger &&
      capability.status === "available" &&
      (capability.invocation === "promptItem" ||
        capability.invocation === "textTrigger")
        ? [[capability.trigger, capability.invocation] as const]
        : []
    )
  );
  return dedupeProviderSkills([
    ...options.skills.map((skill) => ({
      ...skill,
      ...(invocationByTrigger.get(skill.trigger)
        ? { invocation: invocationByTrigger.get(skill.trigger) }
        : {})
    })),
    ...(options.capabilityCatalog ?? [])
      .filter((capability) => {
        if (capability.kind === "connector") {
          return (
            capability.invocation !== "none" && Boolean(capability.trigger)
          );
        }
        return (
          capability.kind === "skill" &&
          capability.invocation === "promptItem" &&
          capability.status === "available" &&
          Boolean(capability.trigger) &&
          Boolean(capability.path)
        );
      })
      .map((capability): AgentGUIProviderSkillOption => {
        const isConnector = capability.kind === "connector";
        return {
          name: isConnector ? capability.label : capability.name,
          ...(isConnector ? { connectorKey: capability.name } : {}),
          trigger: capability.trigger!,
          invocation:
            capability.invocation === "textTrigger"
              ? "textTrigger"
              : "promptItem",
          sourceKind: isConnector ? "connector" : "plugin",
          kind: isConnector ? "connector" : "skill",
          ...(isConnector ? { status: capability.status } : {}),
          ...(isConnector && capability.iconUrl
            ? { iconUrl: capability.iconUrl }
            : {}),
          ...(isConnector && capability.installedAtUnixMs
            ? { installedAtUnixMs: capability.installedAtUnixMs }
            : {}),
          ...(capability.description
            ? { description: capability.description }
            : {}),
          ...(capability.pluginName
            ? { pluginName: capability.pluginName }
            : {}),
          ...(capability.path ? { path: capability.path } : {})
        };
      })
  ]);
}

export function areProviderSkillOptionsEqual(
  left: AgentGUIProviderSkillOption,
  right: AgentGUIProviderSkillOption
): boolean {
  return (
    left.name === right.name &&
    left.connectorKey === right.connectorKey &&
    left.iconUrl === right.iconUrl &&
    left.installedAtUnixMs === right.installedAtUnixMs &&
    left.trigger === right.trigger &&
    left.invocation === right.invocation &&
    left.sourceKind === right.sourceKind &&
    left.description === right.description &&
    left.pluginName === right.pluginName &&
    left.path === right.path &&
    left.kind === right.kind &&
    left.status === right.status
  );
}

export function areProviderSkillOptionListsEqual(
  left: readonly AgentGUIProviderSkillOption[],
  right: readonly AgentGUIProviderSkillOption[]
): boolean {
  return (
    left.length === right.length &&
    left.every((skill, index) =>
      areProviderSkillOptionsEqual(skill, right[index]!)
    )
  );
}

export function slashCommandPoliciesEqual(
  left: AgentActivitySlashCommandPolicy | null | undefined,
  right: AgentActivitySlashCommandPolicy | null | undefined
): boolean {
  if (!left || !right) {
    return left === right;
  }
  return (
    (left.commandCatalogAuthoritative ?? false) ===
      (right.commandCatalogAuthoritative ?? false) &&
    left.fallbackCommands.length === right.fallbackCommands.length &&
    left.fallbackCommands.every(
      (command, index) => command === right.fallbackCommands[index]
    ) &&
    left.commandEffects.length === right.commandEffects.length &&
    left.commandEffects.every((effect, index) => {
      const other = right.commandEffects[index];
      return (
        other !== undefined &&
        effect.command === other.command &&
        effect.effect === other.effect
      );
    })
  );
}

function dedupeProviderSkills(
  skills: readonly AgentGUIProviderSkillOption[]
): AgentGUIProviderSkillOption[] {
  const seen = new Set<string>();
  const result: AgentGUIProviderSkillOption[] = [];
  for (const skill of skills) {
    const key = skill.trigger || `${skill.kind ?? "skill"}:${skill.name}`;
    if (seen.has(key)) {
      continue;
    }
    seen.add(key);
    result.push(skill);
  }
  return result;
}

export function permissionConfigFromComposerOptions(
  options: AgentActivityComposerOptions | null
): AgentSessionPermissionConfig | null {
  const config = options?.permissionConfig;
  if (!config) {
    return null;
  }
  const defaultValue = normalizePermissionModeId(config.defaultValue);
  return {
    configurable: config.configurable,
    ...(defaultValue ? { defaultValue } : {}),
    modes: config.modes.map((mode) => ({
      id: mode.id,
      label: mode.label,
      description: mode.description,
      semantic: normalizePermissionModeSemantic(mode.semantic)
    }))
  };
}

export function normalizePermissionModeSemantic(
  value: string | undefined
): AgentSessionPermissionModeOption["semantic"] {
  switch (value) {
    case "ask-before-write":
    case "accept-edits":
    case "locked-down":
    case "auto":
    case "full-access":
    case "unconfigurable":
      return value;
    default:
      return (normalizeOptionalText(value) ??
        "unconfigurable") as AgentSessionPermissionModeOption["semantic"];
  }
}

export function resolveEffectiveComposerSettings(input: {
  settings: AgentSessionComposerSettings;
}): AgentSessionComposerSettings {
  return {
    model: normalizeOptionalText(input.settings.model) ?? null,
    reasoningEffort:
      (normalizeOptionalText(
        input.settings.reasoningEffort
      ) as AgentSessionReasoningEffort | null) ?? null,
    speed:
      (normalizeOptionalText(
        input.settings.speed
      ) as AgentSessionSpeed | null) ?? null,
    planMode: Boolean(input.settings.planMode),
    // Browser/computer use default on; preserve explicit opt-outs.
    browserUse: input.settings.browserUse ?? true,
    computerUse: input.settings.computerUse ?? true,
    permissionModeId: normalizePermissionModeId(input.settings.permissionModeId)
  };
}

export function normalizePermissionModeId(
  value: string | null | undefined
): string | null {
  return normalizeOptionalText(value);
}

export function cloneComposerSettings(
  settings: AgentSessionComposerSettings | null
): AgentSessionComposerSettings | null {
  if (!settings) {
    return null;
  }
  return { ...settings };
}

export function sameComposerSettings(
  left: AgentSessionComposerSettings | null,
  right: AgentSessionComposerSettings | null
): boolean {
  return (
    left?.codexSaverMode === right?.codexSaverMode &&
    left?.rtkSaverMode === right?.rtkSaverMode &&
    (left?.model ?? null) === (right?.model ?? null) &&
    (left?.reasoningEffort ?? null) === (right?.reasoningEffort ?? null) &&
    (left?.speed ?? null) === (right?.speed ?? null) &&
    Boolean(left?.planMode) === Boolean(right?.planMode) &&
    (left?.browserUse ?? true) === (right?.browserUse ?? true) &&
    (left?.computerUse ?? true) === (right?.computerUse ?? true) &&
    (left?.permissionModeId ?? null) === (right?.permissionModeId ?? null)
  );
}

export function buildNodeDefaultComposerSettings(
  data: AgentGUINodeData,
  options?: {
    defaultReasoningEffort?: AgentSessionReasoningEffort | null;
    defaultSpeed?: AgentSessionSpeed | null;
  }
): AgentSessionComposerSettings {
  // Generic cleanup only — provider-level clamping is owned by the daemon
  // (normalizeComposerSettingsForProvider and the session create path).
  const composerOverrides = nodeComposerOverridesForProvider(data) ?? {};
  return {
    codexSaverMode: composerOverrides.codexSaverMode === true,
    rtkSaverMode: composerOverrides.rtkSaverMode === true,
    model: normalizeOptionalText(composerOverrides.model),
    reasoningEffort:
      (normalizeOptionalText(
        composerOverrides.reasoningEffort
      ) as AgentSessionReasoningEffort | null) ??
      options?.defaultReasoningEffort ??
      null,
    speed:
      (normalizeOptionalText(
        composerOverrides.speed
      ) as AgentSessionSpeed | null) ??
      options?.defaultSpeed ??
      null,
    planMode: Boolean(composerOverrides.planMode),
    browserUse: composerOverrides.browserUse ?? true,
    computerUse: composerOverrides.computerUse ?? true,
    permissionModeId: normalizePermissionModeId(
      composerOverrides.permissionModeId
    )
  };
}

export function nodeComposerOverridesForProvider(
  data: AgentGUINodeData
): AgentSessionComposerSettings | null {
  const agentTargetId = normalizeOptionalText(data.agentTargetId);
  if (agentTargetId) {
    return data.composerOverridesByAgentTargetId?.[agentTargetId] ?? null;
  }
  return (
    data.composerOverridesByProvider?.[data.provider] ??
    data.composerOverrides ??
    null
  );
}

export function permissionModeOptions(
  provider: AgentGUINodeData["provider"],
  permissionConfig: AgentSessionPermissionConfig | null | undefined
): AgentGUIComposerSettingOption[] {
  if (!permissionConfig?.configurable) {
    return [];
  }
  return permissionConfig.modes.map((mode) => ({
    value: mode.id,
    label: permissionModeLabel(provider, mode),
    description: permissionModeDescription(provider, mode)
  }));
}

export function nodeDataFromComposerSettings(
  current: AgentGUINodeData,
  settings: AgentSessionComposerSettings
): AgentGUINodeData {
  // Generic cleanup only — provider-level clamping is owned by the daemon.
  const composerOverrides = {
    codexSaverMode: settings.codexSaverMode === true,
    rtkSaverMode: settings.rtkSaverMode === true,
    model: normalizeOptionalText(settings.model),
    reasoningEffort: normalizeOptionalText(settings.reasoningEffort),
    speed: normalizeOptionalText(settings.speed),
    planMode: Boolean(settings.planMode),
    // Raw passthrough (no Boolean coercion): undefined means "default on", so
    // only an explicit false persists as an opt-out.
    browserUse: settings.browserUse,
    computerUse: settings.computerUse,
    permissionModeId: normalizePermissionModeId(settings.permissionModeId)
  };
  const agentTargetId = normalizeOptionalText(current.agentTargetId);
  if (agentTargetId) {
    return {
      ...current,
      composerOverridesByAgentTargetId: {
        ...current.composerOverridesByAgentTargetId,
        [agentTargetId]: composerOverrides
      }
    };
  }
  return {
    ...current,
    composerOverrides,
    composerOverridesByProvider: {
      ...current.composerOverridesByProvider,
      [current.provider]: composerOverrides
    }
  };
}

export function permissionModeLabel(
  provider: AgentGUINodeData["provider"],
  option: AgentSessionPermissionModeOption
): string {
  const providerKey = `agentHost.agentGui.permissionModes.${provider}.${option.id}.label`;
  const providerLabel = translate(providerKey);
  if (providerLabel !== providerKey) {
    return providerLabel;
  }
  const semanticKey = `agentHost.agentGui.permissionSemantics.${option.semantic}.label`;
  const semanticLabel = translate(semanticKey);
  if (semanticLabel !== semanticKey) {
    return semanticLabel;
  }
  const contractLabel = normalizeOptionalText(option.label);
  if (contractLabel) {
    return contractLabel;
  }
  return option.id;
}

export function permissionModeDescription(
  provider: AgentGUINodeData["provider"],
  option: AgentSessionPermissionModeOption
): string | undefined {
  const providerKey = `agentHost.agentGui.permissionModes.${provider}.${option.id}.description`;
  const providerLabel = translate(providerKey);
  if (providerLabel !== providerKey) {
    return providerLabel;
  }
  const semanticKey = `agentHost.agentGui.permissionSemantics.${option.semantic}.description`;
  const semanticLabel = translate(semanticKey);
  if (semanticLabel !== semanticKey) {
    return semanticLabel;
  }
  const contractDescription = normalizeOptionalText(option.description);
  if (contractDescription) {
    return contractDescription;
  }
  return undefined;
}

export const NODE_DEFAULT_DRAFT_KEY = "__agent_gui_node_defaults__";

export function nodeDefaultDraftKey(
  agentProvider: AgentGUINodeData["provider"],
  agentTargetId?: string | null
): string {
  const normalizedAgentTargetId = normalizeOptionalText(agentTargetId);
  if (normalizedAgentTargetId) {
    return `${NODE_DEFAULT_DRAFT_KEY}:target:${normalizedAgentTargetId}`;
  }
  return `${NODE_DEFAULT_DRAFT_KEY}:${agentProvider}`;
}

export function readNodeDefaultDraftSettings(input: {
  data: AgentGUINodeData;
  defaultReasoningEffort?: AgentSessionReasoningEffort | null;
  defaultSpeed?: AgentSessionSpeed | null;
  drafts: Record<string, AgentSessionComposerSettings>;
}): AgentSessionComposerSettings {
  const agentTargetId = normalizeOptionalText(input.data.agentTargetId);
  if (agentTargetId) {
    return (
      input.drafts[nodeDefaultDraftKey(input.data.provider, agentTargetId)] ??
      {}
    );
  }
  return (
    input.drafts[nodeDefaultDraftKey(input.data.provider)] ??
    input.drafts[NODE_DEFAULT_DRAFT_KEY] ??
    {}
  );
}
