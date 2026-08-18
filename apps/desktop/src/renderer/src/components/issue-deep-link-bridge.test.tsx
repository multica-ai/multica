import { describe, expect, it, vi, beforeEach } from "vitest";
import { render } from "@testing-library/react";
import { IssueDeepLinkBridge } from "./issue-deep-link-bridge";

const state = vi.hoisted(() => ({
  push: vi.fn(),
}));

vi.mock("@multica/views/navigation", () => ({
  useNavigation: () => ({ push: state.push }),
}));

type IssuePayload = { slug: string; issueId: string };

/** Stand-in for the preload bridge, exposing the payload main would send. */
function installDesktopAPI() {
  const unsubscribe = vi.fn();
  let listener: ((payload: IssuePayload) => void) | null = null;
  const onIssueOpen = vi.fn((callback: (payload: IssuePayload) => void) => {
    listener = callback;
    return unsubscribe;
  });
  Object.defineProperty(window, "desktopAPI", {
    configurable: true,
    value: { onIssueOpen },
  });
  return {
    unsubscribe,
    onIssueOpen,
    deliver: (payload: IssuePayload) => listener?.(payload),
  };
}

describe("IssueDeepLinkBridge", () => {
  beforeEach(() => {
    state.push.mockReset();
  });

  it("pushes the issue route named by the deep link", () => {
    const api = installDesktopAPI();
    render(<IssueDeepLinkBridge />);

    api.deliver({ slug: "acme", issueId: "MUL-123" });

    expect(state.push).toHaveBeenCalledWith("/acme/issues/MUL-123");
  });

  it("encodes an issue id that is not URL-safe", () => {
    const api = installDesktopAPI();
    render(<IssueDeepLinkBridge />);

    api.deliver({ slug: "acme", issueId: "MUL 1/2" });

    expect(state.push).toHaveBeenCalledWith("/acme/issues/MUL%201%2F2");
  });

  it("ignores a payload missing either identifier", () => {
    const api = installDesktopAPI();
    render(<IssueDeepLinkBridge />);

    api.deliver({ slug: "", issueId: "MUL-1" });
    api.deliver({ slug: "acme", issueId: "" });

    expect(state.push).not.toHaveBeenCalled();
  });

  it("subscribes once and unsubscribes on unmount", () => {
    const api = installDesktopAPI();
    const { unmount } = render(<IssueDeepLinkBridge />);

    expect(api.onIssueOpen).toHaveBeenCalledTimes(1);
    expect(api.unsubscribe).not.toHaveBeenCalled();

    unmount();

    expect(api.unsubscribe).toHaveBeenCalledTimes(1);
  });
});
