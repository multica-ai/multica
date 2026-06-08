"use client";

import { Card, CardContent } from "@multica/ui/components/ui/card";
import { ArrowRight } from "lucide-react";
import { useNavigation } from "@multica/views/navigation";
import type { DashboardOverview, MessageFlowEntry } from "../../core/api";

interface MessageFlowProps {
  data: DashboardOverview | undefined;
  isLoading: boolean;
  workspaceSlug: string;
}

export function MessageFlow({ data, isLoading, workspaceSlug }: MessageFlowProps) {
  const flow = data?.message_flow ?? [];
  const max = flow.reduce((m, e) => Math.max(m, e.count), 0);
  const { push } = useNavigation();

  return (
    <Card>
      <div className="flex items-center gap-1.5 border-b px-3 py-2">
        <h3 className="flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wide text-muted-foreground">
          <ArrowRight className="size-3.5" />
          Who writes to whom
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
            No chat messages in this period.
          </p>
        ) : (
          <div className="space-y-1.5">
            {flow.map((entry, i) => (
              <FlowRow
                key={i}
                entry={entry}
                maxCount={max}
                onClick={() => push(`/${workspaceSlug}/agents/${entry.recipient_id}`)}
              />
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
  onClick,
}: {
  entry: MessageFlowEntry;
  maxCount: number;
  onClick: () => void;
}) {
  const pct = maxCount === 0 ? 0 : Math.round((entry.count / maxCount) * 100);
  return (
    <button
      type="button"
      onClick={onClick}
      className="flex w-full items-center gap-2 rounded-md px-1 py-0.5 text-left transition-colors hover:bg-accent/50"
    >
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
        {entry.count.toLocaleString()}
      </span>
    </button>
  );
}
