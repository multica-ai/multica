"use client";

import { useState } from "react";
import Link from "next/link";
import { Plus } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useProjectStore } from "@/features/projects";
import { useWorkspaceStore, useActorName } from "@/features/workspace";
import { ProjectStatusBadge } from "./project-status-badge";
import { ProjectProgressBar } from "./project-progress-bar";
import { CreateProjectDialog } from "./create-project-dialog";
import { Skeleton } from "@/components/ui/skeleton";

export function ProjectsPage() {
  const projects = useProjectStore((s) => s.projects);
  const loading = useProjectStore((s) => s.loading);
  const [createOpen, setCreateOpen] = useState(false);
  const { getActorName } = useActorName();

  return (
    <div className="flex flex-col h-full">
      {/* Header */}
      <div className="flex items-center justify-between border-b px-6 py-3">
        <h1 className="text-lg font-semibold">Projects</h1>
        <Button size="sm" onClick={() => setCreateOpen(true)}>
          <Plus className="h-4 w-4 mr-1" />
          New Project
        </Button>
      </div>

      {/* Content */}
      <div className="flex-1 overflow-y-auto p-6">
        {loading ? (
          <div className="space-y-3">
            {Array.from({ length: 5 }).map((_, i) => (
              <Skeleton key={i} className="h-16 w-full rounded-lg" />
            ))}
          </div>
        ) : projects.length === 0 ? (
          <div className="flex flex-col items-center justify-center h-64 text-muted-foreground">
            <p className="text-sm">No projects yet</p>
            <Button variant="outline" size="sm" className="mt-3" onClick={() => setCreateOpen(true)}>
              Create your first project
            </Button>
          </div>
        ) : (
          <div className="space-y-2">
            {projects.map((project) => (
              <Link
                key={project.id}
                href={`/projects/${project.id}`}
                className="flex items-center gap-4 rounded-lg border p-4 hover:bg-accent/50 transition-colors"
              >
                {/* Color dot */}
                <div
                  className="h-3 w-3 rounded-full shrink-0"
                  style={{ backgroundColor: project.color ?? "#6366f1" }}
                />

                {/* Name + description */}
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="font-medium truncate">{project.name}</span>
                    <ProjectStatusBadge status={project.status} />
                  </div>
                  {project.description && (
                    <p className="text-xs text-muted-foreground truncate mt-0.5">
                      {project.description}
                    </p>
                  )}
                </div>

                {/* Lead */}
                {project.lead_type && project.lead_id && (
                  <span className="text-xs text-muted-foreground shrink-0">
                    {getActorName(project.lead_type, project.lead_id)}
                  </span>
                )}

                {/* Progress */}
                <div className="w-32 shrink-0">
                  <ProjectProgressBar progress={project.progress} />
                </div>

                {/* Target date */}
                {project.target_date && (
                  <span className="text-xs text-muted-foreground shrink-0">
                    {new Date(project.target_date).toLocaleDateString("en-US", {
                      month: "short",
                      day: "numeric",
                    })}
                  </span>
                )}
              </Link>
            ))}
          </div>
        )}
      </div>

      <CreateProjectDialog open={createOpen} onOpenChange={setCreateOpen} />
    </div>
  );
}
