// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  createChatStore,
  registerChatStore,
  useChatStore,
} from "@multica/core/chat";

const navigation = vi.hoisted(() => ({ pathname: "/acme/issues" }));

vi.mock("../navigation", () => ({
  useNavigation: () => ({ pathname: navigation.pathname }),
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ chat: () => "/acme/chat" }),
}));

vi.mock("./components/chat-window", () => ({
  ChatWindow: () => {
    const isOpen = useChatStore((state) => state.isOpen);
    const activeSessionId = useChatStore((state) => state.activeSessionId);
    return isOpen ? (
      <section aria-label="Floating conversation">
        {activeSessionId ?? "New conversation"}
      </section>
    ) : null;
  },
}));

vi.mock("./components/chat-fab", () => ({
  ChatFab: () => {
    const isOpen = useChatStore((state) => state.isOpen);
    const toggle = useChatStore((state) => state.toggle);
    return isOpen ? null : <button onClick={toggle}>Open chat</button>;
  },
}));

import { FloatingChat } from "./floating-chat";

function memoryStorage() {
  const values = new Map<string, string>();
  return {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => values.set(key, value),
    removeItem: (key: string) => values.delete(key),
  };
}

beforeEach(() => {
  navigation.pathname = "/acme/issues";
  registerChatStore(createChatStore({ storage: memoryStorage() }));
});

afterEach(cleanup);

describe("FloatingChat shared-store integration", () => {
  it("suppresses the overlay on /chat and restores the same open session after navigation", () => {
    const view = render(<FloatingChat />);

    useChatStore.getState().setActiveSession("session-one");
    fireEvent.click(screen.getByRole("button", { name: "Open chat" }));
    expect(screen.getByRole("region", { name: "Floating conversation" }).textContent).toBe(
      "session-one",
    );

    navigation.pathname = "/acme/chat";
    view.rerender(<FloatingChat />);
    expect(screen.queryByRole("region", { name: "Floating conversation" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Open chat" })).toBeNull();

    navigation.pathname = "/acme/issues";
    view.rerender(<FloatingChat />);
    expect(screen.getByRole("region", { name: "Floating conversation" }).textContent).toBe(
      "session-one",
    );
  });
});
