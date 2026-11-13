// @vitest-environment node
import { describe, expect, it, vi, beforeEach } from "vitest";

// State is created via `vi.hoisted` because `vi.mock` factories are hoisted
// above all top-level statements; a plain `const` would be a TDZ
// ReferenceError when the factory runs.
const ctx = vi.hoisted(() => ({
  clipboardWriteText: vi.fn().mockResolvedValue(undefined),
  openExternalSpy: vi.fn().mockResolvedValue(undefined),
}));

vi.mock("electron", () => ({
  clipboard: { writeText: ctx.clipboardWriteText },
  shell: { openExternal: ctx.openExternalSpy },
}));

import { installContextMenu } from "./context-menu";
import {
  CONTEXT_MENU_COMMAND_CHANNEL,
  CONTEXT_MENU_REQUEST_CHANNEL,
  hasContextMenuItems,
  type ContextMenuRequest,
} from "../shared/context-menu";

type ContextMenuParams = {
  selectionText: string;
  isEditable: boolean;
  linkURL: string;
  mediaType: string;
  hasImageContents: boolean;
  misspelledWord: string;
  dictionarySuggestions: string[];
  x: number;
  y: number;
  editFlags: {
    canCut: boolean;
    canCopy: boolean;
    canPaste: boolean;
    canSelectAll: boolean;
  };
};

// WebContents stub: the `context-menu` hook and per-contents IPC the SUT
// installs, the requests it sends, and the edit commands it can run.
function makeWebContents() {
  const contextMenuHandlers: Array<(event: unknown, params: ContextMenuParams) => void> = [];
  const ipcHandlers = new Map<string, (event: unknown, raw: unknown) => void>();
  const sent: Array<{ channel: string; request: ContextMenuRequest }> = [];
  return {
    sent,
    cut: vi.fn(),
    copy: vi.fn(),
    paste: vi.fn(),
    selectAll: vi.fn(),
    copyImageAt: vi.fn(),
    replaceMisspelling: vi.fn(),
    session: { addWordToSpellCheckerDictionary: vi.fn() },
    on(event: string, fn: (event: unknown, params: ContextMenuParams) => void) {
      if (event === "context-menu") contextMenuHandlers.push(fn);
    },
    send(channel: string, request: ContextMenuRequest) {
      sent.push({ channel, request });
    },
    ipc: {
      on(channel: string, fn: (event: unknown, raw: unknown) => void) {
        ipcHandlers.set(channel, fn);
      },
    },
    fire(params: ContextMenuParams) {
      for (const h of contextMenuHandlers) h({}, params);
    },
    command(raw: unknown) {
      ipcHandlers.get(CONTEXT_MENU_COMMAND_CHANNEL)?.({}, raw);
    },
    lastRequest(): ContextMenuRequest {
      const last = sent[sent.length - 1];
      if (!last) throw new Error("no request was sent");
      return last.request;
    },
  };
}

function setup() {
  const wc = makeWebContents();
  installContextMenu(wc as never);
  return wc;
}

const baseEditFlags = {
  canCut: false,
  canCopy: false,
  canPaste: false,
  canSelectAll: false,
};

beforeEach(() => {
  ctx.clipboardWriteText.mockClear();
  ctx.openExternalSpy.mockClear();
});

