// @vitest-environment jsdom

import { StrictMode } from "react";
import { describe, expect, it, vi } from "vitest";
import { act, render } from "@testing-library/react";
import { NavigationProvider } from "../../navigation";
import type { NavigationAdapter } from "../../navigation";
import {
  CONVERSATION_STARTERS_FOCUS_FLASH_MS,
  useConversationStartersFocus,
} from "./use-conversation-starters-focus";

function Harness({ agentId }: { agentId: string }) {
  const { ref, focused } = useConversationStartersFocus(agentId, true);
  return <div ref={ref} data-focused={focused ? "true" : "false"} />;
}

// The "customize" link in a chat's empty state lands on General settings with
// ?focus=. The page must bring the editor into view and then clear the param,
// so a refresh or a later tab switch does not replay the flash.
describe("useConversationStartersFocus", () => {
  // A fresh adapter object per render — what the web platform actually hands
  // down, and the condition the flash-timer regression below depends on.
  const adapter = (search: string, replace = vi.fn()): NavigationAdapter => ({
    push: vi.fn(),
    replace,
    back: vi.fn(),
    pathname: "/acme/agents/agent-1",
    searchParams: new URLSearchParams(search),
    hash: "",
    getShareableUrl: (path: string) => `https://app.test${path}`,
  });

  function renderWithSearch(search: string) {
    const replace = vi.fn();
    const scrollIntoView = vi.fn();
    // jsdom has no layout, so the method does not exist at all.
    Element.prototype.scrollIntoView = scrollIntoView;
    const tree = (value: NavigationAdapter, agentId: string) => (
      <NavigationProvider value={value}>
        <Harness agentId={agentId} />
      </NavigationProvider>
    );
    const { rerender, container } = render(tree(adapter(search, replace), "agent-1"));
    return {
      replace,
      scrollIntoView,
      focused: () =>
        container.querySelector("[data-focused]")?.getAttribute("data-focused") ===
        "true",
      /**
       * Re-render as the platform does: a new adapter object every time, and
       * optionally a different agent for an in-place navigation between two
       * agent pages on the same route.
       */
      settleUrl: (next: string, agentId = "agent-1") =>
        rerender(tree(adapter(next, replace), agentId)),
    };
  }

  it("scrolls to the editor and drops the focus param, keeping the view", () => {
    const { replace, scrollIntoView, focused } = renderWithSearch(
      "view=general&focus=conversation_starters",
    );

    expect(scrollIntoView).toHaveBeenCalledWith({ block: "nearest" });
    expect(replace).toHaveBeenCalledWith("/acme/agents/agent-1?view=general");
    expect(focused()).toBe(true);
  });

  // Regression: the flash timer must not hang off the effect's cleanup. The
  // navigation adapter is not referentially stable, so React would run that
  // cleanup on the very next render and cancel the timeout, leaving the ring
  // on the editor permanently.
  it("ends the flash instead of ringing the editor forever", () => {
    vi.useFakeTimers();
    try {
      const { settleUrl, focused } = renderWithSearch(
        "view=general&focus=conversation_starters",
      );
      expect(focused()).toBe(true);

      act(() => settleUrl("view=general"));
      act(() => {
        vi.advanceTimersByTime(CONVERSATION_STARTERS_FOCUS_FLASH_MS + 400);
      });

      expect(focused()).toBe(false);
    } finally {
      vi.useRealTimers();
    }
  });

  // Regression: StrictMode replays mount → unmount → mount. The latch keeps
  // the replay from focusing twice, so the flash timer must not be one the
  // simulated unmount cancels for good.
  it("ends the flash under StrictMode", () => {
    vi.useFakeTimers();
    try {
      Element.prototype.scrollIntoView = vi.fn();
      const { container } = render(
        <StrictMode>
          <NavigationProvider value={adapter("view=general&focus=conversation_starters")}>
            <Harness agentId="agent-1" />
          </NavigationProvider>
        </StrictMode>,
      );
      const focused = () =>
        container.querySelector("[data-focused]")?.getAttribute("data-focused");
      expect(focused()).toBe("true");

      act(() => {
        vi.advanceTimersByTime(CONVERSATION_STARTERS_FOCUS_FLASH_MS + 400);
      });

      expect(focused()).toBe("false");
    } finally {
      vi.useRealTimers();
    }
  });

  // Regression: the guard against re-firing before the stripped URL lands must
  // not become a one-shot-per-mount latch. A chat window left open beside this
  // page can send the very same link again, and it has to land again.
  it("focuses again when the link is clicked a second time", () => {
    const { replace, scrollIntoView, settleUrl } = renderWithSearch(
      "view=general&focus=conversation_starters",
    );
    expect(scrollIntoView).toHaveBeenCalledTimes(1);

    act(() => settleUrl("view=general"));
    act(() => settleUrl("view=general&focus=conversation_starters"));

    expect(scrollIntoView).toHaveBeenCalledTimes(2);
    expect(replace).toHaveBeenCalledTimes(2);
  });

  // Regression: the same guard is keyed by agent, so navigating between two
  // agent pages on this route does not swallow the second agent's link.
  it("focuses a link aimed at a different agent", () => {
    const { scrollIntoView, settleUrl } = renderWithSearch(
      "view=general&focus=conversation_starters",
    );
    expect(scrollIntoView).toHaveBeenCalledTimes(1);

    act(() => settleUrl("view=general&focus=conversation_starters", "agent-2"));

    expect(scrollIntoView).toHaveBeenCalledTimes(2);
  });

  it("ignores an ordinary visit", () => {
    const { replace, scrollIntoView } = renderWithSearch("view=general");

    expect(scrollIntoView).not.toHaveBeenCalled();
    expect(replace).not.toHaveBeenCalled();
  });
});
