import { afterAll, describe, expect, it } from "vitest";
import { execFile } from "child_process";
import { promises as fs } from "fs";
import * as os from "os";
import * as path from "path";

import {
  applyTaskDiff,
  createDefaultGitRunner,
  previewTaskDiff,
  type GitRunner,
  type TaskChangeInput,
} from "./task-change-service";

// ---------------------------------------------------------------------
// Test helpers
//
// Each test creates its own pair of temp git repos under os.tmpdir() with a
// unique prefix and tears them down via the returned cleanup function. We
// never share a TMPDIR between tests, never rely on global state, and never
// pollute the user's environment.

type TempRepo = {
  dir: string;
  cleanup: () => Promise<void>;
};

const tempRoots: string[] = [];

afterAll(async () => {
  // Belt-and-braces: if a test failed before its cleanup ran, sweep here.
  await Promise.all(
    tempRoots.map(async (dir) => {
      try {
        await fs.rm(dir, { recursive: true, force: true });
      } catch {
        // best-effort
      }
    }),
  );
});

function execGit(cwd: string, args: string[]): Promise<string> {
  return new Promise((resolve, reject) => {
    execFile(
      "git",
      args,
      {
        cwd,
        env: {
          ...process.env,
          GIT_AUTHOR_NAME: "Test",
          GIT_AUTHOR_EMAIL: "test@example.com",
          GIT_COMMITTER_NAME: "Test",
          GIT_COMMITTER_EMAIL: "test@example.com",
          GIT_CONFIG_GLOBAL: "/dev/null",
          GIT_CONFIG_SYSTEM: "/dev/null",
        },
      },
      (err, stdout, stderr) => {
        if (err) {
          const msg =
            stderr?.toString().trim() ||
            (err as Error).message ||
            "git failed";
          reject(new Error(`git ${args.join(" ")} failed: ${msg}`));
          return;
        }
        resolve(stdout.toString());
      },
    );
  });
}

const FAKE_ORIGIN_URL = "https://github.com/test/multica-fixture.git";

async function makeTempRepo(prefix: string): Promise<TempRepo> {
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), `mt-${prefix}-`));
  tempRoots.push(dir);
  await execGit(dir, ["init", "-q", "-b", "main"]);
  await execGit(dir, ["config", "user.email", "test@example.com"]);
  await execGit(dir, ["config", "user.name", "Test"]);
  await execGit(dir, ["config", "commit.gpgsign", "false"]);
  // Synthetic origin so previewTaskDiff can read a non-empty URL.
  await execGit(dir, ["remote", "add", "origin", FAKE_ORIGIN_URL]);
  // Initial commit with a file we can later modify.
  await fs.writeFile(path.join(dir, "a.txt"), "hello\nworld\n");
  await fs.writeFile(path.join(dir, "b.txt"), "lorem\nipsum\n");
  await execGit(dir, ["add", "a.txt", "b.txt"]);
  await execGit(dir, ["commit", "-q", "-m", "initial"]);

  return {
    dir,
    cleanup: async () => {
      await fs.rm(dir, { recursive: true, force: true });
    },
  };
}

/**
 * Build a "task" repo (the agent's worktree) by cloning from a base repo and
 * making a branch with one or more changes against `main`.
 */
async function makeTaskWorktreeFrom(
  baseDir: string,
  prefix: string,
  branchName: string,
  mutate: (dir: string) => Promise<void>,
): Promise<TempRepo> {
  // Use a fresh tmpdir parent so the clone target itself doesn't pre-exist.
  const parent = await fs.mkdtemp(path.join(os.tmpdir(), `mt-${prefix}-`));
  tempRoots.push(parent);
  const dir = path.join(parent, "task");
  // Clone the base so main+history are present without dance.
  await execGit(parent, ["clone", "-q", baseDir, "task"]);
  await execGit(dir, ["config", "user.email", "task@example.com"]);
  await execGit(dir, ["config", "user.name", "Task"]);
  await execGit(dir, ["config", "commit.gpgsign", "false"]);
  // Branch off main, mutate, commit.
  await execGit(dir, ["checkout", "-q", "-b", branchName]);
  await mutate(dir);
  await execGit(dir, ["add", "-A"]);
  await execGit(dir, ["commit", "-q", "-m", "task changes"]);

  return {
    dir,
    cleanup: async () => {
      await fs.rm(parent, { recursive: true, force: true });
    },
  };
}

function spyRunner(): {
  runner: GitRunner;
  calls: string[][];
} {
  const real = createDefaultGitRunner();
  const calls: string[][] = [];
  return {
    calls,
    runner: {
      run: async (cwd, args) => {
        calls.push(args);
        return real.run(cwd, args);
      },
    },
  };
}

// ---------------------------------------------------------------------
// Tests
//
// Each test owns its temp dirs and tears them down via try/finally. Even when
// an assertion fails, cleanup runs.

