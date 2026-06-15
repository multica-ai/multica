"use client";

import { useMemo, useState } from "react";
import { Plus, Timer, Search } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { sprintListOptions } from "@multica/core/sprints";
import { SPRINT_STATUS_CONFIG } from "@multica/core/sprints/config";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useModalStore } from "@multica/core/modals";
import type { Sprint } from "@multica/core/types";
import { AppLink } from "../../navigation";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { PageHeader } from "../../layout/page-header";
import { useT } from "../../i18n";
import { matchesPinyin } from "../../editor/extensions/pinyin-match";

function formatDate(dateStr: string): string {
  const d = new Date(dateStr);
  return d.toLocaleDateString("en-US", { month: "short", day: "numeric" });
}

function SprintCard({ sprint }: { sprint: Sprint }) {
  const { t } = useT("sprints");
  const wsPaths = useWorkspacePaths();
  const statusCfg = SPRINT_STATUS_CONFIG[sprint.status];
  const progressPercent =
    sprint.issue_count > 0
      ? Math.round((sprint.done_count / sprint.issue_count) * 100)
      : 0;

  return (
    <div className="group/card flex flex-col rounded-md border bg-card hover:border-primary/50 transition-colors">
      <div className="p-3 pb-2">
        <div className="flex items-center gap-2">
          <AppLink
            href={wsPaths.sprintDetail(sprint.id)}
            className="flex items-center gap-2 min-w-0 flex-1"
          >
            <Timer className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
            <h3 className="font-medium text-sm truncate">{sprint.name}</h3>
          </AppLink>
          <span
            className={`text-[10px] px-1.5 py-0.5 rounded-full ${statusCfg.badgeBg} ${statusCfg.badgeText}`}
          >
            {statusCfg.label}
          </span>
        </div>

        <div className="flex items-center gap-1 mt-1.5 text-xs text-muted-foreground">
          <span>{formatDate(sprint.start_date)}</span>
          <span>—</span>
          <span>{formatDate(sprint.end_date)}</span>
        </div>

        {sprint.goal && (
          <p className="text-xs text-muted-foreground mt-1 line-clamp-2">
            {sprint.goal}
          </p>
        )}

        {sprint.issue_count > 0 ? (
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
              {sprint.done_count}/{sprint.issue_count}
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

export function SprintsPage() {
  const { t } = useT("sprints");
  const wsId = useWorkspaceId();
  const { data: sprints = [], isLoading } = useQuery(sprintListOptions(wsId));
  const openCreateSprint = () =>
    useModalStore.getState().open("create-sprint");

  const [search, setSearch] = useState("");
  const filteredSprints = useMemo(() => {
    const q = search.trim().toLowerCase();
    if (!q) return sprints;
    return sprints.filter(
      (s) =>
        s.name.toLowerCase().includes(q) ||
        (s.goal && s.goal.toLowerCase().includes(q)) ||
        matchesPinyin(s.name, q),
    );
  }, [sprints, search]);

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <PageHeader className="justify-between px-5">
        <div className="flex items-center gap-2">
          <Timer className="h-4 w-4 text-muted-foreground" />
          <h1 className="text-sm font-medium">{t(($) => $.page.title)}</h1>
          {!isLoading && sprints.length > 0 && (
            <span className="text-xs text-muted-foreground tabular-nums">
              {sprints.length}
            </span>
          )}
        </div>
        <Button size="sm" variant="outline" onClick={openCreateSprint}>
          <Plus className="h-3.5 w-3.5 mr-1" />
          {t(($) => $.page.new_sprint)}
        </Button>
      </PageHeader>

      <div className="flex flex-1 min-h-0 flex-col overflow-hidden">
        {(sprints.length > 0 || isLoading) && (
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
              {filteredSprints.length} / {sprints.length}
            </span>
          </div>
        )}

        <div className="flex-1 overflow-y-auto">
          {isLoading ? (
            <div className="pt-4 px-5 grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3">
              {Array.from({ length: 4 }).map((_, i) => (
                <div key={i} className="flex flex-col rounded-md border p-3 gap-2">
                  <div className="flex items-center gap-2">
                    <Skeleton className="h-3.5 w-3.5 rounded" />
                    <Skeleton className="h-4 w-3/4" />
                  </div>
                  <Skeleton className="h-3 w-1/2" />
                </div>
              ))}
            </div>
          ) : sprints.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-24 text-muted-foreground">
              <Timer className="h-10 w-10 mb-3 opacity-30" />
              <p className="text-sm">{t(($) => $.page.empty)}</p>
              <Button
                size="sm"
                variant="outline"
                className="mt-3"
                onClick={openCreateSprint}
              >
                {t(($) => $.page.create_first)}
              </Button>
            </div>
          ) : filteredSprints.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-24 text-muted-foreground">
              <Search className="h-10 w-10 mb-3 opacity-30" />
              <p className="text-sm">{t(($) => $.page.no_search_results)}</p>
            </div>
          ) : (
            <div className="pt-4 pb-5 px-5 grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3">
              {filteredSprints.map((sprint) => (
                <SprintCard key={sprint.id} sprint={sprint} />
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
