import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { CODEX_DICTATION_WORLD } from "../shared/dictation";

const expose = vi.hoisted(() => vi.fn());
vi.mock("electron", () => ({ contextBridge: { exposeInIsolatedWorld: expose } }));
import { installCodexDictationActivation } from "./dictation-activation";

describe("isolated mic activation", () => {
  let listeners: Map<string, (event: Event) => void>;
  let consume: () => boolean;
  let button: HTMLButtonElement;
  let editor: HTMLDivElement;
  let time: number;

  beforeEach(() => {
    expose.mockReset();
    listeners = new Map();
    vi.spyOn(document, "addEventListener").mockImplementation((name, listener) => {
      listeners.set(name, listener as (event: Event) => void);
    });
    vi.spyOn(window, "addEventListener").mockImplementation((name, listener) => {
      listeners.set(name, listener as (event: Event) => void);
    });
    vi.spyOn(document, "hasFocus").mockReturnValue(true);
    time = 100;
    vi.spyOn(performance, "now").mockImplementation(() => time);
    button = document.createElement("button");
    button.setAttribute("data-native-dictation", "");
    editor = document.createElement("div");
    editor.tabIndex = 0;
    Object.defineProperty(editor, "isContentEditable", { value: true });
    document.body.append(button, editor);
    editor.focus();
    installCodexDictationActivation();
    expect(expose).toHaveBeenCalledExactlyOnceWith(CODEX_DICTATION_WORLD, "multicaDictationActivation", { consume: expect.any(Function) });
    consume = expose.mock.calls[0]![2].consume;
  });

  afterEach(() => { vi.restoreAllMocks(); document.body.replaceChildren(); });

  // This suite tests the grant state machine through captured callbacks. Real
  // Chromium isTrusted and cross-world isolation are exercised by the separate
  // Electron fixture; a jsdom synthetic event cannot attest a trusted gesture.
  function trusted(type: string, values: object = {}) {
    listeners.get(type)!({ isTrusted: true, target: button, ...values } as unknown as Event);
  }

  it("does not expose admission through a main-world API or accept synthetic clicks", () => {
    expect(consume()).toBe(false);
    const event = new MouseEvent("click", { bubbles: true });
    Object.defineProperty(event, "target", { value: button });
    listeners.get("click")!(event);
    expect(consume()).toBe(false);
  });

  it("consumes one trusted mic click exactly once", () => {
    trusted("click");
    expect(consume()).toBe(true);
    expect(consume()).toBe(false);
  });

  it.each(["Enter", " "])("requires completed %s activation on the same mic", (key) => {
    trusted("keyup", { key });
    expect(consume()).toBe(false);
    trusted("keydown", { key });
    expect(consume()).toBe(false);
    trusted("keyup", { key });
    expect(consume()).toBe(true);
    expect(consume()).toBe(false);
  });

  it.each(["expired", "detached", "blurred", "non-editable", "other-click"])("consumes but rejects a %s grant", (reason) => {
    trusted("click");
    if (reason === "expired") time += 2000;
    if (reason === "detached") button.remove();
    if (reason === "blurred") listeners.get("blur")!(new Event("blur"));
    if (reason === "non-editable") button.focus();
    if (reason === "other-click") trusted("click", { target: editor });
    expect(consume()).toBe(false);
    editor.focus();
    expect(consume()).toBe(false);
  });

  it("does not arm disabled buttons or repeated/modified keyboard activation", () => {
    button.disabled = true;
    trusted("click");
    expect(consume()).toBe(false);
    button.disabled = false;
    for (const modifier of ["repeat", "ctrlKey", "altKey", "shiftKey", "metaKey"]) {
      trusted("keydown", { key: "Enter", [modifier]: true });
      trusted("keyup", { key: "Enter" });
      expect(consume()).toBe(false);
    }
  });
});
