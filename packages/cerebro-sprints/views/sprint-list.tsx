"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { FolderKanban } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@multica/ui/components/ui/dialog";
import { cn } from "@multica/ui/lib/utils";
import {
  projectTreeOptions,
  useSetProjectParent,
} from "@multica/core/projects/nesting";
import {
  getProjectColor,
  PROJECT_STATUS_CONFIG,
} from "@multica/core/projects/config";
import { useCreateProject, useDeleteProject } from "@multica/core/projects";
import type { ProjectTreeItem } from "@multica/core/types";

interface Props {
  workspaceId: string;
  projectId: string;
}

function openDatePicker(e: { currentTarget: HTMLInputElement }) {
  try {
    e.currentTarget.showPicker?.();
  } catch {
    // Native date inputs still focus normally when showPicker is unavailable.
  }
}

function flatten(nodes: ProjectTreeItem[]): ProjectTreeItem[] {
  const out: ProjectTreeItem[] = [];
  for (const node of nodes) {
    out.push(node);
    if (node.children?.length) out.push(...flatten(node.children));
  }
  return out;
}

export function SprintList({ workspaceId, projectId }: Props) {
  const treeQuery = useQuery(projectTreeOptions(workspaceId));
  const createProject = useCreateProject();
  const setProjectParent = useSetProjectParent(workspaceId);
  const deleteProject = useDeleteProject();

  const [open, setOpen] = useState(false);
  const [form, setForm] = useState({
    name: "Sprint 1",
    start_date: "",
    end_date: "",
    goal: "",
  });

  const projects = flatten(treeQuery.data ?? []);
  const sprints = projects.filter((project) => project.parent_project_id === projectId);

  function description() {
    return [
      form.start_date ? `Start: ${form.start_date}` : "",
      form.end_date ? `End: ${form.end_date}` : "",
      form.goal ? `Goal: ${form.goal}` : "",
    ].filter(Boolean).join("\n");
  }

  function submit() {
    if (!form.name.trim()) {
      toast.error("Name is required");
      return;
    }
    createProject.mutate(
      {
        title: form.name.trim(),
        description: description(),
        status: "planned",
      },
      {
        onSuccess: (project) => {
          setProjectParent.mutate(
            { id: project.id, parentProjectId: projectId },
            {
              onSuccess: () => {
                setOpen(false);
                toast.success("Sprint project created");
              },
              onError: () => toast.error("Sprint project was created, but parent placement failed"),
            },
          );
        },
        onError: () => toast.error("Failed to create sprint project"),
      },
    );
  }

  return (
    <div className="flex flex-col gap-3 p-6">
      <div className="flex items-center justify-between gap-3">
        <div>
          <h3 className="text-lg font-medium">Sprints</h3>
          <p className="text-sm text-muted-foreground">
            Sprints are sub-projects under this project. Move an issue into a sprint by setting its Project to the sprint sub-project.
          </p>
        </div>
        <Dialog open={open} onOpenChange={setOpen}>
          <DialogTrigger render={<Button size="sm">New sprint</Button>} />
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Create sprint project</DialogTitle>
            </DialogHeader>
            <div className="grid gap-3">
              <div className="grid gap-1.5">
                <Label htmlFor="sprint-name">Name</Label>
                <Input
                  id="sprint-name"
                  value={form.name}
                  onChange={(e) => setForm({ ...form, name: e.target.value })}
                />
              </div>
              <div className="grid grid-cols-2 gap-3">
                <div className="grid gap-1.5">
                  <Label htmlFor="sprint-start">Start</Label>
                  <Input
                    id="sprint-start"
                    type="date"
                    value={form.start_date}
                    onChange={(e) => setForm({ ...form, start_date: e.target.value })}
                    onClick={openDatePicker}
                    onFocus={openDatePicker}
                  />
                </div>
                <div className="grid gap-1.5">
                  <Label htmlFor="sprint-end">End</Label>
                  <Input
                    id="sprint-end"
                    type="date"
                    value={form.end_date}
                    onChange={(e) => setForm({ ...form, end_date: e.target.value })}
                    onClick={openDatePicker}
                    onFocus={openDatePicker}
                  />
                </div>
              </div>
              <div className="grid gap-1.5">
                <Label htmlFor="sprint-goal">Goal</Label>
                <Input
                  id="sprint-goal"
                  value={form.goal}
                  onChange={(e) => setForm({ ...form, goal: e.target.value })}
                />
              </div>
            </div>
            <DialogFooter>
              <Button variant="ghost" onClick={() => setOpen(false)}>
                Cancel
              </Button>
              <Button onClick={submit} disabled={createProject.isPending || setProjectParent.isPending}>
                Create
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </div>

      {treeQuery.isLoading && (
        <div className="text-sm text-muted-foreground">Loading sprint projects...</div>
      )}

      {sprints.length === 0 && !treeQuery.isLoading && (
        <div className="rounded-md border p-4 text-sm text-muted-foreground">
          No sprint sub-projects yet. Create one here, or use the project parent selector to place an existing project under this one.
        </div>
      )}

      <ul className="flex flex-col divide-y rounded-md border">
        {sprints.map((sprint) => (
          <li key={sprint.id} className="flex items-center justify-between gap-3 p-3">
            <div className="flex min-w-0 items-center gap-3">
              {sprint.icon ? (
                <span className="shrink-0 text-sm">{sprint.icon}</span>
              ) : sprint.color ? (
                <span className={cn("size-2.5 shrink-0 rounded-full", getProjectColor(sprint.color).dot)} />
              ) : (
                <FolderKanban className="h-4 w-4 shrink-0 text-muted-foreground" />
              )}
              <div className="min-w-0">
                <div className="flex min-w-0 items-center gap-2">
                  <span className="truncate font-medium">{sprint.title}</span>
                  <Badge
                    variant="secondary"
                    className={cn(
                      PROJECT_STATUS_CONFIG[sprint.status].badgeBg,
                      PROJECT_STATUS_CONFIG[sprint.status].badgeText,
                    )}
                  >
                    {PROJECT_STATUS_CONFIG[sprint.status].label}
                  </Badge>
                </div>
                <span className="text-xs text-muted-foreground">
                  {sprint.issue_count} issues · {sprint.done_count} done
                </span>
              </div>
            </div>
            <Button
              size="sm"
              variant="ghost"
              onClick={() => {
                if (!window.confirm(`Delete sprint project "${sprint.title}"?`)) return;
                deleteProject.mutate(sprint.id, {
                  onSuccess: () => toast.success("Sprint project deleted"),
                  onError: () => toast.error("Failed to delete sprint project"),
                });
              }}
            >
              Delete
            </Button>
          </li>
        ))}
      </ul>
    </div>
  );
}
