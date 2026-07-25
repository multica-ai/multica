import { act, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { configStore } from "@multica/core/config";

vi.mock("@multica/core/chat", () => ({
  useChatStore: (selector: (state: { floatingChatEnabled: boolean }) => unknown) =>
    selector({ floatingChatEnabled: true }),
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ chat: () => "/lifeos/chat" }),
}));

vi.mock("../navigation", () => ({
  useNavigation: () => ({ pathname: "/lifeos/issues" }),
}));

vi.mock("./components/chat-window", () => ({
  ChatWindow: () => <div>chat-window</div>,
}));

vi.mock("./components/chat-fab", () => ({
  ChatFab: () => <button>chat-fab</button>,
}));

import { FloatingChat } from "./floating-chat";

afterEach(() => {
  act(() => configStore.setState({ localMode: false }));
});

describe("FloatingChat", () => {
  it("renders on a regular workspace", () => {
    act(() => configStore.setState({ localMode: false }));
    render(<FloatingChat />);
    expect(screen.getByText("chat-window")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "chat-fab" })).toBeInTheDocument();
  });

  it("does not create a second AI conversation surface in LifeOS local mode", () => {
    act(() => configStore.setState({ localMode: true }));
    render(<FloatingChat />);
    expect(screen.queryByText("chat-window")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "chat-fab" })).not.toBeInTheDocument();
  });
});
