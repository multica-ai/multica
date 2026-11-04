import type {
  AgentActivityComposerOptions,
  AgentActivityComposerOptionsLoadStatus,
  AgentActivityInteraction,
  AgentActivityMessage,
  AgentActivitySnapshot
} from "@tutti-os/agent-activity-core";
import type {
  AgentModelCatalogInvalidatedEvent,
  AgentSessionCommand,
  AgentSessionComposerSettings
} from "../../../shared/agentSessionTypes";
import type { AgentProviderId } from "../../../shared/contracts/dto";
import type { AgentGUINodeData, AgentGUIAgentTarget } from "../../../types";
import { agentGUIAgentTargetRefsEqual } from "../../../agentTargets";
import {
  matchesAgentGUIConversationSummaryFilter,
  type AgentGUIConversationFilter
} from "../model/agentGuiConversationFilter";
import type { AgentGUIConversationSummary } from "../model/agentGuiConversationModel";
import { normalizeOptionalText } from "./agentGuiController.promptHelpers";
import type { AgentGUIComposerTargetData } from "./agentGuiController.composerPresentation";

export const EMPTY_AGENT_GUI_MESSAGES: readonly AgentActivityMessage[] = [];
export const EMPTY_AGENT_GUI_AVAILABLE_COMMANDS: AgentSessionCommand[] = [];
export const ACTIVITY_STREAM_STATE_RELOAD_DEBOUNCE_MS = 150;
export const AGENT_GUI_SUBMIT_RETARGET_EARLY_MESSAGE_TOLERANCE_MS = 5_000;

export type AgentGUIProviderRailTargetSelection =
  | "keep-active-conversation"
  | "open-home-composer";

export function resolveAgentGUIProviderRailTargetSelection(input: {
  activeConversation: AgentGUIConversationSummary | null;
  nextFilter: AgentGUIConversationFilter;
}): AgentGUIProviderRailTargetSelection {
  return input.nextFilter.kind === "agentTarget" &&
    input.activeConversation &&
    matchesAgentGUIConversationSummaryFilter(
      input.activeConversation,
      input.nextFilter
    )
    ? "keep-active-conversation"
    : "open-home-composer";
}

export function agentTargetForConversation(
  conversation: AgentGUIConversationSummary | null,
  targets: readonly AgentGUIAgentTarget[]
): AgentGUIAgentTarget | null {
  const agentTargetId = conversation?.agentTargetId?.trim() ?? "";
  if (!agentTargetId) return null;
  return (
    targets.find((target) => target.agentTargetId === agentTargetId) ?? null
  );
}

export function ownerDeviceLabelForConversation(
  conversation: AgentGUIConversationSummary | null,
  targets: readonly AgentGUIAgentTarget[]
): string | null {
  return (
    agentTargetForConversation(
      conversation,
      targets
    )?.ownerDeviceLabel?.trim() || null
  );
}

export function targetConnectionForAgentGUIView(input: {
  activeConversation: AgentGUIConversationSummary | null;
  selectedTarget: AgentGUIAgentTarget | null;
  targets: readonly AgentGUIAgentTarget[];
}): { agentTargetId: string | null; ownerDeviceLabel: string | null } {
  const presentationTarget = input.activeConversation
    ? agentTargetForConversation(input.activeConversation, input.targets)
    : input.selectedTarget;
  return {
    agentTargetId: input.activeConversation
      ? input.activeConversation.agentTargetId?.trim() || null
      : (presentationTarget?.agentTargetId?.trim() ?? null),
    ownerDeviceLabel: presentationTarget?.ownerDeviceLabel?.trim() || null
  };
}

export function agentActivityInteractionListsEqual(
  left: readonly AgentActivityInteraction[],
  right: readonly AgentActivityInteraction[]
): boolean {
  return (
    left === right ||
    (left.length === right.length &&
      left.every((interaction, index) => interaction === right[index]))
  );
}

