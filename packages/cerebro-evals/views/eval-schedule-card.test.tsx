// @vitest-environment jsdom

import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { buildScheduleInput, EvalScheduleCard, scheduleFormValue } from "./eval-schedule-card";
import type { EvalSchedule } from "../types";

const schedule: EvalSchedule = {
  id: "s1", workspace_id: "w1", eval_id: "e1", schedule_expr: "30 8 * * 1",
  timezone: "Europe/Copenhagen", enabled: true, next_run_at: "2026-07-20T06:30:00Z",
  created_by_id: "u1", created_at: "2026-07-19T00:00:00Z",
};

describe("EvalScheduleCard", () => {
  it("round-trips weekly schedule choices", () => {
    expect(scheduleFormValue(schedule)).toMatchObject({ mode: "weekly", time: "08:30", weekday: "1" });
    expect(buildScheduleInput("weekly", "08:30", "3", true)).toEqual({
      schedule_expr: "30 8 * * 3", timezone: "Europe/Copenhagen", enabled: true,
    });
  });

  it("saves daily and manual-only choices", () => {
    const onSave = vi.fn();
    render(<EvalScheduleCard schedule={null} loading={false} pending={false} error={false} onSave={onSave} onOpenHooks={() => {}} />);
    fireEvent.change(screen.getByLabelText("Frequency"), { target: { value: "daily" } });
    fireEvent.change(screen.getByLabelText("Time"), { target: { value: "10:15" } });
    fireEvent.click(screen.getByRole("button", { name: "Save schedule" }));
    expect(onSave).toHaveBeenLastCalledWith({ schedule_expr: "15 10 * * *", timezone: "Europe/Copenhagen", enabled: true });
    fireEvent.change(screen.getByLabelText("Frequency"), { target: { value: "manual" } });
    fireEvent.click(screen.getByRole("button", { name: "Save schedule" }));
    expect(onSave).toHaveBeenLastCalledWith(null);
  });

  it("opens Hooks for change-triggered runs", () => {
    const onOpenHooks = vi.fn();
    render(<EvalScheduleCard schedule={schedule} loading={false} pending={false} error={false} onSave={() => {}} onOpenHooks={onOpenHooks} />);
    fireEvent.click(screen.getByRole("button", { name: "Open Workflows and Hooks" }));
    expect(onOpenHooks).toHaveBeenCalledOnce();
  });
});
