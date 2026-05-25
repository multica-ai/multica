"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Check, Copy } from "lucide-react";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { PageHeader } from "@multica/views/layout/page-header";
import { useNavigation } from "@multica/views/navigation";
import { TasksFilters } from "./components/tasks-filters";
import { TasksTable } from "./components/tasks-table";
import { TasksPagination } from "./components/tasks-pagination";
import { useCerebroTasksStore } from "../core/store";
import { cerebroTasksListOptions } from "../core/queries";
import { parseTasksFilterFromSearchParams, tasksFilterPath } from "../core/url-state";

// Cross-agent tasks page (JEH-900). Lists every agent task in the
// workspace with filters for agent, status, time range, and type. Backed
// by /api/cerebro/tasks. Hidden when the cerebro_tasks feature flag is
// off.
export function TasksPage() {
  const enabled = useFeatureFlag("cerebro_tasks");
  const workspace = useCurrentWorkspace();
  const navigation = useNavigation();
  const [copied, setCopied] = useState(false);
  const [hydrated, setHydrated] = useState(false);
  const scrollRef = useRef<HTMLDivElement>(null);

  // Select primitives separately — returning a fresh object from the store
  // each render would loop. See packages/core/CLAUDE.md "Common Zustand
  // footguns".
  const agentId = useCerebroTasksStore((s) => s.agentId);
  const issueId = useCerebroTasksStore((s) => s.issueId);
  const projectId = useCerebroTasksStore((s) => s.projectId);
  const status = useCerebroTasksStore((s) => s.status);
  const type = useCerebroTasksStore((s) => s.type);
  const range = useCerebroTasksStore((s) => s.range);
  const customFrom = useCerebroTasksStore((s) => s.customFrom);
  const customTo = useCerebroTasksStore((s) => s.customTo);
  const limit = useCerebroTasksStore((s) => s.limit);
  const offset = useCerebroTasksStore((s) => s.offset);
  const search = useCerebroTasksStore((s) => s.search);
  const groupBy = useCerebroTasksStore((s) => s.groupBy);
  const visibleColumns = useCerebroTasksStore((s) => s.visibleColumns);
  const setFilter = useCerebroTasksStore((s) => s.setFilter);

  const filter = useMemo(
    () => ({
      agentId,
      issueId,
      projectId,
      status,
      type,
      range,
      customFrom,
      customTo,
      search,
      groupBy,
      limit,
      offset,
    }),
    [
      agentId,
      issueId,
      projectId,
      status,
      type,
      range,
      customFrom,
      customTo,
      search,
      groupBy,
      limit,
      offset,
    ],
  );
  const searchParamsString = navigation.searchParams.toString();
  const currentPath = searchParamsString
    ? `${navigation.pathname}?${searchParamsString}`
    : navigation.pathname;
  const filterPath = tasksFilterPath(navigation.pathname, filter);

  useEffect(() => {
    setFilter(parseTasksFilterFromSearchParams(new URLSearchParams(searchParamsString)));
    setHydrated(true);
  }, [searchParamsString, setFilter]);

  useEffect(() => {
    if (!hydrated || filterPath === currentPath) return;
    navigation.replace(filterPath);
  }, [currentPath, filterPath, hydrated, navigation]);

  const wsId = workspace?.id ?? "";
  const list = useQuery(cerebroTasksListOptions(wsId, filter));
  const tasks = list.data?.tasks ?? [];
  const total = list.data?.total ?? 0;

  const scrollKey = `cerebro-tasks-scroll:${filterPath}`;

  useEffect(() => {
    const node = scrollRef.current;
    if (!node) return;
    const raw = window.sessionStorage.getItem(scrollKey);
    if (!raw) return;
    const top = Number.parseInt(raw, 10);
    if (!Number.isFinite(top)) return;

    let frame = 0;
    const timers: number[] = [];
    const restore = () => {
      node.scrollTop = top;
    };

    // Data can arrive before the browser has laid out enough table height to
    // accept the previous scrollTop. Retry briefly across layout passes.
    frame = window.requestAnimationFrame(restore);
    timers.push(window.setTimeout(restore, 50));
    timers.push(window.setTimeout(restore, 150));
    timers.push(window.setTimeout(restore, 350));

    return () => {
      window.cancelAnimationFrame(frame);
      for (const timer of timers) window.clearTimeout(timer);
    };
  }, [scrollKey, list.dataUpdatedAt, tasks.length]);

  function persistScroll() {
    const node = scrollRef.current;
    if (!node) return;
    window.sessionStorage.setItem(scrollKey, String(node.scrollTop));
  }

  useEffect(() => {
    window.addEventListener("pagehide", persistScroll);
    window.addEventListener("beforeunload", persistScroll);
    return () => {
      window.removeEventListener("pagehide", persistScroll);
      window.removeEventListener("beforeunload", persistScroll);
    };
  }, [scrollKey]);

  function handleScroll() {
    persistScroll();
  }

  async function copyLink() {
    await navigator.clipboard.writeText(navigation.getShareableUrl(filterPath));
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1500);
  }

  if (!enabled) return null;

  if (!workspace) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
        Workspace context indlæses…
      </div>
    );
  }

  return (
    <div className="flex h-full flex-col">
      <PageHeader className="justify-between gap-3">
        <div className="flex min-w-0 flex-col">
          <h1 className="text-sm font-semibold">Tasks</h1>
          <p className="truncate text-[11px] text-muted-foreground">
            Alle agent-tasks på tværs af workspace
          </p>
        </div>
        <button
          type="button"
          onClick={copyLink}
          className="inline-flex h-8 shrink-0 items-center gap-1.5 rounded-md border px-2 text-xs font-medium text-muted-foreground hover:text-foreground"
          aria-label="Kopiér link til denne task-visning"
        >
          {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
          <span className="hidden sm:inline">{copied ? "Kopieret" : "Copy link"}</span>
        </button>
      </PageHeader>

      <div ref={scrollRef} onScroll={handleScroll} className="flex-1 min-h-0 overflow-y-auto">
        <div className="flex flex-col gap-3 p-3 sm:gap-4 sm:p-6">
          <TasksFilters wsId={wsId} />

          <TasksTable
            tasks={tasks}
            isLoading={list.isLoading || list.isFetching}
            isError={list.isError}
            errorMessage={list.error instanceof Error ? list.error.message : undefined}
            workspaceSlug={workspace.slug}
            visibleColumns={visibleColumns}
            groupBy={groupBy}
          />

          {!list.isError && (
            <TasksPagination total={total} loadedCount={tasks.length} />
          )}
        </div>
      </div>
    </div>
  );
}
