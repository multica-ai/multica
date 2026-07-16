import "@testing-library/jest-dom/vitest";
import { fireEvent, render, renderHook, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { OperatingSystemSettingsTab, useCerebroOperatingSystemSettingsTabs } from "./settings-tab";

const state = vi.hoisted(() => ({ enabled: true, mutate: vi.fn() }));
vi.mock("@multica/cerebro-feature-flags", () => ({ useFeatureFlag: () => state.enabled }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));
vi.mock("@tanstack/react-query", async (importOriginal) => ({ ...(await importOriginal<typeof import("@tanstack/react-query")>()), useQuery: () => ({ data: { terminology: { strategy: "Strategy", rock: "Rock", rocks: "Rocks" } } }) }));
vi.mock("../core/queries", async (importOriginal) => ({ ...(await importOriginal<typeof import("../core/queries")>()), useUpdateSettings: () => ({ mutate: state.mutate, isPending: false }) }));

describe("OperatingSystemSettingsTab", () => {
  beforeEach(() => { state.enabled = true; state.mutate.mockReset(); });
  it("registers only while the Operating System is enabled", () => {
    expect(renderHook(() => useCerebroOperatingSystemSettingsTabs()).result.current[0]?.label).toBe("Operating System");
    state.enabled = false;
    expect(renderHook(() => useCerebroOperatingSystemSettingsTabs()).result.current).toEqual([]);
  });
  it("keeps profile and terminology changes in Settings", () => {
    render(<OperatingSystemSettingsTab />);
    expect(screen.getByRole("heading", { name: "Operating System" })).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Profile"), { target: { value: "custom" } });
    fireEvent.change(screen.getByLabelText("Strategy label"), { target: { value: "Direction" } });
    fireEvent.click(screen.getByRole("button", { name: "Save terminology" }));
    expect(state.mutate).toHaveBeenCalledWith({ strategy: "Direction", rock: "Rock", rocks: "Rocks" }, expect.any(Object));
  });
});
