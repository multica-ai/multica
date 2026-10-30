"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Activity, CheckCheck, FileCode, FileText, Plus } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { cn } from "@multica/ui/lib/utils";
import {
  docActivityOptions,
  docTreeOptions,
  folderAncestry,
  groupDocActivity,
  localDayKey,
  useMarkDocRead,
  type DocActivityRow,
} from "@multica/core/docs";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useActorName } from "@multica/core/workspace/hooks";
import type { DocFolder } from "@multica/core/types";
import { ActorAvatar } from "../../common/actor-avatar";
import { PageHeader } from "../../layout/page-header";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";
import { useDocCommands } from "./use-doc-commands";

type Filter = "all" | "unread";

function useDayLabel() {
  const { t } = useT("docs");
  const today = localDayKey(new Date().toISOString());
  const yesterday = localDayKey(new Date(Date.now() - 86_400_000).toISOString());
  return (day: string) => {
    if (day === today) return t(($) => $.feed.today);
    if (day === yesterday) return t(($) => $.feed.yesterday);
    const [y, m, d] = day.split("-").map(Number);
    return new Date(y!, (m ?? 1) - 1, d ?? 1).toLocaleDateString(undefined, { month: "short", day: "numeric", weekday: "short" });
  };
}

function FeedRow({ row, folders }: { row: DocActivityRow; folders: readonly DocFolder[] }) {
  const { t } = useT("docs");
  const p = useWorkspacePaths();
  const { getActorName } = useActorName();
  const names = row.authors.map((a) => getActorName(a.type, a.id)).join(t(($) => $.feed.name_separator));
  const later = row.edits - (row.created ? 1 : 0);
  const verb = row.created
    ? later > 0
      ? t(($) => $.feed.created_and_edited, { count: later })
      : t(($) => $.feed.created)
    : t(($) => $.feed.edited, { count: row.edits });
  const path = folderAncestry(folders, row.folderId).map((f) => f.title).join(" / ");
  const time = new Date(row.latestAt).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
  const message = row.messages[0];

  return (
    <li>
      <AppLink
        href={p.docDetail(row.docId)}
        className="group/feed relative flex items-start gap-3 rounded-lg px-3 py-2.5 outline-none transition-colors hover:bg-surface-hover focus-visible:outline-2 focus-visible:outline-ring"
      >
        {row.unread && (
          <span aria-label={t(($) => $.tree.unread)} className="absolute top-4 left-0.5 size-1.5 rounded-full bg-brand" />
        )}
        <span className="mt-0.5 text-muted-foreground [&_svg]:size-4">
          {row.format === "html" ? <FileCode /> : <FileText />}
        </span>
        <span className="flex min-w-0 flex-1 flex-col gap-1">
          <span className="flex min-w-0 items-center gap-2">
            <span className={cn("truncate text-body", row.unread ? "font-semibold" : "font-medium")}>
              {row.title || t(($) => $.untitled)}
            </span>
            {row.format === "html" && (
              <span className="shrink-0 rounded-xs bg-muted px-1 text-micro font-medium text-muted-foreground">HTML</span>
            )}
            {path && <span className="truncate text-caption text-faint-foreground">{path}</span>}
          </span>
          <span className="flex min-w-0 items-center gap-1.5 text-caption text-muted-foreground">
            <span className="flex shrink-0 -space-x-1">
              {row.authors.slice(0, 4).map((a) => (
                <ActorAvatar key={`${a.type}:${a.id}`} actorType={a.type} actorId={a.id} size="xs" className="ring-1 ring-background" />
              ))}
            </span>
            <span className="shrink-0">
              {names} {verb}
            </span>
            {message && (
              <>
                <span className="text-faint-foreground">·</span>
                <span className="truncate">{message.text}</span>
              </>
            )}
          </span>
        </span>
        <span className="mt-0.5 shrink-0 text-caption tabular-nums text-faint-foreground">{time}</span>
      </AppLink>
    </li>
  );
}

/** The knowledge base home: what changed recently, one row per doc per day. */
export function DocsHomePage() {
  const { t } = useT("docs");
  const wsId = useWorkspaceId();
  const [filter, setFilter] = useState<Filter>("all");
  const { data: items, isPending } = useQuery(docActivityOptions(wsId));
  const { data: tree } = useQuery(docTreeOptions(wsId));
  const markRead = useMarkDocRead();
  const { newDoc } = useDocCommands();
  const dayLabel = useDayLabel();

  const all = useMemo(() => groupDocActivity(items ?? [], false), [items]);
  const days = useMemo(() => (filter === "unread" ? groupDocActivity(items ?? [], true) : all), [all, filter, items]);
  const unreadDocs = useMemo(() => {
    const ids = new Set<string>();
    for (const day of all) for (const row of day.rows) if (row.unread) ids.add(row.docId);
    return [...ids];
  }, [all]);

  const tab = (id: Filter, label: React.ReactNode) => (
    <button
      type="button"
      role="tab"
      aria-selected={filter === id}
      onClick={() => setFilter(id)}
      className={cn(
        "flex h-7 items-center gap-1.5 rounded-md px-2.5 text-body transition-colors",
        filter === id ? "bg-surface-selected font-medium text-foreground" : "text-muted-foreground hover:text-foreground",
      )}
    >
      {label}
    </button>
  );

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader className="gap-3">
        <h1 className="flex items-center gap-2 text-body font-medium">
          <Activity className="size-4 text-muted-foreground" />
          {t(($) => $.feed.title)}
        </h1>
        <div role="tablist" className="flex items-center gap-0.5">
          {tab("all", t(($) => $.feed.all))}
          {tab(
            "unread",
            <>
              {t(($) => $.feed.unread)}
              {unreadDocs.length > 0 && <span className="text-caption tabular-nums text-brand">{unreadDocs.length}</span>}
            </>,
          )}
        </div>
        <div className="flex-1" />
        {unreadDocs.length > 0 && (
          <Button size="xs" variant="ghost" onClick={() => unreadDocs.forEach((id) => markRead.mutate(id))}>
            <CheckCheck className="size-3.5" />
            {t(($) => $.feed.mark_all_read)}
          </Button>
        )}
        <Button size="xs" variant="outline" onClick={() => newDoc("markdown", null)}>
          <Plus className="size-3.5" />
          {t(($) => $.actions.new_doc)}
        </Button>
      </PageHeader>
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto w-full max-w-3xl px-4 py-4">
          {isPending ? (
            <div className="space-y-3 px-3">
              <Skeleton className="h-10 w-full" />
              <Skeleton className="h-10 w-full" />
            </div>
          ) : days.length === 0 ? (
            <div className="flex flex-col items-center gap-2 py-16 text-center">
              <p className="text-body font-medium">
                {filter === "unread" ? t(($) => $.feed.empty_unread) : t(($) => $.feed.empty_title)}
              </p>
              {filter === "all" && <p className="max-w-sm text-caption text-muted-foreground">{t(($) => $.feed.empty_body)}</p>}
            </div>
          ) : (
            days.map((day) => (
              <section key={day.day} className="mb-4">
                <h2 className="px-3 pt-2 pb-1 text-caption font-medium text-muted-foreground">{dayLabel(day.day)}</h2>
                <ul className="space-y-px">
                  {day.rows.map((row) => (
                    <FeedRow key={`${row.day}:${row.docId}`} row={row} folders={tree?.folders ?? []} />
                  ))}
                </ul>
              </section>
            ))
          )}
        </div>
      </div>
    </div>
  );
}
