"use client";

import { useState } from "react";
import { History } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { workspaceTaskRunsOptions } from "@multica/core/runs/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { AppLink } from "../../navigation";
import { PageHeader } from "../../layout/page-header";
import { Badge } from "@multica/ui/components/ui/badge";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  Pagination,
  PaginationContent,
  PaginationItem,
  PaginationNext,
  PaginationPrevious,
} from "@multica/ui/components/ui/pagination";
import { cn } from "@multica/ui/lib/utils";
import type { WorkspaceTaskRun } from "@multica/core/types";

const PAGE_SIZE = 50;

const STATUS_VARIANT: Record<
  string,
  "default" | "secondary" | "destructive" | "outline" | "ghost"
> = {
  queued: "outline",
  dispatched: "secondary",
  running: "secondary",
  completed: "default",
  failed: "destructive",
  cancelled: "outline",
};

function formatDateTime(value: string | null): string {
  if (!value) return "--";
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return "--";
  return d.toLocaleString();
}

function formatDuration(start: string | null, end: string | null): string {
  if (!start || !end) return "--";
  const ms = new Date(end).getTime() - new Date(start).getTime();
  if (!Number.isFinite(ms) || ms < 0) return "--";
  if (ms < 1000) return `${ms}ms`;
  const sec = Math.round(ms / 1000);
  if (sec < 60) return `${sec}s`;
  const min = Math.floor(sec / 60);
  const remSec = sec % 60;
  if (min < 60) return `${min}m ${remSec}s`;
  const hr = Math.floor(min / 60);
  const remMin = min % 60;
  return `${hr}h ${remMin}m`;
}

function RunRow({ run }: { run: WorkspaceTaskRun }) {
  const wsPaths = useWorkspacePaths();
  const variant = STATUS_VARIANT[run.status] ?? "outline";
  const issueLink =
    run.issue_id && run.issue_identifier ? wsPaths.issueDetail(run.issue_id) : null;

  return (
    <div className="grid grid-cols-[100px_minmax(0,2fr)_minmax(0,1fr)_minmax(0,1fr)_80px_minmax(0,2fr)] items-center gap-3 border-b px-5 py-2 text-sm">
      <div>
        <Badge variant={variant} className="capitalize">
          {run.status}
        </Badge>
      </div>
      <div className="min-w-0 truncate">
        {issueLink ? (
          <AppLink
            href={issueLink}
            className="hover:underline"
            title={run.issue_title ?? ""}
          >
            <span className="font-medium tabular-nums text-muted-foreground">
              {run.issue_identifier}
            </span>
            {run.issue_title ? (
              <span className="ml-2">{run.issue_title}</span>
            ) : null}
          </AppLink>
        ) : (
          <span className="text-muted-foreground">--</span>
        )}
      </div>
      <div className="min-w-0 truncate text-muted-foreground">
        {run.agent_name}
      </div>
      <div className="min-w-0 truncate tabular-nums text-muted-foreground">
        {formatDateTime(run.started_at ?? run.created_at)}
      </div>
      <div className="tabular-nums text-muted-foreground">
        {formatDuration(run.started_at, run.completed_at)}
      </div>
      <div
        className={cn(
          "min-w-0 truncate text-xs",
          run.error ? "text-destructive" : "text-muted-foreground",
        )}
        title={run.error ?? ""}
      >
        {run.error ?? "--"}
      </div>
    </div>
  );
}

export function RunsPage() {
  const wsId = useWorkspaceId();
  const [offset, setOffset] = useState(0);
  const { data, isLoading } = useQuery(
    workspaceTaskRunsOptions(wsId, { limit: PAGE_SIZE, offset }),
  );

  const items = data?.items ?? [];
  const total = data?.total ?? 0;
  const hasMore = data?.has_more ?? false;
  const page = Math.floor(offset / PAGE_SIZE) + 1;
  const lastPage = Math.max(1, Math.ceil(total / PAGE_SIZE));

  return (
    <div className="flex h-full flex-col">
      <PageHeader className="justify-between px-5">
        <div className="flex items-center gap-2">
          <History className="h-4 w-4 text-muted-foreground" />
          <h1 className="text-sm font-medium">Runs</h1>
          {!isLoading && total > 0 && (
            <span className="text-xs text-muted-foreground tabular-nums">
              {total}
            </span>
          )}
        </div>
      </PageHeader>

      <div className="flex-1 overflow-y-auto">
        <div className="sticky top-0 z-[1] grid grid-cols-[100px_minmax(0,2fr)_minmax(0,1fr)_minmax(0,1fr)_80px_minmax(0,2fr)] items-center gap-3 border-b bg-muted/30 px-5 py-2 text-xs font-medium text-muted-foreground">
          <span>Status</span>
          <span>Issue</span>
          <span>Agent</span>
          <span>Started</span>
          <span>Duration</span>
          <span>Error</span>
        </div>

        {isLoading ? (
          <div className="space-y-1 p-4">
            {Array.from({ length: 8 }).map((_, i) => (
              <Skeleton key={i} className="h-9 w-full" />
            ))}
          </div>
        ) : items.length === 0 ? (
          <div className="flex flex-col items-center py-16 px-5">
            <History className="h-10 w-10 mb-3 text-muted-foreground opacity-30" />
            <p className="text-sm text-muted-foreground">No runs yet</p>
            <p className="text-xs text-muted-foreground mt-1">
              Agent task executions will appear here.
            </p>
          </div>
        ) : (
          items.map((run) => <RunRow key={run.id} run={run} />)
        )}
      </div>

      {total > PAGE_SIZE && (
        <div className="border-t px-5 py-2">
          <Pagination>
            <PaginationContent>
              <PaginationItem>
                <PaginationPrevious
                  aria-disabled={offset === 0}
                  onClick={(e) => {
                    e.preventDefault();
                    if (offset === 0) return;
                    setOffset(Math.max(0, offset - PAGE_SIZE));
                  }}
                />
              </PaginationItem>
              <PaginationItem>
                <span className="px-3 text-xs text-muted-foreground tabular-nums">
                  {page} / {lastPage}
                </span>
              </PaginationItem>
              <PaginationItem>
                <PaginationNext
                  aria-disabled={!hasMore}
                  onClick={(e) => {
                    e.preventDefault();
                    if (!hasMore) return;
                    setOffset(offset + PAGE_SIZE);
                  }}
                />
              </PaginationItem>
            </PaginationContent>
          </Pagination>
        </div>
      )}
    </div>
  );
}
