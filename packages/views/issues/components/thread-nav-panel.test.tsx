// @vitest-environment jsdom

import { useState, type ReactElement, type ReactNode } from "react";
import { act, cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { TimelineEntry } from "@multica/core/types";
import {
  createShortcutChord,
  useShortcutStore,
} from "@multica/core/shortcuts";
import { renderWithI18n } from "../../test/i18n";
import {
  ThreadNavPanel,
  formatStamp,
  highlightMatches,
  matchesFilter,
  mentionsUser,
  threadDayGroup,
  type ThreadNavThread,
} from "./thread-nav-panel";

type OpenChange = (open: boolean, details: { reason: string }) => void;

const mockState = vi.hoisted(() => ({
  open: false,
  triggerProps: undefined as Record<string, unknown> | undefined,
  onOpenChange: undefined as ((open: boolean, details: { reason: string }) => void) | undefined,
}));

// ActorAvatar resolves workspace-scoped profile paths, which needs a route
// provider this component test has no reason to stand up. The row's identity
// rendering is not what is under test here.
vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: ({ actorId }: { actorId: string }) => (
    <span data-testid={`avatar-${actorId}`} />
  ),
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getActorName: (_type: string, id: string) =>
      ({ "user-1": "Jiayuan", "agent-1": "Lambda" })[id] ?? "Unknown",
    getActorInitials: () => "XX",
    getActorAvatarUrl: () => null,
  }),
}));

// Base UI owns hover timing and focus trapping; those are its tests, not ours.
// The mock keeps the parts this component actually drives — the controlled
// `open` prop, the reason-carrying `onOpenChange`, and the content's keyboard
// and focus handlers — so the open/pin state machine is what gets exercised.
vi.mock("@multica/ui/components/ui/tooltip", () => ({
  Tooltip: ({ children }: { children: ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ render }: { render: ReactElement }) => render,
  TooltipContent: ({ children }: { children: ReactNode }) => (
    <div data-testid="tooltip">{children}</div>
  ),
}));

vi.mock("@multica/ui/components/ui/popover", async () => {
  const React = await vi.importActual<typeof import("react")>("react");
  return {
    Popover: ({
      children,
      open,
      onOpenChange,
    }: {
      children: React.ReactNode;
      open: boolean;
      onOpenChange: OpenChange;
    }) => {
      mockState.open = open;
      mockState.onOpenChange = onOpenChange;
      return <div data-testid="popover">{children}</div>;
    },
    // Base UI's trigger toggles on press and reports the reason; reproduce
    // exactly that so the component's own press handling is under test.
    PopoverTrigger: ({
      render,
      children,
      ...props
    }: {
      render: React.ReactElement;
      children: React.ReactNode;
    } & Record<string, unknown>) => {
      mockState.triggerProps = props;
      return React.cloneElement(
        render as React.ReactElement<Record<string, unknown>>,
        {
          onClick: () =>
            mockState.onOpenChange?.(!mockState.open, { reason: "trigger-press" }),
        },
        children,
      );
    },
    PopoverContent: ({
      children,
      onKeyDown,
      onFocusCapture,
    }: {
      children: React.ReactNode;
      onKeyDown?: React.KeyboardEventHandler;
      onFocusCapture?: React.FocusEventHandler;
    }) =>
      mockState.open ? (
        <div data-testid="panel" onKeyDown={onKeyDown} onFocusCapture={onFocusCapture}>
          {children}
        </div>
      ) : null,
  };
});

function comment(
  id: string,
  content: string,
  overrides: Partial<TimelineEntry> = {},
): TimelineEntry {
  return {
    type: "comment",
    id,
    actor_type: "member",
    actor_id: "user-1",
    created_at: new Date().toISOString(),
    content,
    ...overrides,
  };
}

function thread(
  id: string,
  content: string,
  overrides: Partial<ThreadNavThread> = {},
): ThreadNavThread {
  return {
    id,
    entry: comment(id, content),
    resolved: false,
    replyCount: 0,
    involvesMe: false,
    ...overrides,
  };
}

