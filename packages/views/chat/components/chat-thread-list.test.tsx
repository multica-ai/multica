import { describe, expect, it, vi, beforeEach } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import type { Agent, ChatSession } from "@multica/core/types";
import enChat from "../../locales/en/chat.json";
import enIssues from "../../locales/en/issues.json";

// --- Mocks ------------------------------------------------------------------
// The archive-advance behavior is the unit under test: archiving the chat that
// is currently open should move the selection to the next chat in the list.
// setActiveSession is the observable side effect; setArchived just flips status.

const setActiveSession = vi.fn();
const archiveMutate = vi.fn();

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: ({ actorId }: { actorId: string }) => (
    <span data-testid={`avatar-${actorId}`} />
  ),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/agents", () => ({
  useWorkspacePresenceMap: () => ({ byAgent: new Map() }),
}));

vi.mock("@multica/core/api", () => ({
  api: { cancelTaskById: vi.fn() },
}));

vi.mock("@multica/core/chat", () => ({
  useChatStore: (selector: (s: { setActiveSession: typeof setActiveSession }) => unknown) =>
    selector({ setActiveSession }),
}));

vi.mock("@multica/core/chat/mutations", () => ({
  useDeleteChatSession: () => ({ mutate: vi.fn(), isPending: false }),
  useSetChatSessionPinned: () => ({ mutate: vi.fn(), isPending: false }),
  useSetChatSessionArchived: () => ({ mutate: archiveMutate, isPending: false }),
}));

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useQuery: () => ({ data: { tasks: [] } }),
    useQueryClient: () => ({ setQueryData: vi.fn(), invalidateQueries: vi.fn() }),
  };
});

import { ChatThreadList } from "./chat-thread-list";

const TEST_RESOURCES = { en: { chat: enChat, issues: enIssues } };

function makeSession(overrides: Partial<ChatSession> & Pick<ChatSession, "id">): ChatSession {
  return {
    workspace_id: "ws-1",
    agent_id: "agent-1",
    creator_id: "user-1",
    title: `Chat ${overrides.id}`,
    status: "active",
    has_unread: false,
    unread_count: 0,
    last_message: null,
    pinned: false,
    created_at: new Date(0).toISOString(),
    updated_at: new Date(0).toISOString(),
    ...overrides,
  };
}

const agent = { id: "agent-1", name: "Alpha" } as unknown as Agent;

// Sessions are sorted by most-recent activity (updated_at) first, pinned first.
// Give them descending timestamps so the rendered order is s1, s2, s3.
const sessions: ChatSession[] = [
  makeSession({ id: "s1", updated_at: "2026-07-08T03:00:00Z" }),
  makeSession({ id: "s2", updated_at: "2026-07-08T02:00:00Z" }),
  makeSession({ id: "s3", updated_at: "2026-07-08T01:00:00Z" }),
];

function renderList(activeSessionId: string | null) {
  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <ChatThreadList
        sessions={sessions}
        agents={[agent]}
        activeSessionId={activeSessionId}
        onSelectSession={vi.fn()}
      />
    </I18nProvider>,
  );
}

const ARCHIVE_LABEL = enChat.list.archive;

describe("ChatThreadList archive advance", () => {
  beforeEach(() => {
    setActiveSession.mockClear();
    archiveMutate.mockClear();
  });

  it("advances selection to the next chat when archiving the open one", () => {
    renderList("s2");
    // Each row exposes an "Archive" hover action; archive the one in view (s2).
    const archiveButtons = screen.getAllByRole("button", { name: ARCHIVE_LABEL });
    // Rows render in order s1, s2, s3 → the second archive button is s2's.
    fireEvent.click(archiveButtons[1]!);

    expect(setActiveSession).toHaveBeenCalledWith("s3");
    expect(archiveMutate).toHaveBeenCalledWith({ sessionId: "s2", archived: true });
  });

  it("falls back to the previous chat when archiving the last open one", () => {
    renderList("s3");
    const archiveButtons = screen.getAllByRole("button", { name: ARCHIVE_LABEL });
    fireEvent.click(archiveButtons[2]!);

    expect(setActiveSession).toHaveBeenCalledWith("s2");
    expect(archiveMutate).toHaveBeenCalledWith({ sessionId: "s3", archived: true });
  });

  it("clears the selection when archiving the only chat", () => {
    render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <ChatThreadList
          sessions={[makeSession({ id: "only" })]}
          agents={[agent]}
          activeSessionId="only"
          onSelectSession={vi.fn()}
        />
      </I18nProvider>,
    );
    fireEvent.click(screen.getByRole("button", { name: ARCHIVE_LABEL }));

    expect(setActiveSession).toHaveBeenCalledWith(null);
    expect(archiveMutate).toHaveBeenCalledWith({ sessionId: "only", archived: true });
  });

  it("leaves the selection untouched when archiving a chat that is not open", () => {
    renderList("s1");
    const archiveButtons = screen.getAllByRole("button", { name: ARCHIVE_LABEL });
    // Archive s3 while s1 is the open one.
    fireEvent.click(archiveButtons[2]!);

    expect(setActiveSession).not.toHaveBeenCalled();
    expect(archiveMutate).toHaveBeenCalledWith({ sessionId: "s3", archived: true });
  });
});
