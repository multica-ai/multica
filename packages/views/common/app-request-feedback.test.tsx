import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { toast } from "sonner";
import { ApiError, api } from "@multica/core/api";
import { configStore } from "@multica/core/config";
import { COMPOSIO_MCP_APPS_FLAG } from "@multica/core/feature-flags";
import { I18nProvider } from "@multica/core/i18n/react";
import { connectedAppsKeys } from "@multica/core/connected-apps";
import type { AppRequest } from "@multica/core/types";
import { getTestResources } from "../test/i18n";
import en from "../locales/en/settings.json";
import { NavigationProvider, type NavigationAdapter } from "../navigation";
import { IssueAppRequests } from "./app-requests";

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<object>()),
  api: {
    listAppRequests: vi.fn(), listAppCatalog: vi.fn(), getAppsOverview: vi.fn(),
    approveAppRequest: vi.fn(), rejectAppRequest: vi.fn(),
  },
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: () => "Reviewer" }),
}));
vi.mock("@multica/core/auth", () => {
  const state = { user: { id: "me" } };
  return { useAuthStore: Object.assign((select: (value: typeof state) => unknown) => select(state), { getState: () => state }) };
});

let request: AppRequest;
const clients: QueryClient[] = [];
const copy = en.connected_apps;

beforeEach(() => {
  vi.clearAllMocks();
  configStore.getState().setFeatureFlags({ [COMPOSIO_MCP_APPS_FLAG]: true });
  request = {
    id: "request", kind: "action", status: "pending", agent_id: "agent", issue_id: "issue", chat_session_id: null,
    approver_user_id: "me", account_scope: "personal", toolkit_slug: "calendar", tool_slug: "create_event",
    action_class: "write", summary: "Create the requested calendar event.", error: null, reason: null,
    redacted: false, expires_at: "2027-01-01T12:00:00Z", decided_by: null, decided_at: null,
    created_at: "2026-11-27T00:00:00Z",
  };
  vi.mocked(api.listAppRequests).mockImplementation(async () => ({ requests: [request], next_before: null }));
  vi.mocked(api.listAppCatalog).mockResolvedValue({ catalog_mode: "all", apps: [] });
});

afterEach(() => {
  cleanup();
  for (const client of clients.splice(0)) client.clear();
  configStore.getState().setFeatureFlags({});
});

function renderRequest() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  clients.push(client);
  const navigation: NavigationAdapter = {
    push: vi.fn(), replace: vi.fn(), back: vi.fn(), pathname: "/ws/issues/ISS-1",
    searchParams: new URLSearchParams(), hash: "", getShareableUrl: (path) => path,
  };
  const view = render(<QueryClientProvider client={client}>
    <I18nProvider locale="en" resources={getTestResources()}>
      <NavigationProvider value={navigation}><IssueAppRequests issueId="issue" /></NavigationProvider>
    </I18nProvider>
  </QueryClientProvider>);
  return { ...view, client };
}

function pendingDecision() {
  let resolve!: (value: AppRequest | null) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<AppRequest | null>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

const actions = [
  { action: "approve", success: copy.request.toast_approved, error: copy.request.rate_limited },
  { action: "reject", success: copy.request.toast_rejected, error: copy.request.toast_failed },
  { action: "withdraw", success: copy.account_row.toast_withdrawn, error: copy.request.toast_failed },
] as const;

async function submit(action: typeof actions[number]["action"]) {
  const user = userEvent.setup();
  const label = action === "approve" ? copy.request.approve : action === "withdraw" ? copy.account_row.withdraw : copy.request.reject;
  await user.click(await screen.findByRole("button", { name: label }));
  if (action === "reject") await user.click(screen.getByRole("button", { name: label }));
  await waitFor(() => expect(action === "approve" ? api.approveAppRequest : api.rejectAppRequest).toHaveBeenCalledTimes(1));
}

function prepare(action: typeof actions[number]["action"]) {
  if (action === "withdraw") request = { ...request, kind: "connect", account_scope: "team", can_decide: false };
  const decision = pendingDecision();
  vi.mocked(api.approveAppRequest).mockReturnValue(decision.promise);
  vi.mocked(api.rejectAppRequest).mockReturnValue(decision.promise);
  return decision;
}

describe("app request feedback after leaving the request", () => {
  it.each(actions)("reports $action success after navigation unmounts the card", async ({ action, success }) => {
    const decision = prepare(action);
    const view = renderRequest();
    await submit(action);
    view.unmount();
    expect(view.client.isMutating()).toBe(1);
    await act(async () => decision.resolve(null));
    expect(toast.success).toHaveBeenCalledExactlyOnceWith(success);
    expect(toast.error).not.toHaveBeenCalled();
  });

  it.each(actions)("reports $action failure after navigation unmounts the card", async ({ action, error }) => {
    const decision = prepare(action);
    const view = renderRequest();
    await submit(action);
    view.unmount();
    await act(async () => decision.reject(action === "approve" ? new ApiError("Rate limited", 429, "Too Many Requests") : new Error("Request failed")));
    expect(toast.error).toHaveBeenCalledExactlyOnceWith(error);
    expect(toast.success).not.toHaveBeenCalled();
  });

  it("reports approval when a realtime refresh removes the card before the response arrives", async () => {
    const decision = prepare("approve");
    const view = renderRequest();
    await submit("approve");
    request = { ...request, status: "approved" };
    act(() => view.client.setQueryData(connectedAppsKeys.issueRequests("ws", "issue"), [request]));
    await waitFor(() => expect(screen.queryByRole("button", { name: copy.request.approve })).not.toBeInTheDocument());
    await act(async () => decision.resolve(request));
    expect(toast.success).toHaveBeenCalledExactlyOnceWith(copy.request.toast_approved);
  });
});
