/**
 * @vitest-environment jsdom
 */
import { act, fireEvent, render, screen } from "@testing-library/react";
import { useMemo, useState, type ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useCommandBarContextStore } from "@multica/core/command-bar";
import { useIssueOpeningStore } from "@multica/core/issues/stores/issue-opening-store";
import { useModalStore } from "@multica/core/modals";
import type { Issue } from "@multica/core/types";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { IssueCursorProvider, useIsIssueCursor } from "./issue-cursor";
import {
  IssuePeekActionsContext,
  IssuePeekColumnsContext,
  IssuePeekIdContext,
  type IssuePeekActions,
  type IssuePeekColumns,
} from "./peek-context";
import {
  IssueSurfaceSelectionProvider,
  useCreateIssueSurfaceSelection,
  useIssueSurfaceSelection,
} from "./selection-context";

const LIST: IssuePeekColumns = [["i-1", "i-2", "i-3"]];
const BOARD: IssuePeekColumns = [["i-1", "i-2", "i-3"], ["i-4", "i-5"]];

const issueOf = (id: string) => ({ id, identifier: id.toUpperCase() }) as Issue;
const ISSUES = ["i-1", "i-2", "i-3", "i-4", "i-5"].map(issueOf);

vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useWorkspacePaths: () => ({ issueDetail: (ref: string) => `/acme/issues/${ref}` }),
}));

const onOpen = vi.fn();
const onPullRequest = vi.fn();
const navigation: NavigationAdapter = {
  push: vi.fn(),
  replace: vi.fn(),
  back: vi.fn(),
  pathname: "/acme/issues",
  searchParams: new URLSearchParams(),
  hash: "",
  getShareableUrl: (path) => `https://app.example${path}`,
};

function Row({ id }: { id: string }) {
  const cursor = useIsIssueCursor(id);
  const { selectedIds } = useIssueSurfaceSelection();
  return (
    <div data-peek-target={id} data-testid={id} data-cursor={cursor ? "" : undefined} data-selected={selectedIds.has(id) ? "" : undefined}>
      {/* A table row's first link can be its pull request's. */}
      <a
        href="https://github.com/acme/app/pull/1"
        onClick={(event) => {
          event.preventDefault();
          onPullRequest(id);
        }}
      >
        PR {id}
      </a>
      <a
        href={`/acme/issues/${id}`}
        onClick={(event) => {
          event.preventDefault();
          onOpen(id);
        }}
      >
        {id}
      </a>
      <button type="button">menu {id}</button>
    </div>
  );
}

function Harness({
  columns,
  selectable = true,
  initialPeekId = null,
  children,
}: {
  columns: IssuePeekColumns;
  selectable?: boolean;
  initialPeekId?: string | null;
  children?: ReactNode;
}) {
  const [peekId, setPeekId] = useState<string | null>(initialPeekId);
  const selection = useCreateIssueSurfaceSelection("test");
  const actions = useMemo<IssuePeekActions>(
    () => ({
      open: setPeekId,
      toggle: (id) => setPeekId((current) => (current === id ? null : id)),
      close: () => setPeekId(null),
      publishColumns: () => {},
    }),
    [],
  );
  return (
    <NavigationProvider value={navigation}>
    <IssuePeekActionsContext.Provider value={actions}>
      <IssuePeekIdContext.Provider value={peekId}>
        <IssuePeekColumnsContext.Provider value={columns}>
          <IssueSurfaceSelectionProvider selection={selection}>
            <span data-testid="peek">{peekId ?? ""}</span>
            <button type="button" onClick={actions.close}>
              close peek
            </button>
            <IssueCursorProvider issues={ISSUES} selectable={selectable} enabled>
              {columns.flat().map((id) => (
                <Row key={id} id={id} />
              ))}
            </IssueCursorProvider>
            {children}
          </IssueSurfaceSelectionProvider>
        </IssuePeekColumnsContext.Provider>
      </IssuePeekIdContext.Provider>
    </IssuePeekActionsContext.Provider>
    </NavigationProvider>
  );
}

