"use client";

import type { AgentTask } from "@multica/core/types";
import { useT } from "../../i18n";

export function ExecutionSourceDetails({ task }: { task: AgentTask }) {
  const { t } = useT("projects");
  const source = task.execution_source;
  // Older runs have no receipt. Today's project settings cannot fill that gap.
  if (!source) return null;
  const kind = source.kind === "no_code"
    ? t(($) => $.source.kind_no_code)
    : source.kind === "local_directory"
      ? t(($) => $.source.kind_local_directory)
      : source.kind === "github_repo"
        ? t(($) => $.source.kind_github_repo)
        : source.kind;
  const label = source.repositories?.map((repo) => repo.url + (repo.ref ? " @ " + repo.ref : "")).join(", ") || source.label;
  const mode = source.mode === "worktree"
    ? t(($) => $.source.mode_worktree)
    : source.mode === "in_place"
      ? t(($) => $.source.mode_in_place)
      : source.mode === "managed_checkout"
        ? t(($) => $.source.mode_managed_checkout)
        : "";
  // An unrelated model failure or user cancellation says nothing about the
  // source's availability. Folder failures carry their own stable reason.
  const sourceFailed = task.failure_reason === "local_directory_error" ||
    task.failure_reason === "local_directory_missing";
  const readiness = sourceFailed
    ? t(($) => $.source.unavailable)
    : source.kind === "no_code" || task.started_at
      ? t(($) => $.source.ready)
      : t(($) => $.source.pending);
  return (
    <div className="text-caption space-y-1" data-execution-source>
      <strong>{t(($) => $.source.label)}</strong>
      <p>{[kind, label, source.daemon_id, mode, readiness].filter(Boolean).join(" · ")}</p>
    </div>
  );
}
