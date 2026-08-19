"use client";

import { useMemo, useState } from "react";
import { useWorkspaceList, useLiteLlmHealth } from "@/lib/hooks";
import { Toolbar } from "@/components/toolbar";
import { WorkspaceTable } from "@/components/workspace-table";
import { WorkspacePagination } from "@/components/pagination";
import { DetailPanel } from "@/components/detail-panel";
import { Button } from "@multica/ui/components/ui/button";
import type { SortColumn, SortDirection, WorkspaceListItem, WorkspaceStatus } from "@/lib/types";

const PAGE_SIZE = 50; // plan §3.4: "50 rows per page default"

export default function Home() {
  const [search, setSearch] = useState("");
  const [status, setStatus] = useState<WorkspaceStatus | "all">("all");
  const [sort, setSort] = useState<SortColumn>("activity");
  const [direction, setDirection] = useState<SortDirection>("desc");
  const [page, setPage] = useState(1);
  const [selected, setSelected] = useState<WorkspaceListItem | null>(null);
  const [activityFrom, setActivityFrom] = useState<string | undefined>(undefined);
  const [activityTo, setActivityTo] = useState<string | undefined>(undefined);

  const params = useMemo(
    () => ({ search, status, sort, direction, page, pageSize: PAGE_SIZE, activityFrom, activityTo }),
    [search, status, sort, direction, page, activityFrom, activityTo],
  );
  const { data, isLoading, isError, refetch } = useWorkspaceList(params);
  const { data: litellmHealth } = useLiteLlmHealth();

  const hasActiveFilters =
    search.trim() !== "" || status !== "all" || activityFrom !== undefined || activityTo !== undefined;
  function clearFilters() {
    setSearch("");
    setStatus("all");
    setActivityFrom(undefined);
    setActivityTo(undefined);
    setPage(1);
  }

  function handleSortChange(column: SortColumn) {
    if (column === sort) {
      setDirection((d) => (d === "asc" ? "desc" : "asc"));
    } else {
      // Plan §3.3: "Click column header to sort ascending" on first click of
      // a newly-selected column; a second click toggles descending (above).
      setSort(column);
      setDirection("asc");
    }
    setPage(1);
  }

  return (
    <main className="mx-auto max-w-7xl px-6 py-8">
      <div className="mb-6">
        <h1 className="text-display-sm font-medium text-foreground">Workspaces</h1>
        <p className="mt-1 text-body text-muted-foreground">
          Monitor every agent workspace&rsquo;s status, model, and cost in one place.
        </p>
        {litellmHealth && !litellmHealth.configured && (
          <p className="mt-2 rounded-md border border-warning/30 bg-warning/10 px-3 py-2 text-label text-warning">
            LiteLLM is not configured — cost and team data will show as &quot;not linked&quot;.
          </p>
        )}
      </div>

      <Toolbar
        search={search}
        onSearchChange={(v) => {
          setSearch(v);
          setPage(1);
        }}
        status={status}
        onStatusChange={(v) => {
          setStatus(v);
          setPage(1);
        }}
        rangeStart={data && data.items.length > 0 ? (page - 1) * PAGE_SIZE + 1 : 0}
        rangeEnd={data ? (page - 1) * PAGE_SIZE + data.items.length : 0}
        total={data?.total ?? 0}
        activityFrom={activityFrom}
        activityTo={activityTo}
        onActivityRangeChange={({ from, to }) => {
          setActivityFrom(from);
          setActivityTo(to);
          setPage(1);
        }}
      />

      {isError ? (
        <div className="flex flex-col items-center gap-3 py-10 text-center">
          <p className="text-destructive">Failed to load workspaces.</p>
          <Button variant="outline" size="sm" onClick={() => refetch()}>
            Retry
          </Button>
        </div>
      ) : isLoading && !data ? (
        <p className="py-10 text-center text-muted-foreground">Loading workspaces…</p>
      ) : (
        <WorkspaceTable
          items={data?.items ?? []}
          sort={sort}
          direction={direction}
          onSortChange={handleSortChange}
          onRowClick={setSelected}
          selectedId={selected?.id}
          hasActiveFilters={hasActiveFilters}
          onClearFilters={clearFilters}
        />
      )}

      <div className="mt-6">
        <WorkspacePagination
          page={page}
          pageSize={PAGE_SIZE}
          total={data?.total ?? 0}
          onPageChange={setPage}
        />
      </div>

      <DetailPanel
        workspace={selected}
        onClose={() => setSelected(null)}
      />
    </main>
  );
}
