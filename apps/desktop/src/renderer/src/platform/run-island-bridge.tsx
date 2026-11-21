import { useEffect, useSyncExternalStore } from "react";
import { useQuery } from "@tanstack/react-query";
import { agentTaskSnapshotOptions } from "@multica/core/agents/queries";
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
  issue: string;
  chat: string;
  autopilot: string;
  agent: string;
}

export function islandRuns(
  slug: string,
  tasks: AgentTask[],
  agents: Agent[],
  labels: IslandLabels,
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
    if (
      !running &&
      task.status !== "queued" &&
      task.status !== "dispatched" &&
      task.status !== "waiting_local_directory"
    )
      continue;
    const kind = task.chat_session_id
      ? labels.chat
      : task.issue_id
        ? labels.issue
        : task.autopilot_run_id
          ? labels.autopilot
          : labels.agent;
    const path = task.chat_session_id
      ? routes.chatSession(task.chat_session_id)
      : task.issue_id
        ? routes.issueDetail(task.issue_id)
        : routes.agentDetail(task.agent_id);
    runs.push({
      id: task.id,
      name,
      path,
      detail: task.issue_title || task.trigger_summary || kind,
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
  const { data: tasks } = useQuery({
    ...agentTaskSnapshotOptions(workspace.id),
    enabled: !guest,
  });
  const { data: agents } = useQuery({
    ...agentListOptions(workspace.id),
    enabled: !guest,
  });
  const { t, i18n } = useT("issues");
  const { t: layout } = useT("layout");
  const { t: settings } = useT("settings");
  const { name, slug } = workspace;

  useEffect(() => {
    if (guest || !tasks || !agents) {
      window.desktopAPI.reportRunIsland(null);
      return;
    }
    const runs = islandRuns(slug, tasks, agents, {
      running: t(($) => $.agent_activity.status_running),
      queued: t(($) => $.agent_activity.status_queued),
      issue: layout(($) => $.tab.issue),
      chat: layout(($) => $.nav.chat),
      autopilot: layout(($) => $.tab.autopilot),
      agent: layout(($) => $.tab.agent),
    });
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
  }, [name, slug, guest, tasks, agents, t, layout, settings, i18n]);
  useEffect(() => () => window.desktopAPI.reportRunIsland(null), []);
  return null;
}
