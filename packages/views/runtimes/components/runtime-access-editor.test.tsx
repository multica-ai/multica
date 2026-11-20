import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@multica/core/api";
import type { AgentRuntime, MemberWithUser, WorkspaceFeatures } from "@multica/core/types";
import { toast } from "sonner";
import enBilling from "../../locales/en/billing.json";
import { renderWithI18n } from "../../test/i18n";
import { RuntimeAccessEditor } from "./runtime-access-editor";

const api = vi.hoisted(() => ({
  listMembers: vi.fn(),
  updateRuntime: vi.fn(),
  getWorkspaceFeatures: vi.fn(),
}));
vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api,
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => null }));
vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<object>()),
  useWorkspacePaths: () => ({ settings: () => "/acme/settings" }),
}));
vi.mock("../../navigation", async (importOriginal) => ({
  ...(await importOriginal<object>()),
  AppLink: ({ href, children }: { href: string; children: React.ReactNode }) => <a href={href}>{children}</a>,
}));

const MEMBERS = [
  { user_id: "owner", name: "Owner" },
  { user_id: "u1", name: "Alice" },
  { user_id: "u2", name: "Bob" },
] as unknown as MemberWithUser[];

function runtime(overrides: Partial<AgentRuntime> = {}): AgentRuntime {
  return {
    id: "runtime-1", workspace_id: "ws-1", name: "Local Runtime", custom_name: null,
    daemon_id: null, runtime_mode: "local", provider: "claude", launch_header: "", status: "online",
    device_info: "", metadata: {}, owner_id: "owner", visibility: "private", last_seen_at: null,
    created_at: "2026-09-05T00:00:00Z", updated_at: "2026-09-05T00:00:00Z",
    ...overrides,
  } as AgentRuntime;
}

function setFeature(state: "available" | "locked") {
  api.getWorkspaceFeatures.mockResolvedValue({ features: { runtime_member_sharing: state } } satisfies WorkspaceFeatures);
}

function renderEditor(value: AgentRuntime) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <RuntimeAccessEditor runtime={value} />
    </QueryClientProvider>,
  );
}

const membersButton = () => screen.getByRole("button", { name: "Members" });
const BADGE = enBilling.paid_feature.badge_label;

describe("RuntimeAccessEditor member sharing (MUL-9491)", () => {
  beforeEach(() => {
    api.listMembers.mockResolvedValue(MEMBERS);
    api.updateRuntime.mockResolvedValue(runtime());
    setFeature("available");
  });
  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("offers every member when the plan includes sharing", async () => {
    renderEditor(runtime());
    await waitFor(() => expect(membersButton()).toBeEnabled());
    fireEvent.click(membersButton());
    expect(await screen.findByRole("checkbox", { name: "Alice" })).toBeInTheDocument();
    expect(screen.getByRole("checkbox", { name: "Bob" })).toBeInTheDocument();
    expect(screen.queryByText("View plans")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: BADGE })).not.toBeInTheDocument();
  });

  it("disables Members for a runtime without a list on a locked plan", async () => {
    setFeature("locked");
    renderEditor(runtime({ visibility: "public" }));
    expect(await screen.findByText("Not included in this workspace's plan.")).toBeInTheDocument();
    expect(membersButton()).toBeDisabled();
    expect(screen.getByRole("button", { name: BADGE })).toBeEnabled();
    fireEvent.click(screen.getByRole("button", { name: "Private" }));
    await waitFor(() => expect(api.updateRuntime).toHaveBeenCalledWith("runtime-1", { visibility: "private", shared_with: [] }));
  });

  it("lets a locked plan remove members from an existing list but not add any", async () => {
    setFeature("locked");
    renderEditor(runtime({ shared_with: ["u1"] }));
    expect(await screen.findByText("Read-only. Upgrade to edit.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: BADGE })).toBeInTheDocument();
    fireEvent.click(membersButton());
    expect(await screen.findByRole("checkbox", { name: "Alice" })).toBeInTheDocument();
    expect(screen.queryByRole("checkbox", { name: "Bob" })).not.toBeInTheDocument();
  });

  it("explains a feature_locked refusal and refetches the plan", async () => {
    api.updateRuntime.mockRejectedValue(
      new ApiError("locked", 403, "Forbidden", { code: "feature_locked", feature: "runtime_member_sharing" }),
    );
    renderEditor(runtime());
    await waitFor(() => expect(membersButton()).toBeEnabled());
    fireEvent.click(membersButton());
    fireEvent.click(await screen.findByRole("checkbox", { name: "Bob" }));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith(enBilling.paid_feature.locked_error));
    await waitFor(() => expect(api.getWorkspaceFeatures).toHaveBeenCalledTimes(2));
  });
});
