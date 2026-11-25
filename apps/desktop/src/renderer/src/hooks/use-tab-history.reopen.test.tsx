// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { cleanup, render } from "@testing-library/react";
import { configureShortcutPlatform, type ShortcutPlatform } from "@multica/core/shortcuts";
import { getActiveTab, useTabStore } from "@/stores/tab-store";
import { useWindowOverlayStore } from "@/stores/window-overlay-store";
import { useNavigationInputBindings } from "./use-tab-history";

function Probe() { useNavigationInputBindings(); return null; }
function key(init: KeyboardEventInit = {}, target: EventTarget = window, prevented = false) {
  const event = new KeyboardEvent("keydown", { key: "T", metaKey: true, shiftKey: true, bubbles: true, cancelable: true, ...init });
  if (prevented) event.preventDefault();
  target.dispatchEvent(event);
  return event;
}
function url() { return getActiveTab(useTabStore.getState())?.url; }
function seed() {
  const store = useTabStore.getState();
  store.switchWorkspace("acme");
  const id = store.openTab("/acme/projects?sort=asc#board", "Projects", { activate: true });
  store.closeTab(id);
}
beforeEach(() => {
  useTabStore.getState().reset();
  useWindowOverlayStore.getState().close();
  configureShortcutPlatform("macos");
  Object.assign(window, { desktopAPI: { windowContext: { kind: "main" } } });
  seed();
});
afterEach(() => { cleanup(); configureShortcutPlatform(null); useWindowOverlayStore.getState().close(); });

describe("reopen shortcut input ownership", () => {
  it.each(["macos", "windows", "linux"] as ShortcutPlatform[])("matches exact primary Shift T on %s", (platform) => {
    configureShortcutPlatform(platform);
    render(<Probe />);
    expect(key({ key: "t", metaKey: platform === "macos", ctrlKey: platform !== "macos" }).defaultPrevented).toBe(true);
    expect(url()).toBe("/acme/projects?sort=asc#board");
  });

  it.each([
    { shiftKey: false }, { altKey: true }, { ctrlKey: true },
    { metaKey: false, ctrlKey: true }, { metaKey: false },
    { key: "R" }, { key: "Dead" }, { key: "Process" },
    { repeat: true }, { isComposing: true },
  ])("ignores the non-owning input %j", (init) => {
    render(<Probe />);
    expect(key(init).defaultPrevented).toBe(false);
    expect(url()).toBe("/acme/issues");
  });

  it("respects defaultPrevented", () => {
    render(<Probe />);
    key({}, window, true);
    expect(url()).toBe("/acme/issues");
  });

  it.each(["input", "textarea", "select", "editable", "menu", "dialog", "alertdialog", "listbox"])("leaves %s focus to its owner", (kind) => {
    render(<Probe />);
    const target = document.createElement(["input", "textarea", "select"].includes(kind) ? kind : "div");
    if (kind === "editable") target.setAttribute("contenteditable", "true");
    if (["menu", "dialog", "alertdialog", "listbox"].includes(kind)) target.setAttribute("role", kind);
    const child = document.createElement("span");
    if (target.tagName === "DIV") target.appendChild(child);
    document.body.appendChild(target);
    expect(key({}, child.parentNode ? child : target).defaultPrevented).toBe(false);
    expect(url()).toBe("/acme/issues");
    target.remove();
  });

  it("respects modal inert markers even while shell focus is retained", () => {
    render(<Probe />);
    const modal = document.createElement("div");
    modal.setAttribute("data-base-ui-inert", "");
    document.body.appendChild(modal);
    expect(key().defaultPrevented).toBe(false);
    expect(url()).toBe("/acme/issues");
    modal.remove();
  });

  it("ignores workspace overlays and issue windows", () => {
    render(<Probe />);
    useWindowOverlayStore.getState().open({ type: "new-workspace" });
    expect(key().defaultPrevented).toBe(false);
    useWindowOverlayStore.getState().close();
    Object.assign(window, { desktopAPI: { windowContext: { kind: "issue" } } });
    expect(key().defaultPrevented).toBe(false);
    expect(url()).toBe("/acme/issues");
  });

  it("reads workspace and stack at execution time and removes its listener on unmount", () => {
    const probe = render(<Probe />);
    useTabStore.getState().switchWorkspace("other");
    key();
    expect(url()).toBe("/other/issues");
    useTabStore.getState().switchWorkspace("acme");
    probe.unmount();
    expect(key().defaultPrevented).toBe(false);
    expect(url()).toBe("/acme/issues");
  });
});
