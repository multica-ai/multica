"use client";

import { useCallback, useMemo, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { copyText } from "@multica/ui/lib/clipboard";
import { cn } from "@multica/ui/lib/utils";
import { agentTaskSnapshotOptions, type AgentPresenceDetail } from "@multica/core/agents";
import { issueDetailOptions } from "@multica/core/issues/queries";
import type { AgentTask } from "@multica/core/types";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";

const EMPTY_TASKS: AgentTask[] = [];

export type TeamTone = "brand" | "warning" | "muted";

const TONE: Record<TeamTone, { pill: string; dot: string }> = {
  brand: { pill: "bg-brand/10 text-brand", dot: "bg-brand" },
  warning: { pill: "bg-warning/10 text-warning", dot: "bg-warning" },
  muted: { pill: "bg-muted text-muted-foreground", dot: "bg-muted-foreground/50" },
};

export function TeamPill({ tone, dot = true, children }: { tone: TeamTone; dot?: boolean; children: ReactNode }) {
  return (
    <span className={cn("inline-flex h-5 shrink-0 items-center gap-1.5 rounded-full px-2 text-caption font-medium", TONE[tone].pill)}>
      {dot && <span className={cn("size-1.5 rounded-full", TONE[tone].dot)} />}
      {children}
    </span>
  );
}

export function TeamDot({ tone }: { tone: TeamTone }) {
  return <span className={cn("size-1.5 shrink-0 rounded-full", TONE[tone].dot)} />;
}

/** What an agent is doing, in one word: the header pill and the panel's Status row. */
export function agentStatus(
  presence: AgentPresenceDetail | null | undefined,
  archived: boolean,
): { tone: TeamTone; key: "archived" | "offline" | "working" | "waiting" | "idle" } {
  if (archived) return { tone: "muted", key: "archived" };
  if (!presence) return { tone: "muted", key: "idle" };
  if (presence.workload === "working") return { tone: "brand", key: "working" };
  if (presence.workload === "queued") return { tone: "warning", key: "waiting" };
  if (presence.availability === "offline") return { tone: "muted", key: "offline" };
  return { tone: "muted", key: "idle" };
}

export function AgentStatusPill({
  presence,
  archived,
}: {
  presence: AgentPresenceDetail | null | undefined;
  archived: boolean;
}) {
  const { t } = useT("team");
  const status = agentStatus(presence, archived);
  return <TeamPill tone={status.tone}>{t(($) => $.status[status.key])}</TeamPill>;
}

/** Every agent's running tasks, oldest first, from the shared task snapshot. */
export function useRunningTasksByAgent(wsId: string): Map<string, AgentTask[]> {
  const { data: snapshot = EMPTY_TASKS } = useQuery({ ...agentTaskSnapshotOptions(wsId), enabled: !!wsId });
  return useMemo(() => {
    const out = new Map<string, AgentTask[]>();
    for (const task of snapshot) {
      if (task.status !== "running") continue;
      const list = out.get(task.agent_id) ?? [];
      list.push(task);
      out.set(task.agent_id, list);
    }
    for (const list of out.values()) {
      list.sort((a, b) => (a.started_at ?? "").localeCompare(b.started_at ?? ""));
    }
    return out;
  }, [snapshot]);
}

/** Queued tasks per agent, from the same snapshot. */
export function useQueuedTasksByAgent(wsId: string): Map<string, AgentTask[]> {
  const { data: snapshot = EMPTY_TASKS } = useQuery({ ...agentTaskSnapshotOptions(wsId), enabled: !!wsId });
  return useMemo(() => {
    const out = new Map<string, AgentTask[]>();
    for (const task of snapshot) {
      if (task.status !== "queued" && task.status !== "dispatched") continue;
      const list = out.get(task.agent_id) ?? [];
      list.push(task);
      out.set(task.agent_id, list);
    }
    return out;
  }, [snapshot]);
}

/** The issue behind a task, if it has one. */
export function useTaskIssue(wsId: string, task: AgentTask | null | undefined) {
  const issueId = task?.issue_id ?? "";
  const { data } = useQuery({ ...issueDetailOptions(wsId, issueId), enabled: !!issueId });
  return issueId ? data ?? null : null;
}

export function elapsedLabel(fromIso: string | null | undefined, now = Date.now()): string {
  if (!fromIso) return "";
  const minutes = Math.max(0, Math.floor((now - new Date(fromIso).getTime()) / 60000));
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h`;
  return `${Math.floor(hours / 24)}d`;
}

/** Right rail of a Team object. */
export function TeamPanel({ children }: { children: ReactNode }) {
  return (
    <aside className="hidden w-72 shrink-0 flex-col gap-5 overflow-y-auto border-l px-4 py-4 lg:flex">{children}</aside>
  );
}

export function TeamPanelSection({
  title,
  trailing,
  children,
}: {
  title: ReactNode;
  trailing?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className="flex flex-col gap-2">
      <h3 className="flex items-center text-caption font-medium text-muted-foreground">
        <span className="flex-1">{title}</span>
        {trailing != null && <span className="text-micro font-normal tabular-nums">{trailing}</span>}
      </h3>
      {children}
    </section>
  );
}

export function TeamPanelRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex min-h-6 items-center gap-2 text-caption">
      <dt className="w-20 shrink-0 text-muted-foreground">{label}</dt>
      <dd className="flex min-w-0 flex-1 items-center gap-1.5">{children}</dd>
    </div>
  );
}

/** Copy a Team link (as a shareable URL) or plain text, with a toast. */
export function useTeamCopy() {
  const { t } = useT("team");
  const navigation = useNavigation();
  const copyLink = useCallback(
    (href: string) => {
      void copyText(navigation.getShareableUrl(href)).then((ok) => {
        if (ok) toast.success(t(($) => $.detail.link_copied));
      });
    },
    [navigation, t],
  );
  const copyValue = useCallback(
    (value: string) => {
      void copyText(value).then((ok) => {
        if (ok) toast.success(t(($) => $.detail.copied));
      });
    },
    [t],
  );
  return { copyLink, copyValue };
}
