import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

const mockClose = vi.hoisted(() => vi.fn());
const mockPush = vi.hoisted(() => vi.fn());
const mockSetSelectedAgentId = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/modals", () => ({
  useModalStore: (selector: (state: { close: typeof mockClose }) => unknown) =>
    selector({ close: mockClose }),
}));

vi.mock("@multica/core/chat", () => ({
  useChatStore: {
    getState: () => ({ setSelectedAgentId: mockSetSelectedAgentId }),
  },
}));

vi.mock("../navigation", () => ({
  useNavigation: () => ({ push: mockPush }),
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    issueDetail: (id: string) => `/ws-test/issues/${id}`,
    inbox: () => "/ws-test/inbox",
  }),
}));

vi.mock("../channels", () => ({
  NewMessageModal: ({
    onAgentChatStarted,
  }: {
    onAgentChatStarted: (agentId: string) => void;
  }) => (
    <button type="button" onClick={() => onAgentChatStarted("agent-123")}>
      Start agent chat
    </button>
  ),
}));

import { CerebroNewMessageModal } from "./cerebro-new-message";

describe("CerebroNewMessageModal", () => {
  beforeEach(() => {
    mockClose.mockClear();
    mockPush.mockClear();
    mockSetSelectedAgentId.mockClear();
  });

  it("opens the inbox new-chat panel with the selected agent", async () => {
    render(<CerebroNewMessageModal />);

    await userEvent.click(screen.getByRole("button", { name: "Start agent chat" }));

    expect(mockSetSelectedAgentId).toHaveBeenCalledWith("agent-123");
    expect(mockClose).toHaveBeenCalled();
    expect(mockPush).toHaveBeenCalledWith("/ws-test/inbox?chat=new-chat");
  });
});
