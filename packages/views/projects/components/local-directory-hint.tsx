"use client";

import { useQuery } from "@tanstack/react-query";
import { FolderOpen, GitBranch } from "lucide-react";
import { projectResourcesOptions } from "@multica/core/projects";
import type { LocalDirectoryResourceRef, ProjectResource } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { useLocalDaemonStatus } from "../../platform";
import { useT } from "../../i18n";

/**
 * Banner shown at the top of the issue's Activity section when the
 * project is pinned to a `local_directory` resource on **this** daemon.
 * Tells the user what starting an agent here will actually do to that
 * directory, which differs by the resource's execution mode:
 *
 * - `in_place`: the agent edits {label} ({path}) itself, so the user should
 *   expect their working copy to change under them.
 * - `worktree`: the agent never touches that working copy — it runs in an
 *   isolated worktree of the repo and hands back a branch. Saying "in-place"
 *   here would be a plain factual error, and it would send the user looking
 *   for results in a directory that will not have changed (MUL-5707).
 *
 * Rendered only on desktop: web has no daemon to compare against, so the
 * "this machine" check would always fail. Web users will see local_directory
 * resources read-only in the sidebar but no Activity-section hint.
 *
 * SSR-safe: the underlying hook reads `window.daemonAPI` defensively, so
 * server renders return null.
 */
export function LocalDirectoryHint({
  projectId,
}: {
  projectId: string | null | undefined;
}) {
  const { t } = useT("projects");
  const wsId = useWorkspaceId();
  const daemon = useLocalDaemonStatus();
  const { data: resources = [] } = useQuery({
    ...projectResourcesOptions(wsId, projectId ?? ""),
    enabled: Boolean(projectId),
  });

  if (!projectId) return null;
  if (!daemon.daemonId) return null;

  const matches: Array<ProjectResource & { resource_ref: LocalDirectoryResourceRef }> =
    resources
      .filter(
        (r): r is ProjectResource & { resource_ref: LocalDirectoryResourceRef } =>
          r.resource_type === "local_directory",
      )
      .filter((r) => r.resource_ref.daemon_id === daemon.daemonId);

  if (matches.length === 0) return null;

  return (
    <div className="mt-3 space-y-1 rounded-md border border-dashed bg-muted/40 px-3 py-2 text-caption text-muted-foreground">
      {matches.map((resource) => {
        const ref = resource.resource_ref;
        const label = (ref.label || resource.label || ref.local_path).trim() ||
          ref.local_path;
        // Anything other than an explicit "worktree" is in_place: the mode is
        // absent on resources created before it existed, and an unknown value
        // from a newer server must not claim isolation we cannot verify.
        const isWorktree = ref.execution_mode === "worktree";
        return (
          <div key={resource.id} className="space-y-0.5">
            <div className="flex items-center gap-2">
              {isWorktree ? (
                <GitBranch className="size-3 shrink-0" />
              ) : (
                <FolderOpen className="size-3 shrink-0" />
              )}
              <span className="truncate">
                {isWorktree
                  ? t(($) => $.resources.chat_hint_worktree_prefix)
                  : t(($) => $.resources.chat_hint_prefix)}
                <span className="font-medium text-foreground"> {label} </span>
                <span className="font-mono opacity-70">({ref.local_path})</span>
              </span>
            </div>
            {/* Where the work ends up. Only worktree mode needs saying: in
                place, the answer is the directory already named above. */}
            {isWorktree && (
              <div className="pl-5 opacity-80">
                {t(($) => $.resources.chat_hint_worktree_note)}
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
}
