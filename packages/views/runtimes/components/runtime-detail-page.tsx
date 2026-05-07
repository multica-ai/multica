"use client";

import { Server } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { RuntimeDetail } from "./runtime-detail";
import { useT } from "../../i18n";

/**
 * Routed entry for `/{slug}/runtimes/{id}`. Reads the workspace runtime list
 * from cache (the list page already populated it), finds the matching
 * runtime, and renders the shared detail surface. We deliberately avoid
 * adding a per-runtime fetch endpoint — the list query is already keyed
 * per-workspace and is the source of truth for membership; reading from it
 * keeps cache invariants simple (one cache, one update path).
 */
export function RuntimeDetailPage({ runtimeId }: { runtimeId: string }) {
  const { t } = useT("runtimes");
  const wsId = useWorkspaceId();
  const { data: runtimes, isLoading } = useQuery(runtimeListOptions(wsId));

  if (isLoading) {
    return (
      <div className="flex h-full flex-col p-6">
        <Skeleton className="h-12 w-1/2" />
        <div className="mt-6 grid grid-cols-3 gap-3">
          <Skeleton className="h-20 rounded-lg" />
          <Skeleton className="h-20 rounded-lg" />
          <Skeleton className="h-20 rounded-lg" />
        </div>
        <Skeleton className="mt-4 h-48 w-full rounded-lg" />
      </div>
    );
  }

  const runtime = runtimes?.find((r) => r.id === runtimeId);
  if (!runtime) {
    return (
      <div className="flex h-full flex-col items-center justify-center text-muted-foreground">
        <Server className="h-10 w-10 text-muted-foreground/30" />
        <p className="mt-3 text-sm">{t(($) => $.detail_page.not_found_title)}</p>
        <p className="mt-1 text-xs text-muted-foreground/70">
          {t(($) => $.detail_page.not_found_hint)}
        </p>
      </div>
    );
  }

  return <RuntimeDetail runtime={runtime} />;
}
