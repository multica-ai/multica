"use client";

import { useMemo, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ChevronLeft,
  File,
  Folder,
  FolderUp,
  RefreshCw,
  Sparkles,
  Upload,
} from "lucide-react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import type {
  ProjectSpaceImport,
  ProjectSpaceImportFile,
} from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../i18n";

const MAX_FILES = 5_000;
const MAX_BATCH_BYTES = 5 * 1024 * 1024 * 1024;
const MAX_FILE_BYTES = 100 * 1024 * 1024;
const UPLOAD_CONCURRENCY = 3;

export type BrowserFile = File & { webkitRelativePath?: string };

function displayPath(file: BrowserFile): string {
  return file.webkitRelativePath || file.name;
}

function validateSelection(files: BrowserFile[]): string | null {
  if (files.length === 0) return "empty";
  if (files.length > MAX_FILES) return "count_limit";
  let total = 0;
  for (const file of files) {
    if (file.size > MAX_FILE_BYTES) return "file_too_large";
    total += file.size;
    if (total > MAX_BATCH_BYTES) return "batch_too_large";
  }
  return null;
}

async function runPool<T>(
  items: T[],
  concurrency: number,
  worker: (item: T) => Promise<void>,
): Promise<void> {
  let cursor = 0;
  const runners = Array.from(
    { length: Math.min(concurrency, items.length) },
    async () => {
      while (cursor < items.length) {
        const item = items[cursor++];
        if (item !== undefined) await worker(item);
      }
    },
  );
  await Promise.all(runners);
}

function batchName(files: BrowserFile[]): string {
  const first = displayPath(files[0] ?? ({} as BrowserFile));
  const folder = first.includes("/") ? first.split("/")[0] : "";
  if (folder) return folder.slice(0, 120);
  return `upload-${new Date().toISOString().replace(/[:.]/g, "-").slice(0, 19)}`;
}

export function pathInsideBatch(file: BrowserFile, batch: string): string {
  const original = displayPath(file);
  const prefix = `${batch}/`;
  return file.webkitRelativePath && original.startsWith(prefix)
    ? original.slice(prefix.length)
    : original;
}

function importProgress(item: ProjectSpaceImport): number {
  if (item.total_files === 0) return 0;
  return Math.round(
    ((item.completed_files + item.failed_files) / item.total_files) * 100,
  );
}

