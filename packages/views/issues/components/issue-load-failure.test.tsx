import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, renderHook, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createAuthStore, registerAuthStore } from "@multica/core/auth";
import { createChatStore, registerChatStore } from "@multica/core/chat";
import { WSProvider } from "@multica/core/realtime";
import { defaultStorage } from "@multica/core/platform";
import { TooltipProvider } from "@multica/ui/components/ui/tooltip";
import { ApiClient, setApiInstance } from "@multica/core/api";
import { useCanonicalIssue } from "@multica/core/issues/canonical-id";
import { issueKeys } from "@multica/core/issues/queries";
import { I18nProvider } from "@multica/core/i18n/react";
import { NavigationProvider } from "../../navigation";
import enIssues from "../../locales/en/issues.json";
import enCommon from "../../locales/en/common.json";
import { IssueDetail } from "./issue-detail";
import { IssueDetailRoute } from "./issue-detail-route";

const scope = vi.hoisted(() => ({ wsId: "ws-synthetic" }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => scope.wsId }));
vi.mock("@multica/core/paths", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/paths")>("@multica/core/paths");
  return { ...actual, useCurrentWorkspace: () => ({ id: scope.wsId, slug: "synthetic", name: "Synthetic" }), useWorkspacePaths: () => actual.paths.workspace("synthetic") };
});

const uuid = "11111111-1111-4111-8111-111111111111";
const issue = {
  id: uuid, workspace_id: "ws-synthetic", number: 1, identifier: "SYN-1",
  title: "Synthetic issue", description: "Synthetic description", status: "todo", priority: "none",
  assignee_type: null, assignee_id: null, creator_type: "member", creator_id: "synthetic-user",
  parent_issue_id: null, project_id: null, position: 0, stage: null, start_date: null, due_date: null,
  metadata: {}, properties: {}, created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z",
};
const response = (status: number, body: unknown = { error: "SYNTHETIC_PRIVATE_ERROR" }) => new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
let auth: ReturnType<typeof createAuthStore>;
let qc: QueryClient;
let primary: ReturnType<typeof vi.fn<(url: string) => Promise<Response>>>;
const replace = vi.fn();
const back = vi.fn();
function surface(routeId: string, host: "route" | "inbox" | "peek" = "route") {
  return <I18nProvider resources={{ en: { issues: enIssues, common: enCommon } }} locale="en">
    <QueryClientProvider client={qc}>
      <WSProvider wsUrl="ws://synthetic.invalid" authStore={auth} storage={defaultStorage}><TooltipProvider><NavigationProvider value={{ push: vi.fn(), replace, back, pathname: `/synthetic/issues/${routeId}`, searchParams: new URLSearchParams(), hash: "#comment-synthetic", getShareableUrl: (p) => p }}>
        {host === "route" ? <IssueDetailRoute routeId={routeId} /> : <IssueDetail issueId={routeId} variant={host === "peek" ? "peek" : "page"} leadingAction={<button onClick={back}>Inbox back</button>} trailingActions={<button onClick={back}>Close peek</button>} />}
      </NavigationProvider></TooltipProvider></WSProvider>
    </QueryClientProvider>
  </I18nProvider>;
}
beforeEach(() => {
  scope.wsId = "ws-synthetic";
  qc = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  primary = vi.fn<(url: string) => Promise<Response>>().mockImplementation(() => Promise.resolve(response(503)));
  replace.mockClear(); back.mockClear();
  // Only the HTTP boundary is synthetic. Queries, schemas, resolver and detail components are real.
  vi.stubGlobal("fetch", vi.fn((url: string) => {
    const pathname = new URL(url).pathname;
    if (["SYN-1", uuid, "OTHER-2"].some((id) => pathname === `/api/issues/${id}`)) {
      return primary(url);
    }
    const body = /\/(members|agents|squads|assignee-frequency)$/.test(pathname)
      ? []
      : { issues: [], total: 0, members: [], agents: [], comments: [], tasks: [], entries: [], has_more: false };
    return Promise.resolve(response(200, body));
  }));
  const api = new ApiClient("https://synthetic.invalid");
  setApiInstance(api);
  auth = createAuthStore({ api, storage: defaultStorage });
  registerAuthStore(auth);
  registerChatStore(createChatStore({ storage: defaultStorage }));
});
afterEach(() => { cleanup(); qc.clear(); vi.unstubAllGlobals(); });

