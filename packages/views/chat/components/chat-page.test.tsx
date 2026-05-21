import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render } from "@testing-library/react";

const setOpen = vi.fn();
const replace = vi.fn();
const push = vi.fn();

vi.mock("@multica/core/chat", () => ({
  useChatStore: (sel: (s: { setOpen: (v: boolean) => void }) => unknown) =>
    sel({ setOpen }),
}));

vi.mock("@multica/core/paths", () => ({
  paths: { workspace: (slug: string) => ({ issues: () => `/${slug}/issues` }) },
  useWorkspaceSlug: () => "acme",
}));

vi.mock("../../navigation", () => ({
  useNavigation: () => ({ replace, push, pathname: "/acme/chat" }),
}));

import { ChatPage } from "./chat-page";

describe("ChatPage", () => {
  beforeEach(() => {
    setOpen.mockReset();
    replace.mockReset();
  });

  afterEach(() => {
    Object.defineProperty(window, "innerWidth", {
      configurable: true,
      writable: true,
      value: 1024,
    });
  });

  it("opens the floating chat and redirects to /issues on desktop", () => {
    Object.defineProperty(window, "innerWidth", {
      configurable: true,
      writable: true,
      value: 1280,
    });
    render(<ChatPage />);
    expect(setOpen).toHaveBeenCalledWith(true);
    expect(replace).toHaveBeenCalledWith("/acme/issues");
  });

  it("does nothing on mobile so the bottom-tab tap stays put", () => {
    Object.defineProperty(window, "innerWidth", {
      configurable: true,
      writable: true,
      value: 390,
    });
    render(<ChatPage />);
    expect(setOpen).not.toHaveBeenCalled();
    expect(replace).not.toHaveBeenCalled();
  });
});
