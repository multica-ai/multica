import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, screen, act } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ChatPlacement } from "@multica/cerebro-feature-flags";

// Mutable feature-flag + placement the mocks read; seeded per test.
const flagState = vi.hoisted(() => ({ enabled: true }));
const placementState = vi.hoisted(() => ({
  value: {
    channel: { chat: true, inbox: false },
    dm: { chat: true, inbox: false },
    agent_chat: { chat: true, inbox: false },
  } as ChatPlacement,
}));

// Captures the last showSectionControls the rail was rendered with, plus the
// FIR-4350 create/settings handlers.
const railProps = vi.hoisted(() => ({
  showSectionControls: undefined as unknown,
  flush: undefined as unknown,
  onCreate: undefined as undefined | (() => void),
  onOpenSettings: undefined as undefined | (() => void),
}));
const openModal = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));
vi.mock("@multica/ui/hooks/use-mobile", () => ({ useIsMobile: () => false }));

// Resizable panels render their children inline in jsdom.
vi.mock("@multica/ui/components/ui/resizable", () => ({
  ResizablePanelGroup: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  ResizablePanel: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  ResizableHandle: () => null,
}));

const setSettings = vi.hoisted(() => vi.fn());
vi.mock("@multica/cerebro-feature-flags", () => ({
  useFeatureFlag: () => flagState.enabled,
  showsInChat: (placement: ChatPlacement, kind: keyof ChatPlacement) =>
    placement[kind].chat,
  useChatPlacement: () => ({ placement: placementState.value, setPlacement: () => {} }),
  useChatPageSettings: () => ({
    settings: {
      limit: 10,
      sort: "recent",
      unreadFirst: true,
      groupBy: "type",
      searchDefaultOpen: false,
    },
    setSettings,
  }),
  ChatPlacementSettings: () => <div data-testid="placement-settings" />,
}));

vi.mock("@multica/core/modals", () => ({
  useModalStore: Object.assign(() => undefined, {
    getState: () => ({ open: openModal }),
  }),
}));

// The rail exposes the actions the tests need: open a NEW agent chat, and
// capture the showSectionControls / create / settings props.
vi.mock("@multica/cerebro-inbox-slack-block", () => ({
  SlackBlock: (props: {
    showSectionControls?: boolean;
    flush?: boolean;
    onCreate?: () => void;
    onOpenSettings?: () => void;
    onOpenAgentChat: (id: string) => void;
  }) => {
    railProps.showSectionControls = props.showSectionControls;
    railProps.flush = props.flush;
    railProps.onCreate = props.onCreate;
    railProps.onOpenSettings = props.onOpenSettings;
    return (
      <div data-testid="rail">
        <button type="button" onClick={() => props.onOpenAgentChat("agent-1")}>
          open-new-chat
        </button>
        <button type="button" onClick={() => props.onCreate?.()}>
          rail-create
        </button>
        <button type="button" onClick={() => props.onOpenSettings?.()}>
          rail-settings
        </button>
      </div>
    );
  },
}));

vi.mock("@multica/views/channels", () => ({
  ChannelDetail: () => <div data-testid="channel-detail" />,
}));

// The panel shows its current sessionId and can report a freshly created one.
vi.mock("@multica/views/inbox/components/inbox-list-item", () => ({
  InboxChatPanel: (props: {
    sessionId: string | null;
    onSessionCreated?: (id: string) => void;
  }) => (
    <div>
      <span data-testid="panel-session">{props.sessionId ?? "none"}</span>
      <button type="button" onClick={() => props.onSessionCreated?.("sess-99")}>
        create-session
      </button>
    </div>
  ),
}));

import { ChatPage } from "./chat-page";

const CHAT_ONLY: ChatPlacement = {
  channel: { chat: true, inbox: false },
  dm: { chat: true, inbox: false },
  agent_chat: { chat: true, inbox: false },
};
const NONE_IN_CHAT: ChatPlacement = {
  channel: { chat: false, inbox: true },
  dm: { chat: false, inbox: true },
  agent_chat: { chat: false, inbox: true },
};

describe("ChatPage", () => {
  beforeEach(() => {
    flagState.enabled = true;
    placementState.value = CHAT_ONLY;
    railProps.showSectionControls = undefined;
  });

  it("renders nothing when the feature flag is off", () => {
    flagState.enabled = false;
    const { container } = render(<ChatPage />);
    expect(container).toBeEmptyDOMElement();
  });

  it("keeps the created session so the next send continues it", async () => {
    const user = userEvent.setup();
    await act(async () => {
      render(<ChatPage />);
    });

    // Open a brand-new agent chat → panel starts with no session.
    await user.click(screen.getByText("open-new-chat"));
    expect(screen.getByTestId("panel-session")).toHaveTextContent("none");

    // The panel creates the session and reports it; the page must adopt it so
    // the panel re-renders on that session instead of staying on "new".
    await user.click(screen.getByText("create-session"));
    expect(screen.getByTestId("panel-session")).toHaveTextContent("sess-99");
  });

  // FIR-4350 design review — the page names itself. Before this, only the
  // mobile branch rendered a PageHeader, so on desktop the Chat page opened
  // into a bare two-pane split with nothing on it saying "Chat".
  it("renders a page header with the page name on desktop", async () => {
    await act(async () => {
      render(<ChatPage />);
    });
    expect(screen.getByRole("heading", { name: "Chat" })).toBeInTheDocument();
  });

  // FIR-4350 design review — the rail is this page's left column, not a card
  // floating inside it, so it is rendered flush (no rounded border, no
  // drag-handle inset, pinned header).
  it("renders the rail flush instead of as an inbox card", async () => {
    await act(async () => {
      render(<ChatPage />);
    });
    expect(railProps.flush).toBe(true);
  });

  it("shows an empty-rail hint with a settings button when nothing is in Chat", async () => {
    placementState.value = NONE_IN_CHAT;
    await act(async () => {
      render(<ChatPage />);
    });
    expect(screen.queryByTestId("rail")).not.toBeInTheDocument();
    expect(screen.getByText(/Nothing is placed in Chat yet/)).toBeInTheDocument();
    // FIR-4350 — the empty rail opens the placement settings on the page. The
    // button carries the same name as the dialog it opens ("Chat placement").
    await userEvent.setup().click(screen.getByText("Chat placement"));
    expect(screen.getByTestId("placement-settings")).toBeInTheDocument();
  });

  it("shows the rail's display-settings controls", async () => {
    await act(async () => {
      render(<ChatPage />);
    });
    // FIR-4350 — the Chat page carries the same Sort/Group/Show/Search controls
    // the Inbox's Chat box has.
    expect(railProps.showSectionControls).toBe(true);
  });

  // FIR-4350 — the "+" on the rail opens the new-conversation modal.
  it("opens the new-conversation modal from the rail create button", async () => {
    const user = userEvent.setup();
    await act(async () => {
      render(<ChatPage />);
    });
    await user.click(screen.getByText("rail-create"));
    expect(openModal).toHaveBeenCalledWith("new-message");
  });

  // FIR-4350 — the rail settings gear opens the placement matrix on the page.
  it("opens the placement settings from the rail settings button", async () => {
    const user = userEvent.setup();
    await act(async () => {
      render(<ChatPage />);
    });
    await user.click(screen.getByText("rail-settings"));
    expect(screen.getByTestId("placement-settings")).toBeInTheDocument();
  });
});
