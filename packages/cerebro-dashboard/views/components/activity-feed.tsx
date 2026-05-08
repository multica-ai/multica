"use client";

import { Card, CardContent } from "@multica/ui/components/ui/card";
import { cn } from "@multica/ui/lib/utils";
import {
  CheckCircle2,
  Hash,
  MessageSquare,
  Pencil,
  UserPlus,
  Activity as ActivityIcon,
} from "lucide-react";
import type { ReactNode } from "react";
import type { DashboardOverview } from "../../core/api";
import type { ActorScope } from "../../core/types";

interface ActivityFeedProps {
  data: DashboardOverview | undefined;
  isLoading: boolean;
  scope: ActorScope;
}

// Renders the activity_log feed for the period. Filters client-side by
// actor scope so toggling between Alle / Members / Agents reshapes the list
// without a refetch.
export function ActivityFeed({ data, isLoading, scope }: ActivityFeedProps) {
  const entries = (data?.activity_feed ?? []).filter((e) => {
    if (scope === "members") return e.actor_type === "member";
    if (scope === "agents") return e.actor_type === "agent";
    return true;
  });

  return (
    <Card>
      <div className="flex items-center justify-between border-b px-3 py-2">
        <h3 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
          Recent activity
        </h3>
        {data && (
          <span className="text-[11px] text-muted-foreground">{data.range}</span>
        )}
      </div>
      <CardContent className="p-0">
        {isLoading ? (
          <div className="space-y-2 p-3">
            {[0, 1, 2, 3, 4, 5].map((i) => (
              <div key={i} className="h-6 animate-pulse rounded bg-muted" />
            ))}
          </div>
        ) : entries.length === 0 ? (
          <p className="p-6 text-center text-sm text-muted-foreground">
            Ingen aktivitet i perioden.
          </p>
        ) : (
          <ul className="divide-y">
            {entries.slice(0, 30).map((entry) => (
              <li key={entry.id} className="flex items-start gap-2 px-3 py-2 text-xs">
                <span className="mt-0.5 shrink-0 text-muted-foreground">
                  <ActionIcon action={entry.action} kind={entry.issue_kind} />
                </span>
                <div className="min-w-0 flex-1">
                  <p className="line-clamp-2">
                    <span
                      className={cn(
                        "font-medium",
                        entry.actor_type === "agent" && "text-purple-700 dark:text-purple-400",
                      )}
                    >
                      {prettyActor(entry.actor_type)}
                    </span>{" "}
                    <span className="text-muted-foreground">{prettyAction(entry.action)}</span>{" "}
                    {entry.issue_title && (
                      <span className="font-medium">{truncate(entry.issue_title, 60)}</span>
                    )}
                  </p>
                  <p className="text-[10px] text-muted-foreground">
                    {relativeTime(entry.created_at)}
                  </p>
                </div>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}

function ActionIcon({ action, kind }: { action: string; kind?: string }): ReactNode {
  if (action.startsWith("comment.")) {
    if (kind === "channel" || kind === "dm") return <Hash className="size-3.5" />;
    return <MessageSquare className="size-3.5" />;
  }
  if (action.includes("status") || action.includes("completed")) {
    return <CheckCircle2 className="size-3.5" />;
  }
  if (action.includes("created")) return <Pencil className="size-3.5" />;
  if (action.includes("assigned")) return <UserPlus className="size-3.5" />;
  return <ActivityIcon className="size-3.5" />;
}

function prettyAction(action: string): string {
  return action.replace(/[._]/g, " ");
}

function prettyActor(t: string): string {
  if (t === "member") return "Member";
  if (t === "agent") return "Agent";
  if (t === "system") return "System";
  return t;
}

function truncate(s: string, n: number): string {
  return s.length > n ? s.slice(0, n - 1) + "…" : s;
}

function relativeTime(iso: string): string {
  try {
    const d = new Date(iso);
    const seconds = Math.floor((Date.now() - d.getTime()) / 1000);
    if (seconds < 60) return `${seconds}s ago`;
    if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
    if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`;
    return `${Math.floor(seconds / 86400)}d ago`;
  } catch {
    return iso;
  }
}
