"use client";

import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, Loader2 } from "@multica/ui/icons";
import type {
  SkillPackageApplyResult,
  SkillPackagePreview,
} from "@multica/core/api/schemas";
import { errorCode } from "@multica/core/api";
import { invalidateSkillPackageQueries } from "@multica/core/skills/package-queries";
import { Button } from "@multica/ui/components/ui/button";
import { DialogFooter } from "@multica/ui/components/ui/dialog";
import { useT } from "../../i18n";
import { defaultSelectedPaths, canApplyOverwrite, hasSelectedConflict } from "../lib/package-preview-model";
import {
  PackageApplyReport,
  PackagePreviewPanel,
  type OnConflictChoice,
} from "./package-preview-panel";

type StagePhase = "select" | "applying" | "report" | "stale" | "malformed" | "error";

/** Terminal apply failures split by what actually happened (OL-104 rework):
 *  a structured source read error (timeout / unavailable / tree_unavailable)
 *  is a complete server answer about the source — showing it under the
 *  "couldn't read the server response" title would claim a broken response
 *  and an unknown write outcome the server never produced. Other coded
 *  errors are plain request failures; only a null result or a body-less
 *  network/proxy error keeps the unreadable-response semantics. */
type ApplyErrorKind = "source" | "request" | "unreadable";

function applyErrorKind(code: string | undefined): ApplyErrorKind {
  if (
    code === "source_timeout" ||
    code === "source_unavailable" ||
    code === "tree_unavailable"
  ) {
    return "source";
  }
  return code ? "request" : "unreadable";
}

function StageMessage({
  title,
  description,
}: {
  title: string;
  description: string;
}) {
  return (
    <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-2 px-5 py-8 text-center">
      <AlertTriangle className="h-5 w-5 text-warning" />
      <div className="text-body font-medium">{title}</div>
      <div role="status" className="max-w-sm text-caption text-muted-foreground">
        {description}
      </div>
    </div>
  );
}

/**
 * The select → apply → report flow shared by the import and rescan dialogs.
 * The caller owns preview production (URL form / rescan request); this stage
 * owns selection state, the conflict strategy, and every terminal state —
 * stale token, malformed response, request failure — without ever silently
 * retrying a write or treating an unreadable result as success.
 */
