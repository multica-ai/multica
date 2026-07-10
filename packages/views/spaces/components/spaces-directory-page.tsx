"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { LockKeyhole, Plus, Search, Users } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useJoinSpace } from "@multica/core/spaces/mutations";
import { spaceListOptions } from "@multica/core/spaces/queries";
import type { Space } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { cn } from "@multica/ui/lib/utils";
import { toast } from "sonner";
import { useT } from "../../i18n";
import { PageHeader } from "../../layout/page-header";
import { AppLink } from "../../navigation";
import { SpaceIcon } from "./space-icon";
import { SpacePreferenceActions } from "./space-preference-actions";

type SpaceFilter = "all" | "joined" | "open";

export function SpacesDirectoryPage() {
  const { t } = useT("spaces");
  const workspaceId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const { data: spaces = [], isLoading } = useQuery(spaceListOptions(workspaceId));
  const joinSpace = useJoinSpace();
  const [joiningId, setJoiningId] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const [filter, setFilter] = useState<SpaceFilter>("all");

  const visibleSpaces = useMemo(() => {
    const normalized = query.trim().toLocaleLowerCase();
    return spaces
      .filter((space) => !space.archived_at)
      .filter((space) => {
        if (filter === "joined" && !space.is_member) return false;
        if (filter === "open" && space.visibility !== "open") return false;
        return (
          !normalized ||
          space.name.toLocaleLowerCase().includes(normalized) ||
          space.key.toLocaleLowerCase().includes(normalized)
        );
      })
      .sort(compareSpaces);
  }, [filter, query, spaces]);

  const handleJoin = async (space: Space) => {
    setJoiningId(space.id);
    try {
      await joinSpace.mutateAsync(space.id);
      toast.success(t(($) => $.toast_joined));
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : t(($) => $.toast_save_failed),
      );
    } finally {
      setJoiningId(null);
    }
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader className="gap-3">
        <div className="min-w-0 flex-1">
          <h1 className="truncate text-sm font-medium">{t(($) => $.page.title)}</h1>
        </div>
        <Button size="sm" render={<AppLink href={paths.spaceNew()} />}>
          <Plus className="size-3.5" aria-hidden />
          {t(($) => $.page.new_space)}
        </Button>
      </PageHeader>

      <main className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto flex w-full max-w-5xl flex-col gap-5 px-6 py-8">
          <div className="space-y-1">
            <h2 className="text-xl font-semibold tracking-tight">
              {t(($) => $.page.title)}
            </h2>
            <p className="max-w-2xl text-sm text-muted-foreground">
              {t(($) => $.page.description)}
            </p>
          </div>

          <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
            <div className="relative min-w-0 flex-1">
              <Search
                className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground"
                aria-hidden
              />
              <Input
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                placeholder={t(($) => $.page.search_placeholder)}
                className="pl-9"
              />
            </div>
            <div className="flex items-center gap-1 rounded-lg bg-muted/60 p-1">
              {(["all", "joined", "open"] as const).map((value) => (
                <Button
                  key={value}
                  type="button"
                  size="sm"
                  variant="ghost"
                  className={cn(
                    "h-7 px-3 text-xs",
                    filter === value && "bg-background shadow-sm hover:bg-background",
                  )}
                  onClick={() => setFilter(value)}
                >
                  {t(($) => $.page[`filter_${value}`])}
                </Button>
              ))}
            </div>
          </div>

          {isLoading ? (
            <p className="py-10 text-center text-sm text-muted-foreground">
              {t(($) => $.page.loading)}
            </p>
          ) : visibleSpaces.length === 0 ? (
            <div className="rounded-xl border border-dashed px-6 py-12 text-center text-sm text-muted-foreground">
              {query || filter !== "all"
                ? t(($) => $.page.empty_search)
                : t(($) => $.page.empty_title)}
            </div>
          ) : (
            <div className="overflow-hidden rounded-xl border border-border/70 bg-card">
              {visibleSpaces.map((space, index) => (
                <div
                  key={space.id}
                  className={cn(
                    "flex flex-col gap-3 px-4 py-4 sm:flex-row sm:items-center",
                    index > 0 && "border-t border-border/60",
                  )}
                >
                  <AppLink
                    href={paths.spaceDetail(space.key)}
                    className="flex min-w-0 flex-1 items-center gap-3 rounded-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  >
                    <SpaceIcon space={space} className="size-5 shrink-0" />
                    <div className="min-w-0 flex-1">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="truncate text-sm font-medium">{space.name}</span>
                        <Badge variant="outline" className="font-mono text-[10px]">
                          {space.key}
                        </Badge>
                        {space.is_member && (
                          <Badge variant="secondary" className="text-[10px]">
                            {t(($) => $.page.filter_joined)}
                          </Badge>
                        )}
                      </div>
                      <div className="mt-1 flex items-center gap-1.5 text-xs text-muted-foreground">
                        {space.visibility === "private" ? (
                          <LockKeyhole className="size-3" aria-hidden />
                        ) : (
                          <Users className="size-3" aria-hidden />
                        )}
                        <span>
                          {space.visibility === "private"
                            ? t(($) => $.form.visibility_private)
                            : t(($) => $.form.visibility_open)}
                        </span>
                      </div>
                    </div>
                  </AppLink>

                  <div className="flex shrink-0 items-center justify-end gap-2">
                    <SpacePreferenceActions space={space} iconOnly />
                    {!space.is_member && space.visibility === "open" && (
                      <Button
                        type="button"
                        size="sm"
                        variant="outline"
                        disabled={joiningId === space.id}
                        onClick={() => void handleJoin(space)}
                      >
                        {t(($) => $.actions.join)}
                      </Button>
                    )}
                    <Button
                      size="sm"
                      variant="ghost"
                      render={<AppLink href={paths.spaceDetail(space.key)} />}
                    >
                      {t(($) => $.actions.open)}
                    </Button>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      </main>
    </div>
  );
}

function compareSpaces(a: Space, b: Space) {
  if (a.is_member !== b.is_member) return a.is_member ? -1 : 1;
  if ((a.is_member || a.is_pinned) && (b.is_member || b.is_pinned)) {
    const order = a.sort_order - b.sort_order;
    if (order !== 0) return order;
  }
  return a.name.localeCompare(b.name);
}
