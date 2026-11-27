import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiClient, setApiInstance } from "@multica/core/api";
import { closeGraphScope, denyGraphContent, graphIssueOptions, graphListOptions, membershipOptions } from "@multica/core/trajectory/graph-web";
import type { GraphSubscription } from "@multica/core/trajectory/graph-web";
import type { IssueTableQuerySpec, WSEventType } from "@multica/core/types";
import { paths } from "@multica/core/paths";
import { NavigationProvider, type NavigationAdapter } from "../../../../navigation";
import { renderWithI18n } from "../../../../test/i18n";
import { GraphTrajectoryView } from "./graph-trajectory-view";
import { graphFixture, graphId, graphNode, graphQuery, graphScope } from "./graph-fixture";

// The filter menu is the issues page one, which reads the route's workspace.
vi.mock("@multica/core/hooks", async importOriginal => ({ ...await importOriginal<typeof import("@multica/core/hooks")>(), useWorkspaceId: () => graphScope.workspaceId }));
vi.mock("@multica/core/paths", async importOriginal => ({ ...await importOriginal<typeof import("@multica/core/paths")>(), useWorkspacePaths: () => paths.workspace("synthetic") }));
const adapter: NavigationAdapter = { pathname: "/synthetic/issues", searchParams: new URLSearchParams(), hash: "", push: vi.fn(), replace: vi.fn(), back: vi.fn(), getShareableUrl: path => path };
const clients: QueryClient[] = [];
const windowListKey = (ids: string[]) => [...graphListOptions(graphScope, ids).queryKey, { ...graphQuery, search: "" }];
beforeEach(() => { setApiInstance(new ApiClient("")); });
afterEach(() => { cleanup(); clients.forEach(client => client.clear()); clients.length = 0; vi.unstubAllGlobals(); vi.restoreAllMocks(); });
function setup(count = 3, realtime?: GraphSubscription, navigation?: import("@multica/core/trajectory/web-entry").TrajectoryNavigation, customize?: (fixture: ReturnType<typeof graphFixture>) => void) {
  const fixture = graphFixture(count);
  customize?.(fixture);
  vi.stubGlobal("fetch", fixture.fetch);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  clients.push(client);
  let query: IssueTableQuerySpec = graphQuery;
  let ready = true;
  const render = (scope = graphScope) => <QueryClientProvider client={client}><NavigationProvider value={adapter}><GraphTrajectoryView scope={scope} query={query} ready={ready} realtime={realtime} navigation={navigation}><p>Required filter directory unavailable</p></GraphTrajectoryView></NavigationProvider></QueryClientProvider>;
  const result = renderWithI18n(render());
  return { fixture, client, rerenderReady: (next: boolean) => { ready = next; result.rerender(render()); }, rerenderQuery: (next: IssueTableQuerySpec) => { query = next; result.rerender(render()); }, user: userEvent.setup(), rerender: (scope: typeof graphScope) => result.rerender(render(scope)), hide: () => result.rerender(<p>Native task list</p>), show: () => result.rerender(render()) };
}
async function openInput(user: ReturnType<typeof userEvent.setup>) {
  const summary = within(screen.getByRole("dialog")).getByText("Input");
  const details = summary.closest("details")!;
  if (!details.open) await user.click(summary);
  return within(details);
}
it("keeps one status filter for every expanded project and narrows them to its category", async () => {
  const { user } = setup();
  await screen.findByText("Synthetic parent");
  await user.click(screen.getByRole("button", { name: "Second project" }));
  expect(screen.getAllByTestId("trajectory-timeline")).toHaveLength(2);
  const chips = screen.getAllByRole("radiogroup", { name: "Filter by status" });
  expect(chips).toHaveLength(1);
  const all = within(chips[0]!).getByRole("radio", { name: /^All/ });
  expect(all).toHaveAttribute("aria-checked", "true");
  const empty = within(chips[0]!).getAllByRole("radio").find(radio => radio !== all && radio.textContent?.endsWith("0"))!;
  await user.click(empty);
  expect(empty).toHaveAttribute("aria-checked", "true");
  expect(screen.queryAllByTestId("trajectory-timeline")).toHaveLength(0);
  await user.keyboard("{Home}");
  await user.click(all);
  expect(screen.getAllByTestId("trajectory-timeline")).toHaveLength(2);
});
it.each([
  ["membership", "comment", "500"], ["list", "comment", "500"], ["membership", "run", "500"], ["list", "run", "500"],
  ["membership", "comment", "network"], ["list", "comment", "network"], ["membership", "run", "network"], ["list", "run", "network"],
] as const)("requires actual window recovery after %s failure and %s cleanup (%s)", async (layer, deniedKind, failure) => {
  let mode: "success" | "failed" | "held" = "success";
  const release: Partial<Record<"membership" | "list", () => void>> = {};
  const { client, user } = setup(2, undefined, undefined, value => {
    value.list.nodes[0]!.status = "blocked"; value.list.nodes[1]!.status = "done";
    const original = value.fetch;
    value.fetch = (input, init) => {
      const path = new URL(String(input), "https://synthetic.invalid").pathname;
      const read = path === "/api/issues/table/rows" ? "membership" : path === "/api/issues/trajectory" ? "list" : null;
      if (read === layer && mode === "failed") return failure === "network" ? Promise.reject(new TypeError("Synthetic offline")) : Promise.resolve(new Response("Synthetic failure", { status: 500 }));
      if (read && mode === "held") return new Promise<Response>(resolve => { release[read] = () => { void original(input, init).then(resolve); }; });
      return original(input, init);
    };
  });
  const summary = await screen.findByRole("group", { name: "First project · Current window" });
  await user.click(screen.getByRole("button", { name: "Expand runs Synthetic parent" }));
  await screen.findByRole("button", { name: "Record details Synthetic execution" });
  await user.click(within(summary).getByRole("button", { name: "Blocked 1" }));
  const memberKey = membershipOptions(graphScope, { ...graphQuery, search: "" }).queryKey;
  const listKey = windowListKey([graphId(10), graphId(11)]);
  const failedKey = layer === "membership" ? memberKey : listKey;
  mode = "failed";
  await act(async () => { await client.refetchQueries({ queryKey: failedKey, exact: true }); });
  await within(summary).findByText("Summary unavailable");
  expect(client.getQueryState(failedKey)?.status).toBe("error");
  act(() => denyGraphContent(client, graphScope, [`${deniedKind}:${graphId(deniedKind === "comment" ? 40 : 20)}`]));
  expect(client.getQueryState(failedKey)?.status).toBe("success");
  expect(client.getQueryData(failedKey)).toBeTruthy();
  expect(within(summary).getByText("Summary unavailable")).toBeVisible();
  expect(within(summary).queryByRole("button")).not.toBeInTheDocument();
  await act(async () => { await client.refetchQueries({ queryKey: failedKey, exact: true }); });
  expect(client.getQueryState(failedKey)?.status).toBe("error");
  expect(within(summary).getByText("Summary unavailable")).toBeVisible();
  mode = "held";
  let memberRead!: Promise<void>, listRead!: Promise<void>;
  act(() => { memberRead = client.refetchQueries({ queryKey: memberKey, exact: true }); listRead = client.refetchQueries({ queryKey: listKey, exact: true }); });
  await waitFor(() => { expect(release.membership).toBeTypeOf("function"); expect(release.list).toBeTypeOf("function"); });
  expect(client.getQueryState(memberKey)?.fetchStatus).toBe("fetching");
  expect(client.getQueryState(listKey)?.fetchStatus).toBe("fetching");
  expect(within(summary).getByText("Summary unavailable")).toBeVisible();
  await act(async () => { release.membership!(); await memberRead; });
  expect(within(summary).getByText("Summary unavailable")).toBeVisible();
  await act(async () => { release.list!(); await listRead; });
  const category = await within(summary).findByRole("button", { name: "Blocked 1" });
  expect(document.querySelector("[data-summary-target=true]")).toBeNull();
  await user.click(category);
  expect(screen.getByText("Synthetic parent").closest("[data-node-id]")).toHaveAttribute("data-summary-target", "true");
});

it("uses compact record titles and locally returns from comment to Run without requesting logs", async () => {
  const { fixture, user } = setup();
  await user.click(await screen.findByRole("button", { name: "Expand runs Synthetic parent" }));
  const opener = await screen.findByRole("button", { name: "Record details Synthetic execution" });
  await user.click(opener);
  const dialog = await screen.findByRole("dialog", { name: "Run · 00000020" });
  await user.click(within(dialog).getByRole("button", { name: "Synthetic comment" }));
  const comment = await screen.findByRole("dialog", { name: "Comment · 00000040" });
  expect(within(comment).getByText("Private comment body")).toBeVisible();
  await user.click(within(comment).getByRole("button", { name: "Back" }));
  await screen.findByRole("dialog", { name: "Run · 00000020" });
  expect(fixture.requests.filter(request => request.path.endsWith("/messages"))).toHaveLength(0);
  await user.keyboard("{Escape}");
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(opener).toHaveFocus();
});

