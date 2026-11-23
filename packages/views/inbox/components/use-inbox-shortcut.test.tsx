import { afterEach, describe, expect, it, vi } from "vitest";
import { fireEvent, renderHook } from "@testing-library/react";
import { toast } from "sonner";
import { useModalStore } from "@multica/core/modals";
import { hasInboxUndo, toastInboxUndo } from "./inbox-undo-toast";
import { useInboxShortcut, useInboxUndoShortcut } from "./use-inbox-shortcut";

vi.mock("sonner", () => {
  let next = 0;
  return { toast: { success: vi.fn(() => ++next), dismiss: vi.fn() } };
});

const E = { key: "e", code: "KeyE" };
const Z = { key: "z", code: "KeyZ" };
const press = (init: KeyboardEventInit, target: Element = document.body) => fireEvent.keyDown(target, init);

function mountTarget(html: string) {
  const host = document.createElement("div");
  host.innerHTML = html;
  document.body.appendChild(host);
  return host;
}

afterEach(() => {
  useModalStore.getState().close();
  document.body.innerHTML = "";
  if (hasInboxUndo()) {
    const options = vi.mocked(toast.success).mock.calls.at(-1)![1] as unknown as { onAutoClose: () => void };
    options.onAutoClose();
  }
  vi.clearAllMocks();
});

describe("useInboxShortcut", () => {
  it("runs the handler on its shortcut and takes the key", () => {
    const handler = vi.fn();
    renderHook(() => useInboxShortcut("archiveInboxItem", handler));
    expect(press(E)).toBe(false);
    expect(handler).toHaveBeenCalledTimes(1);
  });

  it("ignores other keys", () => {
    const handler = vi.fn();
    renderHook(() => useInboxShortcut("archiveInboxItem", handler));
    expect(press({ key: "h", code: "KeyH" })).toBe(true);
    expect(press({ ...E, shiftKey: true })).toBe(true);
    expect(handler).not.toHaveBeenCalled();
  });

  it("leaves the key alone without a handler", () => {
    renderHook(() => useInboxShortcut("archiveInboxItem", null));
    expect(press(E)).toBe(true);
  });

  it("runs the latest handler", () => {
    const first = vi.fn();
    const second = vi.fn();
    const { rerender } = renderHook(({ handler }) => useInboxShortcut("archiveInboxItem", handler), {
      initialProps: { handler: first as (() => void) | null },
    });
    rerender({ handler: second });
    press(E);
    expect(first).not.toHaveBeenCalled();
    expect(second).toHaveBeenCalledTimes(1);
    rerender({ handler: null });
    expect(press(E)).toBe(true);
    expect(second).toHaveBeenCalledTimes(1);
  });

  it("stops listening once unmounted", () => {
    const handler = vi.fn();
    const { unmount } = renderHook(() => useInboxShortcut("archiveInboxItem", handler));
    unmount();
    press(E);
    expect(handler).not.toHaveBeenCalled();
  });

  it("ignores a held key", () => {
    const handler = vi.fn();
    renderHook(() => useInboxShortcut("archiveInboxItem", handler));
    expect(press({ ...E, repeat: true })).toBe(true);
    expect(handler).not.toHaveBeenCalled();
  });

  it("ignores IME composition", () => {
    const handler = vi.fn();
    renderHook(() => useInboxShortcut("archiveInboxItem", handler));
    expect(press({ ...E, isComposing: true })).toBe(true);
    expect(press({ ...E, keyCode: 229 })).toBe(true);
    expect(handler).not.toHaveBeenCalled();
  });

  it("ignores a key another listener already handled", () => {
    const handler = vi.fn();
    renderHook(() => useInboxShortcut("archiveInboxItem", handler));
    const host = mountTarget("<button>x</button>");
    host.addEventListener("keydown", (event) => event.preventDefault());
    press(E, host.querySelector("button")!);
    expect(handler).not.toHaveBeenCalled();
  });

  it.each([
    ["an input", "<input />", "input"],
    ["a textarea", "<textarea></textarea>", "textarea"],
    ["a select", "<select></select>", "select"],
    ["rich text", "<div contenteditable='true'><p>x</p></div>", "p"],
  ])("ignores typing into %s", (_name, html, selector) => {
    const handler = vi.fn();
    renderHook(() => useInboxShortcut("archiveInboxItem", handler));
    const host = mountTarget(html);
    expect(press(E, host.querySelector(selector)!)).toBe(true);
    expect(handler).not.toHaveBeenCalled();
  });

  it.each(["menu", "dialog", "alertdialog", "listbox"])("ignores keys inside an open %s", (role) => {
    const handler = vi.fn();
    renderHook(() => useInboxShortcut("archiveInboxItem", handler));
    const host = mountTarget(`<div role="${role}"><button>x</button></div>`);
    expect(press(E, host.querySelector("button")!)).toBe(true);
    expect(handler).not.toHaveBeenCalled();
  });

  it("ignores keys while a modal layer makes the page inert", () => {
    const handler = vi.fn();
    renderHook(() => useInboxShortcut("archiveInboxItem", handler));
    mountTarget("<div data-base-ui-inert></div>");
    expect(press(E)).toBe(true);
    expect(handler).not.toHaveBeenCalled();
  });

  it("ignores keys while a modal is open", () => {
    const handler = vi.fn();
    renderHook(() => useInboxShortcut("archiveInboxItem", handler));
    useModalStore.getState().open("create-issue");
    expect(press(E)).toBe(true);
    expect(handler).not.toHaveBeenCalled();
  });
});

describe("useInboxUndoShortcut", () => {
  it("undoes on Z", () => {
    const undo = vi.fn();
    renderHook(() => useInboxUndoShortcut(undo));
    expect(press(Z)).toBe(false);
    expect(undo).toHaveBeenCalledTimes(1);
  });

  it("leaves Z to a showing undo toast, then takes it back", () => {
    const undo = vi.fn();
    const toastUndo = vi.fn();
    renderHook(() => useInboxUndoShortcut(undo));
    toastInboxUndo("Notification archived", { label: "Undo", undo: toastUndo });

    press(Z);
    expect(toastUndo).toHaveBeenCalledTimes(1);
    expect(undo).not.toHaveBeenCalled();
    expect(hasInboxUndo()).toBe(false);

    press(Z);
    expect(undo).toHaveBeenCalledTimes(1);
    expect(toastUndo).toHaveBeenCalledTimes(1);
  });

  it("leaves Z alone without an undo", () => {
    renderHook(() => useInboxUndoShortcut(null));
    expect(press(Z)).toBe(true);
  });
});
