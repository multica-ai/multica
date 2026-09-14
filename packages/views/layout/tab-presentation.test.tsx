import { describe, it, expect, vi, beforeEach } from "vitest";
import type { ReactNode } from "react";
import { render, renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { projectDetailOptions } from "@multica/core/projects/queries";
import { chatSessionsOptions } from "@multica/core/chat/queries";
import {
  inboxListOptions,
  archivedInboxListOptions,
} from "@multica/core/inbox/queries";
import { agentListOptions } from "@multica/core/workspace/queries";
import { runtimeListOptions } from "@multica/core/runtimes/queries";

// Mutable workspace stub so a test can simulate "workspace not resolved yet".
const ws = vi.hoisted(() => ({ current: { id: "ws1", slug: "acme" } as { id: string; slug: string } | null }));

vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useCurrentWorkspace: () => ws.current,
}));

vi.mock("../i18n", async () => {
  const layout = (await import("../locales/en/layout.json")).default;
  const chat = (await import("../locales/en/chat.json")).default;
  const bundles: Record<string, unknown> = { layout, chat };
  return {
    useT: (ns: string) => ({
      t: (select: (b: Record<string, unknown>) => string) =>
        select(bundles[ns] as Record<string, unknown>),
    }),
  };
});

// ActorAvatar reaches into workspace directory queries; the hook returns a
// descriptor (not the rendered avatar), so the render test stubs it.
vi.mock("../common/actor-avatar", () => ({
  ActorAvatar: ({ actorType, actorId }: { actorType: string; actorId: string }) => (
    <span data-testid="actor-avatar" data-actor={`${actorType}:${actorId}`} />
  ),
}));

import { useTabPresentation, ResourceLeadingVisual } from "./tab-presentation";

function makeClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

function seed(qc: QueryClient) {
  qc.setQueryData(issueDetailOptions("ws1", "i1").queryKey, {
    id: "i1",
    identifier: "MUL-1",
    title: "Fix login",
    status: "in_progress",
  } as never);
  qc.setQueryData(issueDetailOptions("ws1", "i9").queryKey, {
    id: "i9",
    identifier: "MUL-9",
    title: "Crash",
    status: "todo",
  } as never);
  qc.setQueryData(projectDetailOptions("ws1", "p1").queryKey, {
    id: "p1",
    icon: "🚀",
    title: "Apollo",
  } as never);
  qc.setQueryData(chatSessionsOptions("ws1").queryKey, [
    { id: "s1", title: "Deploy plan", status: "active" },
    { id: "s2", title: "  ", status: "active" },
  ] as never);
  qc.setQueryData(inboxListOptions("ws1").queryKey, [
    { id: "n1", issue_id: "i9", title: "Assigned to you", type: "issue_assigned" },
    { id: "n2", issue_id: null, title: "Quick create failed", type: "quick_create_failed" },
  ] as never);
  // Archived list is a distinct cache; these items are NOT in the main list.
  qc.setQueryData(archivedInboxListOptions("ws1").queryKey, [
    { id: "a1", issue_id: "i1", title: "Old assignment", type: "issue_assigned" },
    { id: "a2", issue_id: null, title: "Archived note", type: "quick_create_failed" },
  ] as never);
  qc.setQueryData(agentListOptions("ws1").queryKey, [
    { id: "ag1", name: "Robby", avatar_url: null },
  ] as never);
  qc.setQueryData(runtimeListOptions("ws1").queryKey, [
    { id: "rt1", name: "Claude (host)", custom_name: "Prod Box", status: "online" },
  ] as never);
}

function presentationOf(url: string, fallback?: string) {
  const qc = makeClient();
  seed(qc);
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
  return renderHook(() => useTabPresentation(url, fallback), { wrapper }).result
    .current;
}

beforeEach(() => {
  ws.current = { id: "ws1", slug: "acme" };
});

