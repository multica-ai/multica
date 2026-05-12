"use client";

import { useState } from "react";
import { BookOpen, Plus, Search, AlertCircle, ChevronRight, FileText } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { wikiListOptions } from "@multica/core/wiki";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { AppLink } from "../../navigation";
import { PageHeader } from "../../layout/page-header";
import type { WikiPage } from "@multica/core/types";

function formatRelativeDate(date: string): string {
  const diff = Date.now() - new Date(date).getTime();
  const days = Math.floor(diff / (1000 * 60 * 60 * 24));
  if (days < 1) return "Today";
  if (days === 1) return "1d ago";
  if (days < 30) return `${days}d ago`;
  const months = Math.floor(days / 30);
  return `${months}mo ago`;
}

function WikiPageRow({ page }: { page: WikiPage }) {
  const wsPaths = useWorkspacePaths();

  return (
    <AppLink
      href={wsPaths.wikiPage(page.id)}
      className="group flex h-11 items-center gap-3 border-b px-5 text-sm transition-colors hover:bg-accent/40 last:border-b-0"
    >
      <FileText className="h-4 w-4 shrink-0 text-muted-foreground" />
      <span className="min-w-0 flex-1 truncate font-medium">{page.title}</span>
      <span className="shrink-0 text-xs text-muted-foreground tabular-nums">
        {formatRelativeDate(page.updated_at)}
      </span>
      <ChevronRight className="h-3 w-3 shrink-0 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100" />
    </AppLink>
  );
}

export function WikiListPage() {
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const [search, setSearch] = useState("");

  const {
    data: pages = [],
    isLoading,
    error,
    refetch,
  } = useQuery(wikiListOptions(wsId));

  const filteredPages = search.trim()
    ? pages.filter((p) =>
        p.title.toLowerCase().includes(search.trim().toLowerCase()),
      )
    : pages;

  if (isLoading) {
    return (
      <div className="flex h-full flex-col">
        <PageHeader className="justify-between px-5">
          <div className="flex items-center gap-2">
            <BookOpen className="h-4 w-4 text-muted-foreground" />
            <h1 className="text-sm font-medium">Wiki</h1>
          </div>
        </PageHeader>
        <div className="flex-1 overflow-y-auto">
          <div className="flex h-11 items-center gap-2 border-b px-5">
            <Skeleton className="h-7 w-64 rounded-md" />
          </div>
          <div className="divide-y">
            {Array.from({ length: 5 }).map((_, i) => (
              <Skeleton key={i} className="mx-5 my-2 h-7 w-full" />
            ))}
          </div>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex h-full flex-col">
        <PageHeader className="justify-between px-5">
          <div className="flex items-center gap-2">
            <BookOpen className="h-4 w-4 text-muted-foreground" />
            <h1 className="text-sm font-medium">Wiki</h1>
          </div>
        </PageHeader>
        <div className="flex flex-1 flex-col items-center justify-center gap-3 px-6 py-16 text-center">
          <AlertCircle className="h-8 w-8 text-destructive" />
          <div>
            <p className="text-sm font-medium">Couldn&rsquo;t load wiki pages</p>
            <p className="mt-1 text-xs text-muted-foreground">
              {error instanceof Error
                ? error.message
                : "Something went wrong fetching wiki pages."}
            </p>
          </div>
          <Button type="button" variant="outline" size="sm" onClick={() => refetch()}>
            Try again
          </Button>
        </div>
      </div>
    );
  }

  return (
    <div className="flex h-full flex-col">
      <PageHeader className="justify-between px-5">
        <div className="flex items-center gap-2">
          <BookOpen className="h-4 w-4 text-muted-foreground" />
          <h1 className="text-sm font-medium">Wiki</h1>
          {pages.length > 0 && (
            <span className="text-xs text-muted-foreground tabular-nums">
              {pages.length}
            </span>
          )}
        </div>
        <AppLink href={wsPaths.wikiPage("new")}>
          <Button type="button" size="sm">
            <Plus className="h-3 w-3" />
            New page
          </Button>
        </AppLink>
      </PageHeader>

      <div className="flex-1 overflow-y-auto">
        {pages.length === 0 ? (
          <EmptyState />
        ) : (
          <>
            <div className="flex h-11 items-center gap-2 border-b px-5">
              <div className="relative">
                <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
                <Input
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                  placeholder="Search pages…"
                  className="h-8 w-64 pl-8 text-sm"
                />
              </div>
            </div>

            {filteredPages.length === 0 ? (
              <div className="flex flex-col items-center justify-center gap-2 px-4 py-16 text-center text-muted-foreground">
                <Search className="h-8 w-8 text-muted-foreground/40" />
                <p className="text-sm">No matches</p>
                <p className="max-w-xs text-xs">
                  No pages match &ldquo;{search}&rdquo;.
                </p>
              </div>
            ) : (
              <div className="divide-y">
                {filteredPages.map((page) => (
                  <WikiPageRow key={page.id} page={page} />
                ))}
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}

function EmptyState() {
  const wsPaths = useWorkspacePaths();

  return (
    <div className="flex flex-1 flex-col items-center justify-center px-6 py-16 text-center">
      <div className="flex h-12 w-12 items-center justify-center rounded-full bg-muted">
        <BookOpen className="h-6 w-6 text-muted-foreground" />
      </div>
      <h2 className="mt-4 text-base font-semibold">No pages yet</h2>
      <p className="mt-1 max-w-md text-sm text-muted-foreground">
        Create your first wiki page to start building a knowledge base for your
        workspace.
      </p>
      <AppLink href={wsPaths.wikiPage("new")}>
        <Button type="button" size="sm" className="mt-5">
          <Plus className="h-3 w-3" />
          New page
        </Button>
      </AppLink>
    </div>
  );
}
