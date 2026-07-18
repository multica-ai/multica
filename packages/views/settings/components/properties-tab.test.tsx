// @vitest-environment jsdom

import { cleanup, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderWithI18n } from "../../test/i18n";
import { PropertiesTab } from "./properties-tab";

const state = vi.hoisted(() => ({ role: "admin" as "admin" | "member" }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (value: { user: { id: string } }) => unknown) =>
    selector({ user: { id: "user-1" } }),
}));
vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"] }),
}));
vi.mock("@multica/core/properties", () => ({
  propertyListOptions: () => ({ queryKey: ["properties"] }),
  useCreateProperty: () => ({ mutate: vi.fn(), isPending: false }),
  useUpdateProperty: () => ({ mutate: vi.fn(), isPending: false }),
}));
vi.mock("@tanstack/react-query", () => ({
  useQuery: (options: { queryKey: string[] }) =>
    options.queryKey[0] === "members"
      ? { data: [{ user_id: "user-1", role: state.role }], isLoading: false }
      : { data: [], isLoading: false },
}));

beforeEach(() => {
  state.role = "admin";
});

afterEach(cleanup);

describe("PropertiesTab", () => {
  it("lets workspace admins open the property definition editor", () => {
    renderWithI18n(<PropertiesTab />);

    expect(screen.getByRole("heading", { name: "Properties" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "New property" })).toBeInTheDocument();
  });

  it("keeps the catalog read-only for regular members", () => {
    state.role = "member";
    renderWithI18n(<PropertiesTab />);

    expect(screen.queryByRole("button", { name: "New property" })).not.toBeInTheDocument();
    expect(
      screen.getByText("Only workspace owners and admins can manage property definitions."),
    ).toBeInTheDocument();
  });
});
