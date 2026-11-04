import type {
  AgentActivityInteraction,
  AgentActivitySessionCapabilities,
  AgentActivitySessionPermissionModeOption,
  AgentActivitySessionSettings,
  AgentActivityTurn
} from "../types.ts";
import type { AgentActivitySessionInput } from "../sessionNormalization.ts";

export interface SessionProjectionScope {
  agentSessionId?: string;
  workspaceId: string;
}

/**
 * Decodes a Session projection before it reaches canonical reducers. Product
 * adapters own transport DTO validation; this boundary additionally protects
 * Engine invariants and every nested entity the reducers dereference.
 */
export function decodeSessionProjection(
  value: unknown,
  scope: SessionProjectionScope
): AgentActivitySessionInput | null {
  if (!isRecord(value)) return null;
  const agentSessionId = trimmedString(value.agentSessionId);
  if (
    !agentSessionId ||
    (scope.agentSessionId !== undefined &&
      agentSessionId !== scope.agentSessionId) ||
    trimmedString(value.workspaceId) !== scope.workspaceId ||
    !isNullableString(value.activeTurnId) ||
    typeof value.cwd !== "string" ||
    !isString(value.provider) ||
    typeof value.title !== "string" ||
    !isInteractionArray(value.latestTurnInteractions, agentSessionId) ||
    !isInteractionArray(value.pendingInteractions, agentSessionId)
  ) {
    return null;
  }
  const activeTurn = optionalTurn(value.activeTurn, agentSessionId);
  const latestTurn = optionalTurn(value.latestTurn, agentSessionId);
  if (
    activeTurn === false ||
    latestTurn === false ||
    (activeTurn &&
      value.activeTurnId !== null &&
      activeTurn.turnId.trim() !== value.activeTurnId.trim()) ||
    !optionalSessionFieldsMatch(value, agentSessionId, scope.workspaceId)
  ) {
    return null;
  }
  return value as unknown as AgentActivitySessionInput;
}

export function decodeTurnProjection(
  value: unknown,
  allowedSessionIds: ReadonlySet<string>
): AgentActivityTurn | null {
  if (!isRecord(value)) return null;
  const agentSessionId = trimmedString(value.agentSessionId);
  const turnId = trimmedString(value.turnId);
  if (
    !agentSessionId ||
    !allowedSessionIds.has(agentSessionId) ||
    !turnId ||
    !isOneOf(value.origin, TURN_ORIGINS) ||
    !isOneOf(value.phase, TURN_PHASES) ||
    !isFiniteNumber(value.startedAtUnixMs) ||
    !isFiniteNumber(value.updatedAtUnixMs) ||
    !optionalNullableOneOf(value.outcome, TURN_OUTCOMES) ||
    !optionalNullableNumber(value.settledAtUnixMs) ||
    !optionalNullableString(value.sourceGoalOperationId) ||
    !optionalNullableNumber(value.sourceGoalRevision) ||
    !optionalNullableNumber(value.sourceGoalRepairEpoch) ||
    !optionalBoolean(value.providerForkBindingAvailable) ||
    !optionalOneOf(
      value.providerForkBindingState,
      PROVIDER_FORK_BINDING_STATES
    ) ||
    !optionalCapabilityReferences(value.capabilityRefs) ||
    !optionalCompletedCommand(value.completedCommand) ||
    !optionalTurnError(value.error) ||
    !optionalNullableRecord(value.fileChanges)
  ) {
    return null;
  }
  return value as unknown as AgentActivityTurn;
}

function optionalSessionFieldsMatch(
  value: Record<string, unknown>,
  agentSessionId: string,
  workspaceId: string
): boolean {
  return (
    optionalOneOf(value.kind, SESSION_KINDS) &&
    optionalNullableString(value.rootAgentSessionId) &&
    optionalNullableString(value.rootTurnId) &&
    optionalNullableString(value.parentAgentSessionId) &&
    optionalNullableString(value.parentTurnId) &&
    optionalNullableString(value.parentToolCallId) &&
    optionalNullableString(value.agentTargetId) &&
    optionalNullableString(value.providerSessionId) &&
    optionalString(value.userId) &&
    optionalNullableString(value.model) &&
    optionalNullableBoolean(value.noProject) &&
    optionalString(value.railSectionKey) &&
    optionalSettings(value.settings) &&
    optionalPermissionConfig(value.permissionConfig) &&
    optionalCapabilities(value.capabilities) &&
    optionalLifecycleCapabilities(value.lifecycleCapabilities) &&
    optionalBoolean(value.lifecycleCapabilitiesProjected) &&
    optionalForkLineage(value.forkedFrom) &&
    optionalUsage(value.usage) &&
    optionalBoolean(value.imported) &&
    optionalBoolean(value.visible) &&
    optionalBoolean(value.resumable) &&
    optionalNonNegativeInteger(value.messageVersion) &&
    optionalFiniteNumber(value.lastEventUnixMs) &&
    optionalFiniteNumber(value.startedAtUnixMs) &&
    optionalNullableNumber(value.endedAtUnixMs) &&
    optionalNullableNumber(value.pinnedAtUnixMs) &&
    optionalFiniteNumber(value.createdAtUnixMs) &&
    optionalFiniteNumber(value.updatedAtUnixMs)
  );
}

