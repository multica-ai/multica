import type { PendingChatTaskItem } from "@multica/core/types";
import {
normalizeAgentActivitySession,
type AgentActivityComposerOptions,
type AgentActivityDurableMessage,
type AgentActivitySession,
type AgentActivitySessionCapabilities,
type AgentActivityTurn
} from "@tutti-os/agent-activity-core";
import { localProjectSectionKey } from "../local-projects";
import type {
AgentGuiExecutionTarget,
AgentGuiSession,
AgentGuiSessionDetail
} from "../protocol";

export const MULTICA_GUI_CAPABILITIES: AgentActivitySessionCapabilities = {
  imageInput: true,
  modelImageInputRequired: false,
  modelPlanBinding: false,
  skills: false,
  compact: false,
  tokenUsage: false,
  rateLimits: false,
  planMode: false,
  interrupt: true,
  modelSwitch: true,
  activeTurnGuidance: false,
  browserUse: false,
  computerUse: false,
  goalPause: false,
  planImplementation: false,
  permissionModeChangeDuringTurn: false,
  permissionModeChangeDeferred: false,
  review: false,
  resumeRunningTurn: false
};

export function composerOptions(
  agent: AgentGuiExecutionTarget
): AgentActivityComposerOptions {
  return {
    provider: agent.provider,
    capabilities: { ...MULTICA_GUI_CAPABILITIES, interrupt: agent.canInvoke },
    models: agent.model
      ? [{ value: agent.model, label: agent.model, requested: true }]
      : [],
    reasoningEfforts: [],
    speeds: [],
    skills: [],
    commands: [],
    capabilityCatalog: [],
    modelConfigurable: false,
    reasoningConfigurable: false,
    speedConfigurable: false,
    effectiveModel: agent.model || null,
    effectiveSettings: { model: agent.model || null },
    permissionConfig: { configurable: false, modes: [] },
    behavior: {
      collapseModelOptionsToLatest: false,
      modelOptionsAuthoritative: true,
      refreshModelOptionsAfterSettings: false,
      prewarmDraftSession: false,
      planModeExclusiveWithPermissionMode: false
    },
    slashCommandPolicy: {
      fallbackCommands: [],
      commandCatalogAuthoritative: true,
      commandEffects: []
    },
    loadedAtUnixMs: Date.now()
  };
}

export function readSessionMetadata(
  session: AgentGuiSession,
  agent: AgentGuiExecutionTarget | undefined,
  turns: AgentActivityTurn[] = [],
  activeTurnId: string | null = null
): AgentActivitySession {
  const guiId = session.id;
  const latestTurn =
    [...turns].sort((a, b) => b.updatedAtUnixMs - a.updatedAtUnixMs)[0] ?? null;
  const projectPath = session.localDirectory?.projectPath ?? null;
  return normalizeAgentActivitySession({
    workspaceId: session.workspace_id,
    agentSessionId: guiId,
    provider: agent?.provider ?? "unknown",
    agentTargetId: session.agent_id,
    railSectionKey: projectPath
      ? localProjectSectionKey(projectPath)
      : "conversations",
    providerSessionId: null,
    userId: session.creator_id,
    title: session.title,
    cwd: projectPath ?? "",
    noProject: !projectPath,
    activeTurnId,
    activeTurn: turns.find((turn) => turn.turnId === activeTurnId) ?? null,
    latestTurn,
    latestTurnInteractions: [],
    pendingInteractions: [],
    settings: { model: agent?.model || null },
    capabilities: {
      ...MULTICA_GUI_CAPABILITIES,
      interrupt: Boolean(agent?.canInvoke)
    },
    lifecycleCapabilities: {
      fork: false,
      forkThroughTurn: agent?.canFork === true
    },
    lifecycleCapabilitiesProjected: true,
    resumable: session.status === "active",
    visible: true,
    messageVersion: 0,
    createdAtUnixMs: millis(session.created_at),
    updatedAtUnixMs: millis(session.updated_at),
    // The server only exposes the pinned flag and modification time.
    pinnedAtUnixMs: session.pinned ? millis(session.updated_at) : null
  });
}

/** The aggregate identifies a live run, but does not expose its start time. */
export function readPendingSessionMetadata(
  session: AgentGuiSession,
  agent: AgentGuiExecutionTarget | undefined,
  pending: PendingChatTaskItem | undefined
): AgentActivitySession {
  if (!pending) return readSessionMetadata(session, agent);
  const turn: AgentActivityTurn = {
    agentSessionId: session.id,
    turnId: pending.task_id,
    origin: "legacy_unknown",
    outcome: null,
    phase:
      pending.status === "running"
        ? "running"
        : pending.status === "waiting_local_directory"
          ? "waiting"
          : "submitted",
    // Zero is the source contract's unknown time; never use session metadata
    // as if it were a real task start. Detail hydration supplies actual times.
    startedAtUnixMs: 0,
    updatedAtUnixMs: 0
  };
  return readSessionMetadata(session, agent, [turn], pending.task_id);
}

/** A local conversation the source no longer holds: its metadata, without turns or messages. */
export function readLocalSessionMetadata(
  detail: AgentGuiSessionDetail,
  agent: AgentGuiExecutionTarget | undefined
): {
  session: AgentActivitySession;
  turns: AgentActivityTurn[];
  messages: AgentActivityDurableMessage[];
} {
  const session = readSessionMetadata(detail.session, agent);
  if (detail.forkedFrom) session.forkedFrom = forkedFrom(detail.forkedFrom);
  return { session, turns: [], messages: [] };
}

function forkedFrom(
  fork: NonNullable<AgentGuiSessionDetail["forkedFrom"]>
): NonNullable<AgentActivitySession["forkedFrom"]> {
  return {
    sourceAgentSessionId: fork.sessionId,
    sourceTurnId: fork.turnId,
    targetTurnId: fork.targetTurnId,
    operationId: fork.operationId,
    forkedAtUnixMs: millis(fork.createdAt)
  };
}

function millis(value: string): number {
  const result = Date.parse(value);
  return Number.isFinite(result) ? result : 0;
}
