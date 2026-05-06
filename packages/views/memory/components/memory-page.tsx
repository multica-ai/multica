"use client";

import { useState, useMemo } from "react";
import { Plus, BookOpen, Search as SearchIcon } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import {
  memoryListOptions,
  memorySearchOptions,
  MEMORY_KINDS,
  MEMORY_KIND_LABELS,
} from "@multica/core/memory";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useModalStore } from "@multica/core/modals";
import { useActorName } from "@multica/core/workspace/hooks";
import type { MemoryArtifact, MemoryArtifactKind } from "@multica/core/types";
import { AppLink } from "../../navigation";
import { ActorAvatar } from "../../common/actor-avatar";
import { PageHeader } from "../../layout/page-header";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";

// Same relative-time helper as projects/issues — kept inline rather than
// shared because the bar for promoting it to a util is "third caller."
function formatRelativeDate(date: string): string {
  const diff = Date.now() - new Date(date).getTime();
  const days = Math.floor(diff / (1000 * 60 * 60 * 24));
  if (days < 1) return "Today";
  if (days === 1) return "1d ago";
  if (days < 30) return `${days}d ago`;
  const months = Math.floor(days / 30);
  return `${months}mo ago`;
}

// Tailwind classes for each kind's badge — picked to be visually distinct
// without being loud. Stays here (not in @multica/core) because it's pure
// presentation; the canonical kind list lives in core.
const KIND_BADGE: Record<MemoryArtifactKind, string> = {
  wiki_page: "bg-sky-500/10 text-sky-600 dark:text-sky-400",
  agent_note: "bg-violet-500/10 text-violet-600 dark:text-violet-400",
  runbook: "bg-amber-500/10 text-amber-600 dark:text-amber-400",
  decision: "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400",
};

function MemoryRow({ artifact }: { artifact: MemoryArtifact }) {
  const wsPaths = useWorkspacePaths();
  const { getActorName } = useActorName();
  const authorName = getActorName(artifact.author_type, artifact.author_id);

  return (
    <AppLink
      href={wsPaths.memoryDetail(artifact.id)}
      className="group/row flex h-11 items-center gap-3 px-5 text-sm transition-colors hover:bg-accent/40"
    >
      <span
        className={cn(
          "inline-flex items-center rounded-md px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wide shrink-0 w-20 justify-center",
          KIND_BADGE[artifact.kind],
        )}
      >
        {MEMORY_KIND_LABELS[artifact.kind]}
      </span>

      <span className="min-w-0 flex-1 truncate font-medium">
        {artifact.title}
      </span>

      {artifact.tags.length > 0 && (
        <div className="hidden md:flex items-center gap-1 shrink-0 max-w-[40%] overflow-hidden">
          {artifact.tags.slice(0, 3).map((tag) => (
            <span
              key={tag}
              className="rounded-full border px-2 py-0.5 text-[10px] text-muted-foreground truncate max-w-[120px]"
            >
              {tag}
            </span>
          ))}
          {artifact.tags.length > 3 && (
            <span className="text-[10px] text-muted-foreground">
              +{artifact.tags.length - 3}
            </span>
          )}
        </div>
      )}

      <span className="hidden lg:flex w-32 shrink-0 items-center gap-1.5 text-xs text-muted-foreground">
        <ActorAvatar
          actorType={artifact.author_type}
          actorId={artifact.author_id}
          size={16}
        />
        <span className="truncate">{authorName}</span>
      </span>

      <span className="w-20 shrink-0 text-right text-xs text-muted-foreground tabular-nums">
        {formatRelativeDate(artifact.updated_at)}
      </span>
    </AppLink>
  );
}

