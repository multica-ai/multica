// @vitest-environment jsdom

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@multica/core/api";
import type { AgentRuntime, MemberWithUser } from "@multica/core/types";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { renderWithI18n } from "../../test/i18n";
import { RuntimeList } from "./runtime-list";

const api = vi.hoisted(() => ({
  listMembers: vi.fn(),
  listAgents: vi.fn(),
  updateRuntime: vi.fn(),
  getWorkspaceFeatures: vi.fn(),
  revokeRuntimeWorkspaceAccess: vi.fn(),
}));
vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api,
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
const authUser = vi.hoisted(() => ({ id: "owner" }));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (sel: (s: { user: { id: string } }) => unknown) => sel({ user: authUser }),
}));
vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<object>()),
  useWorkspacePaths: () => ({
    runtimeDetail: (id: string) => `/acme/runtimes/${id}`,
    settings: () => "/acme/settings",
  }),
}));
// Cells that fetch their own data are out of scope here.
vi.mock("./rate-limit-section", () => ({ RateLimitCell: () => null }));
vi.mock("./provider-logo", () => ({ ProviderLogo: () => null }));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => null }));
vi.mock("../../common/use-viewing-timezone", () => ({ useViewingTimezone: () => "UTC" }));
vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }));

const MEMBERS = [
  { user_id: "owner", name: "Owner", role: "member" },
  { user_id: "admin", name: "Admin", role: "admin" },
  { user_id: "u1", name: "Alice", role: "member" },
] as unknown as MemberWithUser[];

function runtime(overrides: Partial<AgentRuntime> = {}): AgentRuntime {
  return {
    id: "rt-1", workspace_id: "ws-1", name: "Claude", custom_name: null,
    daemon_id: "d-1", runtime_mode: "local", provider: "claude", launch_header: "", status: "online",
    device_info: "", metadata: {}, owner_id: "owner", visibility: "private", last_seen_at: null,
    created_at: "2026-09-05T00:00:00Z", updated_at: "2026-09-05T00:00:00Z",
    ...overrides,
  } as AgentRuntime;
}

const navigation: NavigationAdapter = {
  push: vi.fn(),
  replace: vi.fn(),
  back: vi.fn(),
  pathname: "/acme/runtimes",
  searchParams: new URLSearchParams(),
  hash: "",
  getShareableUrl: (path) => path,
};

function renderList(runtimes: AgentRuntime[]) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return renderWithI18n(
    <NavigationProvider value={navigation}>
      <QueryClientProvider client={qc}>
        <RuntimeList runtimes={runtimes} now={Date.now()} />
      </QueryClientProvider>
    </NavigationProvider>,
  );
}

const rowOf = (name: string) =>
  screen.getAllByRole("row").find((row) => within(row).queryByText(name))!;
const trigger = (label: string) => screen.getByRole("button", { name: `Visibility: ${label}` });

describe("RuntimeList visibility (MUL-10200)", () => {
  beforeEach(() => {
    authUser.id = "owner";
    api.listMembers.mockResolvedValue(MEMBERS);
    api.listAgents.mockResolvedValue([]);
    api.updateRuntime.mockResolvedValue(runtime({ visibility: "public" }));
    api.getWorkspaceFeatures.mockResolvedValue({ features: { runtime_member_sharing: "available" } });
  });
  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("marks every scope, private included, with the member count for shared runtimes", () => {
    authUser.id = "u1";
    renderList([
      runtime({ id: "a", name: "Alpha" }),
      runtime({ id: "b", name: "Beta", visibility: "public" }),
      runtime({ id: "c", name: "Gamma", shared_with: ["u1", "admin"] }),
    ]);
    expect(within(rowOf("Alpha")).getByRole("img", { name: "Visibility: Private" })).toBeInTheDocument();
    expect(within(rowOf("Beta")).getByRole("img", { name: "Visibility: Public" })).toBeInTheDocument();
    expect(within(rowOf("Gamma")).getByRole("img", { name: "Visibility: 2 members" })).toHaveTextContent(/^2$/);
  });

  it("lets the owner switch scope from the menu without opening the row", async () => {
    renderList([runtime()]);
    fireEvent.click(trigger("Private"));
    fireEvent.click(await screen.findByRole("menuitem", { name: "Public" }));
    await waitFor(() =>
      expect(api.updateRuntime).toHaveBeenCalledWith("rt-1", { visibility: "public", shared_with: [] }),
    );
    expect(navigation.push).not.toHaveBeenCalled();
  });

  it("shares with members through the picker, saving only on Save", async () => {
    renderList([runtime()]);
    fireEvent.click(trigger("Private"));
    const members = await screen.findByRole("menuitem", { name: "Specific members…" });
    // Enabled once the plan says member sharing is available.
    await waitFor(() => expect(members).not.toHaveAttribute("aria-disabled", "true"));
    fireEvent.click(members);
    fireEvent.click(await screen.findByRole("checkbox", { name: "Alice" }));
    expect(api.updateRuntime).not.toHaveBeenCalled();
    // The owner is never offered to themselves.
    expect(screen.queryByRole("checkbox", { name: "Owner" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(api.updateRuntime).toHaveBeenCalledWith("rt-1", { visibility: "private", shared_with: ["u1"] }),
    );
    expect(navigation.push).not.toHaveBeenCalled();
  });

  it("is read-only for anyone but the owner, admins included", () => {
    authUser.id = "admin";
    renderList([runtime({ visibility: "public" })]);
    expect(screen.queryByRole("button", { name: /^Visibility:/ })).toBeNull();
    fireEvent.click(within(rowOf("Claude")).getByRole("img", { name: "Visibility: Public" }));
    expect(navigation.push).toHaveBeenCalledWith("/acme/runtimes/rt-1");
  });

  it("confirms before revoking access from the list", async () => {
    api.updateRuntime.mockRejectedValue(
      new ApiError("conflict", 409, "Conflict", {
        code: "runtime_has_nonowner_dependents",
        nonowner_agents: [{ id: "00000000-0000-4000-8000-000000000001", name: "Teammate Agent" }],
        active_task_ids: [],
      }),
    );
    renderList([runtime({ visibility: "public" })]);
    fireEvent.click(trigger("Public"));
    fireEvent.click(await screen.findByRole("menuitem", { name: "Private" }));

    const dialog = await screen.findByRole("alertdialog");
    expect(within(dialog).getByText("Teammate Agent")).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
    expect(navigation.push).not.toHaveBeenCalled();
  });

  it("disables member sharing for a runtime without a list on a locked plan", async () => {
    api.getWorkspaceFeatures.mockResolvedValue({ features: { runtime_member_sharing: "locked" } });
    renderList([runtime()]);
    fireEvent.click(trigger("Private"));
    await waitFor(() =>
      expect(screen.getByRole("menuitem", { name: "Specific members…" })).toHaveAttribute("aria-disabled", "true"),
    );
  });
});
