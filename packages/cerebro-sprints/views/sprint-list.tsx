"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
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

import { projectSprintsOptions, useCreateSprint, useDeleteSprint } from "../core/queries";
import type { Sprint, SprintStatus } from "../core/types";

interface Props {
  workspaceId: string;
  projectId: string;
  onSelect?: (sprint: Sprint | null) => void;
  selectedSprintId?: string | null;
}

const STATUS_VARIANT: Record<SprintStatus, "secondary" | "default" | "outline" | "destructive"> = {
  planned: "outline",
  active: "default",
  done: "secondary",
  cancelled: "destructive",
};

export function SprintList({ workspaceId, projectId, onSelect, selectedSprintId }: Props) {
  const sprintsQuery = useQuery(projectSprintsOptions(workspaceId, projectId));
  const create = useCreateSprint(workspaceId, projectId);
  const remove = useDeleteSprint(workspaceId, projectId);

  const [open, setOpen] = useState(false);
  const [form, setForm] = useState({
    name: "Sprint 1",
    start_date: "",
    end_date: "",
    goal: "",
  });

  function submit() {
    if (!form.name || !form.start_date || !form.end_date) {
      toast.error("Name, start date and end date are required");
      return;
    }
    create.mutate(
      {
        name: form.name,
        start_date: form.start_date,
        end_date: form.end_date,
        goal: form.goal || undefined,
        status: "planned",
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

  const sprints = sprintsQuery.data?.sprints ?? [];

  return (
    <div className="flex flex-col gap-3 p-6">
      <div className="flex items-center justify-between">
        <h3 className="text-lg font-medium">Sprints</h3>
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
                  <Label htmlFor="sprint-start">Start (YYYY-MM-DD)</Label>
                  <Input
                    id="sprint-start"
                    type="date"
                    value={form.start_date}
                    onChange={(e) => setForm({ ...form, start_date: e.target.value })}
                  />
                </div>
                <div className="grid gap-1.5">
                  <Label htmlFor="sprint-end">End (YYYY-MM-DD)</Label>
                  <Input
                    id="sprint-end"
                    type="date"
                    value={form.end_date}
                    onChange={(e) => setForm({ ...form, end_date: e.target.value })}
                  />
                </div>
              </div>
              <div className="grid gap-1.5">
                <Label htmlFor="sprint-goal">Goal (optional)</Label>
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
              <Button onClick={submit} disabled={create.isPending}>
                Create
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </div>

      {sprintsQuery.isLoading && (
        <div className="text-sm text-muted-foreground">Loading sprints…</div>
      )}

      {sprints.length === 0 && !sprintsQuery.isLoading && (
        <div className="border rounded-md p-4 text-sm text-muted-foreground">
          No sprints yet. Create the first sprint to start the sequence — the daily sweeper
          will then auto-create the next one based on your settings.
        </div>
      )}

      <ul className="flex flex-col divide-y border rounded-md">
        {sprints.map((sprint) => {
          const isSelected = sprint.id === selectedSprintId;
          return (
            <li
              key={sprint.id}
              className={`flex items-center justify-between gap-3 p-3 cursor-pointer hover:bg-muted/40 ${
                isSelected ? "bg-muted/60" : ""
              }`}
              onClick={() => onSelect?.(sprint)}
            >
              <div className="flex flex-col gap-1">
                <div className="flex items-center gap-2">
                  <span className="font-medium">{sprint.name}</span>
                  <Badge variant={STATUS_VARIANT[sprint.status]}>{sprint.status}</Badge>
                </div>
                <span className="text-xs text-muted-foreground">
                  {sprint.start_date} → {sprint.end_date}
                  {sprint.goal ? ` · ${sprint.goal}` : ""}
                </span>
              </div>
              <Button
                size="sm"
                variant="ghost"
                onClick={(e) => {
                  e.stopPropagation();
                  if (!window.confirm(`Delete sprint "${sprint.name}"?`)) return;
                  remove.mutate(sprint.id, {
                    onSuccess: () => {
                      if (isSelected) onSelect?.(null);
                      toast.success("Sprint deleted");
                    },
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
