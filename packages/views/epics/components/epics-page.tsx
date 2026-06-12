"use client";

import { useMemo, useState } from "react";
import { Plus, Diamond, Search } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { epicListOptions } from "@multica/core/epics";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useModalStore } from "@multica/core/modals";
import type { Epic } from "@multica/core/types";
import { AppLink } from "../../navigation";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { PageHeader } from "../../layout/page-header";
import { useT } from "../../i18n";
import { matchesPinyin } from "../../editor/extensions/pinyin-match";

function EpicCard({ epic }: { epic: Epic }) {
  const { t } = useT("epics");
  const wsPaths = useWorkspacePaths();
  const progressPercent =
    epic.issue_count > 0
      ? Math.round((epic.done_count / epic.issue_count) * 100)
      : 0;

  return (
    <div className="group/card flex flex-col rounded-md border bg-card hover:border-primary/50 transition-colors">
      <div className="p-3 pb-2">
        <div className="flex items-center gap-2">
          <AppLink
            href={wsPaths.epicDetail(epic.id)}
            className="flex items-center gap-2 min-w-0 flex-1"
          >
            <span
              className="inline-block size-4 shrink-0 rounded-sm"
              style={{ backgroundColor: epic.color }}
            />
            <h3 className="font-medium text-sm truncate">{epic.title}</h3>
          </AppLink>
          <span
            className={`text-[10px] px-1.5 py-0.5 rounded-full ${
              epic.status === "open"
                ? "bg-primary/10 text-primary"
                : "bg-muted text-muted-foreground"
            }`}
          >
            {epic.status}
          </span>
        </div>

        {epic.description && (
          <p className="text-xs text-muted-foreground mt-1.5 line-clamp-2">
            {epic.description}
          </p>
        )}

        {epic.issue_count > 0 ? (
          <div className="flex justify-end items-center gap-1.5 pt-2">
            <div className="relative h-4 w-4">
              <svg className="h-4 w-4 -rotate-90" viewBox="0 0 16 16">
                <circle
                  className="text-muted"
                  strokeWidth="2"
                  stroke="currentColor"
                  fill="none"
                  r="6"
                  cx="8"
                  cy="8"
                />
                <circle
                  className="text-emerald-500"
                  strokeWidth="2"
                  stroke="currentColor"
                  fill="none"
                  r="6"
                  cx="8"
                  cy="8"
                  strokeDasharray={`${progressPercent * 0.377} 37.7`}
                  strokeLinecap="round"
                />
              </svg>
            </div>
            <span className="text-[10px] text-muted-foreground tabular-nums">
              {epic.done_count}/{epic.issue_count}
            </span>
          </div>
        ) : (
          <span className="text-[10px] text-muted-foreground pt-2 flex justify-end">
            {t(($) => $.detail.no_issues_yet)}
          </span>
        )}
      </div>
    </div>
  );
}

export function EpicsPage() {
  const { t } = useT("epics");
  const wsId = useWorkspaceId();
  const { data: epics = [], isLoading } = useQuery(epicListOptions(wsId));
  const openCreateEpic = () => useModalStore.getState().open("create-epic");

  const [search, setSearch] = useState("");
  const filteredEpics = useMemo(() => {
    const q = search.trim().toLowerCase();
    if (!q) return epics;
    return epics.filter(
      (e) =>
        e.title.toLowerCase().includes(q) ||
        (e.description && e.description.toLowerCase().includes(q)) ||
        matchesPinyin(e.title, q),
    );
  }, [epics, search]);

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <PageHeader className="justify-between px-5">
        <div className="flex items-center gap-2">
          <Diamond className="h-4 w-4 text-muted-foreground" />
          <h1 className="text-sm font-medium">{t(($) => $.page.title)}</h1>
          {!isLoading && epics.length > 0 && (
            <span className="text-xs text-muted-foreground tabular-nums">
              {epics.length}
            </span>
          )}
        </div>
        <Button size="sm" variant="outline" onClick={openCreateEpic}>
          <Plus className="h-3.5 w-3.5 mr-1" />
          {t(($) => $.page.new_epic)}
        </Button>
      </PageHeader>

      <div className="flex flex-1 min-h-0 flex-col overflow-hidden">
        {(epics.length > 0 || isLoading) && (
          <div className="flex h-12 shrink-0 items-center justify-between border-b px-4">
            <div className="relative flex-1 sm:flex-none">
              <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
              <Input
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder={t(($) => $.page.search_placeholder)}
                className="h-8 w-full sm:w-64 pl-8 text-sm"
              />
            </div>
            <span className="hidden sm:inline-block font-mono text-xs tabular-nums text-muted-foreground/70">
              {filteredEpics.length} / {epics.length}
            </span>
          </div>
        )}

        <div className="flex-1 overflow-y-auto">
          {isLoading ? (
            <div className="pt-4 px-5 grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3">
              {Array.from({ length: 4 }).map((_, i) => (
                <div key={i} className="flex flex-col rounded-md border p-3 gap-2">
                  <div className="flex items-center gap-2">
                    <Skeleton className="h-4 w-4 rounded" />
                    <Skeleton className="h-4 w-3/4" />
                  </div>
                  <Skeleton className="h-3 w-full" />
                </div>
              ))}
            </div>
          ) : epics.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-24 text-muted-foreground">
              <Diamond className="h-10 w-10 mb-3 opacity-30" />
              <p className="text-sm">{t(($) => $.page.empty)}</p>
              <Button
                size="sm"
                variant="outline"
                className="mt-3"
                onClick={openCreateEpic}
              >
                {t(($) => $.page.create_first)}
              </Button>
            </div>
          ) : filteredEpics.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-24 text-muted-foreground">
              <Search className="h-10 w-10 mb-3 opacity-30" />
              <p className="text-sm">{t(($) => $.page.no_search_results)}</p>
            </div>
          ) : (
            <div className="pt-4 pb-5 px-5 grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3">
              {filteredEpics.map((epic) => (
                <EpicCard key={epic.id} epic={epic} />
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
