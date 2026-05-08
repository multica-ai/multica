"use client";

import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { memberListOptions } from "@multica/core/workspace/queries";
import { useNavigation } from "@multica/views/navigation";
import { Card } from "@multica/ui/components/ui/card";
import { cn } from "@multica/ui/lib/utils";
import {
  Bot,
  Hash,
  ListTodo,
  CheckCircle2,
  MessageSquare,
  TrendingUp,
  Users,
  DollarSign,
} from "lucide-react";
import type { ReactNode } from "react";
import type { DashboardOverview, Kpi } from "../../core/api";

interface KpiCardsProps {
  data: DashboardOverview | undefined;
  isLoading: boolean;
  wsId: string;
  workspaceSlug: string;
}

function useIsWorkspaceAdmin(wsId: string): boolean {
  const userId = useAuthStore((s) => s.user?.id ?? null);
  const { data: members } = useQuery(memberListOptions(wsId));
  const member = members?.find((m) => m.user_id === userId) ?? null;
  return member?.role === "owner" || member?.role === "admin";
}

export function KpiCards({ data, isLoading, wsId, workspaceSlug }: KpiCardsProps) {
  const isAdmin = useIsWorkspaceAdmin(wsId);

  const cards: KpiCardSpec[] = [
    {
      icon: <ListTodo className="size-3.5" />,
      label: "Issues created",
      kpi: data?.issues_created,
      href: `/${workspaceSlug}/issues`,
      formatter: numberFmt,
    },
    {
      icon: <CheckCircle2 className="size-3.5" />,
      label: "Issues completed",
      kpi: data?.issues_completed,
      href: `/${workspaceSlug}/issues`,
      formatter: numberFmt,
    },
    {
      icon: <MessageSquare className="size-3.5" />,
      label: "Chat messages",
      kpi: data?.chat_messages,
      href: `/${workspaceSlug}/inbox`,
      formatter: numberFmt,
    },
    {
      icon: <Hash className="size-3.5" />,
      label: "Channel messages",
      kpi: data?.channel_messages,
      href: `/${workspaceSlug}/channels`,
      formatter: numberFmt,
    },
    {
      icon: <TrendingUp className="size-3.5" />,
      label: "Tasks completed",
      kpi: data?.tasks_completed,
      href: `/${workspaceSlug}/agents`,
      formatter: numberFmt,
    },
    {
      icon: <Bot className="size-3.5" />,
      label: "Active agents",
      kpi: data?.agents_active,
      href: `/${workspaceSlug}/agents`,
      formatter: numberFmt,
    },
    {
      icon: <Users className="size-3.5" />,
      label: "Active members",
      kpi: data?.members_active,
      href: `/${workspaceSlug}/members`,
      formatter: numberFmt,
    },
  ];

  if (isAdmin) {
    cards.push({
      icon: <DollarSign className="size-3.5" />,
      label: "Spend",
      kpi: data?.spend_cents,
      href: `/${workspaceSlug}/settings`,
      formatter: usdFmt,
      adminOnly: true,
    });
  }

  return (
    <div className="grid grid-cols-2 gap-3 md:grid-cols-3 lg:grid-cols-4">
      {cards.map((c) => (
        <KpiCard key={c.label} {...c} isLoading={isLoading} />
      ))}
    </div>
  );
}

interface KpiCardSpec {
  icon: ReactNode;
  label: string;
  kpi: Kpi | undefined;
  href: string;
  formatter: (value: number) => string;
  adminOnly?: boolean;
}

function KpiCard({
  icon,
  label,
  kpi,
  href,
  formatter,
  isLoading,
  adminOnly,
}: KpiCardSpec & { isLoading: boolean }) {
  const { push } = useNavigation();
  const value = kpi?.value ?? null;
  const prior = kpi?.prior ?? null;
  const delta = value !== null && prior !== null ? value - prior : null;

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
        {adminOnly && (
          <span className="rounded-sm border px-1 text-[9px] uppercase tracking-wide text-muted-foreground">
            admin
          </span>
        )}
      </div>
      <div className="flex items-baseline gap-2 px-3 pb-1">
        <span className="text-2xl font-semibold tabular-nums">
          {isLoading || value === null ? (
            <span className="text-muted-foreground">—</span>
          ) : (
            formatter(value)
          )}
        </span>
        {delta !== null && delta !== 0 && (
          <DeltaBadge delta={delta} />
        )}
      </div>
      <div className="px-3 pb-3 text-xs text-muted-foreground">
        {prior === null ? (
          <>&nbsp;</>
        ) : (
          <>vs {formatter(prior)} forrige periode</>
        )}
      </div>
    </button>
  );
}

function DeltaBadge({ delta }: { delta: number }) {
  const positive = delta > 0;
  return (
    <span
      className={cn(
        "inline-flex items-center rounded-sm px-1 text-[10px] font-medium tabular-nums",
        positive
          ? "bg-emerald-500/10 text-emerald-700 dark:text-emerald-400"
          : "bg-amber-500/10 text-amber-700 dark:text-amber-400",
      )}
    >
      {positive ? "+" : ""}
      {delta}
      {positive ? " ↑" : " ↓"}
    </span>
  );
}

function numberFmt(n: number): string {
  return n.toLocaleString("da-DK");
}

function usdFmt(cents: number): string {
  if (cents === 0) return "$0.00";
  const usd = cents / 100;
  return `$${usd.toLocaleString("en-US", { maximumFractionDigits: 2, minimumFractionDigits: 2 })}`;
}

// Card is no longer used directly here; retain import to keep the surface
// stable for future custom card content.
void Card;
