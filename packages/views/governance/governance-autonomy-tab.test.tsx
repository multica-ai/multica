/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderWithI18n } from "../test/i18n";
import { GovernanceAutonomyTab } from "./governance-autonomy-tab";

const api = vi.hoisted(() => ({
  listSquads: vi.fn(),
  getSquadAutonomy: vi.fn(),
  setSquadAutonomy: vi.fn(),
  listApprovalPolicies: vi.fn(),
  createApprovalPolicy: vi.fn(),
  revokeApprovalPolicy: vi.fn(),
  listPolicyAutoApprovals: vi.fn(),
}));
const role = vi.hoisted(() => ({ value: "admin" as "owner" | "admin" | "member" }));

vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api,
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/permissions", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/permissions")>()),
  useCurrentMember: () => ({ userId: "u-1", role: role.value, member: null, isLoading: false }),
}));

const squad = (id: string, name: string, level: number) => ({
  id,
  workspace_id: "ws-1",
  name,
  description: "",
  instructions: "",
  avatar_url: null,
  leader_id: "a-1",
  creator_id: "u-1",
  created_at: "2026-11-01T10:00:00Z",
  updated_at: "2026-11-01T10:00:00Z",
  archived_at: null,
  archived_by: null,
  autonomy_level: level,
});

const policy = (extra: Record<string, unknown> = {}) => ({
  id: "p-1",
  name: "Routine notes",
  kind: "decision",
  match: { squad_ids: ["sq-1"] },
  max_per_week: 3,
  valid_until: null,
  active: true,
  created_by: "u-1",
  created_at: "2026-11-01T10:00:00Z",
  week_approvals: 1,
  flagged: false,
  ...extra,
});

function renderTab() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  renderWithI18n(
    <QueryClientProvider client={queryClient}>
      <GovernanceAutonomyTab />
    </QueryClientProvider>,
  );
}

describe("GovernanceAutonomyTab", () => {
  beforeEach(() => {
    role.value = "admin";
    for (const fn of Object.values(api)) fn.mockReset();
    api.listSquads.mockResolvedValue([squad("sq-1", "Research squad", 1), squad("sq-2", "Build squad", 2)]);
    api.getSquadAutonomy.mockImplementation(async (id: string) =>
      id === "sq-2"
        ? { squad_id: "sq-2", level: 2, level_name: "Trusted", reason: "Two clean weeks.", changed_at: "2026-11-02T10:00:00Z" }
        : { squad_id: id, level: 1, level_name: "Work", reason: "", changed_at: null },
    );
    api.listApprovalPolicies.mockResolvedValue([policy()]);
    api.listPolicyAutoApprovals.mockResolvedValue({ auto_approvals: [], flagged: false });
  });
  afterEach(cleanup);

  it("keeps the honest-limit note that stops L3 reading as unsupervised, and explains each level", async () => {
    renderTab();
    expect(
      await screen.findByText(/The honest limit: Multica cannot inspect what a runtime does/),
    ).toBeTruthy();
    expect(screen.getByText("L2 Trusted")).toBeTruthy();
    expect(screen.getByText("L0 Suggest")).toBeTruthy();
  });

  it("shows each squad's level with the reason of its last change", async () => {
    renderTab();
    const levels = await screen.findByRole("region", { name: "Squad levels" });
    const build = await within(levels).findByText("Build squad");
    const row = build.closest("li") as HTMLElement;
    await waitFor(() => expect(within(row).getByText("Two clean weeks.", { exact: false })).toBeTruthy());
    expect(within(row).getAllByText(/L2/).length).toBeGreaterThan(0);
  });

  it("lets a boss change a level only with a reason", async () => {
    api.setSquadAutonomy.mockResolvedValue({
      squad_id: "sq-1", level: 2, level_name: "Trusted", reason: "Reviewed drafts.", changed_at: "2026-11-03T10:00:00Z",
    });
    renderTab();
    const levels = await screen.findByRole("region", { name: "Squad levels" });
    const research = (await within(levels).findByText("Research squad")).closest("li") as HTMLElement;
    const save = within(research).getByRole("button", { name: "Save level" });
    expect((save as HTMLButtonElement).disabled).toBe(true);

    fireEvent.change(within(research).getByRole("textbox"), { target: { value: "Reviewed drafts." } });
    expect((save as HTMLButtonElement).disabled).toBe(true); // still the same level
  });

  it("shows members the levels without any change control", async () => {
    role.value = "member";
    renderTab();
    expect(await screen.findByText(/Only owners and admins can change levels/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Save level" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Save level" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Create standing approval" })).toBeNull();
  });

  it("lists standing approvals with their weekly count and reversal flag", async () => {
    api.listApprovalPolicies.mockResolvedValue([policy({ flagged: true, week_approvals: 2 })]);
    renderTab();
    expect(await screen.findByText("Routine notes")).toBeTruthy();
    expect(screen.getByText("2 of 3 approvals this week", { exact: false })).toBeTruthy();
    expect(screen.getByText("A review reversed one of its approvals")).toBeTruthy();
  });

  it("revokes a standing approval in one click", async () => {
    api.revokeApprovalPolicy.mockResolvedValue(policy({ active: false }));
    renderTab();
    const row = (await screen.findByText("Routine notes")).closest("li") as HTMLElement;
    fireEvent.click(within(row).getByRole("button", { name: "Revoke" }));
    await waitFor(() => expect(api.revokeApprovalPolicy).toHaveBeenCalledWith("p-1"));
  });

  it("shows a policy's history with the reversal note", async () => {
    api.listPolicyAutoApprovals.mockResolvedValue({
      flagged: true,
      auto_approvals: [
        {
          proposal_id: "pr-1", squad_id: "sq-1", kind: "decision", state: "approved", title: "Draft note",
          identifier: "MUL-42", decided_at: "2026-11-02T10:00:00Z", reversed_at: "2026-11-03T09:00:00Z",
          reversal_note: "Not needed after all.", flagged: true,
        },
      ],
    });
    renderTab();
    const row = (await screen.findByText("Routine notes")).closest("li") as HTMLElement;
    fireEvent.click(within(row).getByRole("button", { name: "History" }));
    expect(await within(row).findByText("MUL-42 Draft note")).toBeTruthy();
    expect(within(row).getByText("Reversed on review: Not needed after all.")).toBeTruthy();
  });

  it("hides the create form and revoke for members", async () => {
    role.value = "member";
    renderTab();
    const row = (await screen.findByText("Routine notes")).closest("li") as HTMLElement;
    expect(within(row).queryByRole("button", { name: "Revoke" })).toBeNull();
  });
});