export function MemoryPage() {
  const wsId = useWorkspaceId();
  const [kindFilter, setKindFilter] = useState<MemoryArtifactKind | "all">("all");
  const [searchInput, setSearchInput] = useState("");
  // Trim once for both the enable check and the request payload.
  const trimmedSearch = searchInput.trim();
  const isSearching = trimmedSearch.length > 0;

  const listOptions = useMemo(
    () =>
      memoryListOptions(wsId, kindFilter === "all" ? undefined : { kind: kindFilter }),
    [wsId, kindFilter],
  );
  const searchOptions = useMemo(
    () =>
      memorySearchOptions(wsId, {
        q: trimmedSearch,
        kind: kindFilter === "all" ? undefined : kindFilter,
      }),
    [wsId, trimmedSearch, kindFilter],
  );

  // Search and list are separate cache spaces — switching modes doesn't
  // invalidate the other. We just toggle which query is enabled.
  const listQuery = useQuery({ ...listOptions, enabled: !isSearching });
  const searchQuery = useQuery({ ...searchOptions, enabled: isSearching });

  const artifacts = isSearching ? searchQuery.data ?? [] : listQuery.data ?? [];
  const isLoading = isSearching ? searchQuery.isLoading : listQuery.isLoading;

  const openCreate = () =>
    useModalStore.getState().open("create-memory-artifact");

  return (
    <div className="flex h-full flex-col">
      <PageHeader className="justify-between px-5">
        <div className="flex items-center gap-2">
          <BookOpen className="h-4 w-4 text-muted-foreground" />
          <h1 className="text-sm font-medium">Memory</h1>
          {!isLoading && artifacts.length > 0 && (
            <span className="text-xs text-muted-foreground tabular-nums">
              {artifacts.length}
            </span>
          )}
        </div>
        <Button size="sm" variant="outline" onClick={openCreate}>
          <Plus className="h-3.5 w-3.5 mr-1" />
          New artifact
        </Button>
      </PageHeader>

      {/* Filter strip: kind tabs + search. Tabs are pills; the active one
          fills with accent. Search clears the kind filter context (still
          applies kind to the search query, just visually independent). */}
      <div className="flex items-center gap-3 border-b px-5 py-2">
        <div className="flex items-center gap-1">
          <KindPill
            active={kindFilter === "all"}
            onClick={() => setKindFilter("all")}
          >
            All
          </KindPill>
          {MEMORY_KINDS.map((k) => (
            <KindPill
              key={k}
              active={kindFilter === k}
              onClick={() => setKindFilter(k)}
            >
              {MEMORY_KIND_LABELS[k]}
            </KindPill>
          ))}
        </div>
        <div className="ml-auto flex items-center gap-1.5 rounded-md border bg-background px-2 py-1 w-72 max-w-full">
          <SearchIcon className="h-3.5 w-3.5 text-muted-foreground shrink-0" />
          <input
            type="text"
            placeholder="Search memory..."
            value={searchInput}
            onChange={(e) => setSearchInput(e.target.value)}
            className="flex-1 min-w-0 bg-transparent text-sm placeholder:text-muted-foreground outline-none"
          />
        </div>
      </div>

      <div className="flex-1 overflow-y-auto">
        {isLoading ? (
          <div className="p-5 space-y-1">
            {Array.from({ length: 5 }).map((_, i) => (
              <Skeleton key={i} className="h-11 w-full" />
            ))}
          </div>
        ) : artifacts.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-24 text-muted-foreground">
            <BookOpen className="h-10 w-10 mb-3 opacity-30" />
            <p className="text-sm">
              {isSearching
                ? "No artifacts match your search"
                : "No memory artifacts yet"}
            </p>
            {!isSearching && (
              <Button size="sm" variant="outline" className="mt-3" onClick={openCreate}>
                Create your first artifact
              </Button>
            )}
          </div>
        ) : (
          artifacts.map((a) => <MemoryRow key={a.id} artifact={a} />)
        )}
      </div>
    </div>
  );
}

function KindPill({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "rounded-full px-2.5 py-1 text-xs transition-colors cursor-pointer",
        active
          ? "bg-accent text-accent-foreground"
          : "text-muted-foreground hover:bg-accent/40",
      )}
    >
      {children}
    </button>
  );
}
