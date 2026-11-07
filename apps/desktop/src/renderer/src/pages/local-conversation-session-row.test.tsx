import { act, cleanup, render, screen, within } from "@testing-library/react";
import type { ComponentProps, ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ConversationListModel } from "@multica/conversation-client/models";
import { localV2SummarySchema } from "@multica/core/local-conversation/v2";
import { LocalConversationListModel } from "../platform/local-conversation-list-model";
import { LocalConversationSessionRow } from "./local-conversation-session-row";

const counters = vi.hoisted(() => ({ links: new Map<string, number>(), activity: vi.fn() }));
vi.mock("@multica/views/navigation", () => ({
  AppLink: ({ href, children, ...props }: ComponentProps<"a">) => {
    const id = new URL(href!, "http://test").searchParams.get("session")!;
    counters.links.set(id, (counters.links.get(id) ?? 0) + 1);
    return <a {...props} href={href}>{children}</a>;
  },
}));
vi.mock("@multica/views/runtimes/provider-logo", () => ({ ProviderLogo: () => null }));
vi.mock("@multica/ui/components/ui/context-menu", () => ({ ContextMenuTrigger: ({ children }: { children: ReactNode }) => <div>{children}</div> }));
vi.mock("@multica/ui/components/ui/dropdown-menu", () => ({
  DropdownMenu: ({ children }: { children: ReactNode }) => { counters.activity(); return <>{children}</>; },
  DropdownMenuTrigger: () => null,
  DropdownMenuContent: () => null,
}));
afterEach(() => { cleanup(); counters.links.clear(); counters.activity.mockClear(); });

const summary = (id: string, seq = 1, preview = "", updatedAt = "2026-11-04T00:00:00Z") => localV2SummarySchema.parse({
  conversation_id: id, summary_seq: seq, provider: "codex", title: id, status: "idle", pinned: false,
  archived: false, turn_count: 1, updated_at: updatedAt, last_message_preview: preview,
});

it("updates only selected rows and lets a preview update bypass activity controls", () => {
  const list = new ConversationListModel();
  list.upsert([summary("a"), summary("b"), summary("c")]);
  const model = new LocalConversationListModel(list);
  const common: Omit<ComponentProps<typeof LocalConversationSessionRow>, "id" | "selected"> = {
    model, sessionLinks: { current: new Map() }, flowPath: "/test/conversations",
    select: vi.fn(), updatingId: null, labels: { pinned: "Pinned", storage: "Saved locally" },
    projectLabels: { actions: "Actions" },
    t: ((selector: (messages: object) => string) => selector({ window: { unread: "Unread" } })) as ComponentProps<typeof LocalConversationSessionRow>["t"],
    renderSessionActions: () => <></>, setContextSelection: vi.fn(), chinese: false, remote: true,
    v2: { list } as ComponentProps<typeof LocalConversationSessionRow>["v2"],
  };
  function Page({ selected }: { selected: string }) {
    return <>{["a", "b", "c"].map((id) => <LocalConversationSessionRow {...common} key={id} id={id} selected={selected === id} />)}</>;
  }
  const app = render(<Page selected="a" />);
  counters.links.clear();
  app.rerender(<Page selected="b" />);
  expect(Object.fromEntries(counters.links)).toEqual({ a: 1, b: 1 });
  counters.links.clear(); counters.activity.mockClear();
  act(() => list.upsert([summary("a", 2, "New preview")]));
  expect(Object.fromEntries(counters.links)).toEqual({ a: 1 });
  expect(counters.activity).not.toHaveBeenCalled();
  counters.links.clear();
  act(() => list.upsert([summary("a", 3, "New preview", "2026-11-05T00:00:00Z")]));
  expect(counters.links.size).toBe(0);
  expect(counters.activity).not.toHaveBeenCalled();
  act(() => list.completions.set("b", {
    version: 1, generation: "g", baselineTurnIndex: 0, summarySeq: 2, turnCount: 1, activeTurnId: null, status: "idle",
    completedTurnIndex: 0, completedTurnId: "t0", completedSeq: 1, readSeq: 0, readTurnIndex: -1,
  }));
  expect(screen.getByRole("status")).toHaveClass("bg-info");
  act(() => list.completions.set("b", { ...list.completions.get("b")!, readTurnIndex: 0, readSeq: 1 }));
  expect(screen.queryByRole("status")).toBeNull();
});

