import { useEffect, useSyncExternalStore } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ApiError } from "@multica/core/api/client";
import { agentTaskSnapshotOptions } from "@multica/core/agents/queries";
import { chatSessionsOptions } from "@multica/core/chat/queries";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { paths } from "@multica/core/paths";
import { defaultStorage } from "@multica/core/platform";
import { useIsGuest } from "@multica/core/permissions";
import type { Agent, AgentTask, Workspace } from "@multica/core/types";
import { agentListOptions } from "@multica/core/workspace/queries";
import { useT } from "@multica/views/i18n";
import type { IslandRun } from "../../../shared/run-island";

const key = "multica:desktop:run-island";
const changed = "multica:run-island-changed";

function subscribe(listener: () => void) {
  window.addEventListener("storage", listener);
  window.addEventListener(changed, listener);
  return () => {
    window.removeEventListener("storage", listener);
    window.removeEventListener(changed, listener);
  };
}

export function useRunIslandEnabled() {
  return useSyncExternalStore(
    subscribe,
    () => defaultStorage.getItem(key) === "true",
  );
}

export function setRunIslandEnabled(enabled: boolean) {
  defaultStorage.setItem(key, String(enabled));
  window.dispatchEvent(new Event(changed));
}

interface IslandLabels {
  running: string;
  queued: string;
  issue: (id: string) => string;
  chat: string;
  autopilot: string;
  agent: string;
}

function isActiveRun(task: AgentTask) {
  return (
    task.status === "running" ||
    task.status === "queued" ||
    task.status === "dispatched" ||
    task.status === "waiting_local_directory"
  );
}

export function islandRuns(
  slug: string,
  tasks: AgentTask[],
  agents: Agent[],
  labels: IslandLabels,
  chatTitles: ReadonlyMap<string, string>,
  cachedIssueTitle: (id: string) => string | undefined,
): IslandRun[] {
  const routes = paths.workspace(slug);
  const names = new Map(
    agents.filter((a) => !a.archived_at).map((a) => [a.id, a.name]),
  );
  const runs: IslandRun[] = [];
  for (const task of tasks) {
    const name = names.get(task.agent_id);
    if (!name) continue;
    const running = task.status === "running";
    if (!isActiveRun(task)) continue;
    let path = routes.agentDetail(task.agent_id);
    let detail =
      task.trigger_summary ||
      (task.autopilot_run_id ? labels.autopilot : labels.agent);
    if (task.chat_session_id) {
      // The workload snapshot also includes runs in other members' private chats.
      // Link only to sessions the signed-in member can see in the chat list.
      detail = labels.chat;
      if (chatTitles.has(task.chat_session_id)) {
        detail = chatTitles.get(task.chat_session_id) || labels.chat;
        path = routes.chatSession(task.chat_session_id);
      }
    } else if (task.issue_id) {
      path = routes.issueDetail(task.issue_id);
      detail =
        task.issue_title ||
        cachedIssueTitle(task.issue_id) ||
        task.trigger_summary ||
        task.issue_identifier ||
        labels.issue(task.issue_id);
    }
    runs.push({
      id: task.id,
      name,
      path,
      detail,
      status: running ? "running" : "queued",
      label: running ? labels.running : labels.queued,
    });
  }
  return runs.sort(
    (a, b) => Number(b.status === "running") - Number(a.status === "running"),
  );
}

export function RunIslandBridge({ workspace }: { workspace: Workspace }) {
  const enabled = useRunIslandEnabled();
  return enabled ? <WorkspaceIsland workspace={workspace} /> : null;
}

function WorkspaceIsland({ workspace }: { workspace: Workspace }) {
  const guest = useIsGuest(workspace.id);
  const { data: tasks, error: taskError } = useQuery({
    ...agentTaskSnapshotOptions(workspace.id),
    enabled: !guest,
  });
  const { data: agents, error: agentError } = useQuery({
    ...agentListOptions(workspace.id),
    enabled: !guest,
  });
  const { data: sessions, error: chatError } = useQuery({
    ...chatSessionsOptions(workspace.id),
    enabled:
      !guest &&
      !!tasks?.some((task) => task.chat_session_id && isActiveRun(task)),
  });
  const denied = (error: unknown) =>
    error instanceof ApiError && [401, 403, 404].includes(error.status);
  const workspaceDenied = denied(taskError) || denied(agentError);
  const visibleSessions = denied(chatError) ? undefined : sessions;
  const client = useQueryClient();
  const { t: agentCopy } = useT("agents");
  const { t, i18n } = useT("issues");
  const { t: layout } = useT("layout");
  const { t: settings } = useT("settings");
  const { name, slug } = workspace;

  useEffect(() => {
    if (guest || workspaceDenied || !tasks || !agents) {
      window.desktopAPI.reportRunIsland(null);
      return;
    }
    const runs = islandRuns(
      slug,
      tasks,
      agents,
      {
        running: t(($) => $.agent_activity.status_running),
        queued: t(($) => $.agent_activity.status_queued),
        issue: (id) =>
          agentCopy(($) => $.tab_body.activity.issue_short_fallback, {
            prefix: id.slice(0, 8),
          }),
        chat: layout(($) => $.nav.chat),
        autopilot: layout(($) => $.tab.autopilot),
        agent: layout(($) => $.tab.agent),
      },
      new Map(visibleSessions?.map((session) => [session.id, session.title])),
      (id) =>
        client.getQueryData(issueDetailOptions(workspace.id, id).queryKey)
          ?.title,
    );

    window.desktopAPI.reportRunIsland({
      slug,
      workspace: name,
      heading: settings(($) => $.desktop.island.title),
      summary: runs.length
        ? t(($) => $.agent_activity.hover_header_tasks, { count: runs.length })
        : t(($) => $.agent_activity.empty_hover),
      locale: i18n.resolvedLanguage || i18n.language,
      runs,
    });
  }, [
    name,
    slug,
    workspace.id,
    guest,
    tasks,
    agents,
    visibleSessions,
    workspaceDenied,
    client,
    t,
    agentCopy,
    layout,
    settings,
    i18n,
  ]);
  useEffect(() => () => window.desktopAPI.reportRunIsland(null), []);
  return null;
}
