"use client";

import { useCallback } from "react";
import { toast } from "sonner";
import type { BatchIssueResult, BatchUpdateIssuesResponse } from "@multica/core/types";
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

function SkippedList({ skipped }: { skipped: BatchIssueResult[] }) {
  const { t } = useT("issues");
  const listed = skipped.slice(0, LISTED_SKIPS);
  return (
    <div className="mt-1 flex flex-col gap-1">
      {listed.map((result) => (
        <div key={result.issue_id} className="flex min-w-0 gap-2 text-caption">
          <span className="shrink-0 font-mono text-muted-foreground">{result.identifier ?? result.issue_id.slice(0, 8)}</span>
          <span className="min-w-0 truncate">
            {isSkipReason(result.code)
              ? t(($) => $.batch.skip_reason[result.code as SkipReason])
              : result.message || t(($) => $.batch.skip_reason.write_failed)}
          </span>
        </div>
      ))}
      {skipped.length > LISTED_SKIPS && (
        <div className="text-caption text-muted-foreground">
          {t(($) => $.batch.skipped_more, { count: skipped.length - LISTED_SKIPS })}
        </div>
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
      toast.warning(t(($) => $.batch.skipped_title, { updated: response.updated, count: skipped.length }), {
        description: <SkippedList skipped={skipped} />,
        duration: 10_000,
        action: onSelectSkipped
          ? {
              label: t(($) => $.batch.select_skipped, { count: skipped.length }),
              onClick: () => onSelectSkipped(skipped.map((result) => result.issue_id)),
            }
          : undefined,
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