const THREADS: ThreadNavThread[] = [
  thread("t1", "Invite links require a workspace first"),
  thread("t2", "Move the required check into the service layer", { replyCount: 3 }),
  thread("t3", "Expiry: 7 days or 14 days?", { resolved: true }),
  thread("t4", "Mail template CTA collapses in Outlook", { involvesMe: true }),
];

/** Mirrors how issue-detail owns the panel's open/pinned pair. */
function Harness({
  threads = THREADS,
  onJump = vi.fn(),
  onHoverThread = vi.fn(),
}: {
  threads?: ThreadNavThread[];
  onJump?: (id: string) => void;
  onHoverThread?: (id: string | null) => void;
}) {
  const [state, setState] = useState({ open: false, pinned: false });
  return (
    <ThreadNavPanel
      threads={threads}
      onJump={onJump}
      onHoverThread={onHoverThread}
      open={state.open}
      pinned={state.pinned}
      onOpenChange={(open, pinned) => setState({ open, pinned })}
    />
  );
}

/** Drive the state machine the way Base UI would, with a reason attached. */
function emit(open: boolean, reason: string) {
  act(() => {
    mockState.onOpenChange?.(open, { reason });
  });
}

beforeEach(() => {
  mockState.open = false;
  mockState.triggerProps = undefined;
  mockState.onOpenChange = undefined;
});

afterEach(cleanup);

describe("threadDayGroup", () => {
  const now = new Date("2026-08-05T12:00:00").getTime();

  it("buckets by local calendar day", () => {
    expect(threadDayGroup(new Date("2026-08-05T00:30:00").toISOString(), now)).toBe("today");
    expect(threadDayGroup(new Date("2026-08-04T23:30:00").toISOString(), now)).toBe("yesterday");
    expect(threadDayGroup(new Date("2026-08-03T23:30:00").toISOString(), now)).toBe("earlier");
  });

  it("treats an unparseable timestamp as earlier rather than throwing", () => {
    expect(threadDayGroup("not-a-date", now)).toBe("earlier");
  });
});

describe("formatStamp", () => {
  // The row's stamp and its group header must agree about what day it is. A
  // second "less than 48 hours ago" test disagreed with the calendar-day
  // grouping for most of the day, and an "earlier" row could render a bare
  // clock time with nothing saying which day it belonged to.
  const at = (iso: string) => new Date(iso).toISOString();

  it("shows a clock time under today and yesterday", () => {
    expect(formatStamp(at("2026-08-05T14:20:00"), "today", "en")).toMatch(/\d/);
    expect(formatStamp(at("2026-08-05T14:20:00"), "today", "en")).not.toMatch(/[A-Za-z]{3}/);
    expect(formatStamp(at("2026-08-04T14:20:00"), "yesterday", "en")).not.toMatch(/[A-Za-z]{3}/);
  });

  it("always shows a date under earlier, even when it is under 48 hours old", () => {
    // The regression: 23:41 the day before yesterday is < 48h but groups as
    // "earlier", and used to render as a bare clock time.
    expect(formatStamp(at("2026-08-03T23:41:00"), "earlier", "en")).toMatch(/[A-Za-z]{3}|\d+\/\d+|月/);
  });

  it("formats earlier entries with the selected UI language", () => {
    expect(
      formatStamp(at("2026-08-03T23:41:00"), "earlier", "zh-Hans"),
    ).toContain("月");
  });

  it("returns an empty string for an unparseable timestamp", () => {
    expect(formatStamp("not-a-date", "today", "en")).toBe("");
  });
});

describe("matchesFilter", () => {
  it("splits on resolution and participation", () => {
    const open = thread("a", "x");
    const done = thread("b", "x", { resolved: true });
    const mine = thread("c", "x", { involvesMe: true });

    expect(matchesFilter(open, "all")).toBe(true);
    expect(matchesFilter(open, "unresolved")).toBe(true);
    expect(matchesFilter(done, "unresolved")).toBe(false);
    expect(matchesFilter(done, "resolved")).toBe(true);
    expect(matchesFilter(mine, "mine")).toBe(true);
    expect(matchesFilter(open, "mine")).toBe(false);
  });

  it("keeps every thread for an unknown filter instead of emptying the list", () => {
    expect(matchesFilter(thread("a", "x"), "sideways" as never)).toBe(true);
  });
});

