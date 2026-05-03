"use client";

import { useState } from "react";
import { GitCommit, ExternalLink, Copy, Check, FilePlus, FileEdit, FileMinus, FileSymlink } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import { Dialog, DialogContent, DialogTitle } from "@multica/ui/components/ui/dialog";
import { Button } from "@multica/ui/components/ui/button";
import type { CommitDetails, CommitFileChange } from "@multica/core/types";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function fileStatusIcon(status: CommitFileChange["status"]) {
  switch (status) {
    case "added":
      return <FilePlus className="size-3 shrink-0 text-emerald-500" />;
    case "modified":
      return <FileEdit className="size-3 shrink-0 text-amber-500" />;
    case "deleted":
      return <FileMinus className="size-3 shrink-0 text-red-500" />;
    case "renamed":
      return <FileSymlink className="size-3 shrink-0 text-blue-500" />;
  }
}

function shortenPath(p: string): string {
  const parts = p.split("/");
  if (parts.length <= 3) return p;
  return "\u2026/" + parts.slice(-2).join("/");
}

export function parseCommitDetails(details?: Record<string, unknown>): CommitDetails | null {
  if (!details) return null;
  const sha = details.sha as string | undefined;
  if (!sha) return null;
  return {
    sha,
    short_sha: (details.short_sha as string) ?? sha.slice(0, 7),
    message: (details.message as string) ?? "",
    url: details.url as string | undefined,
    branch: details.branch as string | undefined,
    repo: details.repo as string | undefined,
    author_name: details.author_name as string | undefined,
    author_email: details.author_email as string | undefined,
    committed_at: details.committed_at as string | undefined,
    files: (details.files as CommitFileChange[]) ?? undefined,
    total_additions: details.total_additions as number | undefined,
    total_deletions: details.total_deletions as number | undefined,
    total_files: details.total_files as number | undefined,
    diff: details.diff as string | undefined,
  };
}

// ---------------------------------------------------------------------------
// CommitDiffDialog — two-pane GitHub-style diff viewer
// ---------------------------------------------------------------------------

interface CommitDiffDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  commit: CommitDetails;
  diffSnapshotMode?: boolean;
}

