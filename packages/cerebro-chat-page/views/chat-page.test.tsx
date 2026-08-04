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

// Captures the last showSectionControls the rail was rendered with.
const railProps = vi.hoisted(() => ({ showSectionControls: undefined as unknown }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));
vi.mock("@multica/ui/hooks/use-mobile", () => ({ useIsMobile: () => false }));

// Resizable panels render their children inline in jsdom.
vi.mock("@multica/ui/components/ui/resizable", () => ({
  ResizablePanelGroup: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  ResizablePanel: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  ResizableHandle: () => null,
}));

vi.mock("@multica/cerebro-feature-flags", () => ({
  useFeatureFlag: () => flagState.enabled,
  showsInChat: (placement: ChatPlacement, kind: keyof ChatPlacement) =>
    placement[kind].chat,
  useChatPlacement: () => ({ placement: placementState.value, setPlacement: () => {} }),
}));

// The rail exposes two actions the tests need: open a NEW agent chat, and
// capture the showSectionControls prop.
vi.mock("@multica/cerebro-inbox-slack-block", () => ({
  SlackBlock: (props: {
    showSectionControls?: boolean;
    onOpenAgentChat: (id: string) => void;
  }) => {
    railProps.showSectionControls = props.showSectionControls;
    return (
      <div data-testid="rail">
        <button type="button" onClick={() => props.onOpenAgentChat("agent-1")}>
          open-new-chat
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

  it("shows an empty-rail hint naming the settings path when nothing is in Chat", async () => {
    placementState.value = NONE_IN_CHAT;
    await act(async () => {
      render(<ChatPage />);
    });
    expect(screen.queryByTestId("rail")).not.toBeInTheDocument();
    expect(screen.getByText(/Nothing is placed in Chat yet/)).toBeInTheDocument();
    expect(screen.getByText(/Chat page/)).toBeInTheDocument();
  });

  it("hides the rail's section controls", async () => {
    await act(async () => {
      render(<ChatPage />);
    });
    expect(railProps.showSectionControls).toBe(false);
  });
});
