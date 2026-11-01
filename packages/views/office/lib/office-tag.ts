import type { Issue, Squad } from "@multica/core/types";

/**
 * What each agent shows over its head: its issue in progress, else its first
 * open issue. Done issues and issues in hidden statuses are skipped. An issue
 * assigned to a squad counts for the squad's leader.
 */
export function focusIssues(
  issues: readonly Issue[],
  visibleStatuses: readonly string[],
  squads: readonly Pick<Squad, "id" | "leader_id">[],
): Map<string, Issue> {
  const leaderOf = new Map(squads.map((squad) => [squad.id, squad.leader_id]));
  const shown = new Set(visibleStatuses);
  const map = new Map<string, Issue>();
  for (const issue of issues) {
    if (!issue.assignee_id) continue;
    const agentId =
      issue.assignee_type === "agent"
        ? issue.assignee_id
        : issue.assignee_type === "squad"
          ? leaderOf.get(issue.assignee_id)
          : undefined;
    if (!agentId) continue;
    if (issue.status === "done" || !shown.has(issue.status)) continue;
    const current = map.get(agentId);
    if (!current || (issue.status === "in_progress" && current.status !== "in_progress")) {
      map.set(agentId, issue);
    }
  }
  return map;
}

/** The leaders of squads that have a proposal waiting for the boss. */
export function leadersNeedingYou(
  squads: readonly Pick<Squad, "id" | "leader_id">[],
  pendingSquadIds: ReadonlySet<string>,
): Set<string> {
  const leaders = new Set<string>();
  for (const squad of squads) {
    if (pendingSquadIds.has(squad.id) && squad.leader_id) leaders.add(squad.leader_id);
  }
  return leaders;
}

export type OfficeTag = { kind: "needs_you" } | { kind: "status"; status: string } | { kind: "idle" };

/** The tag over an agent, first match wins: Needs you, the focus issue's status, else Idle. */
export function officeTagOf(
  agentId: string,
  needsYou: ReadonlySet<string>,
  focus: ReadonlyMap<string, Pick<Issue, "status">>,
): OfficeTag {
  if (needsYou.has(agentId)) return { kind: "needs_you" };
  const issue = focus.get(agentId);
  return issue ? { kind: "status", status: issue.status } : { kind: "idle" };
}
