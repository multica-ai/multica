// @vitest-environment jsdom

import { useLayoutEffect, useRef } from "react";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { Markdown } from "@multica/ui/markdown";
import { useHighlightCommentScroll } from "@multica/cerebro-ui/hooks/use-highlight-comment-scroll";

afterEach(cleanup);

function rect(top: number): DOMRect {
  return {
    x: 0,
    y: top,
    top,
    right: 600,
    bottom: top + 20,
    left: 0,
    width: 600,
    height: 20,
    toJSON: () => ({}),
  };
}

function InboxTargetHarness({
  identifierResolved,
  autolink = true,
}: {
  identifierResolved: boolean;
  autolink?: boolean;
}) {
  const scrollRef = useRef<HTMLDivElement>(null);
  useHighlightCommentScroll("trigger", scrollRef, 10_000, 500);

  useLayoutEffect(() => {
    const container = scrollRef.current;
    const target = document.getElementById("comment-trigger");
    if (!container || !target) return;

    container.getBoundingClientRect = () => rect(0);
    target.getBoundingClientRect = () => rect(100 - container.scrollTop);
  }, [autolink, identifierResolved]);

  return (
    <div ref={scrollRef} data-testid="scroll-container">
      <Markdown
        autolinkIssueIdentifiers={autolink}
        renderMention={({ id }) =>
          identifierResolved ? (
            <a data-testid="resolved-issue" href={`/issues/${id}`}>
              {id}
            </a>
          ) : (
            id
          )
        }
      >
        FIR-123
      </Markdown>
      <div id="comment-trigger">Trigger</div>
    </div>
  );
}

describe("Inbox trigger scroll stability", () => {
  it("stays anchored when identifier autolinking is disabled", async () => {
    const view = render(
      <InboxTargetHarness identifierResolved={false} autolink={false} />,
    );
    const container = screen.getByTestId("scroll-container");
    const target = document.getElementById("comment-trigger");

    await waitFor(() => expect(container.scrollTop).toBe(100));
    view.rerender(<InboxTargetHarness identifierResolved autolink={false} />);

    expect(target?.getBoundingClientRect().top).toBe(0);
  });

  it("keeps the triggering item anchored when an issue identifier resolves above it", async () => {
    const view = render(<InboxTargetHarness identifierResolved={false} />);
    const container = screen.getByTestId("scroll-container");
    const target = document.getElementById("comment-trigger");

    await waitFor(() => expect(container.scrollTop).toBe(100));
    expect(target?.getBoundingClientRect().top).toBe(0);

    view.rerender(<InboxTargetHarness identifierResolved />);

    expect(screen.getByTestId("resolved-issue")).not.toBeNull();
    expect(target?.getBoundingClientRect().top).toBe(0);
  });
});