it("joins calls from the two lazy Issue reads and hides them when the trigger proof is denied", async () => {
  const { client, fixture, user } = setup(2, undefined, undefined, value => {
    const target = graphNode("run", 21, 11, "Triggered child", { run_call: { comment_id: graphId(50), source_issue_id: graphId(10) }, delegated_from: value.run.run_id,
      trigger_evidence_kind: "comment", trigger_evidence_ref_id: graphId(50), trigger_comment_id: graphId(50) });
    const comment = { ...graphNode("comment", 50, 11, "Trigger comment", { comment_id: graphId(50), author_type: "agent", source_run_id: value.run.run_id }), run_id: value.run.run_id };
    const original = value.fetch;
    value.fetch = async (input, init) => {
      const response = await original(input, init);
      if (new URL(String(input), "https://synthetic.invalid").pathname !== `/api/issues/${graphId(11)}/trajectory`) return response;
      const data = await response.json();
      return Response.json({ ...data, nodes: [value.list.nodes[1], target, comment] });
    };
  });
  await user.click(await screen.findByRole("button", { name: "Expand runs Synthetic parent" }));
  await screen.findByRole("button", { name: "Record details Synthetic execution" });
  expect(screen.queryByTestId("trajectory-run-link")).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Expand runs Synthetic child" }));
  const arrow = await screen.findByTestId("trajectory-run-link");
  expect(arrow).toHaveAttribute("data-from", fixture.run.id);
  expect(arrow).toHaveAttribute("data-to", `run:${graphId(21)}`);
  expect(fixture.requests.filter(request => request.method === "GET")).toHaveLength(2);
  act(() => denyGraphContent(client, graphScope, [`comment:${graphId(50)}`]));
  await waitFor(() => expect(screen.queryByTestId("trajectory-run-link")).not.toBeInTheDocument());
  // A Run whose trigger comment is denied retains its identity, not its trigger-derived title.
  expect(screen.getByRole("button", { name: "Record details Agent run" })).toBeVisible();
});

it("clears a denied comment's stored purpose from both readable and raw Run details", async () => {
  const { client, user } = setup(3, undefined, undefined, value => { value.run.details.trigger_summary = "Denied private purpose"; });
  await user.click(await screen.findByRole("button", { name: "Expand runs Synthetic parent" }));
  await user.click(await screen.findByRole("button", { name: "Record details Synthetic execution" }));
  await screen.findByRole("dialog", { name: "Run · 00000020" });
  expect(screen.getByText("Denied private purpose")).toBeVisible();
  act(() => denyGraphContent(client, graphScope, ["comment:" + graphId(40)]));
  await waitFor(() => expect(screen.queryByText("Denied private purpose")).not.toBeInTheDocument());
  expect(screen.getByTestId("trajectory-raw-node").textContent).not.toContain("Denied private purpose");
});

it.each([undefined, "", " "])("keeps logs unavailable without a nonblank returned run_id (%s)", async runId => {
  const { fixture, user } = setup(1, undefined, undefined, value => { value.run.run_id = runId; });
  await user.click(await screen.findByRole("button", { name: "Expand runs Synthetic parent" }));
  await user.click(await screen.findByRole("button", { name: "Record details Synthetic execution" }));
  const dialog = await screen.findByRole("dialog");
  const logs = within(dialog).getByRole("tab", { name: "Logs" });
  expect(logs).toHaveAttribute("aria-disabled", "true");
  await user.click(logs);
  expect(within(dialog).queryByTestId("trajectory-log")).not.toBeInTheDocument();
  expect(within(dialog).queryByRole("button", { name: "Open log" })).not.toBeInTheDocument();
  expect(fixture.requests.filter(request => request.path.endsWith("/messages"))).toHaveLength(0);
});

it("shows the actual associated Run identity when opening logs from a returned comment", async () => {
  const { user } = setup();
  await user.click(await screen.findByRole("button", { name: "Expand runs Synthetic parent" }));
  await user.click(await screen.findByRole("button", { name: "Record details Synthetic execution" }));
  let dialog = await screen.findByRole("dialog", { name: "Run · 00000020" });
  await user.click(within(dialog).getByRole("button", { name: "Synthetic comment" }));
  dialog = await screen.findByRole("dialog", { name: "Comment · 00000040" });
  await user.click(within(dialog).getByRole("tab", { name: "Logs" }));
  const log = await within(dialog).findByTestId("trajectory-log");
  expect(within(log).getByText("run:" + graphId(20))).toBeVisible();
  expect(within(log).queryByText("comment:" + graphId(40))).not.toBeInTheDocument();
});

it("does not restore a denied Run through local dialog history", async () => {
  const { client, user } = setup();
  await user.click(await screen.findByRole("button", { name: "Expand runs Synthetic parent" }));
  await user.click(await screen.findByRole("button", { name: "Record details Synthetic execution" }));
  const dialog = await screen.findByRole("dialog", { name: "Run · 00000020" });
  await user.click(within(dialog).getByRole("button", { name: "Synthetic comment" }));
  await screen.findByRole("dialog", { name: "Comment · 00000040" });
  act(() => denyGraphContent(client, graphScope, ["run:" + graphId(20)]));
  await waitFor(() => expect(screen.queryByRole("button", { name: "Back" })).not.toBeInTheDocument());
  expect(screen.queryByTestId("trajectory-output-preview")).not.toBeInTheDocument();
});

it.each(["query", "identity"] as const)("clears local record history and content on %s change", async change => {
  const { fixture, user, rerenderQuery, rerender } = setup();
  await user.click(await screen.findByRole("button", { name: "Expand runs Synthetic parent" }));
  await user.click(await screen.findByRole("button", { name: "Record details Synthetic execution" }));
  const dialog = await screen.findByRole("dialog", { name: "Run · 00000020" });
  await user.click(within(dialog).getByRole("button", { name: "Synthetic comment" }));
  const comment = await screen.findByRole("dialog", { name: "Comment · 00000040" });
  expect(within(comment).getByRole("button", { name: "Back" })).toBeVisible();
  if (change === "query") rerenderQuery({ ...graphQuery, filters: { priorities: ["urgent"] } });
  else { fixture.fail(403); rerender({ ...graphScope, accountId: graphId(9) }); }
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Back" })).not.toBeInTheDocument();
  expect(screen.queryByText("Private comment body")).not.toBeInTheDocument();
});

it("locates an expanded ancestor Run in the project scene that requested it", async () => {
  const { user } = setup(3, undefined, undefined, value => {
    value.list.nodes[1]!.details.project_id = graphId(31); value.list.nodes[1]!.details.project_title = "Second project";
  });
  await user.click(await screen.findByRole("button", { name: "Expand runs Synthetic parent" }));
  await screen.findByRole("button", { name: "Locate latest record" });
  await user.click(screen.getByRole("button", { name: "Second project" }));
  const section = screen.getByRole("group", { name: "Second project · Current window" }).closest("section")!;
  await user.click(within(section).getByRole("button", { name: "Locate latest record" }));
  expect(within(section).getByRole("button", { name: "Record details Synthetic execution" })).toHaveFocus();
});

it("locates only loaded terminal Run history and preserves order without prefetching logs", async () => {
  const { fixture, user } = setup(1, undefined, undefined, value => {
    const original = value.fetch;
    value.fetch = async (input, init) => {
      const response = await original(input, init);
      if (new URL(String(input), "https://synthetic.invalid").pathname !== "/api/issues/" + graphId(10) + "/trajectory") return response;
      const own = await response.json();
      const runs = Array.from({ length: 22 }, (_, n) => ({ ...graphNode("run", 100 + n, 10, "History " + n), status: n % 2 ? "completed" : "cancelled", created_at: new Date(Date.UTC(2026, 10, 19, 0, n)).toISOString() }));
      return Response.json({ ...own, nodes: [{ ...own.nodes[0], details: { ...own.nodes[0].details, run_count: 22 } }, ...runs], edges: [] });
    };
  });
  await screen.findByText("Synthetic parent");
  expect(screen.queryByRole("button", { name: "Locate latest record" })).not.toBeInTheDocument();
  expect(fixture.requests.filter(request => request.path.endsWith("/trajectory") && request.method === "GET")).toHaveLength(0);
  await user.click(screen.getByRole("button", { name: "Expand runs Synthetic parent" }));
  const latest = await screen.findByRole("button", { name: "Locate latest record" });
  expect(screen.getByRole("button", { name: "Locate running" })).toBeDisabled();
  await user.click(latest);
  expect(screen.getByRole("button", { name: "Record details History 21" })).toHaveFocus();
  const ids = Array.from(document.querySelectorAll('[data-node-id^="run:"]')).map(row => row.getAttribute("data-node-id"));
  expect(ids).toEqual(Array.from({ length: 22 }, (_, n) => "run:" + graphId(100 + n)));
  expect(fixture.requests.filter(request => request.path.endsWith("/messages"))).toHaveLength(0);
  await user.click(screen.getByRole("button", { name: "Collapse runs Synthetic parent" }));
  await user.click(screen.getByRole("button", { name: "Expand runs Synthetic parent" }));
  expect(screen.getByRole("button", { name: "Collapse runs Synthetic parent" })).toHaveFocus();
});

it("focuses the newest loaded record from the header", async () => {
  const { user } = setup();
  await screen.findByText("Synthetic parent");
  await user.click(screen.getByRole("button", { name: "Expand runs Synthetic parent" }));
  await screen.findByRole("button", { name: "Record details Synthetic execution" });
  await user.click(screen.getByRole("button", { name: "Latest record" }));
  const focused = document.activeElement as HTMLElement;
  expect(focused).toHaveClass("trajectory-content");
  expect(focused.closest("[data-node-id]")).toBeInTheDocument();
});