describe("useTabPresentation — live from cache", () => {
  it("page: page icon + localized page name", () => {
    expect(presentationOf("/acme/issues")).toEqual({
      visual: { kind: "icon", icon: "ListTodo" },
      title: "Issues",
    });
  });

  it("issue: live status glyph + identifier:title", () => {
    expect(presentationOf("/acme/issues/i1")).toEqual({
      // `category` travels with the visual so the tab strip never resolves a
      // custom status key itself. (MUL-6243)
      visual: { kind: "issue-status", status: "in_progress", category: "in_progress" },
      title: "MUL-1: Fix login",
    });
  });

  it("project: own icon + title", () => {
    expect(presentationOf("/acme/projects/p1")).toEqual({
      visual: { kind: "project-icon", icon: "🚀" },
      title: "Apollo",
    });
  });

  it("actor: avatar visual + resolved name", () => {
    expect(presentationOf("/acme/agents/ag1")).toEqual({
      visual: { kind: "actor", actorType: "agent", id: "ag1" },
      title: "Robby",
    });
  });

  it("machine tab uses the runtime's custom name over the raw daemon name", () => {
    expect(presentationOf("/acme/runtimes/rt1")).toEqual({
      visual: { kind: "icon", icon: "Monitor" },
      title: "Prod Box",
    });
  });

  it("chat container: MessageSquare icon + session title", () => {
    expect(presentationOf("/acme/chat?session=s1")).toEqual({
      visual: { kind: "icon", icon: "MessageSquare" },
      title: "Deploy plan",
    });
  });

  it("chat container: blank session title uses the New chat fallback", () => {
    expect(presentationOf("/acme/chat?session=s2").title).toBe("New chat");
  });

  it("chat container: no session stays Chat", () => {
    expect(presentationOf("/acme/chat")).toEqual({
      visual: { kind: "icon", icon: "MessageSquare" },
      title: "Chat",
    });
  });

  it("inbox container: selected issue shows Inbox icon + issue title", () => {
    // Selection key is issue_id ?? id — n1 links issue i9.
    expect(presentationOf("/acme/inbox?issue=i9")).toEqual({
      visual: { kind: "icon", icon: "Inbox" },
      title: "MUL-9: Crash",
    });
  });

  it("inbox container: selected non-issue shows its display title", () => {
    expect(presentationOf("/acme/inbox?issue=n2")).toEqual({
      visual: { kind: "icon", icon: "Inbox" },
      title: "Quick create failed",
    });
  });

  it("issue-backed selection resolves by object, in either view", () => {
    // The selection key IS the group key (`issue_id ?? id`), so an
    // issue-backed selection names the issue directly and is answered by the
    // issue cache — no inbox list involved. That holds in both views on
    // purpose: i1's notification lives only in the ARCHIVED list, yet the main
    // view still titles the tab by the object the URL names. Treating "not in
    // the list I loaded" as "does not exist" is what this must not do — the
    // page itself resolves such a link to the issue rather than dropping it
    // (see the deep-link fallback in inbox-page).
    expect(presentationOf("/acme/inbox?issue=i1")).toEqual({
      visual: { kind: "icon", icon: "Inbox" },
      title: "MUL-1: Fix login",
    });
    expect(presentationOf("/acme/inbox?view=archived&issue=i1")).toEqual({
      visual: { kind: "icon", icon: "Inbox" },
      title: "MUL-1: Fix login",
    });
  });

  it("issue-backed selection survives a cold inbox list", () => {
    // Regression (MUL-6967): the badge no longer pre-warms the inbox list at
    // app start, so a restored tab may have the issue cached but no list. It
    // must still show the issue, not collapse to the container label.
    const qc = makeClient();
    seed(qc);
    qc.removeQueries({ queryKey: inboxListOptions("ws1").queryKey });
    qc.removeQueries({ queryKey: archivedInboxListOptions("ws1").queryKey });
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={qc}>{children}</QueryClientProvider>
    );

    const { result } = renderHook(
      () => useTabPresentation("/acme/inbox?issue=i9", "MUL-9: Crash"),
      { wrapper },
    );

    expect(result.current.title).toBe("MUL-9: Crash");
  });

  it("falls back to the persisted title while a selection is unresolved", () => {
    // Nothing cached for this key yet — an issue-less notification whose row
    // has not loaded. The persisted title is a better first frame than
    // "Inbox"; without it the tab loses its identity until the page loads.
    const qc = makeClient();
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={qc}>{children}</QueryClientProvider>
    );

    const { result } = renderHook(
      () => useTabPresentation("/acme/inbox?issue=n7", "Autopilot paused"),
      { wrapper },
    );

    expect(result.current.title).toBe("Autopilot paused");
  });

  it("uses the container label when nothing is selected, persisted title or not", () => {
    // The fallback is for a PENDING identity only. An Inbox tab with no
    // selection is fully resolved — it really is just "Inbox", and a stale
    // persisted title must not override it.
    expect(presentationOf("/acme/inbox", "MUL-9: Crash").title).toBe("Inbox");
  });

  it("archived inbox: selected non-issue resolves against the archived list", () => {
    expect(presentationOf("/acme/inbox?view=archived&issue=a2")).toEqual({
      visual: { kind: "icon", icon: "Inbox" },
      title: "Archived note",
    });
  });

  it("attachment: filename from ?name= drives the title and file icon", () => {
    expect(presentationOf("/acme/attachments/att1/preview?name=diagram.png")).toEqual({
      visual: { kind: "icon", icon: "FileImage" },
      title: "diagram.png",
    });
    // Missing filename → generic File + localized "Attachment".
    expect(presentationOf("/acme/attachments/att1/preview")).toEqual({
      visual: { kind: "icon", icon: "File" },
      title: "Attachment",
    });
  });
});

