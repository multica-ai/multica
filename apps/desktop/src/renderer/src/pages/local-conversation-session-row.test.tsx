import { act, cleanup, render, screen } from "@testing-library/react";
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
    completedTurnIndex: 0, completedTurnId: "t0", readTurnIndex: -1,
  }));
  expect(screen.getByRole("status")).toHaveClass("bg-info");
  act(() => list.completions.set("b", { ...list.completions.get("b")!, readTurnIndex: 0 }));
  expect(screen.queryByRole("status")).toBeNull();
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
