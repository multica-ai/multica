"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { FileCode2, Folder, GitCompareArrows, X } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@multica/ui/components/ui/sheet";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  issueCodeChangesOptions,
  parseDiffLines,
  type CodeChangeFile,
} from "@multica/core/issues";
import { useT } from "../../i18n";

export function CodeChangesSection({ issueId }: { issueId: string }) {
  const workspace = useCurrentWorkspace();
  if (
    (workspace?.settings as Record<string, unknown>)?.code_changes_enabled !==
    true
  )
    return null;
  return <EnabledCodeChanges key={issueId} issueId={issueId} />;
}

function EnabledCodeChanges({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const [open, setOpen] = useState(false);
  const [taskId, setTaskId] = useState<string>();
  const [filePath, setFilePath] = useState<string>();
  const { data, isPending, error, refetch } = useQuery(
    issueCodeChangesOptions(wsId, issueId, taskId),
  );
  const snapshot = data?.snapshot;
  const files = snapshot?.files ?? [];
  const selected = files.find((f) => f.path === filePath) ?? files[0];
  return (
    <section className="space-y-2" aria-label={t(($) => $.code_changes.title)}>
      <div className="flex items-center justify-between px-2 text-caption font-medium">
        <span className="flex items-center gap-1.5">
          <GitCompareArrows className="size-3.5 text-muted-foreground" />
          {t(($) => $.code_changes.title)}
        </span>
        {data?.available && (
          <span className="text-muted-foreground tabular-nums">
            {files.length}
          </span>
        )}
      </div>
      <Button
        variant="ghost"
        size="sm"
        className="w-full justify-start text-caption"
        onClick={() => setOpen(true)}
      >
        <FileCode2 className="size-3.5" />
        {t(($) => $.code_changes.open)}
      </Button>
      <Sheet open={open} onOpenChange={setOpen}>
        <SheetContent
          className="data-[side=right]:w-full gap-0 sm:max-w-[min(94vw,1100px)]"
          showCloseButton={false}
        >
          <SheetHeader className="border-b pr-14">
            <SheetTitle>{t(($) => $.code_changes.title)}</SheetTitle>
            <SheetDescription>
              {t(($) => $.code_changes.per_run)}
            </SheetDescription>
            <SheetClose
              render={
                <Button
                  variant="ghost"
                  size="icon-sm"
                  className="absolute right-4 top-4"
                  aria-label={t(($) => $.code_changes.close)}
                />
              }
            >
              <X />
            </SheetClose>
          </SheetHeader>
          <div className="flex min-h-0 flex-1 flex-col">
            {data && data.runs.length > 0 && (
              <div className="space-y-2 border-b bg-muted/20 p-4">
                <label className="flex flex-wrap items-center gap-3 text-caption">
                  {t(($) => $.code_changes.history)}
                  <select
                    className="max-w-full rounded-md border bg-background px-2 py-1.5 font-mono text-caption"
                    value={data.sourceTaskId ?? ""}
                    onChange={(event) => {
                      setTaskId(event.target.value);
                      setFilePath(undefined);
                    }}
                  >
                    {data.runs.map((run) => (
                      <option key={run.taskId} value={run.taskId}>
                        {new Date(run.createdAt).toLocaleString()} ·{" "}
                        {run.taskId.slice(-8)} · {run.fileCount} ·{" "}
                        {run.branchName || run.status}
                      </option>
                    ))}
                  </select>
                </label>
                {snapshot?.baseRef && (
                  <p className="font-mono text-micro text-muted-foreground">
                    {(selected?.baseRef ?? snapshot.baseRef).slice(0, 10)} →{" "}
                    {(selected?.headRef ?? snapshot.headRef).slice(0, 10)}
                  </p>
                )}
              </div>
            )}
            {isPending ? (
              <p
                role="status"
                className="p-6 text-caption text-muted-foreground"
              >
                {t(($) => $.code_changes.loading)}
              </p>
            ) : error ? (
              <div role="alert" className="space-y-3 p-6 text-caption">
                <p>{t(($) => $.code_changes.error)}</p>
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => void refetch()}
                >
                  {t(($) => $.code_changes.retry)}
                </Button>
              </div>
            ) : !data?.available ? (
              <p className="p-6 text-caption text-muted-foreground">
                {t(($) => $.code_changes.not_captured)}
              </p>
            ) : (
              <>
                {snapshot?.reason && (
                  <p role="status" className="border-b p-4 text-caption">
                    {snapshot.reason === "no_repository"
                      ? t(($) => $.code_changes.no_repository)
                      : t(($) => $.code_changes.capture_failed)}
                  </p>
                )}
                {snapshot?.workingTreeDirty && (
                  <p
                    role="status"
                    className="border-b bg-muted/30 p-4 text-caption"
                  >
                    {t(($) => $.code_changes.dirty)}
                  </p>
                )}
                {snapshot?.truncated && (
                  <p
                    role="status"
                    className="border-b bg-muted/30 p-4 text-caption"
                  >
                    {t(($) => $.code_changes.truncated)}
                  </p>
                )}
                {files.length === 0 && !snapshot?.reason ? (
                  <p className="p-6 text-caption text-muted-foreground">
                    {t(($) => $.code_changes.empty)}
                  </p>
                ) : (
                  files.length > 0 && (
                    <div className="flex min-h-0 flex-1 flex-col md:flex-row">
                      <nav
                        aria-label={t(($) => $.code_changes.files)}
                        className="max-h-44 shrink-0 overflow-auto border-b bg-muted/10 p-2 md:max-h-none md:w-64 md:border-r md:border-b-0"
                      >
                        <FileTree
                          files={files}
                          selected={selected?.path}
                          onSelect={setFilePath}
                        />
                      </nav>
                      <div className="flex min-h-0 min-w-0 flex-1 flex-col">
                        {selected && (
                          <>
                            <div className="flex flex-wrap items-center gap-3 border-b px-4 py-3 font-mono text-caption">
                              <span className="break-all">{selected.path}</span>
                              <span className="text-success">
                                +{selected.additions}
                              </span>
                              <span className="text-destructive">
                                −{selected.deletions}
                              </span>
                            </div>
                            <DiffBody
                              key={`${data.sourceTaskId}:${selected.path}`}
                              file={selected}
                            />
                          </>
                        )}
                      </div>
                    </div>
                  )
                )}
              </>
            )}
          </div>
        </SheetContent>
      </Sheet>
    </section>
  );
}