describe("useTabPresentation — pending / fallback", () => {
  it("pending issue keeps the issue-status slot and uses the persisted fallback", () => {
    expect(presentationOf("/acme/issues/unloaded", "MUL-7: Prior")).toEqual({
      visual: { kind: "issue-status", status: null },
      title: "MUL-7: Prior",
    });
  });

  it("pending issue with no fallback shows the localized type label, not Issues", () => {
    const p = presentationOf("/acme/issues/unloaded");
    expect(p.visual).toEqual({ kind: "issue-status", status: null });
    expect(p.title).toBe("Issue");
  });

  it("unknown route is neutral, never Issues", () => {
    expect(presentationOf("/acme/mystery")).toEqual({
      visual: { kind: "icon", icon: "FileQuestion" },
      title: "Unknown page",
    });
  });

  it("actor falls back to a type icon before the workspace resolves", () => {
    ws.current = null;
    expect(presentationOf("/acme/agents/ag1").visual).toEqual({
      kind: "icon",
      icon: "Bot",
    });
  });
});

describe("ResourceLeadingVisual", () => {
  it("renders a project icon with its emoji", () => {
    const { getByText } = render(
      <ResourceLeadingVisual visual={{ kind: "project-icon", icon: "🚀" }} />,
    );
    expect(getByText("🚀")).toBeTruthy();
  });

  it("renders a status glyph for an issue", () => {
    const { container } = render(
      <ResourceLeadingVisual visual={{ kind: "issue-status", status: "done" }} />,
    );
    expect(container.querySelector("svg")).toBeTruthy();
  });

  it("renders the actor avatar for an actor visual", () => {
    const { getByTestId } = render(
      <ResourceLeadingVisual
        visual={{ kind: "actor", actorType: "agent", id: "ag1" }}
      />,
    );
    expect(getByTestId("actor-avatar").getAttribute("data-actor")).toBe("agent:ag1");
  });

  it("renders a lucide icon for a plain icon visual", () => {
    const { container } = render(
      <ResourceLeadingVisual visual={{ kind: "icon", icon: "Inbox" }} />,
    );
    expect(container.querySelector("svg")).toBeTruthy();
  });
});
