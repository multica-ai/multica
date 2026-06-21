"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
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
  sprintSettingsOptions,
  useCreateRecurringTask,
  useDeleteRecurringTask,
  useUpdateRecurringTask,
} from "../core/queries";
import type { CadenceUnit, RecurringTask, RecurringTaskWriteInput } from "../core/types";

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
  sprint_day_offset: 1,
  enabled: true,
};

const WEEKDAY_LABELS = [
  "Monday",
  "Tuesday",
  "Wednesday",
  "Thursday",
  "Friday",
  "Saturday",
  "Sunday",
] as const;

const UNASSIGNED_VALUE = "none";

export function sprintWeekdayLabel(startWeekday: number, sprintDayOffset: number): string {
  const normalizedStart = Math.min(7, Math.max(1, Math.trunc(startWeekday || 1)));
  const normalizedOffset = Math.max(1, Math.trunc(sprintDayOffset || 1));
  const index = (normalizedStart + normalizedOffset - 2) % 7;
  return WEEKDAY_LABELS[index] ?? "Monday";
}

function formFromTask(task: RecurringTask): RecurringTaskWriteInput {
  return {
    cadence_unit: task.cadence_unit,
    cadence_count: task.cadence_count,
    title: task.title,
    description: task.description ?? "",
    priority: task.priority ?? "",
    assignee_type: task.assignee_type,
    assignee_id: task.assignee_id,
    sprint_day_offset: task.sprint_day_offset,
    enabled: task.enabled,
  };
}

function assigneeValue(form: RecurringTaskWriteInput): string {
  return form.assignee_type && form.assignee_id
    ? `${form.assignee_type}:${form.assignee_id}`
    : UNASSIGNED_VALUE;
}

function assigneeLabel(
  form: RecurringTaskWriteInput,
  members: Array<{ user_id: string; name: string }>,
  agents: Array<{ id: string; name: string; archived_at?: string | null }>,
): string {
  if (!form.assignee_type || !form.assignee_id) return "Unassigned";
  if (form.assignee_type === "member") {
    return members.find((member) => member.user_id === form.assignee_id)?.name ?? "Assigned";
  }
  return agents.find((agent) => agent.id === form.assignee_id)?.name ?? "Assigned";
}

function applyAssigneeValue(
  form: RecurringTaskWriteInput,
  value: string,
): RecurringTaskWriteInput {
  if (value === UNASSIGNED_VALUE) {
    const { assignee_type: _type, assignee_id: _id, ...rest } = form;
    return rest;
  }
  const [type, id] = value.split(":");
  if ((type !== "member" && type !== "agent") || !id) return form;
  return { ...form, assignee_type: type, assignee_id: id };
}

