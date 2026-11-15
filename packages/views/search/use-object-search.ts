"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { autopilotListOptions } from "@multica/core/autopilots/queries";
import { chatSessionsOptions } from "@multica/core/chat/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { skillListOptions, workspaceListOptions } from "@multica/core/workspace/queries";
import type {
  Autopilot,
  ChatSession,
  MemberWithUser,
  SkillSummary,
  Workspace,
} from "@multica/core/types";
import { scoreRow, scoreText } from "./matching";
import { useActorDirectory, type AgentEntry, type SquadEntry } from "./use-actor-directory";

export interface Scored<T> {
  item: T;
  score: number;
}

export interface ObjectHits {
  agents: Scored<AgentEntry>[];
  squads: Scored<SquadEntry>[];
  members: Scored<MemberWithUser>[];
  autopilots: Scored<Autopilot>[];
  chats: Scored<ChatSession>[];
  skills: Scored<SkillSummary>[];
  workspaces: Scored<Workspace>[];
}

const EMPTY_AUTOPILOTS: Autopilot[] = [];
const EMPTY_CHATS: ChatSession[] = [];
const EMPTY_SKILLS: SkillSummary[] = [];
const EMPTY_WORKSPACES: Workspace[] = [];

// Hidden aliases for the Workspaces group, prefix-matched like page keywords:
// typing "workspace" lists every workspace the user can switch to.
const WORKSPACE_KEYWORDS = ["switch", "workspace"];
const ALL_MEMBERS_WORDS = ["members", "people", "users", "team"];

function rank<T>(items: readonly T[], score: (item: T) => number, limit: number): Scored<T>[] {
  return items
    .map((item) => ({ item, score: score(item) }))
    .filter((entry) => entry.score > 0)
    .sort((a, b) => b.score - a.score)
    .slice(0, limit);
}

function memberScore(member: MemberWithUser, query: string): number {
  const byName = scoreText(member.name, query);
  if (byName > 0) return byName;
  if (member.email.toLowerCase().includes(query)) return 40;
  if (query.length >= 3 && member.role.startsWith(query)) return 25;
  if (query.length >= 3 && ALL_MEMBERS_WORDS.some((word) => word.startsWith(query))) return 20;
  return 0;
}

/**
 * Client-side search over the workspace lists the app already caches. Lists
 * the palette does not otherwise need (autopilots, chats, skills) load only
 * once the user types.
 */
export function useObjectSearch(query: string, open: boolean): ObjectHits & { directory: ReturnType<typeof useActorDirectory> } {
  const wsId = useWorkspaceId();
  const directory = useActorDirectory(open);
  const typing = open && query.length > 0;
  const { data: autopilots = EMPTY_AUTOPILOTS } = useQuery({ ...autopilotListOptions(wsId), enabled: typing });
  const { data: chats = EMPTY_CHATS } = useQuery({ ...chatSessionsOptions(wsId), enabled: typing });
  const { data: skills = EMPTY_SKILLS } = useQuery({ ...skillListOptions(wsId), enabled: typing });
  // Account-level cache entry the sidebar's workspace switcher already
  // holds, so the Workspaces group adds no request of its own.
  const { data: workspaces = EMPTY_WORKSPACES } = useQuery(workspaceListOptions());

  const hits = useMemo<ObjectHits>(() => {
    if (!query) {
      return { agents: [], squads: [], members: [], autopilots: [], chats: [], skills: [], workspaces: [] };
    }
    return {
      agents: rank(directory.agents, (e) => scoreText(e.agent.name, query), 8),
      squads: rank(directory.squads, (e) => scoreText(e.squad.name, query), 5),
      members: rank(directory.members, (m) => memberScore(m, query), 10),
      autopilots: rank(
        autopilots.filter((a) => a.status !== "archived"),
        (a) => scoreText(a.title, query),
        5,
      ),
      chats: rank(
        chats.filter((c) => c.status !== "archived"),
        (c) => scoreText(c.title, query),
        5,
      ),
      skills: rank(skills, (s) => scoreText(s.name, query), 5),
      // The current workspace is never a switch target, so a single-workspace
      // user never sees the group. Ties keep the server's created_at order,
      // the same order as the sidebar switcher.
      workspaces: rank(
        workspaces.filter((ws) => ws.id !== wsId),
        (ws) => Math.max(scoreRow(ws.name, WORKSPACE_KEYWORDS, query), ws.slug.toLowerCase().includes(query) ? 40 : 0),
        50,
      ),
    };
  }, [autopilots, chats, directory, query, skills, workspaces, wsId]);

  return { ...hits, directory };
}
