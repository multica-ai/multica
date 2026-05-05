"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight, FolderGit, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import {
  projectResourcesOptions,
  useCreateProjectResource,
  useDeleteProjectResource,
} from "@multica/core/projects";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentWorkspace } from "@multica/core/paths";
import type {
  GithubRepoResourceRef,
  ProjectResource,
} from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
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

// Project Resources sidebar section.
//
// Today only renders github_repo, but the rendering layer is type-dispatched
// so adding a new type means: (1) extend the API validator, (2) add a render
// case here. No changes to the schema or query layer.
export function ProjectResourcesSection({ projectId }: { projectId: string }) {
  const wsId = useWorkspaceId();
  const workspace = useCurrentWorkspace();
  const [open, setOpen] = useState(true);
  const [addOpen, setAddOpen] = useState(false);

  const { data: resources = [] } = useQuery(
    projectResourcesOptions(wsId, projectId),
  );
  const createResource = useCreateProjectResource(wsId, projectId);
  const deleteResource = useDeleteProjectResource(wsId, projectId);

  const attachedUrls = new Set(
    resources
      .filter((r) => r.resource_type === "github_repo")
      .map((r) => (r.resource_ref as GithubRepoResourceRef).url),
  );

  const handleAttach = async (url: string) => {
    try {
      await createResource.mutateAsync({
        resource_type: "github_repo",
        resource_ref: { url },
      });
      toast.success("Repository attached");
    } catch (err) {
      const msg = err instanceof Error ? err.message : "Failed to attach";
      toast.error(msg);
    }
  };

  const handleRemove = async (resource: ProjectResource) => {
    try {
      await deleteResource.mutateAsync(resource.id);
      toast.success("Resource removed");
    } catch {
      toast.error("Failed to remove resource");
    }
  };

  return (
    <div>
      <button
        className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors mb-2 hover:bg-accent/70 ${open ? "" : "text-muted-foreground hover:text-foreground"}`}
        onClick={() => setOpen(!open)}
      >
        Resources
        <ChevronRight
          className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${open ? "rotate-90" : ""}`}
        />
      </button>
      {open && (
        <div className="pl-2 space-y-1.5">
          {resources.length === 0 && (
            <p className="text-xs text-muted-foreground">
              No resources attached.
            </p>
          )}
          {resources.map((resource) => (
            <ResourceRow
              key={resource.id}
              resource={resource}
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
                  Add resource
                </Button>
              }
            />
            <PopoverContent align="start" className="w-72 p-2 space-y-2">
              <div className="text-xs font-medium text-muted-foreground">
                Attach a GitHub repo
              </div>
              {workspace?.repos && workspace.repos.length > 0 && (
                <div className="space-y-1">
                  {workspace.repos.map((repo) => {
                    const isAttached = attachedUrls.has(repo.url);
                    return (
                      <button
                        key={repo.url}
                        type="button"
                        disabled={isAttached || createResource.isPending}
                        onClick={async () => {
                          await handleAttach(repo.url);
                          setAddOpen(false);
                        }}
                        className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-xs text-left hover:bg-accent transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
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
                            attached
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
            </PopoverContent>
          </Popover>
        </div>
      )}
    </div>
  );
}

function ResourceRow({
  resource,
  onRemove,
}: {
  resource: ProjectResource;
  onRemove: () => void;
}) {
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
          title="Remove"
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
        title="Remove"
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
    <form onSubmit={handle} className="flex items-center gap-1.5 pt-1 border-t">
      <input
        type="url"
        value={url}
        onChange={(e) => setUrl(e.target.value)}
        placeholder="https://github.com/owner/repo"
        className="flex-1 bg-transparent text-xs px-2 py-1 outline-none placeholder:text-muted-foreground"
      />
      <Button
        type="submit"
        size="sm"
        variant="ghost"
        className="h-6 px-2 text-xs"
        disabled={!url.trim() || submitting}
      >
        Add
      </Button>
    </form>
  );
}
