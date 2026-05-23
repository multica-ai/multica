"use client";

import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight, FolderGit, FolderOpen, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import {
  projectResourcesOptions,
  useCreateProjectResource,
  useDeleteProjectResource,
} from "@multica/core/projects";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentWorkspace } from "@multica/core/paths";
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import type {
  GithubRepoResourceRef,
  LocalPathResourceRef,
  ProjectResource,
} from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  NativeSelect,
  NativeSelectOption,
} from "@multica/ui/components/ui/native-select";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
} from "@multica/ui/components/ui/tooltip";
import { useT } from "../../i18n";

// Project Resources sidebar section.
//
// Renders github_repo and local_path resources. The rendering layer is
// type-dispatched so adding a new type means: (1) extend the API validator,
// (2) add a render case here. No changes to the schema or query layer.
export function ProjectResourcesSection({ projectId }: { projectId: string }) {
  const { t } = useT("projects");
  const wsId = useWorkspaceId();
  const workspace = useCurrentWorkspace();
  const [open, setOpen] = useState(true);
  const [addOpen, setAddOpen] = useState(false);

  const { data: resources = [] } = useQuery(
    projectResourcesOptions(wsId, projectId),
  );
  const createResource = useCreateProjectResource(wsId, projectId);
  const deleteResource = useDeleteProjectResource(wsId, projectId);

  // Fetch online runtimes for the local_path daemon picker.
  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));
  const onlineRuntimes = runtimes.filter((r) => r.status === "online");

  const attachedUrls = new Set(
    resources
      .filter((r) => r.resource_type === "github_repo")
      .map((r) => (r.resource_ref as GithubRepoResourceRef).url),
  );
  const attachedPaths = new Set(
    resources
      .filter((r) => r.resource_type === "local_path")
      .map((r) => (r.resource_ref as LocalPathResourceRef).path),
  );

  const handleAttach = async (url: string) => {
    try {
      await createResource.mutateAsync({
        resource_type: "github_repo",
        resource_ref: { url },
      });
      toast.success(t(($) => $.resources.toast_attached));
    } catch (err) {
      const msg = err instanceof Error ? err.message : t(($) => $.resources.toast_attach_failed);
      toast.error(msg);
    }
  };

  const handleAddLocalPath = async (path: string, daemonId: string, label?: string) => {
    try {
      await createResource.mutateAsync({
        resource_type: "local_path",
        resource_ref: { path, daemon_id: daemonId },
        ...(label?.trim() ? { label: label.trim() } : {}),
      });
      toast.success(t(($) => $.resources.toast_attached));
    } catch (err) {
      const msg = err instanceof Error ? err.message : t(($) => $.resources.toast_attach_failed);
      toast.error(msg);
    }
  };

  const handleRemove = async (resource: ProjectResource) => {
    try {
      await deleteResource.mutateAsync(resource.id);
      toast.success(t(($) => $.resources.toast_removed));
    } catch (err) {
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.resources.toast_remove_failed),
      );
    }
  };

  return (
    <div>
      <button
        className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors mb-2 hover:bg-accent/70 ${open ? "" : "text-muted-foreground hover:text-foreground"}`}
        onClick={() => setOpen(!open)}
      >
        {t(($) => $.resources.section_header)}
        <ChevronRight
          className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${open ? "rotate-90" : ""}`}
        />
      </button>
      {open && (
        <div className="pl-2 space-y-1.5">
          {resources.length === 0 && (
            <p className="text-xs text-muted-foreground">
              {t(($) => $.resources.empty)}
            </p>
          )}
          {resources.map((resource) => (
            <ResourceRow
              key={resource.id}
              resource={resource}
              runtimes={onlineRuntimes}
              onRemove={() => handleRemove(resource)}
            />
          ))}
          <Popover open={addOpen} onOpenChange={setAddOpen}>
            <PopoverTrigger
              render={
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-7 px-2 text-xs text-muted-foreground hover:text-foreground"
                >
                  <Plus className="size-3" />
                  {t(($) => $.resources.add_button)}
                </Button>
              }
            />
            <PopoverContent align="start" className="w-72 p-2 space-y-2">
              <div className="text-xs font-medium text-muted-foreground">
                {t(($) => $.resources.popover_title)}
              </div>
              {workspace?.repos && workspace.repos.length > 0 && (
                <div className="space-y-1 max-h-48 overflow-y-auto">
                  {workspace.repos.map((repo) => {
                    const isAttached = attachedUrls.has(repo.url);
                    const isDisabled = isAttached || createResource.isPending;
                    return (
                      <button
                        key={repo.url}
                        type="button"
                        aria-disabled={isDisabled}
                        onClick={async () => {
                          if (isDisabled) return;
                          await handleAttach(repo.url);
                          setAddOpen(false);
                        }}
                        className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-xs text-left hover:bg-accent transition-colors aria-disabled:opacity-50 aria-disabled:cursor-not-allowed aria-disabled:hover:bg-transparent"
                      >
                        <FolderGit className="size-3.5" />
                        <Tooltip>
                          <TooltipTrigger
                            render={
                              <span className="truncate flex-1">{repo.url}</span>
                            }
                          />
                          <TooltipContent side="top">{repo.url}</TooltipContent>
                        </Tooltip>
                        {isAttached && (
                          <span className="text-[10px] text-muted-foreground">
                            {t(($) => $.resources.attached_badge)}
                          </span>
                        )}
                      </button>
                    );
                  })}
                </div>
              )}
              <CustomRepoForm
                onSubmit={async (url) => {
                  await handleAttach(url);
                  setAddOpen(false);
                }}
              />
              <LocalPathForm
                attachedPaths={attachedPaths}
                runtimes={onlineRuntimes}
                onSubmit={async (path, daemonId, label) => {
                  await handleAddLocalPath(path, daemonId, label);
                  setAddOpen(false);
                }}
                disabled={createResource.isPending}
              />
            </PopoverContent>
          </Popover>
        </div>
      )}
    </div>
  );
}