export function PackageApplyStage({
  wsId,
  preview,
  existingPackageName,
  allowEmptyApply = false,
  applyRequest,
  onRepreview,
  onClose,
  onBusyChange,
}: {
  wsId: string;
  preview: SkillPackagePreview;
  existingPackageName?: string | null;
  /** Rescan applies with no selection (metadata refresh); a fresh import
   *  with nothing selected would create nothing, so it is blocked. */
  allowEmptyApply?: boolean;
  applyRequest: (
    skills: string[],
    onConflict: OnConflictChoice,
  ) => Promise<SkillPackageApplyResult | null>;
  /** Produces a fresh preview; the stage remounts on the new token. */
  onRepreview: () => void;
  onClose: () => void;
  /** Tells the host dialog whether a write is in flight, so it can block
   *  every close path (X, Escape, outside click) until the report lands. */
  onBusyChange?: (busy: boolean) => void;
}) {
  const { t } = useT("skill-packages");
  const qc = useQueryClient();
  const [selected, setSelected] = useState<ReadonlySet<string>>(() =>
    defaultSelectedPaths(preview.candidates),
  );
  const [onConflict, setOnConflict] = useState<OnConflictChoice>("skip");
  const [phase, setPhase] = useState<StagePhase>("select");
  const [report, setReport] = useState<SkillPackageApplyResult | null>(null);
  const [errorKind, setErrorKind] = useState<ApplyErrorKind>("unreadable");
  const [errorMessage, setErrorMessage] = useState("");

  const handleSelectedChange = (next: Set<string>) => {
    setSelected(next);
    setOnConflict((current) => {
      // Overwrite only survives while every selected conflict permits it;
      // losing that (or losing the conflict itself) falls back to skip.
      if (current === "overwrite" && !canApplyOverwrite(preview.candidates, next)) {
        return "skip";
      }
      if (!hasSelectedConflict(preview.candidates, next)) return "skip";
      return current;
    });
  };

  const handleApply = async () => {
    if (phase === "applying") return;
    setPhase("applying");
    onBusyChange?.(true);
    try {
      // Re-validate at submit time: an overwrite that became unavailable
      // since the choice must go out as skip, never as a doomed overwrite.
      const effectiveConflict =
        onConflict === "overwrite" && !canApplyOverwrite(preview.candidates, selected)
          ? "skip"
          : onConflict;
      const result = await applyRequest([...selected], effectiveConflict);
      if (result === null) {
        // Unreadable write result: indeterminate, never a fake success.
        setPhase("malformed");
        return;
      }
      setReport(result);
      setPhase("report");
    } catch (err) {
      const code = errorCode(err);
      if (code === "preview_stale" || code === "source_changed") {
        setPhase("stale");
      } else {
        setErrorKind(applyErrorKind(code));
        setErrorMessage(
          err instanceof Error && err.message
            ? err.message
            : t(($) => $.preview.error_fallback),
        );
        setPhase("error");
      }
    } finally {
      onBusyChange?.(false);
      // Package metadata commits before items, so even a failed or
      // unreadable apply may have persisted. Refresh every projection the
      // write could have touched, on every settle path.
      await invalidateSkillPackageQueries(qc, wsId, { includeSkills: true });
    }
  };

  if (phase === "stale") {
    return (
      <>
        <StageMessage
          title={t(($) => $.preview.stale_title)}
          description={t(($) => $.preview.stale_description)}
        />
        <DialogFooter>
          <Button type="button" variant="ghost" onClick={onClose}>
            {t(($) => $.import.cancel)}
          </Button>
          <Button type="button" onClick={onRepreview}>
            {t(($) => $.preview.repreview)}
          </Button>
        </DialogFooter>
      </>
    );
  }

  if (phase === "malformed" || phase === "error") {
    const title =
      phase === "malformed"
        ? t(($) => $.preview.malformed_title)
        : errorKind === "source"
          ? t(($) => $.preview.source_failed_title)
          : errorKind === "request"
            ? t(($) => $.preview.request_failed_title)
            : t(($) => $.preview.malformed_title);
    return (
      <>
        <StageMessage
          title={title}
          description={
            phase === "malformed"
              ? t(($) => $.preview.malformed_apply_description)
              : errorMessage
          }
        />
        <DialogFooter>
          <Button type="button" variant="ghost" onClick={onClose}>
            {t(($) => $.import.cancel)}
          </Button>
          <Button type="button" onClick={onRepreview}>
            {t(($) => $.preview.repreview)}
          </Button>
        </DialogFooter>
      </>
    );
  }

  if (phase === "report" && report) {
    return (
      <>
        <PackageApplyReport result={report} />
        <DialogFooter>
          <Button type="button" variant="outline" onClick={onRepreview}>
            {t(($) => $.preview.repreview)}
          </Button>
          <Button type="button" onClick={onClose}>
            {t(($) => $.report.done)}
          </Button>
        </DialogFooter>
      </>
    );
  }

  const applying = phase === "applying";
  return (
    <>
      <PackagePreviewPanel
        preview={preview}
        existingPackageName={existingPackageName}
        selected={selected}
        onSelectedChange={handleSelectedChange}
        onConflict={onConflict}
        onConflictChange={setOnConflict}
      />
      <DialogFooter>
        <div className="mr-auto text-caption text-muted-foreground">
          {t(($) => $.preview.selected_count, { count: selected.size })}
        </div>
        <Button type="button" variant="ghost" onClick={onClose} disabled={applying}>
          {t(($) => $.import.cancel)}
        </Button>
        <Button
          type="button"
          onClick={handleApply}
          disabled={applying || (!allowEmptyApply && selected.size === 0)}
          aria-busy={applying}
        >
          {applying ? (
            <>
              <Loader2 className="h-3 w-3 animate-spin" />
              {t(($) => $.preview.applying)}
            </>
          ) : selected.size > 0 ? (
            t(($) => $.preview.apply_selected, { count: selected.size })
          ) : (
            t(($) => $.preview.apply)
          )}
        </Button>
      </DialogFooter>
    </>
  );
}
