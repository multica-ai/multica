import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { configStore } from "@multica/core/config";
import { COMPOSIO_MCP_APPS_FLAG } from "@multica/core/feature-flags";
import { I18nProvider } from "@multica/core/i18n/react";
import type { AppRequest, AppsOverview } from "@multica/core/types";
import { getTestResources } from "../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../navigation";
import { IssueAppRequests } from "./app-requests";

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/api", () => ({
  ApiError: class ApiError extends Error { status = 0; },
  api: {
    listAppRequests: vi.fn(), listAppCatalog: vi.fn(), getAppsOverview: vi.fn(),
    approveAppRequest: vi.fn(), rejectAppRequest: vi.fn(), connectApp: vi.fn(),
  },
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: (type: string) => (type === "agent" ? "Ops Assistant" : "Dev") }),
}));
const viewer = vi.hoisted(() => ({ id: "me" }));
vi.mock("@multica/core/auth", () => {
  const state = () => ({ user: viewer });
  return { useAuthStore: Object.assign((sel: (s: ReturnType<typeof state>) => unknown) => sel(state()), { getState: state }) };
});

let request: AppRequest;
let overview: AppsOverview;

beforeEach(() => {
  configStore.getState().setFeatureFlags({ [COMPOSIO_MCP_APPS_FLAG]: true });
  viewer.id = "me";
  request = {
    id: "req-1", kind: "connect", status: "pending", agent_id: "a1", issue_id: "i1", chat_session_id: null,
    approver_user_id: "me", account_scope: "personal", toolkit_slug: "googlecalendar", tool_slug: "",
    action_class: "read", summary: "I need your Google Calendar to find a free slot.", error: null, reason: null,
    redacted: false, adds_app: true, expires_at: "2026-11-24T12:06:00Z", decided_by: null, decided_at: null,
    created_at: "2026-11-17T12:00:00Z",
  };
  overview = { catalog_mode: "all", can_manage: false, team: [], personal: [], agent_apps: [] };
  vi.mocked(api.listAppRequests).mockImplementation(async () => ({ requests: [request], next_before: null }));
  vi.mocked(api.listAppCatalog).mockResolvedValue({
    catalog_mode: "all",
    apps: [{ slug: "googlecalendar", name: "Google Calendar", curated: true }],
  });
  vi.mocked(api.getAppsOverview).mockImplementation(async () => overview);
  vi.mocked(api.approveAppRequest).mockResolvedValue(null);
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

function renderCard() {
  const navigation: NavigationAdapter = {
    push: vi.fn(), replace: vi.fn(), back: vi.fn(),
    pathname: "/ws/issues/i1", searchParams: new URLSearchParams(), hash: "",
    getShareableUrl: (p) => p,
  };
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <I18nProvider locale="en" resources={getTestResources()}>
        <NavigationProvider value={navigation}>
          <IssueAppRequests issueId="i1" />
        </NavigationProvider>
      </I18nProvider>
    </QueryClientProvider>,
  );
}

describe("App request card for a new app", () => {
  it("asks to use the app by name and says answering adds it to the agent", async () => {
    renderCard();
    expect(await screen.findByText("Ops Assistant wants to use Google Calendar")).toBeVisible();
    expect(screen.getByText("New app for this agent")).toBeVisible();
    expect(screen.getByText("Answering adds Google Calendar to Ops Assistant for future runs.")).toBeVisible();
    expect(screen.getByRole("button", { name: "Decline" })).toBeVisible();
    expect(screen.queryByRole("button", { name: "I've connected it" })).not.toBeInTheDocument();
  });

  it("connects through the pre-connect notice when the approver has no account", async () => {
    const user = userEvent.setup();
    renderCard();
    await user.click(await screen.findByRole("button", { name: "Connect Google Calendar" }));
    expect(await screen.findByRole("dialog", { name: "Connect Google Calendar" })).toBeVisible();
    expect(api.connectApp).not.toHaveBeenCalled();
    expect(api.approveAppRequest).not.toHaveBeenCalled();
  });

  it("adds the app with the approver's existing account", async () => {
    overview.personal = [{ id: "c1", toolkit_slug: "googlecalendar", status: "active", connected_at: "", last_used_at: null }];
    const user = userEvent.setup();
    renderCard();
    await user.click(await screen.findByRole("button", { name: "Add Google Calendar to Ops Assistant" }));
    await waitFor(() => expect(api.approveAppRequest).toHaveBeenCalledWith("req-1", undefined));
  });

  it("shows others the request without the approver's actions", async () => {
    viewer.id = "someone-else";
    renderCard();
    expect(await screen.findByText("Ops Assistant wants to use Google Calendar")).toBeVisible();
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
    expect(api.getAppsOverview).not.toHaveBeenCalled();
  });
});

describe("App request card for an app the agent already has", () => {
  it("keeps the connect-then-confirm flow, with the app's display name", async () => {
    request.adds_app = false;
    const user = userEvent.setup();
    renderCard();
    expect(await screen.findByText("Ops Assistant asks you to connect Google Calendar")).toBeVisible();
    expect(screen.queryByText("New app for this agent")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "I've connected it" })).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Connect Google Calendar" }));
    expect(await screen.findByRole("dialog", { name: "Connect Google Calendar" })).toBeVisible();
    expect(api.connectApp).not.toHaveBeenCalled();
  });
});