function isInteractionArray(
  value: unknown,
  agentSessionId: string
): value is readonly AgentActivityInteraction[] {
  return (
    Array.isArray(value) &&
    value.every((interaction) =>
      isInteractionProjection(interaction, agentSessionId)
    )
  );
}

function isInteractionProjection(
  value: unknown,
  agentSessionId: string
): value is AgentActivityInteraction {
  if (!isRecord(value)) return false;
  return (
    trimmedString(value.agentSessionId) === agentSessionId &&
    Boolean(trimmedString(value.turnId)) &&
    Boolean(trimmedString(value.requestId)) &&
    isOneOf(value.kind, INTERACTION_KINDS) &&
    isOneOf(value.status, INTERACTION_STATUSES) &&
    isFiniteNumber(value.createdAtUnixMs) &&
    isFiniteNumber(value.updatedAtUnixMs) &&
    optionalNullableRecord(value.input) &&
    optionalNullableRecord(value.metadata) &&
    optionalNullableRecord(value.output) &&
    optionalNullableString(value.toolName)
  );
}

function optionalTurn(
  value: unknown,
  agentSessionId: string
): AgentActivityTurn | null | false {
  if (value === undefined || value === null) return null;
  return decodeTurnProjection(value, new Set([agentSessionId])) ?? false;
}

function optionalSettings(value: unknown): boolean {
  if (value === undefined) return true;
  if (!isRecord(value)) return false;
  const settings = value as Record<keyof AgentActivitySessionSettings, unknown>;
  return (
    optionalNullableString(settings.model) &&
    optionalNullableString(settings.permissionModeId) &&
    optionalNullableBoolean(settings.planMode) &&
    optionalNullableBoolean(settings.browserUse) &&
    optionalNullableBoolean(settings.computerUse) &&
    optionalNullableString(settings.reasoningEffort) &&
    optionalNullableString(settings.speed)
  );
}

function optionalPermissionConfig(value: unknown): boolean {
  if (value === undefined) return true;
  if (!isRecord(value)) return false;
  return (
    typeof value.configurable === "boolean" &&
    optionalString(value.defaultValue) &&
    Array.isArray(value.modes) &&
    value.modes.every(isPermissionMode)
  );
}

function isPermissionMode(
  value: unknown
): value is AgentActivitySessionPermissionModeOption {
  return Boolean(
    isRecord(value) &&
    typeof value.id === "string" &&
    typeof value.label === "string" &&
    optionalString(value.description) &&
    isOneOf(value.semantic, PERMISSION_MODE_SEMANTICS)
  );
}

function optionalCapabilities(value: unknown): boolean {
  if (value === undefined || value === null) return true;
  if (!isRecord(value)) return false;
  return SESSION_CAPABILITIES.every(
    (capability) =>
      typeof (value as Record<keyof AgentActivitySessionCapabilities, unknown>)[
        capability
      ] === "boolean"
  );
}

function optionalLifecycleCapabilities(value: unknown): boolean {
  return Boolean(
    value === undefined ||
    (isRecord(value) &&
      typeof value.fork === "boolean" &&
      typeof value.forkThroughTurn === "boolean")
  );
}

function optionalForkLineage(value: unknown): boolean {
  return Boolean(
    value === undefined ||
    value === null ||
    (isRecord(value) &&
      Boolean(trimmedString(value.sourceAgentSessionId)) &&
      Boolean(trimmedString(value.sourceTurnId)) &&
      Boolean(trimmedString(value.targetTurnId)) &&
      Boolean(trimmedString(value.operationId)) &&
      isFiniteNumber(value.forkedAtUnixMs))
  );
}

function optionalUsage(value: unknown): boolean {
  if (value === undefined || value === null) return true;
  if (!isRecord(value)) return false;
  const contextWindow = value.contextWindow;
  return Boolean(
    (contextWindow === null ||
      (isRecord(contextWindow) &&
        isFiniteNumber(contextWindow.usedTokens) &&
        isFiniteNumber(contextWindow.totalTokens))) &&
    Array.isArray(value.quotas) &&
    value.quotas.every(
      (quota) =>
        isRecord(quota) &&
        typeof quota.quotaType === "string" &&
        isFiniteNumber(quota.percentRemaining) &&
        isNullableNumber(quota.resetsAtUnixMs)
    )
  );
}