it.each([false, true])("defers unread completion indicators until all work stops (remote: %s)", (remote) => {
  const list = new ConversationListModel();
  let seq = 1;
  list.upsert([summary("a"), summary("b")]);
  const completion = {
    version: 1 as const, generation: "g", baselineTurnIndex: 0, summarySeq: 1, turnCount: 1,
    activeTurnId: null, status: "idle", completedTurnIndex: 0, completedTurnId: "t0", completedSeq: 1, readSeq: 0, readTurnIndex: -1,
  };
  list.completions.set("a", completion);
  list.completions.set("b", completion);
  const model = new LocalConversationListModel(list);
  render(<>{["a", "b"].map((id) => <LocalConversationSessionRow
    key={id} model={model} id={id} selected={false} sessionLinks={{ current: new Map() }}
    flowPath="/test/conversations" select={vi.fn()} updatingId={null}
    labels={{ pinned: "Pinned", storage: "Saved locally" }} projectLabels={{ actions: "Actions" }}
    t={((selector: (messages: object) => string) => selector({
      window: { unread: "Unread" }, desktop: { local_conversation: {
        waiting_for_answer: "Waiting for answer", waiting_for_answer_label: "Answer question",
      } },
    })) as ComponentProps<typeof LocalConversationSessionRow>["t"]}
    renderSessionActions={() => <></>} setContextSelection={vi.fn()} chinese={false} remote={remote}
    v2={{ list } as ComponentProps<typeof LocalConversationSessionRow>["v2"]}
  />)}</>);
  const row = (id: string) => within(screen.getByRole("link", { name: new RegExp(`^Codex\\s*${id}`) }).parentElement!);
  const unread = () => row("a").queryByRole("status", { name: "Unread" });
  const update = (status: string, backgroundTasks = 0, pendingInteractions = 0) => act(() => list.upsert([{
    ...summary("a", ++seq), status, background_tasks: backgroundTasks, pending_interactions: pendingInteractions,
  }]));
  expect(unread()).toBeInTheDocument();
  update("running");
  expect(unread()).toBeNull();
  expect(row("a").getByLabelText("Conversation running: a")).toBeInTheDocument();
  expect(list.completions.pending("a")).toEqual(completion);
  // A queued turn can finish while another turn is already running.
  act(() => list.completions.set("a", { ...completion, completedTurnIndex: 1, completedTurnId: "t1", completedSeq: 2 }));
  expect(unread()).toBeNull();
  update("running", 0, 1);
  expect(row("a").getByRole("button", { name: "Answer question" })).toBeInTheDocument();
  expect(unread()).toBeNull();
  update("idle", 1);
  expect(row("a").getByRole("status", { name: "Background tasks: a (1)" })).toBeInTheDocument();
  expect(unread()).toBeNull();
  update("idle");
  expect(unread()).toBeInTheDocument();
  expect(list.completions.pending("a")?.completedTurnId).toBe("t1");
  act(() => list.completions.set("a", { ...list.completions.get("a")!, readTurnIndex: 1, readSeq: 2 }));
  expect(unread()).toBeNull();
  update("running");
  update("idle");
  expect(unread()).toBeNull();
  expect(row("b").getByRole("status", { name: "Unread" })).toBeInTheDocument();
});

describe.each(["claude", "codex"])("%s failure status", (provider) => {
  it("shows an existing failure after list reload and follows live updates while unselected", () => {
    const list = new ConversationListModel();
    let seq = 0;
    const update = (status: string, backgroundTasks = 0) => list.upsert([{
      ...summary("unselected", ++seq, "Previous successful reply"), provider, status, background_tasks: backgroundTasks,
    }]);
    update("failed");
    render(<LocalConversationSessionRow
      model={new LocalConversationListModel(list)} id="unselected" selected={false}
      sessionLinks={{ current: new Map() }} flowPath="/test/conversations" select={vi.fn()}
      updatingId={null} labels={{ pinned: "Pinned", storage: "Saved locally" }} projectLabels={{ actions: "Actions" }}
      t={((selector: (messages: object) => string) => selector({
        window: { unread: "Unread" }, workbench: { run_status: { failed: "Failed" } },
      })) as ComponentProps<typeof LocalConversationSessionRow>["t"]}
      renderSessionActions={() => <></>} setContextSelection={vi.fn()} chinese={false} remote
      v2={{ list } as ComponentProps<typeof LocalConversationSessionRow>["v2"]}
    />);
    const failure = () => screen.queryByRole("status", { name: "Failed: unselected" });
    expect(failure()).toHaveClass("text-destructive");
    expect(screen.getByRole("link")).toHaveClass("pr-16");
    expect(screen.getByRole("link")).not.toHaveAttribute("aria-current");

    act(() => update("running"));
    expect(failure()).toBeNull();
    expect(screen.getByLabelText("Conversation running: unselected")).toBeInTheDocument();
    act(() => update("idle"));
    expect(failure()).toBeNull();

    act(() => update("failed", 1));
    expect(failure()).toBeInTheDocument();
    expect(screen.getByRole("link")).toHaveClass("pr-24");
    expect(screen.getByRole("status", { name: "Background tasks: unselected (1)" })).toBeInTheDocument();
    // A late summary must not erase the newer failure.
    act(() => list.upsert([{ ...summary("unselected", 1), provider, status: "idle" }]));
    expect(failure()).toBeInTheDocument();

    act(() => update("running"));
    // The daemon uses idle after either success or user cancellation.
    act(() => update("idle"));
    expect(failure()).toBeNull();
    act(() => update("future-status"));
    expect(failure()).toBeNull();
  });
});
