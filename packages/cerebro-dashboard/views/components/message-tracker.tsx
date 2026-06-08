"use client";

import { Card, CardContent } from "@multica/ui/components/ui/card";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { Bot, MessageSquare, Send } from "lucide-react";
import { useNavigation } from "@multica/views/navigation";
import { useDashboardStore } from "../../core/store";
import type { DashboardOverview, TopActor } from "../../core/api";

interface MessageTrackerProps {
  data: DashboardOverview | undefined;
  isLoading: boolean;
  workspaceSlug: string;
}

export function MessageTracker({ data, isLoading, workspaceSlug }: MessageTrackerProps) {
  const senders = data?.top_message_senders ?? [];
  const recipients = data?.top_message_recipients ?? [];
  const setActor = useDashboardStore((s) => s.setActor);
  const { push } = useNavigation();

  return (
    <div className="grid gap-3 lg:grid-cols-2">
      <ActorList
        title="Mest aktive afsendere"
        icon={<Send className="size-3.5" />}
        list={senders}
        isLoading={isLoading}
        emptyText="Ingen beskeder sendt i perioden."
        onRowClick={(actor) => setActor(actor.id, actor.name || "Unknown")}
      />
      <ActorList
        title="Mest beskedte agenter"
        icon={<Bot className="size-3.5" />}
        list={recipients}
        isLoading={isLoading}
        emptyText="Ingen chat-beskeder modtaget i perioden."
        onRowClick={(actor) => push(`/${workspaceSlug}/agents/${actor.id}`)}
      />
    </div>
  );
}

function ActorList({
  title,
  icon,
  list,
  isLoading,
  emptyText,
  onRowClick,
}: {
  title: string;
  icon: React.ReactNode;
  list: TopActor[];
  isLoading: boolean;
  emptyText: string;
  onRowClick: (actor: TopActor) => void;
}) {
  const max = list.reduce((m, a) => Math.max(m, a.count), 0);

  return (
    <Card>
      <div className="flex items-center gap-1.5 border-b px-3 py-2">
        <MessageSquare className="size-3.5 text-muted-foreground" />
        <h3 className="flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wide text-muted-foreground">
          {icon}
          {title}
        </h3>
      </div>
      <CardContent className="space-y-2 p-3">
        {isLoading ? (
          [0, 1, 2].map((i) => (
            <div key={i} className="h-6 animate-pulse rounded bg-muted" />
          ))
        ) : list.length === 0 ? (
          <p className="py-4 text-center text-xs text-muted-foreground">{emptyText}</p>
        ) : (
          list.map((actor) => (
            <Row key={actor.id} actor={actor} maxCount={max} onClick={() => onRowClick(actor)} />
          ))
        )}
      </CardContent>
    </Card>
  );
}

function Row({
  actor,
  maxCount,
  onClick,
}: {
  actor: TopActor;
  maxCount: number;
  onClick: () => void;
}) {
  const pct = maxCount === 0 ? 0 : Math.round((actor.count / maxCount) * 100);

  return (
    <button
      type="button"
      onClick={onClick}
      className="flex w-full items-center gap-2 rounded-md p-1 text-left transition-colors hover:bg-accent/50"
    >
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
            {actor.count.toLocaleString("da-DK")} beskeder
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
    </button>
  );
}
