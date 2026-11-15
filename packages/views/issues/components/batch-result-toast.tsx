"use client";

import { useCallback } from "react";
import { toast } from "sonner";
import type { BatchUpdateIssuesResponse } from "@multica/core/types";
import { BatchOpsUnsupportedError } from "@multica/core/api";
import { useT } from "../../i18n";

const SKIP_REASONS = [
  "invalid_id",
  "not_found",
  "invalid_value",
  "parent_self",
  "parent_not_found",
  "parent_cycle",
  "workflow_rejected",
  "assignee_not_allowed",
  "properties_too_large",
  "write_failed",
] as const;
type SkipReason = (typeof SKIP_REASONS)[number];

const isSkipReason = (code: string | undefined): code is SkipReason =>
  SKIP_REASONS.includes(code as SkipReason);

/** How many skipped issues the toast names before summarising the rest. */
const LISTED_SKIPS = 3;

let skippedToastSeq = 0;

// Rendered under <Toaster />, outside the app's providers: no hooks here, every
// string arrives translated.
function SkippedList({
  rows,
  more,
  action,
}: {
  rows: { id: string; identifier: string; reason: string }[];
  more: string | null;
  action: { label: string; onClick: () => void } | null;
}) {
  return (
    <div className="mt-1 flex flex-col gap-1">
      {rows.map((row) => (
        <div key={row.id} className="flex min-w-0 gap-2 text-caption">
          <span className="shrink-0 font-mono text-muted-foreground">{row.identifier}</span>
          <span className="min-w-0 break-words">{row.reason}</span>
        </div>
      ))}
      {more && <div className="text-caption text-muted-foreground">{more}</div>}
      {action && (
        <button
          type="button"
          className="mt-1 self-start text-caption font-medium text-primary hover:underline cursor-pointer"
          onClick={action.onClick}
        >
          {action.label}
        </button>
      )}
    </div>
  );
}

export interface BatchResultToastOptions {
  /** Offer "Select only these" so the skipped issues can be fixed and retried. */
  onSelectSkipped?: (issueIds: string[]) => void;
  /** Override the success toast; `null` stays silent on full success. */
  successMessage?: string | null;
}

/**
 * Reports a batch write: a success toast when every issue took the change,
 * otherwise a warning naming the skipped issues and why. Returns whether the
 * whole selection applied, so callers keep the selection for a retry when not.
 */
export function useBatchResultToast() {
  const { t } = useT("issues");

  const report = useCallback(
    (response: BatchUpdateIssuesResponse, count: number, options: BatchResultToastOptions = {}): boolean => {
      if (response.unconfirmed) {
        toast.warning(t(($) => $.batch.result_unknown));
        return false;
      }
      const results = response.results;
      if (!results) {
        // Older servers report only a count.
        if (response.updated >= count) {
          if (options.successMessage !== null) {
            toast.success(options.successMessage ?? t(($) => $.batch.update_success, { count }));
          }
          return true;
        }
        toast.warning(t(($) => $.batch.update_partial, { updated: response.updated, count }));
        return false;
      }
      const skipped = results.filter((result) => result.outcome === "skipped");
      if (skipped.length === 0) {
        if (options.successMessage !== null) {
          toast.success(options.successMessage ?? t(($) => $.batch.update_success, { count }));
        }
        return true;
      }
      const { onSelectSkipped } = options;
      const toastId = `batch-skipped-${++skippedToastSeq}`;
      const rows = skipped.slice(0, LISTED_SKIPS).map((result) => ({
        id: result.issue_id,
        identifier: result.identifier ?? result.issue_id.slice(0, 8),
        reason: isSkipReason(result.code)
          ? t(($) => $.batch.skip_reason[result.code as SkipReason])
          : result.message || t(($) => $.batch.skip_reason.write_failed),
      }));
      const more =
        skipped.length > LISTED_SKIPS
          ? t(($) => $.batch.skipped_more, { count: skipped.length - LISTED_SKIPS })
          : null;
      const action = onSelectSkipped
        ? {
            label: t(($) => $.batch.select_skipped, { count: skipped.length }),
            onClick: () => {
              onSelectSkipped(skipped.map((result) => result.issue_id));
              toast.dismiss(toastId);
            },
          }
        : null;
      toast.warning(t(($) => $.batch.skipped_title, { updated: response.updated, count: skipped.length }), {
        id: toastId,
        description: <SkippedList rows={rows} more={more} action={action} />,
        duration: 10_000,
      });
      return false;
    },
    [t],
  );

  const reportError = useCallback(
    (err: unknown) => {
      toast.error(
        err instanceof BatchOpsUnsupportedError
          ? t(($) => $.batch.server_outdated)
          : err instanceof Error && err.message
            ? err.message
            : t(($) => $.batch.update_failed),
      );
    },
    [t],
  );

  return { report, reportError };
}
