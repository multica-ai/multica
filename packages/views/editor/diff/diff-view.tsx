"use client";

/**
 * DiffView — the diff viewer's body (MUL-7651): changed files on the left,
 * grouped by directory with their status and line counts, and the selected
 * file's diff on the right, unified or side by side.
 *
 * Shared by the code change viewer (a run's changes, the whole issue's) and
 * the attachment viewer's `diff` kind (an uploaded .patch). It fills the
 * viewer's stage; the host owns the frame, including the layout toggle.
 */

import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { Check, Copy, FileCode, Folder, GitBranch, Search } from "lucide-react";
import { toast } from "sonner";
import {
  groupFilesByDirectory,
  type DiffFileView,
} from "@multica/core/code-changes";
import type { CodeChangeFileStatus } from "@multica/core/types";
import { copyText } from "@multica/ui/lib/clipboard";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import type { DiffLayout } from "../viewer-chrome";
import { DiffFileBody } from "./diff-file-body";

const STATUS_LETTER: Record<CodeChangeFileStatus, string> = {
  added: "A",
  modified: "M",
  deleted: "D",
  renamed: "R",
  copied: "C",
  type_changed: "T",
};

const STATUS_COLOR: Record<CodeChangeFileStatus, string> = {
  added: "text-success",
  modified: "text-warning",
  deleted: "text-destructive",
  renamed: "text-info",
  copied: "text-info",
  type_changed: "text-warning",
};

function isTypingTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  if (target.isContentEditable) return true;
  return target.closest("input, textarea, select") !== null;
}

