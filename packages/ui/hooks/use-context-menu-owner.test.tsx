import { afterEach, describe, expect, it } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { contextMenuOriginRef } from "@multica/ui/lib/context-menu-origin";
import { Popover, PopoverContent, PopoverTrigger } from "@multica/ui/components/ui/popover";

let menu: HTMLElement;

/**
 * A context menu portaled to the body. It exists before the popover opens:
 * Base UI ignores presses on elements injected after it.
 */
function openContextMenu(origin: Element) {
  contextMenuOriginRef(origin)(menu);
  return menu.firstElementChild as HTMLElement;
}

function renderPopover() {
  menu = document.body.appendChild(document.createElement("div"));
  menu.setAttribute("role", "menu");
  const item = menu.appendChild(document.createElement("div"));
  item.setAttribute("role", "menuitem");
  item.tabIndex = -1;
  render(
    <>
      <Popover defaultOpen>
        <PopoverTrigger>Open</PopoverTrigger>
        <PopoverContent>
          <input aria-label="Search" />
        </PopoverContent>
      </Popover>
      <input aria-label="Elsewhere" />
    </>,
  );
  return screen.getByRole("textbox", { name: "Search" });
}

afterEach(() => {
  menu.remove();
});

describe("Popover with a context menu", () => {
  it("stays open when focus and a press move into the menu of a field inside it", async () => {
    const field = renderPopover();
    field.focus();
    const item = openContextMenu(field);

    item.focus();
    fireEvent.pointerDown(item);
    fireEvent.mouseDown(item);
    fireEvent.click(item);

    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(field.isConnected).toBe(true);
  });

  it("closes for a context menu opened outside it", async () => {
    const field = renderPopover();
    const item = openContextMenu(screen.getByRole("textbox", { name: "Elsewhere" }));

    fireEvent.pointerDown(item);
    fireEvent.mouseDown(item);
    fireEvent.click(item);

    await waitFor(() => expect(field.isConnected).toBe(false));
  });
});
