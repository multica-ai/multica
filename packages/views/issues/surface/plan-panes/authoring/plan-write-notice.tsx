"use client";

import { AlertTriangle, GitMerge } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../../../i18n";
import type { PlanWriteErrorNotice } from "./use-plan-write-error";

/**
 * Inline failure banner for every authoring dialog.
 *
 * A conflict gets its own icon, heading, and tone. Rendering a 409 with the
 * same generic red "couldn't save" as a 500 would hide the one thing the
 * reader can act on: somebody else changed this plan, so re-read before
 * retrying. Amber/`GitMerge` for a conflict, destructive/`AlertTriangle` for
 * an actual failure.
 */
export function PlanWriteNotice({ notice }: { notice: PlanWriteErrorNotice | null }) {
  const { t } = useT("issues");
  if (!notice) return null;
  const Icon = notice.conflict ? GitMerge : AlertTriangle;
  return (
    <div
      role="alert"
      data-testid={notice.conflict ? "plan-write-conflict" : "plan-write-error"}
      className={cn(
        "mt-3 flex items-start gap-2 rounded-md border px-3 py-2 text-caption",
        notice.conflict
          ? "border-warning/40 bg-warning/5"
          : "border-destructive/40 bg-destructive/5",
      )}
    >
      <Icon
        aria-hidden="true"
        className={cn("mt-0.5 size-3.5 shrink-0", notice.conflict ? "text-warning" : "text-destructive")}
      />
      <div className="min-w-0">
        <p className="font-medium text-foreground">
          {notice.conflict
            ? t(($) => $.plan.authoring.error.conflict_title)
            : t(($) => $.plan.authoring.error.failed_title)}
        </p>
        <p className="text-muted-foreground">{notice.message}</p>
      </div>
    </div>
  );
}