export function mergeAgentModelCatalogInvalidationEvents(
  events: AgentModelCatalogInvalidatedEvent[]
): AgentModelCatalogInvalidatedEvent {
  const providers = new Set<AgentProviderId>();
  let occurredAtUnixMs = 0;
  for (const event of events) {
    occurredAtUnixMs = Math.max(occurredAtUnixMs, event.occurredAtUnixMs);
    for (const provider of event.providers) providers.add(provider);
  }
  const lastEvent = events[events.length - 1]!;
  return {
    ...lastEvent,
    providers: [...providers],
    occurredAtUnixMs: occurredAtUnixMs || lastEvent.occurredAtUnixMs
  };
}

export interface QueuedComposerSettingsUpdate {
  sessionSettingsPatch: AgentSessionComposerSettings;
}

export interface AgentGUIComposerDefaults {
  codexSaverMode?: boolean;
  rtkSaverMode?: boolean;
  model?: string | null;
  permissionModeId?: string | null;
  reasoningEffort?: string | null;
  speed?: string | null;
}

export interface AgentGUIRememberComposerDefaultsInput {
  agentTargetId: string | null;
  provider: AgentGUINodeData["provider"];
  defaults: AgentGUIComposerDefaults | null;
}

export const rememberComposerDefaultsFields = [
  "codexSaverMode",
  "rtkSaverMode",
  "model",
  "permissionModeId",
  "reasoningEffort",
  "speed"
] as const;

export type AgentGUIComposerDefaultsField =
  (typeof rememberComposerDefaultsFields)[number];

export interface AgentGUIRememberComposerDefaultsResult {
  acknowledgedFields: AgentGUIComposerDefaultsField[];
  supersededFields: AgentGUIComposerDefaultsField[];
}

export function composerDefaultsPatchFromSettings(
  touched: Partial<AgentSessionComposerSettings>,
  finalSettings: AgentSessionComposerSettings
): AgentGUIComposerDefaults | null {
  const patch: AgentGUIComposerDefaults = {};
  for (const field of rememberComposerDefaultsFields) {
    if (touched[field] === undefined) continue;
    if (field === "codexSaverMode" || field === "rtkSaverMode") {
      patch[field] = touched[field] === true;
      continue;
    }
    const touchedValue = normalizeOptionalText(touched[field]);
    if (touchedValue === null) continue;
    const finalValue = normalizeOptionalText(finalSettings[field]);
    if (finalValue === null) continue;
    patch[field] = finalValue;
  }
  return Object.keys(patch).length > 0 ? patch : null;
}

export function overlayComposerDefaults(
  base: AgentSessionComposerSettings,
  optimistic: AgentSessionComposerSettings | null | undefined
): AgentSessionComposerSettings {
  if (!optimistic) return base;
  const result = { ...base };
  for (const field of rememberComposerDefaultsFields) {
    if (field === "codexSaverMode" || field === "rtkSaverMode") {
      if (typeof optimistic[field] === "boolean") {
        result[field] = optimistic[field];
      }
      continue;
    }
    const value = normalizeOptionalText(optimistic[field]);
    if (value !== null) {
      Object.assign(result, { [field]: value });
    }
  }
  return result;
}

export function withoutAcknowledgedComposerDefaults(
  settings: AgentSessionComposerSettings,
  acknowledged: AgentGUIComposerDefaults
): AgentSessionComposerSettings {
  const result = { ...settings };
  for (const field of rememberComposerDefaultsFields) {
    if (field === "codexSaverMode" || field === "rtkSaverMode") {
      if (
        typeof acknowledged[field] === "boolean" &&
        result[field] === acknowledged[field]
      ) {
        delete result[field];
      }
      continue;
    }
    const acknowledgedValue = normalizeOptionalText(acknowledged[field]);
    if (
      acknowledgedValue !== null &&
      normalizeOptionalText(result[field]) === acknowledgedValue
    ) {
      delete result[field];
    }
  }
  return result;
}

export function normalizedComposerDefaultValue(
  settings: AgentSessionComposerSettings | AgentGUIComposerDefaults,
  field: AgentGUIComposerDefaultsField
): string | null {
  if (field === "codexSaverMode" || field === "rtkSaverMode") {
    return typeof settings[field] === "boolean"
      ? String(settings[field])
      : null;
  }
  return normalizeOptionalText(settings[field]);
}

