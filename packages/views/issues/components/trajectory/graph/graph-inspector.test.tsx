import { afterEach, expect, it, vi } from "vitest";
import { cleanup, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { paths } from "@multica/core/paths";
import type { TrajectoryGraph, TrajectoryNode } from "@multica/core/trajectory/graph";
import { NavigationProvider, type NavigationAdapter } from "../../../../navigation";
import { renderWithI18n } from "../../../../test/i18n";
import { GraphInspector } from "./graph-inspector";
import { graphFixture, graphId } from "./graph-fixture";

vi.mock("@multica/core/paths", async importOriginal => ({
  ...await importOriginal<typeof import("@multica/core/paths")>(),
  useWorkspacePaths: () => paths.workspace("synthetic"),
}));

afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

it("keeps a completed Run's native state explicit alongside a historical blocked workflow", () => {
  const fixture = graphFixture(1);
  fixture.run.details.workflow_step = "blocked";
  setup(fixture.run, { ...fixture.list, nodes: [...fixture.list.nodes, fixture.run] });
  expect(screen.getByText("Run · Execution ended")).toBeVisible();
  expect(screen.getByText("Run · Execution ended")).toHaveAttribute("data-slot", "badge");
  expect(screen.getByText("Workflow blocked")).toHaveAttribute("data-slot", "badge");
  expect(screen.queryByText("Run · Blocked workflow")).not.toBeInTheDocument();
});

it("shows returned attachment names and record payloads while preserving readable structural relations", () => {
  const fixture = graphFixture(1);
  const record: TrajectoryNode = { ...fixture.comment, kind: "platform_event", details: { record: { message: "Returned record fact" } }, status: "recorded" };
  setup(record, { ...fixture.list, nodes: [...fixture.list.nodes, record], edges: [{ id: "owner", from: fixture.list.nodes[0]!.id, to: record.id, kind: "contains" }] });
  expect(screen.getByTestId("trajectory-record-content")).toHaveTextContent("Returned record fact");
  expect(screen.getByRole("heading", { name: "Recorded relationships" })).toBeVisible();
  expect(screen.getByRole("button", { name: /Contains/ })).toBeVisible();
  cleanup();
  setup(fixture.attachment, { ...fixture.list, nodes: [...fixture.list.nodes, fixture.attachment] });
  expect(screen.getByTestId("trajectory-record-content")).toHaveTextContent("report.txt");
});

it("keeps different native relations to the same target distinguishable and unknown kinds inspectable", () => {
  const fixture = graphFixture(1);
  const other = { ...fixture.run, id: "run:other", run_id: "other", title: "Related Run" };
  setup(fixture.run, { ...fixture.list, nodes: [...fixture.list.nodes, fixture.run, other], edges: [
    { id: "retry", from: fixture.run.id, to: other.id, kind: "retry" },
    { id: "rerun", from: fixture.run.id, to: other.id, kind: "rerun" },
    { id: "parent", from: fixture.list.nodes[0]!.id, to: fixture.run.id, kind: "parent" },
    { id: "future", from: fixture.run.id, to: other.id, kind: "future_relation" },
  ] });
  expect(screen.getByRole("button", { name: "Retry · Run · Related Run" })).toBeVisible();
  expect(screen.getByRole("button", { name: "Rerun · Run · Related Run" })).toBeVisible();
  expect(screen.getByRole("button", { name: /Parent · Issue/ })).toBeVisible();
  expect(screen.getByRole("button", { name: "future_relation · Run · Related Run" })).toBeVisible();
});

it("shows an explicit wrapped result output instead of serialized session metadata", () => {
  const result = { output: "执行结果正文\n下一行", pr_url: "", session_id: "synthetic", work_dir: "/synthetic" };
  const fixture = runFixture({ result, output: JSON.stringify(result, null, 2) });
  setup(fixture.run, fixture.graph);
  expect(screen.getByTestId("trajectory-output-preview").textContent).toBe(result.output);
  expect(screen.getByTestId("trajectory-raw-node").textContent).toContain("session_id");
  expect(screen.getByTestId("trajectory-output-preview").textContent).not.toContain("session_id");
});

it("keeps the actual result body and historical instructions literal even when they resemble Markdown links", () => {
  const output = 'const pattern = "[a-z](https://literal.invalid)";';
  const fixture = runFixture({ result: { output }, instructions: output });
  const { user } = setup(fixture.run, fixture.graph);
  expect(screen.getByTestId("trajectory-output-preview").textContent).toBe(output);
  return user.click(screen.getByText("Input", { selector: "summary" })).then(() => {
    expect(screen.getByRole("heading", { name: "Instructions" }).closest("section")!.querySelector(".trajectory-body-text")!.textContent).toBe(output);
  });
});

it("preserves structured snapshot and record values as literal JSON while prose still projects labels", async () => {
  const value = { content: "[download](https://synthetic.invalid/artifact)", regexp: "[x](y)", jsx: '<Box value="[literal](value)" />' };
  const fixture = runFixture({ issue_snapshot: value, trigger_summary: "查看 [文档](https://synthetic.invalid/doc)" });
  const { user } = setup(fixture.run, fixture.graph);
  expect(screen.getByTestId("trajectory-run-purpose")).toHaveTextContent("查看 文档");
  await user.click(screen.getByText("Input", { selector: "summary" }));
  const snapshot = screen.getByRole("heading", { name: "Snapshot" }).closest("section")!;
  expect(snapshot.querySelector(".trajectory-body-text")!.textContent).toBe(JSON.stringify(value, null, 2));
  cleanup();
  const record = { ...fixture.run, kind: "platform_event", details: { record: value } };
  setup(record, { ...fixture.graph, nodes: [...fixture.list.nodes, record] });
  expect(screen.getByTestId("trajectory-record-content").querySelector(".trajectory-body-text")!.textContent).toBe(JSON.stringify(value, null, 2));
  expect(document.querySelector("img,script,iframe,[onerror]")).toBeNull();
});

it.each([{ output: "" }, { output: null }, { steps: ["真实结构化事实"] }, 0, false])("keeps a non-text result %j in an explicit structured disclosure", result => {
  const fixture = runFixture({ result, output: JSON.stringify(result) });
  setup(fixture.run, fixture.graph);
  expect(screen.queryByTestId("trajectory-output-preview")).not.toBeInTheDocument();
  expect(screen.getByText("Structured result")).toBeVisible();
  expect(screen.getByTestId("trajectory-structured-result").textContent).toBe(JSON.stringify(result, null, 2));
});

it("offers an already-returned native triggering comment without requiring synthetic run linkage", async () => {
  const fixture = graphFixture(1);
  const comment = { ...fixture.comment, run_id: undefined, details: { comment_id: graphId(40), content: "Returned triggering comment" } };
  const { user, select } = setup(fixture.run, { ...fixture.list, nodes: [...fixture.list.nodes, fixture.run, comment], edges: [] });
  await user.click(screen.getByRole("button", { name: "Synthetic comment" }));
  expect(select).toHaveBeenCalledWith(comment.id);
});

function setup(node: TrajectoryNode, graph: TrajectoryGraph, locale: "en" | "zh-Hans" = "en") {
  const select = vi.fn(), openLog = vi.fn(), push = vi.fn();
  const adapter: NavigationAdapter = { pathname: "/synthetic/issues", searchParams: new URLSearchParams(), hash: "", push, replace: vi.fn(), back: vi.fn(), getShareableUrl: path => path };
  const result = renderWithI18n(<NavigationProvider value={adapter}><GraphInspector graph={graph} node={node} select={select} openLog={openLog}/></NavigationProvider>, { locale });
  return { ...result, select, openLog, push, user: userEvent.setup() };
}

function runFixture(details: Record<string, unknown> = {}) {
  const fixture = graphFixture(1);
  fixture.run.details = details;
  return { ...fixture, graph: { ...fixture.list, nodes: [...fixture.list.nodes, fixture.run] } };
}

it("keeps missing input behind its disclosure while showing the returned Run result first", async () => {
  const fixture = runFixture({ output: "Result survives missing input", trigger_summary: "Check the saved response" });
  const { user } = setup(fixture.run, fixture.graph);
  const output = screen.getByTestId("trajectory-output-preview");
  expect(output).toBeVisible();
  const resultSection = output.closest("section")!;
  expect(within(resultSection).getByRole("heading", { name: "Result" })).toBeVisible();
  expect(screen.getByTestId("trajectory-missing-snapshot")).not.toBeVisible();
  const headings = screen.getAllByRole("heading").map(heading => heading.textContent);
  expect(headings.indexOf("Result")).toBeLessThan(headings.indexOf("Purpose"));
  expect(screen.queryByText("No result recorded.")).not.toBeInTheDocument();
  await user.click(screen.getByText("Input", { selector: "summary" }));
  expect(screen.getByTestId("trajectory-missing-snapshot")).toBeVisible();
});

it.each(["error", "failure_reason"])("shows a native Run %s without requiring output or an input snapshot", field => {
  const fixture = runFixture({ [field]: "Returned execution failure" });
  fixture.run.status = "failed";
  setup(fixture.run, fixture.graph);
  expect(within(screen.getByTestId("trajectory-run-error").closest("section")!).getByRole("heading", { name: "Error" })).toBeVisible();
  expect(screen.getByTestId("trajectory-run-error")).toHaveTextContent("Returned execution failure");
  expect(screen.queryByText("No result recorded.")).not.toBeInTheDocument();
  expect(screen.getByTestId("trajectory-missing-snapshot")).not.toBeVisible();
});

it.each(["pending", "running", "completed"])("does not turn a %s Run title or status into purpose or a result", status => {
  const fixture = runFixture();
  fixture.run.status = status;
  fixture.run.title = "Title is not a saved purpose";
  setup(fixture.run, fixture.graph);
  expect(screen.getByTestId("trajectory-no-result")).toBeVisible();
  expect(screen.queryByRole("heading", { name: "Purpose" })).not.toBeInTheDocument();
  expect(screen.queryByTestId("trajectory-output-preview")).not.toBeInTheDocument();
  expect(screen.queryByTestId("trajectory-run-error")).not.toBeInTheDocument();
  expect(screen.queryByRole("heading", { name: "Input" })).not.toBeInTheDocument();
});

it("puts saved instructions and historical snapshot behind collapsed Input while using the true trigger purpose", async () => {
  const fixture = runFixture({ output: "Visible result", trigger_summary: "Actual saved purpose", trigger_content: "Original trigger body", instructions: "Original instructions", issue_snapshot: { title: "Historical owner title" } });
  const { user, container } = setup(fixture.run, fixture.graph);
  expect(screen.getByTestId("trajectory-run-purpose")).toHaveTextContent("Actual saved purpose");
  const input = screen.getByText("Input", { selector: "summary" }).closest("details")!;
  expect(input).not.toHaveAttribute("open");
  expect(input).toHaveTextContent("Historical owner title");
  expect(input).toHaveTextContent("Original instructions");
  expect(input).toHaveTextContent("Original trigger body");
  expect(container.querySelector(".trajectory-inspector-header")).not.toHaveTextContent("Historical owner title");
  await user.click(within(input).getByText("Input", { selector: "summary" }));
  expect(input).toHaveAttribute("open");
  expect(screen.queryByTestId("trajectory-missing-snapshot")).not.toBeInTheDocument();
});

it("identifies the current graph owner with its real key/title and selects it locally", async () => {
  const fixture = runFixture({ output: "Returned result", started_at: "2026-11-19T00:00:04Z", completed_at: "2026-11-19T00:01:05Z" });
  const owner = fixture.list.nodes[0]!;
  owner.title = "Current owner title";
  const { container, select, push, user } = setup(fixture.run, fixture.graph);
  const header = container.querySelector(".trajectory-inspector-header")!;
  expect(header).toHaveTextContent("SYN-10");
  expect(header).toHaveTextContent("Current owner title");
  expect(header).toHaveTextContent("Run · Execution ended");
  expect(header).toHaveTextContent(fixture.run.id);
  const nativeTimes = [...header.querySelectorAll("time")].map(time => time.getAttribute("dateTime"));
  expect(nativeTimes).toEqual(expect.arrayContaining([fixture.run.created_at, "2026-11-19T00:00:04Z", "2026-11-19T00:01:05Z"]));
  await user.click(within(header as HTMLElement).getByRole("button", { name: "Issue · SYN-10 · Current owner title" }));
  expect(select).toHaveBeenCalledWith(owner.id);
  expect(push).not.toHaveBeenCalled();
  await user.click(within(header as HTMLElement).getByRole("button", { name: "Open task" }));
  expect(select).toHaveBeenLastCalledWith(owner.id);
  expect(push).not.toHaveBeenCalled();
});

it("shows Chinese Issue acceptance and historical Run completion as different native states", () => {
  const fixture = runFixture({ output: "已有结果" });
  const owner = fixture.list.nodes[0]!;
  owner.status = "in_review";
  const { container, unmount } = setup(owner, fixture.graph, "zh-Hans");
  expect(container.querySelector(".trajectory-inspector-header")).toHaveTextContent("任务 · 待验收");
  expect(container.querySelector(".trajectory-inspector-header")).not.toHaveTextContent("执行结束");
  unmount();
  const runView = setup(fixture.run, fixture.graph, "zh-Hans");
  expect(runView.container.querySelector(".trajectory-inspector-header")).toHaveTextContent("运行 · 执行结束");
  expect(screen.queryByText(/绿色勾表示正常执行结束/)).not.toBeInTheDocument();
});

it("does not guess an owner title or local node when that Issue is absent from the graph", () => {
  const fixture = runFixture({ output: "Returned result", issue_snapshot: { title: "Old title must not become current owner" } });
  const { container } = setup(fixture.run, { ...fixture.graph, nodes: [fixture.run] });
  const header = container.querySelector(".trajectory-inspector-header")!;
  expect(within(header as HTMLElement).getByTestId("trajectory-owner-unavailable")).toBeVisible();
  expect(header).not.toHaveTextContent("Old title must not become current owner");
  expect(within(header as HTMLElement).queryByRole("button")).not.toBeInTheDocument();
  expect(within(header as HTMLElement).queryByRole("link", { name: "Open task" })).not.toBeInTheDocument();
});

it("marks source-truncated comments explicitly while keeping their original returned fields inspectable", () => {
  const fixture = graphFixture(1);
  fixture.comment.details.content_truncated = true;
  const original = JSON.stringify(fixture.comment);
  setup(fixture.comment, { ...fixture.list, nodes: [...fixture.list.nodes, fixture.comment] });
  expect(screen.getByTestId("trajectory-record-content")).toHaveTextContent("Private comment body");
  expect(screen.getByTestId("trajectory-content-truncated")).toBeVisible();
  expect(screen.queryByText("Full content", { selector: "summary" })).not.toBeInTheDocument();
  const raw = screen.getByText("Raw data", { selector: "summary" }).closest("details")!;
  expect(raw).not.toHaveAttribute("open");
  expect(JSON.parse(within(raw).getByTestId("trajectory-raw-node").textContent!)).toEqual(fixture.comment);
  expect(JSON.stringify(fixture.comment)).toBe(original);
});

it("reads long Chinese body through inert text and retains the full original after disclosure", async () => {
  const fixture = graphFixture(1);
  const chinese = "核对真实执行结果与任务验收状态。".repeat(600);
  fixture.comment.details.content = `[@复核](mention://agent/${graphId(91)})\n<img src="https://external.invalid/tracker"><script>window.unsafe=1</script>\n${chinese}结束尾标`;
  const fetch = vi.fn(() => { throw new Error("Content rendering must never fetch"); });
  vi.stubGlobal("fetch", fetch);
  const { user } = setup(fixture.comment, { ...fixture.list, nodes: [...fixture.list.nodes, fixture.comment] });
  const body = screen.getByTestId("trajectory-record-content");
  expect(body.querySelector("a, img, iframe, script, [onerror]")).toBeNull();
  expect(body.textContent).not.toMatch(/mention:\/\//);
  expect(body.textContent).toContain('<img src="https://external.invalid/tracker"><script>window.unsafe=1</script>');
  const more = within(body).getByText("Read more", { selector: "summary" });
  expect(more.closest("details")).not.toHaveAttribute("open");
  await user.click(more);
  expect(body.textContent).toContain(chinese);
  expect(body.textContent).toContain("结束尾标");
  const raw = screen.getByText("Raw data", { selector: "summary" }).closest("details")!;
  expect(JSON.parse(within(raw).getByTestId("trajectory-raw-node").textContent!)).toEqual(fixture.comment);
  expect(fetch).not.toHaveBeenCalled();
});

it("keeps raw native fields out of the default prose while allowing full node and edge inspection", () => {
  const fixture = runFixture({ output: "Only one result", unrelated_native_field: { private_marker: "Original raw field" } });
  fixture.graph.edges = [{ id: "native-owner", from: fixture.list.nodes[0]!.id, to: fixture.run.id, kind: "contains" }];
  const original = JSON.stringify(fixture.run);
  const { container } = setup(fixture.run, fixture.graph);
  const raw = screen.getByText("Raw data", { selector: "summary" }).closest("details")!;
  expect(raw).not.toHaveAttribute("open");
  expect(JSON.parse(within(raw).getByTestId("trajectory-raw-node").textContent!)).toEqual(fixture.run);
  expect(raw).toHaveTextContent("contains");
  const renderedProse = container.cloneNode(true) as HTMLElement;
  renderedProse.querySelectorAll("details").forEach(detail => detail.remove());
  expect(renderedProse.textContent).not.toContain("contains");
  expect(renderedProse.textContent).not.toContain("unrelated_native_field");
  expect(screen.getAllByTestId("trajectory-output-preview")).toHaveLength(1);
  expect(JSON.stringify(fixture.run)).toBe(original);
});

it("labels related records for readers and selects only returned current graph nodes", async () => {
  const fixture = runFixture({ output: "Returned result" });
  fixture.graph.nodes.push(fixture.comment, fixture.attachment);
  fixture.graph.edges = [
    { id: "comment-link", from: fixture.run.id, to: fixture.comment.id, kind: "comment" },
    { id: "artifact-link", from: fixture.run.id, to: fixture.attachment.id, kind: "artifact" },
    { id: "missing-link", from: fixture.run.id, to: `comment:${graphId(99)}`, kind: "recorded" },
  ];
  const { select, user } = setup(fixture.run, fixture.graph);
  const evidence = screen.getByRole("heading", { name: "Evidence" }).closest("section")!;
  const comment = within(evidence).getByRole("button", { name: "Synthetic comment" });
  expect(comment).toHaveTextContent("Comment");
  await user.click(comment);
  expect(select).toHaveBeenCalledWith(fixture.comment.id);
  const artifact = within(evidence).getByRole("button", { name: "report.txt" });
  expect(artifact).toHaveTextContent("Attachment");
  await user.click(artifact);
  expect(select).toHaveBeenLastCalledWith(fixture.attachment.id);
  expect(screen.queryByRole("button", { name: /000000000099/ })).not.toBeInTheDocument();
});

it("warns once for truncated Run output and offers the existing explicit Log action", async () => {
  const fixture = runFixture({ output: "Stored output fragment", output_truncated: true });
  const { openLog, user } = setup(fixture.run, fixture.graph);
  expect(screen.getByText("Output preview is truncated.")).toBeVisible();
  const log = screen.getAllByRole("button", { name: "Open log" });
  expect(log).toHaveLength(1);
  await user.click(log[0]!);
  expect(openLog).toHaveBeenCalledOnce();
});

it("keeps the full long Run result distinct from its short reading excerpt", async () => {
  const output = "保留已有结果及其完整字符。".repeat(650) + "结果尾标";
  const fixture = runFixture({ output });
  const { user } = setup(fixture.run, fixture.graph);
  const fullResult = screen.getByTestId("trajectory-output-preview");
  expect(fullResult.textContent).toBe(output);
  const disclosure = fullResult.closest("details")!;
  expect(disclosure).not.toHaveAttribute("open");
  await user.click(within(disclosure).getByText("Read more", { selector: "summary" }));
  expect(fullResult).toBeVisible();
  expect(fullResult.textContent).toHaveLength(output.length);
});

it("links a Run to its visible reply on the canonical task page, then to its trigger, and offers nothing otherwise", async () => {
  const fixture = graphFixture(1);
  const parent = fixture.list.nodes[0]!;
  const { push, user } = setup(fixture.run, { ...fixture.list, nodes: [parent, fixture.run, fixture.comment] });
  const reply = screen.getByRole("link", { name: "Go to reply" });
  const href = `/synthetic/issues/SYN-10#comment-${graphId(40)}`;
  expect(reply).toHaveAttribute("href", href);
  for (const action of [reply, screen.getByRole("button", { name: "Open log" })]) {
    expect(action).toHaveAttribute("data-slot", "button");
    expect(action).toHaveAttribute("data-variant", "outline");
    expect(action).toHaveAttribute("data-size", "sm");
  }
  await user.click(reply);
  expect(push).toHaveBeenCalledExactlyOnceWith(href);
  cleanup();
  const trigger = { ...fixture.comment, run_id: undefined };
  setup(fixture.run, { ...fixture.list, nodes: [parent, fixture.run, trigger] });
  expect(screen.getByRole("link", { name: "Go to triggering comment" })).toHaveAttribute("href", `/synthetic/issues/SYN-10#comment-${graphId(40)}`);
  cleanup();
  setup(fixture.run, { ...fixture.list, nodes: [parent, fixture.run] });
  expect(screen.queryByRole("link", { name: /Go to/ })).not.toBeInTheDocument();
});
