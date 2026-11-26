"use client";

import type { ReactNode } from "react";
import { ChevronLeft, ChevronRight } from "@multica/ui/icons";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { useActorName } from "@multica/core/workspace/hooks";
import { ActorAvatar } from "../../../common/actor-avatar";
import { useT } from "../../../i18n";
import { pct } from "./contribution-utils";

/** Server-side bucket for agents this viewer may not see (handler/dashboard.go). */
export const RESTRICTED_AGENTS_ID = "__restricted_agents__";

export function Card({
  title,
  aside,
  children,
  flush = false,
}: {
  title: ReactNode;
  aside?: ReactNode;
  children: ReactNode;
  /** Flush cards put a divider under the header and let rows reach the edge. */
  flush?: boolean;
}) {
  return (
    <section className={`rounded-lg border bg-card ${flush ? "" : "p-4"}`}>
      <div
        className={`flex flex-wrap items-center justify-between gap-3 ${
          flush ? "border-b px-4 pt-4 pb-3" : "mb-3"
        }`}
      >
        <h4 className="text-body font-semibold">{title}</h4>
        {aside != null && <div className="flex flex-wrap items-center gap-3">{aside}</div>}
      </div>
      {children}
    </section>
  );
}

export function LegendDot({ color, label }: { color: string; label: string }) {
  return (
    <span className="inline-flex items-center gap-1.5 text-caption text-muted-foreground">
      <span aria-hidden className="size-2 shrink-0 rounded-full" style={{ background: color }} />
      {label}
    </span>
  );
}

/** A thin horizontal stack; segments carry their own share of `total`. */
export function StackBar({
  segments,
  className = "h-2",
}: {
  segments: readonly { value: number; color: string }[];
  className?: string;
}) {
  const total = segments.reduce((sum, s) => sum + s.value, 0);
  return (
    <div aria-hidden className={`flex overflow-hidden rounded-full bg-muted ${className}`}>
      {segments.map((s, i) =>
        s.value > 0 ? (
          <span key={i} className="h-full" style={{ width: `${pct(s.value, total)}%`, background: s.color }} />
        ) : null,
      )}
    </div>
  );
}

/** Avatar plus display name, covering the restricted-agents bucket. */
export function ActorLabel({ actorType, actorId }: { actorType: string; actorId: string }) {
  const { t } = useT("usage");
  const { getActorName } = useActorName();
  if (actorId === RESTRICTED_AGENTS_ID) {
    return (
      <span className="truncate text-body italic text-muted-foreground">
        {t(($) => $.contribution.board.restricted)}
      </span>
    );
  }
  return (
    <span className="flex min-w-0 items-center gap-2">
      <ActorAvatar actorType={actorType} actorId={actorId} size="xs" />
      <span className="truncate text-body font-medium">{getActorName(actorType, actorId)}</span>
    </span>
  );
}

export function Pager({
  offset,
  limit,
  total,
  onChange,
}: {
  offset: number;
  limit: number;
  total: number;
  onChange: (offset: number) => void;
}) {
  const { t } = useT("usage");
  if (total <= limit) return null;
  return (
    <div className="flex items-center justify-between gap-3 border-t px-4 py-2">
      <span className="text-caption tabular-nums text-muted-foreground">
        {t(($) => $.contribution.activity.range, {
          from: offset + 1,
          to: Math.min(offset + limit, total),
          total,
        })}
      </span>
      <div className="flex items-center gap-1">
        <IconButton
          label={t(($) => $.contribution.activity.prev)}
          disabled={offset === 0}
          onClick={() => onChange(Math.max(0, offset - limit))}
        >
          <ChevronLeft />
        </IconButton>
        <IconButton
          label={t(($) => $.contribution.activity.next)}
          disabled={offset + limit >= total}
          onClick={() => onChange(offset + limit)}
        >
          <ChevronRight />
        </IconButton>
      </div>
    </div>
  );
}

export function EmptyRow({ children }: { children: ReactNode }) {
  return <p className="px-4 py-8 text-center text-caption text-muted-foreground">{children}</p>;
}
