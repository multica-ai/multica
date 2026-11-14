import { useNavigation } from "@react-navigation/native";
import { useQueries, useQuery } from "@tanstack/react-query";
import { chatSessionsOptions, sortChatSessions } from "@multica/core/chat/queries";
import { issueDetailOptions } from "@multica/core/issues/queries";
import type { ChatSession } from "@multica/core/types";
import { useActorResolver } from "../../model/people";
import { runPhase, useActiveTasks } from "../../model/tasks";
import { useWorkspace } from "../../model/workspace-context";
import { markdownPreview } from "../../markdown/parse";
import { relativeTime, useStrings } from "../../strings";
import { useDetail } from "../../shell/detail";
import { AgentsView, type AgentListRow } from "./AgentsView";

export function AgentsScreen() {
  const navigation = useNavigation();
  const detail = useDetail();
  const strings = useStrings();
  const { workspace } = useWorkspace();
  const resolveActor = useActorResolver();
  const active = useActiveTasks(workspace.id);
  const sessions = useQuery({
    ...chatSessionsOptions(workspace.id),
    select: (list: ChatSession[]) => sortChatSessions(list.filter((session) => session.status !== "archived")),
  });

  const issueIds = [...new Set(active.tasks.map((task) => task.issue_id).filter(Boolean))];
  const issues = useQueries({ queries: issueIds.map((id) => issueDetailOptions(workspace.id, id)) });
  const issueById = new Map(issues.flatMap((query) => (query.data ? [[query.data.id, query.data] as const] : [])));

  const running = active.tasks.flatMap((task): AgentListRow[] => {
    const agent = resolveActor("agent", task.agent_id).name;
    const state = { text: runPhase(task.status) === "running" ? strings.agents.working : strings.agents.queued, tone: "warning" as const };
    const time = relativeTime(task.started_at ?? task.created_at, strings);
    if (task.issue_id) {
      const issue = issueById.get(task.issue_id);
      return [{ id: task.id, title: issue?.title ?? task.issue_title ?? "…", state, meta: [agent, issue?.identifier ?? task.issue_identifier].filter(Boolean).join(" · "), time, unread: false, target: { kind: "issue", issueId: task.issue_id } }];
    }
    const session = task.chat_session_id ? sessions.data?.find((s) => s.id === task.chat_session_id) : undefined;
    if (!session) return [];
    return [{ id: task.id, title: session.title || strings.agents.newChat, state, meta: agent, time, unread: false, target: { kind: "chat", sessionId: session.id } }];
  });

  const runningChats = new Set(running.flatMap((row) => (row.target.kind === "chat" ? [row.target.sessionId] : [])));
  const conversations: AgentListRow[] = (sessions.data ?? [])
    .filter((session) => !runningChats.has(session.id))
    .map((session) => {
      const agent = resolveActor("agent", session.agent_id).name;
      const last = session.last_message ? markdownPreview(session.last_message.content) : "";
      return {
        id: session.id,
        title: session.title || strings.agents.newChat,
        meta: last ? `${agent} · ${last}` : agent,
        time: relativeTime(session.last_message?.created_at ?? session.updated_at, strings),
        unread: session.has_unread,
        target: { kind: "chat", sessionId: session.id },
      };
    });

  return (
    <AgentsView
      state={sessions.data ? "ready" : sessions.isError ? "error" : "loading"}
      running={running}
      conversations={conversations}
      refreshing={sessions.isRefetching}
      onRefresh={() => {
        void sessions.refetch();
        active.refetch();
      }}
      onRetry={() => {
        void sessions.refetch();
        active.refetch();
      }}
      selected={detail.selected}
      onOpen={(row) => detail.open(row.target)}
      onNewChat={() => navigation.navigate("NewChat")}
    />
  );
}
