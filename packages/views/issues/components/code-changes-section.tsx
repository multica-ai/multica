"use client";

import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight, X } from "lucide-react";
import type { CodeChangeFile } from "@multica/core/api";
import { issueCodeChangesOptions } from "@multica/core/code-changes";
import {
  ResizableHandle,
  ResizablePanel,
  ResizablePanelGroup,
} from "@multica/ui/components/ui/resizable";
import { useT } from "../../i18n";

/**
 * "Code changes" in the issue sidebar (local_directory projects): the issue's
 * latest task branch diffed against the repository default branch.
 *
 * The sidebar keeps a compact summary — a file list with +/- counts. Clicking
 * a file opens a GitHub-style "files changed" drawer on the right: it starts
 * at half the window width and its overall width can be dragged wider/narrower
 * at its left edge; inside, a resizable file tree on the left (drag the
 * separator) and a line-numbered, red/green unified diff on the right. The
 * backend reports available=false when the deployment has no host repo mount
 * or the issue has no local repo/task branch; the section hides itself in
 * those cases.
 */
export function CodeChangesSection({
  issueId,
  open,
  onOpenChange,
}: {
  issueId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT("issues");
  const { data } = useQuery(issueCodeChangesOptions(issueId));
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [selectedPath, setSelectedPath] = useState<string | null>(null);

  if (!data?.available || data.files.length === 0) return null;
  const count = data.files.length;
  const selected = data.files.find((f) => f.path === selectedPath) ?? data.files[0]!;

  const openDrawer = (path: string) => {
    setSelectedPath(path);
    setDrawerOpen(true);
  };

  return (
    <>
      <div>
        <div className="mb-2 flex w-full items-center gap-0.5">
          <button
            type="button"
            aria-expanded={open}
            className={`flex min-w-0 flex-1 items-center gap-1 rounded-md px-2 py-1 text-caption font-medium transition-colors hover:bg-accent/70 ${open ? "" : "text-muted-foreground hover:text-foreground"}`}
            onClick={() => onOpenChange(!open)}
          >
            <span className="truncate">{t(($) => $.detail.section_code_changes)}</span>
            <span className="shrink-0 rounded-xs bg-muted px-1 text-micro font-medium tabular-nums text-muted-foreground">
              {count}
            </span>
            <ChevronRight
              className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${open ? "rotate-90" : ""}`}
            />
          </button>
        </div>
        {open && (
          <div className="pl-2">
            <div className="mb-1 truncate px-1 text-micro text-muted-foreground" title={data.summary}>
              {data.summary}
              <span className="mx-1">·</span>
              {data.base_ref}
              <span className="mx-1 text-muted-foreground/60">→</span>
              {data.head_ref}
            </div>
            <ul className="space-y-px">
              {data.files.map((file) => (
                <FileRow key={file.path} file={file} onClick={() => openDrawer(file.path)} />
              ))}
            </ul>
          </div>
        )}
      </div>
      <CodeChangesDrawer
        files={data.files}
        selected={selected}
        summary={data.summary}
        repoPath={data.repo_path}
        baseRef={data.base_ref}
        headRef={data.head_ref}
        open={drawerOpen}
        onSelect={(path) => setSelectedPath(path)}
        onClose={() => setDrawerOpen(false)}
      />
    </>
  );
}

function FileRow({ file, onClick }: { file: CodeChangeFile; onClick: () => void }) {
  return (
    <li>
      <button
        type="button"
        className="group flex w-[calc(100%+1rem)] -mx-2 items-center gap-2 rounded-md px-2 py-1 text-left text-caption transition-colors hover:bg-accent/50"
        onClick={onClick}
        title={`${file.path} — view full diff`}
      >
        <StatusBadge status={file.status} />
        <span className="min-w-0 flex-1 truncate">{file.path}</span>
        <span className="shrink-0 font-mono text-micro tabular-nums">
          {file.additions > 0 ? (
            <span className="text-green-600">+{file.additions}</span>
          ) : null}
          {file.deletions > 0 ? (
            <span className="ml-1 text-red-600">−{file.deletions}</span>
          ) : null}
        </span>
      </button>
    </li>
  );
}

function StatusBadge({ status }: { status: string }) {
  const classes: Record<string, string> = {
    A: "bg-green-600/10 text-green-700 ring-green-600/30",
    M: "bg-amber-600/10 text-amber-700 ring-amber-600/30",
    D: "bg-red-600/10 text-red-700 ring-red-600/30",
  };
  const cls = classes[status] ?? classes.M;
  return (
    <span className={`shrink-0 rounded-xs px-1 text-micro font-bold ring-1 ${cls}`}>
      {status}
    </span>
  );
}

/**
 * GitHub-style "files changed" drawer anchored to the right edge. Opens at
 * half the window width; drag its left edge to resize (360px … 92vw). Esc or
 * the close button dismisses it; the backdrop click also closes.
 */
function CodeChangesDrawer({
  files,
  selected,
  summary,
  repoPath,
  baseRef,
  headRef,
  open,
  onSelect,
  onClose,
}: {
  files: CodeChangeFile[];
  selected: CodeChangeFile;
  summary: string;
  repoPath: string;
  baseRef: string;
  headRef: string;
  open: boolean;
  onSelect: (path: string) => void;
  onClose: () => void;
}) {
  const { t } = useT("issues");
  const [width, setWidth] = useState<number | null>(null);
  const widthRef = useRef<number | null>(null);

  useEffect(() => {
    if (!open) return;
    const w = Math.round(window.innerWidth / 2);
    widthRef.current = w;
    setWidth(w);
  }, [open]);

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, onClose]);

  if (!open) return null;

  const startDrag = (e: React.PointerEvent<HTMLDivElement>) => {
    e.preventDefault();
    const startX = e.clientX;
    const startW = widthRef.current ?? Math.round(window.innerWidth / 2);
    const onMove = (ev: PointerEvent) => {
      const w = Math.max(360, Math.min(window.innerWidth * 0.92, startW + (startX - ev.clientX)));
      widthRef.current = w;
      setWidth(w);
    };
    const onUp = () => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
    };
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
  };

  return (
    <>
      <div className="fixed inset-0 z-50 bg-black/20" onClick={onClose} />
      <div
        className="fixed inset-y-0 right-0 z-50 flex flex-col bg-surface-raised shadow-[var(--floating-shadow)]"
        style={{ width: width ?? "50vw" }}
      >
        {/* Drag handle on the drawer's left edge */}
        <div
          role="separator"
          aria-orientation="vertical"
          aria-label="Resize panel"
          onPointerDown={startDrag}
          className="absolute top-0 left-0 h-full w-1.5 cursor-ew-resize bg-transparent transition-colors hover:bg-foreground/15"
        />
        {/* Header */}
        <div className="flex h-12 shrink-0 items-center gap-3 border-b border-border pl-4 pr-3">
          <span className="font-heading text-title-sm font-medium text-foreground">
            {t(($) => $.detail.section_code_changes)}
          </span>
          <span className="min-w-0 flex-1 truncate text-caption text-muted-foreground">
            {repoPath}
            <span className="mx-1.5">·</span>
            {summary}
            <span className="mx-1.5">·</span>
            {baseRef}
            <span className="mx-1 text-muted-foreground/70">→</span>
            {headRef}
          </span>
          <button
            type="button"
            aria-label="Close"
            onClick={onClose}
            className="shrink-0 rounded-md p-1.5 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
          >
            <X className="size-4" />
          </button>
        </div>

        <ResizablePanelGroup orientation="horizontal" className="min-h-0 flex-1">
          {/* File tree */}
          <ResizablePanel
            defaultSize="280px"
            minSize="200px"
            maxSize="640px"
            className="overflow-auto border-r border-border"
          >
            <div className="sticky top-0 z-10 border-b border-border bg-surface-raised px-3 py-2 text-micro text-muted-foreground">
              {files.length} {files.length === 1 ? "file" : "files"}
            </div>
            <ul className="p-1.5">
              {files.map((file) => {
                const active = file.path === selected.path;
                return (
                  <li key={file.path}>
                    <button
                      type="button"
                      onClick={() => onSelect(file.path)}
                      className={`flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-caption transition-colors ${
                        active
                          ? "bg-accent text-foreground"
                          : "text-muted-foreground hover:bg-accent/60 hover:text-foreground"
                      }`}
                      title={file.path}
                    >
                      <StatusBadge status={file.status} />
                      <span className="min-w-0 flex-1 truncate">{file.path}</span>
                      <span className="shrink-0 font-mono text-micro tabular-nums">
                        {file.additions > 0 ? (
                          <span className="text-green-600">+{file.additions}</span>
                        ) : null}
                        {file.deletions > 0 ? (
                          <span className="ml-1 text-red-600">−{file.deletions}</span>
                        ) : null}
                      </span>
                    </button>
                  </li>
                );
              })}
            </ul>
          </ResizablePanel>

          <ResizableHandle withHandle />

          {/* Diff */}
          <ResizablePanel className="min-w-0">
            <div className="flex min-h-0 h-full flex-col">
              <div className="flex shrink-0 items-center gap-2 border-b border-border px-4 py-2">
                <StatusBadge status={selected.status} />
                <span className="truncate text-caption font-medium text-foreground">
                  {selected.path}
                </span>
                <span className="shrink-0 font-mono text-micro tabular-nums text-muted-foreground">
                  {selected.additions > 0 ? (
                    <span className="text-green-600">+{selected.additions}</span>
                  ) : null}
                  {selected.deletions > 0 ? (
                    <span className="ml-1 text-red-600">−{selected.deletions}</span>
                  ) : null}
                </span>
                {selected.truncated ? (
                  <span className="shrink-0 rounded-xs bg-amber-600/10 px-1 text-micro text-amber-700">
                    diff truncated
                  </span>
                ) : null}
              </div>
              <div className="min-h-0 flex-1 overflow-auto">
                {selected.diff ? (
                  <DiffView diff={selected.diff} />
                ) : (
                  <p className="p-4 text-caption text-muted-foreground">
                    No diff available for this file.
                  </p>
                )}
              </div>
            </div>
          </ResizablePanel>
        </ResizablePanelGroup>
      </div>
    </>
  );
}

type ParsedLine =
  | { kind: "hunk"; text: string }
  | { kind: "add" | "del" | "ctx"; text: string; oldNo?: number; newNo?: number };

/**
 * Splits a unified diff into display rows with GitHub-style line numbers.
 * Hunk headers reset the counters; context lines advance both, additions only
 * the new-side counter, deletions only the old-side counter. File header lines
 * (`diff --git`, `index`, `---`, `+++`) are skipped.
 */
function parseDiff(diff: string): ParsedLine[] {
  const out: ParsedLine[] = [];
  let oldNo = 0;
  let newNo = 0;
  for (const raw of diff.split("\n")) {
    if (raw.startsWith("@@")) {
      const m = /@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/.exec(raw);
      if (m) {
        oldNo = Number(m[1]);
        newNo = Number(m[2]);
      }
      out.push({ kind: "hunk", text: raw });
      continue;
    }
    if (
      raw.startsWith("diff --git ") ||
      raw.startsWith("index ") ||
      raw === "---" ||
      raw === "+++" ||
      raw.startsWith("--- ") ||
      raw.startsWith("+++ ")
    ) {
      continue;
    }
    if (raw.startsWith("+")) {
      newNo += 1;
      out.push({ kind: "add", text: raw.slice(1), newNo });
      continue;
    }
    if (raw.startsWith("-")) {
      oldNo += 1;
      out.push({ kind: "del", text: raw.slice(1), oldNo });
      continue;
    }
    if (raw.startsWith(" ")) {
      oldNo += 1;
      newNo += 1;
      out.push({ kind: "ctx", text: raw.slice(1), oldNo, newNo });
      continue;
    }
    out.push({ kind: "ctx", text: raw });
  }
  return out;
}

function DiffView({ diff }: { diff: string }) {
  const lines = parseDiff(diff);
  return (
    <table className="w-full border-collapse font-mono text-caption leading-5">
      <tbody>
        {lines.map((line, i) => (
          <DiffRow key={i} line={line} />
        ))}
      </tbody>
    </table>
  );
}

function DiffRow({ line }: { line: ParsedLine }) {
  if (line.kind === "hunk") {
    return (
      <tr className="bg-blue-500/10">
        <td className="px-2 text-right text-micro tabular-nums text-blue-700/60 select-none" />
        <td className="px-2 text-right text-micro tabular-nums text-blue-700/60 select-none" />
        <td className="whitespace-pre-wrap break-all px-3 py-px text-blue-700">
          {line.text}
        </td>
      </tr>
    );
  }
  const isAdd = line.kind === "add";
  const isDel = line.kind === "del";
  return (
    <tr
      className={
        isAdd
          ? "bg-green-500/10"
          : isDel
            ? "bg-red-500/10"
            : "hover:bg-accent/40"
      }
    >
      <td className="w-10 px-2 text-right text-micro tabular-nums text-muted-foreground/60 select-none">
        {line.kind === "del" || line.kind === "ctx" ? line.oldNo ?? "" : ""}
      </td>
      <td className="w-10 px-2 text-right text-micro tabular-nums text-muted-foreground/60 select-none">
        {line.kind === "add" || line.kind === "ctx" ? line.newNo ?? "" : ""}
      </td>
      <td
        className={`whitespace-pre-wrap break-all px-3 py-px ${
          isAdd ? "text-green-800" : isDel ? "text-red-800" : "text-foreground"
        }`}
      >
        {line.text || " "}
      </td>
    </tr>
  );
}
