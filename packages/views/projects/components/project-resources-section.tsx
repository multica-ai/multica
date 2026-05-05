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
import { useT } from "../../i18n";

// Project Resources sidebar section.
//
// Today only renders github_repo, but the rendering layer is type-dispatched
// so adding a new type means: (1) extend the API validator, (2) add a render
// case here. No changes to the schema or query layer.
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

  const attachedUrls = new Set(
    resources
      .filter((r) => r.resource_type === "github_repo")
      .map((r) => (r.resource_ref as GithubRepoResourceRef).url),
  );

  // Only approved workspace repos can be attached. Pending repos must be
  // approved through the workspace settings flow first.
  const approvedRepos = (workspace?.repos ?? []).filter(
    (r) => r.status === "approved",
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

  const handleRemove = async (resource: ProjectResource) => {
    try {
      await deleteResource.mutateAsync(resource.id);
      toast.success(t(($) => $.resources.toast_removed));
    } catch {
      toast.error(t(($) => $.resources.toast_remove_failed));
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
              {approvedRepos.length > 0 ? (
                <div className="space-y-1">
                  {approvedRepos.map((repo) => {
                    const isAttached = attachedUrls.has(repo.url);
                    const isDisabled = isAttached || createResource.isPending;
                    return (
                      // Use aria-disabled instead of the native `disabled` attribute so
                      // hover events still reach the tooltip trigger on attached rows
                      // (browsers suppress pointer events on disabled form controls).
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
              ) : (
                <p className="text-xs text-muted-foreground">
                  {t(($) => $.resources.popover_empty_approval)}
                </p>
              )}
            </PopoverContent>
          </Popover>
        </div>
      )}
    </div>
  );
}

// Convert SSH-style git URL (git@host:owner/repo.git) to https for browser navigation.
function toHttpUrl(url: string): string {
  const match = /^git@([^:]+):(.+)$/.exec(url);
  return match ? `https://${match[1]}/${match[2]}` : url;
}

function ResourceRow({
  resource,
  onRemove,
}: {
  resource: ProjectResource;
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
                href={toHttpUrl(ref.url)}
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
