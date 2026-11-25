"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { taskCodeChangesOptions } from "@multica/core/agents";
import type { AgentTask, TaskCodeChangeFile, TaskCodeChangeRepo } from "@multica/core/types/agent";
import { Dialog, DialogClose, DialogContent, DialogTitle, DialogTrigger } from "@multica/ui/components/ui/dialog";
import { Tooltip, TooltipContent, TooltipTrigger } from "@multica/ui/components/ui/tooltip";
import { FileCode, GitBranch, Loader2, X } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { parseUnifiedDiff } from "../task-transcript/trace-event-presenter";
import { renderDiffRows } from "../task-transcript/detail-surfaces";
import { highlightToLines, languageForPath } from "../task-transcript/diff-highlight";
import { useT, useTimeAgo } from "../../i18n";
import { splitPatchByFile } from "./split-patch";

// What a run changed in the git checkouts under its working directory, as the
// daemon recorded it when the run ended: file list on the left, the selected
// file's diff on the right. The daemon records once per run, so the button
// belongs on finished runs only.

/** Opens the run's code changes. The dialog fetches when it opens. */
export function CodeChangesButton({ task }: { task: Pick<AgentTask, "id"> }) {
  const { t } = useT("agents");
  const [open, setOpen] = useState(false);
  const label = t(($) => $.code_changes.open_tooltip);
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <Tooltip>
        <TooltipTrigger
          render={<DialogTrigger render={<button type="button" />} />}
          aria-label={label}
          className="flex items-center justify-center rounded-xs p-1 text-muted-foreground transition-colors hover:bg-accent/50 hover:text-foreground"
        >
          <FileCode className="h-3.5 w-3.5" />
        </TooltipTrigger>
        <TooltipContent>{label}</TooltipContent>
      </Tooltip>
      {open && <CodeChangesDialogContent taskId={task.id} />}
    </Dialog>
  );
}

interface Selection {
  repo: number;
  path: string;
}

function CodeChangesDialogContent({ taskId }: { taskId: string }) {
  const { t } = useT("agents");
  const timeAgo = useTimeAgo();
  const wsId = useWorkspaceId();
  const { data, isLoading, isError } = useQuery(taskCodeChangesOptions(wsId, taskId));
  const repos = useMemo(() => data?.repos ?? [], [data]);
  const [picked, setPicked] = useState<Selection | null>(null);
  const firstFile = repos.findIndex((r) => r.files.length > 0);
  const selection =
    picked ?? (firstFile >= 0 ? { repo: firstFile, path: repos[firstFile]!.files[0]!.path } : null);
  const selectedRepo = selection ? repos[selection.repo] : undefined;
  const selectedFile = selectedRepo?.files.find((f) => f.path === selection?.path);
  const totalFiles = repos.reduce((n, r) => n + r.files.length, 0);

  return (
    <DialogContent
      className="!max-w-6xl !w-[calc(100%-4rem)] !max-h-[calc(100%-4rem)] !h-[calc(100%-4rem)] flex flex-col !p-0 !gap-0 overflow-hidden"
      showCloseButton={false}
    >
      <div className="flex shrink-0 items-center gap-3 border-b px-4 py-3">
        <FileCode className="size-4 text-muted-foreground" aria-hidden />
        <DialogTitle className="text-body font-medium">{t(($) => $.code_changes.title)}</DialogTitle>
        {totalFiles > 0 && (
          <span className="text-caption text-muted-foreground">
            {t(($) => $.code_changes.file_count, { count: totalFiles })}
          </span>
        )}
        {data?.updated_at && (
          <span className="text-caption text-muted-foreground">
            {t(($) => $.code_changes.recorded, { when: timeAgo(data.updated_at) })}
          </span>
        )}
        <DialogClose
          aria-label={t(($) => $.code_changes.close)}
          className="ml-auto rounded-xs p-1 text-muted-foreground transition-colors hover:bg-accent/50 hover:text-foreground"
        >
          <X className="size-4" />
        </DialogClose>
      </div>

      {isLoading ? (
        <div className="flex flex-1 items-center justify-center text-muted-foreground">
          <Loader2 className="size-4 animate-spin" />
        </div>
      ) : isError ? (
        <Notice>{t(($) => $.code_changes.load_failed)}</Notice>
      ) : totalFiles === 0 ? (
        <Notice>
          {data?.updated_at ? t(($) => $.code_changes.none) : t(($) => $.code_changes.not_recorded)}
        </Notice>
      ) : (
        <div className="flex min-h-0 flex-1">
          <nav
            aria-label={t(($) => $.code_changes.file_list)}
            className="w-72 shrink-0 overflow-y-auto border-r py-2 max-md:w-48"
          >
            {data?.repos_truncated && (
              <p role="note" className="mx-3 mb-2 rounded-xs bg-warning/10 px-2 py-1.5 text-micro text-warning">
                {t(($) => $.code_changes.repos_truncated)}
              </p>
            )}
            {repos.map((repo, ri) => (
              <RepoFiles
                key={`${repo.path}:${ri}`}
                repo={repo}
                showHeader={repos.length > 1 || repo.path !== "."}
                selectedPath={selection?.repo === ri ? selection.path : undefined}
                onSelect={(path) => setPicked({ repo: ri, path })}
              />
            ))}
          </nav>
          <div className="min-w-0 flex-1 overflow-y-auto">
            {selectedRepo && selectedFile && <FileDiff repo={selectedRepo} file={selectedFile} />}
          </div>
        </div>
      )}
    </DialogContent>
  );
}