it("does not invalidate a successful window for an ordinary own-Issue read failure", async () => {
  const { user } = setup(2, undefined, undefined, value => {
    value.list.nodes[0]!.status = "blocked";
    const original = value.fetch;
    value.fetch = (input, init) => new URL(String(input), "https://synthetic.invalid").pathname === `/api/issues/${graphId(10)}/trajectory`
      ? Promise.resolve(new Response("Synthetic own failure", { status: 500 })) : original(input, init);
  });
  await user.click(await screen.findByRole("button", { name: "Expand runs Synthetic parent" }));
  await screen.findByText("Could not load records");
  const summary = within(screen.getByRole("group", { name: "First project · Current window" }));
  expect(summary.getByRole("button", { name: "Blocked 1" })).toBeEnabled();
  expect(summary.queryByText("Summary unavailable")).not.toBeInTheDocument();
});
it("keeps a healthy required window available during ordinary comment cleanup", async () => {
  const { client, user } = setup(2, undefined, undefined, value => { value.list.nodes[0]!.status = "blocked"; });
  await user.click(await screen.findByRole("button", { name: "Expand runs Synthetic parent" }));
  await screen.findByRole("button", { name: "Record details Synthetic execution" });
  act(() => denyGraphContent(client, graphScope, [`comment:${graphId(40)}`]));
  const summary = within(screen.getByRole("group", { name: "First project · Current window" }));
  expect(summary.getByRole("button", { name: "Blocked 1" })).toBeEnabled();
  expect(summary.queryByText("Summary unavailable")).not.toBeInTheDocument();
});
it("recovers an empty membership window after real success without requiring its obsolete list", async () => {
  let mode: "success" | "failed" | "empty" = "success";
  const { client } = setup(2, undefined, undefined, value => {
    value.list.nodes[0]!.status = "blocked";
    const original = value.fetch;
    value.fetch = (input, init) => {
      if (new URL(String(input), "https://synthetic.invalid").pathname !== "/api/issues/table/rows" || mode === "success") return original(input, init);
      return Promise.resolve(mode === "failed" ? new Response("Synthetic failure", { status: 500 }) : Response.json({ query_fingerprint: "empty", group_key: null, parent_id: null, total: 0, rows: [], branch_total: 0, next_cursor: null }));
    };
  });
  await screen.findByRole("button", { name: "Blocked 1" });
  const memberKey = membershipOptions(graphScope, { ...graphQuery, search: "" }).queryKey;
  mode = "failed";
  await act(async () => { await client.refetchQueries({ queryKey: memberKey, exact: true }); });
  await screen.findByText("Summary unavailable", { selector: ".trajectory-loaded" });
  mode = "empty";
  await act(async () => { await client.refetchQueries({ queryKey: memberKey, exact: true }); });
  expect(await screen.findByRole("button", { name: "View full trajectory" })).toBeEnabled();
  expect(screen.getByRole("status", { name: "0 loaded tasks" })).toBeVisible();
});
it.each(["membership", "list"] as const)("invalidates cached %s summary on HTTP failure and restores fresh numbers without old targets", async layer => {
  let failed = false;
  const path = layer === "membership" ? "/api/issues/table/rows" : "/api/issues/trajectory";
  const { client, user } = setup(3, undefined, undefined, value => {
    value.list.nodes[1]!.status = "blocked";
    const original = value.fetch;
    value.fetch = (input, init) => failed && new URL(String(input), "https://synthetic.invalid").pathname === path
      ? Promise.resolve(new Response("Synthetic read failure", { status: 500 })) : original(input, init);
  });
  const summary = await screen.findByRole("group", { name: "First project · Current window" });
  await user.click(within(summary).getByRole("button", { name: "Blocked 1" }));
  const key = layer === "membership" ? membershipOptions(graphScope, { ...graphQuery, search: "" }).queryKey
    : windowListKey([graphId(10), graphId(11), graphId(12)]);
  failed = true;
  await act(async () => { await client.refetchQueries({ queryKey: key, exact: true }); });
  expect(client.getQueryState(key)?.status).toBe("error");
  expect(client.getQueryData(key)).toBeTruthy();
  await within(summary).findByText("Summary unavailable");
  expect(within(summary).queryByRole("button")).not.toBeInTheDocument();
  expect(screen.getByText("Synthetic child").closest("[data-node-id]")).not.toHaveAttribute("data-summary-target");
  expect(screen.getByText("Synthetic child").closest("[data-node-id]")).not.toHaveAttribute("data-current-state");
  failed = false;
  await act(async () => {
    await client.refetchQueries({ queryKey: membershipOptions(graphScope, { ...graphQuery, search: "" }).queryKey, exact: true });
    await client.refetchQueries({ queryKey: windowListKey([graphId(10), graphId(11), graphId(12)]), exact: true });
  });
  const category = await within(summary).findByRole("button", { name: "Blocked 1" });
  expect(category).toBeEnabled();
  expect(screen.getByText("Synthetic child").closest("[data-node-id]")).not.toHaveAttribute("data-summary-target");
  await user.click(category);
  expect(screen.getByText("Synthetic child").closest("[data-node-id]")).toHaveAttribute("data-summary-target", "true");
});

it.each(["membership", "list"] as const)("hides cached %s numbers on a network failure without presenting zero completion", async layer => {
  let offline = false;
  const path = layer === "membership" ? "/api/issues/table/rows" : "/api/issues/trajectory";
  const { client } = setup(3, undefined, undefined, value => {
    value.list.nodes[1]!.status = "blocked";
    const original = value.fetch;
    value.fetch = (input, init) => offline && new URL(String(input), "https://synthetic.invalid").pathname === path
      ? Promise.reject(new TypeError("Synthetic offline")) : original(input, init);
  });
  const summary = await screen.findByRole("group", { name: "First project · Current window" });
  expect(within(summary).getByRole("button", { name: "Blocked 1" })).toBeEnabled();
  const key = layer === "membership" ? membershipOptions(graphScope, { ...graphQuery, search: "" }).queryKey
    : windowListKey([graphId(10), graphId(11), graphId(12)]);
  offline = true;
  await act(async () => { await client.refetchQueries({ queryKey: key, exact: true }); });
  expect(client.getQueryState(key)?.status).toBe("error");
  await within(summary).findByText("Summary unavailable");
  expect(within(summary).queryByText("Blocked 0")).not.toBeInTheDocument();
  expect(within(summary).queryByText("Blocked 1")).not.toBeInTheDocument();
  expect(screen.queryByText("No known running, blocked or awaiting-review tasks in the current window")).not.toBeInTheDocument();
});

it("removes a known denied Issue from current summary and targets without restoring it", async () => {
  const { client, user } = setup(3, undefined, undefined, value => { value.list.nodes[1]!.status = "blocked"; });
  const summary = await screen.findByRole("group", { name: "First project · Current window" });
  await user.click(within(summary).getByRole("button", { name: "Blocked 1" }));
  act(() => denyGraphContent(client, graphScope, [`issue:${graphId(11)}`]));
  await waitFor(() => expect(screen.queryByText("Synthetic child")).not.toBeInTheDocument());
  expect(screen.queryByRole("button", { name: "Blocked 1" })).not.toBeInTheDocument();
  expect(document.querySelector("[data-summary-target=true]")).toBeNull();
});
it("defaults to all parallel current Issue focuses without prefetching Runs and can show the full trajectory", async () => {
  const { fixture, user } = setup(4, undefined, undefined, value => {
    value.list.nodes[0]!.status = "in_progress"; value.list.nodes[1]!.status = "blocked";
    value.list.nodes[2]!.status = "done"; value.list.nodes[3]!.status = "in_review";
  });
  const focus = await screen.findByRole("button", { name: "Focus current execution" });
  await screen.findByText("Synthetic parent");
  expect(focus).toHaveAttribute("aria-pressed", "true");
  const summary = within(screen.getByRole("group", { name: "First project · Current window" }));
  expect(summary.getByRole("button", { name: "Running 1" })).toBeEnabled();
  expect(summary.getByRole("button", { name: "Blocked 1" })).toBeEnabled();
  expect(summary.getByRole("button", { name: "Awaiting review 1" })).toBeEnabled();
  expect(screen.getByText("Synthetic parent").closest("[data-node-id]")).toHaveAttribute("data-current-state", "running");
  expect(screen.getByText("Synthetic child").closest("[data-node-id]")).toHaveAttribute("data-current-state", "blocked");
  expect(screen.getByText("Window issue 0").closest("[data-node-id]")).toHaveAttribute("data-current-state", "review");
  expect(fixture.requests.filter(r => /^\/api\/issues\/[^/]+\/trajectory$/.test(r.path))).toHaveLength(0);
  await user.click(focus);
  expect(focus).toHaveAttribute("aria-pressed", "false");
  expect(screen.getByText("Synthetic child")).toBeVisible();
  expect(summary.getByRole("button", { name: "Blocked 1" })).toBeVisible();
});

it("keeps completed ancestor context readable without adding it to the matching summary", async () => {
  const { user } = setup(3, undefined, undefined, value => { value.list.nodes[0]!.status = "done"; value.list.nodes[1]!.status = "blocked"; });
  await screen.findByText("Synthetic parent");
  await user.type(screen.getByRole("textbox", { name: "Search tasks" }), "child");
  expect(screen.getByRole("textbox", { name: "Search tasks" })).toHaveValue("child");
  const summary = await screen.findByRole("group", { name: "First project · Current window" });
  expect(within(summary).getByRole("button", { name: "Blocked 1" })).toBeVisible();
  expect(screen.getByText("Synthetic parent").closest("[data-node-id]")).toHaveAttribute("data-focus-role", "ancestor");
  expect(await screen.findByRole("status", { name: "1 loaded task" })).toBeVisible();
});

