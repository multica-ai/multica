"use client";

import { Card, CardContent } from "@multica/ui/components/ui/card";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { Bot, Users } from "lucide-react";
import type { DashboardOverview, TopActor } from "../../core/api";

interface TopActorsProps {
  data: DashboardOverview | undefined;
  isLoading: boolean;
  kind: "agents" | "members";
}

// Top contributors in the period — agents ranked by tasks completed, members
// by activity_log events. Counts come straight from the overview payload.
export function TopActors({ data, isLoading, kind }: TopActorsProps) {
  const list = kind === "agents" ? data?.top_agents ?? [] : data?.top_members ?? [];
  const max = list.reduce((m, a) => Math.max(m, a.count), 0);
  const title = kind === "agents" ? "Top agents" : "Top members";
  const Icon = kind === "agents" ? Bot : Users;

  return (
    <Card>
      <div className="flex items-center justify-between border-b px-3 py-2">
        <h3 className="flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wide text-muted-foreground">
          <Icon className="size-3.5" />
          {title}
        </h3>
      </div>
      <CardContent className="space-y-2 p-3">
        {isLoading ? (
          [0, 1, 2].map((i) => <div key={i} className="h-6 animate-pulse rounded bg-muted" />)
        ) : list.length === 0 ? (
          <p className="py-4 text-center text-xs text-muted-foreground">
            Ingen aktivitet i perioden.
          </p>
        ) : (
          list.map((a) => <Row key={a.id} actor={a} maxCount={max} />)
        )}
      </CardContent>
    </Card>
  );
}

function Row({ actor, maxCount }: { actor: TopActor; maxCount: number }) {
  const pct = maxCount === 0 ? 0 : Math.round((actor.count / maxCount) * 100);
  return (
    <div className="flex items-center gap-2">
      <ActorAvatar
        name={actor.name || "?"}
        initials={(actor.name || "?").charAt(0).toUpperCase()}
        size={18}
      />
      <div className="min-w-0 flex-1">
        <div className="flex items-baseline justify-between gap-2">
          <span className="truncate text-xs font-medium">
            {actor.name || "Unknown"}
          </span>
          <span className="text-[11px] tabular-nums text-muted-foreground">
            {actor.count}
          </span>
        </div>
        <div className="mt-0.5 h-1 w-full rounded bg-muted">
          <div
            className="h-full rounded bg-primary"
            style={{ width: `${pct}%` }}
            aria-hidden
          />
        </div>
      </div>
    </div>
  );
}
