"use client";

import { useState } from "react";
import { ChevronRight, ExternalLink, Link2, Plus, X } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { workspaceIntegrationsOptions, projectIntegrationLinksOptions, redmineProjectsOptions } from "@multica/core/integrations/queries";
import { useUpsertProjectIntegrationLink, useDeleteProjectIntegrationLink } from "@multica/core/integrations/mutations";
import { api } from "@multica/core/api";
import { Popover, PopoverTrigger, PopoverContent } from "@multica/ui/components/ui/popover";
import { toast } from "sonner";
import type { RedmineProject } from "@multica/core/types";

// ---------------------------------------------------------------------------
// PropRow (mirrors the one in project-detail)
// ---------------------------------------------------------------------------

function PropRow({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex min-h-8 items-center gap-2 rounded-md px-2 -mx-2 hover:bg-accent/50 transition-colors">
      <span className="w-16 shrink-0 text-xs text-muted-foreground">{label}</span>
      <div className="flex min-w-0 flex-1 items-center gap-1.5 text-xs truncate">
        {children}
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Link/create popover
// ---------------------------------------------------------------------------

function LinkOrCreateProjectPopover({
  wsId,
  projectId,
  projectTitle,
  projectDescription,
  onClose,
}: {
  wsId: string;
  projectId: string;
  projectTitle: string;
  projectDescription?: string | null;
  onClose: () => void;
}) {
  const [filter, setFilter] = useState("");
  const [creating, setCreating] = useState(false);
  const upsert = useUpsertProjectIntegrationLink();

  const { data: projectsData, isLoading } = useQuery(redmineProjectsOptions(wsId));
  const redmineProjects: RedmineProject[] = projectsData?.projects ?? [];

  const filtered = filter.trim()
    ? redmineProjects.filter(
      (p) =>
        p.name.toLowerCase().includes(filter.toLowerCase()) ||
        p.identifier.toLowerCase().includes(filter.toLowerCase()),
    )
    : redmineProjects;

  const handleLink = async (project: RedmineProject) => {
    try {
      await upsert.mutateAsync({
        projectId,
        provider: "redmine",
        external_project_id: String(project.id),
        external_project_name: project.name,
      });
      toast.success("Linked to Redmine project");
      onClose();
    } catch {
      toast.error("Failed to link project");
    }
  };

  const toKebab = (s: string) =>
    s
      .toLowerCase()
      .replace(/[^a-z0-9\s-]/g, "")
      .trim()
      .replace(/\s+/g, "-");

  const handleCreate = async () => {
    const identifier = (toKebab(projectTitle).slice(0, 100) || "multica-project");
    const multicaUrl = typeof window !== "undefined" ? window.location.href : "";
    const descParts: string[] = [];
    if (projectDescription) descParts.push(projectDescription);
    descParts.push("---");
    descParts.push(`Creado desde Multica${multicaUrl ? ` · ${multicaUrl}` : ""}`);
    const description = descParts.join("\n");

    setCreating(true);
    try {
      const created = await api.createRedmineProject({ name: projectTitle, identifier, description });
      await upsert.mutateAsync({
        projectId,
        provider: "redmine",
        external_project_id: String(created.id),
        external_project_name: created.name,
      });
      toast.success("Project created and linked in Redmine");
      onClose();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to create project in Redmine");
    } finally {
      setCreating(false);
    }
  };

  return (
    <div className="w-64">
      <div className="px-2 py-1.5 border-b">
        <input
          type="text"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          placeholder="Search Redmine projects..."
          className="w-full bg-transparent text-sm placeholder:text-muted-foreground outline-none"
          autoFocus
        />
      </div>
      <div className="max-h-60 overflow-y-auto p-1">
        {isLoading && (
          <p className="px-2 py-3 text-xs text-muted-foreground text-center">Loading...</p>
        )}
        {filtered.map((project) => (
          <button
            key={project.id}
            type="button"
            onClick={() => handleLink(project)}
            className="flex w-full items-start gap-2 rounded-md px-2 py-1.5 text-left text-sm hover:bg-accent transition-colors"
          >
            <span className="min-w-0">
              <span className="block truncate">{project.name}</span>
              <span className="text-xs text-muted-foreground">{project.identifier}</span>
            </span>
          </button>
        ))}
        {filtered.length === 0 && !isLoading && (
          <p className="px-2 py-2 text-xs text-muted-foreground text-center">No projects found</p>
        )}
      </div>
      <div className="border-t p-1">
        <button
          type="button"
          onClick={handleCreate}
          disabled={creating || upsert.isPending}
          className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-accent transition-colors disabled:opacity-50"
        >
          <Plus className="h-3.5 w-3.5 text-muted-foreground shrink-0" />
          <span>{creating ? "Creating..." : "Create new in Redmine"}</span>
        </button>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// ProjectRedmineSection
// ---------------------------------------------------------------------------

interface ProjectRedmineSectionProps {
  wsId: string;
  projectId: string;
  projectTitle: string;
  projectDescription?: string | null;
}

export function ProjectRedmineSection({ wsId, projectId, projectTitle, projectDescription }: ProjectRedmineSectionProps) {
  const [open, setOpen] = useState(false);
  const [popoverOpen, setPopoverOpen] = useState(false);

  const { data: integrationsData } = useQuery(workspaceIntegrationsOptions(wsId));
  const { data: linksData } = useQuery(projectIntegrationLinksOptions(wsId, projectId));
  const deleteLink = useDeleteProjectIntegrationLink();

  const redmineIntegration = (integrationsData?.integrations ?? []).find(
    (i) => i.provider === "redmine",
  );
  if (!redmineIntegration) return null;

  const redmineLink = (linksData?.links ?? []).find((l) => l.provider === "redmine");
  const projectURL = redmineLink
    ? `${redmineIntegration.instance_url.replace(/\/$/, "")}/projects/${redmineLink.external_project_id}`
    : null;

  const handleUnlink = async () => {
    try {
      await deleteLink.mutateAsync({ projectId, provider: "redmine" });
      toast.success("Unlinked from Redmine");
    } catch {
      toast.error("Failed to unlink");
    }
  };

  return (
    <div>
      <button
        className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors mb-2 hover:bg-accent/70 ${open ? "" : "text-muted-foreground hover:text-foreground"}`}
        onClick={() => setOpen(!open)}
      >
        Redmine
        <ChevronRight
          className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${open ? "rotate-90" : ""}`}
        />
      </button>

      {open && (
        <div className="space-y-0.5 pl-2">
          {redmineLink ? (
            <PropRow label="Project">
              <a
                href={projectURL ?? "#"}
                target="_blank"
                rel="noopener noreferrer"
                className="flex items-center gap-1 min-w-0 hover:text-foreground"
              >
                <ExternalLink className="h-3 w-3 shrink-0" />
                <span className="truncate">
                  {redmineLink.external_project_name ?? `#${redmineLink.external_project_id}`}
                </span>
              </a>
              <button
                type="button"
                onClick={handleUnlink}
                disabled={deleteLink.isPending}
                className="ml-auto shrink-0 text-muted-foreground hover:text-destructive"
                title="Unlink"
              >
                <X className="h-3 w-3" />
              </button>
            </PropRow>
          ) : (
            <Popover open={popoverOpen} onOpenChange={setPopoverOpen}>
              <PopoverTrigger
                render={
                  <button
                    type="button"
                    className="flex w-full items-center gap-1.5 rounded-md px-2 py-1.5 text-xs text-muted-foreground hover:bg-accent hover:text-foreground transition-colors"
                  >
                    <Link2 className="h-3 w-3 shrink-0" />
                    Link to Redmine project
                  </button>
                }
              />
              <PopoverContent align="start" className="w-auto p-0">
                <LinkOrCreateProjectPopover
                  wsId={wsId}
                  projectId={projectId}
                  projectTitle={projectTitle}
                  projectDescription={projectDescription}
                  onClose={() => setPopoverOpen(false)}
                />
              </PopoverContent>
            </Popover>
          )}
        </div>
      )}
    </div>
  );
}
