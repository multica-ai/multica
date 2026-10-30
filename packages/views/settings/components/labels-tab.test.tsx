import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithI18n } from "../../test/i18n";
import { LabelsTab } from "./labels-tab";
import { LabelManager } from "../../labels/label-manager";

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

const queried = vi.hoisted(() => ({ keys: [] as unknown[][] }));
vi.mock("@tanstack/react-query", () => ({
  queryOptions: <T,>(options: T) => options,
  useQuery: (options: { queryKey: unknown[] }) => {
    queried.keys.push(options.queryKey);
    return { data: [], isLoading: false };
  },
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "workspace-1", name: "Acme" }),
}));

vi.mock("@multica/core/labels", () => ({
  labelListOptions: (wsId: string, resourceType: string) => ({
    queryKey: ["labels", wsId, "list", resourceType],
  }),
  useCreateLabel: () => ({ mutate: vi.fn(), isPending: false }),
  useUpdateLabel: () => ({ mutate: vi.fn(), isPending: false }),
  useDeleteLabel: () => ({ mutate: vi.fn(), isPending: false }),
}));

describe("Label catalogs", () => {
  afterEach(() => {
    cleanup();
    queried.keys = [];
  });

  // Agent labels were removed from the product (MUL-5600) even though the
  // backend still models the `agent` resource type.
  it("switches between the issue and skill catalogs, never agent labels", async () => {
    const user = userEvent.setup();
    renderWithI18n(<LabelsTab />);

    const issues = screen.getByRole("tab", { name: /Issues/ });
    expect(issues).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("tab", { name: /Skills/ })).toHaveAttribute("aria-selected", "false");
    expect(screen.queryByRole("tab", { name: /Agents/ })).toBeNull();

    queried.keys = [];
    await user.click(screen.getByRole("tab", { name: /Skills/ }));

    expect(screen.getByRole("tab", { name: /Skills/ })).toHaveAttribute("aria-selected", "true");
    expect(queried.keys).toContainEqual(["labels", "workspace-1", "list", "skill"]);
    expect(queried.keys.some((key) => key.includes("agent"))).toBe(false);
  });

  it("manages the skill catalog where it is mounted for skills", () => {
    renderWithI18n(<LabelManager scope="skill" />);

    expect(queried.keys).toContainEqual(["labels", "workspace-1", "list", "skill"]);
    expect(screen.getByRole("button", { name: /New label/ })).toBeInTheDocument();
  });
});