function RecurringTaskForm({
  form,
  setForm,
  members,
  agents,
  startWeekday,
}: {
  form: RecurringTaskWriteInput;
  setForm: (next: RecurringTaskWriteInput) => void;
  members: Array<{ user_id: string; name: string }>;
  agents: Array<{ id: string; name: string; archived_at?: string | null }>;
  startWeekday: number | null;
}) {
  const sprintDay = form.sprint_day_offset ?? 1;
  const weekday = startWeekday ? sprintWeekdayLabel(startWeekday, sprintDay) : null;
  const activeAgents = agents.filter((agent) => !agent.archived_at);

  return (
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
            onValueChange={(v) => setForm({ ...form, cadence_unit: v as CadenceUnit })}
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
        <Label htmlFor="rt-sprint-day">Sprint day</Label>
        <Input
          id="rt-sprint-day"
          type="number"
          min={1}
          value={sprintDay}
          onChange={(e) =>
            setForm({
              ...form,
              sprint_day_offset: Math.max(1, Number(e.target.value)),
            })
          }
        />
        <span className="text-xs text-muted-foreground">
          {weekday
            ? `Day ${sprintDay} is ${weekday}.`
            : "Day 1 is the sprint start date."}
        </span>
      </div>
      <div className="grid gap-1.5">
        <Label htmlFor="rt-assignee">Assignee</Label>
        <Select
          value={assigneeValue(form)}
          onValueChange={(value) => {
            if (!value) return;
            setForm(applyAssigneeValue(form, value));
          }}
        >
          <SelectTrigger id="rt-assignee" aria-label="Assignee">
            <SelectValue>{() => assigneeLabel(form, members, agents)}</SelectValue>
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={UNASSIGNED_VALUE}>Unassigned</SelectItem>
            {members.map((member) => (
              <SelectItem key={`member:${member.user_id}`} value={`member:${member.user_id}`}>
                {member.name}
              </SelectItem>
            ))}
            {activeAgents.map((agent) => (
              <SelectItem key={`agent:${agent.id}`} value={`agent:${agent.id}`}>
                {agent.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
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
  );
}

export function RecurringTasksPanel({ workspaceId, projectId }: Props) {
  const tasksQuery = useQuery(recurringTasksOptions(workspaceId, projectId));
  const settingsQuery = useQuery(sprintSettingsOptions(workspaceId, projectId));
  const { data: members = [] } = useQuery(memberListOptions(workspaceId));
  const { data: agents = [] } = useQuery(agentListOptions(workspaceId));
  const create = useCreateRecurringTask(workspaceId, projectId);
  const update = useUpdateRecurringTask(workspaceId, projectId);
  const remove = useDeleteRecurringTask(workspaceId, projectId);

  const [createOpen, setCreateOpen] = useState(false);
  const [editingTask, setEditingTask] = useState<RecurringTask | null>(null);
  const [form, setForm] = useState<RecurringTaskWriteInput>(DEFAULT_FORM);
  const startWeekday = settingsQuery.data?.start_weekday ?? null;

  function submit() {
    if (!form.title.trim()) {
      toast.error("Title is required");
      return;
    }
    create.mutate(form, {
      onSuccess: () => {
        setCreateOpen(false);
        setForm(DEFAULT_FORM);
        toast.success("Recurring task added");
      },
      onError: () => toast.error("Failed to add recurring task"),
    });
  }

  function saveEdit() {
    if (!editingTask) return;
    if (!form.title.trim()) {
      toast.error("Title is required");
      return;
    }
    update.mutate(
      { id: editingTask.id, payload: form },
      {
        onSuccess: () => {
          setEditingTask(null);
          setForm(DEFAULT_FORM);
          toast.success("Recurring task updated");
        },
        onError: () => toast.error("Failed to update recurring task"),
      },
    );
  }

  const tasks = tasksQuery.data?.recurring_tasks ?? [];
  const assigneeNames = useMemo(() => {
    const names = new Map<string, string>();
    for (const member of members) names.set(`member:${member.user_id}`, member.name);
    for (const agent of agents) names.set(`agent:${agent.id}`, agent.name);
    return names;
  }, [agents, members]);

  return (
    <div className="flex flex-col gap-3 p-6">
      <div className="flex items-center justify-between">
        <div>
          <h3 className="text-lg font-medium">Recurring tasks</h3>
          <p className="text-sm text-muted-foreground">
            Tasks here are cloned into every new sprint whose cadence matches.
          </p>
        </div>
        <Dialog
          open={createOpen}
          onOpenChange={(nextOpen) => {
            setCreateOpen(nextOpen);
            if (nextOpen) setForm(DEFAULT_FORM);
          }}
        >
          <DialogTrigger render={<Button size="sm">Add recurring task</Button>} />
          <DialogContent>
            <DialogHeader>
              <DialogTitle>New recurring task</DialogTitle>
            </DialogHeader>
            <RecurringTaskForm
              form={form}
              setForm={setForm}
              members={members}
              agents={agents}
              startWeekday={startWeekday}
            />
            <DialogFooter>
              <Button variant="ghost" onClick={() => setCreateOpen(false)}>
                Cancel
              </Button>
              <Button onClick={submit} disabled={create.isPending}>
                Add
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </div>

      <Dialog
        open={!!editingTask}
        onOpenChange={(nextOpen) => {
          if (!nextOpen) {
            setEditingTask(null);
            setForm(DEFAULT_FORM);
          }
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Edit recurring task</DialogTitle>
          </DialogHeader>
          <RecurringTaskForm
            form={form}
            setForm={setForm}
            members={members}
            agents={agents}
            startWeekday={startWeekday}
          />
          <DialogFooter>
            <Button
              variant="ghost"
              onClick={() => {
                setEditingTask(null);
                setForm(DEFAULT_FORM);
              }}
            >
              Cancel
            </Button>
            <Button onClick={saveEdit} disabled={update.isPending}>
              Save
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

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
                {` · day ${task.sprint_day_offset}`}
                {startWeekday
                  ? ` (${sprintWeekdayLabel(startWeekday, task.sprint_day_offset)})`
                  : ""}
                {task.assignee_type && task.assignee_id
                  ? ` · ${assigneeNames.get(`${task.assignee_type}:${task.assignee_id}`) ?? "Assigned"}`
                  : ""}
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
                        sprint_day_offset: task.sprint_day_offset,
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
                  setEditingTask(task);
                  setForm(formFromTask(task));
                }}
              >
                Edit
              </Button>
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
