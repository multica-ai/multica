import type { AgentTask, WorkingAgentSummary } from "@multica/core/types";

export interface ExecutionActorRef {
  type: "agent" | "squad";
  id: string;
}

export interface GroupedExecutionActor extends ExecutionActorRef {
  tasks: AgentTask[];
  runningCount: number;
  queuedCount: number;
  agentIds: string[];
}

export type KnownSquadLookup =
  | ReadonlySet<string>
  | ((squadId: string) => boolean)
  | null
  | undefined;

function resolvesToKnownSquad(lookup: KnownSquadLookup, squadId: string): boolean {
  if (!lookup) return true;
  if (typeof lookup === "function") return lookup(squadId);
  return lookup.has(squadId);
}

/**
 * Collapse active tasks that belong to a squad execution chain (leader or
 * delegated member carrying `squad_id`) into a single squad execution unit,
 * while keeping standalone agent tasks grouped by `agent_id`.
 *
 * When `knownSquadIds` is provided, tasks whose `squad_id` no longer resolves
 * to a workspace squad fall back to their underlying `agent_id` so the UI
 * never renders a dangling UUID or anonymous placeholder.
 */
export function groupExecutionTasksByActor(
  tasks: readonly AgentTask[],
  knownSquadIds?: KnownSquadLookup,
): GroupedExecutionActor[] {
  const groups = new Map<string, GroupedExecutionActor>();
  for (const task of tasks) {
    const squadId = task.squad_id?.trim() || "";
    const useSquad =
      squadId.length > 0 && resolvesToKnownSquad(knownSquadIds, squadId);
    const type: "agent" | "squad" = useSquad ? "squad" : "agent";
    const id = useSquad ? squadId : task.agent_id;
    if (!id) continue;
    const key = `${type}:${id}`;
    let entry = groups.get(key);
    if (!entry) {
      entry = {
        type,
        id,
        tasks: [],
        runningCount: 0,
        queuedCount: 0,
        agentIds: [],
      };
      groups.set(key, entry);
    }
    entry.tasks.push(task);
    if (task.status === "running") entry.runningCount += 1;
    else entry.queuedCount += 1;
    if (task.agent_id && !entry.agentIds.includes(task.agent_id)) {
      entry.agentIds.push(task.agent_id);
    }
  }
  return Array.from(groups.values());
}

export interface WorkingExecutionUnit extends ExecutionActorRef {
  runningTaskCount: number;
  memberAgentIds: string[];
}

/**
 * Derive squad-collapsed working execution units for header chips
 * (`WorkspaceAgentWorkingChip` / `SubIssuesAgentWorkingChip`).
 *
 * The server-filtered `agents` list remains the authority for which agents are
 * working inside the current surface's scope and active filters. We consult
 * `snapshot` (running tasks) only to map each working agent's active tasks
 * onto their owning `squad_id` when present; if a working agent has no matching
 * squad-attributed task in the snapshot (or snapshot hasn't loaded), it stays
 * a standalone agent unit with its exact projected `running_task_count`.
 */
export function collapseWorkingAgentsWithSnapshot(
  agents: readonly WorkingAgentSummary[],
  snapshot: readonly AgentTask[],
  knownSquadIds?: KnownSquadLookup,
): WorkingExecutionUnit[] {
  if (agents.length === 0) return [];

  const runningByAgent = new Map<string, AgentTask[]>();
  for (const task of snapshot) {
    if (task.status !== "running") continue;
    const list = runningByAgent.get(task.agent_id) ?? [];
    list.push(task);
    runningByAgent.set(task.agent_id, list);
  }

  const units = new Map<string, WorkingExecutionUnit>();
  const addUnit = (
    type: "agent" | "squad",
    id: string,
    taskCount: number,
    memberAgentId?: string,
  ) => {
    const key = `${type}:${id}`;
    let entry = units.get(key);
    if (!entry) {
      entry = { type, id, runningTaskCount: 0, memberAgentIds: [] };
      units.set(key, entry);
    }
    entry.runningTaskCount += taskCount;
    if (memberAgentId && !entry.memberAgentIds.includes(memberAgentId)) {
      entry.memberAgentIds.push(memberAgentId);
    }
  };

  for (const agent of agents) {
    const runningTasks = runningByAgent.get(agent.id) ?? [];
    if (runningTasks.length === 0) {
      addUnit("agent", agent.id, agent.running_task_count);
      continue;
    }

    // Count how many of this agent's running tasks belong to each squad vs standalone.
    const squadCounts = new Map<string, number>();
    let standaloneCount = 0;
    for (const task of runningTasks) {
      const squadId = task.squad_id?.trim() || "";
      const useSquad =
        squadId.length > 0 && resolvesToKnownSquad(knownSquadIds, squadId);
      if (useSquad) {
        squadCounts.set(squadId, (squadCounts.get(squadId) ?? 0) + 1);
      } else {
        standaloneCount += 1;
      }
    }

    if (squadCounts.size === 0) {
      addUnit("agent", agent.id, agent.running_task_count);
      continue;
    }

    // Cap attributed counts to the surface-projected `running_task_count` so a
    // narrowed view (e.g. a single project) never inflates counts with tasks
    // running outside that view.
    let remaining = agent.running_task_count;
    for (const [squadId, count] of squadCounts) {
      if (remaining <= 0) break;
      const take = Math.min(count, remaining);
      addUnit("squad", squadId, take, agent.id);
      remaining -= take;
    }
    if (remaining > 0 && standaloneCount > 0) {
      addUnit("agent", agent.id, remaining);
    } else if (remaining > 0) {
      // Any leftover projected count stays attributed to the first squad rather
      // than dropping tasks from the surface total.
      const firstSquad = squadCounts.keys().next().value;
      if (firstSquad) addUnit("squad", firstSquad, remaining, agent.id);
    }
  }

  return Array.from(units.values());
}
