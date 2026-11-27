import { afterEach, expect, it, vi } from "vitest";
import { cleanup, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithI18n } from "../../../../test/i18n";
import { GraphTimeline } from "./graph-timeline";
import { trajectoryTitleText } from "@multica/core/trajectory/readable-text";
import { graphFixture, graphId, graphNode } from "./graph-fixture";

const plainRunSummary = (value: unknown) => Array.from(trajectoryTitleText(value).replace(/\s+/g, " ")).slice(0, 96).join("");

afterEach(cleanup);

it("draws a cross-Issue Run call only while both endpoints and native trigger evidence are present, without changing the row order", () => {
  const { list, run, comment } = graphFixture(2);
  const childRun = graphNode("run", 21, 11, "Child execution");
  run.status = "running";
  childRun.status = "blocked";
  const [parent, child] = list.nodes;
  const laterParent = { ...run, id: "run:" + graphId(23), run_id: graphId(23), title: "Later parent Run", created_at: "2026-11-19T00:03:00Z" };
  child!.created_at = "2026-11-19T00:01:00Z";
  childRun.created_at = "2026-11-19T00:02:00Z";
  comment.issue_id = child!.issue_id;
  comment.details.author_type = "agent";
  Object.assign(childRun.details, { run_call: { comment_id: graphId(40), source_issue_id: graphId(10) }, delegated_from: run.run_id, trigger_comment_id: comment.details.comment_id,
    trigger_evidence_kind: "comment", trigger_evidence_ref_id: comment.details.comment_id });
  const graph = { ...list, nodes: [...list.nodes, run, laterParent, childRun, comment] };
  const render = (order: "task" | "time", expandedIssues: Set<string>, data = graph) => <GraphTimeline
    graph={data} projectKey={graphId(30)} issueIds={[parent!.issue_id, child!.issue_id]} expandedIssues={expandedIssues} expandedRuns={new Set()}
    order={order} toggleIssue={vi.fn()} toggleRun={vi.fn()} select={vi.fn()}/>;
  const expanded = new Set([parent!.issue_id, child!.issue_id]);
  const { rerender } = renderWithI18n(render("task", expanded));
  const rows = () => [...screen.getByTestId("trajectory-timeline").querySelectorAll("[data-node-id]")].map(row => row.getAttribute("data-node-id"));
  const arrow = () => screen.getByTestId("trajectory-run-link");
  expect(rows()).toEqual([parent!.id, run.id, laterParent.id, child!.id, childRun.id]);
  expect(arrow()).toHaveAttribute("data-from", run.id);
  expect(arrow()).toHaveAttribute("data-to", childRun.id);
  expect(screen.getByRole("img", { name: "Synthetic execution · 00000020 → Child execution · 00000021" })).toBe(arrow());
  const sourceRow = screen.getByText(run.title).closest<HTMLElement>("[data-node-id]");
  expect(sourceRow?.querySelector(".trajectory-marker")).toHaveAttribute("data-marker-column", "0");
  expect(sourceRow?.style.getPropertyValue("--trajectory-owner")).toMatch(/^var\(--trajectory-identity-/);
  expect(arrow().style.color).toBe("var(--trajectory-border)");
  expect(screen.getByTestId("trajectory-rail").querySelector('path[stroke="var(--trajectory-border)"]')).toBeInTheDocument();
  const start = arrow().querySelector("circle");
  expect(start).toHaveAttribute("fill", "currentColor");
  expect(arrow().querySelector("path")).toHaveAttribute("stroke", "currentColor");
  expect(arrow().querySelector("polygon")).toHaveAttribute("fill", "currentColor");
  const firstPath = arrow().querySelector("path")?.getAttribute("d");
  expect(firstPath?.startsWith(`M ${start?.getAttribute("cx")} ${start?.getAttribute("cy")} V `)).toBe(true);
  rerender(render("time", expanded));
  expect(rows()).toEqual([parent!.id, run.id, child!.id, childRun.id, laterParent.id]);
  expect(arrow()).toHaveAttribute("data-from", run.id);
  expect(arrow().querySelector("path")?.getAttribute("d")).not.toBe(firstPath);
  rerender(render("time", new Set([parent!.issue_id])));
  expect(screen.queryByTestId("trajectory-run-link")).not.toBeInTheDocument();
  rerender(render("time", new Set([child!.issue_id])));
  expect(screen.queryByTestId("trajectory-run-link")).not.toBeInTheDocument();
  rerender(render("time", expanded));
  expect(arrow().querySelector("circle")).toBeInTheDocument();
  rerender(render("time", expanded, { ...graph, nodes: graph.nodes.filter(node => node !== comment) }));
  expect(screen.queryByTestId("trajectory-run-link")).not.toBeInTheDocument();
  expect(screen.getByTestId("trajectory-rail").querySelector("path[data-parent]")).toBeInTheDocument();
});

function renderTimeline(expanded = false, status = "completed", finding = false) {
  const fixture = graphFixture();
  fixture.run.status = status;
  const select = vi.fn(), toggleRun = vi.fn(), toggleIssue = vi.fn();
  renderWithI18n(<GraphTimeline graph={{ ...fixture.list, nodes: [...fixture.list.nodes, fixture.run, fixture.comment, fixture.attachment], findings: finding ? [{ id: "missing-input", node_id: fixture.run.id, code: "missing_input_snapshot", severity: "gap", message: "Saved input snapshot is missing" }] : [] }}
    projectKey={graphId(30)} issueIds={[graphId(10), graphId(11)]}
    expandedIssues={new Set(expanded ? [graphId(10)] : [])} expandedRuns={new Set()}
    toggleIssue={toggleIssue} toggleRun={toggleRun} select={select} />);
  return { fixture, select, toggleRun, toggleIssue, user: userEvent.setup() };
}

it("uses the scene's issue and run rhythm rather than one fixed row height", () => {
  renderTimeline(true);
  const issue = screen.getByText("Synthetic parent").closest("[data-node-id]")!;
  const run = screen.getByText("Synthetic execution").closest("[data-node-id]")!;
  expect(issue).toHaveStyle({ height: "44px" });
  expect(within(issue as HTMLElement).getByText("1 run")).toBeInTheDocument();
  expect(run).toHaveStyle({ height: "32px" });
  expect(run.querySelector('[data-meta-column="model"]')).toHaveTextContent("Synthetic model");
  expect(run.querySelector('[data-meta-column="time"]')).toHaveTextContent("1m 5s");
  expect(within(run as HTMLElement).queryByText("completed")).not.toBeInTheDocument();
});

it("opens the Run from its content and expands returned artifacts from the separate count control", async () => {
  const { fixture, user, select, toggleRun } = renderTimeline(true);
  await user.click(screen.getByRole("button", { name: "Record details Synthetic execution" }));
  expect(select).toHaveBeenCalledWith(fixture.run.id);
  const count = screen.getByRole("button", { name: "1 · Artifact" });
  expect(count).toHaveAttribute("aria-expanded", "false");
  expect(screen.queryByText("Output")).not.toBeInTheDocument();
  await user.click(count);
  expect(toggleRun).toHaveBeenCalledWith(fixture.run.run_id);
});

it("keeps issue controls in one aligned content column across forked lanes", () => {
  renderTimeline();
  const rows = [...screen.getByTestId("trajectory-timeline").querySelectorAll<HTMLElement>("[data-node-id]")];
  expect(rows).toHaveLength(2);
  const contentOffsets = rows.map(row => row.querySelector<HTMLElement>("[data-row-content]")?.style.marginLeft);
  expect(contentOffsets[0]).toBeTruthy();
  expect(new Set(contentOffsets).size).toBe(1);
  expect(screen.getByTestId("trajectory-rail").querySelector("path[data-parent]")).toBeInTheDocument();
});

it("uses the compact pending grade and filled start marker for a dispatched native Run", () => {
  renderTimeline(true, "dispatched");
  const run = screen.getByText("Synthetic execution").closest("[data-node-id]")!;
  expect(run).toHaveStyle({ height: "24px" });
  const marker = within(run as HTMLElement).getByRole("img", { name: "Starting" });
  expect(marker.querySelector("svg")).toHaveAttribute("fill", "currentColor");
});

it("uses medium weight for the title of a running task", () => {
  renderTimeline(true, "running");
  expect(screen.getByText("Synthetic execution")).toHaveStyle({ fontWeight: "500" });
});

it("shows a native Run finding in its menu and opens the same record from that indicator", async () => {
  const { user, select, fixture } = renderTimeline(true, "completed", true);
  const finding = screen.getByRole("button", { name: "Input snapshot · Not recorded" });
  expect(finding).toHaveAttribute("title", "Saved input snapshot is missing");
  await user.click(finding);
  expect(select).toHaveBeenCalledWith(fixture.run.id);
});

it("opens the native branch from its rail without changing the current row's owner", async () => {
  const { user, toggleIssue } = renderTimeline();
  await user.click(screen.getAllByRole("button", { name: "Synthetic child" })[0]!);
  expect(toggleIssue).toHaveBeenCalledWith(graphId(11));
});

it("opens a Run from its output rail's native origin", async () => {
  const { user, select, fixture } = renderTimeline(true);
  await user.click(screen.getAllByRole("button", { name: "Synthetic execution" })[0]!);
  expect(select).toHaveBeenCalledWith(fixture.run.id);
});

it.each([false, true])("preserves both cross-project ancestors of a completed match independently of global context_only=%s", contextOnly => {
  const fixture = graphFixture(2);
  fixture.list.nodes[0]!.status = "done";
  fixture.list.nodes[0]!.details.parent_id = graphId(13);
  if (contextOnly) fixture.list.nodes[0]!.details.context_only = true;
  fixture.list.nodes[1]!.status = "done";
  fixture.list.nodes[1]!.details.project_id = graphId(31);
  const grandparent = graphNode("issue", 13, 13, "Completed grandparent", { project_id: graphId(32), ...(contextOnly ? { context_only: true } : {}) });
  grandparent.status = "done";
  renderWithI18n(<GraphTimeline graph={{ ...fixture.list, nodes: [...fixture.list.nodes, grandparent] }}
    projectKey={graphId(31)} issueIds={[graphId(11)]} expandedIssues={new Set()} expandedRuns={new Set()}
    toggleIssue={vi.fn()} toggleRun={vi.fn()} select={vi.fn()}/>);
  expect(screen.getByText("Synthetic parent")).toBeVisible();
  expect(screen.getByText("Completed grandparent")).toBeVisible();
  expect(screen.getByText("Stage 2")).toBeVisible();
  expect(screen.getAllByText("Context issues")).toHaveLength(2);
});

it("shows Chinese current Issue states and completed Run history as distinct inline facts", () => {
  const fixture = graphFixture(2);
  fixture.list.nodes[0]!.status = "in_review";
  fixture.list.nodes[1]!.status = "blocked";
  const running = { ...fixture.run, id: `run:${graphId(22)}`, run_id: graphId(22), status: "running", title: "当前执行" };
  const blocked = { ...fixture.run, id: `run:${graphId(23)}`, run_id: graphId(23), status: "blocked", title: "等待处理" };
  fixture.run.details.models = [];
  renderWithI18n(<GraphTimeline graph={{ ...fixture.list, nodes: [...fixture.list.nodes, fixture.run, running, blocked] }}
    projectKey={graphId(30)} issueIds={[graphId(10), graphId(11)]} expandedIssues={new Set([graphId(10)])} expandedRuns={new Set()}
    toggleIssue={vi.fn()} toggleRun={vi.fn()} select={vi.fn()}/>, { locale: "zh-Hans" });
  const row = (title: string) => screen.getByText(title).closest<HTMLElement>("[data-node-id]")!;
  expect(within(row("Synthetic parent")).getByText("待验收")).toBeVisible();
  expect(within(row("Synthetic child")).getByText("阻塞")).toBeVisible();
  expect(within(row("Synthetic execution")).getByText("执行结束")).toBeVisible();
  expect(within(row("Synthetic execution")).queryByText("待验收")).not.toBeInTheDocument();
  expect(within(row("Synthetic execution")).getByText("未记录模型")).toBeVisible();
  expect(within(row("当前执行")).getByText("运行中")).toBeVisible();
  expect(within(row("等待处理")).getByText("阻塞")).toBeVisible();
  expect(within(row("当前执行")).getByRole("img", { name: "运行中" })).toHaveAttribute("data-tone", "info");
  expect(within(row("等待处理")).getByRole("img", { name: "阻塞" })).toHaveAttribute("data-tone", "danger");
  expect(within(row("Synthetic parent")).getByRole("img", { name: "待验收" })).toHaveAttribute("data-tone", "warning");
});

it("renders native plain summary labels without executable HTML or mention and dangerous links", () => {
  const fixture = graphFixture(1);
  const summary = "[@复核](mention://agent/77777777-7777-4777-8777-777777777777) [验证](javascript:alert(1)) <img src=x onerror=alert(1)><script>window.bad=1</script>结束";
  fixture.run.title = plainRunSummary(summary);
  renderWithI18n(<GraphTimeline graph={{ ...fixture.list, nodes: [...fixture.list.nodes, fixture.run] }}
    projectKey={graphId(30)} issueIds={[graphId(10)]} expandedIssues={new Set([graphId(10)])} expandedRuns={new Set()}
    toggleIssue={vi.fn()} toggleRun={vi.fn()} select={vi.fn()}/>);
  const title = screen.getByText("@复核 验证 结束");
  expect(title).toBeVisible();
  const row = title.closest<HTMLElement>("[data-node-id]")!;
  expect(row.querySelector("a, img, script, [onerror]")).toBeNull();
  expect(row.textContent).not.toMatch(/mention:\/\/|javascript:|window.bad/);
});

it("interleaves sibling sub-Issues' Runs by time in time order, tinted by owner, with titles as the only Issue label", () => {
  const project = { project_id: graphId(30) };
  const minute = (n: number) => `2026-11-19T00:${String(n).padStart(2, "0")}:00Z`;
  const node = (kind: string, n: number, owner: number, title: string, at: number, details: Record<string, unknown> = {}) => ({ ...graphNode(kind, n, owner, title, { ...project, ...details }), created_at: minute(at) });
  const fixture = graphFixture();
  const nodes = [node("issue", 10, 10, "Root", 0), node("issue", 11, 11, "First child", 1, { parent_id: graphId(10) }), node("issue", 12, 12, "Second child", 2, { parent_id: graphId(10) }),
    node("run", 21, 11, "First run A", 3), node("run", 22, 12, "Second run", 4), node("run", 23, 11, "First run B", 5)];
  const order = (value: "task" | "time") => {
    renderWithI18n(<GraphTimeline graph={{ ...fixture.list, nodes }} projectKey={graphId(30)} issueIds={[graphId(10), graphId(11), graphId(12)]} order={value}
      expandedIssues={new Set([graphId(11), graphId(12)])} expandedRuns={new Set()} toggleIssue={vi.fn()} toggleRun={vi.fn()} select={vi.fn()} />);
    const rows = [...screen.getByTestId("trajectory-timeline").querySelectorAll<HTMLElement>("[data-node-id]")];
    const titles = rows.map(row => row.querySelector(".trajectory-title")?.textContent);
    const tint = (title: string) => rows.find(row => row.querySelector(".trajectory-title")?.textContent === title)!.style.getPropertyValue("--trajectory-owner");
    cleanup();
    return { titles, tint };
  };
  expect(order("task").titles).toEqual(["Root", "First child", "First run A", "First run B", "Second child", "Second run"]);
  const time = order("time");
  expect(time.titles).toEqual(["Root", "First child", "Second child", "First run A", "Second run", "First run B"]);
  expect(time.tint("First run A")).toBeTruthy();
  expect(time.tint("First run B")).toBe(time.tint("First run A"));
  expect(time.tint("Second run")).not.toBe(time.tint("First run A"));
  renderWithI18n(<GraphTimeline graph={{ ...fixture.list, nodes }} projectKey={graphId(30)} issueIds={[graphId(10)]} expandedIssues={new Set()} expandedRuns={new Set()} toggleIssue={vi.fn()} toggleRun={vi.fn()} select={vi.fn()} />);
  expect(screen.queryByText("Subtask")).not.toBeInTheDocument();
});
