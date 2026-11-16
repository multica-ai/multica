import type { AgentTask } from "../types/agent";

type WorkdirTask = Pick<
  AgentTask,
  | "branch_name"
  | "created_at"
  | "durable_work_dir"
  | "relative_durable_work_dir"
  | "relative_work_dir"
  | "status"
  | "work_dir"
>;

export interface WorkdirCopyTarget {
  path: string;
  relativePath?: string;
  branchName?: string;
  source: "durable_project_directory" | "task_workdir";
}

const terminalStatuses = new Set(["completed", "failed", "cancelled"]);

/**
 * Resolves the path copied for a task without inferring ownership from the
 * current machine or mutable project configuration.
 *
 * `durable_work_dir` is a daemon-reported lifecycle fact: it is populated only
 * after a disposable worktree was finalized and its removal confirmed. Until
 * then (and for mixed-version deployments) `work_dir` remains authoritative.
 */
export function resolveWorkdirCopyTarget(
  tasks: readonly WorkdirTask[] | undefined,
): WorkdirCopyTarget | undefined {
  const latestTask = tasks
    ?.filter((task) => {
      if (task.work_dir?.trim()) return true;
      return (
        terminalStatuses.has(task.status) &&
        Boolean(task.durable_work_dir?.trim())
      );
    })
    .reduce<WorkdirTask | undefined>(
      (latest, task) =>
        !latest || task.created_at > latest.created_at ? task : latest,
      undefined,
    );

  if (!latestTask) return undefined;

  const durablePath = latestTask.durable_work_dir?.trim();
  if (terminalStatuses.has(latestTask.status) && durablePath) {
    return {
      path: durablePath,
      relativePath: latestTask.relative_durable_work_dir?.trim() || undefined,
      branchName: latestTask.branch_name?.trim() || undefined,
      source: "durable_project_directory",
    };
  }

  const workDir = latestTask.work_dir?.trim();
  if (!workDir) return undefined;
  return {
    path: workDir,
    relativePath: latestTask.relative_work_dir?.trim() || undefined,
    branchName: latestTask.branch_name?.trim() || undefined,
    source: "task_workdir",
  };
}