describe("task-change-service", () => {
  it("preview lists changed files", async () => {
    const target = await makeTempRepo("tgt-changed");
    const task = await makeTaskWorktreeFrom(
      target.dir,
      "task-changed",
      "task/feat",
      async (dir) => {
        await fs.writeFile(path.join(dir, "a.txt"), "hello\nworld\nMORE\n");
      },
    );
    try {
      const input: TaskChangeInput = {
        taskWorktreePath: task.dir,
        taskBranchName: "task/feat",
        baseRef: "main",
        taskRepoURL: FAKE_ORIGIN_URL,
        targetCheckout: target.dir,
        newBranchName: "task/feat-applied",
      };
      const preview = await previewTaskDiff(input);

      expect(preview.changedFiles).toContain("a.txt");
      expect(preview.deletedFiles).toEqual([]);
      expect(preview.diff).toContain("a.txt");
    } finally {
      await task.cleanup();
      await target.cleanup();
    }
  });

  it("preview lists deleted files", async () => {
    const target = await makeTempRepo("tgt-deleted");
    const task = await makeTaskWorktreeFrom(
      target.dir,
      "task-deleted",
      "task/feat",
      async (dir) => {
        await execGit(dir, ["rm", "-q", "b.txt"]);
      },
    );
    try {
      const input: TaskChangeInput = {
        taskWorktreePath: task.dir,
        taskBranchName: "task/feat",
        baseRef: "main",
        taskRepoURL: FAKE_ORIGIN_URL,
        targetCheckout: target.dir,
        newBranchName: "task/feat-applied",
      };
      const preview = await previewTaskDiff(input);

      expect(preview.deletedFiles).toContain("b.txt");
    } finally {
      await task.cleanup();
      await target.cleanup();
    }
  });

  it("preview detects a clean target", async () => {
    const target = await makeTempRepo("tgt-clean");
    const task = await makeTaskWorktreeFrom(
      target.dir,
      "task-clean",
      "task/feat",
      async (dir) => {
        await fs.writeFile(path.join(dir, "a.txt"), "changed\n");
      },
    );
    try {
      const preview = await previewTaskDiff({
        taskWorktreePath: task.dir,
        taskBranchName: "task/feat",
        baseRef: "main",
        taskRepoURL: FAKE_ORIGIN_URL,
        targetCheckout: target.dir,
        newBranchName: "task/feat-applied",
      });

      expect(preview.targetClean).toBe(true);
    } finally {
      await task.cleanup();
      await target.cleanup();
    }
  });

  it("preview detects a dirty target", async () => {
    const target = await makeTempRepo("tgt-dirty");
    const task = await makeTaskWorktreeFrom(
      target.dir,
      "task-dirty",
      "task/feat",
      async (dir) => {
        await fs.writeFile(path.join(dir, "a.txt"), "changed\n");
      },
    );
    try {
      // Make target dirty.
      await fs.writeFile(path.join(target.dir, "a.txt"), "uncommitted\n");

      const preview = await previewTaskDiff({
        taskWorktreePath: task.dir,
        taskBranchName: "task/feat",
        baseRef: "main",
        taskRepoURL: FAKE_ORIGIN_URL,
        targetCheckout: target.dir,
        newBranchName: "task/feat-applied",
      });

      expect(preview.targetClean).toBe(false);
    } finally {
      await task.cleanup();
      await target.cleanup();
    }
  });

  it("preview detects a repo mismatch", async () => {
    const target = await makeTempRepo("tgt-mismatch");
    const task = await makeTaskWorktreeFrom(
      target.dir,
      "task-mismatch",
      "task/feat",
      async (dir) => {
        await fs.writeFile(path.join(dir, "a.txt"), "x\n");
      },
    );
    try {
      // Override target's origin to a different URL so URLs don't match.
      await execGit(target.dir, [
        "remote",
        "set-url",
        "origin",
        "https://github.com/example/other.git",
      ]);
      const preview = await previewTaskDiff({
        taskWorktreePath: task.dir,
        taskBranchName: "task/feat",
        baseRef: "main",
        taskRepoURL: "https://github.com/example/expected.git",
        targetCheckout: target.dir,
        newBranchName: "task/feat-applied",
      });

      expect(preview.repoMatches).toBe(false);
      expect(preview.targetRepoURL).toContain("other");
    } finally {
      await task.cleanup();
      await target.cleanup();
    }
  });

  it("apply creates the branch when missing", async () => {
    const target = await makeTempRepo("tgt-newbr");
    const task = await makeTaskWorktreeFrom(
      target.dir,
      "task-newbr",
      "task/feat",
      async (dir) => {
        await fs.writeFile(path.join(dir, "a.txt"), "hello\nworld\nMORE\n");
      },
    );
    try {
      const result = await applyTaskDiff({
        taskWorktreePath: task.dir,
        taskBranchName: "task/feat",
        baseRef: "main",
        taskRepoURL: FAKE_ORIGIN_URL,
        targetCheckout: target.dir,
        newBranchName: "applied/task-feat",
      });

      expect(result).toEqual({
        ok: true,
        createdBranch: true,
        branchName: "applied/task-feat",
      });
      const branches = await execGit(target.dir, ["branch", "--list"]);
      expect(branches).toContain("applied/task-feat");
    } finally {
      await task.cleanup();
      await target.cleanup();
    }
  });

  it("apply leaves changes unstaged (working tree) — not staged", async () => {
    const target = await makeTempRepo("tgt-unstaged");
    const task = await makeTaskWorktreeFrom(
      target.dir,
      "task-unstaged",
      "task/feat",
      async (dir) => {
        await fs.writeFile(path.join(dir, "a.txt"), "hello\nworld\nMORE\n");
      },
    );
    try {
      const result = await applyTaskDiff({
        taskWorktreePath: task.dir,
        taskBranchName: "task/feat",
        baseRef: "main",
        taskRepoURL: FAKE_ORIGIN_URL,
        targetCheckout: target.dir,
        newBranchName: "applied/task-feat",
      });

      expect(result.ok).toBe(true);
      const status = await execGit(target.dir, ["status", "--porcelain"]);
      // Worktree-only modification: status code is " M <file>" — second column
      // is 'M' (worktree), first column is space (not staged).
      expect(status).toMatch(/^ M a\.txt/m);
      // No fully-staged files (would be 'M ' with first column 'M').
      expect(status).not.toMatch(/^M\s/m);
    } finally {
      await task.cleanup();
      await target.cleanup();
    }
  });

  it("apply does not create a commit", async () => {
    const target = await makeTempRepo("tgt-nocommit");
    const task = await makeTaskWorktreeFrom(
      target.dir,
      "task-nocommit",
      "task/feat",
      async (dir) => {
        await fs.writeFile(path.join(dir, "a.txt"), "hello\nworld\nMORE\n");
      },
    );
    try {
      const before = (
        await execGit(target.dir, ["rev-list", "--count", "HEAD"])
      ).trim();

      await applyTaskDiff({
        taskWorktreePath: task.dir,
        taskBranchName: "task/feat",
        baseRef: "main",
        taskRepoURL: FAKE_ORIGIN_URL,
        targetCheckout: target.dir,
        newBranchName: "applied/task-feat",
      });

      const after = (
        await execGit(target.dir, ["rev-list", "--count", "HEAD"])
      ).trim();
      expect(after).toBe(before);
    } finally {
      await task.cleanup();
      await target.cleanup();
    }
  });

  it("apply does not invoke git push", async () => {
    const target = await makeTempRepo("tgt-nopush");
    const task = await makeTaskWorktreeFrom(
      target.dir,
      "task-nopush",
      "task/feat",
      async (dir) => {
        await fs.writeFile(path.join(dir, "a.txt"), "hello\nworld\nMORE\n");
      },
    );
    const spy = spyRunner();
    try {
      await applyTaskDiff(
        {
          taskWorktreePath: task.dir,
          taskBranchName: "task/feat",
          baseRef: "main",
          taskRepoURL: FAKE_ORIGIN_URL,
          targetCheckout: target.dir,
          newBranchName: "applied/task-feat",
        },
        { runner: spy.runner },
      );

      const pushed = spy.calls.some((args) => args[0] === "push");
      expect(pushed).toBe(false);
    } finally {
      await task.cleanup();
      await target.cleanup();
    }
  });

  it("apply reports a recoverable conflict when the patch does not apply", async () => {
    const target = await makeTempRepo("tgt-conflict");
    const task = await makeTaskWorktreeFrom(
      target.dir,
      "task-conflict",
      "task/feat",
      async (dir) => {
        await fs.writeFile(
          path.join(dir, "a.txt"),
          "hello\nAGENT-LINE\nworld\n",
        );
      },
    );
    try {
      // Mutate target to conflict on the same line as the agent.
      await fs.writeFile(
        path.join(target.dir, "a.txt"),
        "hello\nUSER-LINE\nworld\n",
      );
      await execGit(target.dir, ["add", "a.txt"]);
      await execGit(target.dir, ["commit", "-q", "-m", "user conflict"]);

      const result = await applyTaskDiff({
        taskWorktreePath: task.dir,
        taskBranchName: "task/feat",
        baseRef: "main",
        taskRepoURL: FAKE_ORIGIN_URL,
        targetCheckout: target.dir,
        newBranchName: "applied/task-feat",
      });

      expect(result.ok).toBe(false);
      if (!result.ok) {
        expect(result.reason).toBe("conflict");
        expect(typeof result.detail).toBe("string");
        expect(result.detail.length).toBeGreaterThan(0);
      }
    } finally {
      await task.cleanup();
      await target.cleanup();
    }
  });
});