export function ProjectSpacePanel({ projectId }: { projectId: string }) {
  const { t } = useT("projects");
  const workspaceId = useWorkspaceId();
  const queryClient = useQueryClient();
  const fileInput = useRef<HTMLInputElement>(null);
  const folderInput = useRef<HTMLInputElement>(null);
  const [path, setPath] = useState("");
  const [uploading, setUploading] = useState(false);
  const [organizing, setOrganizing] = useState(false);
  const [selectedAgentId, setSelectedAgentId] = useState("");
  const [localProgress, setLocalProgress] = useState(0);

  const filesQueryKey = useMemo(
    () => ["project-space", projectId, "files", path],
    [projectId, path],
  );
  const importsQueryKey = useMemo(
    () => ["project-space", projectId, "imports"],
    [projectId],
  );
  const files = useQuery({
    queryKey: filesQueryKey,
    queryFn: () => api.listProjectSpaceFiles(projectId, path),
  });
  const project = useQuery({
    queryKey: ["project", projectId],
    queryFn: () => api.getProject(projectId),
  });
  const agents = useQuery({
    queryKey: ["agents", workspaceId, "project-space-organize"],
    queryFn: () => api.listAgents({ workspace_id: workspaceId }),
  });
  const imports = useQuery({
    queryKey: importsQueryKey,
    queryFn: () => api.listProjectSpaceImports(projectId),
    refetchInterval: (query) =>
      query.state.data?.imports.some((item) =>
        item.status === "queued" || item.status === "uploading",
      )
        ? 2_000
        : false,
  });

  const uploadSelection = async (selected: FileList | null) => {
    const browserFiles = Array.from(selected ?? []) as BrowserFile[];
    const validation = validateSelection(browserFiles);
    if (validation) {
      const key = `upload_${validation}` as
        | "upload_empty"
        | "upload_count_limit"
        | "upload_file_too_large"
        | "upload_batch_too_large";
      toast.error(t(($) => $.resources[key]));
      return;
    }

    setUploading(true);
    setLocalProgress(0);
    try {
      const batch = batchName(browserFiles);
      const uploadItems = browserFiles.map((file) => ({
        file,
        relativePath: pathInsideBatch(file, batch),
      }));
      const created = await api.createProjectSpaceImport(projectId, {
        batch_name: batch,
        files: uploadItems.map(({ file, relativePath }) => ({
          relative_path: relativePath,
          content_type: file.type || "application/octet-stream",
          size_bytes: file.size,
        })),
      });
      const byPath = new Map(
        created.files.map((file: ProjectSpaceImportFile) => [
          file.relative_path,
          file,
        ]),
      );
      let finished = 0;
      const failures: unknown[] = [];
      await runPool(uploadItems, UPLOAD_CONCURRENCY, async ({ file, relativePath }) => {
        const manifestFile = byPath.get(relativePath);
        if (!manifestFile) {
          failures.push(new Error("missing import manifest entry"));
          return;
        }
        try {
          await api.uploadProjectSpaceImportFile(
            projectId,
            created.id,
            manifestFile.id,
            file,
          );
        } catch (error) {
          failures.push(error);
        } finally {
          finished += 1;
          setLocalProgress(Math.round((finished / uploadItems.length) * 100));
        }
      });
      const completed = await api.completeProjectSpaceImport(projectId, created.id);
      if (failures.length > 0 || completed.status === "partial" || completed.status === "failed") {
        toast.warning(t(($) => $.resources.upload_partial));
      } else {
        toast.success(t(($) => $.resources.upload_complete));
      }
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["project-space", projectId, "files"] }),
        queryClient.invalidateQueries({ queryKey: importsQueryKey }),
      ]);
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t(($) => $.resources.upload_failed),
      );
    } finally {
      setUploading(false);
      setLocalProgress(0);
      if (fileInput.current) fileInput.current.value = "";
      if (folderInput.current) folderInput.current.value = "";
    }
  };

  const parentPath = path.includes("/")
    ? path.slice(0, path.lastIndexOf("/"))
    : "";
  const activeImport = imports.data?.imports.find(
    (item) => item.status === "queued" || item.status === "uploading",
  );
  const progress = uploading
    ? localProgress
    : activeImport
      ? importProgress(activeImport)
      : null;
  const leadAgentId =
    project.data?.lead_type === "agent" ? project.data.lead_id ?? "" : "";
  const organizeAgentId = selectedAgentId || leadAgentId;
  const canOrganize = Boolean(organizeAgentId);

  const organize = async () => {
    if (!canOrganize) {
      toast.error(t(($) => $.resources.organize_needs_agent));
      return;
    }
    setOrganizing(true);
    try {
      await api.organizeProjectSpace(projectId, organizeAgentId);
      toast.success(t(($) => $.resources.organize_started));
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t(($) => $.resources.organize_failed),
      );
    } finally {
      setOrganizing(false);
    }
  };

  return (
    <div className="rounded-md border bg-muted/20 p-2 space-y-2">
      <input
        ref={fileInput}
        type="file"
        multiple
        className="hidden"
        onChange={(event) => void uploadSelection(event.target.files)}
      />
      <input
        ref={(node) => {
          folderInput.current = node;
          node?.setAttribute("webkitdirectory", "");
          node?.setAttribute("directory", "");
        }}
        type="file"
        multiple
        className="hidden"
        onChange={(event) => void uploadSelection(event.target.files)}
      />
      <div className="flex flex-wrap gap-1">
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="h-7 text-xs"
          disabled={uploading}
          onClick={() => fileInput.current?.click()}
        >
          <Upload className="size-3" />
          {t(($) => $.resources.upload_files)}
        </Button>
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="h-7 text-xs"
          disabled={uploading}
          onClick={() => folderInput.current?.click()}
        >
          <FolderUp className="size-3" />
          {t(($) => $.resources.upload_folder)}
        </Button>
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          disabled={files.isFetching}
          onClick={() => void files.refetch()}
          title={t(($) => $.resources.refresh_space)}
        >
          <RefreshCw className="size-3" />
        </Button>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="h-7 text-xs"
          disabled={organizing || !canOrganize}
          onClick={() => void organize()}
          title={
            canOrganize
              ? t(($) => $.resources.organize)
              : t(($) => $.resources.organize_needs_agent)
          }
        >
          <Sparkles className="size-3" />
          {t(($) => $.resources.organize)}
        </Button>
      </div>
      {!leadAgentId && (
        <select
          value={selectedAgentId}
          onChange={(event) => setSelectedAgentId(event.target.value)}
          className="h-7 w-full rounded-md border bg-background px-2 text-xs"
          aria-label={t(($) => $.resources.organize_agent)}
        >
          <option value="">{t(($) => $.resources.organize_agent)}</option>
          {agents.data?.map((agent) => (
            <option key={agent.id} value={agent.id}>
              {agent.name}
            </option>
          ))}
        </select>
      )}
      {progress !== null && (
        <div className="space-y-1">
          <div className="flex justify-between text-[10px] text-muted-foreground">
            <span>{t(($) => $.resources.upload_progress)}</span>
            <span>{progress}%</span>
          </div>
          <div className="h-1 overflow-hidden rounded-full bg-muted">
            <div
              className="h-full bg-primary transition-[width]"
              style={{ width: `${progress}%` }}
            />
          </div>
        </div>
      )}
      <div className="flex min-w-0 items-center gap-1 text-[10px] text-muted-foreground">
        {path && (
          <button
            type="button"
            className="rounded p-0.5 hover:bg-accent"
            onClick={() => setPath(parentPath)}
            title={t(($) => $.resources.space_parent)}
          >
            <ChevronLeft className="size-3" />
          </button>
        )}
        <span className="truncate">/{path}</span>
      </div>
      {files.isError && (
        <p className="text-xs text-destructive">
          {t(($) => $.resources.space_unavailable)}
        </p>
      )}
      {!files.isError && (files.data?.entries.length ?? 0) === 0 && (
        <p className="text-xs text-muted-foreground">
          {t(($) => $.resources.space_empty)}
        </p>
      )}
      <div className="max-h-48 space-y-0.5 overflow-y-auto">
        {files.data?.entries.map((entry) => (
          <button
            key={entry.relative_path}
            type="button"
            disabled={entry.kind !== "directory"}
            onClick={() => {
              if (entry.kind === "directory") setPath(entry.relative_path);
            }}
            className="flex w-full items-center gap-1.5 rounded px-1.5 py-1 text-left text-xs hover:bg-accent disabled:cursor-default disabled:hover:bg-transparent"
            title={entry.relative_path}
          >
            {entry.kind === "directory" ? (
              <Folder className="size-3.5 shrink-0 text-muted-foreground" />
            ) : (
              <File className="size-3.5 shrink-0 text-muted-foreground" />
            )}
            <span className="min-w-0 flex-1 truncate">{entry.name}</span>
            {entry.kind === "file" && (
              <span className="text-[10px] text-muted-foreground">
                {formatBytes(entry.size_bytes)}
              </span>
            )}
          </button>
        ))}
      </div>
      {imports.data?.imports.some((item) => item.status === "partial" || item.status === "failed") && (
        <p className="text-[10px] text-amber-700 dark:text-amber-400">
          {t(($) => $.resources.upload_retry_hint)}
        </p>
      )}
    </div>
  );
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}
