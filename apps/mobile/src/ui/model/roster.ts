import { useCallback, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
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