describe("highlightMatches", () => {
  it("returns the text untouched when there is no query", () => {
    expect(highlightMatches("Invite links", "")).toBe("Invite links");
    expect(highlightMatches("Invite links", "   ")).toBe("Invite links");
  });

  it("splits on every occurrence, case-insensitively", () => {
    const parts = highlightMatches("Workspace and workspace_id", "workspace");
    expect(Array.isArray(parts)).toBe(true);
    const marks = (parts as ReactNode[]).filter(
      (p): p is ReactElement<{ children: string }> =>
        typeof p === "object" && p !== null && "type" in p && p.type === "mark",
    );
    expect(marks).toHaveLength(2);
    // Source casing survives — the reader sees their text, not their typing.
    expect(marks[0]!.props.children).toBe("Workspace");
    expect(marks[1]!.props.children).toBe("workspace");
  });

  it("does not drop text before, between, or after matches", () => {
    const parts = highlightMatches("ab X cd X ef", "X") as ReactNode[];
    const text = parts
      .map((p) =>
        typeof p === "string"
          ? p
          : (p as ReactElement<{ children: string }>).props.children,
      )
      .join("");
    expect(text).toBe("ab X cd X ef");
  });
});

describe("mentionsUser", () => {
  it("matches the markdown mention link form", () => {
    expect(mentionsUser("hey [@Jiayuan](mention://member/user-1) look", "user-1")).toBe(true);
  });

  it("matches the legacy shortcode form still sitting in the database", () => {
    // preprocessMarkdown migrates `[@ id=... label=...]` on READ, not before
    // storage, so the timeline hands this straight through.
    expect(mentionsUser('hey [@ id="user-1" label="Jiayuan"] look', "user-1")).toBe(true);
    expect(mentionsUser('[@ id="user-2" label="Wei"]', "user-1")).toBe(false);
  });

  it("does not match another member or an agent mention", () => {
    expect(mentionsUser("[@Wei](mention://member/user-2)", "user-1")).toBe(false);
    expect(mentionsUser("[@Lambda](mention://agent/user-1)", "user-1")).toBe(false);
  });

  it("is false for empty content or a missing user id", () => {
    expect(mentionsUser(undefined, "user-1")).toBe(false);
    expect(mentionsUser("[@x](mention://member/)", "")).toBe(false);
  });
});

