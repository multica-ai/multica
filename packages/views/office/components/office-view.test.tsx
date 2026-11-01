/**
 * @vitest-environment jsdom
 *
 * The office is the pixtuoid engine on a canvas. jsdom has no WebAssembly office,
 * so the engine is replaced by a stub that records the sessions it is given and
 * reports a tag for each seated worker. These tests cover what the view decides:
 * which issues are workers, the tag each one shows, and which panel a click opens.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ViewStoreProvider } from "@multica/core/issues/stores/view-store-context";
import { getIssueSurfaceViewStore } from "@multica/core/issues/stores/surface-view-store";
import { IssuePeekActionsContext } from "../../issues/surface/peek-context";
import type { Issue, IssueStatus } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { IssueContextMenuProvider } from "../../issues/actions/issue-actions-context-menu";
import { workspaceKeys } from "@multica/core/workspace/queries";
import type { Agent } from "@multica/core/types";
import { OfficeView } from "./office-view";

// The stub engine: the worker is replaced by a fake that seats sessions the way the
// engine does, and reports a tag for each seated worker.
const engine = vi.hoisted(() => ({
  starts: [] as string[],
  working: new Map<string, boolean>(),
  ends: [] as string[],
  rooms: new Map<string, number>(),
}));

class FakeWorker {
  onmessage: ((event: { data: unknown }) => void) | null = null;
  private seated = new Map<string, boolean>();

  postMessage(message: { type: string; sessions?: { key: string; working: boolean; room: number }[] }) {
    if (message.type !== "sessions" || !message.sessions) return;
    for (const session of message.sessions) engine.rooms.set(session.key, session.room);
    const wanted = new Map(message.sessions.map((session) => [session.key, session.working]));
    for (const key of [...this.seated.keys()]) {
      if (wanted.has(key)) continue;
      engine.ends.push(key);
      this.seated.delete(key);
    }
    for (const [key, working] of wanted) {
      if (!this.seated.has(key)) {
        engine.starts.push(key);
        this.seated.set(key, false);
      }
      if (this.seated.get(key) === working) continue;
      engine.working.set(key, working);
      this.seated.set(key, working);
    }
    const labels = [...wanted.keys()].map((key, index) => ({ key, x: 20 + index * 10, y: 30 }));
    queueMicrotask(() => {
      this.onmessage?.({
        data: {
          type: "rooms",
          rooms: [
            { x: 80, y: 60, w: 66, h: 44 },
            { x: 160, y: 60, w: 66, h: 44 },
          ],
          tank: { x: 146, y: 50 },
          boards: [{ x: 20, y: 40, w: 14, h: 11 }],
        },
      });
      this.onmessage?.({ data: { type: "labels", labels } });
    });
  }

  terminate() {}
}

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

// The panel has its own tests; here it only reports who it was opened for.
vi.mock("./office-agent-panel", () => ({
  OfficeAgentPanel: ({ target, onClose }: { target: { agentId: string; issueId?: string } | null; onClose: () => void }) =>
    target ? (
      <div role="dialog" aria-label="panel">
        <span>{`panel ${target.agentId} ${target.issueId ?? "chat"}`}</span>
        <button type="button" onClick={onClose}>
          close panel
        </button>
      </div>
    ) : null,
}));

// The board dialog has its own tests; here it only reports that it is open.
vi.mock("./office-board-dialog", () => ({
  OfficeBoardDialog: ({ open }: { open: boolean }) => (open ? <div role="dialog" aria-label="Board" /> : null),
}));

vi.mock("@multica/core/properties", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/properties")>()),
  propertyListOptions: () => ({ queryKey: ["properties"], queryFn: async () => [] }),
  useSetIssueProperty: () => ({ mutate: () => {} }),
  useUnsetIssueProperty: () => ({ mutate: () => {} }),
}));

vi.mock("@multica/core/workspace/hooks", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/workspace/hooks")>()),
  useActorName: () => ({ getActorName: () => "Ada" }),
}));

vi.mock("@multica/core/auth", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/auth")>()),
  useAuthStore: (selector: (state: { user: { id: string } }) => unknown) =>
    selector({ user: { id: "viewer-1" } }),
}));

vi.mock("@multica/core/agents", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/agents")>()),
  isAgentRuntimeBound: () => true,
  useAgentPresenceDetail: () => ({ availability: "offline", workload: null }),
  agentTaskSnapshotOptions: () => ({ queryKey: ["task-snapshot"], queryFn: async () => [] }),
}));

vi.mock("@multica/core/paths", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/paths")>();
  return {
    ...actual,
    useCurrentWorkspace: () => ({ id: "ws-1", slug: "acme" }),
    useWorkspacePaths: () => actual.paths.workspace("acme"),
  };
});

vi.mock("../../navigation", () => ({
  AppLink: ({ children, ...props }: React.ComponentProps<"a">) => <a {...props}>{children}</a>,
  useNavigation: () => ({
    push: () => {},
    openInNewTab: () => {},
    getShareableUrl: (path: string) => `https://app.example${path}`,
    pathname: "/",
  }),
  resolveClickIntent: () => "push",
  useIntentNavigate: () => () => {},
}));

function makeIssue(id: string, status: IssueStatus, assignee?: string): Issue {
  return {
    id,
    workspace_id: "ws-1",
    number: 1,
    identifier: `MUL-${id}`,
    title: `Task ${id}`,
    description: null,
    status,
    priority: "high",
    assignee_type: assignee ? "agent" : null,
    assignee_id: assignee ?? null,
    creator_type: "member",
    creator_id: "member-1",
    parent_issue_id: null,
    project_id: null,
    position: 1,
    stage: null,
    start_date: null,
    due_date: null,
    labels: [],
    metadata: {},
    properties: {},
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
}

class ObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
  takeRecords() {
    return [];
  }
}

const STATUSES: IssueStatus[] = ["backlog", "todo", "in_progress", "in_review", "blocked", "done"];

function makeAgent(id: string, room: string | undefined): Agent {
  return { id, name: `Agent ${id}`, office_room: room } as Agent;
}

describe("OfficeView", () => {
  let queryClient: QueryClient;

  beforeEach(() => {
    engine.starts.length = 0;
    engine.working.clear();
    engine.ends.length = 0;
    vi.stubGlobal("Worker", FakeWorker);
    HTMLCanvasElement.prototype.transferControlToOffscreen = () => ({}) as OffscreenCanvas;
    vi.stubGlobal("IntersectionObserver", ObserverStub);
    vi.stubGlobal("ResizeObserver", ObserverStub);
    queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    });
  });

  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
    Reflect.deleteProperty(HTMLCanvasElement.prototype, "transferControlToOffscreen");
  });

  function setup(
    issues: Issue[],
    options: { grouping?: "status" | "assignee"; open?: (issueId: string) => void; agents?: Agent[] } = {},
  ) {
    queryClient.setQueryData(workspaceKeys.agents("ws-1"), options.agents ?? []);
    const store = getIssueSurfaceViewStore(`office-${Math.floor(Math.random() * 1e9)}`);
    store.getState().setGrouping(options.grouping ?? "status");
    const peek = { open: options.open ?? (() => {}), toggle: () => {}, close: () => {}, publishColumns: () => {} };
    const view = (list: Issue[]) => (
      <QueryClientProvider client={queryClient}>
        <ViewStoreProvider store={store}>
          <IssueContextMenuProvider>
            <IssuePeekActionsContext.Provider value={peek}>
              <OfficeView
                issues={list}
                visibleStatuses={STATUSES}
                onMoveIssue={() => {}}
              />
            </IssuePeekActionsContext.Provider>
          </IssueContextMenuProvider>
        </ViewStoreProvider>
      </QueryClientProvider>
    );
    const result = renderWithI18n(view(issues));
    return { store, rerender: (list: Issue[]) => result.rerender(view(list)) };
  }

  it("seats one worker for each agent on the project, not one per issue", async () => {
    setup([
      makeIssue("a", "todo", "agent-1"),
      makeIssue("b", "todo"),
      makeIssue("c", "in_progress", "agent-1"),
      makeIssue("d", "todo", "agent-2"),
    ]);

    await screen.findByRole("button", { name: "Todo" });
    expect(engine.starts).toEqual(["agent-1", "agent-2"]);
  });

  it("shows the status of the agent's issue in progress over its head", async () => {
    setup([makeIssue("a", "todo", "agent-1"), makeIssue("c", "in_progress", "agent-1")]);

    expect(await screen.findByRole("button", { name: "In Progress" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Todo" })).toBeNull();
  });

  it("shows waiting for work, not the status, when all of an agent's issues are done", async () => {
    setup([makeIssue("d", "done", "agent-2")]);

    expect(await screen.findByRole("button", { name: "Waiting for work" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Done" })).toBeNull();
    expect(engine.starts).toEqual(["agent-2"]);
  });

  it("opens the agent's panel on its current issue when its tag is clicked", async () => {
    setup([makeIssue("c", "in_progress", "agent-1")]);

    fireEvent.click(await screen.findByRole("button", { name: "In Progress" }));

    expect(screen.getByText("panel agent-1 c")).toBeTruthy();
  });

  it("opens the same panel, on its chat, when an idle agent's tag is clicked", async () => {
    setup([makeIssue("d", "done", "agent-1")]);

    fireEvent.click(await screen.findByRole("button", { name: "Waiting for work" }));

    expect(screen.getByText("panel agent-1 chat")).toBeTruthy();
  });

  it("opens the panel on the chat when the worker itself is clicked", async () => {
    setup([makeIssue("c", "in_progress", "agent-1")]);
    await screen.findByRole("button", { name: "In Progress" });
    const canvas = document.querySelector("canvas") as HTMLCanvasElement;
    // The tag is anchored at (20, 30) in layout units; the figure hangs below it.
    canvas.getBoundingClientRect = () => ({ left: 0, top: 0, width: 256, height: 171 }) as DOMRect;

    fireEvent.click(canvas, { clientX: 20, clientY: 40 });

    expect(screen.getByText("panel agent-1 chat")).toBeTruthy();
  });

  it("closes the panel", async () => {
    setup([makeIssue("c", "in_progress", "agent-1")]);
    fireEvent.click(await screen.findByRole("button", { name: "In Progress" }));

    fireEvent.click(screen.getByRole("button", { name: "close panel" }));

    expect(screen.queryByRole("dialog", { name: "panel" })).toBeNull();
  });

  it("moves the tag to the new status when the agent's issue changes status", async () => {
    const { rerender } = setup([makeIssue("a", "todo", "agent-1")]);
    expect(await screen.findByRole("button", { name: "Todo" })).toBeTruthy();

    rerender([makeIssue("a", "in_review", "agent-1")]);

    expect(await screen.findByRole("button", { name: "In Review" })).toBeTruthy();
    expect(engine.starts).toEqual(["agent-1"]);
    expect(engine.ends).toEqual([]);
  });

  it("moves the tag off a worker's issue once that issue is done", async () => {
    const { rerender } = setup([makeIssue("a", "in_review", "agent-1")]);
    expect(await screen.findByRole("button", { name: "In Review" })).toBeTruthy();

    rerender([makeIssue("a", "done", "agent-1")]);

    expect(await screen.findByRole("button", { name: "Waiting for work" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "In Review" })).toBeNull();
  });

  it("shows the worker's next open issue once the one it was showing is done", async () => {
    const { rerender } = setup([makeIssue("a", "in_review", "agent-1"), makeIssue("b", "todo", "agent-1")]);
    expect(await screen.findByRole("button", { name: "In Review" })).toBeTruthy();

    rerender([makeIssue("a", "done", "agent-1"), makeIssue("b", "todo", "agent-1")]);

    expect(await screen.findByRole("button", { name: "Todo" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "In Review" })).toBeNull();
  });

  it("sends a worker home once its agent has no issues on the project", async () => {
    const { rerender } = setup([makeIssue("a", "todo", "agent-1")]);
    await screen.findByRole("button", { name: "Todo" });

    rerender([makeIssue("a", "todo")]);

    await vi.waitFor(() => expect(engine.ends).toEqual(["agent-1"]));
  });

  it("opens the kanban board from the wall board", async () => {
    setup([makeIssue("b", "backlog")]);

    fireEvent.click(await screen.findByRole("button", { name: "Open the board" }));

    expect(await screen.findByRole("dialog", { name: "Board" })).toBeTruthy();
  });

  it("seats each worker in the room its agent sits in", async () => {
    setup([makeIssue("a", "todo", "agent-1"), makeIssue("b", "todo", "agent-2")], {
      agents: [makeAgent("agent-1", "planning"), makeAgent("agent-2", undefined)],
    });

    await vi.waitFor(() => expect(engine.rooms.get("agent-1")).toBe(2));
    expect(engine.rooms.get("agent-2")).toBe(3);
  });

  it("hands the board back for any grouping other than status", () => {
    const { store } = setup([makeIssue("a", "todo")], { grouping: "assignee" });

    expect(screen.getByText(/Group by Status to see the office/)).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Group by Status" }));

    expect(store.getState().grouping).toBe("status");
  });
});
