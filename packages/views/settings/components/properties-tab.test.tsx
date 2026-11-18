import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { IssueProperty } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { PropertiesTab } from "./properties-tab";

const OPTION_NAMES = ["Web", "Desktop", "Daemon", "CLI", "Mobile", "Server", "Billing", "Docs"];

const component: IssueProperty = {
  id: "prop-1",
  workspace_id: "workspace-1",
  name: "Component",
  type: "multi_select",
  description: "",
  icon: "",
  config: {
    options: OPTION_NAMES.map((name, i) => ({ id: `opt-${i}`, name, color: "#3b82f6" })),
  },
  archived: false,
  usage_count: 0,
  created_at: "2026-11-15T00:00:00Z",
  updated_at: "2026-11-15T00:00:00Z",
} as IssueProperty;

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "workspace-1", name: "Acme" }),
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (select: (s: { user: { id: string } }) => unknown) =>
    select({ user: { id: "user-1" } }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: (wsId: string) => ({ queryKey: ["members", wsId] }),
}));

vi.mock("@multica/core/properties", () => ({
  propertyListOptions: (wsId: string) => ({ queryKey: ["properties", wsId] }),
  useCreateProperty: () => ({ mutate: vi.fn(), isPending: false }),
  useUpdateProperty: () => ({ mutate: vi.fn(), isPending: false }),
}));

vi.mock("@tanstack/react-query", () => ({
  queryOptions: <T,>(options: T) => options,
  useQuery: (options: { queryKey: unknown[] }) => {
    if (options.queryKey[0] === "members") {
      return {
        data: [{ user_id: "user-1", role: "member", name: "Ada" }],
        isLoading: false,
      };
    }
    return { data: [component], isLoading: false };
  },
}));

describe("PropertiesTab options", () => {
  afterEach(() => cleanup());

  // The row shows only a few chips; the rest must stay reachable for members,
  // who have no edit menu to open the property.
  it("lets a member open every option from the keyboard", async () => {
    const user = userEvent.setup();
    renderWithI18n(<PropertiesTab />);

    expect(screen.queryByRole("button", { name: "Actions for Component" })).toBeNull();

    const trigger = screen.getByRole("button", { name: "Show all options for Component" });
    trigger.focus();
    await user.keyboard("{Enter}");

    const popup = await screen.findByRole("dialog");
    for (const name of OPTION_NAMES) {
      expect(within(popup).getByText(name)).toBeInTheDocument();
    }
  });
});
