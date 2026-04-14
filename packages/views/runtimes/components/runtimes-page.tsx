"use client";

import React, { useState, useCallback } from "react";
import { Server } from "lucide-react";
import { useDefaultLayout } from "react-resizable-panels";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ResizablePanelGroup,
  ResizablePanel,
  ResizableHandle,
} from "@multica/ui/components/ui/resizable";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { runtimeListOptions, runtimeKeys } from "@multica/core/runtimes/queries";
import { useUpdatableRuntimeIds } from "@multica/core/runtimes/hooks";
import { useWSEvent } from "@multica/core/realtime";
import { RuntimeList } from "./runtime-list";
import { RuntimeDetail } from "./runtime-detail";

type RuntimeFilter = "mine" | "all";

interface RuntimesPageProps {
  /** Desktop-only slot rendered above the runtime list (e.g. local daemon card) */
  topSlot?: React.ReactNode;
}

export default function RuntimesPage({ topSlot }: RuntimesPageProps = {}) {
  const isLoading = useAuthStore((s) => s.isLoading);
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const [filter, setFilter] = useState<RuntimeFilter>("mine");
  const [ownerFilter, setOwnerFilter] = useState<string | null>(null);
  const [selectedId, setSelectedId] = useState("");

  const ownerParam = filter === "mine" ? "me" as const : undefined;
  const { data: runtimes = [], isLoading: fetching } = useQuery(runtimeListOptions(wsId, ownerParam));

  const { defaultLayout, onLayoutChanged } = useDefaultLayout({
    id: "multica_runtimes_layout",
  });

  // Re-fetch on daemon register/deregister events.
  const handleDaemonEvent = useCallback(() => {
    qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
  }, [qc, wsId]);

  useWSEvent("daemon:register", handleDaemonEvent);
  const updatableIds = useUpdatableRuntimeIds(wsId);

  // Auto-select first runtime if nothing selected
  const effectiveSelectedId = selectedId && runtimes.some((r) => r.id === selectedId)
    ? selectedId
    : runtimes[0]?.id ?? "";
  const selected = runtimes.find((r) => r.id === effectiveSelectedId) ?? null;

  if (isLoading || fetching) {
    return (
      <div className="flex flex-1 min-h-0">
        {/* List skeleton */}
        <div className="w-72 border-r">
          <div className="flex h-12 items-center justify-between border-b px-4">
            <Skeleton className="h-4 w-20" />
          </div>
          <div className="divide-y">
            {Array.from({ length: 3 }).map((_, i) => (
              <div key={i} className="flex items-center gap-3 px-4 py-3">
                <Skeleton className="h-5 w-5 rounded" />
                <div className="flex-1 space-y-1.5">
                  <Skeleton className="h-4 w-28" />
                  <Skeleton className="h-3 w-20" />
                </div>
              </div>
            ))}
          </div>
        </div>
        {/* Detail skeleton */}
        <div className="flex-1 p-6 space-y-6">
          <div className="flex items-center gap-3">
            <Skeleton className="h-5 w-5 rounded" />
            <Skeleton className="h-5 w-32" />
          </div>
          <div className="space-y-3">
            {Array.from({ length: 3 }).map((_, i) => (
              <Skeleton key={i} className="h-16 w-full rounded-lg" />
            ))}
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      {topSlot}
      <ResizablePanelGroup
        orientation="horizontal"
        className="flex-1 min-h-0"
        defaultLayout={defaultLayout}
        onLayoutChanged={onLayoutChanged}
      >
        <ResizablePanel
          id="list"
          defaultSize={280}
          minSize={240}
          maxSize={400}
          groupResizeBehavior="preserve-pixel-size"
        >
          <RuntimeList
            runtimes={runtimes}
            selectedId={effectiveSelectedId}
            onSelect={setSelectedId}
            filter={filter}
            onFilterChange={setFilter}
            ownerFilter={ownerFilter}
            onOwnerFilterChange={setOwnerFilter}
            updatableIds={updatableIds}
          />
        </ResizablePanel>

        <ResizableHandle />

        <ResizablePanel id="detail" minSize="50%">
          {selected ? (
            <RuntimeDetail key={selected.id} runtime={selected} />
          ) : (
            <div className="flex h-full flex-col items-center justify-center text-muted-foreground">
              <Server className="h-10 w-10 text-muted-foreground/30" />
              <p className="mt-3 text-sm">Select a runtime to view details</p>
            </div>
          )}
        </ResizablePanel>
      </ResizablePanelGroup>
    </div>
  );
}