function Notice({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex flex-1 items-center justify-center px-6 py-6 text-center text-body text-muted-foreground">
      {children}
    </div>
  );
}

function RepoFiles({
  repo,
  showHeader,
  selectedPath,
  onSelect,
}: {
  repo: TaskCodeChangeRepo;
  showHeader: boolean;
  selectedPath?: string;
  onSelect: (path: string) => void;
}) {
  const { t } = useT("agents");
  if (repo.files.length === 0) return null;
  return (
    <div className="mb-2">
      {showHeader && (
        <div className="flex min-w-0 items-center gap-1.5 px-3 py-1 text-caption text-muted-foreground">
          <span className="truncate font-medium text-foreground" title={repo.path}>
            {repo.path === "." ? "/" : repo.path}
          </span>
          {repo.branch && (
            <span className="inline-flex min-w-0 items-center gap-1">
              <GitBranch className="size-3 shrink-0" aria-hidden />
              <span className="truncate font-mono text-micro">{repo.branch}</span>
            </span>
          )}
        </div>
      )}
      <ul>
        {repo.files.map((file) => (
          <li key={file.path}>
            <button
              type="button"
              onClick={() => onSelect(file.path)}
              aria-current={file.path === selectedPath ? "true" : undefined}
              title={file.old_path ? `${file.old_path} → ${file.path}` : file.path}
              className={cn(
                "flex w-full min-w-0 items-center gap-2 px-3 py-1 text-left font-mono text-micro transition-colors hover:bg-accent/50",
                file.path === selectedPath && "bg-accent text-foreground hover:bg-accent",
              )}
            >
              <StatusMark status={file.status} />
              <span className="min-w-0 flex-1 truncate">{file.path}</span>
              {!file.binary && (
                <span className="shrink-0 tabular-nums">
                  {file.additions > 0 && <span className="text-success">+{file.additions}</span>}
                  {file.additions > 0 && file.deletions > 0 && " "}
                  {file.deletions > 0 && <span className="text-destructive">-{file.deletions}</span>}
                </span>
              )}
            </button>
          </li>
        ))}
      </ul>
      {repo.files_truncated && (
        <p className="px-3 py-1 text-micro text-muted-foreground">{t(($) => $.code_changes.files_truncated)}</p>
      )}
    </div>
  );
}

function StatusMark({ status }: { status: TaskCodeChangeFile["status"] }) {
  const { t } = useT("agents");
  const letter = status === "added" ? "A" : status === "deleted" ? "D" : status === "renamed" ? "R" : "M";
  const label = t(($) => $.code_changes.status[status]);
  return (
    <span
      title={label}
      className={cn(
        "w-3 shrink-0 text-center font-semibold",
        status === "added" && "text-success",
        status === "deleted" && "text-destructive",
        (status === "modified" || status === "renamed") && "text-warning",
      )}
    >
      <span aria-hidden>{letter}</span>
      <span className="sr-only">{label}</span>
    </span>
  );
}

function FileDiff({ repo, file }: { repo: TaskCodeChangeRepo; file: TaskCodeChangeFile }) {
  const { t } = useT("agents");
  const sections = useMemo(() => splitPatchByFile(repo.patch), [repo.patch]);
  const section = sections.get(file.path) ?? (file.old_path ? sections.get(file.old_path) : undefined);
  // Only the hunks: git's header lines (`new file mode`, `similarity index`,
  // ...) are metadata, not file content.
  const lines = useMemo(() => {
    const hunks = section ? section.indexOf("\n@@") : -1;
    if (!section || hunks < 0) return [];
    const parsed = parseUnifiedDiff(section.slice(hunks + 1));
    // A leading gap would mark skipped content above the first hunk.
    return parsed[0]?.kind === "gap" ? parsed.slice(1) : parsed;
  }, [section]);
  const highlighted = useMemo(() => {
    const language = languageForPath(file.path);
    if (!language || lines.length === 0) return null;
    const sides: Record<"add" | "remove" | "context", string[] | null> = { add: null, remove: null, context: null };
    for (const kind of ["add", "remove", "context"] as const) {
      const side = lines.filter((l) => l.kind === kind);
      if (side.length > 0) sides[kind] = highlightToLines(side.map((l) => l.text).join("\n"), language);
    }
    return sides;
  }, [lines, file.path]);

  return (
    <div>
      <div className="sticky top-0 z-10 flex min-w-0 items-center gap-2 border-b bg-background px-4 py-2 font-mono text-micro">
        {file.old_path && (
          <>
            <span className="truncate text-muted-foreground">{file.old_path}</span>
            <span className="shrink-0 text-faint-foreground" aria-hidden>
              →
            </span>
          </>
        )}
        <span className="truncate">{file.path}</span>
      </div>
      {file.binary ? (
        <p className="px-4 py-6 text-body text-muted-foreground">{t(($) => $.code_changes.binary)}</p>
      ) : lines.length > 0 ? (
        <pre className="transcript-code px-4 py-3 font-mono text-micro whitespace-pre-wrap break-all">
          {renderDiffRows(lines, highlighted)}
        </pre>
      ) : (
        <p className="px-4 py-6 text-body text-muted-foreground">
          {repo.patch_truncated && !section
            ? t(($) => $.code_changes.patch_truncated)
            : t(($) => $.code_changes.no_content)}
        </p>
      )}
    </div>
  );
}
