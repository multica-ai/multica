"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Textarea } from "@multica/ui/components/ui/textarea";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Switch } from "@multica/ui/components/ui/switch";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@multica/ui/components/ui/dialog";

import {
  recurringTasksOptions,
  useCreateRecurringTask,
  useDeleteRecurringTask,
  useUpdateRecurringTask,
} from "../core/queries";
import type { CadenceUnit, RecurringTaskWriteInput } from "../core/types";

interface Props {
  workspaceId: string;
  projectId: string;
}

const DEFAULT_FORM: RecurringTaskWriteInput = {
  cadence_unit: "week",
  cadence_count: 1,
  title: "",
  description: "",
  priority: "",
  enabled: true,
};

export function RecurringTasksPanel({ workspaceId, projectId }: Props) {
  const tasksQuery = useQuery(recurringTasksOptions(workspaceId, projectId));
  const create = useCreateRecurringTask(workspaceId, projectId);
  const update = useUpdateRecurringTask(workspaceId, projectId);
  const remove = useDeleteRecurringTask(workspaceId, projectId);

  const [open, setOpen] = useState(false);
  const [form, setForm] = useState<RecurringTaskWriteInput>(DEFAULT_FORM);

  function submit() {
    if (!form.title.trim()) {
      toast.error("Title is required");
      return;
    }
    create.mutate(form, {
      onSuccess: () => {
        setOpen(false);
        setForm(DEFAULT_FORM);
        toast.success("Recurring task added");
      },
      onError: () => toast.error("Failed to add recurring task"),
    });
  }

  const tasks = tasksQuery.data?.recurring_tasks ?? [];

  return (
    <div className="flex flex-col gap-3 p-6">
      <div className="flex items-center justify-between">
        <div>
          <h3 className="text-lg font-medium">Recurring tasks</h3>
          <p className="text-sm text-muted-foreground">
            Tasks here are cloned into every new sprint whose cadence matches.
          </p>
        </div>
        <Dialog open={open} onOpenChange={setOpen}>
          <DialogTrigger render={<Button size="sm">Add recurring task</Button>} />
          <DialogContent>
            <DialogHeader>
              <DialogTitle>New recurring task</DialogTitle>
            </DialogHeader>
            <div className="grid gap-3">
              <div className="grid gap-1.5">
                <Label htmlFor="rt-title">Title</Label>
                <Input
                  id="rt-title"
                  value={form.title}
                  onChange={(e) => setForm({ ...form, title: e.target.value })}
                />
              </div>
              <div className="grid grid-cols-2 gap-3">
                <div className="grid gap-1.5">
                  <Label htmlFor="rt-unit">Cadence unit</Label>
                  <Select
                    value={form.cadence_unit}
                    onValueChange={(v) =>
                      setForm({ ...form, cadence_unit: v as CadenceUnit })
                    }
                  >
                    <SelectTrigger id="rt-unit">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="day">Day</SelectItem>
                      <SelectItem value="week">Week</SelectItem>
                      <SelectItem value="month">Month</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                <div className="grid gap-1.5">
                  <Label htmlFor="rt-count">Cadence count</Label>
                  <Input
                    id="rt-count"
                    type="number"
                    min={1}
                    value={form.cadence_count}
                    onChange={(e) =>
                      setForm({
                        ...form,
                        cadence_count: Math.max(1, Number(e.target.value)),
                      })
                    }
                  />
                </div>
              </div>
              <div className="grid gap-1.5">
                <Label htmlFor="rt-desc">Description (optional)</Label>
                <Textarea
                  id="rt-desc"
                  value={form.description ?? ""}
                  onChange={(e) => setForm({ ...form, description: e.target.value })}
                />
              </div>
            </div>
            <DialogFooter>
              <Button variant="ghost" onClick={() => setOpen(false)}>
                Cancel
              </Button>
              <Button onClick={submit} disabled={create.isPending}>
                Add
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </div>

      {tasksQuery.isLoading && (
        <div className="text-sm text-muted-foreground">Loading recurring tasks…</div>
      )}

      {tasks.length === 0 && !tasksQuery.isLoading && (
        <div className="border rounded-md p-4 text-sm text-muted-foreground">
          No recurring tasks yet. Add one to have it automatically rolled into the next
          sprint of the matching cadence.
        </div>
      )}

      <ul className="flex flex-col divide-y border rounded-md">
        {tasks.map((task) => (
          <li key={task.id} className="flex items-center justify-between gap-3 p-3">
            <div className="flex flex-col gap-1">
              <span className="font-medium">{task.title}</span>
              <span className="text-xs text-muted-foreground">
                Every {task.cadence_count} {task.cadence_unit}
                {task.description ? ` · ${task.description}` : ""}
              </span>
            </div>
            <div className="flex items-center gap-3">
              <Switch
                checked={task.enabled}
                onCheckedChange={(v) =>
                  update.mutate(
                    {
                      id: task.id,
                      payload: {
                        cadence_unit: task.cadence_unit,
                        cadence_count: task.cadence_count,
                        title: task.title,
                        description: task.description,
                        priority: task.priority,
                        assignee_type: task.assignee_type,
                        assignee_id: task.assignee_id,
                        enabled: v,
                      },
                    },
                    {
                      onError: () => toast.error("Failed to toggle"),
                    },
                  )
                }
              />
              <Button
                size="sm"
                variant="ghost"
                onClick={() => {
                  if (!window.confirm(`Delete recurring task "${task.title}"?`)) return;
                  remove.mutate(task.id, {
                    onSuccess: () => toast.success("Recurring task deleted"),
                    onError: () => toast.error("Failed to delete"),
                  });
                }}
              >
                Delete
              </Button>
            </div>
          </li>
        ))}
      </ul>
    </div>
  );
}
