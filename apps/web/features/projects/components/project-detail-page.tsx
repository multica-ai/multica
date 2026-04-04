"use client";

import { useState, useEffect, useCallback } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import {
  ChevronLeft,
  Check,
  MoreHorizontal,
  Trash2,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from "@/components/ui/dropdown-menu";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Skeleton } from "@/components/ui/skeleton";
import type { UpdateProjectRequest } from "@/shared/types";
import { useProjectStore } from "@/features/projects";
import { useIssueStore } from "@/features/issues";
import { useActorName } from "@/features/workspace";
import { ProjectStatusBadge } from "./project-status-badge";
import { ProjectProgressBar } from "./project-progress-bar";
import { PROJECT_STATUSES, PROJECT_STATUS_CONFIG } from "@/features/projects/config/status";
import { StatusIcon } from "@/features/issues/components";
import { STATUS_CONFIG } from "@/features/issues/config";
import { api } from "@/shared/api";

export function ProjectDetailPage({ projectId }: { projectId: string }) {
  const router = useRouter();
  const project = useProjectStore((s) => s.projects.find((p) => p.id === projectId)) ?? null;
  const updateProjectApi = useProjectStore((s) => s.updateProjectApi);
  const deleteProject = useProjectStore((s) => s.deleteProject);
  const allIssues = useIssueStore((s) => s.issues);
  const { getActorName } = useActorName();
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [loading, setLoading] = useState(!project);

  // If project isn't in the store yet, fetch it
  useEffect(() => {
    if (project) {
      setLoading(false);
      return;
    }
    api.getProject(projectId).then((p) => {
      useProjectStore.getState().addProject(p);
      setLoading(false);
    }).catch(() => {
      setLoading(false);
    });
  }, [project, projectId]);

  const projectIssues = allIssues.filter((i) => i.project_id === projectId);

  const handleUpdateField = useCallback(
    (updates: UpdateProjectRequest) => {
      if (!project) return;
      updateProjectApi(project.id, updates);
    },
    [project, updateProjectApi],
  );

  const handleDelete = async () => {
    await deleteProject(projectId);
    router.push("/projects");
  };

  if (loading) {
    return (
      <div className="p-6 space-y-4">
        <Skeleton className="h-8 w-48" />
        <Skeleton className="h-4 w-96" />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }

  if (!project) {
    return (
      <div className="flex items-center justify-center h-full text-muted-foreground">
        Project not found
      </div>
    );
  }

  return (
    <div className="flex flex-col h-full">
      {/* Header */}
      <div className="flex items-center gap-3 border-b px-6 py-3">
        <Link
          href="/projects"
          className="inline-flex items-center justify-center h-7 w-7 rounded-md hover:bg-accent transition-colors"
        >
          <ChevronLeft className="h-4 w-4" />
        </Link>
        <div
          className="h-3 w-3 rounded-full shrink-0"
          style={{ backgroundColor: project.color ?? "#6366f1" }}
        />
        <h1 className="text-lg font-semibold truncate flex-1">{project.name}</h1>

        {/* Status dropdown */}
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button variant="ghost" size="sm" className="gap-1.5">
                <ProjectStatusBadge status={project.status} />
              </Button>
            }
          />
          <DropdownMenuContent align="end" className="w-44">
            {PROJECT_STATUSES.map((s) => (
              <DropdownMenuItem key={s} onClick={() => handleUpdateField({ status: s })}>
                <span className={PROJECT_STATUS_CONFIG[s].color}>
                  {PROJECT_STATUS_CONFIG[s].label}
                </span>
                {s === project.status && <Check className="ml-auto h-3.5 w-3.5" />}
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>

        {/* More menu */}
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button variant="ghost" size="icon" className="h-7 w-7">
                <MoreHorizontal className="h-4 w-4" />
              </Button>
            }
          />
          <DropdownMenuContent align="end">
            <DropdownMenuItem
              variant="destructive"
              onClick={() => setDeleteDialogOpen(true)}
            >
              <Trash2 className="h-3.5 w-3.5" />
              Delete project
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>

      {/* Content */}
      <div className="flex-1 overflow-y-auto p-6 space-y-6">
        {/* Progress */}
        <div className="max-w-sm">
          <ProjectProgressBar progress={project.progress} />
        </div>

        {/* Description */}
        {project.description && (
          <p className="text-sm text-muted-foreground">{project.description}</p>
        )}

        {/* Meta */}
        <div className="flex gap-6 text-xs text-muted-foreground">
          {project.lead_type && project.lead_id && (
            <span>Lead: {getActorName(project.lead_type, project.lead_id)}</span>
          )}
          {project.start_date && (
            <span>Start: {new Date(project.start_date).toLocaleDateString()}</span>
          )}
          {project.target_date && (
            <span>Target: {new Date(project.target_date).toLocaleDateString()}</span>
          )}
        </div>

        {/* Issues list */}
        <div>
          <h2 className="text-sm font-medium mb-3">
            Issues ({projectIssues.length})
          </h2>
          {projectIssues.length === 0 ? (
            <p className="text-xs text-muted-foreground">
              No issues in this project yet. Assign issues from the issue detail page.
            </p>
          ) : (
            <div className="space-y-1">
              {projectIssues.map((issue) => (
                <Link
                  key={issue.id}
                  href={`/issues/${issue.id}`}
                  className="flex items-center gap-3 rounded-md px-3 py-2 hover:bg-accent/50 transition-colors text-sm"
                >
                  <StatusIcon status={issue.status} className="h-3.5 w-3.5 shrink-0" />
                  <span className="text-xs text-muted-foreground shrink-0">
                    {issue.identifier}
                  </span>
                  <span className="truncate flex-1">{issue.title}</span>
                  <span className="text-xs text-muted-foreground shrink-0">
                    {STATUS_CONFIG[issue.status].label}
                  </span>
                </Link>
              ))}
            </div>
          )}
        </div>
      </div>

      {/* Delete dialog */}
      <AlertDialog open={deleteDialogOpen} onOpenChange={setDeleteDialogOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete project?</AlertDialogTitle>
            <AlertDialogDescription>
              This will delete the project. Issues in this project will not be deleted,
              but they will no longer be associated with any project.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={handleDelete} className="bg-destructive text-destructive-foreground hover:bg-destructive/90">
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
