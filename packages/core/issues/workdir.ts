import type { AgentRuntime, AgentTask } from "../types/agent";
import type { ProjectResource } from "../types/project";

type WorkdirTask = Pick<AgentTask, "created_at" | "runtime_id" | "work_dir">;
type WorkdirRuntime = Pick<AgentRuntime, "daemon_id" | "id">;
type WorkdirProjectResource = Pick<
  ProjectResource,
  "resource_ref" | "resource_type"
>;

export interface WorkdirCopyContext {
  localDaemonId?: string | null;
  projectResources?: readonly WorkdirProjectResource[];
  runtimes?: readonly WorkdirRuntime[];
}

/**
 * Resolves the path copied by issue actions.
 *
 * A worktree-mode local_directory task records its disposable task worktree in
 * `work_dir`. Once that task is finalized the worktree is removed, so Desktop
 * should copy the durable project `local_path` when it can prove the latest
 * task ran on this daemon. Every unverified case keeps the existing `work_dir`
 * behavior instead of guessing that a local project binding owns a remote run.
 */
export function resolveWorkdirCopyPath(
  tasks: readonly WorkdirTask[] | undefined,
  context: WorkdirCopyContext = {},
): string | undefined {
  const latestTask = tasks
    ?.filter((task) => Boolean(task.work_dir))
    .reduce<WorkdirTask | undefined>(
      (latest, task) =>
        !latest || task.created_at > latest.created_at ? task : latest,
      undefined,
    );
  if (!latestTask?.work_dir) return undefined;

  const { localDaemonId, projectResources = [], runtimes = [] } = context;
  if (!localDaemonId) return latestTask.work_dir;

  const latestRuntime = runtimes.find(
    (runtime) => runtime.id === latestTask.runtime_id,
  );
  if (latestRuntime?.daemon_id !== localDaemonId) return latestTask.work_dir;

  const localPaths = projectResources.flatMap((resource) => {
    if (resource.resource_type !== "local_directory") return [];
    const ref = resource.resource_ref;
    if (
      typeof ref !== "object" ||
      ref === null ||
      !("daemon_id" in ref) ||
      !("local_path" in ref) ||
      ref.daemon_id !== localDaemonId ||
      typeof ref.local_path !== "string" ||
      ref.local_path.length === 0
    ) {
      return [];
    }
    return [ref.local_path];
  });

  return localPaths.length === 1 ? localPaths[0] : latestTask.work_dir;
}
