import {
  act,
  fireEvent,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useState } from "react";
import { VirtuosoMockContext } from "react-virtuoso";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiClient, setApiInstance } from "@multica/core/api";
import { setCurrentWorkspace } from "@multica/core/platform";
import { autopilotKeys } from "@multica/core/autopilots/queries";
import { chatKeys, mergeTaskMessagesBySeq } from "@multica/core/chat/queries";
import type { TaskMessagePayload } from "@multica/core/types/events";
import {
  NavigationProvider,
  useNavigation,
  type NavigationAdapter,
} from "../../navigation";
import { renderWithI18n } from "../../test/i18n";

let workspace = "ws";
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => workspace }));
vi.mock("@multica/core/paths", () => ({
  useWorkspaceSlug: () => workspace,
  useWorkspacePaths: () => ({
    autopilots: () => `/${workspace}/autopilots`,
    autopilotDetail: (id: string) => `/${workspace}/autopilots/${id}`,
    issueDetail: (id: string) => `/${workspace}/issues/${id}`,
    agentDetail: (id: string) => `/${workspace}/agents/${id}`,
  }),
}));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: () => "Log Agent" }),
}));
vi.mock("./autopilot-dialog", () => ({ AutopilotDialog: () => null }));
vi.mock("./edit-schedule-trigger-dialog", () => ({
  EditScheduleTriggerDialog: () => null,
}));
vi.mock("./webhook-deliveries-section", () => ({
  WebhookDeliveriesSection: () => null,
}));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => null }));
vi.mock("../../search/register-context", () => ({
  useRegisterCommandBarContext: () => {},
}));
vi.mock("../../editor", () => ({ ReadonlyContent: () => null }));

import { AutopilotDetailPage } from "./autopilot-detail-page";
const taskA = "11111111-1111-4111-8111-111111111111";
const taskB = "22222222-2222-4222-8222-222222222222";
function run(id = "old-run", overrides = {}) {
  return {
    id,
    autopilot_id: "ap",
    source: "api",
    status: "completed",
    issue_id: null,
    task_id: taskA,
    triggered_at: "2026-10-01T00:00:00Z",
    created_at: "2026-10-01T00:00:00Z",
    completed_at: "2026-10-01T00:01:00Z",
    trigger_payload: null,
    ...overrides,
  };
}
const message = (task_id = taskA, seq = 1): TaskMessagePayload => ({
  task_id,
  issue_id: "",
  seq,
  type: "text",
  content: `Synthetic log ${task_id} seq ${seq}`,
});
const response = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
let calls: string[];
let getRun: (path: string) => Response | Promise<Response>;
let getMessages: (path: string) => Response | Promise<Response>;
let navigate: NavigationAdapter;
let client: QueryClient;

function mount(initial = "/ws/autopilots/ap?run=old-run&keep=yes#history") {
  client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  const entries = [initial];
  let index = 0;
  function Fixture() {
    const [path, setPath] = useState(initial);
    const url = new URL(path, "https://connected.test");
    const adapter: NavigationAdapter = {
      pathname: url.pathname,
      searchParams: url.searchParams,
      hash: url.hash,
      push: (next) => {
        entries.splice(++index);
        entries.push(next);
        setPath(next);
      },
      replace: (next) => {
        entries[index] = next;
        setPath(next);
      },
      back: () => {
        index = Math.max(0, index - 1);
        setPath(entries[index]!);
      },
      forward: () => {
        index = Math.min(entries.length - 1, index + 1);
        setPath(entries[index]!);
      },
      getShareableUrl: (next) => `https://connected.test${next}`,
    };
    return (
      <NavigationProvider value={adapter}>
        <Probe />
        <AutopilotDetailPage autopilotId={url.pathname.split("/").at(-1)!} />
      </NavigationProvider>
    );
  }
  function Probe() {
    navigate = useNavigation();
    return null;
  }
  return renderWithI18n(
    <QueryClientProvider client={client}>
      <VirtuosoMockContext.Provider
        value={{ viewportHeight: 800, itemHeight: 48 }}
      >
        <Fixture />
      </VirtuosoMockContext.Provider>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  workspace = "ws";
  calls = [];
  setApiInstance(new ApiClient("https://api.test"));
  setCurrentWorkspace("ws", "ws");
  getRun = () => response(run());
  getMessages = () => response([message()]);
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: string) => {
      const path = new URL(input).pathname;
      calls.push(path);
      if (/\/runs\/[^/]+$/.test(path)) return getRun(path);
      if (path.endsWith("/messages")) return getMessages(path);
      if (path.endsWith("/runs"))
        return response({
          runs: [
            run("recent", { task_id: taskB }),
            run("issue-run", { task_id: null, issue_id: "ISS-1" }),
          ],
        });
      if (/\/autopilots\/[^/]+$/.test(path))
        return response({
          autopilot: {
            id: "ap",
            workspace_id: workspace,
            title: "Read-only autopilot",
            status: "active",
            execution_mode: "run_only",
            assignee_type: "agent",
            assignee_id: "agent",
            can_write: false,
          },
          triggers: [],
          collaborators: [],
        });
      return response([]);
    }),
  );
});
afterEach(() => {
  client?.clear();
  vi.unstubAllGlobals();
  setCurrentWorkspace(null, null);
});

