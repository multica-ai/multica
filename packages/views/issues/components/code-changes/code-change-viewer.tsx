"use client";

/**
 * CodeChangeViewer — a run's code change in the full-window viewer (MUL-7651).
 *
 * Same frame as the attachment viewer: a near-black stage, a dark top bar,
 * Esc to close. The top bar switches between the run's own change and the
 * whole issue's: the linked pull request's diff when GitHub can supply it,
 * otherwise the branch diff the daemon captured with the newest run.
 */

import { useEffect, useMemo, useState } from "react";
import { createPortal } from "react-dom";
import { AnimatePresence, motion } from "motion/react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Copy, Download, GitCompareArrows, GitPullRequest, Loader2, X } from "lucide-react";
import {
  buildDiffFiles,
  latestLineOfWork,
  parsePatch,
  pullRequestForChange,
  type DiffFileView,
} from "@multica/core/code-changes";
import { issuePullRequestDiffOptions, issuePullRequestsOptions } from "@multica/core/github";
import { issueCodeChangeOptions, issueCodeChangesOptions } from "@multica/core/issues/queries";
import type { CodeChangeFile, TaskCodeChange } from "@multica/core/types";
import { useActorName } from "@multica/core/workspace/hooks";
import { copyText } from "@multica/ui/lib/clipboard";
import { UI_EASE_OUT, UI_MOTION_DURATION } from "@multica/ui/lib/motion";
import { cn } from "@multica/ui/lib/utils";
import { DiffStat, DiffView } from "../../../editor/diff/diff-view";
import { downloadBlob } from "../../../editor/utils/mermaid-export";
import {
  ChromeButton,
  ChromeDivider,
  DRAG,
  DiffLayoutToggle,
  NO_DRAG,
  type DiffLayout,
} from "../../../editor/viewer-chrome";
import { useT, useTimeAgo } from "../../../i18n";
import { openExternal } from "../../../platform";
import { useImmersiveMode } from "../../../platform/use-immersive-mode";

type Scope = "run" | "issue";

interface ScopeData {
  files: DiffFileView[];
  patch: string | null;
  fileCount: number;
  additions: number;
  deletions: number;
  filesTruncated: boolean;
  patchOmitted: string | null;
  branch: string;
}

function toScopeData(
  detail: {
    files: CodeChangeFile[];
    patch: string | null;
    file_count: number;
    additions: number;
    deletions: number;
    files_truncated: boolean;
    patch_omitted: string | null;
  },
  branch: string,
): ScopeData {
  return {
    files: buildDiffFiles(detail.files, detail.patch ? parsePatch(detail.patch) : []),
    patch: detail.patch,
    fileCount: detail.file_count,
    additions: detail.additions,
    deletions: detail.deletions,
    filesTruncated: detail.files_truncated,
    patchOmitted: detail.patch_omitted,
    branch,
  };
}

function patchFilename(label: string, suffix: string): string {
  const stem = label.replace(/[^A-Za-z0-9._-]+/g, "-").replace(/^[-.]+|[-.]+$/g, "") || "changes";
  return `${stem}-${suffix}.patch`;
}

export function CodeChangeViewer({
  issueId,
  change,
  runNumber,
  open,
  onClose,
}: {
  issueId: string;
  /** The run row this viewer opened on. */
  change: TaskCodeChange;
  /** The run's place on the issue; 0 when unknown. */
  runNumber: number;
  open: boolean;
  onClose: () => void;
}) {
  useImmersiveMode(open);

  useEffect(() => {
    if (!open) return;
    const handler = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    document.addEventListener("keydown", handler);
    return () => document.removeEventListener("keydown", handler);
  }, [open, onClose]);

  if (typeof document === "undefined") return null;
  return createPortal(
    <AnimatePresence>
      {open && (
        <motion.div
          className="fixed inset-0 z-50 flex flex-col bg-black/95 backdrop-blur-xl"
          role="dialog"
          aria-modal="true"
          style={NO_DRAG}
          initial={{ opacity: 0 }}
          animate={{ opacity: 1, transition: { duration: UI_MOTION_DURATION.fast, ease: UI_EASE_OUT } }}
          exit={{ opacity: 0, transition: { duration: UI_MOTION_DURATION.fast, ease: UI_EASE_OUT } }}
        >
          <CodeChangePanel issueId={issueId} change={change} runNumber={runNumber} onClose={onClose} />
        </motion.div>
      )}
    </AnimatePresence>,
    document.body,
  );
}

