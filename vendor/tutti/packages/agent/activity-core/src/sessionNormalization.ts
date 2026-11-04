import type { AgentActivitySession } from "./types.ts";

type RequiredSessionInputFields =
  | "activeTurnId"
  | "agentSessionId"
  | "cwd"
  | "latestTurnInteractions"
  | "pendingInteractions"
  | "provider"
  | "title"
  | "workspaceId";

export type AgentActivitySessionInput = Pick<
  AgentActivitySession,
  RequiredSessionInputFields
> &
  Partial<Omit<AgentActivitySession, RequiredSessionInputFields>>;

export function normalizeAgentActivitySession<const Provider extends string>(
  source: AgentActivitySessionInput & {
    provider: Provider;
    providerSessionId: string;
  }
): AgentActivitySession & { provider: Provider; providerSessionId: string };
export function normalizeAgentActivitySession(
  source: AgentActivitySessionInput
): AgentActivitySession;
export function normalizeAgentActivitySession(
  source: AgentActivitySessionInput
): AgentActivitySession {
  const createdAtUnixMs = source.createdAtUnixMs ?? source.startedAtUnixMs ?? 0;
  const updatedAtUnixMs =
    source.updatedAtUnixMs ?? source.lastEventUnixMs ?? createdAtUnixMs;
  return {
    ...source,
    kind: source.kind ?? "root",
    rootAgentSessionId: source.rootAgentSessionId ?? null,
    rootTurnId: source.rootTurnId ?? null,
    parentAgentSessionId: source.parentAgentSessionId ?? null,
    parentTurnId: source.parentTurnId ?? null,
    parentToolCallId: source.parentToolCallId ?? null,
    agentTargetId: source.agentTargetId ?? null,
    providerSessionId: source.providerSessionId ?? null,
    isolation: source.isolation
      ? {
          mode: source.isolation.mode,
          ...(source.isolation.worktreeId?.trim()
            ? { worktreeId: source.isolation.worktreeId.trim() }
            : {}),
          worktreePath: source.isolation.worktreePath.trim(),
          branch: source.isolation.branch.trim(),
          baseCommit: source.isolation.baseCommit.trim()
        }
      : null,
    activeTurnId: source.activeTurnId,
    activeTurn: source.activeTurn ?? null,
    latestTurn: source.latestTurn ?? source.activeTurn ?? null,
    latestTurnInteractions: source.latestTurnInteractions,
    pendingInteractions: source.pendingInteractions,
    settings: source.settings ?? {},
    permissionConfig: source.permissionConfig ?? {
      configurable: false,
      modes: []
    },
    capabilities: source.capabilities ?? null,
    lifecycleCapabilities: source.lifecycleCapabilities
      ? {
          fork: source.lifecycleCapabilities.fork === true,
          forkThroughTurn: source.lifecycleCapabilities.forkThroughTurn === true
        }
      : {
          fork: false,
          forkThroughTurn: false
        },
    ...(source.lifecycleCapabilitiesProjected === undefined
      ? {}
      : {
          lifecycleCapabilitiesProjected:
            source.lifecycleCapabilitiesProjected === true
        }),
    forkedFrom: source.forkedFrom
      ? {
          sourceAgentSessionId: source.forkedFrom.sourceAgentSessionId.trim(),
          sourceTurnId: source.forkedFrom.sourceTurnId.trim(),
          targetTurnId: source.forkedFrom.targetTurnId.trim(),
          operationId: source.forkedFrom.operationId.trim(),
          forkedAtUnixMs: source.forkedFrom.forkedAtUnixMs
        }
      : null,
    usage: source.usage ?? null,
    imported: source.imported ?? false,
    visible: source.visible ?? true,
    resumable: source.resumable ?? false,
    messageVersion: source.messageVersion ?? 0,
    lastEventUnixMs: source.lastEventUnixMs ?? updatedAtUnixMs,
    startedAtUnixMs: source.startedAtUnixMs ?? createdAtUnixMs,
    endedAtUnixMs: source.endedAtUnixMs ?? null,
    pinnedAtUnixMs: source.pinnedAtUnixMs ?? null,
    createdAtUnixMs,
    updatedAtUnixMs
  };
}
