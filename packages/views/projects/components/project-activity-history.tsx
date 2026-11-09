"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight } from "@multica/ui/icons";
import { projectActivitiesOptions } from "@multica/core/projects";
import type { ProjectPriority, ProjectStatus, TimelineEntry } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { useActorName } from "@multica/core/workspace/hooks";
import { ActorAvatar } from "../../common/actor-avatar";
import { useT, useTimeAgo } from "../../i18n";
import { useProjectPriorityLabels, useProjectStatusLabels } from "./labels";

type ActivityT = ReturnType<typeof useT<"projects">>["t"];

function detail(entry: TimelineEntry, key: string): string | undefined {
  const value = entry.details?.[key];
  return typeof value === "string" && value ? value : undefined;
}

export function formatProjectActivity(
  entry: TimelineEntry,
  t: ActivityT,
  statusLabels: Record<ProjectStatus, string>,
  priorityLabels: Record<ProjectPriority, string>,
  getActorName: (type: string, id: string) => string,
): string {
  if (entry.action === "project_created") return t(($) => $.activity.created);
  if (entry.action === "project_description_updated") return t(($) => $.activity.description_updated);
  if (entry.action === "project_icon_changed") return t(($) => $.activity.icon_changed);

  const fields = {
    project_title_changed: t(($) => $.activity.fields.title),
    project_status_changed: t(($) => $.activity.fields.status),
    project_priority_changed: t(($) => $.activity.fields.priority),
    project_lead_changed: t(($) => $.activity.fields.lead),
    project_start_date_changed: t(($) => $.activity.fields.start_date),
    project_due_date_changed: t(($) => $.activity.fields.due_date),
  } as const;
  const field = entry.action ? fields[entry.action as keyof typeof fields] : undefined;
  if (!field) return entry.action ?? "";

  let from = detail(entry, "from");
  let to = detail(entry, "to");
  if (entry.action === "project_status_changed") {
    from = from && from in statusLabels ? statusLabels[from as ProjectStatus] : from;
    to = to && to in statusLabels ? statusLabels[to as ProjectStatus] : to;
  } else if (entry.action === "project_priority_changed") {
    from = from && from in priorityLabels ? priorityLabels[from as ProjectPriority] : from;
    to = to && to in priorityLabels ? priorityLabels[to as ProjectPriority] : to;
  } else if (entry.action === "project_lead_changed") {
    const fromType = detail(entry, "from_type");
    const fromID = detail(entry, "from_id");
    const toType = detail(entry, "to_type");
    const toID = detail(entry, "to_id");
    from = fromType && fromID ? getActorName(fromType, fromID) : undefined;
    to = toType && toID ? getActorName(toType, toID) : undefined;
  }

  if (from && to) return t(($) => $.activity.changed, { field, from, to });
  if (to) return t(($) => $.activity.set, { field, value: to });
  return t(($) => $.activity.removed, { field });
}

export function ProjectActivityHistory({ projectId }: { projectId: string }) {
  const { t } = useT("projects");
  const wsId = useWorkspaceId();
  const timeAgo = useTimeAgo();
  const { getActorName } = useActorName();
  const statusLabels = useProjectStatusLabels();
  const priorityLabels = useProjectPriorityLabels();
  const { data: entries = [] } = useQuery(projectActivitiesOptions(wsId, projectId));
  const [open, setOpen] = useState(false);

  if (entries.length === 0) return null;
  return (
    <div>
      <button
        type="button"
        aria-expanded={open}
        className={`mb-2 flex w-full items-center gap-1 rounded-md px-2 py-1 text-caption font-medium transition-colors hover:bg-accent/70 ${open ? "" : "text-muted-foreground hover:text-foreground"}`}
        onClick={() => setOpen((value) => !value)}
      >
        {t(($) => $.detail.section_activity_history)}
        <ChevronRight className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${open ? "rotate-90" : ""}`} />
        <span className="ml-auto font-mono tabular-nums text-muted-foreground">{entries.length}</span>
      </button>
      {open && (
        <div className="max-h-80 space-y-3 overflow-y-auto px-2">
          {entries.map((entry) => (
            <div key={entry.id} className="flex items-start gap-2 text-caption">
              <ActorAvatar
                actorType={entry.actor_type}
                actorId={entry.actor_id}
                name={entry.actor_name}
                avatarUrl={entry.actor_avatar_url}
                profileRequiresDirectoryEntry
                size="xs"
              />
              <div className="min-w-0 flex-1">
                <p className="break-words leading-4 text-muted-foreground">
                  <span className="font-medium text-foreground/90">
                    {entry.actor_name || getActorName(entry.actor_type, entry.actor_id)}
                  </span>{" "}
                  {formatProjectActivity(entry, t, statusLabels, priorityLabels, getActorName)}
                </p>
                <time dateTime={entry.created_at} className="text-[10px] leading-4 text-muted-foreground">
                  {timeAgo(entry.created_at)}
                </time>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