function ResourceRow({
  resource,
  runtimes,
  onRemove,
}: {
  resource: ProjectResource;
  runtimes: Array<{ id: string; daemon_id: string | null; name: string; device_info: string }>;
  onRemove: () => void;
}) {
  const { t } = useT("projects");
  if (resource.resource_type === "github_repo") {
    const ref = resource.resource_ref as GithubRepoResourceRef;
    return (
      <div className="flex items-center gap-2 text-xs group">
        <FolderGit className="size-3.5 text-muted-foreground shrink-0" />
        <Tooltip>
          <TooltipTrigger
            render={
              <a
                href={ref.url}
                target="_blank"
                rel="noopener noreferrer"
                className="truncate flex-1 hover:underline"
              >
                {resource.label || ref.url}
              </a>
            }
          />
          <TooltipContent side="top">{ref.url}</TooltipContent>
        </Tooltip>
        <button
          type="button"
          onClick={onRemove}
          className="opacity-0 group-hover:opacity-100 transition-opacity rounded-sm p-0.5 hover:bg-accent"
          title={t(($) => $.resources.remove_tooltip)}
        >
          <Trash2 className="size-3 text-muted-foreground" />
        </button>
      </div>
    );
  }
  if (resource.resource_type === "local_path") {
    const ref = resource.resource_ref as LocalPathResourceRef;
    const runtime = runtimes.find((r) => r.daemon_id === ref.daemon_id);
    const machineName = runtime ? machineLabel(runtime) : ref.daemon_id.slice(0, 8);
    return (
      <div className="flex items-center gap-2 text-xs group">
        <FolderOpen className="size-3.5 text-muted-foreground shrink-0" />
        <Tooltip>
          <TooltipTrigger
            render={
              <span className="truncate flex-1">
                {resource.label || ref.path}
              </span>
            }
          />
          <TooltipContent side="top">{ref.path}</TooltipContent>
        </Tooltip>
        <span className="text-[10px] text-muted-foreground shrink-0">
          {machineName}
        </span>
        <button
          type="button"
          onClick={onRemove}
          className="opacity-0 group-hover:opacity-100 transition-opacity rounded-sm p-0.5 hover:bg-accent"
          title={t(($) => $.resources.remove_tooltip)}
        >
          <Trash2 className="size-3 text-muted-foreground" />
        </button>
      </div>
    );
  }
  return (
    <div className="flex items-center gap-2 text-xs text-muted-foreground">
      <span className="truncate flex-1">
        {resource.label || resource.resource_type}
      </span>
      <button
        type="button"
        onClick={onRemove}
        className="rounded-sm p-0.5 hover:bg-accent"
        title={t(($) => $.resources.remove_tooltip)}
      >
        <Trash2 className="size-3" />
      </button>
    </div>
  );
}

function CustomRepoForm({
  onSubmit,
}: {
  onSubmit: (url: string) => Promise<void> | void;
}) {
  const { t } = useT("projects");
  const [url, setUrl] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const handle = async (e: React.FormEvent) => {
    e.preventDefault();
    const trimmed = url.trim();
    if (!trimmed) return;
    setSubmitting(true);
    try {
      await onSubmit(trimmed);
      setUrl("");
    } finally {
      setSubmitting(false);
    }
  };
  return (
    <form onSubmit={handle} className="space-y-1.5 pt-1 border-t">
      <input
        type="text"
        value={url}
        onChange={(e) => setUrl(e.target.value)}
        placeholder={t(($) => $.resources.url_placeholder)}
        className="w-full bg-transparent text-xs px-2 py-1 outline-none placeholder:text-muted-foreground"
      />
      <div className="flex justify-end">
        <Button
          type="submit"
          size="sm"
          variant="ghost"
          className="h-6 px-2 text-xs"
          disabled={!url.trim() || submitting}
        >
          {t(($) => $.resources.url_submit)}
        </Button>
      </div>
    </form>
  );
}

