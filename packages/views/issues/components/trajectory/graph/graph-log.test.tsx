import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { act, cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiClient, setApiInstance } from "@multica/core/api";
import type { TaskMessagePayload } from "@multica/core/types";
import { graphMessagesOptions } from "@multica/core/trajectory/graph-web";
import { renderWithI18n } from "../../../../test/i18n";
import { GraphLog } from "./graph-log";
import { graphId, graphNode, graphScope } from "./graph-fixture";

const at = "2026-11-19T00:00:07Z";
const run = graphNode("run", 20, 10, "A long purpose that should not become the log heading");
const clients: QueryClient[] = [];
const message = (fields: Partial<TaskMessagePayload> = {}): TaskMessagePayload => ({
  task_id: graphId(20), issue_id: graphId(10), seq: 7, type: "text", created_at: at,
  ...fields,
});

beforeEach(() => { setApiInstance(new ApiClient("")); });
afterEach(() => {
  cleanup();
  clients.forEach(client => client.clear());
  clients.length = 0;
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function setup(messages: unknown[], options: { mounted?: boolean; messageSeq?: number; read?: () => Response | Promise<Response> } = {}) {
  const requests: { path: string; method: string; signal?: AbortSignal | null }[] = [];
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = new URL(String(input), "https://synthetic.invalid").pathname;
    requests.push({ path, method: init?.method ?? "GET", signal: init?.signal });
    if (path !== `/api/tasks/${graphId(20)}/messages`) throw new Error(`Unexpected synthetic request: ${path}`);
    return options.read ? options.read() : Response.json(messages);
  });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  clients.push(client);
  const view = (mounted = true, messageSeq = options.messageSeq) => <QueryClientProvider client={client}>
    {mounted ? <GraphLog scope={graphScope} run={run} messageSeq={messageSeq}/> : <p>Log closed</p>}
  </QueryClientProvider>;
  const rendered = renderWithI18n(view(options.mounted ?? true));
  return { client, requests, user: userEvent.setup(), rerender: (mounted = true, messageSeq = options.messageSeq) => rendered.rerender(view(mounted, messageSeq)) };
}

function record(seq: number): HTMLDetailsElement {
  return screen.getByTestId("trajectory-log").querySelector<HTMLDetailsElement>(`[data-message-seq="${seq}"]`)!;
}

it("keeps a copied native future type visible through the existing decoder fallback", async () => {
  setup([{ ...message(), type: "future_tool_event", source_message_type: "future_tool_event", payload: { note: "Returned future facts" } }]);
  await screen.findByText("Unrecognized message type");
  expect(record(7).querySelector("summary")).toHaveTextContent("7 · future_tool_event");
  const payload = within(record(7)).getByRole("heading", { name: "payload" }).closest("section")!;
  expect(payload).toBeVisible();
  expect(payload).toHaveTextContent("Returned future facts");
});

it("preserves literal tool input and output patterns in the primary reading area", async () => {
  const input = { pattern: "[a-z](foo)", linkPattern: "[label](https://literal.invalid)" };
  const output = 'handlers[task](input);\npattern [a-z](https://literal.invalid)\n<Box label="[item](literal)" />';
  setup([message({ type: "tool_result", input, output })]);
  const heading = await screen.findByRole("heading", { name: "Tool output" });
  expect(heading.closest("section")!.querySelector(".trajectory-body-text")!.textContent).toBe(output);
  const inputSection = screen.getByRole("heading", { name: "Tool input" }).closest("section")!;
  expect(inputSection.querySelector(".trajectory-body-text")!.textContent).toBe(JSON.stringify(input, null, 2));
});

it("keeps the reported Write document object and a string command unchanged with no executable DOM", async () => {
  const input = { content: "[download](https://synthetic.invalid/artifact)", regexp: "[x](y)" };
  const output = 'printf "[download](https://synthetic.invalid/artifact)"\n<img src="https://synthetic.invalid/pixel" onerror="bad()">';
  const { requests } = setup([message({ type: "tool_result", tool: "Write", input, output })]);
  const inputHeading = await screen.findByRole("heading", { name: "Tool input" });
  expect(inputHeading.closest("section")!.querySelector(".trajectory-body-text")!.textContent).toBe(JSON.stringify(input, null, 2));
  const outputHeading = screen.getByRole("heading", { name: "Tool output" });
  expect(outputHeading.closest("section")!.querySelector(".trajectory-body-text")!.textContent).toBe(output);
  expect(screen.getByTestId("trajectory-log").querySelector("a,img,script,iframe,[onerror]")).toBeNull();
  expect(requests).toHaveLength(1);
});

