/**
 * What the diff viewer renders, derived from a patch and (when the server
 * stored one) its file list (MUL-7651). Pure; shared with mobile.
 */

import type { AgentTask } from "../types/agent";
import type { CodeChangeFile, CodeChangeFileStatus, TaskCodeChange } from "../types/code-change";
import type { GitHubPullRequest } from "../types/github";
import type { DiffHunk, DiffLine, ParsedDiffFile } from "./parse-patch";

export interface DiffFileView {
  path: string;
  oldPath: string | null;
  status: CodeChangeFileStatus;
  additions: number;
  deletions: number;
  binary: boolean;
  /** null when the patch does not carry this file (too large, or not diffable). */
  hunks: DiffHunk[] | null;
}

const KNOWN_STATUSES = new Set<CodeChangeFileStatus>([
  "added", "modified", "deleted", "renamed", "copied", "type_changed",
]);

function normalizeStatus(status: string): CodeChangeFileStatus {
  return KNOWN_STATUSES.has(status as CodeChangeFileStatus)
    ? (status as CodeChangeFileStatus)
    : "modified";
}

/**
 * Join the stored file list with the parsed patch. The list is authoritative
 * for which files changed and by how much — it survives a patch too large to
 * keep — and the patch supplies the hunks. Without a list the patch alone
 * describes the change (an uploaded `.patch` file).
 */
export function buildDiffFiles(
  files: readonly CodeChangeFile[] | null,
  parsed: readonly ParsedDiffFile[],
): DiffFileView[] {
  if (!files) {
    return parsed.map((f) => ({
      path: f.path,
      oldPath: f.oldPath,
      status: f.status,
      additions: f.additions,
      deletions: f.deletions,
      binary: f.binary,
      hunks: f.hunks,
    }));
  }
  const byPath = new Map<string, ParsedDiffFile>();
  for (const f of parsed) {
    if (!byPath.has(f.path)) byPath.set(f.path, f);
  }
  return files.map((f) => {
    const match = byPath.get(f.path);
    return {
      path: f.path,
      oldPath: f.old_path || null,
      status: normalizeStatus(f.status),
      additions: f.additions,
      deletions: f.deletions,
      binary: !!f.binary || !!match?.binary,
      hunks: match ? match.hunks : null,
    };
  });
}

export interface DiffTotals {
  files: number;
  additions: number;
  deletions: number;
}

export function diffTotals(files: readonly Pick<DiffFileView, "additions" | "deletions">[]): DiffTotals {
  let additions = 0;
  let deletions = 0;
  for (const f of files) {
    additions += f.additions;
    deletions += f.deletions;
  }
  return { files: files.length, additions, deletions };
}

export interface DiffFileGroup<T> {
  /** Directory, "" for the repository root. */
  directory: string;
  files: T[];
}

/**
 * Group files by directory for the viewer's file list, directories in path
 * order and each file under its own directory.
 */
export function groupFilesByDirectory<T extends { path: string }>(files: readonly T[]): DiffFileGroup<T>[] {
  const groups = new Map<string, T[]>();
  const sorted = [...files].sort((a, b) => a.path.localeCompare(b.path));
  for (const file of sorted) {
    const slash = file.path.lastIndexOf("/");
    const directory = slash >= 0 ? file.path.slice(0, slash) : "";
    const list = groups.get(directory) ?? [];
    list.push(file);
    groups.set(directory, list);
  }
  return [...groups.entries()].map(([directory, list]) => ({ directory, files: list }));
}

export interface SplitRow {
  left: DiffLine | null;
  right: DiffLine | null;
}

/**
 * Lay one hunk out side by side: context on both sides, and each run of
 * removals paired line for line with the additions that replace it.
 */
export function splitHunkRows(hunk: DiffHunk): SplitRow[] {
  const rows: SplitRow[] = [];
  let dels: DiffLine[] = [];
  let adds: DiffLine[] = [];
  const flush = () => {
    const n = Math.max(dels.length, adds.length);
    for (let i = 0; i < n; i++) rows.push({ left: dels[i] ?? null, right: adds[i] ?? null });
    dels = [];
    adds = [];
  };
  for (const line of hunk.lines) {
    if (line.kind === "context") {
      flush();
      rows.push({ left: line, right: line });
    } else if (line.kind === "del") {
      if (adds.length > 0) flush();
      dels.push(line);
    } else {
      adds.push(line);
    }
  }
  flush();
  return rows;
}

/**
 * A run's place among the issue's runs, counting from 1, the way the page
 * reads them top to bottom. Quick create owns the issue's creation rather
 * than a turn inside it, and is not counted. 0 when the run is not listed.
 */
export function runNumber(tasks: readonly Pick<AgentTask, "id" | "created_at" | "kind">[], taskId: string): number {
  const ordered = tasks
    .filter((task) => task.kind !== "quick_create")
    .sort((a, b) => a.created_at.localeCompare(b.created_at) || a.id.localeCompare(b.id));
  return ordered.findIndex((task) => task.id === taskId) + 1;
}

/**
 * The code change that stands for the issue's whole line of work in one
 * repository: the newest run's branch row, or — when that run's change was
 * the whole line of work and the daemon sent no branch row — its run row.
 */
export function latestLineOfWork(
  changes: readonly TaskCodeChange[],
  repoKey: string,
): TaskCodeChange | null {
  const inRepo = changes.filter((c) => c.repo_key === repoKey);
  if (inRepo.length === 0) return null;
  const newest = inRepo.reduce((a, b) => (b.created_at > a.created_at ? b : a));
  const sameRun = inRepo.filter((c) => c.task_id === newest.task_id);
  return sameRun.find((c) => c.scope === "branch") ?? sameRun.find((c) => c.scope === "run") ?? newest;
}

function ownerRepo(pr: Pick<GitHubPullRequest, "repo_owner" | "repo_name">): string {
  return `${pr.repo_owner}/${pr.repo_name}`.toLowerCase();
}

/**
 * The linked GitHub pull request that carries a change's branch: the one
 * whose head branch is the branch the run delivered. A run on a detached
 * checkout names no branch; it takes the only GitHub PR on the issue in the
 * same repository. Anything less certain returns null — the branch diff the
 * daemon captured is always right about the line of work, a guessed PR is not.
 */
export function pullRequestForChange(
  prs: readonly GitHubPullRequest[],
  change: Pick<TaskCodeChange, "branch" | "repo_label">,
): GitHubPullRequest | null {
  const repo = change.repo_label.toLowerCase();
  // A label without a slash is a directory name: the repository has no hosted
  // remote to compare, so the branch alone decides.
  const github = prs.filter((pr) => (pr.provider ?? "github") === "github"
    && (!repo.includes("/") || ownerRepo(pr) === repo));
  if (change.branch) return github.find((pr) => pr.branch === change.branch) ?? null;
  return github.length === 1 ? github[0]! : null;
}