// machineLabel extracts a human-readable machine name from a runtime.
// The backend formats device_info as "hostname · version" and name as
// "Provider (hostname)", so we prefer the hostname from device_info.
function machineLabel(rt: { name: string; device_info: string }): string {
  // device_info is "<hostname> · <version>"; take the hostname.
  if (rt.device_info) {
    const host = rt.device_info.split(" · ")[0];
    if (host) return host;
  }
  // Fallback: extract "(hostname)" from runtime name like "Claude (hostname)"
  const match = rt.name.match(/\(([^)]+)\)$/);
  if (match && match[1]) return match[1];
  return rt.name;
}

// buildRuntimeMachines groups a list of runtimes by daemon_id so the UI
// shows one entry per physical machine instead of one per runtime.
function buildRuntimeMachines(
  runtimes: Array<{ id: string; daemon_id: string | null; name: string; device_info: string }>,
) {
  const map = new Map<string, { daemonId: string; label: string }>();
  for (const r of runtimes) {
    if (!r.daemon_id || map.has(r.daemon_id)) continue;
    map.set(r.daemon_id, { daemonId: r.daemon_id, label: machineLabel(r) });
  }
  return Array.from(map.values());
}

function LocalPathForm({
  attachedPaths,
  runtimes,
  onSubmit,
  disabled,
}: {
  attachedPaths: Set<string>;
  runtimes: Array<{ id: string; daemon_id: string | null; name: string; device_info: string }>;
  onSubmit: (path: string, daemonId: string, label?: string) => Promise<void> | void;
  disabled: boolean;
}) {
  const { t } = useT("projects");
  const [path, setPath] = useState("");
  const [label, setLabel] = useState("");
  const machines = buildRuntimeMachines(runtimes);
  const [daemonId, setDaemonId] = useState(() => machines[0]?.daemonId ?? "");
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (machines.length === 0) {
      setDaemonId("");
      return;
    }
    setDaemonId((current) =>
      machines.some((machine) => machine.daemonId === current)
        ? current
        : machines[0]!.daemonId,
    );
  }, [machines]);

  const isAttached = path.trim() && attachedPaths.has(path.trim());
  const canSubmit =
    path.trim() && daemonId && !isAttached && !disabled && !submitting;
  const hasMachines = machines.length > 0;

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    const trimmed = path.trim();
    if (!trimmed || !daemonId || !canSubmit) return;
    setSubmitting(true);
    try {
      await onSubmit(trimmed, daemonId, label.trim() || undefined);
      setPath("");
      setLabel("");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <form onSubmit={handleSubmit} className="space-y-2 pt-1 border-t">
      <div className="space-y-1">
        <div className="text-[10px] font-medium text-muted-foreground">
          {t(($) => $.resources.alias_label)}
        </div>
        <Input
          type="text"
          value={label}
          onChange={(e) => setLabel(e.target.value)}
          placeholder={t(($) => $.resources.alias_placeholder)}
          className="h-7 text-xs"
        />
      </div>
      <input
        type="text"
        value={path}
        onChange={(e) => setPath(e.target.value)}
        placeholder="/absolute/path/to/project"
        className="w-full bg-transparent text-xs px-2 py-1 outline-none placeholder:text-muted-foreground"
      />
      <div className="flex items-center gap-1.5">
        <NativeSelect
          value={daemonId}
          onChange={(e) => setDaemonId(e.target.value)}
          className="w-full min-w-0"
          disabled={!hasMachines || disabled || submitting}
        >
          <NativeSelectOption value="" disabled>
            {hasMachines
              ? t(($) => $.resources.machine_placeholder)
              : t(($) => $.resources.no_online_machines)}
          </NativeSelectOption>
          {machines.map((m) => (
            <NativeSelectOption key={m.daemonId} value={m.daemonId}>
              {m.label}
            </NativeSelectOption>
          ))}
        </NativeSelect>
        <Button
          type="submit"
          size="sm"
          variant="ghost"
          className="h-7 px-2 text-xs shrink-0"
          disabled={!canSubmit}
        >
          {t(($) => $.resources.url_submit)}
        </Button>
      </div>
      {isAttached && (
        <p className="text-[10px] text-muted-foreground">
          {t(($) => $.resources.attached_badge)}
        </p>
      )}
      {!hasMachines && (
        <p className="text-[10px] text-muted-foreground">
          {t(($) => $.resources.no_online_machines_hint)}
        </p>
      )}
    </form>
  );
}
