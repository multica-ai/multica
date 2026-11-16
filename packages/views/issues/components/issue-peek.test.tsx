import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import { useEffect, type ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import {
  useIsIssuePeeked,
  useIssuePeekActions,
  type IssuePeekColumns,
} from "../surface/peek-context";
import { IssuePeekHost } from "./issue-peek";

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ issueDetail: (id: string) => `/acme/issues/${id}` }),
}));

// The panel's job is hosting; IssueDetail itself is covered by its own suite.
vi.mock("./issue-detail", () => ({
  IssueDetail: ({
    issueId,
    variant,
    leadingAction,
    trailingActions,
    onDelete,
  }: {
    issueId: string;
    variant: string;
    leadingAction: ReactNode;
    trailingActions: ReactNode;
    onDelete: () => void;
  }) => {
    if (issueId === "boom") throw new Error("Could not render this issue");
    return (
    <div data-testid="detail" data-variant={variant}>
      {issueId}
      {leadingAction}
      {trailingActions}
      <button type="button" onClick={onDelete}>
        delete
      </button>
    </div>
    );
  },
}));

const navigation: NavigationAdapter = {
  push: vi.fn(),
  replace: vi.fn(),
  back: vi.fn(),
  pathname: "/acme/issues",
  searchParams: new URLSearchParams(),
  hash: "",
  getShareableUrl: (path) => `https://app.example${path}`,
};

const COLUMNS: IssuePeekColumns = [["i-1", "i-2", "i-3"], ["i-4"]];

/** Stands in for the board: publishes columns and renders peekable cards. */
function FakeBoard({ columns = COLUMNS }: { columns?: IssuePeekColumns }) {
  const peek = useIssuePeekActions();
  useEffect(() => {
    peek?.publishColumns(columns);
  }, [peek, columns]);
  // One container per column, like the board: moving a card between columns
  // remounts its element.
  return (
    <>
      {columns.map((column, c) => (
        <div key={c}>
          {column.map((id) => (
            <FakeCard key={id} id={id} />
          ))}
        </div>
      ))}
    </>
  );
}

function FakeCard({ id }: { id: string }) {
  const peek = useIssuePeekActions();
  const peeked = useIsIssuePeeked(id);
  return (
    <button
      type="button"
      data-peek-target={id}
      data-peeked={peeked ? "" : undefined}
      onClick={() => peek?.toggle(id)}
    >
      {`card ${id}`}
    </button>
  );
}

function renderHost(columns: IssuePeekColumns = COLUMNS) {
  const ui = (cols: IssuePeekColumns) => (
    <NavigationProvider value={navigation}>
      <IssuePeekHost>
        <FakeBoard columns={cols} />
      </IssuePeekHost>
    </NavigationProvider>
  );
  const result = renderWithI18n(ui(columns));
  return { ...result, setColumns: (cols: IssuePeekColumns) => result.rerender(ui(cols)) };
}

const panel = () => screen.queryByRole("complementary", { name: "Issue preview" });
// Closing plays a short exit animation before the panel unmounts.
const waitForClosed = () => waitFor(() => expect(panel()).toBeNull());
const openCard = (id: string) => fireEvent.click(screen.getByText(`card ${id}`));