it("opens a collapsed project and locates all category matches without reading their Runs", async () => {
  const scroll = vi.fn();
  vi.spyOn(HTMLElement.prototype, "scrollIntoView").mockImplementation(scroll);
  const { user, fixture } = setup(3, undefined, undefined, value => { value.list.nodes[2]!.status = "in_review"; });
  const summary = await screen.findByRole("group", { name: "Second project · Current window" });
  await user.click(within(summary).getByRole("button", { name: "Awaiting review 1" }));
  const target = await screen.findByText("Synthetic other");
  expect(screen.getByRole("button", { name: "Second project" })).toHaveAttribute("aria-expanded", "true");
  await waitFor(() => expect(target.closest("button")).toHaveFocus());
  expect(target.closest("[data-node-id]")).toHaveAttribute("data-summary-target", "true");
  expect(scroll).toHaveBeenCalled();
  expect(fixture.requests.filter(r => /^\/api\/issues\/[^/]+\/trajectory$/.test(r.path))).toHaveLength(0);
});
it("retains a loaded cross-project parent as context without counting or expanding it", async () => {
  const { user, fixture } = setup(3, undefined, undefined, value => {
    value.list.nodes[0]!.status = "done"; value.list.nodes[1]!.status = "blocked";
    value.list.nodes[1]!.details.project_id = graphId(31); value.list.nodes[1]!.details.project_title = "Child project";
  });
  await screen.findByText("Synthetic parent");
  await user.type(screen.getByRole("textbox", { name: "Search tasks" }), "child");
  await screen.findByRole("status", { name: "1 loaded task" });
  const section = screen.getByRole("group", { name: "Child project · Current window" }).closest("section")!;
  expect(within(section).getByText("Synthetic parent").closest("[data-node-id]")).toHaveAttribute("data-focus-role", "ancestor");
  expect(within(section).getByRole("button", { name: "Blocked 1" })).toBeEnabled();
  await user.click(screen.getByRole("button", { name: "Focus current execution" }));
  expect(within(section).getByText("Synthetic parent")).toBeVisible();
  expect(fixture.requests.filter(r => /^\/api\/issues\/[^/]+\/trajectory$/.test(r.path))).toHaveLength(0);
});
it("locates the chosen native project instead of an earlier duplicate ancestor row", async () => {
  const { user } = setup(4, undefined, undefined, value => {
    value.list.nodes[0]!.status = "in_progress";
    value.list.nodes[0]!.details.project_id = graphId(31); value.list.nodes[0]!.details.project_title = "Parent project";
    value.list.nodes[1]!.status = "blocked"; value.list.nodes[3]!.created_at = "2026-11-18T00:00:00Z";
  });
  await screen.findByText("Synthetic parent");
  const summary = screen.getByRole("group", { name: "Parent project · Current window" });
  await user.click(within(summary).getByRole("button", { name: "Running 1" }));
  await waitFor(() => expect(document.activeElement?.closest("[data-project-key]")).toHaveAttribute("data-project-key", graphId(31)));
  const first = screen.getByRole("group", { name: "First project · Current window" }).closest("section")!;
  expect(first.querySelector("[data-summary-target=true]")).toBeNull();
});
it("returns from duplicate Issue details to the main row in the project that opened it", async () => {
  const { user } = setup(4, undefined, undefined, value => {
    value.list.nodes[0]!.status = "in_progress";
    value.list.nodes[0]!.details.project_id = graphId(31); value.list.nodes[0]!.details.project_title = "Parent project";
    value.list.nodes[1]!.status = "blocked"; value.list.nodes[3]!.created_at = "2026-11-18T00:00:00Z";
  });
  await user.click(await screen.findByRole("button", { name: "Parent project" }));
  const section = screen.getByRole("group", { name: "Parent project · Current window" }).closest("section")!;
  const main = within(section).getByRole("button", { name: "Expand runs Synthetic parent" });
  await user.click(within(section).getByRole("button", { name: "Record details Synthetic parent" }));
  await screen.findByRole("dialog");
  await user.keyboard("{Escape}");
  await waitFor(() => expect(main).toHaveFocus());
});

it("keeps summary numbers unchanged when a historical completed blocked-workflow Run expands", async () => {
  const { user } = setup(3, undefined, undefined, value => { value.list.nodes[0]!.status = "done"; value.run.details.workflow_step = "blocked"; });
  const summary = await screen.findByRole("group", { name: "First project · Current window" });
  expect(within(summary).getByRole("button", { name: "Blocked 0" })).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Expand runs Synthetic parent" }));
  await screen.findByRole("button", { name: "Record details Synthetic execution" });
  expect(within(summary).getByRole("button", { name: "Blocked 0" })).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "1 · Artifact" }));
  expect(within(summary).getByRole("button", { name: "Running 0" })).toBeDisabled();
});
it("clears category targets when refreshed current Issue state leaves that category", async () => {
  const { user, fixture } = setup(3, undefined, undefined, value => { value.list.nodes[1]!.status = "blocked"; });
  const summary = await screen.findByRole("group", { name: "First project · Current window" });
  await user.click(within(summary).getByRole("button", { name: "Blocked 1" }));
  expect(screen.getByText("Synthetic child").closest("[data-node-id]")).toHaveAttribute("data-summary-target", "true");
  fixture.list.nodes[1]!.status = "done";
  await user.click(screen.getByRole("button", { name: "Refresh" }));
  await waitFor(() => expect(within(summary).getByRole("button", { name: "Blocked 0" })).toBeDisabled());
  expect(screen.getByText("Synthetic child").closest("[data-node-id]")).not.toHaveAttribute("data-summary-target");
});

it("provides an honest no-focus empty state and a complete-trajectory action", async () => {
  const { user } = setup();
  await screen.findByText("No known running, blocked or awaiting-review tasks in the current window");
  await user.click(screen.getByRole("button", { name: "View full trajectory" }));
  expect(screen.getByRole("button", { name: "Focus current execution" })).toHaveAttribute("aria-pressed", "false");
  expect(screen.getByText("Synthetic parent")).toBeVisible();
});
it("offers the complete trajectory entry for a zero matching window without a graph request", async () => {
  const { user, fixture } = setup(0);
  await user.click(await screen.findByRole("button", { name: "View full trajectory" }));
  expect(screen.getByRole("button", { name: "Focus current execution" })).toHaveAttribute("aria-pressed", "false");
  expect(fixture.requests.filter(r => r.path === "/api/issues/trajectory")).toHaveLength(0);
});
it("retains current list Issue state and project when an expanded own graph carries stale owner fields", async () => {
  const { user } = setup(3, undefined, undefined, value => {
    value.list.nodes[0]!.status = "in_progress";
    const original = value.fetch;
    value.fetch = async (input, init) => {
      const response = await original(input, init);
      if (new URL(String(input), "https://synthetic.invalid").pathname === `/api/issues/${graphId(10)}/trajectory`) {
        const data = await response.json();
        data.nodes = data.nodes.map((node: { kind: string; details: Record<string, unknown> }) => node.kind === "issue" ? { ...node, title: "Stale owner title", status: "blocked", details: { ...node.details, project_id: graphId(31) } } : node);
        return Response.json(data);
      }
      return response;
    };
  });
  await user.click(await screen.findByRole("button", { name: "Expand runs Synthetic parent" }));
  await screen.findByRole("button", { name: "Record details Synthetic execution" });
  const parent = screen.getByText("Synthetic parent").closest("[data-node-id]")!;
  expect(parent).toHaveAttribute("data-current-state", "running");
  expect(parent).toHaveAttribute("data-status-tone", "info");
  const summary = within(screen.getByRole("group", { name: "First project · Current window" }));
  expect(summary.getByRole("button", { name: "Running 1" })).toBeEnabled();
  expect(summary.getByRole("button", { name: "Blocked 0" })).toBeDisabled();
});

it("shows unknown current states instead of presenting an unknown window as fully inactive", async () => {
  setup(3, undefined, undefined, value => { value.list.nodes[0]!.status = "future-status"; });
  await screen.findByText("1 task with unknown current state");
  expect(screen.getByText("No known running, blocked or awaiting-review tasks in the current window")).toBeVisible();
  expect(screen.getByRole("button", { name: "View full trajectory" })).toBeEnabled();
});

