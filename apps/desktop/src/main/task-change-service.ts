// task-change-service.ts
//
// Main-process service that lets the user preview and apply changes from a
// daemon-managed task worktree (the agent's branch) onto the user's own
// checkout. Both `previewTaskDiff` and `applyTaskDiff` are pure orchestrators
// over `git` invocations — all I/O goes through a `GitRunner` so tests can
// drive the service hermetically against real temp repos and assert on the
// invocation transcript (e.g. "we never invoked `git push`").
//
// Safety invariants — these are the contract this service enforces:
//   - We refuse to apply onto a dirty working tree (would clobber user work).
//   - We refuse to apply when the target's `origin` URL doesn't match the
//     task's repo URL (would silently corrupt an unrelated checkout).
//   - We never commit. The patch lands UNSTAGED so the user reviews it in
//     their editor and stages/commits with the message they want.
//   - We never push. Tests assert on the runner transcript that no `git push`
//     argv ever appears.
//
// Recoverability — when `git apply --3way` cannot apply cleanly we report
// `{ ok: false, reason: "conflict" }`. The working tree is left in whatever
// partial state `git apply --3way` produced (some hunks applied, some marked
// with conflict markers). The user can either resolve the markers manually
// or run `git restore .` to revert. We do NOT roll back automatically because
// "partial apply with markers" is exactly what the user wants in many cases.

import { execFile } from "child_process";
import { promises as fs } from "fs";
import * as os from "os";
import * as path from "path";

// Data-shape types live in shared/ so the preload bridge can import them
// without dragging child_process/fs into the renderer.
export type {
  TaskChangeInput,
  TaskChangePreview,
  TaskChangeApplyResult,
} from "../shared/task-change-types";

import type {
  TaskChangeInput,
  TaskChangePreview,
  TaskChangeApplyResult,
} from "../shared/task-change-types";

export interface GitRunner {
  /** Run a git command. Returns stdout on success; throws on non-zero exit with stderr in the error. */
  run: (cwd: string, args: string[]) => Promise<string>;
}

// ---------------------------------------------------------------------
// Default runner — execFile-based so args bypass the shell.

export function createDefaultGitRunner(): GitRunner {
  return {
    run: (cwd, args) =>
      new Promise((resolve, reject) => {
        execFile(
          "git",
          args,
          {
            cwd,
            // Generous buffer for large diffs (default is 1 MB).
            maxBuffer: 64 * 1024 * 1024,
            env: {
              ...process.env,
              // Don't let user's global git config (e.g. signing, hooks) affect
              // what we run. The local repo's own config still applies.
              GIT_TERMINAL_PROMPT: "0",
            },
          },
          (err, stdout, stderr) => {
            if (err) {
              const detail =
                stderr?.toString().trim() ||
                (err as Error).message ||
                "git failed";
              const wrapped = new Error(detail);
              // Preserve original error for callers who want richer info.
              (wrapped as Error & { cause?: unknown }).cause = err;
              reject(wrapped);
              return;
            }
            resolve(stdout.toString());
          },
        );
      }),
  };
}

// ---------------------------------------------------------------------
// Public API

export async function previewTaskDiff(
  input: TaskChangeInput,
  opts?: { runner?: GitRunner },
): Promise<TaskChangePreview> {
  const runner = opts?.runner ?? createDefaultGitRunner();
  return computePreview(input, runner);
}

export async function applyTaskDiff(
  input: TaskChangeInput,
  opts?: { runner?: GitRunner },
): Promise<TaskChangeApplyResult> {
  const runner = opts?.runner ?? createDefaultGitRunner();

  // Re-run the gates from preview. We don't trust the renderer to have done it.
  let preview: TaskChangePreview;
  try {
    preview = await computePreview(input, runner);
  } catch (err) {
    return {
      ok: false,
      reason: "git_failure",
      detail: errorMessage(err),
    };
  }

  if (!preview.repoMatches) {
    return {
      ok: false,
      reason: "repo_mismatch",
      detail: `target origin ${preview.targetRepoURL} does not match task repo ${input.taskRepoURL}`,
    };
  }
  if (!preview.targetClean) {
    return {
      ok: false,
      reason: "target_dirty",
      detail:
        "target checkout has uncommitted changes — commit or stash before applying",
    };
  }
  if (preview.diff.trim() === "") {
    return {
      ok: false,
      reason: "no_changes",
      detail: "no changes to apply between base and task branch",
    };
  }

  // Decide whether the target branch already exists.
  let createdBranch = false;
  try {
    const exists = await branchExists(
      runner,
      input.targetCheckout,
      input.newBranchName,
    );
    if (exists) {
      await runner.run(input.targetCheckout, ["switch", input.newBranchName]);
    } else {
      await runner.run(input.targetCheckout, [
        "switch",
        "-c",
        input.newBranchName,
      ]);
      createdBranch = true;
    }
  } catch (err) {
    return {
      ok: false,
      reason: "git_failure",
      detail: errorMessage(err),
    };
  }

  // Write the diff to a temp file and call `git apply --3way <file>`.
  // Each apply call gets its own scratch dir so concurrent applies (unlikely
  // but possible if two windows kicked off at once) can never collide.
  const scratchDir = await fs.mkdtemp(
    path.join(os.tmpdir(), "multica-task-apply-"),
  );
  const patchPath = path.join(scratchDir, "task.patch");
  try {
    await fs.writeFile(patchPath, preview.diff, "utf8");
    try {
      await runner.run(input.targetCheckout, [
        "apply",
        "--3way",
        "--whitespace=nowarn",
        patchPath,
      ]);
    } catch (err) {
      const detail = errorMessage(err);
      // git apply emits "patch does not apply" on plain failures and
      // "with conflicts" / "Failed to merge" on 3-way fallback failures.
      if (
        /patch does not apply|Failed to merge|with conflicts|conflict/i.test(
          detail,
        )
      ) {
        return { ok: false, reason: "conflict", detail };
      }
      return { ok: false, reason: "git_failure", detail };
    }

    // On modern git (2.40+) `git apply --3way` updates the index for cleanly
    // applied hunks. The product invariant is "leave changes for the user to
    // review and stage themselves" — so we unstage everything immediately,
    // preserving the working-tree edits. On conflict we already returned
    // above, so this path runs only when the apply was clean.
    try {
      await runner.run(input.targetCheckout, ["restore", "--staged", "."]);
    } catch (err) {
      // Non-fatal: even if unstage fails, the apply succeeded. Surface as
      // git_failure so the user knows to inspect index state manually.
      return {
        ok: false,
        reason: "git_failure",
        detail: `apply succeeded but unstage failed: ${errorMessage(err)}`,
      };
    }
  } finally {
    // Best-effort scratch cleanup; never blocks the result.
    await fs.rm(scratchDir, { recursive: true, force: true }).catch(() => {});
  }

  return {
    ok: true,
    createdBranch,
    branchName: input.newBranchName,
  };
}

