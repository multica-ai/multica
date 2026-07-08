import { describe, expect, it, vi, beforeEach } from "vitest";

// Capture every MenuItem the SUT constructs so each test can assert
// on the menu that would appear at popup time without booting an
// actual Electron window. State is created via `vi.hoisted` because
// `vi.mock` factories are hoisted above all top-level statements; a
// plain `const` would be a TDZ ReferenceError when the factory runs.
type CapturedMenuItem = {
  label?: string;
  role?: string;
  type?: string;
  click?: () => void;
};
const ctx = vi.hoisted(() => ({
  capturedItems: [] as CapturedMenuItem[][],
  browserWindowFromWebContents: vi.fn(),
  popupSpy: vi.fn(),
  clipboardWriteText: vi.fn(),
  openExternalSpy: vi.fn().mockResolvedValue(undefined),
  preferredLanguagesRef: { current: ["en-US"] as string[] },
}));

vi.mock("electron", () => {
  class MockMenu {
    items: CapturedMenuItem[] = [];
    constructor() {
      ctx.capturedItems.push(this.items);
    }
    append(item: CapturedMenuItem) {
      this.items.push(item);
    }
    popup(opts: unknown) {
      ctx.popupSpy(opts);
    }
  }
  class MockMenuItem {
    label?: string;
    role?: string;
    type?: string;
    click?: () => void;
    constructor(opts: CapturedMenuItem) {
      Object.assign(this, opts);
    }
  }
  return {
    BrowserWindow: { fromWebContents: ctx.browserWindowFromWebContents },
    Menu: MockMenu,
    MenuItem: MockMenuItem,
    app: {
      getPreferredSystemLanguages: () => ctx.preferredLanguagesRef.current,
    },
    clipboard: { writeText: ctx.clipboardWriteText },
    shell: { openExternal: ctx.openExternalSpy },
  };
});

import { installContextMenu } from "./context-menu";

type ContextMenuParams = {
  selectionText: string;
  isEditable: boolean;
  linkURL: string;
  editFlags: {
    canCut: boolean;
    canCopy: boolean;
    canPaste: boolean;
    canSelectAll: boolean;
  };
};

type Listener = (event: unknown, params: ContextMenuParams) => void;

// Tiny WebContents stub — we only need the `.on("context-menu", ...)`
// hook the SUT installs and a way to fire it back at our own listener
// list. Everything else (clipboard, link opening, label resolution) is
// mocked above.
function makeWebContents() {
  const handlers: Listener[] = [];
  return {
    on(event: string, fn: Listener) {
      if (event === "context-menu") handlers.push(fn);
    },
    fire(params: ContextMenuParams) {
      for (const h of handlers) h({}, params);
    },
  };
}

const baseEditFlags = {
  canCut: false,
  canCopy: false,
  canPaste: false,
  canSelectAll: false,
};

