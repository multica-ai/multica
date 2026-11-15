import { type ComponentProps, useState } from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { Dialog, DialogContent, DialogTitle, DialogTrigger } from "../dialog";
import { CommandDialog, Command, CommandInput } from "../command";

async function click(element: Element) {
  fireEvent.pointerDown(element, { pointerType: "mouse", button: 0 });
  fireEvent.mouseDown(element, { button: 0 });
  fireEvent.pointerUp(element, { pointerType: "mouse", button: 0 });
  fireEvent.mouseUp(element, { button: 0 });
  fireEvent.click(element, { button: 0 });
}

function NestedPicker() {
  const [open, setOpen] = useState(false);
  return (
    <Dialog defaultOpen>
      <DialogContent>
        <DialogTitle>Create issue</DialogTitle>
        <input aria-label="Issue title" defaultValue="Keep my draft" />
        <button onClick={() => setOpen(true)}>Choose parent</button>
        <CommandDialog open={open} onOpenChange={setOpen} title="Choose parent">
          <Command><CommandInput placeholder="Search parents" /></Command>
        </CommandDialog>
      </DialogContent>
    </Dialog>
  );
}

describe("nested dialog dismissal", () => {
  it("dismisses only the picker when its backdrop is clicked, preserving the parent draft", async () => {
    render(<NestedPicker />);
    await click(screen.getByRole("button", { name: "Choose parent" }));
    expect(screen.getByPlaceholderText("Search parents")).toBeVisible();
    const backdrops = document.querySelectorAll("[data-slot=dialog-overlay]");
    expect(backdrops).toHaveLength(2);
    await click(backdrops[1]!);
    await waitFor(() => expect(screen.queryByPlaceholderText("Search parents")).not.toBeInTheDocument());
    expect(screen.getByRole("dialog", { name: "Create issue" })).toBeVisible();
    expect(screen.getByRole("textbox", { name: "Issue title" })).toHaveValue("Keep my draft");
    await click(screen.getByRole("button", { name: "Choose parent" }));
    fireEvent.keyDown(screen.getByPlaceholderText("Search parents"), { key: "Escape" });
    await waitFor(() => expect(screen.queryByPlaceholderText("Search parents")).not.toBeInTheDocument());
    expect(screen.getByRole("dialog", { name: "Create issue" })).toBeVisible();
  });

  it("still dismisses a standalone dialog from its backdrop", async () => {
    render(<Dialog><DialogTrigger>Open dialog</DialogTrigger><DialogContent><DialogTitle>Standalone</DialogTitle></DialogContent></Dialog>);
    await click(screen.getByRole("button", { name: "Open dialog" }));
    await click(document.querySelector("[data-slot=dialog-overlay]")!);
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  });
});

describe("extension popup dismissal", () => {
  it.each(["body", "html"])("preserves a draft when a disappearing extension popup retargets its click to %s", async (tag) => {
    const onOpenChange = vi.fn();
    render(
      <Dialog defaultOpen onOpenChange={onOpenChange}>
        <DialogContent>
          <DialogTitle>Create issue</DialogTitle>
          <input aria-label="Issue title" defaultValue="Keep my draft" />
        </DialogContent>
      </Dialog>,
    );
    // An extension lives outside the dialog's React tree. Hiding its
    // suggestion during a press can send the final click to a document root.
    const host = document.createElement("grammarly-extension");
    document.body.append(host);
    const suggestion = document.createElement("button");
    host.attachShadow({ mode: "closed" }).append(suggestion);
    fireEvent.pointerDown(suggestion, { pointerType: "mouse", button: 0, composed: true });
    fireEvent.mouseDown(suggestion, { button: 0, composed: true });
    host.hidden = true;
    const target = tag === "body" ? document.body : document.documentElement;
    fireEvent.pointerUp(target, { pointerType: "mouse", button: 0 });
    fireEvent.mouseUp(target, { button: 0 });
    fireEvent.click(target, { button: 0 });
    host.remove();

    expect(onOpenChange).not.toHaveBeenCalled();
    expect(screen.getByRole("dialog", { name: "Create issue" })).toBeVisible();
    expect(screen.getByRole("textbox", { name: "Issue title" })).toHaveValue("Keep my draft");

    await click(screen.getByRole("button", { name: "Close" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(onOpenChange).toHaveBeenCalledWith(false, expect.objectContaining({ reason: "close-press" }));
  });

  it("keeps document-root outside dismissal for non-modal dialogs", async () => {
    render(<Dialog defaultOpen modal={false}><DialogContent><DialogTitle>Non-modal</DialogTitle></DialogContent></Dialog>);
    await click(document.body);
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  });

  it("lets the caller cancel an ordinary backdrop dismissal", async () => {
    const onOpenChange = vi.fn<NonNullable<ComponentProps<typeof Dialog>["onOpenChange"]>>((_open, details) => details.cancel());
    render(<Dialog defaultOpen onOpenChange={onOpenChange}><DialogContent><DialogTitle>Keep open</DialogTitle></DialogContent></Dialog>);
    await click(document.querySelector("[data-slot=dialog-overlay]")!);
    expect(onOpenChange).toHaveBeenCalledWith(false, expect.objectContaining({ reason: "outside-press" }));
    expect(screen.getByRole("dialog", { name: "Keep open" })).toBeVisible();
  });
});
