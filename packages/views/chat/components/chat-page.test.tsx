import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

vi.mock("./chat-window", () => ({
  ChatWindow: ({
    variant,
    showSessionHistoryTrigger,
  }: {
    variant?: "floating" | "page";
    showSessionHistoryTrigger?: boolean;
  }) => (
    <div
      data-testid="chat-window"
      data-variant={variant ?? "floating"}
      data-show-session-history-trigger={String(showSessionHistoryTrigger ?? true)}
    >
      Mock chat window
    </div>
  ),
  ChatSessionHistoryPanel: () => (
    <aside data-testid="chat-history-panel">Mock chat history</aside>
  ),
}));

import { ChatPage } from "./chat-page";

describe("ChatPage", () => {
  it("renders the shared chat window in page mode", () => {
    const { container } = render(<ChatPage />);

    expect(screen.getByTestId("chat-window").getAttribute("data-variant")).toBe("page");
    expect(screen.getByTestId("chat-window")).toHaveAttribute(
      "data-show-session-history-trigger",
      "false",
    );
    expect(screen.getByText("Mock chat window")).not.toBeNull();
    expect(screen.getByTestId("chat-history-panel")).not.toBeNull();
    expect(container.innerHTML.includes("rounded-[28px]")).toBe(false);
    expect(container.firstElementChild).toHaveClass("flex-row");
  });
});
