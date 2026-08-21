import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen } from "@testing-library/react";
import { renderWithI18n } from "../../test/i18n";
import { LabelsTab } from "./labels-tab";

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: [], isLoading: false }),
}));

vi.mock("@multica/core/labels", () => ({
  labelListOptions: (wsId: string, resourceType: string) => ({
    queryKey: ["labels", wsId, "list", resourceType],
  }),
  useCreateLabel: () => ({ mutate: vi.fn(), isPending: false }),
  useUpdateLabel: () => ({ mutate: vi.fn(), isPending: false }),
  useDeleteLabel: () => ({ mutate: vi.fn(), isPending: false }),
}));

describe("LabelsTab scopes", () => {
  afterEach(cleanup);

  // Agent labels were removed from the product (MUL-5600). The backend still
  // models the `agent` resource type, so the guard here is that the settings
  // UI never offers it as a manageable catalog again.
  it("offers only the issue and skill catalogs", () => {
    renderWithI18n(<LabelsTab />);

    expect(screen.getByRole("button", { name: /Issues/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Skills/ })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Agents/ })).toBeNull();
  });

  it("describes the tab without promising an agent catalog", () => {
    renderWithI18n(<LabelsTab />);

    expect(screen.getByText(/organize issues and skills/i)).toBeInTheDocument();
  });
});