describe("ThreadNavPanel", () => {
  it("renders nothing when the issue has no threads", () => {
    renderWithI18n(<Harness threads={[]} />);
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("shows the thread count on the trigger", () => {
    renderWithI18n(<Harness />);
    expect(screen.getByRole("button", { name: /Comment threads \(4\)/ })).toBeTruthy();
  });

  // getShortcut() is a getState() snapshot with no subscription, so a rebind in
  // Settings left the tooltip advertising the old key while the keydown handler
  // already honoured the new one. useShortcut subscribes.
  it("re-renders the shortcut hint when the binding changes", () => {
    renderWithI18n(<Harness />);
    const keys = () =>
      [...screen.getByTestId("tooltip").querySelectorAll("kbd")]
        .map((k) => k.textContent?.trim())
        .join("");
    expect(keys()).toContain("O");

    act(() => {
      useShortcutStore
        .getState()
        .setShortcut("openThreadNav", createShortcutChord("G", { primary: true, shift: true }));
    });
    expect(keys()).toContain("G");
    expect(keys()).not.toContain("O");

    act(() => {
      useShortcutStore.getState().setShortcut("openThreadNav", null);
    });
    // Unbound: the label stays, the keycaps go.
    expect(screen.getByTestId("tooltip").querySelectorAll("kbd")).toHaveLength(0);

    // The store is a module singleton; leaving an override here would follow
    // every later test in this file.
    act(() => {
      useShortcutStore.getState().resetShortcut("openThreadNav");
    });
  });

  it("wires the trigger to open on hover, not only on press", () => {
    renderWithI18n(<Harness />);
    expect(mockState.triggerProps?.openOnHover).toBe(true);
    expect(mockState.triggerProps?.delay).toBeGreaterThan(0);
    expect(mockState.triggerProps?.closeDelay).toBeGreaterThan(0);
  });

  it("lists every thread once open", () => {
    renderWithI18n(<Harness />);
    fireEvent.click(screen.getByRole("button", { name: /Comment threads/ }));
    expect(screen.getByText("Invite links require a workspace first")).toBeTruthy();
    expect(screen.getByText("Expiry: 7 days or 14 days?")).toBeTruthy();
  });

  it("focuses search when opened by a press", async () => {
    renderWithI18n(<Harness />);
    fireEvent.click(screen.getByRole("button", { name: /Comment threads/ }));
    const search = screen.getByPlaceholderText("Search threads or authors");
    await waitFor(() => expect(document.activeElement).toBe(search));
  });

  // The open model is the part of this design that differs from the original
  // "hover shows the list" request, so it carries the most test weight.
  describe("hover previews, press pins", () => {
    it("does not take focus when opened by hover", async () => {
      renderWithI18n(<Harness />);
      const before = document.activeElement;
      emit(true, "trigger-hover");
      expect(screen.getByTestId("panel")).toBeTruthy();
      // A hover preview over a half-written comment must leave the caret alone.
      await waitFor(() => {
        expect(document.activeElement).toBe(before);
      });
    });

    it("closes again when the pointer leaves an unpinned preview", () => {
      renderWithI18n(<Harness />);
      emit(true, "trigger-hover");
      emit(false, "trigger-hover");
      expect(screen.queryByTestId("panel")).toBeNull();
    });

    it("pins instead of closing when the trigger is pressed over a preview", async () => {
      renderWithI18n(<Harness />);
      emit(true, "trigger-hover");
      // Base UI reports a press on an open popover as a close request; over an
      // unpinned preview that means "keep this", not "dismiss".
      emit(false, "trigger-press");
      expect(screen.getByTestId("panel")).toBeTruthy();
      await waitFor(() =>
        expect(document.activeElement).toBe(
          screen.getByPlaceholderText("Search threads or authors"),
        ),
      );
    });

    it("survives the pointer leaving once pinned", () => {
      renderWithI18n(<Harness />);
      fireEvent.click(screen.getByRole("button", { name: /Comment threads/ }));
      emit(false, "trigger-hover");
      expect(screen.getByTestId("panel")).toBeTruthy();
    });

    it("closes a pinned panel on a second press, escape, or outside press", () => {
      const { unmount } = renderWithI18n(<Harness />);
      fireEvent.click(screen.getByRole("button", { name: /Comment threads/ }));
      fireEvent.click(screen.getByRole("button", { name: /Comment threads/ }));
      expect(screen.queryByTestId("panel")).toBeNull();
      unmount();

      for (const reason of ["escape-key", "outside-press"]) {
        renderWithI18n(<Harness />);
        fireEvent.click(screen.getByRole("button", { name: /Comment threads/ }));
        emit(false, reason);
        expect(screen.queryByTestId("panel")).toBeNull();
        cleanup();
      }
    });

    it("upgrades a preview to pinned when focus lands inside it", async () => {
      renderWithI18n(<Harness />);
      emit(true, "trigger-hover");
      fireEvent.focus(screen.getByTestId("panel"));
      // Now pinned: a pointer-out no longer closes it.
      emit(false, "trigger-hover");
      expect(screen.getByTestId("panel")).toBeTruthy();
      await waitFor(() =>
        expect(document.activeElement).toBe(
          screen.getByPlaceholderText("Search threads or authors"),
        ),
      );
    });
  });

  it("filters rows by the search query and reports the match count", () => {
    const { container } = renderWithI18n(<Harness />);
    fireEvent.click(screen.getByRole("button", { name: /Comment threads/ }));
    fireEvent.change(screen.getByPlaceholderText("Search threads or authors"), {
      target: { value: "outlook" },
    });
    // Assert on the row identity, not its text: highlighting splits the title
    // across elements, so a whole-string text query would miss it.
    const rows = [...container.querySelectorAll("[data-thread-id]")].map((el) =>
      el.getAttribute("data-thread-id"),
    );
    expect(rows).toEqual(["t4"]);
    expect(screen.getByText("1 match")).toBeTruthy();
  });

  it("tints the matched term in the title and the excerpt", () => {
    const { container } = renderWithI18n(<Harness />);
    fireEvent.click(screen.getByRole("button", { name: /Comment threads/ }));
    fireEvent.change(screen.getByPlaceholderText("Search threads or authors"), {
      target: { value: "workspace" },
    });
    const marks = [...container.querySelectorAll("mark")];
    expect(marks.length).toBeGreaterThan(0);
    // Case-insensitive match keeps the source casing rather than the query's.
    expect(marks.every((m) => m.textContent?.toLowerCase() === "workspace")).toBe(true);
  });

  it("matches on the author name as well as the content", () => {
    renderWithI18n(<Harness />);
    fireEvent.click(screen.getByRole("button", { name: /Comment threads/ }));
    fireEvent.change(screen.getByPlaceholderText("Search threads or authors"), {
      target: { value: "jiayuan" },
    });
    expect(screen.getByText("4 matches")).toBeTruthy();
  });

  it("narrows to unresolved threads", () => {
    renderWithI18n(<Harness />);
    fireEvent.click(screen.getByRole("button", { name: /Comment threads/ }));
    fireEvent.click(screen.getByRole("button", { name: /^Unresolved/ }));
    expect(screen.queryByText("Expiry: 7 days or 14 days?")).toBeNull();
    expect(screen.getByText("Invite links require a workspace first")).toBeTruthy();
  });

  it("narrows to threads the reader is involved in", () => {
    renderWithI18n(<Harness />);
    fireEvent.click(screen.getByRole("button", { name: /Comment threads/ }));
    fireEvent.click(screen.getByRole("button", { name: /^@me/ }));
    expect(screen.getByText("Mail template CTA collapses in Outlook")).toBeTruthy();
    expect(screen.queryByText("Invite links require a workspace first")).toBeNull();
  });

  it("shows an empty state rather than a blank list when nothing matches", () => {
    renderWithI18n(<Harness />);
    fireEvent.click(screen.getByRole("button", { name: /Comment threads/ }));
    fireEvent.change(screen.getByPlaceholderText("Search threads or authors"), {
      target: { value: "zzzz" },
    });
    expect(screen.getByText("No threads match")).toBeTruthy();
  });

  it("jumps to the clicked thread and closes", () => {
    const onJump = vi.fn();
    renderWithI18n(<Harness onJump={onJump} />);
    fireEvent.click(screen.getByRole("button", { name: /Comment threads/ }));
    fireEvent.click(screen.getByText("Expiry: 7 days or 14 days?"));
    expect(onJump).toHaveBeenCalledWith("t3");
    expect(screen.queryByTestId("panel")).toBeNull();
  });

  it("moves the cursor with arrow keys and jumps on Enter", () => {
    const onJump = vi.fn();
    renderWithI18n(<Harness onJump={onJump} />);
    fireEvent.click(screen.getByRole("button", { name: /Comment threads/ }));
    const search = screen.getByPlaceholderText("Search threads or authors");
    fireEvent.keyDown(search, { key: "ArrowDown" });
    fireEvent.keyDown(search, { key: "Enter" });
    expect(onJump).toHaveBeenCalledWith("t2");
  });

  // Same chords cmdk's vimBindings give the command palette, so the muscle
  // memory carries across every list-with-a-highlight in the product.
  it.each([
    ["n", "t2"],
    ["j", "t2"],
    ["p", "t4"],
    ["k", "t4"],
  ])("moves the cursor on Ctrl+%s", (key, expected) => {
    const onJump = vi.fn();
    renderWithI18n(<Harness onJump={onJump} />);
    fireEvent.click(screen.getByRole("button", { name: /Comment threads/ }));
    const search = screen.getByPlaceholderText("Search threads or authors");
    fireEvent.keyDown(search, { key, ctrlKey: true });
    fireEvent.keyDown(search, { key: "Enter" });
    expect(onJump).toHaveBeenCalledWith(expected);
  });

  it("leaves the letters alone without Ctrl, so they can be typed into search", () => {
    const onJump = vi.fn();
    renderWithI18n(<Harness onJump={onJump} />);
    fireEvent.click(screen.getByRole("button", { name: /Comment threads/ }));
    const search = screen.getByPlaceholderText("Search threads or authors");
    fireEvent.keyDown(search, { key: "n" });
    fireEvent.keyDown(search, { key: "Enter" });
    expect(onJump).toHaveBeenCalledWith("t1");
  });

  it("ignores the aliases when another modifier is held", () => {
    const onJump = vi.fn();
    renderWithI18n(<Harness onJump={onJump} />);
    fireEvent.click(screen.getByRole("button", { name: /Comment threads/ }));
    const search = screen.getByPlaceholderText("Search threads or authors");
    // Ctrl+Shift+N is the browser's incognito window, not our navigation.
    fireEvent.keyDown(search, { key: "n", ctrlKey: true, shiftKey: true });
    fireEvent.keyDown(search, { key: "Enter" });
    expect(onJump).toHaveBeenCalledWith("t1");
  });

  it("wraps the cursor at the top of the list", () => {
    const onJump = vi.fn();
    renderWithI18n(<Harness onJump={onJump} />);
    fireEvent.click(screen.getByRole("button", { name: /Comment threads/ }));
    const search = screen.getByPlaceholderText("Search threads or authors");
    fireEvent.keyDown(search, { key: "ArrowUp" });
    fireEvent.keyDown(search, { key: "Enter" });
    expect(onJump).toHaveBeenCalledWith("t4");
  });

  // The panel used to mark every thread inside the scroll viewport as "you are
  // here". A tall viewport holds several at once, so the marker landed on two or
  // three rows and read as a broken multi-select; absolute position is the
  // rail's job now. Guard against it coming back.
  it("marks no row as a current position", () => {
    const { container } = renderWithI18n(<Harness />);
    fireEvent.click(screen.getByRole("button", { name: /Comment threads/ }));
    expect(screen.queryByText(/you are here/i)).toBeNull();
    expect(container.querySelector(".bg-brand")).toBeNull();
  });

  it("renders the keyboard hints as real keycaps, not glyphs in a string", () => {
    renderWithI18n(<Harness />);
    fireEvent.click(screen.getByRole("button", { name: /Comment threads/ }));
    // Scoped to the footer: the trigger's tooltip renders keycaps too.
    const footer = screen.getByText("select").closest("div")!.parentElement!;
    const keycaps = footer.querySelectorAll('[data-slot="shortcut-keycaps"]');
    // Up, Down, Enter, Escape.
    expect(keycaps).toHaveLength(4);
    expect(screen.getByText("select")).toBeTruthy();
    expect(screen.getByText("jump")).toBeTruthy();
    expect(screen.getByText("close")).toBeTruthy();
  });

  // Enter used to be handled for every keydown bubbling up to the popup, and
  // preventDefault on a button's keydown cancels the click the browser is about
  // to synthesize — so Enter on a filter pill jumped away instead of filtering.
  it("lets Enter activate a focused filter pill instead of jumping", () => {
    const onJump = vi.fn();
    const { container } = renderWithI18n(<Harness onJump={onJump} />);
    fireEvent.click(screen.getByRole("button", { name: /Comment threads/ }));
    const resolved = screen.getByRole("button", { name: /^Resolved/ });
    resolved.focus();
    fireEvent.keyDown(resolved, { key: "Enter" });
    // A real browser turns that keydown into a click on the focused button.
    fireEvent.click(resolved);
    expect(onJump).not.toHaveBeenCalled();
    const rows = [...container.querySelectorAll("[data-thread-id]")].map((el) =>
      el.getAttribute("data-thread-id"),
    );
    expect(rows).toEqual(["t3"]);
  });

  // One cursor, structurally: rows cannot take DOM focus, so focus and the
  // highlight cannot drift apart. Tab reaches the filter pills and then leaves.
  it("keeps rows out of the tab order and points at the active one", () => {
    const { container } = renderWithI18n(<Harness />);
    fireEvent.click(screen.getByRole("button", { name: /Comment threads/ }));
    const rows = [...container.querySelectorAll("[data-thread-id]")];
    expect(rows.length).toBeGreaterThan(0);
    expect(rows.every((r) => r.getAttribute("tabindex") === "-1")).toBe(true);
    expect(rows.every((r) => r.getAttribute("role") === "option")).toBe(true);

    const search = screen.getByPlaceholderText("Search threads or authors");
    expect(search.getAttribute("aria-activedescendant")).toBe(rows[0]!.id);
    expect(rows[0]!.getAttribute("aria-selected")).toBe("true");

    fireEvent.keyDown(search, { key: "ArrowDown" });
    expect(search.getAttribute("aria-activedescendant")).toBe(rows[1]!.id);
    expect(rows[1]!.getAttribute("aria-selected")).toBe("true");
    expect(rows[0]!.getAttribute("aria-selected")).toBe("false");
  });

  // The reviewer's repro: with focus on a child control, an arrow key must not
  // move a highlight that Enter will then ignore.
  it("does not move the highlight from a focused filter pill", () => {
    const onJump = vi.fn();
    const { container } = renderWithI18n(<Harness onJump={onJump} />);
    fireEvent.click(screen.getByRole("button", { name: /Comment threads/ }));
    const search = screen.getByPlaceholderText("Search threads or authors");
    const pill = screen.getByRole("button", { name: /^Unresolved/ });
    pill.focus();

    fireEvent.keyDown(pill, { key: "ArrowDown" });
    fireEvent.keyDown(pill, { key: "n", ctrlKey: true });
    // Highlight untouched — the search field owns it.
    const rows = [...container.querySelectorAll("[data-thread-id]")];
    expect(search.getAttribute("aria-activedescendant")).toBe(rows[0]!.id);

    // And Enter still activates the pill rather than jumping.
    fireEvent.keyDown(pill, { key: "Enter" });
    fireEvent.click(pill);
    expect(onJump).not.toHaveBeenCalled();
  });

  // CJK users commit an IME candidate with Enter. Acting on it jumped away and
  // closed the panel mid-word, which makes the search field unusable for them.
  it("ignores keys while an IME is composing", () => {
    const onJump = vi.fn();
    renderWithI18n(<Harness onJump={onJump} />);
    fireEvent.click(screen.getByRole("button", { name: /Comment threads/ }));
    const search = screen.getByPlaceholderText("Search threads or authors");

    fireEvent.keyDown(search, { key: "ArrowDown", isComposing: true });
    fireEvent.keyDown(search, { key: "Enter", isComposing: true });
    expect(onJump).not.toHaveBeenCalled();

    // Some IMEs report the commit as keyCode 229 rather than isComposing.
    fireEvent.keyDown(search, { key: "Enter", keyCode: 229 });
    expect(onJump).not.toHaveBeenCalled();

    // Composition over: Enter works again, and the cursor never moved.
    fireEvent.keyDown(search, { key: "Enter" });
    expect(onJump).toHaveBeenCalledWith("t1");
  });

  it("reports the hovered row so the rail can light the matching tick", () => {
    const onHoverThread = vi.fn();
    renderWithI18n(<Harness onHoverThread={onHoverThread} />);
    fireEvent.click(screen.getByRole("button", { name: /Comment threads/ }));
    const row = screen.getByText("Expiry: 7 days or 14 days?").closest("button")!;
    fireEvent.pointerEnter(row);
    expect(onHoverThread).toHaveBeenCalledWith("t3");
    fireEvent.pointerLeave(row);
    expect(onHoverThread).toHaveBeenCalledWith(null);
  });

  it("resets the query and filter between openings", () => {
    renderWithI18n(<Harness />);
    const trigger = screen.getByRole("button", { name: /Comment threads/ });
    fireEvent.click(trigger);
    fireEvent.change(screen.getByPlaceholderText("Search threads or authors"), {
      target: { value: "outlook" },
    });
    expect(screen.queryByText("Invite links require a workspace first")).toBeNull();
    fireEvent.click(trigger);
    fireEvent.click(trigger);
    expect(screen.getByText("Invite links require a workspace first")).toBeTruthy();
  });
});