it("drops current summaries and category targets immediately on account switch", async () => {
  const { user, fixture, rerender } = setup(3, undefined, undefined, value => { value.list.nodes[1]!.status = "blocked"; });
  const summary = await screen.findByRole("group", { name: "First project · Current window" });
  await user.click(within(summary).getByRole("button", { name: "Blocked 1" }));
  fixture.fail(403); rerender({ ...graphScope, accountId: graphId(9) });
  expect(screen.queryByRole("group", { name: "First project · Current window" })).not.toBeInTheDocument();
  expect(document.querySelector("[data-summary-target=true]")).toBeNull();
  await screen.findByRole("alert");
});
it("opens the first project and keeps parent-child rails visible while Issues independently fetch their own Runs", async () => {
  const { fixture, user } = setup();
  const first = await screen.findByRole("button", { name: "First project" });
  expect(first).toHaveAttribute("aria-expanded", "true");
  expect(screen.queryByText("11/19/2026")).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Second project" })).toHaveAttribute("aria-expanded", "false");
  expect(screen.queryByText("Synthetic other")).not.toBeInTheDocument();
  const parent = screen.getByRole("button", { name: "Expand runs Synthetic parent" });
  const child = screen.getByRole("button", { name: "Expand runs Synthetic child" });
  expect(parent).toHaveAttribute("aria-expanded", "false");
  expect(child).toHaveAttribute("aria-expanded", "false");
  expect(screen.getByTestId("trajectory-rail").querySelector("path[data-parent]")).toBeInTheDocument();
  expect(fixture.requests.filter(r => /^\/api\/issues\/[^/]+\/trajectory$/.test(r.path))).toHaveLength(0);
  await user.click(child);
  await screen.findByText("Child execution");
  expect(screen.queryByText("Synthetic execution")).not.toBeInTheDocument();
  await user.click(parent);
  await screen.findByText("Synthetic execution");
  expect(screen.getByText("Child execution")).toBeVisible();
  expect(fixture.requests.filter(r => /^\/api\/issues\/[^/]+\/trajectory$/.test(r.path)).map(r => r.path)).toEqual([`/api/issues/${graphId(11)}/trajectory`, `/api/issues/${graphId(10)}/trajectory`]);
  expect(fixture.requests.every(r => r.signal instanceof AbortSignal)).toBe(true);
});
it("renders native details, output preview and records, and loads messages only on Log with local task/comment viewing", async () => {
  const { fixture, user } = setup();
  await user.click(await screen.findByRole("button", { name: "Expand runs Synthetic parent" }));
  const opener = await screen.findByRole("button", { name: "Record details Synthetic execution" });
  await user.click(opener);
  const dialog = await screen.findByRole("dialog", { name: "Run · 00000020" });
  expect(dialog).toHaveClass("max-h-[85%]", "sm:max-w-[760px]");
  const input = await openInput(user);
  expect(input.getByText("Historical task title", { exact: false })).toBeVisible();
  expect(within(dialog).getByText("Saved instructions")).toBeVisible();
  expect(within(dialog).getByTestId("trajectory-run-purpose")).toHaveTextContent("Private trigger body");
  expect(within(dialog).getByTestId("trajectory-output-preview").textContent).toHaveLength(16000);
  expect(within(dialog).getByRole("button", { name: "Open task" })).toBeEnabled();
  expect(within(dialog).queryByRole("link", { name: "Open task" })).not.toBeInTheDocument();
  expect(fixture.requests.some(r => r.path.endsWith("/messages"))).toBe(false);
  await user.click(within(dialog).getByRole("button", { name: "Synthetic comment" }));
  expect(within(dialog).queryByRole("link", { name: "Open comment" })).not.toBeInTheDocument();
  expect(within(dialog).getByText("Private comment body")).toBeVisible();
  await user.click(within(dialog).getByRole("button", { name: /Run .*Synthetic execution/ }));
  await user.click(within(dialog).getByRole("button", { name: "report.txt" }));
  expect(within(dialog).getByRole("link", { name: "Preview" })).toHaveAttribute("href", `/synthetic/attachments/${graphId(41)}/preview`);
  await user.click(within(dialog).getByRole("button", { name: /Run .*Synthetic execution/ }));
  await user.click(within(dialog).getAllByRole("button", { name: "Open log" }).at(-1)!);
  const message = await within(dialog).findByText("7 · tool_use · Read");
  expect(within(dialog).getByText("Native message body")).toBeVisible();
  await user.click(message);
  await user.click(message);
  expect(within(dialog).getByText("Native message body")).toBeVisible();
  expect(fixture.requests.filter(r => r.path.endsWith("/messages"))).toHaveLength(1);
  await user.keyboard("{Escape}");
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(opener).toHaveFocus();
});
it("presents a manual 100-row window and stops at 200 without calling an extra page", async () => {
  const { fixture, user } = setup(205);
  await screen.findByRole("status", { name: "100 loaded tasks" });
  expect(fixture.requests.filter(r => r.path.endsWith("/table/rows"))).toHaveLength(1);
  await user.click(screen.getByRole("button", { name: "Next page" }));
  await screen.findByRole("status", { name: "200 loaded tasks" });
  expect(screen.getByText(/^Graph limit reached/)).toBeVisible();
  expect(screen.getByText(/^Graph limit reached/)).toHaveTextContent("Showing what this window loaded: 200 tasks, 0 runs.");
  expect(screen.queryByRole("button", { name: "Next page" })).not.toBeInTheDocument();
  const membership = fixture.requests.filter(r => r.path.endsWith("/table/rows"));
  expect(membership).toHaveLength(2);
  expect(membership[0]?.body).toMatchObject({ page: { limit: 100 }, group: { kind: "none" }, hierarchy: { enabled: false }, parent_id: null });
  expect(membership[1]?.body).toMatchObject({ page: { cursor: "page-2" } });
  expect(fixture.requests.filter(r => r.path === "/api/issues/trajectory").at(-1)?.body?.issue_ids).toHaveLength(200);
});
it("immediately removes a selected denied comment and aliased trigger content while refresh fails", async () => {
  const { fixture, client, user } = setup();
  await user.click(await screen.findByRole("button", { name: "Expand runs Synthetic parent" }));
  await user.click(await screen.findByRole("button", { name: "Record details Synthetic execution" }));
  await screen.findByTestId("trajectory-run-purpose");
  expect(screen.getByTestId("trajectory-run-purpose")).toHaveTextContent("Private trigger body");
  fixture.fail(503);
  act(() => denyGraphContent(client, graphScope, [`comment:${graphId(40)}`]));
  await waitFor(() => expect(screen.queryByText("Private trigger body")).not.toBeInTheDocument());
  expect(screen.queryByRole("button", { name: "Synthetic comment" })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "report.txt" })).not.toBeInTheDocument();
  await openInput(user);
  expect(screen.getByText("Saved instructions")).toBeVisible();
});
it("scope change clears selected detail and expansion before the new account loads", async () => {
  const { fixture, user, rerender } = setup();
  await user.click(await screen.findByRole("button", { name: "Expand runs Synthetic parent" }));
  await user.click(await screen.findByRole("button", { name: "Record details Synthetic execution" }));
  await screen.findByTestId("trajectory-run-purpose");
  expect(screen.getByTestId("trajectory-run-purpose")).toHaveTextContent("Private trigger body");
  fixture.fail(403);
  rerender({ ...graphScope, accountId: graphId(9) });
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(screen.queryByText("Private trigger body")).not.toBeInTheDocument();
  await screen.findByRole("alert");
  expect(screen.queryByText("SENSITIVE_SERVER_RESPONSE")).not.toBeInTheDocument();
});

it("reopens the same scope with new reads and preserves known comment denials", async () => {
  const { client, user, hide, show } = setup();
  await user.click(await screen.findByRole("button", { name: "Expand runs Synthetic parent" }));
  await user.click(await screen.findByRole("button", { name: "Record details Synthetic execution" }));
  await screen.findByTestId("trajectory-run-purpose");
  expect(screen.getByTestId("trajectory-run-purpose")).toHaveTextContent("Private trigger body");
  act(() => denyGraphContent(client, graphScope, [`comment:${graphId(40)}`]));
  hide();
  show();
  await user.click(await screen.findByRole("button", { name: "Expand runs Synthetic parent" }));
  await user.click(await screen.findByRole("button", { name: "Record details Agent run" }));
  await openInput(user);
  expect(screen.getByText("Saved instructions")).toBeVisible();
  expect(screen.queryByText("Private trigger body")).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Synthetic comment" })).not.toBeInTheDocument();
});

it("shows incomplete coverage when the graph returns no warning text", async () => {
  const { fixture } = setup();
  fixture.list.coverage.complete = false;
  await screen.findByRole("button", { name: "First project" });
  expect(screen.getByText("Some fields or records could not be read")).toBeVisible();
});

it("shows unavailable immediately on native membership removal and retries only on manual refresh", async () => {
  const handlers = new Map<WSEventType, (payload: unknown) => void>();
  const realtime: GraphSubscription = { subscribe: (event, handler) => { handlers.set(event, handler); return () => handlers.delete(event); }, onReconnect: () => () => {} };
  const { fixture, user } = setup(3, realtime);
  await user.click(await screen.findByRole("button", { name: "Expand runs Synthetic parent" }));
  await user.click(await screen.findByRole("button", { name: "Record details Synthetic execution" }));
  await screen.findByTestId("trajectory-run-purpose");
  expect(screen.getByTestId("trajectory-run-purpose")).toHaveTextContent("Private trigger body");
  const priorRequests = fixture.requests.length;
  act(() => handlers.get("member:removed")?.({ workspace_id: graphScope.workspaceId, user_id: graphScope.accountId }));
  await screen.findByText("Unavailable");
  expect(screen.queryByText("Private trigger body")).not.toBeInTheDocument();
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(fixture.requests).toHaveLength(priorRequests);
  await user.click(screen.getByRole("button", { name: "Refresh" }));
  await screen.findByRole("button", { name: "First project" });
  expect(fixture.requests.length).toBeGreaterThan(priorRequests);
});
it("supports keyboard tab navigation and keeps messages lazy until Log is selected", async () => {
  const { fixture, user } = setup();
  await user.click(await screen.findByRole("button", { name: "Expand runs Synthetic parent" }));
  await user.click(await screen.findByRole("button", { name: "Record details Synthetic execution" }));
  const evidence = await screen.findByRole("tab", { name: "Evidence details" });
  expect(fixture.requests.some(request => request.path.endsWith("/messages"))).toBe(false);
  evidence.focus();
  await user.keyboard("{ArrowRight}");
  expect(screen.getByRole("tab", { name: "Logs" })).toHaveFocus();
  await user.keyboard("{Enter}");
  expect(screen.getByRole("tab", { name: "Logs" })).toHaveAttribute("aria-selected", "true");
  await screen.findByText("7 · tool_use · Read");
});

it("resets the graph window and cancels old Issue reads when search changes", async () => {
  const { fixture, user } = setup();
  await user.click(await screen.findByRole("button", { name: "Expand runs Synthetic parent" }));
  await user.click(await screen.findByRole("button", { name: "Record details Synthetic execution" }));
  await screen.findByTestId("trajectory-run-purpose");
  expect(screen.getByTestId("trajectory-run-purpose")).toHaveTextContent("Private trigger body");
  const search = screen.getByLabelText("Search tasks", { selector: "input" });
  fireEvent.change(search, { target: { value: "other" } });
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  await screen.findByText("Synthetic other");
  expect(screen.queryByText("Synthetic parent")).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Refresh" }));
  await waitFor(() => expect(fixture.requests.filter(request => request.path === `/api/issues/${graphId(10)}/trajectory`)).toHaveLength(1));
});
it("shows native day/time, models, duration and stage without adding a Stage container", async () => {
  const { user } = setup();
  await user.click(await screen.findByRole("button", { name: "Expand runs Synthetic parent" }));
  const title = await screen.findByText("Synthetic execution");
  const run = title.closest("[data-node-id]")! as HTMLElement;
  const time = run.querySelector("time");
  expect(time).toHaveAttribute("dateTime", "2026-11-19T00:00:00Z");
  expect(time?.textContent).toMatch(/^\d{2}:\d{2}$/);
  expect(screen.getByText("11/19/2026")).toBeVisible();
  expect(within(run).getByText("Synthetic model")).toBeVisible();
  expect(within(run).getByText("1m 5s")).toBeVisible();
  expect(screen.getByText("Stage 2")).toBeVisible();
  expect(screen.queryByRole("button", { name: "Stage 2" })).not.toBeInTheDocument();
});

