"use client";

import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@multica/ui/components/ui/sheet";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@multica/ui/components/ui/table";
import { useAnalyticsWorkspaceBreakdown } from "@/lib/hooks";
import type { AnalyticsWorkspaceBreakdownParams } from "@/lib/types";
import { bucketLabel } from "./bucket-label";

interface WorkspaceBreakdownSheetProps {
  selection: AnalyticsWorkspaceBreakdownParams | null;
  onClose: () => void;
}

const ERROR_LABELS: Record<string, string> = {
  auth: "Auth",
  rate_limit: "Rate limit",
  timeout: "Timeout",
  provider: "Provider",
  runtime: "Runtime",
  agent: "Agent",
  other: "Other",
};

const RUN_LABELS: Record<string, string> = {
  completed: "Completed",
  failed: "Failed",
  skipped: "Skipped",
  other: "In-flight",
};

function titleFor(selection: AnalyticsWorkspaceBreakdownParams): string {
  const labels = selection.kind === "errors" ? ERROR_LABELS : RUN_LABELS;
  const label = labels[selection.segment] ?? selection.segment;
  return selection.kind === "errors" ? `${label} errors by workspace` : `${label} autopilot runs by workspace`;
}

export function WorkspaceBreakdownSheet({ selection, onClose }: WorkspaceBreakdownSheetProps) {
  const { data, isLoading, isError, refetch } = useAnalyticsWorkspaceBreakdown(selection);

  return (
    <Sheet open={selection !== null} onOpenChange={(open) => !open && onClose()}>
      <SheetContent
        side="right"
        className="data-[side=right]:w-full data-[side=right]:sm:max-w-none data-[side=right]:sm:w-1/2 overflow-y-auto p-6"
      >
        {!selection ? null : (
          <>
            <SheetHeader className="p-0 pr-10">
              <SheetTitle>{titleFor(selection)}</SheetTitle>
              <SheetDescription>
                {bucketLabel(selection.from)} – {bucketLabel(selection.to)}
              </SheetDescription>
            </SheetHeader>

            {isError ? (
              <div className="flex flex-col items-start gap-3 py-6">
                <p className="text-destructive">Failed to load the workspace breakdown.</p>
                <button type="button" onClick={() => refetch()} className="text-primary hover:underline">
                  Retry
                </button>
              </div>
            ) : isLoading || !data ? (
              <p className="py-6 text-muted-foreground">Loading workspace breakdown…</p>
            ) : data.items.length === 0 ? (
              <p className="py-6 text-muted-foreground">No workspaces contributed to this segment.</p>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Workspace</TableHead>
                    <TableHead className="text-right">Count</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {data.items.map((item) => (
                    <TableRow key={item.workspaceId}>
                      <TableCell className="font-medium text-foreground">{item.workspaceName}</TableCell>
                      <TableCell className="text-right tabular-nums">{item.count.toLocaleString()}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </>
        )}
      </SheetContent>
    </Sheet>
  );
}
