"use client";

// Labrastro fork (OL-104): success-with-diagnostics notices for the single
// refresh dialog and the batch update dialog. Upstream
// `refresh-skill-dialog.tsx` and `skill-list-actions.tsx` keep only one-line
// mounts that call these hooks; the state, branching and notice dialogs live
// here so upstream changes to those dialogs merge without touching them.

import { useRef, useState, type ReactNode } from "react";
import { AlertTriangle } from "@multica/ui/icons";
import type { Skill, SkillImportDiagnostic, SkillSummary } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { useT } from "../../i18n";
import { SkillDiagnosticsNotice } from "./skill-diagnostics-notice";
import type { SkillRow } from "./skill-list-filter";

/**
 * Single refresh (`RefreshSkillDialog`). `hold` runs after the caches are
 * updated: when the refreshed skill carries diagnostics it keeps the dialog
 * open on the notice view (no success toast, no close, `onRefreshed` deferred
 * to Continue) and returns true so the caller stops there.
 */
export function useRefreshSkillNotices({
  skill,
  open,
  onOpenChange,
  onRefreshed,
}: {
  skill: SkillSummary;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onRefreshed?: (updated: Skill) => void;
}): { hold: (updated: Skill) => boolean; dialog: ReactNode } {
  const { t } = useT("skills");
  const { t: tPkg } = useT("skill-packages");
  // Diagnostics carried by a successful refresh (OL-104): kept visible in
  // the dialog instead of closing on a bare success toast.
  const [notices, setNotices] = useState<SkillImportDiagnostic[] | null>(null);
  const refreshedRef = useRef<Skill | null>(null);

  const hold = (updated: Skill): boolean => {
    const diagnostics = updated.id === skill.id ? (updated.diagnostics ?? []) : [];
    if (diagnostics.length === 0) return false;
    refreshedRef.current = updated;
    setNotices(diagnostics);
    return true;
  };

  const dialog = notices ? (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        if (!v) setNotices(null);
        onOpenChange(v);
      }}
    >
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t(($) => $.detail.refresh.dialog.title)}</DialogTitle>
        </DialogHeader>
        <SkillDiagnosticsNotice
          title={tPkg(($) => $.diagnostics.refreshed_with_notices, { count: notices.length })}
          diagnostics={notices}
        />
        <DialogFooter>
          <Button
            type="button"
            onClick={() => {
              const updated = refreshedRef.current;
              refreshedRef.current = null;
              setNotices(null);
              onOpenChange(false);
              if (updated && updated.id === skill.id) onRefreshed?.(updated);
            }}
          >
            {tPkg(($) => $.diagnostics.continue)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  ) : null;

  return { hold, dialog };
}

type BatchRefreshNotice = { skillName: string; diagnostic: SkillImportDiagnostic };

/** Per-run collector returned by `useBatchRefreshNotices().start()`. */
export interface BatchRefreshNoticeCollector {
  /** Record a successful refresh's diagnostics, tagged with its skill. */
  add: (row: SkillRow, refreshed: Skill) => void;
  /**
   * After the caches are invalidated: when any diagnostics were collected,
   * show them (with the partial-failure line when some refreshes failed)
   * instead of the toast/close, and return true so the caller stops there.
   */
  hold: (updated: number, failed: number) => boolean;
}

/** Batch update (`UpdateSkillsDialog`). Call `start()` once per confirm run. */
export function useBatchRefreshNotices({
  open,
  onOpenChange,
  onUpdated,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onUpdated?: () => void;
}): { start: () => BatchRefreshNoticeCollector; dialog: ReactNode } {
  const { t } = useT("skills");
  const { t: tPkg } = useT("skill-packages");
  // Diagnostics collected from successful refreshes (OL-104), each tagged
  // with its skill so a mixed batch stays readable. Shown in the dialog
  // instead of closing silently — including after a partial failure, where
  // they would otherwise be discarded with the error toast.
  const [notices, setNotices] = useState<{
    items: BatchRefreshNotice[];
    partial: { done: number; failed: number } | null;
  } | null>(null);

  const start = (): BatchRefreshNoticeCollector => {
    const collected: BatchRefreshNotice[] = [];
    return {
      add: (row, refreshed) => {
        for (const diagnostic of refreshed.diagnostics ?? []) {
          collected.push({ skillName: row.skill.name, diagnostic });
        }
      },
      hold: (updated, failed) => {
        if (collected.length === 0) return false;
        setNotices({
          items: collected,
          partial: failed === 0 ? null : { done: updated, failed },
        });
        return true;
      },
    };
  };

  const dialog = notices ? (
    <Dialog open={open} onOpenChange={(v) => { if (!v) setNotices(null); onOpenChange(v); }}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t(($) => $.actions.update_dialog_title)}</DialogTitle>
        </DialogHeader>
        <div className="rounded-md bg-warning/10 px-3 py-2 text-caption text-muted-foreground">
          <div className="mb-1.5 flex items-center gap-1.5 text-foreground">
            <AlertTriangle className="h-3.5 w-3.5 shrink-0 text-warning" />
            {tPkg(($) => $.diagnostics.refreshed_with_notices, { count: notices.items.length })}
          </div>
          {notices.partial && (
            <p className="mb-1.5">
              {t(($) => $.actions.update_partial_toast, {
                count: notices.partial.done,
                failed: notices.partial.failed,
              })}
            </p>
          )}
          <ul className="space-y-1.5">
            {notices.items.map((item, i) => (
              <li key={`${item.skillName}-${i}`} className="text-caption">
                <span className="font-medium text-foreground">{item.skillName}</span>
                <span className="text-muted-foreground">
                  {` · ${item.diagnostic.message}`}
                </span>
                <span className="ml-1.5 text-muted-foreground">
                  {item.diagnostic.code}
                  {item.diagnostic.path ? ` · ${item.diagnostic.path}` : ""}
                  {item.diagnostic.target ? ` · ${item.diagnostic.target}` : ""}
                </span>
              </li>
            ))}
          </ul>
        </div>
        <DialogFooter>
          <Button
            type="button"
            onClick={() => {
              const completed = !notices.partial;
              setNotices(null);
              onOpenChange(false);
              // A partial run keeps the selection for retry; only a clean
              // run clears it.
              if (completed) onUpdated?.();
            }}
          >
            {tPkg(($) => $.diagnostics.continue)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  ) : null;

  return { start, dialog };
}