it("retains a cross-project matching parent as local context without mixing project counts", async () => {
  const { fixture, user } = setup();
  fixture.list.nodes[1]!.details.project_id = graphId(31);
  fixture.list.nodes[1]!.details.project_title = "Second project";
  await screen.findByText("Synthetic parent");
  expect(screen.queryByText("Synthetic child")).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Second project" }));
  expect(screen.getAllByText("Synthetic child")).toHaveLength(1);
  expect(screen.getAllByText("Synthetic parent")).toHaveLength(2);
  const first = screen.getByRole("group", { name: "First project · Current window" }).closest("section")!;
  const second = screen.getByRole("group", { name: "Second project · Current window" }).closest("section")!;
  expect(within(first).queryByText("Context issues")).not.toBeInTheDocument();
  expect(within(second).getByText("Context issues")).toBeVisible();
  expect(within(first).getByRole("button", { name: "First project" })).toHaveTextContent("1 task");
  expect(within(second).getByRole("button", { name: "Second project" })).toHaveTextContent("2 tasks");
});

it.each([
  { priorities: ["none"] },
  { advanced: { match: "all", children: [{ field: "priorities", values: ["none"] }] } },
] as IssueTableQuerySpec["filters"][])("retains completed cross-project matching ancestors for filter %j", async filters => {
  const { user, fixture, rerenderQuery } = setup(2, undefined, undefined, value => {
    value.list.nodes[0]!.status = "done"; value.list.nodes[1]!.status = "done";
    value.list.nodes[1]!.details.project_id = graphId(31);
    value.list.nodes[1]!.details.project_title = "Child project";
  });
  await screen.findByText("Synthetic parent");
  rerenderQuery({ ...graphQuery, filters });
  await screen.findByRole("group", { name: "Child project · Current window" });
  expect(fixture.requests.filter(request => request.path === "/api/issues/table/rows").at(-1)?.body?.query).toMatchObject({ filters });
  await user.click(screen.getByRole("button", { name: "Child project" }));
  const childScene = screen.getByRole("group", { name: "Child project · Current window" }).closest("section")!;
  expect(within(childScene).getByText("Synthetic parent")).toBeVisible();
  expect(within(childScene).getByText("Synthetic child")).toBeVisible();
  expect(within(childScene).getByText("Context issues")).toBeVisible();
  expect(within(childScene).getByText("Stage 2")).toBeVisible();
  const summary = within(screen.getByRole("group", { name: "Child project · Current window" }));
  expect(summary.getByRole("button", { name: "Running 0" })).toBeVisible();
  expect(summary.getByRole("button", { name: "Blocked 0" })).toBeVisible();
  expect(summary.getByRole("button", { name: "Awaiting review 0" })).toBeVisible();
  expect(within(childScene).getByRole("button", { name: "Child project" })).toHaveTextContent("1 task");
});

it("reads a cold Run link through its validated known Issue owner without reading messages", async () => {
  const { fixture, user } = setup(3, undefined, { requested: true, selection: { kind: "run", id: graphId(20), issueId: graphId(10) } });
  await screen.findByRole("dialog", { name: "Run · 00000020" });
  await openInput(user);
  expect(screen.getByText("Saved instructions")).toBeVisible();
  expect(fixture.requests.filter(request => request.path.endsWith("/messages"))).toHaveLength(0);
  expect(fixture.requests.filter(request => /^\/api\/issues\/[^/]+\/trajectory$/.test(request.path)).map(request => request.path)).toEqual([`/api/issues/${graphId(10)}/trajectory`]);
});
it("offers native fallback for an unresolved old link without unbounded owner lookup", async () => {
  const { fixture } = setup(3, undefined, { requested: true, selection: { kind: "comment", id: graphId(40) } });
  await screen.findByText("Unavailable");
  expect(screen.getByRole("link", { name: "Back to list" })).toHaveAttribute("href", "/synthetic/issues");
  expect(fixture.requests.map(request => request.path)).toEqual(["/api/issues/table/rows", "/api/issues/trajectory"]);
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
});
it("rejects a cold link whose returned native Run belongs to a different Issue", async () => {
  setup(3, undefined, { requested: true, selection: { kind: "run", id: graphId(20), issueId: graphId(10) } }, fixture => { fixture.run.issue_id = graphId(11); });
  await screen.findByText("Unavailable");
  expect(screen.queryByText("Private trigger body")).not.toBeInTheDocument();
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
});

it("expands a Run's returned artifact count without additional reads and retains its output in native detail", async () => {
  const { fixture, user } = setup();
  await user.click(await screen.findByRole("button", { name: "Expand runs Synthetic parent" }));
  const toggle = await screen.findByRole("button", { name: "1 · Artifact" });
  expect(screen.queryByText("Synthetic comment")).not.toBeInTheDocument();
  await user.click(toggle);
  expect(screen.queryByText("Output")).not.toBeInTheDocument();
  expect(screen.getByText("report.txt")).toBeVisible();
  expect(fixture.requests.filter(request => request.path.endsWith("/messages"))).toHaveLength(0);
  expect(fixture.requests.filter(request => /^\/api\/issues\/[^/]+\/trajectory$/.test(request.path))).toHaveLength(1);
  await user.click(screen.getByRole("button", { name: "Record details Synthetic execution" }));
  const dialog = await screen.findByRole("dialog", { name: "Run · 00000020" });
  const fullOutput = within(dialog).getByTestId("trajectory-output-preview");
  await user.click(within(fullOutput.closest("details")!).getByText("Read more"));
  expect(fullOutput).toBeVisible();
  expect(within(dialog).getByRole("button", { name: "Synthetic comment" })).toBeVisible();
  await user.keyboard("{Escape}");
  await user.click(screen.getByRole("button", { name: "Collapse runs Synthetic parent" }));
  expect(screen.queryByText("report.txt")).not.toBeInTheDocument();
});
it("keeps invalid timestamps readable as unknown and leaves absent duration empty", async () => {
  const { user } = setup(3, undefined, undefined, fixture => {
    fixture.run.created_at = "INVALID_TIMESTAMP";
    fixture.run.details.started_at = "INVALID_START";
    fixture.run.details.completed_at = null;
  });
  await user.click(await screen.findByRole("button", { name: "Expand runs Synthetic parent" }));
  const title = await screen.findByText("Synthetic execution");
  const run = title.closest("[data-node-id]")! as HTMLElement;
  expect(run.querySelector("time")).toBeNull();
  expect(within(run).getAllByText("Unknown")).toHaveLength(1);
  expect(run.querySelector('[data-meta-column="time"]')).toBeEmptyDOMElement();
  expect(within(run).queryByText(/INVALID/)).not.toBeInTheDocument();
});

it("opens and scrolls a requested high log sequence into view after native messages load", async () => {
  const scroll = vi.fn();
  vi.spyOn(HTMLElement.prototype, "scrollIntoView").mockImplementation(scroll);
  const { user } = setup(3, undefined, { requested: true, selection: { kind: "run", id: graphId(20), issueId: graphId(10), seq: 9999 } }, fixture => {
    const fetch = fixture.fetch;
    fixture.fetch = async (input, init) => String(input).endsWith("/messages") ? Response.json([{ task_id: graphId(20), issue_id: graphId(10), seq: 9999, type: "text", content: "High stored sequence" }]) : fetch(input, init);
  });
  const dialog = await screen.findByRole("dialog", { name: "Run · 00000020" });
  await user.click(within(dialog).getByRole("tab", { name: "Logs" }));
  const summary = await within(dialog).findByText("9999 · text");
  expect(summary.closest("details")).toHaveAttribute("open");
  expect(scroll).toHaveBeenCalledWith({ block: "nearest" });
});

it("presents native relationship order after the graph merge orders its identities", async () => {
  const { user } = setup(3, undefined, undefined, fixture => {
    const fetch = fixture.fetch;
    fixture.fetch = async (input, init) => {
      const response = await fetch(input, init);
      if (!String(input).endsWith(`/issues/${graphId(10)}/trajectory`)) return response;
      const graph = await response.json();
      return Response.json({ ...graph, edges: [
        { id: "z-comment", from: fixture.run.id, to: fixture.comment.id, kind: "comment" },
        { id: "a-artifact", from: fixture.run.id, to: fixture.attachment.id, kind: "artifact" },
      ] });
    };
  });
  await user.click(await screen.findByRole("button", { name: "Expand runs Synthetic parent" }));
  await user.click(await screen.findByRole("button", { name: "Record details Synthetic execution" }));
  const dialog = await screen.findByRole("dialog", { name: "Run · 00000020" });
  const relationList = within(dialog).getByRole("heading", { name: "Recorded relationships" }).parentElement!;
  expect(within(relationList).getAllByRole("button").map(button => button.textContent)).toEqual([
    "→ comment · Comment · Synthetic comment", "→ artifact · Attachment · report.txt",
  ]);
});