const press = (key: string, init: KeyboardEventInit = {}, target: Element = document.body) =>
  fireEvent.keyDown(target, { key, ...init });
const cursorId = () => document.querySelector("[data-cursor]")?.getAttribute("data-peek-target") ?? null;
const selectedIds = () =>
  Array.from(document.querySelectorAll("[data-selected]")).map((element) => element.getAttribute("data-peek-target"));
const cursorContexts = () =>
  useCommandBarContextStore
    .getState()
    .entries.map((entry) => entry.context)
    .filter((context) => context.kind === "issue" && context.cursor);

describe("IssueCursorProvider", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useIssueOpeningStore.setState({ openMode: "page" });
    Element.prototype.scrollIntoView = vi.fn();
  });

  afterEach(() => {
    act(() => useModalStore.getState().close());
  });

  it("moves with J / K and the arrows, starting at the top and stopping at the ends", () => {
    render(<Harness columns={LIST} />);
    expect(cursorId()).toBeNull();

    press("j");
    expect(cursorId()).toBe("i-1");
    press("ArrowDown");
    press("j");
    expect(cursorId()).toBe("i-3");
    press("j");
    expect(cursorId()).toBe("i-3");
    press("k");
    press("ArrowUp");
    expect(cursorId()).toBe("i-1");
  });

  it("starts going up from the bottom", () => {
    render(<Harness columns={LIST} />);
    press("k");
    expect(cursorId()).toBe("i-3");
  });

  it("starts from the row the pointer is over, without showing until a key moves it", () => {
    render(<Harness columns={LIST} />);
    fireEvent.pointerMove(screen.getByTestId("i-2"), { clientX: 5, clientY: 5 });
    expect(cursorId()).toBeNull();

    press("j");
    expect(cursorId()).toBe("i-3");

    fireEvent.pointerMove(screen.getByTestId("i-1"), { clientX: 5, clientY: 50 });
    expect(cursorId()).toBeNull();

    press("j");
    expect(cursorId()).toBe("i-2");
    fireEvent.pointerMove(screen.getByTestId("i-2"), { clientX: 9, clientY: 60 });
    expect(cursorId()).toBeNull();
  });

  it("crosses board columns with ← / →", () => {
    render(<Harness columns={BOARD} />);
    press("j");
    press("j");
    expect(cursorId()).toBe("i-2");
    press("ArrowRight");
    expect(cursorId()).toBe("i-5");
    press("ArrowLeft");
    expect(cursorId()).toBe("i-2");
  });

  it("leaves ← / → alone in a single column", () => {
    render(<Harness columns={LIST} />);
    press("j");
    const event = new KeyboardEvent("keydown", { key: "ArrowRight", bubbles: true, cancelable: true });
    document.body.dispatchEvent(event);
    expect(event.defaultPrevented).toBe(false);
  });

  it("opens the issue's page with Enter, never a link inside its row", () => {
    render(<Harness columns={LIST} />);
    press("j");
    press("j");
    press("Enter");
    expect(navigation.push).toHaveBeenCalledWith("/acme/issues/I-2");
    expect(onPullRequest).not.toHaveBeenCalled();
    expect(onOpen).not.toHaveBeenCalled();
  });

  it("peeks with Enter where issues open in the peek, and with Space anywhere", () => {
    useIssueOpeningStore.setState({ openMode: "peek" });
    render(<Harness columns={LIST} />);
    press("j");
    press("Enter");
    expect(screen.getByTestId("peek")).toHaveTextContent("i-1");
    expect(navigation.push).not.toHaveBeenCalled();

    fireEvent.click(screen.getByText("close peek"));
    press("j");
    press(" ");
    expect(screen.getByTestId("peek")).toHaveTextContent("i-2");
  });

  it("does nothing with Enter or Space until the cursor shows", () => {
    render(<Harness columns={LIST} />);
    press("Enter");
    press(" ");
    expect(onOpen).not.toHaveBeenCalled();
    expect(screen.getByTestId("peek")).toHaveTextContent("");
  });

  it("follows an open peek and leaves the keys to it", () => {
    render(<Harness columns={LIST} initialPeekId="i-2" />);
    press("j");
    press("x");
    expect(screen.getByTestId("peek")).toHaveTextContent("i-2");
    expect(cursorId()).toBeNull();
    expect(selectedIds()).toEqual([]);

    fireEvent.click(screen.getByText("close peek"));
    expect(cursorId()).toBe("i-2");
    press("j");
    expect(cursorId()).toBe("i-3");
  });

  it("selects with X, extends with Shift, takes every row with ⌘A and clears with Esc", () => {
    render(<Harness columns={LIST} />);
    press("j");
    press("x");
    expect(selectedIds()).toEqual(["i-1"]);

    press("j", { shiftKey: true });
    press("ArrowDown", { shiftKey: true });
    expect(selectedIds()).toEqual(["i-1", "i-2", "i-3"]);
    press("k", { shiftKey: true });
    expect(selectedIds()).toEqual(["i-1", "i-2", "i-3"]);

    press("Escape");
    expect(selectedIds()).toEqual([]);
    expect(cursorId()).toBe("i-2");

    press("a", { metaKey: true });
    expect(selectedIds()).toEqual(["i-1", "i-2", "i-3"]);
    press("Escape");
    press("Escape");
    expect(cursorId()).toBeNull();
  });

  it("leaves ⌘A to the page until the cursor shows or something is selected", () => {
    render(<Harness columns={LIST} />);
    const event = new KeyboardEvent("keydown", { key: "a", metaKey: true, bubbles: true, cancelable: true });
    document.body.dispatchEvent(event);
    expect(event.defaultPrevented).toBe(false);
    expect(selectedIds()).toEqual([]);
  });

  it("does not select where the view shows no selection", () => {
    render(<Harness columns={BOARD} selectable={false} />);
    press("j");
    press("x");
    press("a", { metaKey: true });
    press("j", { shiftKey: true });
    expect(selectedIds()).toEqual([]);
    expect(cursorId()).toBe("i-2");
  });

  it("works from a focused row link, but leaves fields, buttons and dialogs their keys", () => {
    render(
      <Harness columns={LIST}>
        <input aria-label="field" />
      </Harness>,
    );
    press("j", {}, screen.getByText("i-1"));
    expect(cursorId()).toBe("i-1");

    press("j", {}, screen.getByLabelText("field"));
    press("j", {}, screen.getByText("menu i-1"));
    expect(cursorId()).toBe("i-1");

    act(() => useModalStore.getState().open("create-issue"));
    press("j");
    expect(cursorId()).toBe("i-1");
  });

  it("leaves every key to a modal layer, even with focus on the page behind it", () => {
    render(<Harness columns={LIST} />);
    press("j");
    const inert = document.createElement("div");
    inert.setAttribute("data-base-ui-inert", "");
    document.body.appendChild(inert);
    try {
      for (const key of ["j", "Escape", "Enter", " ", "x"]) {
        const event = new KeyboardEvent("keydown", { key, bubbles: true, cancelable: true });
        document.body.dispatchEvent(event);
        expect(event.defaultPrevented).toBe(false);
      }
      expect(cursorId()).toBe("i-1");
      expect(navigation.push).not.toHaveBeenCalled();
    } finally {
      inert.remove();
    }
  });

  it("gives the command bar the cursor's issue while it shows", () => {
    render(<Harness columns={LIST} />);
    expect(cursorContexts()).toEqual([]);

    press("j");
    expect(cursorContexts()).toEqual([{ kind: "issue", issue: ISSUES[0], cursor: true }]);

    press("Escape");
    expect(cursorContexts()).toEqual([]);
  });

  it("keeps the hover peek's Space while the cursor is hidden", () => {
    const onSpace = vi.fn();
    document.addEventListener("keydown", onSpace);
    render(<Harness columns={LIST} />);
    press(" ");
    expect(onSpace).toHaveBeenCalled();
    onSpace.mockClear();

    press("j");
    press(" ");
    expect(onSpace).not.toHaveBeenCalled();
    document.removeEventListener("keydown", onSpace);
  });
});