function optionalCapabilityReferences(value: unknown): boolean {
  return Boolean(
    value === undefined ||
    (Array.isArray(value) &&
      value.every(
        (reference) =>
          isRecord(reference) &&
          reference.capability === "tutti" &&
          reference.source === "slash_command"
      ))
  );
}

function optionalCompletedCommand(value: unknown): boolean {
  return Boolean(
    value === undefined ||
    value === null ||
    (isRecord(value) &&
      isOneOf(value.kind, COMPLETED_COMMAND_KINDS) &&
      isOneOf(value.status, COMPLETED_COMMAND_STATUSES))
  );
}

function optionalTurnError(value: unknown): boolean {
  return Boolean(
    value === undefined ||
    value === null ||
    (isRecord(value) &&
      typeof value.message === "string" &&
      optionalString(value.code))
  );
}

function optionalNullableRecord(value: unknown): boolean {
  return value === undefined || value === null || isRecord(value);
}

function optionalNullableString(value: unknown): boolean {
  return value === undefined || isNullableString(value);
}

function optionalString(value: unknown): boolean {
  return value === undefined || typeof value === "string";
}

function isNullableString(value: unknown): value is string | null {
  return value === null || typeof value === "string";
}

function isString(value: unknown): value is string {
  return typeof value === "string";
}

function optionalNullableBoolean(value: unknown): boolean {
  return value === undefined || value === null || typeof value === "boolean";
}

function optionalBoolean(value: unknown): boolean {
  return value === undefined || typeof value === "boolean";
}

function optionalNullableNumber(value: unknown): boolean {
  return value === undefined || isNullableNumber(value);
}

function isNullableNumber(value: unknown): value is number | null {
  return value === null || isFiniteNumber(value);
}

function optionalFiniteNumber(value: unknown): boolean {
  return value === undefined || isFiniteNumber(value);
}

function isFiniteNumber(value: unknown): value is number {
  return typeof value === "number" && Number.isFinite(value);
}

function optionalNonNegativeInteger(value: unknown): boolean {
  return (
    value === undefined ||
    (Number.isSafeInteger(value) && (value as number) >= 0)
  );
}

function optionalOneOf<const Value extends string>(
  value: unknown,
  allowed: readonly Value[]
): boolean {
  return value === undefined || isOneOf(value, allowed);
}

function optionalNullableOneOf<const Value extends string>(
  value: unknown,
  allowed: readonly Value[]
): boolean {
  return value === undefined || value === null || isOneOf(value, allowed);
}

function isOneOf<const Value extends string>(
  value: unknown,
  allowed: readonly Value[]
): value is Value {
  return typeof value === "string" && allowed.includes(value as Value);
}

function trimmedString(value: unknown): string {
  return typeof value === "string" ? value.trim() : "";
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value && typeof value === "object" && !Array.isArray(value));
}

const SESSION_KINDS = ["root", "child"] as const;
const TURN_ORIGINS = [
  "user_prompt",
  "goal_arm",
  "goal_continuation",
  "provider_initiated",
  "legacy_unknown"
] as const;
const TURN_PHASES = [
  "submitted",
  "running",
  "waiting",
  "settling",
  "settled"
] as const;
const TURN_OUTCOMES = [
  "completed",
  "failed",
  "canceled",
  "interrupted"
] as const;
const PROVIDER_FORK_BINDING_STATES = [
  "bound",
  "recovery_required",
  "unavailable"
] as const;
const INTERACTION_KINDS = ["approval", "question", "plan"] as const;
const INTERACTION_STATUSES = ["pending", "answered", "superseded"] as const;
const PERMISSION_MODE_SEMANTICS = [
  "ask-before-write",
  "accept-edits",
  "locked-down",
  "auto",
  "full-access",
  "unconfigurable"
] as const;
const COMPLETED_COMMAND_KINDS = ["compact", "review", "undo", "goal"] as const;
const COMPLETED_COMMAND_STATUSES = ["completed", "failed", "canceled"] as const;
const SESSION_CAPABILITIES = [
  "imageInput",
  "modelImageInputRequired",
  "modelPlanBinding",
  "skills",
  "compact",
  "tokenUsage",
  "rateLimits",
  "planMode",
  "interrupt",
  "modelSwitch",
  "activeTurnGuidance",
  "browserUse",
  "computerUse",
  "goalPause",
  "planImplementation",
  "permissionModeChangeDuringTurn",
  "permissionModeChangeDeferred",
  "review",
  "resumeRunningTurn"
] as const satisfies readonly (keyof AgentActivitySessionCapabilities)[];
