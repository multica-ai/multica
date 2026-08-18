// @vitest-environment jsdom

import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

const state = vi.hoisted(() => ({ isOpen: false, toggle: vi.fn() }));

vi.mock("@multica/core/chat", () => ({
  useChatStore: Object.assign(
    (selector: (value: { isOpen: boolean; toggle: () => void }) => unknown) =>
      selector({ isOpen: state.isOpen, toggle: state.toggle }),
    { getState: () => ({ isOpen: state.isOpen, toggle: state.toggle }) },
  ),
}));

vi.mock("@multica/core/chat/queries", () => ({
  chatSessionsOptions: () => ({ queryKey: ["chat-sessions"] }),
  countUnreadChatSessions: () => 0,
  hasPendingChatTasksOptions: () => ({ queryKey: ["pending-chat-tasks"] }),
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: [] }),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-a" }));
vi.mock("@multica/core/shortcuts", () => ({ useShortcut: () => null }));
vi.mock("@multica/core/logger", () => ({
  createLogger: () => ({ info: vi.fn() }),
}));
vi.mock("../../i18n", () => ({ useT: () => ({ t: () => "Open chat" }) }));
vi.mock("../../common/shortcut-keycaps", () => ({ ShortcutKeycaps: () => null }));
vi.mock("@multica/ui/components/ui/tooltip", () => ({
  Tooltip: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipTrigger: (props: React.ComponentProps<"button">) => <button {...props} />,
  TooltipContent: ({ children }: { children: React.ReactNode }) => <span>{children}</span>,
}));

import { ChatFab } from "./chat-fab";

afterEach(cleanup);

describe("ChatFab", () => {
  it("opens Chat when the launcher is activated", () => {
    render(<ChatFab />);

    const launcher = screen.getByRole("button", { name: "Open chat" });
    launcher.click();

    expect(state.toggle).toHaveBeenCalledOnce();
  });

  it("returns focus to the launcher when the open Chat window is collapsed", () => {
    const view = render(<ChatFab />);
    const firstLauncher = screen.getByRole("button", { name: "Open chat" });
    expect(document.activeElement).not.toBe(firstLauncher);

    state.isOpen = true;
    view.rerender(<ChatFab />);
    state.isOpen = false;
    view.rerender(<ChatFab />);

    expect(document.activeElement).toBe(
      screen.getByRole("button", { name: "Open chat" }),
    );
  });
});
