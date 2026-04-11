"use client";

import { useEffect, useMemo, useState } from "react";
import { useDefaultLayout } from "react-resizable-panels";
import {
  FolderKanban,
  Plus,
  Save,
  Trash2,
  Check,
} from "lucide-react";
import {
  ResizablePanelGroup,
  ResizablePanel,
  ResizableHandle,
} from "@/components/ui/resizable";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
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
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { toast } from "sonner";
import { useAuthStore } from "@/features/auth";
import { useIssueStore } from "@/features/issues";
import { STATUS_CONFIG } from "@/features/issues/config";
import { StatusIcon } from "@/features/issues/components";
import { MobileDetailHeader } from "@/features/layout/components/mobile-detail-header";
import { useIsMobile } from "@/hooks/use-mobile";
import { Link, useRouter } from "@/shared/router";
import type { CreateProjectRequest, Project, UpdateProjectRequest } from "@/shared/types";
import { PROJECT_STATUS_CONFIG, PROJECT_STATUS_ORDER } from "../config";
import {
  useCreateProjectMutation,
  useDeleteProjectMutation,
  useUpdateProjectMutation,
} from "../mutations";
import { useProjectsQuery } from "../queries";

function CreateProjectDialog({
  open,
  onOpenChange,
  onCreate,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreate: (data: CreateProjectRequest) => Promise<void>;
}) {
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [icon, setIcon] = useState("📁");
  const [creating, setCreating] = useState(false);

  const handleCreate = async () => {
    const trimmedTitle = title.trim();
    if (!trimmedTitle) return;

    setCreating(true);
    try {
      await onCreate({
        title: trimmedTitle,
        description: description.trim() || undefined,
        icon: icon.trim() || undefined,
      });
      setTitle("");
      setDescription("");
      setIcon("📁");
      onOpenChange(false);
    } finally {
      setCreating(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Create Project</DialogTitle>
          <DialogDescription>
            Group related issues under a shared project plan.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <div>
            <Label className="text-xs text-muted-foreground">Title</Label>
            <Input
              autoFocus
              value={title}
              onChange={(event) => setTitle(event.target.value)}
              placeholder="e.g. Mobile app rollout"
              className="mt-1"
              onKeyDown={(event) => {
                if (event.key === "Enter") {
                  void handleCreate();
                }
              }}
            />
          </div>

          <div>
            <Label className="text-xs text-muted-foreground">Icon</Label>
            <Input
              value={icon}
              onChange={(event) => setIcon(event.target.value)}
              placeholder="📁"
              className="mt-1"
            />
          </div>

          <div>
            <Label className="text-xs text-muted-foreground">Description</Label>
            <Textarea
              value={description}
              onChange={(event) => setDescription(event.target.value)}
              placeholder="What outcome does this project own?"
              className="mt-1 min-h-24"
            />
          </div>
        </div>

        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={() => void handleCreate()} disabled={creating || !title.trim()}>
            {creating ? "Creating..." : "Create"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function ProjectListItem({
  project,
  isSelected,
  onClick,
}: {
  project: Project;
  isSelected: boolean;
  onClick: () => void;
}) {
  const statusConfig = PROJECT_STATUS_CONFIG[project.status];

  return (
    <button
      onClick={onClick}
      className={`flex w-full items-center gap-3 px-4 py-3 text-left transition-colors ${
        isSelected ? "bg-accent" : "hover:bg-accent/50"
      }`}
    >
      <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-muted text-base">
        {project.icon || "📁"}
      </div>
      <div className="min-w-0 flex-1">
        <div className="truncate text-sm font-medium">{project.title}</div>
        {project.description ? (
          <div className="mt-0.5 truncate text-xs text-muted-foreground">
            {project.description}
          </div>
        ) : (
          <div className="mt-0.5 truncate text-xs text-muted-foreground">
            No description yet
          </div>
        )}
      </div>
      <Badge className={`${statusConfig.badgeBg} ${statusConfig.badgeText}`} variant="secondary">
        {statusConfig.label}
      </Badge>
    </button>
  );
}

function ProjectDetailPanel({
  project,
  onUpdate,
  onDelete,
}: {
  project: Project;
  onUpdate: (id: string, updates: UpdateProjectRequest) => Promise<void>;
  onDelete: (id: string) => Promise<void>;
}) {
  const issues = useIssueStore((state) => state.issues);
  const [title, setTitle] = useState(project.title);
  const [description, setDescription] = useState(project.description ?? "");
  const [icon, setIcon] = useState(project.icon ?? "📁");
  const [saving, setSaving] = useState(false);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);

  useEffect(() => {
    setTitle(project.title);
    setDescription(project.description ?? "");
    setIcon(project.icon ?? "📁");
  }, [project.id, project.title, project.description, project.icon]);

  const relatedIssues = useMemo(
    () => issues.filter((issue) => issue.project_id === project.id),
    [issues, project.id],
  );

  const isDirty =
    title.trim() !== project.title
    || description !== (project.description ?? "")
    || icon !== (project.icon ?? "📁");

  const statusConfig = PROJECT_STATUS_CONFIG[project.status];

  const handleSave = async () => {
    const nextTitle = title.trim();
    if (!nextTitle) return;

    setSaving(true);
    try {
      await onUpdate(project.id, {
        title: nextTitle,
        description: description.trim() ? description.trim() : null,
        icon: icon.trim() ? icon.trim() : null,
      });
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <div className="flex items-center gap-3 border-b px-4 py-3">
        <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-muted text-lg">
          {project.icon || "📁"}
        </div>
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-semibold">{project.title}</div>
          <div className="text-xs text-muted-foreground">
            {relatedIssues.length} linked issue{relatedIssues.length === 1 ? "" : "s"}
          </div>
        </div>

        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button variant="outline" size="sm" aria-label="Project status">
                <span className={statusConfig.color}>{statusConfig.label}</span>
              </Button>
            }
          />
          <DropdownMenuContent align="end" className="w-44">
            {PROJECT_STATUS_ORDER.map((status) => (
              <DropdownMenuItem
                key={status}
                onClick={() => {
                  void onUpdate(project.id, { status });
                }}
              >
                <span className={PROJECT_STATUS_CONFIG[status].color}>
                  {PROJECT_STATUS_CONFIG[status].label}
                </span>
                {status === project.status ? <Check className="ml-auto h-3.5 w-3.5" /> : null}
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>

        <Button
          variant="ghost"
          size="icon-sm"
          aria-label="Delete project"
          className="text-muted-foreground hover:text-destructive"
          onClick={() => setDeleteDialogOpen(true)}
        >
          <Trash2 className="h-4 w-4" />
        </Button>
      </div>

      <div className="flex-1 overflow-y-auto px-4 py-5">
        <div className="space-y-5">
          <div className="grid gap-4 md:grid-cols-[minmax(0,1fr)_7rem]">
            <div>
              <Label className="text-xs text-muted-foreground">Title</Label>
              <Input
                value={title}
                onChange={(event) => setTitle(event.target.value)}
                className="mt-1"
              />
            </div>
            <div>
              <Label className="text-xs text-muted-foreground">Icon</Label>
              <Input
                value={icon}
                onChange={(event) => setIcon(event.target.value)}
                className="mt-1"
              />
            </div>
          </div>

          <div>
            <Label className="text-xs text-muted-foreground">Description</Label>
            <Textarea
              value={description}
              onChange={(event) => setDescription(event.target.value)}
              className="mt-1 min-h-32"
              placeholder="Capture the scope, success metric, and important context for this project."
            />
          </div>

          <div className="flex items-center justify-end">
            <Button onClick={() => void handleSave()} disabled={!isDirty || saving}>
              <Save className="h-3.5 w-3.5" />
              {saving ? "Saving..." : "Save"}
            </Button>
          </div>

          <div className="space-y-3">
            <div className="flex items-center justify-between">
              <div>
                <h3 className="text-sm font-semibold">Linked Issues</h3>
                <p className="text-xs text-muted-foreground">
                  Issues currently grouped under this project.
                </p>
              </div>
              <Badge variant="outline">{relatedIssues.length}</Badge>
            </div>

            {relatedIssues.length === 0 ? (
              <div className="rounded-lg border border-dashed px-4 py-8 text-center text-sm text-muted-foreground">
                No issues linked yet.
              </div>
            ) : (
              <div className="space-y-2">
                {relatedIssues.map((issue) => (
                  <Link
                    key={issue.id}
                    href={`/issues/${issue.id}`}
                    className="flex items-center gap-2 rounded-lg border px-3 py-2 text-sm transition-colors hover:bg-accent/50"
                  >
                    <StatusIcon status={issue.status} className="h-3.5 w-3.5 shrink-0" />
                    <span className="shrink-0 text-xs text-muted-foreground">{issue.identifier}</span>
                    <span className="min-w-0 flex-1 truncate">{issue.title}</span>
                    <span className={`shrink-0 text-xs ${STATUS_CONFIG[issue.status].iconColor}`}>
                      {STATUS_CONFIG[issue.status].label}
                    </span>
                  </Link>
                ))}
              </div>
            )}
          </div>
        </div>
      </div>

      <AlertDialog open={deleteDialogOpen} onOpenChange={setDeleteDialogOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete project</AlertDialogTitle>
            <AlertDialogDescription>
              This removes the project and unlinks its issues. The issues themselves will stay in the workspace.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                void onDelete(project.id);
              }}
            >
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

export function ProjectsPage({
  selectedProjectId,
  syncSelectionToPath = false,
}: {
  selectedProjectId?: string;
  syncSelectionToPath?: boolean;
}) {
  const router = useRouter();
  const isMobile = useIsMobile();
  const isLoading = useAuthStore((state) => state.isLoading);
  const { data: projects = [], isLoading: projectsLoading } = useProjectsQuery();
  const createProject = useCreateProjectMutation();
  const updateProject = useUpdateProjectMutation();
  const deleteProject = useDeleteProjectMutation();
  const [selectedId, setSelectedId] = useState("");
  const [createOpen, setCreateOpen] = useState(false);
  const { defaultLayout, onLayoutChanged } = useDefaultLayout({
    id: "multica_projects_layout",
  });

  useEffect(() => {
    if (!selectedProjectId) return;
    if (projects.some((project) => project.id === selectedProjectId)) {
      setSelectedId(selectedProjectId);
    }
  }, [projects, selectedProjectId]);

  useEffect(() => {
    if (selectedProjectId || isMobile || projects.length === 0) return;
    if (!projects.some((project) => project.id === selectedId)) {
      setSelectedId(projects[0]!.id);
    }
  }, [isMobile, projects, selectedId, selectedProjectId]);

  const activeSelectedId = selectedProjectId ?? (isMobile ? "" : selectedId);
  const selectedProject = projects.find((project) => project.id === activeSelectedId) ?? null;

  const handleSelect = (projectId: string) => {
    if (isMobile || syncSelectionToPath) {
      router.push(`/projects/${projectId}`);
      return;
    }

    setSelectedId(projectId);
  };

  const handleCreate = async (data: CreateProjectRequest) => {
    try {
      const project = await createProject.mutateAsync(data);
      toast.success("Project created");
      if (isMobile || syncSelectionToPath) {
        router.push(`/projects/${project.id}`);
      } else {
        setSelectedId(project.id);
      }
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Failed to create project");
      throw error;
    }
  };

  const handleUpdate = async (projectId: string, updates: UpdateProjectRequest) => {
    try {
      await updateProject.mutateAsync({ id: projectId, ...updates });
      toast.success("Project updated");
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Failed to update project");
      throw error;
    }
  };

  const handleDelete = async (projectId: string) => {
    try {
      await deleteProject.mutateAsync(projectId);
      useIssueStore.getState().setIssues(
        useIssueStore.getState().issues.map((issue) => (
          issue.project_id === projectId ? { ...issue, project_id: null } : issue
        )),
      );
      toast.success("Project deleted");

      const remaining = projects.filter((project) => project.id !== projectId);
      if (isMobile || syncSelectionToPath) {
        router.replace("/projects");
      } else {
        setSelectedId(remaining[0]?.id ?? "");
      }
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Failed to delete project");
    }
  };

  if (isLoading || projectsLoading) {
    return (
      <div className="flex flex-1 min-h-0">
        <div className="w-72 border-r">
          <div className="flex h-12 items-center justify-between border-b px-4">
            <Skeleton className="h-4 w-20" />
            <Skeleton className="h-6 w-6 rounded" />
          </div>
          <div className="divide-y">
            {Array.from({ length: 3 }).map((_, index) => (
              <div key={index} className="flex items-center gap-3 px-4 py-3">
                <Skeleton className="h-8 w-8 rounded-lg" />
                <div className="flex-1 space-y-1.5">
                  <Skeleton className="h-4 w-28" />
                  <Skeleton className="h-3 w-40" />
                </div>
              </div>
            ))}
          </div>
        </div>
        <div className="flex-1 p-6 space-y-4">
          <Skeleton className="h-8 w-48" />
          <Skeleton className="h-24 w-full rounded-lg" />
          <Skeleton className="h-56 w-full rounded-lg" />
        </div>
      </div>
    );
  }

  if (isMobile) {
    if (selectedProject) {
      return (
        <div className="flex flex-1 min-h-0 flex-col">
          <MobileDetailHeader
            title="Projects"
            subtitle={selectedProject.title}
            onBack={() => router.replace("/projects")}
          />
          <div className="min-h-0 flex-1">
            <ProjectDetailPanel
              project={selectedProject}
              onUpdate={handleUpdate}
              onDelete={handleDelete}
            />
          </div>
          <CreateProjectDialog
            open={createOpen}
            onOpenChange={setCreateOpen}
            onCreate={handleCreate}
          />
        </div>
      );
    }

    return (
      <div className="flex flex-1 min-h-0 flex-col">
        <div className="flex h-12 items-center justify-between border-b px-4">
          <div className="flex items-center gap-2">
            <FolderKanban className="h-4 w-4 text-muted-foreground" />
            <h1 className="text-sm font-semibold">Projects</h1>
          </div>
          <Button
            variant="ghost"
            size="icon-xs"
            aria-label="Create project"
            onClick={() => setCreateOpen(true)}
          >
            <Plus className="h-4 w-4 text-muted-foreground" />
          </Button>
        </div>

        <div className="flex-1 overflow-y-auto">
          {projects.length === 0 ? (
            <div className="flex flex-col items-center justify-center px-4 py-12">
              <FolderKanban className="h-8 w-8 text-muted-foreground/40" />
              <p className="mt-3 text-sm text-muted-foreground">No projects yet</p>
              <p className="mt-1 text-center text-xs text-muted-foreground">
                Create a project to group related issues and planning work.
              </p>
              <Button onClick={() => setCreateOpen(true)} size="xs" className="mt-3">
                <Plus className="h-3 w-3" />
                Create Project
              </Button>
            </div>
          ) : (
            <div className="divide-y">
              {projects.map((project) => (
                <ProjectListItem
                  key={project.id}
                  project={project}
                  isSelected={false}
                  onClick={() => handleSelect(project.id)}
                />
              ))}
            </div>
          )}
        </div>

        <CreateProjectDialog
          open={createOpen}
          onOpenChange={setCreateOpen}
          onCreate={handleCreate}
        />
      </div>
    );
  }

  return (
    <ResizablePanelGroup
      orientation="horizontal"
      className="flex-1 min-h-0"
      defaultLayout={defaultLayout}
      onLayoutChanged={onLayoutChanged}
    >
      <ResizablePanel
        id="list"
        defaultSize={280}
        minSize={240}
        maxSize={420}
        groupResizeBehavior="preserve-pixel-size"
      >
        <div className="h-full overflow-y-auto border-r">
          <div className="flex h-12 items-center justify-between border-b px-4">
            <div className="flex items-center gap-2">
              <FolderKanban className="h-4 w-4 text-muted-foreground" />
              <h1 className="text-sm font-semibold">Projects</h1>
            </div>
            <Button
              variant="ghost"
              size="icon-xs"
              aria-label="Create project"
              onClick={() => setCreateOpen(true)}
            >
              <Plus className="h-4 w-4 text-muted-foreground" />
            </Button>
          </div>

          {projects.length === 0 ? (
            <div className="flex flex-col items-center justify-center px-4 py-12">
              <FolderKanban className="h-8 w-8 text-muted-foreground/40" />
              <p className="mt-3 text-sm text-muted-foreground">No projects yet</p>
              <p className="mt-1 text-center text-xs text-muted-foreground">
                Create a project to group related issues and planning work.
              </p>
              <Button onClick={() => setCreateOpen(true)} size="xs" className="mt-3">
                <Plus className="h-3 w-3" />
                Create Project
              </Button>
            </div>
          ) : (
            <div className="divide-y">
              {projects.map((project) => (
                <ProjectListItem
                  key={project.id}
                  project={project}
                  isSelected={project.id === activeSelectedId}
                  onClick={() => handleSelect(project.id)}
                />
              ))}
            </div>
          )}
        </div>
      </ResizablePanel>

      <ResizableHandle />

      <ResizablePanel id="detail" minSize="50%">
        {selectedProject ? (
          <ProjectDetailPanel
            project={selectedProject}
            onUpdate={handleUpdate}
            onDelete={handleDelete}
          />
        ) : (
          <div className="flex h-full flex-col items-center justify-center text-muted-foreground">
            <FolderKanban className="h-10 w-10 text-muted-foreground/30" />
            <p className="mt-3 text-sm">Select a project to view details</p>
          </div>
        )}
      </ResizablePanel>

      <CreateProjectDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        onCreate={handleCreate}
      />
    </ResizablePanelGroup>
  );
}