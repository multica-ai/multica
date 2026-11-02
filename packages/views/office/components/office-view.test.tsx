/**
 * @vitest-environment jsdom
 *
 * The office is the pixtuoid engine on a canvas. jsdom has no WebAssembly office,
 * so the engine is replaced by a stub that records the sessions it is given and
 * reports a tag for each seated worker. These tests cover what the view decides:
 * who sits where (home squad, floor), the tag each worker shows, and which panel a click opens.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ViewStoreProvider } from "@multica/core/issues/stores/view-store-context";
import { getIssueSurfaceViewStore } from "@multica/core/issues/stores/surface-view-store";
import { IssuePeekActionsContext } from "../../issues/surface/peek-context";
import type { Issue, IssueStatus, Squad } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { IssueContextMenuProvider } from "../../issues/actions/issue-actions-context-menu";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { officeKeys, proposalKeys, useOfficeFloorStore, type OfficeFloor, type Proposal } from "@multica/core/office";
import { useModalStore } from "@multica/core/modals";
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

// The brief has its own tests; here it is not under test.
vi.mock("./office-brief-button", () => ({ OfficeBriefButton: () => null }));

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

// The decisions dialog has its own tests; here it only reports what it was given.
vi.mock("./office-decisions-dialog", () => ({
  OfficeDecisionsDialog: ({ open, proposals }: { open: boolean; proposals: { id: string }[] }) =>
    open ? <div role="dialog" aria-label="Decisions">{`decisions ${proposals.length}`}</div> : null,
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

const FLOORS: OfficeFloor[] = [
  { id: "f1", workspace_id: "ws-1", name: "Floor 1", position: 0, created_at: "", updated_at: "" },
  { id: "f2", workspace_id: "ws-1", name: "Floor 2", position: 1, created_at: "", updated_at: "" },
];

function makeSquad(id: string, leader: string, seat?: { floor: string; slot: number }): Squad {
  return {
    id,
    name: `Squad ${id}`,
    leader_id: leader,
    archived_at: null,
    floor_id: seat?.floor ?? null,
    office_slot: seat?.slot ?? null,
  } as Squad;
}

function makeAgent(id: string, home: string | null): Agent {
  return { id, name: `Agent ${id}`, home_squad_id: home, archived_at: null } as Agent;
}

function pendingProposal(squadId: string, id = "p1"): Proposal {
  return { id, squad_id: squadId, state: "pending", title: "A proposal", issue_identifier: "MUL-1" } as Proposal;
}

const SQUADS = [makeSquad("s1", "agent-1", { floor: "f1", slot: 0 }), makeSquad("s2", "agent-3", { floor: "f2", slot: 2 })];
const AGENTS = [makeAgent("agent-1", "s1"), makeAgent("agent-2", "s1"), makeAgent("agent-3", "s2"), makeAgent("agent-4", null)];

describe("OfficeView", () => {
  let queryClient: QueryClient;

  beforeEach(() => {
    engine.starts.length = 0;
    engine.working.clear();
    engine.ends.length = 0;
    engine.rooms.clear();
    useOfficeFloorStore.setState({ lastFloorId: null });
    useModalStore.getState().close();
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
    options: {
      grouping?: "status" | "assignee";
      open?: (issueId: string) => void;
      agents?: Agent[];
      squads?: Squad[];
      floors?: OfficeFloor[];
      role?: string;
      proposals?: Proposal[];
    } = {},
  ) {
    queryClient.setQueryData(workspaceKeys.agents("ws-1"), options.agents ?? AGENTS);
    queryClient.setQueryData(workspaceKeys.squads("ws-1"), options.squads ?? SQUADS);
    queryClient.setQueryData(officeKeys.floors("ws-1"), options.floors ?? FLOORS);
    queryClient.setQueryData(workspaceKeys.members("ws-1"), [{ user_id: "viewer-1", role: options.role ?? "owner" }]);
    queryClient.setQueryData(proposalKeys.listFiltered("ws-1", { state: "pending" }), options.proposals ?? []);
    for (const squad of options.squads ?? SQUADS) {
      queryClient.setQueryData(
        officeKeys.squadMembers("ws-1", squad.id),
        (options.agents ?? AGENTS)
          .filter((agent) => agent.home_squad_id === squad.id)
          .map((agent) => ({ member_type: "agent", member_id: agent.id })),
      );
    }
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

  it("seats each agent in its home squad's room, with or without an issue", async () => {
    setup([]);

    await vi.waitFor(() => expect(engine.starts).toEqual(["agent-1", "agent-2"]));
    expect(engine.rooms.get("agent-1")).toBe(0);
    expect(engine.rooms.get("agent-2")).toBe(0);
  });

  it("does not seat agents of another floor or agents without a home squad", async () => {
    setup([makeIssue("a", "todo", "agent-4"), makeIssue("b", "todo", "agent-3")]);

    await screen.findAllByRole("button", { name: "Waiting for work" });
    expect(screen.queryByRole("button", { name: "Todo" })).toBeNull();
    expect(engine.starts).toEqual(["agent-1", "agent-2"]);
  });

  it("shows the floor last viewed", async () => {
    useOfficeFloorStore.setState({ lastFloorId: "f2" });
    setup([]);

    await vi.waitFor(() => expect(engine.starts).toEqual(["agent-3"]));
    expect(engine.rooms.get("agent-3")).toBe(2);
  });

  it("falls back to the first floor when the remembered one is gone", async () => {
    useOfficeFloorStore.setState({ lastFloorId: "deleted" });
    setup([]);

    await vi.waitFor(() => expect(engine.starts).toEqual(["agent-1", "agent-2"]));
  });

  it("switches floors from the tabs and remembers the choice", async () => {
    setup([]);
    await vi.waitFor(() => expect(engine.starts).toEqual(["agent-1", "agent-2"]));

    fireEvent.click(screen.getByRole("tab", { name: "Floor 2" }));

    await vi.waitFor(() => expect(engine.starts).toContain("agent-3"));
    expect(useOfficeFloorStore.getState().lastFloorId).toBe("f2");
    expect(screen.getByRole("tab", { name: "Floor 2" }).getAttribute("aria-selected")).toBe("true");
  });

  it("offers the owner Seat all for squads without a room, and lists them in the tray", async () => {
    setup([], { squads: [...SQUADS, makeSquad("loose", "agent-4")] });

    expect(await screen.findByText("1 squad has no room")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Seat all" })).toBeTruthy();
    expect(screen.getByRole("button", { name: /Not on a floor/ })).toBeTruthy();
  });

  it("keeps Seat all for the owner or an admin", async () => {
    setup([], { squads: [...SQUADS, makeSquad("loose", "agent-4")], role: "member" });

    await screen.findByRole("button", { name: /Not on a floor/ });
    expect(screen.queryByRole("button", { name: "Seat all" })).toBeNull();
  });

  it("shows a first-run card, not an empty floor, while no squad is seated", async () => {
    setup([], { squads: [], agents: [] });

    expect(await screen.findByText("No squads in the office")).toBeTruthy();
    expect(document.querySelector("canvas")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Start from a template" }));
    expect(useModalStore.getState().modal).toBe("squad-quickstart");
  });

  it("tags the leader of a squad with a proposal waiting as Needs you, ahead of the issue status", async () => {
    setup([makeIssue("c", "in_progress", "agent-1")], { proposals: [pendingProposal("s1")] });

    expect(await screen.findByRole("button", { name: "Needs you" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "In Progress" })).toBeNull();
  });

  it("counts the pending proposals over the room of the squad that filed them", async () => {
    setup([], { proposals: [pendingProposal("s1"), pendingProposal("s1", "p2")] });

    const badge = await screen.findByRole("button", { name: "2 decisions waiting" });
    fireEvent.click(badge);

    expect(await screen.findByRole("dialog", { name: "Decisions" })).toBeTruthy();
    expect(screen.getByText("decisions 2")).toBeTruthy();
  });

  it("shows waiting for work over an agent with no open issue", async () => {
    setup([makeIssue("d", "done", "agent-2")]);

    expect((await screen.findAllByRole("button", { name: "Waiting for work" })).length).toBe(2);
    expect(screen.queryByRole("button", { name: "Done" })).toBeNull();
  });

  it("shows the status of the agent's issue in progress over its head", async () => {
    setup([makeIssue("a", "todo", "agent-1"), makeIssue("c", "in_progress", "agent-1")]);

    expect(await screen.findByRole("button", { name: "In Progress" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Todo" })).toBeNull();
  });

  it("shows a squad's issue over the squad leader", async () => {
    const squadIssue = { ...makeIssue("q", "in_review"), assignee_type: "squad", assignee_id: "s1" } as Issue;
    setup([squadIssue]);

    expect(await screen.findByRole("button", { name: "In Review" })).toBeTruthy();
  });

  it("opens the agent's panel on its current issue when its tag is clicked", async () => {
    setup([makeIssue("c", "in_progress", "agent-1")]);

    fireEvent.click(await screen.findByRole("button", { name: "In Progress" }));

    expect(screen.getByText("panel agent-1 c")).toBeTruthy();
  });

  it("opens the same panel, on its chat, when an idle agent's tag is clicked", async () => {
    setup([makeIssue("d", "done", "agent-1")]);

    const tags = await screen.findAllByRole("button", { name: "Waiting for work" });
    fireEvent.click(tags[0]!);

    expect(screen.getByText("panel agent-1 chat")).toBeTruthy();
  });

  it("opens the panel on the chat when the worker itself is clicked", async () => {
    setup([makeIssue("c", "in_progress", "agent-1")]);
    await screen.findByRole("button", { name: "In Progress" });
    const canvas = document.querySelector("canvas") as HTMLCanvasElement;
    // The tag is anchored at (20, 30) in layout units; the figure hangs below it.
    canvas.getBoundingClientRect = () => ({ left: 0, top: 0, width: 416, height: 171 }) as DOMRect;

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
    expect(engine.ends).toEqual([]);
  });

  it("keeps an agent seated when its issue goes away", async () => {
    const { rerender } = setup([makeIssue("a", "todo", "agent-1")]);
    await screen.findByRole("button", { name: "Todo" });

    rerender([makeIssue("a", "todo")]);

    await vi.waitFor(() => expect(screen.queryByRole("button", { name: "Todo" })).toBeNull());
    expect(engine.ends).toEqual([]);
  });

  it("names the rooms after their squads and offers to create one in an empty room", async () => {
    setup([]);

    expect(await screen.findByText("Squad s1")).toBeTruthy();
    expect(screen.getByText("Empty room")).toBeTruthy();

    fireEvent.click(await screen.findByRole("button", { name: "Create squad" }));

    expect(useModalStore.getState().modal).toBe("create-squad");
  });

  it("opens the kanban board from the wall board", async () => {
    setup([makeIssue("b", "backlog")]);

    fireEvent.click(await screen.findByRole("button", { name: "Open the board" }));

    expect(await screen.findByRole("dialog", { name: "Board" })).toBeTruthy();
  });

  it("hands the board back for any grouping other than status", () => {
    const { store } = setup([makeIssue("a", "todo")], { grouping: "assignee" });

    expect(screen.getByText(/Group by Status to see the office/)).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Group by Status" }));

    expect(store.getState().grouping).toBe("status");
  });
});
