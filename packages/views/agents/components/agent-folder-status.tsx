"use client";

import type { AgentPresenceDetail } from "@multica/core/agents";
import { ListGridCell } from "@multica/ui/components/ui/list-grid";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { useT } from "../../i18n";
import type { AgentListRow } from "./agents-page";

export function aggregateAgentFolderPresence(rows: readonly AgentListRow[]) {
  const active: { row: AgentListRow; presence: AgentPresenceDetail }[] = [];
  let workingCount = 0;
  let queuedCount = 0;
  let onlineCount = 0;
  for (const row of rows) {
    const presence = row.presence;
    if (!presence) continue;
    if (presence.availability === "online") onlineCount++;
    if (presence.workload === "working") workingCount++;
    else if (presence.workload === "queued") queuedCount++;
    if (presence.workload !== "idle") active.push({ row, presence });
  }
  return { workingCount, queuedCount, onlineCount, active };
}

export function AgentFolderStatusCell({ rows }: { rows: readonly AgentListRow[] }) {
  const { t } = useT("agent-folders");
  if (rows.length === 0) {
    return <ListGridCell><span className="text-caption text-faint-foreground">—</span></ListGridCell>;
  }

  const { workingCount, queuedCount, onlineCount, active } = aggregateAgentFolderPresence(rows);
  const working = workingCount > 0;
  const queued = !working && queuedCount > 0;
  const online = !working && !queued && onlineCount > 0;
  const dotClass = working
    ? "bg-info animate-pulse motion-reduce:animate-none"
    : queued
      ? "bg-warning"
      : online
        ? "bg-success"
        : "bg-muted-foreground/40";
  const textClass = working
    ? "text-info"
    : queued
      ? "text-warning"
      : online
        ? "text-success"
        : "text-muted-foreground";
  const primaryCount = working ? workingCount : queued ? queuedCount : onlineCount;
  const primaryLabel = working
    ? t(($) => $.tree.status_working, { count: primaryCount })
    : queued
      ? t(($) => $.tree.status_queued, { count: primaryCount })
      : t(($) => $.tree.status_online, { count: primaryCount });

  const cell = (
    <ListGridCell className="gap-1.5">
      <span aria-hidden="true" className={`size-1.5 shrink-0 rounded-full ${dotClass}`} />
      <span className={`truncate text-caption ${textClass}`}>
        {working || queued || online
          ? primaryLabel
          : t(($) => $.tree.status_offline)}
        {working && onlineCount > 0 && (
          <span className="text-muted-foreground">
            {" · "}
            {t(($) => $.tree.status_online, { count: onlineCount })}
          </span>
        )}
      </span>
    </ListGridCell>
  );

  if (active.length === 0) return cell;
  return (
    <Tooltip>
      <TooltipTrigger render={cell} />
      <TooltipContent side="top" className="max-w-80">
        {active.map(({ row, presence }) => (
          <div key={row.agent.id} className="flex items-center justify-between gap-4">
            <span className="min-w-0 truncate">{row.agent.name}</span>
            <span className="shrink-0 tabular-nums">
              {presence.runningCount > 0 && t(($) => $.tree.status_running_tasks, { count: presence.runningCount })}
              {presence.runningCount > 0 && presence.queuedCount > 0 && " · "}
              {presence.queuedCount > 0 && t(($) => $.tree.status_queued_tasks, { count: presence.queuedCount })}
            </span>
          </div>
        ))}
      </TooltipContent>
    </Tooltip>
  );
}