describe("installContextMenu — link items", () => {
  beforeEach(() => {
    ctx.capturedItems.length = 0;
    ctx.popupSpy.mockClear();
    ctx.clipboardWriteText.mockClear();
    ctx.openExternalSpy.mockClear();
    ctx.browserWindowFromWebContents.mockReset();
    ctx.preferredLanguagesRef.current = ["en-US"];
  });

  it("adds 'Open Link in Browser' and 'Copy Link Address' when right-clicking an http(s) link", () => {
    // The link case is the one this test file is here to cover —
    // before MUL-3083 follow-up, right-clicking an <a> in the
    // renderer only surfaced 'copy' (when the user happened to have
    // text selected) and gave no way to open the URL externally.
    const wc = makeWebContents();
    installContextMenu(wc as never);
    wc.fire({
      ...baseSelection({ linkURL: "https://multica.ai/welcome" }),
    });

    const labels = lastMenuLabels();
    expect(labels).toContain("Open Link in Browser");
    expect(labels).toContain("Copy Link Address");

    // The two click handlers must route to the existing
    // openExternalSafely allowlist + clipboard.writeText.
    invokeByLabel("Open Link in Browser");
    expect(ctx.openExternalSpy).toHaveBeenCalledWith("https://multica.ai/welcome");

    invokeByLabel("Copy Link Address");
    expect(ctx.clipboardWriteText).toHaveBeenCalledWith(
      "https://multica.ai/welcome",
    );
    expect(ctx.popupSpy).toHaveBeenCalledTimes(1);
  });

  it("does NOT add link items when the cursor is over a non-http(s) URL", () => {
    // Only http(s) links are surfaced — we don't promise anything for
    // mailto:, javascript:, custom app schemes, etc. Surfacing them
    // would shell out via openExternalSafely (which would block the
    // call anyway) or write a non-URL string to the clipboard, both
    // of which violate user expectations for a "link" item.
    const wc = makeWebContents();
    installContextMenu(wc as never);
    wc.fire(baseSelection({ linkURL: "javascript:alert(1)" }));
    const labels = lastMenuLabelsOrEmpty();
    expect(labels).not.toContain("Open Link in Browser");
    expect(labels).not.toContain("Copy Link Address");
  });

  it("does NOT add link items when there is no link under the cursor", () => {
    const wc = makeWebContents();
    installContextMenu(wc as never);
    wc.fire({
      selectionText: "hello",
      isEditable: false,
      linkURL: "",
      editFlags: { ...baseEditFlags, canCopy: true },
    });
    const labels = lastMenuLabelsOrEmpty();
    expect(labels).not.toContain("Open Link in Browser");
    // Selection-only context still surfaces copy as before — guards
    // against a regression where adding the link branch broke the
    // base path.
    expect(menuItemRoles()).toContain("copy");
  });

  it("uses zh-Hans labels when the OS preferred language is Chinese", () => {
    // Locale fallback is intentionally permissive: every zh-* variant
    // routes to zh-Hans so users on zh-CN / zh-TW / zh-HK still see
    // Chinese rather than dropping to English. The renderer ships only
    // zh-Hans translations, so this matches the rest of the app.
    ctx.preferredLanguagesRef.current = ["zh-CN"];
    const wc = makeWebContents();
    installContextMenu(wc as never);
    wc.fire(baseSelection({ linkURL: "https://multica.ai" }));
    expect(lastMenuLabels()).toContain("在浏览器中打开链接");
    expect(lastMenuLabels()).toContain("复制链接地址");
  });

  it("falls back to English when the OS preferred language is something we don't ship", () => {
    ctx.preferredLanguagesRef.current = ["fr-FR"];
    const wc = makeWebContents();
    installContextMenu(wc as never);
    wc.fire(baseSelection({ linkURL: "https://multica.ai" }));
    expect(lastMenuLabels()).toContain("Open Link in Browser");
  });
});

// --- helpers ---

function baseSelection(over: Partial<ContextMenuParams>): ContextMenuParams {
  return {
    selectionText: "",
    isEditable: false,
    linkURL: "",
    editFlags: { ...baseEditFlags },
    ...over,
  };
}

function lastMenu(): CapturedMenuItem[] {
  const last = ctx.capturedItems[ctx.capturedItems.length - 1];
  if (!last) throw new Error("no menu was constructed");
  return last;
}

function lastMenuLabelsOrEmpty(): string[] {
  const last = ctx.capturedItems[ctx.capturedItems.length - 1] ?? [];
  return last.map((i) => i.label ?? "");
}

function lastMenuLabels(): string[] {
  return lastMenu().map((i) => i.label ?? "");
}

function menuItemRoles(): string[] {
  return lastMenu().map((i) => i.role ?? "");
}

function invokeByLabel(label: string): void {
  const item = lastMenu().find((i) => i.label === label);
  if (!item) throw new Error(`menu item not found: ${label}`);
  item.click?.();
}