function FileTree({
  files,
  selected,
  onSelect,
  prefix = "",
}: {
  files: CodeChangeFile[];
  selected?: string;
  onSelect: (path: string) => void;
  prefix?: string;
}) {
  const groups = new Map<string, CodeChangeFile[]>();
  const leaves: CodeChangeFile[] = [];
  for (const file of files) {
    const rest = file.path.slice(prefix.length);
    const slash = rest.indexOf("/");
    if (slash < 0) leaves.push(file);
    else {
      const folder = rest.slice(0, slash);
      groups.set(folder, [...(groups.get(folder) ?? []), file]);
    }
  }
  return (
    <ul className="space-y-0.5 text-caption">
      {[...groups].map(([name, children]) => (
        <li key={name}>
          <details open>
            <summary className="cursor-pointer rounded-md px-2 py-1.5 hover:bg-accent">
              <Folder className="mr-1.5 inline size-3.5 text-muted-foreground" />
              {name}
            </summary>
            <div className="ml-3 border-l pl-1">
              <FileTree
                files={children}
                selected={selected}
                onSelect={onSelect}
                prefix={`${prefix}${name}/`}
              />
            </div>
          </details>
        </li>
      ))}
      {leaves.map((file) => (
        <li key={file.path}>
          <button
            type="button"
            aria-label={file.path}
            aria-current={selected === file.path ? "true" : undefined}
            className={`flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left hover:bg-accent ${selected === file.path ? "bg-accent text-accent-foreground" : "text-muted-foreground"}`}
            onClick={() => onSelect(file.path)}
          >
            <FileCode2 className="size-3.5 shrink-0" />
            <span className="min-w-0 flex-1 truncate">
              {file.path.slice(prefix.length)}
            </span>
            <span className="font-mono text-micro">{file.status}</span>
          </button>
        </li>
      ))}
    </ul>
  );
}

function DiffBody({ file }: { file: CodeChangeFile }) {
  const { t } = useT("issues");
  const [visibleLines, setVisibleLines] = useState(2000);
  if (file.binary)
    return (
      <p className="p-6 text-caption text-muted-foreground">
        {t(($) => $.code_changes.binary)}
      </p>
    );
  const lines = parseDiffLines(file.diff);
  return (
    <div className="min-h-0 flex-1 overflow-auto">
      {file.truncated && (
        <p role="status" className="p-4 text-caption text-muted-foreground">
          {t(($) => $.code_changes.file_truncated)}
        </p>
      )}
      <table
        aria-label={file.path}
        className="w-full border-collapse font-mono text-caption leading-5"
      >
        <thead className="sr-only">
          <tr>
            <th>{t(($) => $.code_changes.old_line)}</th>
            <th>{t(($) => $.code_changes.new_line)}</th>
            <th>{t(($) => $.code_changes.content)}</th>
          </tr>
        </thead>
        <tbody>
          {lines.slice(0, visibleLines).map((line, index) => (
            <tr
              key={index}
              className={
                line.kind === "add"
                  ? "bg-success/10"
                  : line.kind === "remove"
                    ? "bg-destructive/10"
                    : line.kind === "header"
                      ? "bg-muted/40 text-muted-foreground"
                      : ""
              }
            >
              <td className="w-12 select-none border-r border-border/40 px-2 text-right align-top text-muted-foreground">
                {line.oldLine}
              </td>
              <td className="w-12 select-none border-r border-border/40 px-2 text-right align-top text-muted-foreground">
                {line.newLine}
              </td>
              <td className="whitespace-pre px-3">{line.text}</td>
            </tr>
          ))}
        </tbody>
      </table>
      {lines.length > visibleLines && (
        <Button
          variant="outline"
          size="sm"
          className="m-4"
          onClick={() => setVisibleLines((n) => n + 2000)}
        >
          {t(($) => $.code_changes.more)}
        </Button>
      )}
    </div>
  );
}
