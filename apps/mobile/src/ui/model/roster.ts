import { useCallback, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { canAssignAgentToIssue } from "@multica/core/permissions";
import type { Agent, MemberRole, MemberWithUser } from "@multica/core/types";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import { agentListOptions, memberListOptions, squadListOptions } from "@multica/core/workspace/queries";
import type { AvatarActor } from "../components/Avatar";
import { useActorResolver } from "./people";

export interface RosterEntry {
  /** `member:<user id>` or `agent:<agent id>`, the value a picker hands back. */
  value: string;
  type: "member" | "agent";
  id: string;
  actor: AvatarActor;
  detail?: string;
}

export const rosterValue = (type: string, id: string) => `${type}:${id}`;

export function parseRosterValue(value: string): { type: "member" | "agent"; id: string } | null {
  const [type, id] = value.split(":");
  return (type === "member" || type === "agent") && id ? { type, id } : null;
}

/** Names an autopilot's assignee, which is an agent or a squad. */
export function useAssigneeResolver(wsId: string): (type: string, id: string) => AvatarActor {
  const resolveActor = useActorResolver();
  const squads = useQuery(squadListOptions(wsId));
  return useCallback(
    (type: string, id: string) => {
      if (type !== "squad") return resolveActor(type, id);
      const squad = squads.data?.find((candidate) => candidate.id === id);
      return { kind: "agent", name: squad?.name ?? "—", avatarUrl: resolvePublicFileUrl(squad?.avatar_url ?? null) };
    },
    [resolveActor, squads.data],
  );
}

const EMPTY_MEMBERS: MemberWithUser[] = [];
const EMPTY_AGENTS: Agent[] = [];

/** The people and agents of the workspace, ready for pickers, plus who the viewer is. */
export function useRoster(wsId: string) {
  const userId = useAuthStore((state) => state.user?.id ?? null);
  const membersQuery = useQuery(memberListOptions(wsId));
  const agentsQuery = useQuery(agentListOptions(wsId));
  const members = membersQuery.data ?? EMPTY_MEMBERS;
  const agents = agentsQuery.data ?? EMPTY_AGENTS;
  const role: MemberRole | null = members.find((member) => member.user_id === userId)?.role ?? null;

  const entries = useMemo(() => {
    const people: RosterEntry[] = members.map((member) => ({
      value: rosterValue("member", member.user_id),
      type: "member",
      id: member.user_id,
      actor: { kind: "member", name: member.name, avatarUrl: resolvePublicFileUrl(member.avatar_url) },
      detail: member.email,
    }));
    const bots: RosterEntry[] = agents
      .filter((agent) => !agent.archived_at)
      .map((agent) => ({
        value: rosterValue("agent", agent.id),
        type: "agent",
        id: agent.id,
        actor: { kind: "agent", name: agent.name, avatarUrl: resolvePublicFileUrl(agent.avatar_url) },
        detail: agent.description || undefined,
      }));
    return { people, bots };
  }, [members, agents]);

  return {
    userId,
    role,
    isAdmin: role === "owner" || role === "admin",
    agents,
    members: entries.people,
    activeAgents: entries.bots,
    ready: membersQuery.isSuccess && agentsQuery.isSuccess,
  };
}

export interface AssigneeEntry {
  /** `member:<id>`, `agent:<id>` or `squad:<id>`. */
  value: string;
  type: "member" | "agent" | "squad";
  id: string;
  actor: AvatarActor;
  detail?: string;
}

export function parseAssigneeValue(value: string): { type: "member" | "agent" | "squad"; id: string } | null {
  const [type, id] = value.split(":");
  return (type === "member" || type === "agent" || type === "squad") && id ? { type, id } : null;
}

/**
 * Who the viewer may hand an issue to: every member, the agents they may invoke that
 * have a runtime, and the squads whose leader is one of those agents.
 */
export function useAssignees(wsId: string) {
  const roster = useRoster(wsId);
  const squadsQuery = useQuery(squadListOptions(wsId));
  const { userId, role, agents, members, activeAgents } = roster;
  const squads = squadsQuery.data;
  return useMemo(() => {
    const runnable = new Set(
      agents
        .filter((agent) => !agent.archived_at && (agent.runtime_id ?? "").trim() && canAssignAgentToIssue(agent, { userId, role }).allowed)
        .map((agent) => agent.id),
    );
    const teams: AssigneeEntry[] = (squads ?? [])
      .filter((squad) => !squad.archived_at && runnable.has(squad.leader_id))
      .map((squad) => ({
        value: rosterValue("squad", squad.id),
        type: "squad",
        id: squad.id,
        actor: { kind: "agent", name: squad.name, avatarUrl: resolvePublicFileUrl(squad.avatar_url) },
        detail: squad.description || undefined,
      }));
    return {
      userId,
      ready: roster.ready,
      members: members as AssigneeEntry[],
      agents: activeAgents.filter((entry) => runnable.has(entry.id)) as AssigneeEntry[],
      squads: teams,
    };
  }, [agents, squads, userId, role, members, activeAgents, roster.ready]);
}
