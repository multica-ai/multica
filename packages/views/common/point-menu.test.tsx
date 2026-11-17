import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { CLOSED_ROW_MENU, nextRowMenuState, shouldOpenRowMenu } from "./point-menu";

describe("nextRowMenuState", () => {
  it("keeps a right-click menu open when Base UI reads the pointer leaving its trigger", () => {
    const menu = { open: true, point: { x: 1, y: 2 } };
    expect(nextRowMenuState(menu, false, "trigger-hover")).toBeNull();
  });

  it("keeps the point while closing, so the menu does not jump as it animates out", () => {
    const menu = { open: true, point: { x: 1, y: 2 } };
    expect(nextRowMenuState(menu, false, "outside-press")).toEqual({ open: false, point: { x: 1, y: 2 } });
  });

  it("anchors a trigger-opened menu to its trigger", () => {
    expect(nextRowMenuState(CLOSED_ROW_MENU, true, "trigger-press")).toEqual({ open: true, point: null });
  });
});

describe("shouldOpenRowMenu", () => {
  function renderRow(deferToContent: boolean) {
    const onOpen = vi.fn();
    render(
      <div
        data-testid="row"
        onContextMenu={(event) => {
          if (shouldOpenRowMenu(event, { deferToContent })) onOpen();
        }}
      >
        <span>plain text</span>
        <a href="https://multica.ai">a link</a>
        <img alt="a picture" src="data:," />
        <pre>some code</pre>
        <table><tbody><tr><td>a cell</td></tr></tbody></table>
        <div data-context-menu-content="">an attachment</div>
        <span
          onContextMenu={(event) => event.preventDefault()}
        >
          handled inside
        </span>
      </div>,
    );
    return onOpen;
  }

  it("opens on plain row content", () => {
    const onOpen = renderRow(true);
    fireEvent.contextMenu(screen.getByText("plain text"));
    expect(onOpen).toHaveBeenCalledTimes(1);
  });

  it("leaves a right-click something inside already handled", () => {
    const onOpen = renderRow(false);
    fireEvent.contextMenu(screen.getByText("handled inside"));
    expect(onOpen).not.toHaveBeenCalled();
  });

  it("leaves links and images their own menu when deferring to content", () => {
    const onOpen = renderRow(true);
    fireEvent.contextMenu(screen.getByText("a link"));
    fireEvent.contextMenu(screen.getByAltText("a picture"));
    expect(onOpen).not.toHaveBeenCalled();
  });

  it("leaves code, tables and attachments their own menu when deferring to content", () => {
    const onOpen = renderRow(true);
    fireEvent.contextMenu(screen.getByText("some code"));
    fireEvent.contextMenu(screen.getByText("a cell"));
    fireEvent.contextMenu(screen.getByText("an attachment"));
    expect(onOpen).not.toHaveBeenCalled();
  });

  it("treats links as part of the row when not deferring to content", () => {
    const onOpen = renderRow(false);
    fireEvent.contextMenu(screen.getByText("a link"));
    expect(onOpen).toHaveBeenCalledTimes(1);
  });

  it("leaves selected text its own menu when deferring to content", () => {
    const onOpen = renderRow(true);
    const text = screen.getByText("plain text");
    const range = document.createRange();
    range.selectNodeContents(text);
    window.getSelection()!.removeAllRanges();
    window.getSelection()!.addRange(range);

    fireEvent.contextMenu(text);
    expect(onOpen).not.toHaveBeenCalled();
    window.getSelection()!.removeAllRanges();
  });
});
