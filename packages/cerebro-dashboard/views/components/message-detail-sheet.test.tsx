// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { NavigationProvider, type NavigationAdapter } from "@multica/views/navigation";
import { MessageDetailSheet } from "./message-detail-sheet";

vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQuery: () => ({
    data: {
      messages: [
        {
          id: "message-1",
          role: "user",
          content: "Dashboard eval message for detail flow",
          created_at: "2026-07-15T10:00:00Z",
          sender_name: "Jesper",
          agent_name: "Tine",
        },
      ],
      cost_cents: 0,
    },
    isLoading: false,
  }),
}));

afterEach(cleanup);

describe("MessageDetailSheet", () => {
  it("opens the full conversation through the shared app navigation", () => {
    const push = vi.fn();
    const adapter: NavigationAdapter = {
      push,
      replace: vi.fn(),
      back: vi.fn(),
      pathname: "/firtal/dashboard",
      searchParams: new URLSearchParams(),
      getShareableUrl: (path) => `https://app.test${path}`,
    };

    render(
      <NavigationProvider value={adapter}>
        <MessageDetailSheet
          wsId="workspace-1"
          workspaceSlug="firtal"
          currentUserId="member-1"
          onClose={vi.fn()}
          message={{
            id: "message-1",
            content: "Dashboard eval message for detail flow",
            created_at: "2026-07-15T10:00:00Z",
            sender_id: "member-1",
            sender_name: "Jesper",
            agent_id: "agent-1",
            agent_name: "Tine",
            session_id: "session-1",
          }}
        />
      </NavigationProvider>,
    );

    const link = screen.getByRole("link", { name: "Open full conversation in Inbox" });
    expect(link.getAttribute("href")).toBe("/firtal/inbox?chat=session-1");
    expect(screen.getByRole("dialog").className).toContain("data-[side=right]:w-full");

    fireEvent.click(link);
    expect(push).toHaveBeenCalledWith("/firtal/inbox?chat=session-1");
  });

  it("does not promise an Inbox conversation owned by another member", () => {
    const adapter: NavigationAdapter = {
      push: vi.fn(),
      replace: vi.fn(),
      back: vi.fn(),
      pathname: "/firtal/dashboard",
      searchParams: new URLSearchParams(),
      getShareableUrl: (path) => `https://app.test${path}`,
    };

    render(
      <NavigationProvider value={adapter}>
        <MessageDetailSheet
          wsId="workspace-1"
          workspaceSlug="firtal"
          currentUserId="member-1"
          onClose={vi.fn()}
          message={{
            id: "message-2",
            content: "Another member's conversation",
            created_at: "2026-07-15T10:00:00Z",
            sender_id: "member-2",
            sender_name: "Nikolaj",
            agent_id: "agent-1",
            agent_name: "Tine",
            session_id: "session-2",
          }}
        />
      </NavigationProvider>,
    );

    expect(screen.queryByRole("link", { name: "Open full conversation in Inbox" })).toBeNull();
    expect(screen.getByText("Only Nikolaj can open this conversation in Inbox.")).toBeTruthy();
  });
});
