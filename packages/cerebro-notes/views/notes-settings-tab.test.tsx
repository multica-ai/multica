// @vitest-environment jsdom

import "@testing-library/jest-dom/vitest";

import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { NotesSettingsTab } from "./notes-settings-tab";

afterEach(cleanup);

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));
vi.mock("@multica/core/permissions", () => ({
  useCurrentMember: () => ({ role: "owner" }),
}));
vi.mock("@multica/cerebro-feature-flags", () => ({
  useFeatureFlagsQuery: () => ({ isError: false, error: null }),
  // Render the label + description so the test sees exactly what the user reads.
  CerebroFlagRow: ({
    label,
    description,
  }: {
    label: string;
    description: string;
  }) => (
    <div>
      <span>{label}</span>
      <span>{description}</span>
    </div>
  ),
}));

describe("NotesSettingsTab", () => {
  it("labels the per-line attribution feature 'Line history', not 'Line authors' (FIR-3601)", () => {
    render(<NotesSettingsTab />);
    expect(screen.getByText("Line history")).toBeInTheDocument();
    expect(screen.queryByText("Line authors")).not.toBeInTheDocument();
  });

  it("describes the toggle by its current 'Line history' menu name", () => {
    render(<NotesSettingsTab />);
    expect(
      screen.getByText(/the 'Line history' toggle in a note's ⋯ menu/, {
        selector: "span",
      }),
    ).toBeInTheDocument();
  });
});
