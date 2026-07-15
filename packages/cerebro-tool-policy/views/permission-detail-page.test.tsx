import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const mockGetPermissionHolders = vi.hoisted(() => vi.fn());
const mockGetPermissionChanges = vi.hoisted(() => vi.fn());
const mockGetPermissionUsage = vi.hoisted(() => vi.fn());
const mockFetchToolPolicyTable = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/cerebro-feature-flags", () => ({
  useFeatureFlag: () => true,
}));

vi.mock("../api", () => ({
  getPermissionHolders: mockGetPermissionHolders,
  getPermissionChanges: mockGetPermissionChanges,
  getPermissionUsage: mockGetPermissionUsage,
}));

vi.mock("../core", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../core")>();
  return { ...actual, fetchToolPolicyTable: mockFetchToolPolicyTable };
});

vi.mock("./use-holder-directory", () => ({
  useHolderDirectory: () => ({
    labelFor: (_layer: string, id: string) => id,
    isLoading: false,
    members: [
      {
        id: "user-jesper",
        name: "Jesper Hvejsel",
        email: "jesper@example.com",
        role: "admin",
      },
      {
        id: "user-sara",
        name: "Sara",
        email: "sara@example.com",
        role: "member",
      },
    ],
    agents: [
      {
        id: "agent-lone",
        name: "Lone",
        ownerId: "user-jesper",
        runtimeId: "runtime-cloud",
      },
    ],
    runtimes: [
      {
        id: "runtime-cloud",
        name: "Codex Cloud",
        ownerId: "user-jesper",
      },
    ],
    groups: [
      {
        id: "group-leadership",
        name: "Leadership",
        userIds: ["user-jesper"],
      },
    ],
  }),
}));

import { PermissionDetailPage } from "./permission-detail-page";

function renderPage() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <PermissionDetailPage toolKey="tools:test-as-user" onBack={() => {}} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  mockGetPermissionHolders.mockReset();
  mockGetPermissionHolders.mockResolvedValue({
    tool_key: "tools:test-as-user",
    enforced: true,
    holders: [
      { layer: "workspace", subject_id: "ws-1", setting: "deny" },
      { layer: "runtime", subject_id: "runtime-cloud", setting: "ask" },
      { layer: "agent", subject_id: "agent-lone", setting: "allow" },
      { layer: "group", subject_id: "group-leadership", setting: "deny" },
      { layer: "user", subject_id: "user-jesper", setting: "allow" },
    ],
  });
  mockGetPermissionChanges.mockReset();
  mockGetPermissionChanges.mockResolvedValue({
    tool_key: "tools:test-as-user",
    changes: [],
  });
  mockGetPermissionUsage.mockReset();
  mockGetPermissionUsage.mockResolvedValue({
    tool_key: "tools:test-as-user",
    usage: [],
  });
  mockFetchToolPolicyTable.mockReset();
  mockFetchToolPolicyTable.mockImplementation(
    async (_wsId: string, ctx: { userId?: string }) => ({
      tools: ctx,
    }),
  );
  mockFetchToolPolicyTable.mockResolvedValue([
    {
      tool_key: "tools:test-as-user",
      effective: {
        setting: "deny",
        decided_by: "workspace",
        capped_by: "",
      },
    },
  ]);
});

describe("PermissionDetailPage permission audit", () => {
  it("renders one desktop row per user with every permission layer", async () => {
    renderPage();

    expect(
      await screen.findByRole("heading", { name: "Permission audit" }),
    ).toBeInTheDocument();
    const table = await screen.findByRole("table", { name: "Permission audit" });
    for (const heading of [
      "User",
      "Workspace",
      "Runtimes",
      "Agents",
      "Groups",
      "Direct",
      "Effective",
    ]) {
      expect(within(table).getByRole("columnheader", { name: heading })).toBeInTheDocument();
    }
    for (const label of [
      "Jesper Hvejsel",
      "Sara",
      "Codex Cloud",
      "Lone",
      "Leadership",
    ]) {
      expect(within(table).getAllByText(label).length).toBeGreaterThan(0);
    }
  });

  it("renders the same layers as readable mobile user cards", async () => {
    renderPage();

    const mobile = await screen.findByTestId("permission-audit-mobile");
    expect(within(mobile).getAllByText("Jesper Hvejsel").length).toBeGreaterThan(0);
    for (const label of [
      "Workspace",
      "Runtimes",
      "Agents",
      "Groups",
      "Direct",
      "Effective",
    ]) {
      expect(within(mobile).getAllByText(label).length).toBeGreaterThan(0);
    }
  });

  it("filters users by decision without hiding the audit structure", async () => {
    const user = userEvent.setup();
    renderPage();

    const table = await screen.findByRole("table", { name: "Permission audit" });
    await user.click(screen.getByRole("button", { name: "Ask" }));

    expect(within(table).getAllByText("Jesper Hvejsel").length).toBeGreaterThan(0);
    expect(within(table).queryByText("Sara")).not.toBeInTheDocument();
    expect(
      within(table).getByRole("columnheader", { name: "Effective" }),
    ).toBeInTheDocument();
  });

  it("collapses mobile user detail by default and keeps the decision visible", async () => {
    renderPage();

    const card = await screen.findByTestId(
      "permission-audit-mobile-user-jesper",
    );
    expect(card).not.toHaveAttribute("open");
    expect(within(card).getByText("Effective Deny")).toBeInTheDocument();
  });
});
