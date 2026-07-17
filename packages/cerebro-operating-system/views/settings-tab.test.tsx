import "@testing-library/jest-dom/vitest";
import { fireEvent, render, renderHook, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { OperatingSystemSettingsTab, useCerebroOperatingSystemSettingsTabs } from "./settings-tab";

const state = vi.hoisted(() => ({
  enabled: true,
  terminology: { strategy: "Strategy", rock: "Rock", rocks: "Rocks", vision_plan: "Vision Plan", meetings: "Meetings", org_chart: "Org Chart", scorecard: "Scorecard", issues_list: "Issues List", strategy_map: "Strategy Map" },
  elements: [
    { key: "goals", enabled: true, default_enabled: true },
    { key: "scorecard", enabled: false, default_enabled: false },
  ],
  goalTypes: [{ id: "type-1", workspace_id: "workspace-1", name: "Company", color: "#22C55E", scope_label: "company-wide", position: 0, created_at: "", updated_at: "" }],
  periods: [{ id: "period-1", workspace_id: "workspace-1", name: "Q3 2026", unit: "quarter", starts_on: "2026-07-01", ends_on: "2026-09-30" }],
  updateSettings: vi.fn(),
  updateElement: vi.fn(),
  saveGoalType: vi.fn(),
  deleteGoalType: vi.fn(),
  savePeriod: vi.fn(),
  deletePeriod: vi.fn(),
}));

vi.mock("@multica/cerebro-feature-flags", () => ({ useFeatureFlag: () => state.enabled }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQuery: (options: { queryKey: readonly string[] }) => {
    if (options.queryKey.includes("settings")) return { data: { terminology: state.terminology } };
    if (options.queryKey.includes("elements")) return { data: { elements: state.elements }, isLoading: false, isError: false };
    if (options.queryKey.includes("goal-types")) return { data: { goal_types: state.goalTypes }, isLoading: false, isError: false };
    if (options.queryKey.includes("periods")) return { data: { periods: state.periods }, isLoading: false, isError: false };
    return { data: undefined };
  },
}));
vi.mock("../core/queries", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../core/queries")>()),
  useUpdateSettings: () => ({ mutate: state.updateSettings, isPending: false }),
  useUpdateElement: () => ({ mutate: state.updateElement, isPending: false }),
  useSaveGoalType: () => ({ mutate: state.saveGoalType, isPending: false }),
  useDeleteGoalType: () => ({ mutate: state.deleteGoalType, isPending: false }),
  useSavePeriod: () => ({ mutate: state.savePeriod, isPending: false }),
  useDeletePeriod: () => ({ mutate: state.deletePeriod, isPending: false }),
}));

describe("OperatingSystemSettingsTab", () => {
  beforeEach(() => {
    state.enabled = true;
    state.updateSettings.mockReset();
    state.updateElement.mockReset();
    state.saveGoalType.mockReset();
    state.deleteGoalType.mockReset();
    state.savePeriod.mockReset();
    state.deletePeriod.mockReset();
  });

  it("registers only while the Operating System is enabled", () => {
    expect(renderHook(() => useCerebroOperatingSystemSettingsTabs()).result.current[0]?.label).toBe("Operating System");
    state.enabled = false;
    expect(renderHook(() => useCerebroOperatingSystemSettingsTabs()).result.current).toEqual([]);
  });

  it("toggles elements per workspace", () => {
    render(<OperatingSystemSettingsTab />);
    expect(screen.getByLabelText("Rocks enabled")).toBeChecked();
    fireEvent.click(screen.getByLabelText("Scorecard enabled"));
    expect(state.updateElement).toHaveBeenCalledWith({ key: "scorecard", enabled: true });
  });

  it("saves the full terminology set including element labels", () => {
    render(<OperatingSystemSettingsTab />);
    fireEvent.change(screen.getByLabelText("Strategy label"), { target: { value: "Direction" } });
    fireEvent.click(screen.getByRole("button", { name: "Save terminology" }));
    expect(state.updateSettings).toHaveBeenCalledWith({ ...state.terminology, strategy: "Direction" }, expect.any(Object));
  });

  it("creates a goal type from the Settings tab", () => {
    render(<OperatingSystemSettingsTab />);
    expect(screen.getByText("Company")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "+ New type" }));
    fireEvent.change(screen.getByLabelText("Type name"), { target: { value: "Team" } });
    fireEvent.change(screen.getByLabelText("Scope label"), { target: { value: "per team" } });
    fireEvent.click(screen.getByRole("button", { name: "Save type" }));
    expect(state.saveGoalType).toHaveBeenCalledWith({ id: undefined, input: { name: "Team", color: "#6366F1", scope_label: "per team" } }, expect.any(Object));
  });

  it("plans a future period with a unit", () => {
    render(<OperatingSystemSettingsTab />);
    expect(screen.getByText("Q3 2026")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "+ New period" }));
    fireEvent.change(screen.getByLabelText("Period unit"), { target: { value: "month" } });
    fireEvent.change(screen.getByLabelText("Starts on"), { target: { value: "2026-10-01" } });
    fireEvent.click(screen.getByRole("button", { name: "Save period" }));
    expect(state.savePeriod).toHaveBeenCalledWith({ input: { unit: "month", name: undefined, starts_on: "2026-10-01", ends_on: undefined } }, expect.any(Object));
  });

  it("deletes goal types and periods", () => {
    render(<OperatingSystemSettingsTab />);
    fireEvent.click(screen.getByRole("button", { name: "Delete Company" }));
    expect(state.deleteGoalType).toHaveBeenCalledWith("type-1");
    fireEvent.click(screen.getByRole("button", { name: "Delete Q3 2026" }));
    expect(state.deletePeriod).toHaveBeenCalledWith("period-1");
  });
});