export function CommitDiffDialog({ open, onOpenChange, commit, diffSnapshotMode = true }: CommitDiffDialogProps) {
  const [copied, setCopied] = useState(false);
  const [selectedFile, setSelectedFile] = useState(0);
  const [loading, setLoading] = useState(false);
  const [fetchError, setFetchError] = useState<string | null>(null);
  const [dynamicDiff, setDynamicDiff] = useState<string | null>(null);

  const handleCopySha = async () => {
    await navigator.clipboard.writeText(commit.sha);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  // Fetch diff on-demand when in dynamic mode and no stored diff
  const effectiveDiff = diffSnapshotMode ? commit.diff : (dynamicDiff ?? commit.diff);
  const files = commit.files ?? [];
  const hasDiff = !!effectiveDiff && effectiveDiff.length > 0;

  // Reset state when dialog opens
  const handleOpenChange = (open: boolean) => {
    if (open && !diffSnapshotMode && !commit.diff && !dynamicDiff) {
      // Need to fetch
      fetchDiffFromRepo();
    }
    onOpenChange(open);
  };

  const fetchDiffFromRepo = async () => {
    if (!commit.url) {
      setFetchError("No commit URL available to fetch diff from.");
      return;
    }
    setLoading(true);
    setFetchError(null);
    try {
      // Extract owner/repo from GitHub URL: https://github.com/owner/repo/commit/sha
      const match = commit.url.match(/github\.com\/([^/]+)\/([^/]+)\/commit/);
      if (!match) {
        setFetchError("Could not parse repository URL. Only GitHub is supported for dynamic diff fetching.");
        return;
      }
      const [, owner, repo] = match;
      const apiUrl = `https://api.github.com/repos/${owner}/${repo}/commits/${commit.sha}`;
      const res = await fetch(apiUrl, {
        headers: { Accept: "application/vnd.github.v3.diff" },
      });
      if (!res.ok) {
        if (res.status === 404) {
          setFetchError("Commit not found. The branch or repository may have been deleted.");
        } else {
          setFetchError(`Failed to fetch diff: ${res.status} ${res.statusText}`);
        }
        return;
      }
      const text = await res.text();
      setDynamicDiff(text);
    } catch (e) {
      setFetchError(e instanceof Error ? e.message : "Failed to fetch diff from repository.");
    } finally {
      setLoading(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent
        className="w-[75vw] h-[75vh] max-w-none sm:max-w-none max-h-[75vh] overflow-hidden p-0 !flex flex-col gap-0"
        showCloseButton
      >
        <DialogTitle className="sr-only">Commit {commit.short_sha}</DialogTitle>

        {/* Header */}
        <div className="border-b border-border px-5 py-3 shrink-0">
          <div className="flex items-start gap-3">
            <div className="rounded-md border bg-muted/50 p-2 text-muted-foreground">
              <GitCommit className="h-4 w-4" />
            </div>
            <div className="min-w-0 flex-1">
              <p className="text-sm font-medium leading-snug">{commit.message}</p>
              <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
                <button
                  type="button"
                  className="inline-flex items-center gap-1 rounded px-1 py-0.5 font-mono hover:bg-muted transition-colors"
                  onClick={handleCopySha}
                >
                  {copied ? <Check className="size-3" /> : <Copy className="size-3" />}
                  {commit.short_sha}
                </button>
                {commit.branch && <span>{commit.branch}</span>}
                {commit.repo && <span>{commit.repo}</span>}
                {commit.author_name && <span>{commit.author_name}</span>}
                {commit.committed_at && (
                  <Tooltip>
                    <TooltipTrigger
                      render={<span className="cursor-default">{new Date(commit.committed_at).toLocaleString()}</span>}
                    />
                    <TooltipContent side="top">
                      {new Date(commit.committed_at).toLocaleString()}
                    </TooltipContent>
                  </Tooltip>
                )}
                {commit.total_files != null && (
                  <span className="text-muted-foreground">
                    {commit.total_files} {commit.total_files === 1 ? "file" : "files"}
                  </span>
                )}
                {commit.total_additions != null && commit.total_additions > 0 && (
                  <span className="text-emerald-600 dark:text-emerald-400">
                    +{commit.total_additions}
                  </span>
                )}
                {commit.total_deletions != null && commit.total_deletions > 0 && (
                  <span className="text-red-600 dark:text-red-400">
                    -{commit.total_deletions}
                  </span>
                )}
                {commit.url && (
                  <a
                    href={commit.url}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="inline-flex items-center gap-1 text-brand hover:underline"
                  >
                    <ExternalLink className="size-3" />
                    View on GitHub
                  </a>
                )}
              </div>
            </div>
          </div>
        </div>

        {/* Two-pane body */}
        <div className="flex flex-1 overflow-hidden">
          {loading ? (
            <div className="flex-1 flex items-center justify-center text-sm text-muted-foreground">
              Fetching diff from repository...
            </div>
          ) : fetchError ? (
            <div className="flex-1 flex flex-col items-center justify-center gap-3 text-sm">
              <p className="text-destructive">{fetchError}</p>
              <Button variant="outline" size="sm" onClick={fetchDiffFromRepo}>
                Retry
              </Button>
            </div>
          ) : (
            <>
          {/* Left: file tree */}
          <div className="w-64 shrink-0 border-r border-border overflow-y-auto bg-muted/20">
            <div className="p-2">
              <p className="px-2 py-1 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
                Files changed
              </p>
              {files.map((file, idx) => (
                <button
                  key={file.path + idx}
                  type="button"
                  className={cn(
                    "flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-xs hover:bg-accent/40 transition-colors text-left",
                    selectedFile === idx && "bg-accent",
                  )}
                  onClick={() => setSelectedFile(idx)}
                >
                  {fileStatusIcon(file.status)}
                  <span className="min-w-0 flex-1 truncate font-mono text-foreground/80">
                    {shortenPath(file.path)}
                  </span>
                  <span className="shrink-0 text-[11px] text-muted-foreground">
                    {file.additions > 0 && <span className="text-emerald-600 dark:text-emerald-400">+{file.additions}</span>}
                    {file.deletions > 0 && <span className="ml-1 text-red-600 dark:text-red-400">-{file.deletions}</span>}
                  </span>
                </button>
              ))}
            </div>
          </div>

          {/* Right: diff viewer */}
          <div className="flex-1 overflow-y-auto">
            {files.length > 0 && files[selectedFile] ? (
              <DiffFileView
                file={files[selectedFile]}
                diff={hasDiff ? extractFileDiff(effectiveDiff!, files[selectedFile]!.path) : undefined}
              />
            ) : hasDiff ? (
              <pre className="p-4 text-[11px] font-mono text-muted-foreground whitespace-pre-wrap break-all">
                {effectiveDiff}
              </pre>
            ) : (
              <p className="p-4 text-sm text-muted-foreground">No file details available.</p>
            )}
          </div>
            </>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}

// ---------------------------------------------------------------------------
// DiffFileView — renders a single file's diff with unified/split toggle
// ---------------------------------------------------------------------------

function DiffFileView({
  file,
  diff,
}: {
  file: CommitFileChange;
  diff?: string;
}) {
  const [viewMode, setViewMode] = useState<"unified" | "split">("unified");

  const contentLines = diff ? parseDiffLines(diff) : [];

  return (
    <div>
      {/* File header */}
      <div className="sticky top-0 z-10 border-b border-border bg-background px-4 py-2 flex items-center gap-2">
        {fileStatusIcon(file.status)}
        <span className="font-mono text-sm text-foreground">{file.path}</span>
        <span className="ml-auto text-[11px] text-muted-foreground">
          {file.additions > 0 && <span className="text-emerald-600 dark:text-emerald-400">+{file.additions}</span>}
          {file.deletions > 0 && <span className="ml-2 text-red-600 dark:text-red-400">-{file.deletions}</span>}
        </span>
        <div className="ml-4 flex items-center border border-border rounded-md overflow-hidden">
          <button
            type="button"
            className={cn(
              "px-2 py-0.5 text-[11px] font-medium transition-colors",
              viewMode === "unified" ? "bg-accent text-foreground" : "text-muted-foreground hover:text-foreground",
            )}
            onClick={() => setViewMode("unified")}
          >
            Unified
          </button>
          <div className="w-px h-4 bg-border" />
          <button
            type="button"
            className={cn(
              "px-2 py-0.5 text-[11px] font-medium transition-colors",
              viewMode === "split" ? "bg-accent text-foreground" : "text-muted-foreground hover:text-foreground",
            )}
            onClick={() => setViewMode("split")}
          >
            Split
          </button>
        </div>
      </div>

      {/* Diff content */}
      {contentLines.length > 0 ? (
        viewMode === "unified" ? (
          <UnifiedDiffView lines={contentLines} />
        ) : (
          <SplitDiffView lines={contentLines} />
        )
      ) : (
        <p className="p-4 text-sm text-muted-foreground">No diff available for this file.</p>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// parseDiffLines — strip git metadata, return only content lines
// ---------------------------------------------------------------------------

interface DiffLine {
  type: "add" | "del" | "ctx" | "hunk";
  text: string;
  oldNum?: number;
  newNum?: number;
}

function parseDiffLines(diff: string): DiffLine[] {
  const lines = diff.split("\n");
  const result: DiffLine[] = [];
  let oldLine = 0;
  let newLine = 0;

  for (const line of lines) {
    // Skip git metadata
    if (line.startsWith("diff ") || line.startsWith("index ") || line.startsWith("new file") || line.startsWith("deleted file") || line.startsWith("rename")) continue;
    if (line.startsWith("---") || line.startsWith("+++")) continue;

    // Parse hunk header: @@ -oldStart,oldCount +newStart,newCount @@
    if (line.startsWith("@@")) {
      const match = line.match(/@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/);
      if (match) {
        oldLine = parseInt(match[1], 10);
        newLine = parseInt(match[2], 10);
      }
      result.push({ type: "hunk", text: line });
      continue;
    }

    if (line.startsWith("+")) {
      result.push({ type: "add", text: line, newNum: newLine });
      newLine++;
    } else if (line.startsWith("-")) {
      result.push({ type: "del", text: line, oldNum: oldLine });
      oldLine++;
    } else {
      result.push({ type: "ctx", text: line, oldNum: oldLine, newNum: newLine });
      oldLine++;
      newLine++;
    }
  }

  return result;
}

// ---------------------------------------------------------------------------
// UnifiedDiffView — standard unified diff
// ---------------------------------------------------------------------------

function UnifiedDiffView({ lines }: { lines: DiffLine[] }) {
  return (
    <div className="text-[11px] font-mono leading-relaxed">
      {lines.map((line, i) => {
        if (line.type === "hunk") {
          return <div key={i} className="text-blue-600 dark:text-blue-400 font-semibold px-4 py-0.5 bg-muted/30">{line.text}</div>;
        }
        if (line.type === "add") {
          return <div key={i} className="bg-emerald-500/10 text-emerald-700 dark:text-emerald-400 px-4 py-0.5">{line.text}</div>;
        }
        if (line.type === "del") {
          return <div key={i} className="bg-red-500/10 text-red-700 dark:text-red-400 px-4 py-0.5">{line.text}</div>;
        }
        return <div key={i} className="text-muted-foreground/70 px-4 py-0.5">{line.text}</div>;
      })}
    </div>
  );
}

// ---------------------------------------------------------------------------
// SplitDiffView — side-by-side diff
// ---------------------------------------------------------------------------

function SplitDiffView({ lines }: { lines: DiffLine[] }) {
  // Group lines into paired add/del blocks for side-by-side alignment
  const rows: { left: DiffLine | null; right: DiffLine | null }[] = [];
  let i = 0;
  while (i < lines.length) {
    const line = lines[i]!;
    if (line.type === "hunk") {
      rows.push({ left: line, right: null });
      i++;
    } else if (line.type === "ctx") {
      rows.push({ left: line, right: line });
      i++;
    } else if (line.type === "del") {
      // Collect consecutive dels
      const dels: DiffLine[] = [];
      while (i < lines.length && lines[i]!.type === "del") {
        dels.push(lines[i]!);
        i++;
      }
      // Collect consecutive adds that follow
      const adds: DiffLine[] = [];
      while (i < lines.length && lines[i]!.type === "add") {
        adds.push(lines[i]!);
        i++;
      }
      // Pair them up
      const maxLen = Math.max(dels.length, adds.length);
      for (let j = 0; j < maxLen; j++) {
        rows.push({
          left: j < dels.length ? dels[j]! : null,
          right: j < adds.length ? adds[j]! : null,
        });
      }
    } else if (line.type === "add") {
      // Standalone adds (no preceding del)
      rows.push({ left: null, right: line });
      i++;
    }
  }

  return (
    <div className="text-[11px] font-mono leading-relaxed">
      {rows.map((row, i) => {
        if (row.left?.type === "hunk") {
          return (
            <div key={i} className="grid grid-cols-2 border-b border-border bg-muted/30 text-blue-600 dark:text-blue-400 font-semibold">
              <div className="px-4 py-0.5">{row.left.text}</div>
              <div />
            </div>
          );
        }
        const leftBg = row.left?.type === "del" ? "bg-red-500/10 text-red-700 dark:text-red-400" : row.left?.type === "ctx" ? "text-muted-foreground/70" : "bg-muted/20";
        const rightBg = row.right?.type === "add" ? "bg-emerald-500/10 text-emerald-700 dark:text-emerald-400" : row.right?.type === "ctx" ? "text-muted-foreground/70" : "bg-muted/20";

        return (
          <div key={i} className="grid grid-cols-2 border-b border-border/30">
            <div className={cn("px-4 py-0.5 min-h-[1.25rem]", leftBg)}>
              {row.left ? row.left.text : "\u00A0"}
            </div>
            <div className={cn("px-4 py-0.5 min-h-[1.25rem]", rightBg)}>
              {row.right ? row.right.text : "\u00A0"}
            </div>
          </div>
        );
      })}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Extract a single file's diff from a unified diff string
// ---------------------------------------------------------------------------

function extractFileDiff(fullDiff: string, filePath: string): string | undefined {
  const lines = fullDiff.split("\n");
  let start = -1;
  for (let i = 0; i < lines.length; i++) {
    if (lines[i]!.includes(filePath) && (lines[i]!.startsWith("+++") || lines[i]!.startsWith("---") || lines[i]!.startsWith("diff --git"))) {
      start = i;
      break;
    }
  }
  if (start === -1) return undefined;

  // Find the end: next "diff --git" line or end of string
  let end = lines.length;
  for (let i = start + 1; i < lines.length; i++) {
    if (lines[i]!.startsWith("diff --git")) {
      end = i;
      break;
    }
  }

  return lines.slice(start, end).join("\n");
}

// ---------------------------------------------------------------------------
// CommitCard — compact inline card for the timeline
// ---------------------------------------------------------------------------

interface CommitCardProps {
  commit: CommitDetails;
  actorName: string;
  createdAt: string;
  diffSnapshotMode?: boolean;
}

export function CommitCard({ commit, actorName, createdAt, diffSnapshotMode = true }: CommitCardProps) {
  const [dialogOpen, setDialogOpen] = useState(false);
  const files = commit.files ?? [];
  const fileCount = commit.total_files ?? files.length;
  const additions = commit.total_additions ?? files.reduce((s, f) => s + f.additions, 0);
  const deletions = commit.total_deletions ?? files.reduce((s, f) => s + f.deletions, 0);

  return (
    <>
      <div className="rounded-lg border bg-muted/20 px-3 py-2.5">
        <div className="flex items-center gap-2">
          <div className="rounded-md border bg-muted/50 p-1 text-muted-foreground">
            <GitCommit className="h-3.5 w-3.5" />
          </div>
          <div className="min-w-0 flex-1">
            <p className="truncate text-sm font-medium">{commit.message}</p>
            <div className="mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-[11px] text-muted-foreground">
              <span>{actorName}</span>
              <span>committed</span>
              <button
                type="button"
                className="inline-flex items-center gap-0.5 rounded px-1 py-0.5 font-mono hover:bg-muted transition-colors"
                onClick={() => setDialogOpen(true)}
              >
                {commit.short_sha}
              </button>
              {commit.branch && <span className="truncate">{commit.branch}</span>}
              <span>{timeAgo(createdAt)}</span>
            </div>
          </div>

          {/* Stats + view button */}
          <div className="flex shrink-0 items-center gap-2">
            {(additions > 0 || deletions > 0) && (
              <div className="flex items-center gap-1 text-[11px]">
                {additions > 0 && <span className="text-emerald-600 dark:text-emerald-400">+{additions}</span>}
                {deletions > 0 && <span className="text-red-600 dark:text-red-400">-{deletions}</span>}
              </div>
            )}
            {fileCount > 0 && (
              <span className="text-[11px] text-muted-foreground">
                {fileCount} {fileCount === 1 ? "file" : "files"}
              </span>
            )}
            <Button
              variant="ghost"
              size="sm"
              className="h-6 px-2 text-xs"
              onClick={() => setDialogOpen(true)}
            >
              View diff
            </Button>
          </div>
        </div>
      </div>

      <CommitDiffDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        commit={commit}
        diffSnapshotMode={diffSnapshotMode}
      />
    </>
  );
}

// ---------------------------------------------------------------------------
// Compact timeAgo (avoids extra import)
// ---------------------------------------------------------------------------

function timeAgo(dateStr: string): string {
  const diff = Date.now() - new Date(dateStr).getTime();
  const minutes = Math.floor(diff / 60000);
  if (minutes < 1) return "just now";
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  if (days < 7) return `${days}d ago`;
  return new Date(dateStr).toLocaleDateString();
}
