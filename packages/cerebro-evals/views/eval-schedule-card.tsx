"use client";

import { useEffect, useMemo, useState } from "react";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Input } from "@multica/ui/components/ui/input";
import { NativeSelect, NativeSelectOption } from "@multica/ui/components/ui/native-select";
import type { EvalSchedule, EvalScheduleInput } from "../types";

type ScheduleMode = "manual" | "daily" | "weekly";

const WEEKDAYS = [
  ["1", "Monday"], ["2", "Tuesday"], ["3", "Wednesday"], ["4", "Thursday"],
  ["5", "Friday"], ["6", "Saturday"], ["0", "Sunday"],
] as const;

export function scheduleFormValue(schedule: EvalSchedule | null): { mode: ScheduleMode; time: string; weekday: string; enabled: boolean } {
  if (!schedule) return { mode: "manual", time: "09:00", weekday: "1", enabled: true };
  const parts = schedule.schedule_expr.trim().split(/\s+/);
  const minute = parts[0] ?? "0";
  const hour = parts[1] ?? "9";
  const time = `${hour.padStart(2, "0")}:${minute.padStart(2, "0")}`;
  if (parts.length === 5 && parts[2] === "*" && parts[3] === "*" && parts[4] !== "*") {
    return { mode: "weekly", time, weekday: parts[4]!, enabled: schedule.enabled };
  }
  return { mode: "daily", time, weekday: "1", enabled: schedule.enabled };
}

export function buildScheduleInput(mode: ScheduleMode, time: string, weekday: string, enabled: boolean): EvalScheduleInput | null {
  if (mode === "manual") return null;
  const [hour = "9", minute = "0"] = time.split(":");
  return {
    schedule_expr: `${Number(minute)} ${Number(hour)} * * ${mode === "weekly" ? weekday : "*"}`,
    timezone: "Europe/Copenhagen",
    enabled,
  };
}

export function EvalScheduleCard({ schedule, loading, pending, error, onSave, onOpenHooks }: {
  schedule: EvalSchedule | null;
  loading: boolean;
  pending: boolean;
  error: boolean;
  onSave: (input: EvalScheduleInput | null) => void;
  onOpenHooks: () => void;
}) {
  const initial = useMemo(() => scheduleFormValue(schedule), [schedule]);
  const [mode, setMode] = useState<ScheduleMode>(initial.mode);
  const [time, setTime] = useState(initial.time);
  const [weekday, setWeekday] = useState(initial.weekday);
  const [enabled, setEnabled] = useState(initial.enabled);

  useEffect(() => {
    setMode(initial.mode); setTime(initial.time); setWeekday(initial.weekday); setEnabled(initial.enabled);
  }, [initial]);

  return (
    <div className="flex flex-col gap-3 rounded-md border p-3">
      <div><h3 className="text-xs font-semibold">Schedule</h3><p className="text-[11px] text-muted-foreground">Run this eval manually, every day, or once a week in Europe/Copenhagen time.</p></div>
      {loading ? <p className="text-xs text-muted-foreground">Loading schedule…</p> : <>
        <div className="grid gap-3 sm:grid-cols-3">
          <label className="flex flex-col gap-1 text-xs font-medium">Frequency
            <NativeSelect className="w-full" aria-label="Frequency" value={mode} onChange={(event) => setMode(event.target.value as ScheduleMode)}>
              <NativeSelectOption value="manual">Manual only</NativeSelectOption>
              <NativeSelectOption value="daily">Daily</NativeSelectOption>
              <NativeSelectOption value="weekly">Weekly</NativeSelectOption>
            </NativeSelect>
          </label>
          {mode === "weekly" && <label className="flex flex-col gap-1 text-xs font-medium">Weekday
            <NativeSelect className="w-full" aria-label="Weekday" value={weekday} onChange={(event) => setWeekday(event.target.value)}>
              {WEEKDAYS.map(([value, label]) => <NativeSelectOption key={value} value={value}>{label}</NativeSelectOption>)}
            </NativeSelect>
          </label>}
          {mode !== "manual" && <label className="flex flex-col gap-1 text-xs font-medium">Time<Input aria-label="Time" type="time" value={time} onChange={(event) => setTime(event.target.value)} /></label>}
        </div>
        {mode !== "manual" && <label className="flex items-center gap-2 text-xs"><Checkbox checked={enabled} onCheckedChange={(checked) => setEnabled(checked === true)} />Schedule enabled</label>}
        {schedule?.next_run_at && mode !== "manual" && <p className="text-[11px] text-muted-foreground">Next run: {new Date(schedule.next_run_at).toLocaleString()}</p>}
        {error && <p className="text-xs text-destructive">Failed to save schedule.</p>}
        <div><Button size="sm" disabled={pending} onClick={() => onSave(buildScheduleInput(mode, time, weekday, enabled))}>Save schedule</Button></div>
      </>}
      <div className="border-t pt-3"><p className="text-[11px] text-muted-foreground">To run when a target changes, add an <code>eval.run</code> action to a Hook.</p><Button size="sm" variant="link" className="h-auto px-0" onClick={onOpenHooks}>Open Workflows and Hooks</Button></div>
    </div>
  );
}