it("returns focus to the visible main Run after opening its transparent rail hit", async () => {
  const { user } = setup();
  await user.click(await screen.findByRole("button", { name: "Expand runs Synthetic parent" }));
  const main = await screen.findByRole("button", { name: "Record details Synthetic execution" });
  await user.click(screen.getAllByRole("button", { name: "Synthetic execution" })[0]!);
  await screen.findByRole("dialog", { name: "Run · 00000020" });
  await user.keyboard("{Escape}");
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(main).toHaveFocus();
  expect(main).toHaveAttribute("aria-pressed", "true");
});

it("retains a safe selected Issue pointer after close without observing or refetching its hidden owner graph", async () => {
  const { user, client, fixture } = setup(3, undefined, { requested: true, selection: { kind: "issue", id: graphId(10), issueId: graphId(10) } });
  const dialog = await screen.findByRole("dialog", { name: "Synthetic parent" });
  await within(dialog).findByRole("button", { name: "Synthetic comment" });
  await user.keyboard("{Escape}");
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  const queryKey = graphIssueOptions(graphScope, graphId(10)).queryKey;
  expect(client.getQueryCache().find({ queryKey })?.getObserversCount()).toBe(0);
  const reads = fixture.requests.filter(request => request.path === `/api/issues/${graphId(10)}/trajectory`).length;
  await act(async () => { await client.invalidateQueries({ queryKey }); });
  expect(fixture.requests.filter(request => request.path === `/api/issues/${graphId(10)}/trajectory`)).toHaveLength(reads);
  expect(screen.queryByText("Private trigger body")).not.toBeInTheDocument();
});

it.each([19, 0])("keeps the authorized loaded Run count %s while current query status remains authoritative", async count => {
  const { user, fixture, client } = setup(1, undefined, undefined, value => {
    delete value.list.nodes[0]!.details.run_count;
    value.list.nodes[0]!.status = "in_review";
    const original = value.fetch;
    value.fetch = async (input, init) => {
      const response = await original(input, init);
      if (new URL(String(input), "https://synthetic.invalid").pathname !== `/api/issues/${graphId(10)}/trajectory`) return response;
      const own = await response.json();
      own.nodes[0] = { ...own.nodes[0], status: "done", details: { ...own.nodes[0].details, run_count: count, context_only: true } };
      if (count === 0) own.nodes = own.nodes.filter((n: { kind: string }) => n.kind === "issue");
      return Response.json(own);
    };
  });
  const parent = (await screen.findByText("Synthetic parent")).closest<HTMLElement>("[data-node-id]")!;
  expect(within(parent).getByText("Unknown")).toBeVisible();
  await user.click(screen.getByRole("button", { name: "Expand runs Synthetic parent" }));
  await within(parent).findByText(`${count} runs`);
  expect(parent).toHaveAttribute("data-current-state", "review");
  expect(within(parent).queryByText("Context issues")).not.toBeInTheDocument();
  fixture.list.nodes[0]!.status = "blocked";
  await act(async () => { await client.refetchQueries({ queryKey: windowListKey([graphId(10)]), exact: true }); });
  await waitFor(() => expect(parent).toHaveAttribute("data-current-state", "blocked"));
  expect(within(parent).getByText(`${count} runs`)).toBeVisible();
  expect(screen.getByRole("button", { name: "Blocked 1" })).toBeVisible();
});

it("distinguishes a missing Run count from the native zero count", async () => {
  setup(3, undefined, undefined, fixture => { delete fixture.list.nodes[0]!.details.run_count; });
  const title = await screen.findByText("Synthetic parent");
  const parent = title.closest("[data-node-id]")! as HTMLElement;
  expect(within(parent).getByText("Unknown")).toBeVisible();
  expect(within(parent).queryByText("0 runs")).not.toBeInTheDocument();
});

it("renders a native 16000-character Unicode preview without a second clip or false truncation", async () => {
  const { user } = setup(3, undefined, undefined, fixture => {
    fixture.run.details.output = "😀".repeat(16000);
    fixture.run.details.output_truncated = false;
  });
  await user.click(await screen.findByRole("button", { name: "Expand runs Synthetic parent" }));
  await user.click(await screen.findByRole("button", { name: "Record details Synthetic execution" }));
  const dialog = await screen.findByRole("dialog", { name: "Run · 00000020" });
  const preview = within(dialog).getByTestId("trajectory-output-preview").textContent!;
  expect(preview).toHaveLength(32000);
  expect(Array.from(preview)).toHaveLength(16000);
  expect(within(dialog).queryByText("Output preview is truncated. Open the log to read stored messages.")).not.toBeInTheDocument();
});


it("preserves the same focused input during Chinese IME composition and combines page search with filters", async () => {
  const { fixture } = setup();
  await screen.findByText("Synthetic parent");
  const input = screen.getByRole("textbox", { name: "Search tasks" });
  input.focus();
  fireEvent.compositionStart(input);
  fireEvent.change(input, { target: { value: "中" } });
  expect(screen.getByRole("textbox", { name: "Search tasks" })).toBe(input);
  expect(input).toHaveFocus();
  fireEvent.change(input, { target: { value: "中文" } });
  fireEvent.compositionEnd(input, { data: "中文" });
  expect(input).toHaveValue("中文");
  // Base UI menus take pointer input through fireEvent, as in the issues page menu tests.
  fireEvent.click(screen.getByRole("button", { name: "Filter" }));
  fireEvent.click(await screen.findByRole("menuitem", { name: /^Priority/ }));
  fireEvent.click(await screen.findByRole("menuitemcheckbox", { name: "Urgent" }));
  expect(input).toHaveValue("中文");
  await waitFor(() => expect(fixture.requests.some(request => (request.body?.query as IssueTableQuerySpec | undefined)?.search === "中文" && (request.body?.query as IssueTableQuerySpec | undefined)?.filters?.priorities?.[0] === "urgent")).toBe(true));
  fireEvent.change(input, { target: { value: "" } });
  expect(input).toHaveValue("");
  fireEvent.change(input, { target: { value: "SYN-10" } });
  expect(input).toHaveValue("SYN-10");
  fireEvent.click(await screen.findByRole("menuitem", { name: "Reset all filters" }));
  expect(input).toHaveValue("SYN-10");
  expect(input).toHaveAttribute("data-readonly-preview", "search");
});

it("isolates a late list result when different filters return the same Issue IDs", async () => {
  const pending: Array<{ signal?: AbortSignal | null; resolve: (response: Response) => void }> = [];
  const { fixture, rerenderQuery } = setup(1, undefined, undefined, value => {
    const original = value.fetch;
    value.fetch = (input, init) => new URL(String(input), "https://synthetic.invalid").pathname === "/api/issues/trajectory"
      ? new Promise<Response>(resolve => pending.push({ signal: init?.signal, resolve })) : original(input, init);
  });
  await waitFor(() => expect(pending).toHaveLength(1));
  rerenderQuery({ ...graphQuery, filters: { priorities: ["urgent"] } });
  await waitFor(() => expect(pending).toHaveLength(2));
  expect(pending[0]!.signal?.aborted).toBe(true);
  const response = (title: string) => Response.json({ ...fixture.list, nodes: fixture.list.nodes.map(node => ({ ...node, title })) });
  await act(async () => pending[1]!.resolve(response("Current filtered task")));
  await screen.findByText("Current filtered task");
  await act(async () => pending[0]!.resolve(response("Stale prior task")));
  expect(screen.queryByText("Stale prior task")).not.toBeInTheDocument();
  expect(screen.getByText("Current filtered task")).toBeVisible();
});

it("cancels a selected old Issue read before a filter change can restore its detail", async () => {
  let release!: (response: Response) => void;
  let signal: AbortSignal | null | undefined;
  const { fixture, user, rerenderQuery } = setup(1, undefined, undefined, value => {
    const original = value.fetch;
    value.fetch = (input, init) => new URL(String(input), "https://synthetic.invalid").pathname === `/api/issues/${graphId(10)}/trajectory`
      ? new Promise<Response>(resolve => { release = resolve; signal = init?.signal; }) : original(input, init);
  });
  await user.click(await screen.findByRole("button", { name: "Record details Synthetic parent" }));
  await waitFor(() => expect(release).toBeTypeOf("function"));
  rerenderQuery({ ...graphQuery, filters: { priorities: ["urgent"] } });
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(signal?.aborted).toBe(true);
  await act(async () => release(Response.json({ ...fixture.list, nodes: [
    { ...fixture.list.nodes[0]!, details: { ...fixture.list.nodes[0]!.details, run_count: 19 } }, fixture.run,
  ] })));
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(screen.queryByText("Private trigger body")).not.toBeInTheDocument();
  expect(screen.queryByText("19 runs")).not.toBeInTheDocument();
});

it("does not restore an Issue or its count from an own read after it leaves the current query graph", async () => {
  let release!: (response: Response) => void;
  let missing = false;
  const { fixture, client, user } = setup(2, undefined, undefined, value => {
    const original = value.fetch;
    value.fetch = (input, init) => {
      const path = new URL(String(input), "https://synthetic.invalid").pathname;
      if (path === `/api/issues/${graphId(10)}/trajectory`) return new Promise<Response>(resolve => { release = resolve; });
      if (path === "/api/issues/trajectory" && missing) return Promise.resolve(Response.json({ ...value.list, nodes: [value.list.nodes[1]!] }));
      return original(input, init);
    };
  });
  await user.click(await screen.findByRole("button", { name: "Expand runs Synthetic parent" }));
  await waitFor(() => expect(release).toBeTypeOf("function"));
  missing = true;
  await act(async () => { await client.refetchQueries({ queryKey: windowListKey([graphId(10), graphId(11)]), exact: true }); });
  await waitFor(() => expect(screen.queryByText("Synthetic parent")).not.toBeInTheDocument());
  await act(async () => release(Response.json({ ...fixture.list, nodes: [
    { ...fixture.list.nodes[0]!, details: { ...fixture.list.nodes[0]!.details, run_count: 19 } }, fixture.run,
  ] })));
  expect(screen.queryByText("Synthetic parent")).not.toBeInTheDocument();
  expect(screen.queryByText("Synthetic execution")).not.toBeInTheDocument();
  expect(screen.queryByText("19 runs")).not.toBeInTheDocument();
});


