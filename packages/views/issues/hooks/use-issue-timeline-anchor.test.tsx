import type { ReactNode } from "react";
import { renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ScrollRestorationProvider } from "../../platform/scroll-restoration";
import { useIssueTimelineAnchor } from "./use-issue-timeline-anchor";

const saved = { commentId: "comment-1", offset: 40 };

function wrapper({ children }: { children: ReactNode }) {
  return (
    <ScrollRestorationProvider
      adapter={{
        get: () => undefined,
        getViewState: (key) => (key === "timeline-anchor:issue-1" ? JSON.stringify(saved) : undefined),
        setViewState: () => {},
      }}
    >
      {children}
    </ScrollRestorationProvider>
  );
}

function render(initial: { ready: boolean; skipRestore: boolean }) {
  return renderHook(
    (props: { ready: boolean; skipRestore: boolean }) =>
      useIssueTimelineAnchor({ containerKey: "issue-1", container: null, ...props }),
    { initialProps: initial, wrapper },
  );
}

describe("useIssueTimelineAnchor", () => {
  it("restores the reading position when a landing turns out to have no target", () => {
    const { result, rerender } = render({ ready: false, skipRestore: true });
    expect(result.current.pending).toBeNull();

    rerender({ ready: true, skipRestore: false });
    expect(result.current.pending).toEqual(saved);
  });

  it("drops the reading position for a landing that had a target, even once it is consumed", () => {
    const { result, rerender } = render({ ready: false, skipRestore: true });
    rerender({ ready: true, skipRestore: true });
    expect(result.current.pending).toBeNull();

    rerender({ ready: true, skipRestore: false });
    expect(result.current.pending).toBeNull();
  });

  it("restores the reading position when nothing lands", () => {
    const { result } = render({ ready: true, skipRestore: false });
    expect(result.current.pending).toEqual(saved);
  });
});