it("shows the native text and metadata with a compact Run heading and collapsed original data", async () => {
  const body = "The native answer remains readable before opening raw data.";
  const native = message({ content: body });
  setup([native]);
  expect(await screen.findByText(body)).toBeVisible();
  expect(screen.getByRole("heading", { name: "Run", level: 3 })).toBeVisible();
  expect(screen.getByText(run.id)).toBeVisible();
  expect(screen.queryByText(run.title)).not.toBeInTheDocument();
  const item = record(7);
  expect(item).toHaveAttribute("open");
  expect(item.querySelector("summary")).toHaveTextContent("7 · text");
  expect(within(item).getByText(at)).toHaveAttribute("datetime", at);
  expect(within(item).getByRole("heading", { name: "Message" })).toBeVisible();
  const raw = within(item).getByText("Raw data").closest("details")!;
  expect(raw).not.toHaveAttribute("open");
  expect(raw.querySelector("pre")).toHaveTextContent('"task_id": "00000000-0000-4000-8000-000000000020"');
  expect(raw.querySelector("pre")).not.toBeVisible();
});

it("reads actual nested tool input and the corresponding output as separate message sections", async () => {
  setup([
    message({ seq: 8, type: "tool_use", call_id: "call-read-1", tool: "Read", content: "Inspect the supplied file", input: { path: "synthetic.txt", options: { lines: [1, 2], note: "Keep all fields" } } }),
    message({ seq: 9, type: "tool_result", call_id: "call-read-1", tool: "Read", output: "Returned file content\nSecond line", output_truncated: false }),
  ]);
  expect(await screen.findByText("Inspect the supplied file")).toBeVisible();
  const input = record(8);
  expect(input.querySelector("summary")).toHaveTextContent("8 · tool_use · Read");
  expect(within(input).getByRole("heading", { name: "Tool input" })).toBeVisible();
  expect(within(input).getByText(/"path": "synthetic.txt"/, { selector: ".trajectory-body-text" })).toBeVisible();
  expect(within(input).getByText(/"lines": \[/, { selector: ".trajectory-body-text" })).toHaveTextContent("Keep all fields");
  const output = record(9);
  expect(within(output).getByRole("heading", { name: "Tool output" })).toBeVisible();
  expect(within(output).getByText("Returned file content Second line")).toBeVisible();
  expect(screen.queryByText("Stored output truncation")).not.toBeInTheDocument();
});

it("keeps schema-normalized opaque fields readable and the exact returned event inspectable", async () => {
  const opaque = { ...message(), type: "provider_status", provider_payload: { phase: "provider native status", attempt: 2 }, provider_flag: false };
  setup([opaque]);
  const payload = await screen.findByRole("heading", { name: "provider_payload" });
  expect(payload).toBeVisible();
  expect(within(record(7)).getByText(/provider native status/, { selector: ".trajectory-body-text" })).toBeVisible();
  expect(within(record(7)).getByText("false", { selector: ".trajectory-body-text" })).toBeVisible();
  const raw = within(record(7)).getByText("Raw data").closest("details")!;
  expect(raw.querySelector("pre")).toHaveTextContent('"provider_flag": false');
  expect(raw.querySelector("pre")).toHaveTextContent('"attempt": 2');
  expect(raw.querySelector("pre")).toHaveTextContent('"type": "text"');
});

it("makes a future returned type nonblank without replacing its native type or fields", async () => {
  const future = { ...message(), type: "provider_status", provider_payload: { phase: "waiting for native provider" } };
  const api = new ApiClient("");
  // Keep the complete native shape while exercising a future type after the API boundary.
  vi.spyOn(api, "listTrajectoryTaskMessages").mockResolvedValue([future] as unknown as TaskMessagePayload[]);
  setApiInstance(api);
  setup([]);
  expect(await screen.findByText("Unrecognized message type")).toBeVisible();
  expect(record(7).querySelector("summary")).toHaveTextContent("7 · provider_status");
  expect(within(record(7)).getByText(/waiting for native provider/, { selector: ".trajectory-body-text" })).toBeVisible();
  expect(within(record(7)).getByText("Raw data").closest("details")?.querySelector("pre")).toHaveTextContent('"type": "provider_status"');
});

it("keeps returned sequence order and opens, highlights and scrolls the explicit target", async () => {
  const scroll = vi.spyOn(Element.prototype, "scrollIntoView");
  const { rerender } = setup([
    message({ seq: 9, content: "First returned message" }),
    message({ seq: 3, content: "Second returned message" }),
    message({ seq: 7, content: "Third returned message" }),
  ], { messageSeq: 7 });
  await screen.findByText("Third returned message");
  const items = [...screen.getByTestId("trajectory-log").querySelectorAll("[data-message-seq]")];
  expect(items.map(item => item.getAttribute("data-message-seq"))).toEqual(["9", "3", "7"]);
  expect(record(7)).toHaveAttribute("data-highlighted", "true");
  expect(record(7)).toHaveAttribute("open");
  expect(scroll).toHaveBeenLastCalledWith({ block: "nearest" });
  expect(scroll.mock.contexts.at(-1)).toBe(record(7));
  record(3).open = false;
  rerender(true, 3);
  expect(record(3)).toHaveAttribute("open");
  expect(record(3)).toHaveAttribute("data-highlighted", "true");
  expect(record(7)).not.toHaveAttribute("data-highlighted");
  expect(scroll.mock.contexts.at(-1)).toBe(record(3));
});

it("shows the stored truncation marker only when the native flag is true", async () => {
  setup([
    message({ seq: 1, type: "tool_result", tool: "Read", output: "Native partial output", output_truncated: true }),
    message({ seq: 2, type: "tool_result", tool: "Read", output: "Native measured output", output_truncated: false }),
    message({ seq: 3, type: "tool_result", tool: "Read", output: "Native unmeasured output" }),
  ]);
  expect(await screen.findByText("Native partial output")).toBeVisible();
  expect(within(record(1)).getByText("Stored output truncation")).toBeVisible();
  expect(screen.getAllByText("Stored output truncation")).toHaveLength(1);
  expect(within(record(2)).queryByText("Stored output truncation")).not.toBeInTheDocument();
  expect(within(record(3)).queryByText("Stored output truncation")).not.toBeInTheDocument();
});

it("reads only the mounted Run endpoint and cancels a late response when Log closes", async () => {
  let release!: (response: Response) => void;
  const { client, requests, rerender } = setup([], { mounted: false, read: () => new Promise(resolve => { release = resolve; }) });
  expect(requests).toHaveLength(0);
  rerender(true);
  expect(screen.getByRole("status")).toHaveTextContent("Loading...");
  await waitFor(() => { expect(requests).toHaveLength(1); });
  expect(requests[0]?.path).toBe(`/api/tasks/${graphId(20)}/messages`);
  expect(requests[0]?.method).toBe("GET");
  rerender(false);
  expect(requests[0]?.signal?.aborted).toBe(true);
  await act(async () => { release(Response.json([message({ content: "Late private response" })])); });
  expect(screen.queryByText("Late private response")).not.toBeInTheDocument();
  expect(client.getQueryData(graphMessagesOptions(graphScope, graphId(20)).queryKey)).toBeUndefined();
  expect(requests).toHaveLength(1);
});

it("keeps a failed read distinct from empty and retries through the existing query", async () => {
  let failed = true;
  const { requests, user } = setup([], { read: () => failed
    ? new Response("Private server diagnostic", { status: 500 })
    : Response.json([message({ content: "Recovered native message" })]) });
  expect(await screen.findByRole("alert")).toHaveTextContent("Could not load records");
  expect(screen.queryByText("No messages returned")).not.toBeInTheDocument();
  expect(screen.queryByText("Private server diagnostic")).not.toBeInTheDocument();
  failed = false;
  await user.click(screen.getByRole("button", { name: "Retry" }));
  expect(await screen.findByText("Recovered native message")).toBeVisible();
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  expect(requests).toHaveLength(2);
});

it("shows true empty only after the message read succeeds", async () => {
  let release!: (response: Response) => void;
  setup([], { read: () => new Promise(resolve => { release = resolve; }) });
  expect(screen.getByRole("status")).toHaveTextContent("Loading...");
  expect(screen.queryByText("No messages returned")).not.toBeInTheDocument();
  await waitFor(() => { expect(release).toBeTypeOf("function"); });
  await act(async () => { release(Response.json([])); });
  expect(await screen.findByText("No messages returned")).toBeVisible();
  expect(screen.queryByRole("status")).not.toBeInTheDocument();
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
});

it("retains full long unsafe content behind readable expansion and never creates executable markup", async () => {
  const unsafe = "<img src='https://synthetic.invalid/remote.png'><script>window.bad=1</script>[@Owner](mention://agent/fake) [Result](javascript:alert(1)) ";
  const body = `Readable start ${unsafe}${"完整内容🙂 ".repeat(500)}Native final line`;
  const { user, requests } = setup([message({ content: body })]);
  await screen.findByText(/Readable start/, { selector: "p.trajectory-body-text" });
  expect(screen.getByTestId("trajectory-log").querySelector("a, img, script, iframe, [onerror]")).toBeNull();
  expect(screen.queryByText(/Native final line/, { selector: ".trajectory-body-text" })).not.toBeVisible();
  await user.click(screen.getByText("Read more"));
  expect(screen.getByText(/Native final line/, { selector: ".trajectory-body-text" })).toBeVisible();
  const raw = within(record(7)).getByText("Raw data").closest("details")!;
  await user.click(within(raw).getByText("Raw data"));
  expect(raw.querySelector("pre")?.textContent).toContain(body);
  expect(requests).toHaveLength(1);
  expect(screen.getByTestId("trajectory-log").querySelector("a, img, script, iframe, [onerror]")).toBeNull();
});
