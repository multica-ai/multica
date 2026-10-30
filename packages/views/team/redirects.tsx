import { useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { chatSessionsWithThreadsOptions } from "@multica/core/chat/queries";
import { useFeatureEnabled } from "@multica/core/config";
import { TEAM_PAGE_FLAG } from "@multica/core/feature-flags";
import { pickPrimaryChatSession, isThreadChatSession } from "@multica/core/chat/primary-session";
import type { ChatSession } from "@multica/core/types";
import { useNavigation } from "../navigation";
import { teamHref } from "./selection";

/**
 * With the Team page on, the old per-object pages (an agent, a squad, a
 * member, a chat with an agent) open the same object in Team instead, so
 * links, bookmarks and saved desktop tabs keep working.
 *
 * Each mapping returns the Team URL, or `undefined` while it cannot tell yet.
 */

/**
 * `/chat?session=` opens that conversation, `/chat?agent=` the agent; a bare
 * `/chat` opens Team itself (the Chat page syncs its open session into the
 * URL, so letting it stay would only defer the jump to the next click).
 */
export function teamHrefForChat(
  base: string,
  params: URLSearchParams,
  sessions: readonly ChatSession[] | undefined,
): string | undefined {
  const sessionId = params.get("session");
  const agentId = params.get("agent");
  if (!sessionId) return agentId ? teamHref(base, { kind: "agent", id: agentId }) : base;
  if (!sessions) return undefined;
  const session = sessions.find((s) => s.id === sessionId);
  if (!session) return agentId ? teamHref(base, { kind: "agent", id: agentId }) : base;
  if (isThreadChatSession(session)) {
    return teamHref(base, { kind: "agent", id: session.agent_id, threadId: session.id });
  }
  const primary = pickPrimaryChatSession(sessions, session.agent_id);
  return teamHref(base, {
    kind: "agent",
    id: session.agent_id,
    sessionId: primary?.id === session.id ? null : session.id,
  });
}

// The agent page's `?view=` sections live under Team's Setup tab, except the
// two Team shows as tabs of their own.
export function teamHrefForAgent(base: string, agentId: string, params: URLSearchParams): string {
  const view = params.get("view");
  if (!view || view === "overview") return teamHref(base, { kind: "agent", id: agentId });
  if (view === "work") return teamHref(base, { kind: "agent", id: agentId, tab: "work" });
  return `${teamHref(base, { kind: "agent", id: agentId, tab: "setup" })}&view=${encodeURIComponent(view)}`;
}

export function teamHrefForSquad(base: string, squadId: string): string {
  return teamHref(base, { kind: "squad", id: squadId });
}

export function teamHrefForMember(base: string, userId: string): string {
  return teamHref(base, { kind: "member", id: userId, tab: "work" });
}

export function useTeamPageEnabled(): boolean {
  return useFeatureEnabled(TEAM_PAGE_FLAG, false);
}

/** Rendered in place of an old page while the Team page is on. */
export function TeamRedirect({ to }: { to: string | undefined }) {
  const { replace } = useNavigation();
  useEffect(() => {
    if (to) replace(to);
  }, [to, replace]);
  return null;
}

export function ChatTeamRedirect() {
  const { searchParams } = useNavigation();
  const wsPaths = useWorkspacePaths();
  const wsId = useWorkspaceId();
  const { data: sessions } = useQuery({
    ...chatSessionsWithThreadsOptions(wsId),
    enabled: !!searchParams.get("session"),
  });
  return <TeamRedirect to={teamHrefForChat(wsPaths.team(), searchParams, sessions)} />;
}