function CodeChangePanel({
  issueId,
  change,
  runNumber,
  onClose,
}: {
  issueId: string;
  change: TaskCodeChange;
  runNumber: number;
  onClose: () => void;
}) {
  const { t } = useT("issues");
  const { t: tEditor } = useT("editor");
  const timeAgo = useTimeAgo();
  const { getActorName } = useActorName();
  const [scope, setScope] = useState<Scope>("run");
  const [layout, setLayout] = useState<DiffLayout>("unified");

  // Whole issue, preferred source: the pull request carrying the branch.
  const { data: prList } = useQuery(issuePullRequestsOptions(issueId));
  const pr = useMemo(
    () => (prList ? pullRequestForChange(prList.pull_requests, change) : null),
    [prList, change],
  );
  const prDiff = useQuery({
    ...issuePullRequestDiffOptions(issueId, pr?.id ?? ""),
    enabled: scope === "issue" && !!pr,
  });
  // Fallback: the newest run's branch diff in this repository.
  const { data: allChanges } = useQuery(issueCodeChangesOptions(issueId));
  const line = useMemo(
    () => (allChanges ? latestLineOfWork(allChanges, change.repo_key) : null),
    [allChanges, change.repo_key],
  );
  const usePR = !!pr && !prDiff.isError;
  const lineId = line?.id ?? change.id;

  const runQuery = useQuery(issueCodeChangeOptions(issueId, change.id));
  const lineQuery = useQuery({
    ...issueCodeChangeOptions(issueId, lineId),
    enabled: scope === "issue" && !usePR && lineId !== change.id,
  });

  const run = useMemo(
    () => (runQuery.data ? toScopeData(runQuery.data, change.branch) : null),
    [runQuery.data, change.branch],
  );
  const issueData = useMemo((): ScopeData | null => {
    if (usePR) {
      if (!prDiff.data) return null;
      return toScopeData(prDiff.data, pr?.branch ?? change.branch);
    }
    if (lineId === change.id) return run;
    return lineQuery.data ? toScopeData(lineQuery.data, lineQuery.data.branch) : null;
  }, [usePR, prDiff.data, pr, change.branch, lineId, change.id, run, lineQuery.data]);

  const current = scope === "run" ? run : issueData;
  const loading =
    scope === "run"
      ? runQuery.isPending
      : usePR
        ? prDiff.isPending
        : lineId === change.id
          ? runQuery.isPending
          : lineQuery.isPending;
  const failed =
    scope === "run" ? runQuery.isError : !usePR && lineId !== change.id && lineQuery.isError;

  const runLabel = runNumber > 0
    ? t(($) => $.code_changes.scope_run_numbered, { number: runNumber })
    : t(($) => $.code_changes.scope_run);
  // The issue-wide totals before anything is fetched: the PR's own counts, or
  // the branch row's.
  const issueTotals = usePR && pr && (pr.additions || pr.deletions)
    ? { additions: pr.additions ?? 0, deletions: pr.deletions ?? 0 }
    : line
      ? { additions: line.additions, deletions: line.deletions }
      : null;

  const meta = [
    getActorName("agent", change.agent_id),
    runNumber > 0 ? t(($) => $.code_changes.run_number, { number: runNumber }) : "",
    timeAgo(change.created_at),
    change.repo_label,
  ].filter(Boolean);

  const copyPatch = async () => {
    if (!current?.patch) return;
    const ok = await copyText(current.patch);
    if (ok) toast.success(t(($) => $.code_changes.patch_copied));
    else toast.error(t(($) => $.code_changes.copy_failed));
  };
  const downloadPatch = () => {
    if (!current?.patch) return;
    const suffix = scope === "run" ? (runNumber > 0 ? `run-${runNumber}` : "run") : pr ? `pr-${pr.number}` : "issue";
    downloadBlob(new Blob([current.patch], { type: "text/x-diff;charset=utf-8" }), patchFilename(change.repo_label, suffix));
  };

  const listLabel = current
    ? scope === "run"
      ? t(($) => $.code_changes.list_run, { count: current.fileCount })
      : t(($) => $.code_changes.list_issue, { count: current.fileCount })
    : "";
  const notes: string[] = [];
  if (current) {
    if (scope === "issue" && pr && prDiff.isError) {
      notes.push(t(($) => $.code_changes.pr_fallback, { number: pr.number }));
    }
    if (current.patchOmitted === "too_large") notes.push(t(($) => $.code_changes.patch_too_large));
    else if (current.patchOmitted) notes.push(t(($) => $.code_changes.patch_unavailable));
    if (current.filesTruncated) notes.push(t(($) => $.code_changes.files_truncated, { count: current.files.length }));
  }

  return (
    <>
      <header
        className="dark @container grid h-14 shrink-0 grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] items-center gap-4 pl-3 pr-2 text-foreground"
        style={DRAG}
      >
        <div className="flex min-w-0 items-center gap-2.5">
          <span className="flex size-8 shrink-0 items-center justify-center rounded-md bg-secondary text-muted-foreground">
            <GitCompareArrows className="size-4" />
          </span>
          <div className="min-w-0">
            <p className="truncate text-body font-medium">{t(($) => $.code_changes.viewer_title)}</p>
            <p className="flex min-w-0 items-center gap-1.5 truncate text-caption text-muted-foreground">
              <span className="truncate">{meta.join(" · ")}</span>
              {current && (
                <>
                  <span aria-hidden>·</span>
                  <span className="shrink-0 tabular-nums">{t(($) => $.code_changes.files, { count: current.fileCount })}</span>
                  <DiffStat additions={current.additions} deletions={current.deletions} />
                </>
              )}
            </p>
          </div>
        </div>
        <div
          role="group"
          aria-label={t(($) => $.code_changes.scope_label)}
          className="flex items-center gap-0.5 rounded-lg bg-secondary/60 p-0.5"
          style={NO_DRAG}
        >
          <ScopeButton selected={scope === "run"} onSelect={() => setScope("run")}>
            {runLabel}
          </ScopeButton>
          <ScopeButton selected={scope === "issue"} onSelect={() => setScope("issue")}>
            {usePR && pr
              ? t(($) => $.code_changes.scope_issue_pr, { number: pr.number })
              : t(($) => $.code_changes.scope_issue)}
            {issueTotals && (
              <DiffStat additions={issueTotals.additions} deletions={issueTotals.deletions} className="opacity-80" />
            )}
          </ScopeButton>
        </div>
        <div className="flex items-center justify-self-end gap-0.5" style={NO_DRAG}>
          <DiffLayoutToggle layout={layout} onChange={setLayout} />
          <ChromeDivider />
          {scope === "issue" && usePR && pr && (
            <ChromeButton
              label={t(($) => $.code_changes.open_pr, { number: pr.number })}
              onClick={() => openExternal(pr.html_url)}
            >
              <GitPullRequest className="size-4" />
            </ChromeButton>
          )}
          {current?.patch && (
            <>
              <ChromeButton label={t(($) => $.code_changes.copy_patch)} onClick={() => void copyPatch()}>
                <Copy className="size-4" />
              </ChromeButton>
              <ChromeButton label={t(($) => $.code_changes.download_patch)} onClick={downloadPatch}>
                <Download className="size-4" />
              </ChromeButton>
            </>
          )}
          <ChromeDivider />
          <ChromeButton label={tEditor(($) => $.attachment.close)} onClick={onClose}>
            <X className="size-4" />
          </ChromeButton>
        </div>
      </header>
      <div className="min-h-0 flex-1 px-4 pb-4">
        {loading ? (
          <div className="dark flex h-full items-center justify-center gap-2 text-body text-muted-foreground">
            <Loader2 className="size-4 animate-spin" />
            {t(($) => $.code_changes.loading)}
          </div>
        ) : failed || !current ? (
          <div className="dark flex h-full items-center justify-center text-body text-muted-foreground">
            {t(($) => $.code_changes.load_failed)}
          </div>
        ) : (
          <DiffView
            files={current.files}
            layout={layout}
            listLabel={listLabel}
            branch={current.branch || undefined}
            listFooter={
              notes.length > 0 ? (
                <div className="mt-3 space-y-1.5 rounded-md bg-secondary/50 px-3 py-2.5 text-caption text-muted-foreground">
                  {notes.map((note) => (
                    <p key={note}>{note}</p>
                  ))}
                </div>
              ) : null
            }
          />
        )}
      </div>
    </>
  );
}

function ScopeButton({
  selected,
  onSelect,
  children,
}: {
  selected: boolean;
  onSelect: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      aria-pressed={selected}
      className={cn(
        "flex h-7 items-center gap-2 whitespace-nowrap rounded-md px-3 text-label transition-colors",
        selected ? "bg-secondary text-foreground shadow-xs" : "text-muted-foreground hover:text-foreground",
      )}
      onClick={onSelect}
    >
      {children}
    </button>
  );
}