describe("installContextMenu — forwarding", () => {
  it("forwards an unhandled right-click to the renderer instead of popping a native menu", () => {
    const wc = setup();
    wc.fire(
      params({
        isEditable: true,
        selectionText: "hello",
        x: 12,
        y: 34,
        editFlags: { canCut: true, canCopy: true, canPaste: true, canSelectAll: true },
      }),
    );

    expect(wc.sent).toHaveLength(1);
    expect(wc.sent[0]!.channel).toBe(CONTEXT_MENU_REQUEST_CHANNEL);
    expect(wc.lastRequest()).toMatchObject({
      x: 12,
      y: 34,
      isEditable: true,
      canCut: true,
      canCopy: true,
      canPaste: true,
      canSelectAll: true,
      hasLink: false,
      hasImage: false,
    });
  });

  it("forwards a right-click with nothing of main's to offer, for the page's own items", () => {
    const wc = setup();
    wc.fire(params({}));
    expect(wc.sent).toHaveLength(1);
    expect(hasContextMenuItems(wc.lastRequest())).toBe(false);
  });

  it("does not offer copy for a whitespace-only selection", () => {
    const wc = setup();
    wc.fire(params({ selectionText: "   ", editFlags: { ...baseEditFlags, canCopy: true } }));
    expect(wc.lastRequest()).toMatchObject({ canCopy: false });
  });

  it("does not offer cut or paste outside an editable field", () => {
    const wc = setup();
    wc.fire(
      params({
        selectionText: "hello",
        editFlags: { canCut: true, canCopy: true, canPaste: true, canSelectAll: true },
      }),
    );
    expect(wc.lastRequest()).toMatchObject({
      canCut: false,
      canCopy: true,
      canPaste: false,
      canSelectAll: false,
    });
  });

  it("never sends the link URL or the misspelled word to the renderer", () => {
    const wc = setup();
    wc.fire(
      params({
        isEditable: true,
        linkURL: "https://multica.ai/welcome",
        misspelledWord: "teh",
        dictionarySuggestions: ["the"],
      }),
    );
    const serialized = JSON.stringify(wc.lastRequest());
    expect(serialized).not.toContain("multica.ai");
    expect(serialized).not.toContain("teh");
  });
});

describe("installContextMenu — commands", () => {
  it("runs the edit command for the latest request", () => {
    const wc = setup();
    wc.fire(
      params({
        isEditable: true,
        selectionText: "hi",
        editFlags: { canCut: true, canCopy: true, canPaste: true, canSelectAll: true },
      }),
    );
    const { id } = wc.lastRequest();
    wc.command({ id, type: "paste" });
    expect(wc.paste).toHaveBeenCalledTimes(1);
  });

  it("runs one command per request", () => {
    const wc = setup();
    wc.fire(params({ selectionText: "hi", editFlags: { ...baseEditFlags, canCopy: true } }));
    const { id } = wc.lastRequest();
    wc.command({ id, type: "copy" });
    wc.command({ id, type: "copy" });
    expect(wc.copy).toHaveBeenCalledTimes(1);
  });

  it("drops a command for an older request", () => {
    const wc = setup();
    wc.fire(params({ selectionText: "hi", editFlags: { ...baseEditFlags, canCopy: true } }));
    const stale = wc.lastRequest().id;
    wc.fire(params({ selectionText: "hi", editFlags: { ...baseEditFlags, canCopy: true } }));
    wc.command({ id: stale, type: "copy" });
    expect(wc.copy).not.toHaveBeenCalled();
  });

  it("drops a command the request did not offer", () => {
    const wc = setup();
    wc.fire(params({ selectionText: "hi", editFlags: { ...baseEditFlags, canCopy: true } }));
    const { id } = wc.lastRequest();
    wc.command({ id, type: "openLink" });
    wc.command({ id, type: "paste" });
    expect(ctx.openExternalSpy).not.toHaveBeenCalled();
    expect(wc.paste).not.toHaveBeenCalled();
  });

  it("ignores malformed commands", () => {
    const wc = setup();
    wc.fire(params({ selectionText: "hi", editFlags: { ...baseEditFlags, canCopy: true } }));
    const { id } = wc.lastRequest();
    wc.command(null);
    wc.command({ id: String(id), type: "copy" });
    wc.command({ id, type: "exec" });
    expect(wc.copy).not.toHaveBeenCalled();
    wc.command({ id, type: "copy" });
    expect(wc.copy).toHaveBeenCalledTimes(1);
  });
});

