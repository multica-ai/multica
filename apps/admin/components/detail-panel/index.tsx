"use client";

import { AlertTriangle } from "lucide-react";
import { Sheet, SheetContent } from "@multica/ui/components/ui/sheet";
import { Button } from "@multica/ui/components/ui/button";
import { useWorkspaceDetail } from "@/lib/hooks";
import { DetailHeader } from "./header";
import { MetadataGrid } from "./metadata-grid";
import { ActivityTimeline } from "./activity-timeline";
import { IssueMetricsSection } from "./issue-metrics";
import { LiteLlmSection } from "./litellm-section";
import { DerivedInsights } from "./derived-insights";
import type { WorkspaceListItem } from "@/lib/types";

interface DetailPanelProps {
  workspace: WorkspaceListItem | null;
  onClose: () => void;
}

export function DetailPanel({ workspace, onClose }: DetailPanelProps) {
  const { data, isLoading, isError, refetch } = useWorkspaceDetail(workspace?.id ?? null);

  return (
    <Sheet open={workspace !== null} onOpenChange={(open) => !open && onClose()}>
      {/* packages/ui's SheetContent caps side="right" at sm:max-w-sm (24rem) —
          override to the prototype's 50%-viewport-width slide-out panel. */}
      <SheetContent side="right" className="w-full sm:max-w-none sm:w-1/2 overflow-y-auto p-6">
        {!workspace ? null : isError ? (
          // Plan §5.3: "Panel load error: inline error message with retry button."
          <div className="flex flex-col items-center gap-3 py-10 text-center">
            <AlertTriangle className="size-8 text-destructive" aria-hidden />
            <p className="text-destructive">Failed to load workspace detail.</p>
            <Button variant="outline" size="sm" onClick={() => refetch()}>
              Retry
            </Button>
          </div>
        ) : isLoading || !data ? (
          <p className="text-muted-foreground">Loading workspace…</p>
        ) : (
          <div className="flex flex-col gap-6">
            {/* Section order follows plan §2.2 A–F: Header, Activity,
                Metadata, LiteLLM, Issue Metrics, Derived Insights. */}
            <DetailHeader workspace={workspace} status={data.status} />
            <ActivityTimeline events={data.activity} />
            <MetadataGrid metadata={data.metadata} />
            <LiteLlmSection litellm={data.litellm} />
            <IssueMetricsSection issues={data.issues} />
            <DerivedInsights insights={data.insights} />
          </div>
        )}
      </SheetContent>
    </Sheet>
  );
}
