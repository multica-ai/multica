"use client";

import { useQuery } from "@tanstack/react-query";
import {
  agentRunCounts30dOptions,
  agentTaskSnapshotOptions,
} from "@multica/core/agents/queries";
import { useAuthStore } from "@multica/core/auth";
import { memberListOptions } from "@multica/core/workspace/queries";
import { useNavigation } from "@multica/views/navigation";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { cn } from "@multica/ui/lib/utils";
import { Bot, ListTodo, DollarSign, Activity } from "lucide-react";
import type { ReactNode } from "react";
import { workspaceOpenIssuesOptions } from "../../core/queries";

interface KpiCardsProps {
  wsId: string;
  workspaceSlug: string;
}

// Resolves whether the signed-in user is admin/owner of the workspace.
// Inlined here (instead of using `useCurrentMember`) because that hook is
// internal to `@multica/core/permissions` and not part of its public exports;
// adding an export would require a CEREBRO-PATCH in the upstream zone.
function useIsWorkspaceAdmin(wsId: string): boolean {
  const userId = useAuthStore((s) => s.user?.id ?? null);
  const { data: members } = useQuery(memberListOptions(wsId));
  const member = members?.find((m) => m.user_id === userId) ?? null;
  return member?.role === "owner" || member?.role === "admin";
}

export function KpiCards({ wsId, workspaceSlug }: KpiCardsProps) {
  const isAdmin = useIsWorkspaceAdmin(wsId);

  return (
    <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
      <ActiveAgentsCard wsId={wsId} slug={workspaceSlug} />
      <TasksRunningCard wsId={wsId} slug={workspaceSlug} />
      {isAdmin ? (
        <SpendCard wsId={wsId} slug={workspaceSlug} />
      ) : (
        <SpendCardHidden />
      )}
      <IssuesOpenCard wsId={wsId} slug={workspaceSlug} />
    </div>
  );
}

function ActiveAgentsCard({ wsId, slug }: { wsId: string; slug: string }) {
  const { data: counts, isLoading } = useQuery(agentRunCounts30dOptions(wsId));
  const activeCount = counts?.filter((c) => c.run_count > 0).length ?? 0;
  return (
    <KpiCard
      icon={<Bot className="size-3.5" />}
      label="Active agents"
      value={isLoading ? null : activeCount}
      hint="med kørsler i de sidste 30 dage"
      href={`/${slug}/agents`}
    />
  );
}

function TasksRunningCard({ wsId, slug }: { wsId: string; slug: string }) {
  const { data: snapshot, isLoading } = useQuery(agentTaskSnapshotOptions(wsId));
  const running = snapshot?.filter(
    (t) => t.status === "running" || t.status === "dispatched",
  ).length;
  return (
    <KpiCard
      icon={<Activity className="size-3.5" />}
      label="Tasks running"
      value={isLoading ? null : (running ?? 0)}
      hint="lige nu på tværs af agents"
      href={`/${slug}/agents`}
    />
  );
}

function SpendCard({ slug }: { wsId: string; slug: string }) {
  // TODO(JEH-703): backend endpoint for workspace-wide spend in selected
  // time-range. The /api/workspaces/{id}/runtimes per-runtime usage exists
  // but summing client-side is N requests; we add a dedicated endpoint in
  // the cerebro dashboard handler instead.
  return (
    <KpiCard
      icon={<DollarSign className="size-3.5" />}
      label="Spend"
      value={null}
      hint="kommer i Fase 2"
      href={`/${slug}/settings`}
    />
  );
}

function SpendCardHidden() {
  return (
    <Card className="opacity-50">
      <CardContent className="flex h-full items-center justify-center p-3 text-xs text-muted-foreground">
        Spend (admin only)
      </CardContent>
    </Card>
  );
}

function IssuesOpenCard({ wsId, slug }: { wsId: string; slug: string }) {
  const { data, isLoading } = useQuery(workspaceOpenIssuesOptions(wsId));
  const total = data?.total ?? data?.issues?.length ?? 0;
  return (
    <KpiCard
      icon={<ListTodo className="size-3.5" />}
      label="Issues open"
      value={isLoading ? null : total}
      hint="todo, in_progress og backlog"
      href={`/${slug}/issues`}
    />
  );
}

interface KpiCardProps {
  icon: ReactNode;
  label: string;
  value: number | null;
  hint: string;
  href: string;
}

function KpiCard({ icon, label, value, hint, href }: KpiCardProps) {
  const { push } = useNavigation();
  return (
    <button
      type="button"
      onClick={() => push(href)}
      className={cn(
        "group block w-full rounded-lg border bg-card text-left text-card-foreground",
        "transition-colors hover:border-foreground/30 hover:bg-accent/30",
      )}
    >
      <div className="flex items-center justify-between gap-2 p-3 pb-1">
        <span className="flex items-center gap-1.5 text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
          {icon}
          {label}
        </span>
      </div>
      <div className="px-3 pb-2 text-2xl font-semibold tabular-nums">
        {value === null ? <span className="text-muted-foreground">—</span> : value}
      </div>
      <div className="px-3 pb-3 text-xs text-muted-foreground">{hint}</div>
    </button>
  );
}
