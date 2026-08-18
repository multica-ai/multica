// @vitest-environment node

import { describe, expect, it } from "vitest";
import { resolveWorkdirCopyPath } from "./workdir";

const localRuntime = { id: "runtime-local", daemon_id: "daemon-local" };
const remoteRuntime = { id: "runtime-remote", daemon_id: "daemon-remote" };

function task(
  runtimeId: string,
  createdAt: string,
  workDir: string | undefined,
) {
  return {
    runtime_id: runtimeId,
    created_at: createdAt,
    work_dir: workDir,
  };
}

function localDirectory(daemonId: string, localPath: string) {
  return {
    resource_type: "local_directory" as const,
    resource_ref: {
      daemon_id: daemonId,
      local_path: localPath,
      execution_mode: "worktree" as const,
    },
  };
}

describe("resolveWorkdirCopyPath", () => {
  it("uses the durable project directory for a local worktree task", () => {
    expect(
      resolveWorkdirCopyPath(
        [
          task(
            "runtime-local",
            "2026-08-18T10:00:00Z",
            "/managed/task/worktree",
          ),
        ],
        {
          localDaemonId: "daemon-local",
          runtimes: [localRuntime],
          projectResources: [
            localDirectory("daemon-local", "/Users/dev/project"),
          ],
        },
      ),
    ).toBe("/Users/dev/project");
  });

  it("keeps the newest task workdir when no local daemon is available", () => {
    expect(
      resolveWorkdirCopyPath([
        task("runtime-local", "2026-08-18T09:00:00Z", "/older"),
        task("runtime-local", "2026-08-18T10:00:00Z", "/newer"),
      ]),
    ).toBe("/newer");
  });

  it("does not replace a remote task path with this machine's project path", () => {
    expect(
      resolveWorkdirCopyPath(
        [
          task(
            "runtime-remote",
            "2026-08-18T10:00:00Z",
            "/remote/task/worktree",
          ),
        ],
        {
          localDaemonId: "daemon-local",
          runtimes: [localRuntime, remoteRuntime],
          projectResources: [
            localDirectory("daemon-local", "/Users/dev/project"),
          ],
        },
      ),
    ).toBe("/remote/task/worktree");
  });

  it("keeps the task path when its runtime cannot be verified", () => {
    expect(
      resolveWorkdirCopyPath(
        [task("missing", "2026-08-18T10:00:00Z", "/task/worktree")],
        {
          localDaemonId: "daemon-local",
          projectResources: [
            localDirectory("daemon-local", "/Users/dev/project"),
          ],
        },
      ),
    ).toBe("/task/worktree");
  });

  it("ignores project directories owned by another daemon", () => {
    expect(
      resolveWorkdirCopyPath(
        [
          task(
            "runtime-local",
            "2026-08-18T10:00:00Z",
            "/task/worktree",
          ),
        ],
        {
          localDaemonId: "daemon-local",
          runtimes: [localRuntime],
          projectResources: [
            localDirectory("daemon-remote", "/remote/project"),
          ],
        },
      ),
    ).toBe("/task/worktree");
  });

  it("does not guess when duplicate local directories reach the client", () => {
    expect(
      resolveWorkdirCopyPath(
        [
          task(
            "runtime-local",
            "2026-08-18T10:00:00Z",
            "/task/worktree",
          ),
        ],
        {
          localDaemonId: "daemon-local",
          runtimes: [localRuntime],
          projectResources: [
            localDirectory("daemon-local", "/Users/dev/a"),
            localDirectory("daemon-local", "/Users/dev/b"),
          ],
        },
      ),
    ).toBe("/task/worktree");
  });

  it("returns undefined when no task has a workdir", () => {
    expect(
      resolveWorkdirCopyPath([
        task("runtime-local", "2026-08-18T10:00:00Z", undefined),
      ]),
    ).toBeUndefined();
  });
});