describe("IssuePeekHost", () => {
  beforeEach(() => vi.clearAllMocks());

  it("opens the peeked issue as a peek-variant detail and marks its card", () => {
    renderHost();
    expect(panel()).toBeNull();

    openCard("i-2");

    expect(panel()).not.toBeNull();
    expect(screen.getByTestId("detail")).toHaveAttribute("data-variant", "peek");
    expect(screen.getByTestId("detail")).toHaveTextContent("i-2");
    expect(screen.getByText("card i-2")).toHaveAttribute("data-peeked");
    expect(screen.getByText("card i-1")).not.toHaveAttribute("data-peeked");
  });

  it("switches to another card, and closes when the peeked card is toggled again", async () => {
    renderHost();
    openCard("i-1");
    openCard("i-4");
    expect(screen.getByTestId("detail")).toHaveTextContent("i-4");

    openCard("i-4");
    await waitForClosed();
  });

  it("shows the position in the column and steps with J / K", () => {
    renderHost();
    openCard("i-2");
    expect(panel()).toHaveTextContent("2 / 3");

    fireEvent.keyDown(document.body, { key: "j" });
    expect(screen.getByTestId("detail")).toHaveTextContent("i-3");
    expect(panel()).toHaveTextContent("3 / 3");
    // Past the end of the column J does nothing.
    fireEvent.keyDown(document.body, { key: "j" });
    expect(screen.getByTestId("detail")).toHaveTextContent("i-3");

    fireEvent.keyDown(document.body, { key: "k" });
    fireEvent.keyDown(document.body, { key: "k" });
    expect(screen.getByTestId("detail")).toHaveTextContent("i-1");
  });

  it("disables the step buttons at the ends of the column", () => {
    renderHost();
    openCard("i-1");
    expect(screen.getByRole("button", { name: "Previous issue" })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "Next issue" }));
    expect(screen.getByTestId("detail")).toHaveTextContent("i-2");
  });

  it("closes on Escape, but not while typing or inside a popup", async () => {
    renderHost();
    openCard("i-1");

    const input = document.createElement("input");
    document.body.appendChild(input);
    fireEvent.keyDown(input, { key: "Escape" });
    expect(panel()).not.toBeNull();

    const menu = document.createElement("div");
    menu.setAttribute("role", "menu");
    document.body.appendChild(menu);
    fireEvent.keyDown(menu, { key: "Escape" });
    expect(panel()).not.toBeNull();

    fireEvent.keyDown(document.body, { key: "Escape" });
    await waitForClosed();

    input.remove();
    menu.remove();
  });

  it("ignores J / K typed into an editor", () => {
    renderHost();
    openCard("i-1");
    const editor = document.createElement("div");
    editor.setAttribute("contenteditable", "true");
    document.body.appendChild(editor);
    fireEvent.keyDown(editor, { key: "j" });
    expect(screen.getByTestId("detail")).toHaveTextContent("i-1");
    editor.remove();
  });

  it("closes from the close button and when the issue is deleted", async () => {
    renderHost();
    openCard("i-1");
    fireEvent.click(screen.getByRole("button", { name: "Close preview" }));
    await waitForClosed();

    openCard("i-1");
    fireEvent.click(screen.getByRole("button", { name: "delete" }));
    await waitForClosed();
  });

  it("links to the full issue page", () => {
    renderHost();
    openCard("i-3");
    expect(screen.getByRole("link", { name: "Open full page" })).toHaveAttribute(
      "href",
      "/acme/issues/i-3",
    );
  });

  it("stays open when the view changes, stepping in the new view's order", () => {
    const { setColumns } = renderHost();
    openCard("i-2");
    // e.g. board → list: one column in display order.
    act(() => setColumns([["i-4", "i-2", "i-1", "i-3"]]));
    expect(screen.getByTestId("detail")).toHaveTextContent("i-2");
    expect(panel()).toHaveTextContent("2 / 4");
    fireEvent.keyDown(document.body, { key: "j" });
    expect(screen.getByTestId("detail")).toHaveTextContent("i-1");
  });

  it("brings the peeked card back into view when it moves to another column", () => {
    const scrollIntoView = vi.fn();
    const original = Element.prototype.scrollIntoView;
    Element.prototype.scrollIntoView = scrollIntoView;
    try {
      // i-4 is alone in its column; the column to its right is empty.
      const { setColumns } = renderHost([["i-1", "i-2", "i-3"], ["i-4"], []]);
      openCard("i-4");
      expect(scrollIntoView.mock.contexts.at(-1)).toBe(screen.getByText("card i-4"));

      // Its status changes: it lands alone in the empty column. Its neighbours
      // (none on either side) are unchanged, but its element is a new one.
      scrollIntoView.mockClear();
      act(() => setColumns([["i-1", "i-2", "i-3"], [], ["i-4"]]));
      expect(scrollIntoView).toHaveBeenCalledTimes(1);
      expect(scrollIntoView.mock.contexts[0]).toBe(screen.getByText("card i-4"));

      // A reorder elsewhere leaves its element in place: no scroll.
      scrollIntoView.mockClear();
      act(() => setColumns([["i-3", "i-2", "i-1"], [], ["i-4"]]));
      expect(scrollIntoView).not.toHaveBeenCalled();
    } finally {
      Element.prototype.scrollIntoView = original;
    }
  });

  it("keeps a way to close the panel when the issue fails to render", async () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    renderHost([["i-1", "boom"]]);
    openCard("boom");
    expect(panel()).toHaveTextContent("Could not render this issue");
    fireEvent.click(screen.getByRole("button", { name: "Close preview" }));
    await waitForClosed();
    vi.mocked(console.error).mockRestore();
  });

  it("steps across columns with H / L, keeping the row where it can", () => {
    renderHost([["a1", "a2", "a3"], [], ["c1"], ["d1", "d2", "d3"]]);
    openCard("a3");
    // The empty column is skipped; the short one clamps to its last card.
    fireEvent.keyDown(document.body, { key: "l" });
    expect(screen.getByTestId("detail")).toHaveTextContent("c1");
    fireEvent.keyDown(document.body, { key: "l" });
    expect(screen.getByTestId("detail")).toHaveTextContent("d1");
    fireEvent.keyDown(document.body, { key: "j" });
    fireEvent.keyDown(document.body, { key: "h" });
    expect(screen.getByTestId("detail")).toHaveTextContent("c1");
    fireEvent.keyDown(document.body, { key: "h" });
    expect(screen.getByTestId("detail")).toHaveTextContent("a1");
  });

  it("steps with the arrow keys, unless the reader clicked into the panel", () => {
    renderHost();
    openCard("i-1");
    expect(fireEvent.keyDown(document.body, { key: "ArrowDown" })).toBe(false);
    expect(screen.getByTestId("detail")).toHaveTextContent("i-2");
    fireEvent.keyDown(document.body, { key: "ArrowRight" });
    expect(screen.getByTestId("detail")).toHaveTextContent("i-4");
    fireEvent.keyDown(document.body, { key: "ArrowLeft" });
    expect(screen.getByTestId("detail")).toHaveTextContent("i-1");

    // After a click inside the panel the arrows scroll it instead.
    fireEvent.pointerDown(screen.getByTestId("detail"));
    expect(fireEvent.keyDown(document.body, { key: "ArrowDown" })).toBe(true);
    expect(screen.getByTestId("detail")).toHaveTextContent("i-1");
    // J still steps, and a click back on the view hands the arrows back.
    fireEvent.keyDown(document.body, { key: "j" });
    expect(screen.getByTestId("detail")).toHaveTextContent("i-2");
    fireEvent.pointerDown(screen.getByText("card i-4"));
    fireEvent.keyDown(document.body, { key: "ArrowDown" });
    expect(screen.getByTestId("detail")).toHaveTextContent("i-3");
  });

  it("peeks the card under the pointer on Space, and closes it on a second Space", async () => {
    renderHost();
    fireEvent.pointerOver(screen.getByText("card i-3"));
    expect(fireEvent.keyDown(document.body, { key: " " })).toBe(false);
    expect(screen.getByTestId("detail")).toHaveTextContent("i-3");

    fireEvent.keyDown(document.body, { key: " " });
    await waitForClosed();
  });

  it("leaves Space alone with no card under the pointer, or a control focused", () => {
    renderHost();
    fireEvent.keyDown(document.body, { key: " " });
    expect(panel()).toBeNull();

    // Focus on a control keeps Space for that control, even over a card.
    fireEvent.pointerOver(screen.getByText("card i-3"));
    const button = document.createElement("button");
    document.body.appendChild(button);
    expect(fireEvent.keyDown(button, { key: " " })).toBe(true);
    expect(panel()).toBeNull();
    button.remove();

    // Leaving the view forgets the hovered card.
    // (FakeBoard renders its cards straight into the host wrapper.)
    fireEvent.pointerLeave(screen.getByText("card i-3").parentElement!);
    fireEvent.keyDown(document.body, { key: " " });
    expect(panel()).toBeNull();
  });
});