it("does not reuse cached graph counts or titles for a new filter with identical members", async () => {
  let hold = false;
  let release!: (response: Response) => void;
  const { fixture, rerenderQuery } = setup(1, undefined, undefined, value => {
    const original = value.fetch;
    value.fetch = (input, init) => hold && new URL(String(input), "https://synthetic.invalid").pathname === "/api/issues/trajectory"
      ? new Promise<Response>(resolve => { release = resolve; }) : original(input, init);
  });
  await screen.findByRole("status", { name: "1 loaded task" });
  expect(screen.getByText("Synthetic parent")).toBeVisible();
  hold = true;
  rerenderQuery({ ...graphQuery, filters: { priorities: ["urgent"] } });
  await waitFor(() => expect(release).toBeTypeOf("function"));
  expect(screen.queryByText("Synthetic parent")).not.toBeInTheDocument();
  expect(screen.queryByRole("status", { name: "1 loaded task" })).not.toBeInTheDocument();
  await act(async () => release(Response.json({ ...fixture.list, nodes: fixture.list.nodes.map(node => ({ ...node, title: "Fresh filtered result" })) })));
  await screen.findByText("Fresh filtered result");
});


it.each([false, true])("retains real-read recovery proof when returning to a manually repaired failed query (directory pause: %s)", async directoryPause => {
  let mode: "success" | "failed" | "held" = "success";
  const release: Partial<Record<"membership" | "list", () => void>> = {};
  const { fixture, client, rerenderQuery, rerenderReady, user } = setup(1, undefined, undefined, value => {
    value.list.nodes[0]!.status = "blocked";
    const original = value.fetch;
    value.fetch = (input, init) => {
      const path = new URL(String(input), "https://synthetic.invalid").pathname;
      const read = path === "/api/issues/table/rows" ? "membership" : path === "/api/issues/trajectory" ? "list" : null;
      if (read === "list" && mode === "failed") return Promise.resolve(new Response("Synthetic failure", { status: 500 }));
      if (read && mode === "held") return new Promise<Response>(resolve => { release[read] = () => { void original(input, init).then(resolve); }; });
      return original(input, init);
    };
  });
  const summary = await screen.findByRole("group", { name: "First project · Current window" });
  await user.click(within(summary).getByRole("button", { name: "Blocked 1" }));
  const listKey = windowListKey([graphId(10)]);
  mode = "failed";
  await act(async () => { await client.refetchQueries({ queryKey: listKey, exact: true }); });
  await within(summary).findByText("Summary unavailable");
  act(() => denyGraphContent(client, graphScope, [`comment:${graphId(40)}`]));
  expect(client.getQueryState(listKey)?.status).toBe("success");
  mode = "success";
  rerenderQuery({ ...graphQuery, filters: { priorities: ["high"] } });
  await screen.findByRole("status", { name: "1 loaded task" });
  mode = "held";
  if (directoryPause) rerenderReady(false);
  rerenderQuery(graphQuery);
  if (directoryPause) rerenderReady(true);
  const returned = await screen.findByRole("group", { name: "First project · Current window" });
  expect(within(returned).getByText("Summary unavailable")).toBeVisible();
  await waitFor(() => { expect(release.membership).toBeTypeOf("function"); expect(release.list).toBeTypeOf("function"); });
  act(() => client.setQueryData(listKey, fixture.list));
  expect(within(returned).getByText("Summary unavailable")).toBeVisible();
  await act(async () => release.membership!());
  expect(within(returned).getByText("Summary unavailable")).toBeVisible();
  await act(async () => release.list!());
  await within(returned).findByRole("button", { name: "Blocked 1" });
  expect(document.querySelector("[data-summary-target=true]")).toBeNull();
});

it("does not update another Graph window during scope-cache registration", async () => {
  const error = vi.spyOn(console, "error").mockImplementation(() => {});
  const { client } = setup();
  await screen.findByText("Synthetic parent");
  act(() => closeGraphScope(client, graphScope));
  await screen.findByRole("alert");
  const second = renderWithI18n(<QueryClientProvider client={client}><NavigationProvider value={adapter}><GraphTrajectoryView scope={graphScope} query={graphQuery} /></NavigationProvider></QueryClientProvider>);
  await within(second.container).findByText("Synthetic parent");
  expect(JSON.stringify(error.mock.calls)).not.toContain("Cannot update a component");
});

it("opens a cold seq link on its log, locates the message once and keeps the reader's place on refetch", async () => {
  const scroll = vi.fn();
  vi.spyOn(HTMLElement.prototype, "scrollIntoView").mockImplementation(scroll);
  const { client } = setup(3, undefined, { requested: true, selection: { kind: "run", id: graphId(20), issueId: graphId(10), seq: 7 } }, fixture => {
    const fetch = fixture.fetch;
    fixture.fetch = async (input, init) => String(input).endsWith("/messages") ? Response.json([{ task_id: graphId(20), issue_id: graphId(10), seq: 7, type: "text", content: "Linked message" }]) : fetch(input, init);
  });
  const dialog = await screen.findByRole("dialog", { name: "Run · 00000020" });
  expect(within(dialog).getByRole("tab", { name: "Logs" })).toHaveAttribute("aria-selected", "true");
  await within(dialog).findByText("7 · text");
  const located = scroll.mock.calls.filter(([options]) => JSON.stringify(options) === JSON.stringify({ block: "nearest" })).length;
  expect(located).toBe(1);
  await act(async () => { await client.refetchQueries({ queryKey: ["trajectory-graph", graphScope.accountId, graphScope.workspaceId, "messages"] }); });
  expect(scroll.mock.calls.filter(([options]) => JSON.stringify(options) === JSON.stringify({ block: "nearest" }))).toHaveLength(located);
});

it("reveals a cold Run link's row under its expanded issue", async () => {
  const scroll = vi.fn();
  vi.spyOn(HTMLElement.prototype, "scrollIntoView").mockImplementation(scroll);
  setup(3, undefined, { requested: true, selection: { kind: "run", id: graphId(20), issueId: graphId(10) } });
  await screen.findByRole("dialog", { name: "Run · 00000020" });
  await waitFor(() => expect(document.querySelector(`[data-node-id="run:${graphId(20)}"]`)).not.toBeNull());
  expect(screen.getByRole("button", { name: "Collapse runs Synthetic parent", hidden: true })).toHaveAttribute("aria-expanded", "true");
  await waitFor(() => expect(scroll).toHaveBeenCalledWith({ block: "center", behavior: "auto" }));
});

it("follows an in-page link to another record without moving the reader's scroll position", async () => {
  const scroll = vi.fn();
  vi.spyOn(HTMLElement.prototype, "scrollIntoView").mockImplementation(scroll);
  const fixture = graphFixture(3);
  vi.stubGlobal("fetch", fixture.fetch);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } }); clients.push(client);
  const select = vi.fn();
  const view = (selection: import("@multica/core/trajectory/web-entry").TrajectorySelection | null) => <QueryClientProvider client={client}><NavigationProvider value={adapter}>
    <GraphTrajectoryView scope={graphScope} query={graphQuery} navigation={{ requested: true, selection, error: null, select }} /></NavigationProvider></QueryClientProvider>;
  const result = renderWithI18n(view(null));
  await screen.findByText("Synthetic parent");
  result.rerender(view({ kind: "run", id: graphId(20), issueId: graphId(10) }));
  await screen.findByRole("dialog", { name: "Run · 00000020" });
  result.rerender(view({ kind: "comment", id: graphId(40), issueId: graphId(10) }));
  await waitFor(() => expect(screen.getByRole("dialog")).toHaveAccessibleName(/00000040/));
  expect(scroll).not.toHaveBeenCalledWith({ block: "center", behavior: "auto" });
  expect(select).not.toHaveBeenCalled();
});

it("reports one expanded task's own truncation on that task with its native page, not as a window limit", async () => {
  const { user } = setup(3, undefined, undefined, fixture => {
    const fetch = fixture.fetch;
    fixture.fetch = async (input, init) => {
      const response = await fetch(input, init);
      if (new URL(String(input), "https://synthetic.invalid").pathname !== `/api/issues/${graphId(10)}/trajectory`) return response;
      const graph = await response.json();
      return Response.json({ ...graph, coverage: { complete: false, truncated: true, warnings: [] } });
    };
  });
  await user.click(await screen.findByRole("button", { name: "Expand runs Synthetic parent" }));
  const note = await screen.findByText(/too large to load in full/);
  expect(within(note).getByRole("link", { name: "Open task" })).toHaveAttribute("href", "/synthetic/issues/SYN-10");
  expect(screen.queryByText(/^Graph limit reached/)).not.toBeInTheDocument();
});
it("switches between task line and timeline from the header, keeping expanded Runs", async () => {
  const { user } = setup();
  await screen.findByRole("button", { name: "First project" });
  const task = screen.getByRole("tab", { name: "Task line" });
  const time = screen.getByRole("tab", { name: "Timeline" });
  expect(task).toHaveAttribute("aria-selected", "true");
  await user.click(screen.getByRole("button", { name: "Expand runs Synthetic child" }));
  await screen.findByText("Child execution");
  await user.click(time);
  expect(time).toHaveAttribute("aria-selected", "true");
  expect(task).toHaveAttribute("aria-selected", "false");
  expect(screen.getByText("Child execution")).toBeVisible();
  expect(screen.getByRole("button", { name: "Collapse runs Synthetic child" })).toHaveAttribute("aria-expanded", "true");
});
