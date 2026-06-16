// @vitest-environment jsdom

import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ChatMessage } from "@multica/core/types";

// Capture the exact props handed to <Virtuoso> so we can assert which keys are
// forwarded. Regression guard: the live chat (rendered in the dashboard layout,
// every page) drives Virtuoso with a large `firstItemIndex` base (1_000_000 −
// older) and does NOT anchor to a top index. Passing `initialTopMostItemIndex`
// as `undefined` still registers the prop, and react-virtuoso then resolves it
// to its default index 0 and anchors there — ~1M rows out of range. That reads
// `.index` off a non-existent sized record and throws on mount ("undefined is
// not an object (evaluating 'e.index')"), white-screening the page. The prop
// must therefore be OMITTED, not passed as undefined.
const virtuosoProps = vi.hoisted(
  () => ({ current: null }) as { current: Record<string, unknown> | null },
);

vi.mock("react-virtuoso", () => ({
  // Capture props only. We deliberately do NOT invoke itemContent/components,
  // keeping the heavy message children out of this prop-contract test.
  Virtuoso: (props: Record<string, unknown>) => {
    virtuosoProps.current = props;
    return <div data-testid="virtuoso" />;
  },
}));

// useT is only exercised by the (unrendered) Header/Footer/item children here.
vi.mock("../../i18n", () => ({
  useT: () => ({ t: () => "" }),
}));

import { ChatMessageList } from "./chat-message-list";

const MESSAGES: ChatMessage[] = [
  {
    id: "m1",
    chat_session_id: "s1",
    role: "user",
    content: "hello",
    task_id: null,
    created_at: "2026-06-16T00:00:00Z",
  },
];

function renderList(props: Parameters<typeof ChatMessageList>[0]) {
  const client = new QueryClient();
  return render(
    <QueryClientProvider client={client}>
      <ChatMessageList {...props} />
    </QueryClientProvider>,
  );
}

afterEach(() => {
  virtuosoProps.current = null;
  cleanup();
});

describe("ChatMessageList Virtuoso props", () => {
  it("omits initialTopMostItemIndex for the live chat (large firstItemIndex)", () => {
    renderList({
      messages: MESSAGES,
      pendingTask: null,
      availability: undefined,
      firstItemIndex: 1_000_000,
    });

    expect(virtuosoProps.current).not.toBeNull();
    // Must be ABSENT — a registered-but-undefined prop is what triggers the
    // out-of-range anchor crash.
    expect(
      Object.prototype.hasOwnProperty.call(
        virtuosoProps.current,
        "initialTopMostItemIndex",
      ),
    ).toBe(false);
  });

  it("forwards initialTopMostItemIndex when a caller anchors the list (terminal run view)", () => {
    renderList({
      messages: MESSAGES,
      pendingTask: null,
      availability: undefined,
      initialTopMostItemIndex: 0,
    });

    expect(virtuosoProps.current?.initialTopMostItemIndex).toBe(0);
  });
});