export function DiffView({
  files,
  layout,
  listLabel,
  branch,
  notice,
  listFooter,
}: {
  files: DiffFileView[];
  layout: DiffLayout;
  /** Heading over the file list, e.g. "This run · 3 files". */
  listLabel: string;
  branch?: string;
  /** Replaces the diff pane when there is nothing to show per file. */
  notice?: ReactNode;
  /** Under the file list: a pointer to the other scope, a truncation note. */
  listFooter?: ReactNode;
}) {
  const { t } = useT("editor");
  const [filter, setFilter] = useState("");
  const [selectedPath, setSelectedPath] = useState<string | null>(null);
  const paneRef = useRef<HTMLDivElement>(null);

  const visible = useMemo(() => {
    const query = filter.trim().toLowerCase();
    return query ? files.filter((f) => f.path.toLowerCase().includes(query)) : files;
  }, [files, filter]);
  const groups = useMemo(() => groupFilesByDirectory(visible), [visible]);
  // List order, which is directory order — the order j / k walk.
  const ordered = useMemo(() => groups.flatMap((g) => g.files), [groups]);

  const selected =
    files.find((f) => f.path === selectedPath) ??
    // First file with something to read, so a lone binary does not open first.
    ordered.find((f) => f.hunks && f.hunks.length > 0) ??
    ordered[0] ??
    null;

  // A new scope (other files) starts from its own first file, at the top.
  useEffect(() => {
    setSelectedPath(null);
    setFilter("");
  }, [files]);
  useEffect(() => {
    if (paneRef.current) paneRef.current.scrollTop = 0;
  }, [selected?.path, layout]);

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey || e.repeat || isTypingTarget(e.target)) return;
      if (e.key !== "j" && e.key !== "k") return;
      if (ordered.length === 0) return;
      const at = selected ? ordered.indexOf(selected) : -1;
      const next = e.key === "j" ? Math.min(at + 1, ordered.length - 1) : Math.max(at - 1, 0);
      const target = ordered[next];
      if (!target) return;
      e.preventDefault();
      setSelectedPath(target.path);
    };
    document.addEventListener("keydown", handler);
    return () => document.removeEventListener("keydown", handler);
  }, [ordered, selected]);

  const copyPath = async () => {
    if (!selected) return;
    const ok = await copyText(selected.path);
    if (ok) toast.success(t(($) => $.diff.path_copied));
    else toast.error(t(($) => $.diff.copy_failed));
  };

  return (
    <div className="flex h-full min-h-0">
      <nav
        className="dark flex w-80 max-w-[40%] shrink-0 flex-col gap-1 overflow-y-auto pb-4 pr-3 text-foreground"
        aria-label={listLabel}
      >
        <label className="flex h-9 shrink-0 items-center gap-2 rounded-md bg-secondary/70 px-2.5 text-muted-foreground focus-within:ring-2 focus-within:ring-ring">
          <Search className="size-4 shrink-0" />
          <input
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            placeholder={t(($) => $.diff.filter_files)}
            aria-label={t(($) => $.diff.filter_files)}
            className="min-w-0 flex-1 bg-transparent text-body text-foreground outline-none placeholder:text-muted-foreground"
          />
        </label>
        {branch && (
          <div className="flex min-w-0 items-center gap-2 px-2 py-1.5 text-muted-foreground" title={branch}>
            <GitBranch className="size-3.5 shrink-0" />
            <span className="truncate font-mono text-caption">{branch}</span>
          </div>
        )}
        <p className="px-2 pb-0.5 pt-2 text-caption text-muted-foreground">{listLabel}</p>
        {groups.map((group) => (
          <div key={group.directory || "."} className="flex flex-col">
            {group.directory && (
              <div
                className="flex min-w-0 items-center gap-2 px-2 py-1 text-caption text-muted-foreground"
                title={group.directory}
              >
                <Folder className="size-3.5 shrink-0" />
                <span className="truncate">{group.directory}</span>
              </div>
            )}
            {group.files.map((file) => {
              const name = file.path.slice(file.path.lastIndexOf("/") + 1);
              const active = file === selected;
              return (
                <button
                  key={file.path}
                  type="button"
                  aria-current={active ? "true" : undefined}
                  title={file.oldPath ? `${file.oldPath} → ${file.path}` : file.path}
                  className={cn(
                    "flex min-w-0 items-center gap-2 rounded-md py-1.5 pr-2 text-left text-body transition-colors",
                    group.directory ? "pl-7" : "pl-2",
                    active
                      ? "bg-secondary text-foreground"
                      : "text-foreground/85 hover:bg-secondary/60 hover:text-foreground",
                  )}
                  onClick={() => setSelectedPath(file.path)}
                >
                  <FileCode className="size-3.5 shrink-0 text-muted-foreground" />
                  <span className="min-w-0 flex-1 truncate">{name}</span>
                  <DiffStat additions={file.additions} deletions={file.deletions} className="text-caption" />
                  <span className={cn("w-3 shrink-0 text-center text-caption font-medium", STATUS_COLOR[file.status])}>
                    {STATUS_LETTER[file.status]}
                  </span>
                </button>
              );
            })}
          </div>
        ))}
        {visible.length === 0 && (
          <p className="px-2 py-3 text-caption text-muted-foreground">{t(($) => $.diff.no_matching_files)}</p>
        )}
        {listFooter}
      </nav>
      <div ref={paneRef} className="min-h-0 min-w-0 flex-1 overflow-y-auto">
        <div className="min-h-full rounded-t-lg bg-background text-foreground">
          {notice ? (
            <div className="px-6 py-16 text-center text-body text-muted-foreground">{notice}</div>
          ) : selected ? (
            <>
              <div className="sticky top-0 z-10 flex min-w-0 items-center gap-3 rounded-t-lg border-b border-border bg-background px-4 py-3">
                <span className="min-w-0 flex-1 truncate text-body" title={selected.path}>
                  {selected.oldPath && (
                    <span className="text-muted-foreground">{selected.oldPath} → </span>
                  )}
                  <span className="text-muted-foreground">
                    {selected.path.slice(0, selected.path.lastIndexOf("/") + 1)}
                  </span>
                  <span className="font-medium">{selected.path.slice(selected.path.lastIndexOf("/") + 1)}</span>
                </span>
                <DiffStat additions={selected.additions} deletions={selected.deletions} className="text-body" />
                <button
                  type="button"
                  className="flex size-7 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                  title={t(($) => $.diff.copy_path)}
                  aria-label={t(($) => $.diff.copy_path)}
                  onClick={() => void copyPath()}
                >
                  <Copy className="size-3.5" />
                </button>
              </div>
              <DiffFileBody key={selected.path} file={selected} layout={layout} />
            </>
          ) : (
            <div className="px-6 py-16 text-center text-body text-muted-foreground">
              <Check className="mx-auto mb-2 size-5" />
              {t(($) => $.diff.no_changes)}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

/** "+12 −3" in the diff colors. */
export function DiffStat({
  additions,
  deletions,
  className,
}: {
  additions: number;
  deletions: number;
  className?: string;
}) {
  return (
    <span className={cn("inline-flex shrink-0 items-center gap-1.5 tabular-nums", className)}>
      <span className="text-success">+{additions}</span>
      <span className="text-destructive">−{deletions}</span>
    </span>
  );
}
