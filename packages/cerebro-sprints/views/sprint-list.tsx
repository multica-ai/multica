"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { CalendarRange } from "lucide-react";
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
import { useWorkspacePaths } from "@multica/core/paths";
// Imported as a leaf primitive WITHOUT declaring `@multica/views` in this
// package's package.json — `views` already depends on `@multica/cerebro-sprints`,
// so declaring the reverse edge creates a turbo build cycle (broke all frontend
// CI on main, #1352). Same phantom-import pattern as cerebro-groups. Do not add
// `@multica/views` to dependencies here.
import { AppLink } from "@multica/views/navigation";

import { projectSprintsOptions, useCreateSprint, useDeleteSprint } from "../core/queries";
import type { SprintStatus } from "../core/types";

interface Props {
  workspaceId: string;
  projectId: string;
}

// Sprint status → badge styling. Sprints carry their own lifecycle
// (planned/active/done/cancelled), distinct from project status.
const SPRINT_STATUS_CONFIG: Record<SprintStatus, { label: string; className: string }> = {
  planned: { label: "Planned", className: "bg-muted text-muted-foreground" },
  active: { label: "Active", className: "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400" },
  done: { label: "Done", className: "bg-blue-500/15 text-blue-600 dark:text-blue-400" },
  cancelled: { label: "Cancelled", className: "bg-destructive/15 text-destructive" },
};

function openDatePicker(e: { currentTarget: HTMLInputElement }) {
  try {
    e.currentTarget.showPicker?.();
  } catch {
    // Native date inputs still focus normally when showPicker is unavailable.
  }
}

export function SprintList({ workspaceId, projectId }: Props) {
  const sprintsQuery = useQuery(projectSprintsOptions(workspaceId, projectId));
  const createSprint = useCreateSprint(workspaceId, projectId);
  const deleteSprint = useDeleteSprint(workspaceId, projectId);
  const paths = useWorkspacePaths();

  const [open, setOpen] = useState(false);
  const [form, setForm] = useState({
    name: "Sprint 1",
    start_date: "",
    end_date: "",
    goal: "",
  });

  const sprints = sprintsQuery.data?.sprints ?? [];

  function submit() {
    if (!form.name.trim()) {
      toast.error("Name is required");
      return;
    }
    if (!form.start_date || !form.end_date) {
      toast.error("Start and end dates are required");
      return;
    }
    if (form.end_date < form.start_date) {
      toast.error("End date must be on or after start date");
      return;
    }
    createSprint.mutate(
      {
        name: form.name.trim(),
        start_date: form.start_date,
        end_date: form.end_date,
        goal: form.goal.trim() || undefined,
      },
      {
        onSuccess: () => {
          setOpen(false);
          toast.success("Sprint created");
        },
        onError: () => toast.error("Failed to create sprint"),
      },
    );
  }

  return (
    <div className="flex flex-col gap-3 p-6">
      <div className="flex items-center justify-between gap-3">
        <div>
          <h3 className="text-lg font-medium">Sprints</h3>
          <p className="text-sm text-muted-foreground">
            Sprints belong to this project. Assign an issue to a sprint from the issue&apos;s
            Sprint picker — the issue stays in its project and also shows on the sprint board.
          </p>
        </div>
        <Dialog open={open} onOpenChange={setOpen}>
          <DialogTrigger render={<Button size="sm">New sprint</Button>} />
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Create sprint</DialogTitle>
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
              <Button onClick={submit} disabled={createSprint.isPending}>
                Create
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </div>

      {sprintsQuery.isLoading && (
        <div className="text-sm text-muted-foreground">Loading sprints...</div>
      )}

      {sprints.length === 0 && !sprintsQuery.isLoading && (
        <div className="rounded-md border p-4 text-sm text-muted-foreground">
          No sprints yet. Create one here, then assign issues to it from the issue sidebar.
        </div>
      )}

      <ul className="flex flex-col divide-y rounded-md border">
        {sprints.map((sprint) => {
          const statusConfig = SPRINT_STATUS_CONFIG[sprint.status] ?? SPRINT_STATUS_CONFIG.planned;
          return (
            <li key={sprint.id} className="flex items-center justify-between gap-3 p-3">
              <div className="flex min-w-0 items-center gap-3">
                <CalendarRange className="h-4 w-4 shrink-0 text-muted-foreground" />
                <div className="min-w-0">
                  <div className="flex min-w-0 items-center gap-2">
                    {/* Clicking the sprint opens it as its own board view (create + drag-and-drop). */}
                    <AppLink
                      href={paths.sprintDetail(sprint.id)}
                      className="truncate font-medium hover:underline"
                    >
                      {sprint.name}
                    </AppLink>
                    <Badge variant="secondary" className={cn(statusConfig.className)}>
                      {statusConfig.label}
                    </Badge>
                  </div>
                  <span className="text-xs text-muted-foreground">
                    {sprint.start_date} → {sprint.end_date}
                    {sprint.goal ? ` · ${sprint.goal}` : ""}
                  </span>
                </div>
              </div>
              <Button
                size="sm"
                variant="ghost"
                onClick={() => {
                  if (!window.confirm(`Delete sprint "${sprint.name}"?`)) return;
                  deleteSprint.mutate(sprint.id, {
                    onSuccess: () => toast.success("Sprint deleted"),
                    onError: () => toast.error("Failed to delete sprint"),
                  });
                }}
              >
                Delete
              </Button>
            </li>
          );
        })}
      </ul>
    </div>
  );
}
