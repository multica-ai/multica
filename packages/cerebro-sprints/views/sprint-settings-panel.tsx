"use client";

import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Switch } from "@multica/ui/components/ui/switch";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";

import {
  sprintSettingsOptions,
  useDeleteSprintSettings,
  useUpsertSprintSettings,
} from "../core/queries";
import type { CadenceUnit, SprintSettingsWriteInput } from "../core/types";

interface Props {
  workspaceId: string;
  projectId: string;
}

const WEEKDAY_LABELS = [
  { value: "1", label: "Monday" },
  { value: "2", label: "Tuesday" },
  { value: "3", label: "Wednesday" },
  { value: "4", label: "Thursday" },
  { value: "5", label: "Friday" },
  { value: "6", label: "Saturday" },
  { value: "7", label: "Sunday" },
];

const DEFAULT_FORM: Required<SprintSettingsWriteInput> = {
  enabled: true,
  duration_unit: "week",
  duration_count: 2,
  start_weekday: 1,
  name_template: "Sprint {n}",
  auto_create_enabled: true,
  auto_create_lead_days: 2,
  move_incomplete_enabled: true,
  timezone: "UTC",
};

export function SprintSettingsPanel({ workspaceId, projectId }: Props) {
  const settingsQuery = useQuery(sprintSettingsOptions(workspaceId, projectId));
  const upsert = useUpsertSprintSettings(workspaceId, projectId);
  const remove = useDeleteSprintSettings(workspaceId, projectId);

  const [form, setForm] = useState<Required<SprintSettingsWriteInput>>(DEFAULT_FORM);

  useEffect(() => {
    if (settingsQuery.data) {
      setForm({
        enabled: settingsQuery.data.enabled,
        duration_unit: settingsQuery.data.duration_unit,
        duration_count: settingsQuery.data.duration_count,
        start_weekday: settingsQuery.data.start_weekday,
        name_template: settingsQuery.data.name_template,
        auto_create_enabled: settingsQuery.data.auto_create_enabled,
        auto_create_lead_days: settingsQuery.data.auto_create_lead_days,
        move_incomplete_enabled: settingsQuery.data.move_incomplete_enabled,
        timezone: settingsQuery.data.timezone,
      });
    }
  }, [settingsQuery.data]);

  const exists = settingsQuery.data != null;

  function save() {
    upsert.mutate(form, {
      onSuccess: () => toast.success("Sprint settings saved"),
      onError: () => toast.error("Failed to save sprint settings"),
    });
  }

  function disableFeature() {
    remove.mutate(undefined, {
      onSuccess: () => toast.success("Sprints turned off for this project"),
      onError: () => toast.error("Failed to disable sprints"),
    });
  }

  if (settingsQuery.isLoading) {
    return <div className="p-4 text-sm text-muted-foreground">Loading sprint settings…</div>;
  }

  return (
    <div className="flex flex-col gap-6 p-6 max-w-2xl">
      <div className="flex items-center justify-between">
        <div>
          <h3 className="text-lg font-medium">Sprint container</h3>
          <p className="text-sm text-muted-foreground">
            Turn this project into a recurring sprint container. Sprints are auto-created
            ahead of time using your settings — no values are hardcoded.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Label htmlFor="sprint-enabled" className="text-sm">
            Enabled
          </Label>
          <Switch
            id="sprint-enabled"
            checked={form.enabled}
            onCheckedChange={(v) => setForm({ ...form, enabled: v })}
          />
        </div>
      </div>

      <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="duration-unit">Duration unit</Label>
          <Select
            value={form.duration_unit}
            onValueChange={(v) => setForm({ ...form, duration_unit: v as CadenceUnit })}
          >
            <SelectTrigger id="duration-unit">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="day">Day</SelectItem>
              <SelectItem value="week">Week</SelectItem>
              <SelectItem value="month">Month</SelectItem>
            </SelectContent>
          </Select>
        </div>

        <div className="flex flex-col gap-1.5">
          <Label htmlFor="duration-count">Duration count</Label>
          <Input
            id="duration-count"
            type="number"
            min={1}
            value={form.duration_count}
            onChange={(e) =>
              setForm({ ...form, duration_count: Math.max(1, Number(e.target.value)) })
            }
          />
        </div>

        {form.duration_unit === "week" && (
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="start-weekday">Start weekday</Label>
            <Select
              value={String(form.start_weekday)}
              onValueChange={(v) => setForm({ ...form, start_weekday: Number(v) })}
            >
              <SelectTrigger id="start-weekday">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {WEEKDAY_LABELS.map((w) => (
                  <SelectItem key={w.value} value={w.value}>
                    {w.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        )}

        <div className="flex flex-col gap-1.5">
          <Label htmlFor="name-template">Name template</Label>
          <Input
            id="name-template"
            value={form.name_template}
            onChange={(e) => setForm({ ...form, name_template: e.target.value })}
          />
          <span className="text-xs text-muted-foreground">
            {"Use {n} for the sprint number."}
          </span>
        </div>

        <div className="flex flex-col gap-1.5">
          <Label htmlFor="timezone">Timezone</Label>
          <Input
            id="timezone"
            value={form.timezone}
            onChange={(e) => setForm({ ...form, timezone: e.target.value })}
            placeholder="UTC, Europe/Copenhagen, …"
          />
        </div>
      </div>

      <div className="border-t pt-4 flex flex-col gap-4">
        <h4 className="text-sm font-medium">Auto-create next sprint</h4>
        <div className="flex items-center justify-between">
          <Label htmlFor="auto-create-enabled" className="text-sm font-normal">
            Auto-create the next sprint before the current one ends
          </Label>
          <Switch
            id="auto-create-enabled"
            checked={form.auto_create_enabled}
            onCheckedChange={(v) => setForm({ ...form, auto_create_enabled: v })}
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="auto-create-lead-days">Create how many days before the end?</Label>
          <Input
            id="auto-create-lead-days"
            type="number"
            min={0}
            value={form.auto_create_lead_days}
            onChange={(e) =>
              setForm({
                ...form,
                auto_create_lead_days: Math.max(0, Number(e.target.value)),
              })
            }
          />
          <span className="text-xs text-muted-foreground">
            This is the setting — there is no hardcoded "2 days". Change it freely.
          </span>
        </div>
        <div className="flex items-center justify-between">
          <Label htmlFor="move-incomplete" className="text-sm font-normal">
            Move incomplete issues into the new sprint
          </Label>
          <Switch
            id="move-incomplete"
            checked={form.move_incomplete_enabled}
            onCheckedChange={(v) => setForm({ ...form, move_incomplete_enabled: v })}
          />
        </div>
      </div>

      <div className="flex items-center gap-2">
        <Button onClick={save} disabled={upsert.isPending}>
          {exists ? "Save changes" : "Enable sprints"}
        </Button>
        {exists && (
          <Button
            variant="ghost"
            onClick={disableFeature}
            disabled={remove.isPending}
          >
            Turn off
          </Button>
        )}
      </div>
    </div>
  );
}