export function composerTargetDataFromProviderTarget(input: {
  current: AgentGUINodeData;
  isExplicit: boolean;
  target: AgentGUIAgentTarget;
}): AgentGUIComposerTargetData {
  const agentTargetId =
    normalizeOptionalText(input.target.agentTargetId) ?? input.target.targetId;
  const currentAgentTargetId = normalizeOptionalText(
    input.current.agentTargetId
  );
  const canPromoteLegacyComposerOverrides =
    agentTargetId !== null &&
    currentAgentTargetId === null &&
    input.current.provider === input.target.provider &&
    input.current.composerOverrides != null &&
    !input.current.composerOverridesByAgentTargetId?.[agentTargetId];
  const composerOverridesByAgentTargetId = canPromoteLegacyComposerOverrides
    ? {
        ...(input.current.composerOverridesByAgentTargetId ?? {}),
        [agentTargetId]: input.current.composerOverrides!
      }
    : input.current.composerOverridesByAgentTargetId;
  const currentTargetIdentityChanged =
    input.current.provider !== input.target.provider ||
    (currentAgentTargetId !== null && currentAgentTargetId !== agentTargetId);
  return {
    agentTargetId,
    provider: input.target.provider,
    targetId: input.target.targetId,
    data: {
      ...input.current,
      provider: input.target.provider,
      agentTargetId,
      composerOverrides: canPromoteLegacyComposerOverrides
        ? null
        : currentTargetIdentityChanged
          ? null
          : input.current.composerOverrides,
      composerOverridesByAgentTargetId
    }
  };
}

export function isExplicitAgentGUIAgentTarget(
  target: AgentGUIAgentTarget,
  explicitTargets: readonly AgentGUIAgentTarget[]
): boolean {
  return explicitTargets.some(
    (candidate) =>
      candidate.provider === target.provider &&
      candidate.targetId === target.targetId &&
      agentGUIAgentTargetRefsEqual(candidate.ref, target.ref)
  );
}

export function agentGUINodeDataHasComposerTarget(
  data: AgentGUINodeData
): boolean {
  return normalizeOptionalText(data.agentTargetId) !== null;
}

/** What choosing composer settings reads of a target's options. */
export interface AgentGUIComposerOptionsReader {
  options(targetKey: string | null | undefined): AgentActivityComposerOptions | null;
  status(targetKey: string | null | undefined, section?: "core" | "capabilities" | "connectors"): AgentActivityComposerOptionsLoadStatus | undefined;
}

/** A conversation's unsettled write of its settings; none once it settles. */
export interface AgentGUISessionSettingsUpdate {
  errorCode: string | null;
  errorMessage: string | null;
  queuedSettings: Readonly<Record<string, unknown>> | null;
  settings: Readonly<Record<string, unknown>> | null;
  status: "failed" | "inFlight" | "unknown" | "waitingForRuntime";
}

/** Writing a conversation's settings: reads what is unsettled, asks for a change. */
export interface AgentGUIComposerSettingsLane {
  settingsUpdate(
    agentSessionId: string | null | undefined
  ): AgentGUISessionSettingsUpdate | null;
  updateSettings(input: {
    agentSessionId: string;
    settings: Readonly<Record<string, unknown>>;
  }): void;
}

export function composerOptionsForTarget(input: {
  composer: AgentGUIComposerOptionsReader;
  target: AgentGUIComposerTargetData;
}): AgentActivityComposerOptions | null {
  return input.composer.options(input.target.agentTargetId);
}

export function composerOptionsLoadingForTarget(input: {
  composer: AgentGUIComposerOptionsReader;
  target: AgentGUIComposerTargetData;
}): boolean {
  const targetKey = input.target.agentTargetId?.trim() ?? "";
  return Boolean(
    targetKey &&
    !input.composer.options(targetKey) &&
    input.composer.status(targetKey) === "loading"
  );
}

export function agentGUIProviderTargetsEqual(
  left: AgentGUIAgentTarget,
  right: AgentGUIAgentTarget
): boolean {
  return (
    left.provider === right.provider &&
    left.targetId === right.targetId &&
    (left.agentTargetId ?? null) === (right.agentTargetId ?? null) &&
    agentGUIAgentTargetRefsEqual(left.ref, right.ref)
  );
}
