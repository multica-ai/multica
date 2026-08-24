"use client";

import { AlertTriangle } from "lucide-react";
import { Sheet, SheetContent } from "@multica/ui/components/ui/sheet";
import { Button } from "@multica/ui/components/ui/button";
import { useWorkspaceDetail } from "@/lib/hooks";
import { DetailHeader } from "./header";
import { MetadataGrid } from "./metadata-grid";
import { MembersSection } from "./members-section";
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
      {/* packages/ui's SheetContent sets its side="right" width/max-width
          under the same [data-side=right] attribute selector — an unscoped
          override class loses that specificity fight (max-w-sm still caps
          the panel at 24rem even though w-1/2 is "later" in the className
          string). Scope the override under the same data-[side=right]:
          modifier so cn()'s tailwind-merge treats them as the same conflict
          group and actually replaces them. */}
      <SheetContent
        side="right"
        className="data-[side=right]:w-full data-[side=right]:sm:max-w-none data-[side=right]:sm:w-1/2 overflow-y-auto p-6"
      >
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
            {/* Recent activity renders last — it's a chronological log, not
                a summary, so it belongs after the at-a-glance sections. */}
            <DetailHeader workspace={workspace} status={data.status} />
            <MetadataGrid metadata={data.metadata} />
            <MembersSection
              workspaceId={data.metadata.id}
              members={data.members}
              pendingInvitations={data.pendingInvitations}
              inviteEligibility={data.inviteEligibility}
            />
            <LiteLlmSection litellm={data.litellm} />
            <IssueMetricsSection issues={data.issues} />
            <DerivedInsights insights={data.insights} />
            <ActivityTimeline events={data.activity} />
          </div>
        )}
      </SheetContent>
    </Sheet>
  );
}