describe("autopilot run log URL", () => {
  it("cold loads a terminal run outside the recent list through the real API client", async () => {
    mount();
    await waitFor(() =>
      expect(calls).toContain(`/api/tasks/${taskA}/messages`),
    );
    await screen.findByRole("dialog");
    await screen.findByText(/Synthetic log/);
    expect(screen.getByRole("dialog")).toHaveTextContent("Log Agent");
    expect(calls).toContain("/api/autopilots/ap/runs/old-run");
    expect(screen.queryByRole("button", { name: "Run now" })).toBeNull();
  });
  it("preserves the overview, issue links, query, fragment, and history", async () => {
    mount("/ws/autopilots/ap?keep=yes#history");
    const link = await screen.findByRole("link", {
      name: "View execution log",
    });
    expect(link).toHaveAttribute(
      "href",
      "/ws/autopilots/ap?keep=yes&run=recent#history",
    );
    expect(screen.getByRole("link", { name: /Issue linked/ })).toHaveAttribute(
      "href",
      "/ws/issues/ISS-1",
    );
    expect(calls.some((c) => /\/runs\/[^/]+$|\/messages$/.test(c))).toBe(false);
    getRun = () => response(run("recent", { task_id: taskB }));
    getMessages = () => response([message(taskB)]);
    fireEvent.click(link);
    await screen.findByText(/Synthetic log/);
    act(() => navigate.back());
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    act(() => navigate.forward?.());
    await screen.findByText(/Synthetic log/);
    fireEvent.click(
      within(screen.getByRole("dialog")).getByRole("button", { name: "Close" }),
    );
    await waitFor(() => expect(navigate.searchParams.has("run")).toBe(false));
    expect(navigate.searchParams.get("keep")).toBe("yes");
    expect(navigate.hash).toBe("#history");
  });
  it.each([403, 404])(
    "keeps missing and denied runs opaque (%i)",
    async (status) => {
      getRun = () => response({ error: "private existence detail" }, status);
      mount();
      expect(
        await screen.findByText("Execution log unavailable"),
      ).toBeInTheDocument();
      expect(screen.queryByText(/private existence/)).toBeNull();
      expect(calls.some((c) => c.endsWith("/messages"))).toBe(false);
    },
  );
  it("shows no-task separately from empty logs", async () => {
    getRun = () => response(run("old-run", { task_id: null }));
    mount();
    expect(
      await screen.findByText("This run has no execution log."),
    ).toBeInTheDocument();
    expect(calls.some((c) => c.endsWith("/messages"))).toBe(false);
  });
  it("shows a messages failure and supports a bounded manual retry", async () => {
    getMessages = () => response({ error: "synthetic failure" }, 503);
    mount();
    expect(
      await screen.findByText("Could not load execution log."),
    ).toBeInTheDocument();
    expect(calls.filter((c) => c.endsWith("/messages"))).toHaveLength(1);
    getMessages = () => response([]);
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(
      await screen.findByText("No execution data recorded."),
    ).toBeInTheDocument();
  });
  it("discards a late run response after closing and isolates the next selected run", async () => {
    let release!: (r: Response) => void;
    getRun = () =>
      new Promise<Response>((resolve) => {
        release = resolve;
      });
    mount();
    await screen.findByRole("dialog");
    act(() => navigate.replace("/ws/autopilots/ap?keep=yes#history"));
    await act(async () => release(response(run())));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(calls.some((c) => c.endsWith("/messages"))).toBe(false);
    getRun = () => response(run("recent", { task_id: taskB }));
    getMessages = () => response([message(taskB)]);
    act(() => navigate.push("/ws/autopilots/ap?run=recent"));
    await screen.findByText(/Synthetic log/);
    expect(screen.getByRole("dialog")).toHaveTextContent(taskB);
    expect(screen.getByRole("dialog")).not.toHaveTextContent(taskA);
  });
  it("keeps the existing live cache and backfill", async () => {
    getRun = () =>
      response(run("old-run", { status: "running", completed_at: null }));
    mount();
    await screen.findByText("1 step");
    act(() =>
      client.setQueryData<TaskMessagePayload[]>(
        chatKeys.taskMessages(taskA),
        (old = []) =>
          mergeTaskMessagesBySeq(old, [
            {
              ...message(taskA, 2),
              type: "tool_use",
              tool: "Read",
              input: { path: "synthetic.txt" },
            },
          ]),
      ),
    );
    await screen.findByText("2 steps");
    expect(client.getQueryData(chatKeys.taskMessages(taskA))).toHaveLength(2);
  });
  it("retries a failed run lookup manually without requesting arbitrary URL task IDs", async () => {
    getRun = () => response({ error: "synthetic failure" }, 500);
    mount("/ws/autopilots/ap?run=old-run&task=arbitrary");
    await screen.findByText("Could not load execution log.");
    expect(calls.filter((c) => c.endsWith("/old-run"))).toHaveLength(1);
    getRun = () => response(run());
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    await screen.findByText(/Synthetic log/);
    expect(calls).toContain(`/api/tasks/${taskA}/messages`);
    expect(calls).not.toContain("/api/tasks/arbitrary/messages");
  });

  it.each([403, 404])(
    "keeps unavailable transcripts opaque (%i)",
    async (status) => {
      getMessages = () =>
        response({ error: "private transcript detail" }, status);
      mount();
      await screen.findByText("Execution log unavailable");
      expect(screen.queryByText(/private transcript/)).toBeNull();
      expect(screen.queryByText("No execution data recorded.")).toBeNull();
    },
  );

  it("does not read a task from a run returned for the wrong autopilot", async () => {
    getRun = () => response(run("old-run", { autopilot_id: "other" }));
    mount();
    await screen.findByText("Could not load execution log.");
    expect(calls.some((c) => c.endsWith("/messages"))).toBe(false);
  });

  it("isolates late transcript responses across workspace and autopilot changes", async () => {
    let release!: (r: Response) => void;
    getMessages = () =>
      new Promise<Response>((resolve) => {
        release = resolve;
      });
    mount();
    await waitFor(() =>
      expect(calls).toContain(`/api/tasks/${taskA}/messages`),
    );
    workspace = "other";
    setCurrentWorkspace("other", "other");
    getRun = () =>
      response(run("old-run", { autopilot_id: "ap-other", task_id: taskB }));
    getMessages = () => response([message(taskB)]);
    act(() => navigate.push("/other/autopilots/ap-other?run=old-run"));
    await screen.findByText(/Synthetic log/);
    await act(async () => release(response([message()])));
    expect(screen.getByRole("dialog")).toHaveTextContent(taskB);
    expect(screen.getByRole("dialog")).not.toHaveTextContent(taskA);
    expect(
      client.getQueryData(autopilotKeys.run("other", "ap-other", "old-run")),
    ).toMatchObject({ task_id: taskB });
  });

  it("does not reopen after a transcript arrives after close or unmount", async () => {
    let release!: (r: Response) => void;
    getMessages = () =>
      new Promise<Response>((resolve) => {
        release = resolve;
      });
    const view = mount();
    await waitFor(() =>
      expect(calls).toContain(`/api/tasks/${taskA}/messages`),
    );
    fireEvent.click(
      within(screen.getByRole("dialog")).getByRole("button", { name: "Close" }),
    );
    await waitFor(() => expect(navigate.searchParams.has("run")).toBe(false));
    view.unmount();
    await act(async () => release(response([message()])));
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("retains streamed events during live-to-terminal backfill", async () => {
    getRun = () => response(run("old-run", { status: "running" }));
    mount();
    await screen.findByText("1 step");
    const streamed = {
      ...message(taskA, 2),
      type: "tool_use" as const,
      tool: "Read",
      input: { path: "synthetic.txt" },
    };
    act(() =>
      client.setQueryData<TaskMessagePayload[]>(
        chatKeys.taskMessages(taskA),
        (old = []) => mergeTaskMessagesBySeq(old, [streamed]),
      ),
    );
    await screen.findByText("2 steps");
    getRun = () => response(run());
    getMessages = () => response([message(), { ...streamed, seq: 3 }]);
    await act(async () => {
      await client.refetchQueries({
        queryKey: autopilotKeys.run("ws", "ap", "old-run"),
        exact: true,
      });
    });
    await screen.findByText("3 steps");
    expect(
      client
        .getQueryData<TaskMessagePayload[]>(chatKeys.taskMessages(taskA))
        ?.map((m) => m.seq),
    ).toEqual([1, 2, 3]);
  });

  it("allows a live transcript read failure to be retried without re-running the job", async () => {
    getRun = () => response(run("old-run", { status: "running" }));
    getMessages = () => response({ error: "synthetic failure" }, 503);
    mount();
    await screen.findByText("Could not load execution log.");
    expect(calls.filter((c) => c.endsWith("/messages"))).toHaveLength(1);
    getMessages = () => response([message()]);
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    await screen.findByText("1 step");
    expect(calls.filter((c) => c.endsWith("/messages"))).toHaveLength(2);
  });
});
