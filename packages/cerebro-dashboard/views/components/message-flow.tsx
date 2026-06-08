"use client";

import { Card, CardContent } from "@multica/ui/components/ui/card";
import { ArrowRight } from "lucide-react";
import type { DashboardOverview } from "../../core/api";

interface MessageFlowProps {
  data: DashboardOverview | undefined;
  isLoading: boolean;
}

export function MessageFlow({ data, isLoading }: MessageFlowProps) {
  const flow = data?.message_flow ?? [];
  const max = flow.reduce((m, e) => Math.max(m, e.count), 0);

  return (
    <Card>
      <div className="flex items-center gap-1.5 border-b px-3 py-2">
        <h3 className="flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wide text-muted-foreground">
          <ArrowRight className="size-3.5" />
          Hvem skriver til hvem
        </h3>
      </div>
      <CardContent className="p-3">
        {isLoading ? (
          <div className="space-y-2">
            {[0, 1, 2, 3].map((i) => (
              <div key={i} className="h-7 animate-pulse rounded bg-muted" />
            ))}
          </div>
        ) : flow.length === 0 ? (
          <p className="py-6 text-center text-xs text-muted-foreground">
            Ingen chat-beskeder i perioden.
          </p>
        ) : (
          <div className="space-y-1.5">
            {flow.map((entry, i) => (
              <FlowRow key={i} entry={entry} maxCount={max} />
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function FlowRow({
  entry,
  maxCount,
}: {
  entry: { sender_name: string; recipient_name: string; count: number };
  maxCount: number;
}) {
  const pct = maxCount === 0 ? 0 : Math.round((entry.count / maxCount) * 100);
  return (
    <div className="flex items-center gap-2 rounded-md px-1 py-0.5">
      <div className="flex min-w-0 flex-1 items-center gap-1.5">
        <span className="shrink-0 max-w-[110px] truncate text-xs font-medium">
          {entry.sender_name}
        </span>
        <ArrowRight className="size-3 shrink-0 text-muted-foreground" />
        <span className="shrink-0 max-w-[110px] truncate text-xs text-muted-foreground">
          {entry.recipient_name}
        </span>
        <div className="ml-1 min-w-0 flex-1">
          <div className="h-1 w-full rounded bg-muted">
            <div
              className="h-full rounded bg-primary/60"
              style={{ width: `${pct}%` }}
              aria-hidden
            />
          </div>
        </div>
      </div>
      <span className="shrink-0 text-[11px] tabular-nums text-muted-foreground">
        {entry.count.toLocaleString("da-DK")}
      </span>
    </div>
  );
}
