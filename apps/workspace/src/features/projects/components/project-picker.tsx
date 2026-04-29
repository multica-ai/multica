"use client";

import { useMemo, useState } from "react";
import { FolderKanban, FolderMinus } from "lucide-react";
import type { UpdateIssueRequest } from "@/shared/types";
import { PROJECT_STATUS_CONFIG } from "@/features/projects/config";
import { useProjectsQuery } from "@/features/projects/queries";
import {
  PropertyPicker,
  PickerItem,
  PickerEmpty,
} from "@/features/issues/components/pickers/property-picker";

export function ProjectPicker({
  projectId,
  onUpdate,
  align,
}: {
  projectId: string | null;
  onUpdate: (updates: Partial<UpdateIssueRequest>) => void;
  align?: "start" | "center" | "end";
}) {
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState("");
  const { data: projects = [], isLoading } = useProjectsQuery();

  const filteredProjects = useMemo(() => {
    const query = filter.trim().toLowerCase();
    if (!query) return projects;

    return projects.filter((project) =>
      project.title.toLowerCase().includes(query)
        || project.description?.toLowerCase().includes(query),
    );
  }, [filter, projects]);

  const selectedProject = projects.find((project) => project.id === projectId) ?? null;

  return (
    <PropertyPicker
      open={open}
      onOpenChange={(nextOpen) => {
        setOpen(nextOpen);
        if (!nextOpen) setFilter("");
      }}
      width="w-64"
      align={align ?? "end"}
      searchable
      searchPlaceholder="Move to project..."
      onSearchChange={setFilter}
      trigger={selectedProject ? (
        <>
          <span className="shrink-0 text-base leading-none">{selectedProject.icon || "📁"}</span>
          <span className="truncate">{selectedProject.title}</span>
        </>
      ) : (
        <span className="text-muted-foreground">No project</span>
      )}
    >
      <PickerItem
        selected={!projectId}
        onClick={() => {
          onUpdate({ project_id: null });
          setOpen(false);
        }}
      >
        <FolderMinus className="h-3.5 w-3.5 text-muted-foreground" />
        <span className="text-muted-foreground">No project</span>
      </PickerItem>

      {filteredProjects.map((project) => {
        const statusConfig = PROJECT_STATUS_CONFIG[project.status];

        return (
          <PickerItem
            key={project.id}
            selected={project.id === projectId}
            onClick={() => {
              onUpdate({ project_id: project.id });
              setOpen(false);
            }}
          >
            <span className="shrink-0 text-base leading-none">{project.icon || "📁"}</span>
            <span className="flex min-w-0 flex-1 items-center gap-2">
              <span className="truncate">{project.title}</span>
              <span className={`shrink-0 rounded px-1.5 py-0.5 text-[10px] font-medium ${statusConfig.badgeBg} ${statusConfig.badgeText}`}>
                {statusConfig.label}
              </span>
            </span>
          </PickerItem>
        );
      })}

      {!isLoading && filteredProjects.length === 0 ? <PickerEmpty /> : null}

      {isLoading ? (
        <div className="px-2 py-3 text-center text-sm text-muted-foreground">
          Loading projects...
        </div>
      ) : null}

      {!isLoading && filteredProjects.length > 0 && !selectedProject ? (
        <div className="mt-1 border-t px-2 py-2 text-[11px] text-muted-foreground">
          <span className="inline-flex items-center gap-1">
            <FolderKanban className="h-3 w-3" />
            Select a project to group related issues.
          </span>
        </div>
      ) : null}
    </PropertyPicker>
  );
}