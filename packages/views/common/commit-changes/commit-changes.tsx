"use client";

import { useCallback, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  getCommitDiffs,
  hasRecordedChanges,
  type CommitDiff,
} from "@multica/core/chat/commit-diff";
import { taskMessagesOptions, useTaskMessages } from "@multica/core/chat/queries";
import { issueTasksOptions } from "@multica/core/issues/queries";
import type { AgentTask, TaskMessagePayload } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@multica/ui/components/ui/dialog";
import { FilePen } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import { DiffDetailSurface } from "../task-transcript/detail-surfaces";
import { parseUnifiedDiff } from "../task-transcript/trace-event-presenter";

const NO_MESSAGES: TaskMessagePayload[] = [];

/** Shown only once the issue's run list says this run recorded changed files. */
export function IssueTaskCommitChanges({ issueId, taskId }: { issueId: string; taskId: string }) {
  const select = useCallback(
    (tasks: AgentTask[]) => tasks.some((task) => task.id === taskId && task.has_recorded_changes === true),
    [taskId],
  );
  const { data: recorded } = useQuery({ ...issueTasksOptions(issueId), select });
  return recorded ? <TaskCommitChanges taskId={taskId} /> : null;
}

/** Reads the transcript the caller already cached; never fetches it again. */
export function CachedTaskCommitChanges({ taskId }: { taskId: string }) {
  const { data } = useQuery({ ...taskMessagesOptions(taskId), enabled: false });
  return <CommitChanges messages={data ?? NO_MESSAGES} />;
}

/** Loads the record on open; callers decide whether the run has one. */
export function TaskCommitChanges({ taskId }: { taskId: string }) {
  const [open, setOpen] = useState(false);
  const { data, isPending, isError, refetch } = useTaskMessages(
    taskId,
    false,
    open,
  );
  const diffs = useMemo(() => getCommitDiffs(data ?? []), [data]);
  return (
    <ChangesDialog
      open={open}
      onOpenChange={setOpen}
      diffs={diffs}
      pending={isPending}
      error={isError}
      retry={() => void refetch()}
    />
  );
}

export function CommitChanges({
  messages,
}: {
  messages: readonly TaskMessagePayload[];
}) {
  const [open, setOpen] = useState(false);
  const diffs = useMemo(() => getCommitDiffs(messages), [messages]);
  if (!hasRecordedChanges(diffs)) return null;
  return <ChangesDialog open={open} onOpenChange={setOpen} diffs={diffs} />;
}

function ChangesDialog({
  open,
  onOpenChange,
  diffs,
  pending = false,
  error = false,
  retry,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  diffs: CommitDiff[];
  pending?: boolean;
  error?: boolean;
  retry?: () => void;
}) {
  const { t } = useT("chat");
  const files = useMemo(() => {
    const paths = new Map<string, string>();
    const multipleRepos = new Set(diffs.map((diff) => diff.repository_id)).size > 1;
    for (const diff of diffs) {
      for (const change of diff.changes) {
        paths.set(
          JSON.stringify([diff.repository_id, change.path]),
          multipleRepos ? `${diff.repository}/${change.path}` : change.path,
        );
      }
    }
    return [...paths.values()];
  }, [diffs]);
  const filePreview = files.slice(0, 2).join(", ");
  const repositoriesOmitted = diffs.some((diff) => diff.repositories_omitted);
  const summary = diffs.some((diff) =>
    diff.repositories_omitted || diff.truncated || diff.unavailable ||
    diff.changes.some((change) => change.truncated || change.unavailable),
  )
    ? t(($) => $.commit_changes.incomplete_record)
    : files.length > 0
      ? t(($) => $.commit_changes.recorded_files, { count: files.length })
      : "";
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <div className="my-2 flex min-w-0 items-center gap-2">
        <DialogTrigger render={<Button variant="outline" size="sm" className="max-w-full" />}>
          <FilePen />
          <span className="min-w-0 truncate">
            {t(($) => $.commit_changes.heading)}
            {summary && ` · ${summary}`}
          </span>
        </DialogTrigger>
        {filePreview && (
          <span className="hidden min-w-0 truncate font-mono text-caption text-muted-foreground sm:block" title={filePreview}>
            {filePreview}
          </span>
        )}
      </div>
      <DialogContent className="flex h-[85vh] w-[94vw] max-w-[1100px] flex-col gap-0 overflow-hidden p-0 sm:max-w-[1100px]">
        <DialogHeader className="shrink-0 border-b px-5 py-3 pr-12">
          <DialogTitle>{t(($) => $.commit_changes.heading)}</DialogTitle>
        </DialogHeader>
        {repositoriesOmitted && (
          <p role="status" className="shrink-0 border-b px-5 py-2 text-caption text-muted-foreground">
            {t(($) => $.commit_changes.repositories_omitted)}
          </p>
        )}
        {pending ? (
          <p role="status" className="p-5 text-body text-muted-foreground">
            {t(($) => $.commit_changes.loading)}
          </p>
        ) : error ? (
          <div role="alert" className="p-5">
            <Button variant="outline" onClick={retry}>
              {t(($) => $.commit_changes.retry)}
            </Button>
          </div>
        ) : diffs.length === 0 ? (
          <p className="p-5 text-body text-muted-foreground">
            {t(($) => $.commit_changes.empty)}
          </p>
        ) : (
          <ChangesViewer diffs={diffs} />
        )}
      </DialogContent>
    </Dialog>
  );
}