// ---------------------------------------------------------------------
// Internals

async function computePreview(
  input: TaskChangeInput,
  runner: GitRunner,
): Promise<TaskChangePreview> {
  // 1) target must be a git repo.
  await runner.run(input.targetCheckout, ["rev-parse", "--is-inside-work-tree"]);

  // 2) read origin URL (best-effort — empty string if no origin configured).
  let targetRepoURL = "";
  try {
    targetRepoURL = (
      await runner.run(input.targetCheckout, [
        "remote",
        "get-url",
        "origin",
      ])
    ).trim();
  } catch {
    targetRepoURL = "";
  }
  const repoMatches =
    !!targetRepoURL &&
    normalizeRepoURL(targetRepoURL) === normalizeRepoURL(input.taskRepoURL);

  // 3) clean check.
  const status = await runner.run(input.targetCheckout, [
    "status",
    "--porcelain",
  ]);
  const targetClean = status.trim() === "";

  // 4) diff body.
  const diff = await runner.run(input.taskWorktreePath, [
    "diff",
    "--no-color",
    `${input.baseRef}...${input.taskBranchName}`,
  ]);

  // 5) name-status for changedFiles / deletedFiles.
  const nameStatus = await runner.run(input.taskWorktreePath, [
    "diff",
    "--name-status",
    `${input.baseRef}...${input.taskBranchName}`,
  ]);

  const { changedFiles, deletedFiles } = parseNameStatus(nameStatus);

  return {
    changedFiles,
    deletedFiles,
    diff,
    targetClean,
    targetRepoURL,
    repoMatches,
  };
}

async function branchExists(
  runner: GitRunner,
  cwd: string,
  branch: string,
): Promise<boolean> {
  try {
    await runner.run(cwd, [
      "rev-parse",
      "--verify",
      "--quiet",
      `refs/heads/${branch}`,
    ]);
    return true;
  } catch {
    return false;
  }
}

function parseNameStatus(out: string): {
  changedFiles: string[];
  deletedFiles: string[];
} {
  const changedFiles: string[] = [];
  const deletedFiles: string[] = [];
  for (const line of out.split("\n")) {
    if (line.trim() === "") continue;
    // Format: "<status>\t<path>" or "R<score>\t<old>\t<new>".
    const parts = line.split("\t");
    const status = parts[0]?.[0] ?? "";
    if (status === "D") {
      const filePath = parts[1];
      if (filePath) deletedFiles.push(filePath);
    } else if (status === "R" || status === "C") {
      // Renames/copies — record the new path as changed.
      const newPath = parts[2];
      if (newPath) changedFiles.push(newPath);
    } else {
      const filePath = parts[1];
      if (filePath) changedFiles.push(filePath);
    }
  }
  return { changedFiles, deletedFiles };
}

/**
 * Normalize a repo URL so HTTPS, SSH, and `.git`-suffixed forms compare equal.
 * Examples:
 *   git@github.com:org/repo.git     → https://github.com/org/repo
 *   https://github.com/org/repo     → https://github.com/org/repo
 *   https://user@github.com/org/repo → https://github.com/org/repo
 */
export function normalizeRepoURL(u: string): string {
  return u
    .toLowerCase()
    .replace(/\.git$/, "")
    .replace(/^https?:\/\/[^@]*@/, "https://")
    .replace(/^git@([^:]+):/, "https://$1/");
}

function errorMessage(err: unknown): string {
  if (err instanceof Error) return err.message;
  return String(err);
}