describe("primary issue read failures through the production API client", () => {
  it.each(["SYN-1", uuid])("shows a safe load failure for 503 at %s without looping", async (id) => {
    const view = render(surface(id));
    await screen.findByText("Could not load issue.");
    expect(screen.queryByText(enIssues.detail.not_found_title)).not.toBeInTheDocument();
    expect(document.body).not.toHaveTextContent("SYNTHETIC_PRIVATE_ERROR");
    view.rerender(surface(id));
    await act(async () => { await new Promise((r) => setTimeout(r, 80)); });
    expect(primary).toHaveBeenCalledTimes(1);
    expect(replace).not.toHaveBeenCalled();
  });
  it("shows a network failure without displaying the exception", async () => {
    primary.mockRejectedValue(new TypeError("SYNTHETIC_PRIVATE_ERROR"));
    render(surface("SYN-1"));
    await screen.findByText("Could not load issue.");
    expect(document.body).not.toHaveTextContent("SYNTHETIC_PRIVATE_ERROR");
  });
  it.each([["SYN-1", 403], ["SYN-1", 404], [uuid, 403], [uuid, 404]] as const)("retains the opaque unavailable view for %s HTTP %s", async (id, status) => {
    primary.mockImplementation(() => Promise.resolve(response(status)));
    render(surface(id));
    await screen.findByText(enIssues.detail.not_found_title);
    expect(screen.queryByRole("button", { name: "Retry" })).not.toBeInTheDocument();
    expect(document.body).not.toHaveTextContent("SYNTHETIC_PRIVATE_ERROR");
    expect(primary).toHaveBeenCalledTimes(1);
  });
  it.each(["inbox", "peek"] as const)("retains %s host controls on failed reads", async (host) => {
    render(surface(uuid, host));
    await screen.findByText("Could not load issue.");
    fireEvent.click(screen.getByRole("button", { name: host === "inbox" ? "Inbox back" : "Close peek" }));
    expect(back).toHaveBeenCalledTimes(1);
  });
  it.each(["route", "inbox", "peek"] as const)("%s retries only the current failed read, prevents reentry and settles after another failure", async (host) => {
    render(surface(host === "route" ? "SYN-1" : uuid, host));
    const retry = await screen.findByRole("button", { name: "Retry" });
    const previousReads = primary.mock.calls.length;
    let settle!: (value: Response) => void;
    primary.mockImplementation(() => new Promise<Response>((resolve) => { settle = resolve; }));
    fireEvent.click(retry);
    fireEvent.click(retry);
    expect(retry).toBeDisabled();
    expect(retry).toHaveAttribute("aria-busy", "true");
    expect(primary).toHaveBeenCalledTimes(previousReads + 1);
    await act(async () => { settle(response(503)); });
    await waitFor(() => expect(retry).not.toBeDisabled());
    expect(primary).toHaveBeenCalledTimes(previousReads + 1);
  });
  it("treats an unreadable successful response as a generic load failure", async () => {
    primary.mockImplementation(() => Promise.resolve(response(200, null)));
    render(surface("SYN-1"));
    await screen.findByText("Could not load issue.");
    expect(screen.getByRole("button", { name: "Retry" })).toBeEnabled();
  });
  it.each(["SYN-1", uuid])("opens a valid %s route without redundant primary refresh", async (id) => {
    primary.mockImplementation(() => Promise.resolve(response(200, issue)));
    render(surface(id));
    await screen.findAllByText("Synthetic issue");
    await act(async () => { await new Promise((r) => setTimeout(r, 80)); });
    // One primary read plus the existing independent reactions read.
    expect(primary).toHaveBeenCalledTimes(2);
    if (id === uuid) expect(replace).toHaveBeenCalledWith("/synthetic/issues/SYN-1#comment-synthetic");
  });
  it.each(["route", "inbox", "peek"] as const)("%s recovers the real detail after a manual retry returns 200", async (host) => {
    render(surface(host === "route" ? "SYN-1" : uuid, host));
    const retry = await screen.findByRole("button", { name: "Retry" });
    primary.mockImplementation(() => Promise.resolve(response(200, issue)));
    fireEvent.click(retry);
    await screen.findAllByText("Synthetic issue");
    expect(screen.queryByText("Could not load issue.")).not.toBeInTheDocument();
    // Recovery plus the existing independent reactions read.
    expect(primary).toHaveBeenCalledTimes(3);
  });
  it.each(["route", "inbox", "peek"] as const)("%s keeps hydrated content on a background refresh failure", async (host) => {
    qc.setQueryData(issueKeys.detail(scope.wsId, uuid), issue);
    render(surface(uuid, host));
    const title = (await screen.findAllByText("Synthetic issue"))[0];
    await waitFor(() => expect(qc.getQueryState(issueKeys.detail(scope.wsId, uuid))?.status).toBe("error"));
    expect(title).toBeInTheDocument();
    expect(screen.queryByText("Could not load issue.")).not.toBeInTheDocument();
    expect(screen.queryByText(enIssues.detail.not_found_title)).not.toBeInTheDocument();
  });
  it.each(["workspace", "route", "both"])("ignores a retry finishing after the %s changes", async (changed) => {
    const view = render(surface("SYN-1"));
    const retry = await screen.findByRole("button", { name: "Retry" });
    let settle!: (value: Response) => void;
    primary.mockImplementationOnce(() => new Promise<Response>((resolve) => { settle = resolve; }));
    fireEvent.click(retry);
    if (changed !== "route") scope.wsId = "ws-other";
    view.rerender(surface(changed === "workspace" ? "SYN-1" : "OTHER-2"));
    await screen.findByText("Could not load issue.");
    await act(async () => { settle(response(200, issue)); });
    expect(screen.queryByText("Synthetic issue")).not.toBeInTheDocument();
    expect(replace).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "Retry" })).toBeEnabled();
    expect(qc.getQueryData(issueKeys.detail("ws-other", uuid))).toBeUndefined();
  });
  it("allows an in-flight retry to settle after unmount", async () => {
    const view = render(surface("SYN-1"));
    const retry = await screen.findByRole("button", { name: "Retry" });
    let settle!: (value: Response) => void;
    primary.mockImplementationOnce(() => new Promise<Response>((resolve) => { settle = resolve; }));
    fireEvent.click(retry);
    view.unmount();
    await act(async () => { settle(response(503)); });
    expect(replace).not.toHaveBeenCalled();
  });

  it.each(["SYN-1", uuid])("resolves %s through the real API with one canonical lookup", async (id) => {
    primary.mockImplementation(() => Promise.resolve(response(200, issue)));
    const { result, rerender } = renderHook(() => useCanonicalIssue(scope.wsId, id), {
      wrapper: ({ children }) => <QueryClientProvider client={qc}>{children}</QueryClientProvider>,
    });
    await waitFor(() => expect(result.current.issue?.id).toBe(uuid));
    rerender();
    expect(primary).toHaveBeenCalledTimes(1);
  });

  it("preserves the API client's unauthorized callback", async () => {
    const onUnauthorized = vi.fn();
    setApiInstance(new ApiClient("https://synthetic.invalid", { onUnauthorized }));
    primary.mockImplementation(() => Promise.resolve(response(401)));
    render(surface("SYN-1"));
    await screen.findByText(enIssues.detail.not_found_title);
    expect(onUnauthorized).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole("button", { name: "Retry" })).not.toBeInTheDocument();
  });

});