const comparisonOrder = { worktree: 0, commits: 1, draft: 2 };

function hasRecordedFiles(diff: CommitDiff) {
  return !diff.unavailable && diff.changes.length > 0;
}

function ChangesViewer({ diffs }: { diffs: CommitDiff[] }) {
  const { t } = useT("chat");
  const [selection, setSelection] = useState<{
    repository?: string;
    kind?: CommitDiff["kind"];
    path?: string;
  }>({});
  const comparisonButtons = useRef<Partial<Record<CommitDiff["kind"], HTMLButtonElement | null>>>({});
  const repositories = [
    ...new Map(diffs.map((diff) => [diff.repository_id, diff.repository])).entries(),
  ];
  const repositoryId =
    repositories.find(([id]) => id === selection.repository)?.[0] ??
    diffs.find(hasRecordedFiles)?.repository_id ??
    diffs[0]!.repository_id;
  const comparisons = diffs
    .filter((diff) => diff.repository_id === repositoryId)
    .sort((a, b) => comparisonOrder[a.kind] - comparisonOrder[b.kind]);
  const diff =
    comparisons.find((candidate) => candidate.kind === selection.kind) ??
    comparisons.find(hasRecordedFiles) ??
    comparisons[0]!;
  const unavailableComparisons = diff.unavailable
    ? []
    : comparisons.filter((comparison) => comparison.unavailable);
  const file =
    diff.changes.find((change) => change.path === selection.path) ?? diff.changes[0];
  const lines = useMemo(
    () => (file?.diff ? parseUnifiedDiff(file.diff) : []),
    [file?.diff],
  );
  const label = {
    worktree: t(($) => $.commit_changes.worktree),
    commits: t(($) => $.commit_changes.commits),
    draft: t(($) => $.commit_changes.draft),
  };
  const unavailableLabel = {
    worktree: t(($) => $.commit_changes.worktree_unavailable),
    commits: t(($) => $.commit_changes.commits_unavailable),
    draft: t(($) => $.commit_changes.draft_unavailable),
  };
  const commitRange = diff.kind === "commits" && diff.base && diff.head
    ? `${diff.base.slice(0, 8)} → ${diff.head.slice(0, 8)}`
    : null;

  return (
    <>
      <div className="flex shrink-0 flex-wrap items-center gap-x-3 gap-y-1 border-b px-5 py-2 text-caption">
        {repositories.length > 1 ? (
          <div className="flex min-w-0 flex-wrap gap-1">
            {repositories.map(([id, name]) => (
              <Button
                key={id}
                size="xs"
                variant={id === repositoryId ? "secondary" : "ghost"}
                aria-pressed={id === repositoryId}
                onClick={() => setSelection({ repository: id })}
                className="max-w-full"
                title={name}
              >
                <span className="truncate">{name}</span>
              </Button>
            ))}
          </div>
        ) : (
          <span className="font-mono break-all">{diff.repository}</span>
        )}
        {comparisons.length > 1 ? (
          <div className="flex flex-wrap gap-1">
            {comparisons.map((comparison) => (
              <Button
                key={comparison.kind}
                ref={(button) => {
                  comparisonButtons.current[comparison.kind] = button;
                }}
                size="xs"
                variant={comparison.kind === diff.kind ? "secondary" : "ghost"}
                aria-pressed={comparison.kind === diff.kind}
                onClick={() => setSelection({ repository: repositoryId, kind: comparison.kind })}
              >
                {label[comparison.kind]}
              </Button>
            ))}
          </div>
        ) : (
          <span>{label[diff.kind]}</span>
        )}
        {commitRange && <span className="font-mono text-muted-foreground">{commitRange}</span>}
      </div>
      {unavailableComparisons.length > 0 && (
        <div className="flex shrink-0 flex-wrap gap-x-4 border-b px-5 py-1">
          {unavailableComparisons.map((comparison) => (
            <Button
              key={comparison.kind}
              size="xs"
              variant="link"
              className="h-auto min-w-0 px-0 py-1 whitespace-normal text-left underline underline-offset-2"
              onClick={() => {
                comparisonButtons.current[comparison.kind]?.focus();
                setSelection({ repository: repositoryId, kind: comparison.kind });
              }}
            >
              {unavailableLabel[comparison.kind]}
            </Button>
          ))}
        </div>
      )}
      {diff.unavailable ? (
        <p role="status" className="p-5 text-body text-muted-foreground">
          {unavailableLabel[diff.kind]}
        </p>
      ) : (
        <>
          {diff.truncated && (
            <p role="status" className="border-b px-5 py-2 text-caption text-muted-foreground">
              {t(($) => $.commit_changes.incomplete)}
            </p>
          )}
          {diff.changes.length === 0 && !diff.truncated && (
            <p className="p-5 text-body text-muted-foreground">
              {diff.kind === "worktree"
                ? t(($) => $.commit_changes.worktree_unchanged)
                : t(($) => $.commit_changes.comparison_unchanged)}
            </p>
          )}
          {file && (
            <div className="flex min-h-0 flex-1 flex-col sm:flex-row">
              <nav
                aria-label={t(($) => $.commit_changes.files)}
                className="max-h-40 shrink-0 overflow-auto border-b sm:max-h-none sm:w-64 sm:border-r sm:border-b-0"
              >
                {diff.changes.map((change) => (
                  <button
                    key={change.path}
                    aria-label={change.path}
                    type="button"
                    onClick={() =>
                      setSelection({ repository: repositoryId, kind: diff.kind, path: change.path })
                    }
                    aria-current={change.path === file.path ? "true" : undefined}
                    className={cn(
                      "flex w-full gap-2 px-4 py-3 text-left text-caption hover:bg-muted",
                      change.path === file.path && "bg-muted font-medium",
                    )}
                  >
                    <span
                      className={
                        change.kind === "add"
                          ? "text-success"
                          : change.kind === "delete"
                            ? "text-destructive"
                            : "text-muted-foreground"
                      }
                    >
                      {change.kind === "add" ? "A" : change.kind === "delete" ? "D" : "M"}
                    </span>
                    <span className="break-all">{change.path}</span>
                  </button>
                ))}
              </nav>
              <div className="min-w-0 flex-1 overflow-auto">
                <div className="border-b px-4 py-3 font-mono text-caption break-all">{file.path}</div>
                {file.truncated || file.unavailable ? (
                  <p className="p-4 text-body text-muted-foreground">
                    {t(($) => $.commit_changes.unavailable)}
                  </p>
                ) : lines.length > 0 ? (
                  <DiffDetailSurface path={file.path} lines={lines} />
                ) : (
                  <p className="p-4 text-body text-muted-foreground">
                    {t(($) => $.commit_changes.no_text)}
                  </p>
                )}
              </div>
            </div>
          )}
        </>
      )}
    </>
  );
}