describe("installContextMenu — spelling", () => {
  it("offers suggestions and add-to-dictionary for a misspelled word in an editable field", () => {
    const wc = setup();
    wc.fire(
      params({
        isEditable: true,
        misspelledWord: "teh",
        dictionarySuggestions: ["the", "tea"],
      }),
    );
    expect(wc.lastRequest()).toMatchObject({
      suggestions: ["the", "tea"],
      canAddToDictionary: true,
    });

    wc.command({ id: wc.lastRequest().id, type: "replaceMisspelling", index: 1 });
    expect(wc.replaceMisspelling).toHaveBeenCalledWith("tea");
  });

  it("adds main's copy of the misspelled word to the dictionary", () => {
    const wc = setup();
    wc.fire(params({ isEditable: true, misspelledWord: "teh", dictionarySuggestions: [] }));
    wc.command({ id: wc.lastRequest().id, type: "addToDictionary" });
    expect(wc.session.addWordToSpellCheckerDictionary).toHaveBeenCalledWith("teh");
  });

  it("rejects a suggestion index outside the offered list", () => {
    const wc = setup();
    wc.fire(params({ isEditable: true, misspelledWord: "teh", dictionarySuggestions: ["the"] }));
    wc.command({ id: wc.lastRequest().id, type: "replaceMisspelling", index: 3 });
    expect(wc.replaceMisspelling).not.toHaveBeenCalled();
  });

  it("omits spelling outside an editable field", () => {
    const wc = setup();
    wc.fire(
      params({
        selectionText: "teh",
        misspelledWord: "teh",
        dictionarySuggestions: ["the"],
        editFlags: { ...baseEditFlags, canCopy: true },
      }),
    );
    expect(wc.lastRequest()).toMatchObject({ suggestions: [], canAddToDictionary: false });
  });
});

describe("installContextMenu — links and images", () => {
  it("opens and copies main's copy of an http(s) link", () => {
    const wc = setup();
    wc.fire(params({ linkURL: "https://multica.ai/welcome" }));
    expect(wc.lastRequest().hasLink).toBe(true);

    wc.command({ id: wc.lastRequest().id, type: "openLink" });
    expect(ctx.openExternalSpy).toHaveBeenCalledWith("https://multica.ai/welcome");

    wc.fire(params({ linkURL: "https://multica.ai/welcome" }));
    wc.command({ id: wc.lastRequest().id, type: "copyLink" });
    expect(ctx.clipboardWriteText).toHaveBeenCalledWith("https://multica.ai/welcome");
  });

  it("does not offer links that are not http(s)", () => {
    const wc = setup();
    wc.fire(params({ linkURL: "javascript:alert(1)" }));
    expect(wc.lastRequest().hasLink).toBe(false);
    wc.fire(params({ linkURL: "file:///index.html#/acme/issues/1" }));
    expect(wc.lastRequest().hasLink).toBe(false);
  });

  it("copies the pixels of a loaded image under the cursor", () => {
    const wc = setup();
    wc.fire(params({ mediaType: "image", hasImageContents: true, x: 120, y: 48 }));
    expect(wc.lastRequest().hasImage).toBe(true);
    wc.command({ id: wc.lastRequest().id, type: "copyImage" });
    expect(wc.copyImageAt).toHaveBeenCalledWith(120, 48);
  });

  it("does not offer copy image before the image has loaded", () => {
    const wc = setup();
    wc.fire(params({ mediaType: "image", hasImageContents: false }));
    expect(wc.lastRequest().hasImage).toBe(false);
  });
});

function params(over: Partial<ContextMenuParams>): ContextMenuParams {
  return {
    selectionText: "",
    isEditable: false,
    linkURL: "",
    mediaType: "none",
    hasImageContents: false,
    misspelledWord: "",
    dictionarySuggestions: [],
    x: 0,
    y: 0,
    editFlags: { ...baseEditFlags },
    ...over,
  };
}
