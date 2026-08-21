import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import { Editor } from "@tiptap/core";
import StarterKit from "@tiptap/starter-kit";
import { Markdown } from "@tiptap/markdown";
import { Slice } from "@tiptap/pm/model";
import type { UploadResult } from "@multica/core/hooks/use-file-upload";
import { ImageExtension, createEditorExtensions } from "./index";
import { FileCardExtension } from "./file-card";
import {
  createFileUploadExtension,
  uploadAndInsertFile,
  insertUploadPlaceholder,
  settleUploadNode,
  pastedTextSource,
  PASTED_TEXT_FILENAME,
} from "./file-upload";

const BLOB_URL = "blob:test-image";
const FINAL_URL = "https://cdn.example.com/photo.png";

let editors: Editor[] = [];
let originalCreateObjectURL: typeof URL.createObjectURL | undefined;
let originalRevokeObjectURL: typeof URL.revokeObjectURL | undefined;

function makeEditor() {
  const element = document.createElement("div");
  document.body.appendChild(element);
  const editor = new Editor({
    element,
    extensions: [
      StarterKit,
      ImageExtension,
      FileCardExtension,
      Markdown.configure({ indentation: { style: "space", size: 3 } }),
    ],
  });
  editors.push(editor);
  return editor;
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

function makeUpload(
  overrides: Partial<UploadResult> & {
    id: string;
    link: string;
    filename: string;
  },
): UploadResult {
  return {
    workspace_id: "ws-1",
    issue_id: null,
    comment_id: null,
    chat_session_id: null,
    chat_message_id: null,
    uploader_type: "member",
    uploader_id: "user-1",
    url: overrides.link,
    download_url: overrides.link,
    markdown_url: overrides.link,
    content_type: "image/png",
    size_bytes: 1,
    created_at: new Date(0).toISOString(),
    // markdownLink defaults to the same value as `link` so legacy tests
    // continue to assert the previous URL shape unless they pass an
    // explicit override. Real callers always set it to the stable
    // `/api/attachments/<id>/download` path via useFileUpload.
    markdownLink: overrides.link,
    ...overrides,
  };
}

// jsdom lays nothing out, so ProseMirror's scrollToSelection (reached via the
// `focus()` in the fileCard insert) throws on the missing rect APIs. Same stub
// set as suggestion-popup.test.tsx.
beforeAll(() => {
  const rect = () => new DOMRect(0, 0, 0, 0);
  const rectList = () =>
    ({ length: 0, item: () => null, [Symbol.iterator]: function* () {} }) as DOMRectList;
  Object.defineProperty(Range.prototype, "getBoundingClientRect", {
    configurable: true,
    value: rect,
  });
  Object.defineProperty(Range.prototype, "getClientRects", {
    configurable: true,
    value: rectList,
  });
  Object.defineProperty(HTMLElement.prototype, "getClientRects", {
    configurable: true,
    value: rectList,
  });
  Object.defineProperty(Text.prototype, "getClientRects", {
    configurable: true,
    value: rectList,
  });
});

beforeEach(() => {
  originalCreateObjectURL = URL.createObjectURL;
  originalRevokeObjectURL = URL.revokeObjectURL;
  Object.defineProperty(URL, "createObjectURL", {
    configurable: true,
    value: vi.fn(() => BLOB_URL),
  });
  Object.defineProperty(URL, "revokeObjectURL", {
    configurable: true,
    value: vi.fn(),
  });
});

afterEach(() => {
  for (const editor of editors) editor.destroy();
  editors = [];
  document.body.innerHTML = "";

  if (originalCreateObjectURL) {
    Object.defineProperty(URL, "createObjectURL", {
      configurable: true,
      value: originalCreateObjectURL,
    });
  } else {
    delete (URL as Partial<typeof URL>).createObjectURL;
  }

  if (originalRevokeObjectURL) {
    Object.defineProperty(URL, "revokeObjectURL", {
      configurable: true,
      value: originalRevokeObjectURL,
    });
  } else {
    delete (URL as Partial<typeof URL>).revokeObjectURL;
  }
});

function firstImageAttrs(editor: Editor): Record<string, unknown> | null {
  let attrs: Record<string, unknown> | null = null;
  editor.state.doc.descendants((node) => {
    if (attrs) return false;
    if (node.type.name === "image") {
      attrs = node.attrs;
      return false;
    }
    return undefined;
  });
  return attrs;
}

describe("uploadAndInsertFile", () => {
  it("lets typing continue in the trailing paragraph after pasted image upload preview", async () => {
    const editor = makeEditor();
    const upload = deferred<UploadResult | null>();
    const handler = vi.fn(() => upload.promise);
    const file = new File(["image"], "photo.png", { type: "image/png" });

    const uploadTask = uploadAndInsertFile(editor, file, handler);

    expect(handler).toHaveBeenCalledWith(file, expect.any(String));
    expect(editor.state.selection.$from.parent.type.name).toBe("paragraph");

    editor.commands.insertContent("after");
    // The blob preview is visible in the document but is NOT content: the
    // document is the persisted draft body, and a process-local blob: URL
    // would outlive the upload as a dead link. It becomes markdown only once
    // the swap below gives it a real URL.
    expect(editor.getMarkdown()).not.toContain(BLOB_URL);
    expect(editor.getMarkdown().trim()).toBe("after");

    upload.resolve(
      makeUpload({ id: "attachment-1", link: FINAL_URL, filename: "photo.png" }),
    );
    await uploadTask;

    const saved = editor.getMarkdown().trimEnd();
    expect(saved).toBe([`![photo.png](${FINAL_URL})`, "", "after"].join("\n"));

    const reparsed = makeEditor();
    reparsed.commands.setContent(saved, { contentType: "markdown" });
    expect(reparsed.getMarkdown().trimEnd()).toBe(saved);
  });

  it("reserves the image box by capturing intrinsic dimensions, kept through the URL swap", async () => {
    const close = vi.fn();
    const createImageBitmap = vi.fn(async () => ({
      width: 800,
      height: 600,
      close,
    }));
    vi.stubGlobal("createImageBitmap", createImageBitmap);

    try {
      const editor = makeEditor();
      const upload = deferred<UploadResult | null>();
      const handler = vi.fn(() => upload.promise);
      const file = new File(["image"], "photo.png", { type: "image/png" });

      const uploadTask = uploadAndInsertFile(editor, file, handler);

      // Dimensions are measured off-thread and patched onto the node before the
      // upload resolves, so the blob preview already reserves its box.
      await vi.waitFor(() => {
        const attrs = firstImageAttrs(editor);
        expect(attrs?.width).toBe(800);
        expect(attrs?.height).toBe(600);
      });
      expect(createImageBitmap).toHaveBeenCalledWith(file);
      expect(close).toHaveBeenCalled();

      upload.resolve(
        makeUpload({ id: "attachment-1", link: FINAL_URL, filename: "photo.png" }),
      );
      await uploadTask;

      // The src swap preserves width/height (spread of existing attrs).
      const finalAttrs = firstImageAttrs(editor);
      expect(finalAttrs?.src).toBe(FINAL_URL);
      expect(finalAttrs?.width).toBe(800);
      expect(finalAttrs?.height).toBe(600);

      // width/height are render-only — they never reach the markdown.
      expect(editor.getMarkdown().trimEnd()).toBe(`![photo.png](${FINAL_URL})`);
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it("persists markdownLink (the stable per-attachment URL) into the markdown body, not the short-lived storage URL", async () => {
    // Regression pin for MUL-3130 review feedback. useFileUpload returns
    // both `link` (= att.url, short-lived signed `/uploads/<key>?exp&sig`
    // on LocalStorage) and `markdownLink` (= /api/attachments/<id>/download).
    // The editor must persist `markdownLink` so the comment doesn't
    // capture a 30-min signature, while non-markdown callers (avatar
    // pickers, logo upload) keep using `link` for backward compatibility.
    const editor = makeEditor();
    const SIGNED_URL = "/uploads/workspaces/ws-1/photo.png?exp=42&sig=fake";
    const STABLE_URL = "/api/attachments/attachment-7/download";
    const handler = vi.fn(async () =>
      makeUpload({
        id: "attachment-7",
        link: SIGNED_URL,
        markdownLink: STABLE_URL,
        filename: "photo.png",
      }),
    );
    const file = new File(["image"], "photo.png", { type: "image/png" });

    await uploadAndInsertFile(editor, file, handler);

    // The img node ends up with the stable URL as its src — the
    // expiring signed URL never makes it into the persisted markdown.
    const attrs = firstImageAttrs(editor);
    expect(attrs?.src).toBe(STABLE_URL);
    expect(editor.getMarkdown().trimEnd()).toBe(`![photo.png](${STABLE_URL})`);
    expect(editor.getMarkdown()).not.toContain("?exp=");
    expect(editor.getMarkdown()).not.toContain("?sig=");
  });
});

describe("one identity from node to draft", () => {
  it("hands the node's uploadId to the uploader, so both records name the same upload", async () => {
    const editor = makeEditor();
    const handler = vi.fn(async (_file: File, _uploadId: string) => null);
    await uploadAndInsertFile(editor, new File(["x"], "photo.png", { type: "image/png" }), handler);
    const [, uploadId] = handler.mock.calls[0]!;
    expect(typeof uploadId).toBe("string");
    expect(uploadId).toBeTruthy();
  });

  it("settles a placeholder a DIFFERENT document drew, by id alone", async () => {
    // The reopen case: this editor never ran the upload, it only rebuilt the
    // card from the draft record. Only a shared id can connect the two.
    const editor = makeEditor();
    expect(
      insertUploadPlaceholder(editor, { uploadId: "u-1", filename: "report.pdf", size: 12 }),
    ).toBe(true);
    expect(editor.getMarkdown()).not.toContain("report.pdf");

    expect(
      settleUploadNode(
        editor,
        "u-1",
        makeUpload({
          id: "att-1",
          link: "/api/attachments/att-1/download",
          filename: "report.pdf",
          content_type: "application/pdf",
        }),
      ),
    ).toBe(true);
    expect(editor.getMarkdown()).toContain("!file[report.pdf](/api/attachments/att-1/download)");
  });

  it("promotes a rebuilt card to an image when that is what arrived", async () => {
    // The rebuild path has only a filename, never bytes, so it always draws a
    // card; the settle is where the document learns it was an image.
    const editor = makeEditor();
    insertUploadPlaceholder(editor, { uploadId: "u-2", filename: "shot.png" });
    settleUploadNode(
      editor,
      "u-2",
      makeUpload({
        id: "att-2",
        link: "/api/attachments/att-2/download",
        filename: "shot.png",
        content_type: "image/png",
      }),
    );
    expect(editor.getMarkdown()).toContain("![shot.png](/api/attachments/att-2/download)");
  });

  it("reports false when this document holds no node for the id", () => {
    const editor = makeEditor();
    expect(
      settleUploadNode(
        editor,
        "nope",
        makeUpload({ id: "a", link: "/l", filename: "f", content_type: "application/pdf" }),
      ),
    ).toBe(false);
  });

  it("is idempotent, so a retrying caller cannot draw two cards", () => {
    const editor = makeEditor();
    expect(insertUploadPlaceholder(editor, { uploadId: "u-3", filename: "a.pdf" })).toBe(true);
    expect(insertUploadPlaceholder(editor, { uploadId: "u-3", filename: "a.pdf" })).toBe(true);
    let cards = 0;
    editor.state.doc.descendants((n) => {
      if (n.type.name === "fileCard") cards++;
      return undefined;
    });
    expect(cards).toBe(1);
  });
});

describe("paste-as-file", () => {
  const THRESHOLD = 20;

  function makePasteEditor(opts: {
    handler?: (file: File) => Promise<UploadResult | null>;
    threshold?: number;
  }) {
    const onUploadFileRef = { current: opts.handler };
    const thresholdRef = { current: opts.threshold };
    const element = document.createElement("div");
    document.body.appendChild(element);
    const editor = new Editor({
      element,
      extensions: [
        StarterKit,
        ImageExtension,
        FileCardExtension,
        Markdown.configure({ indentation: { style: "space", size: 3 } }),
        createFileUploadExtension(
          onUploadFileRef as React.RefObject<
            ((file: File) => Promise<UploadResult | null>) | undefined
          >,
          thresholdRef as React.RefObject<number | undefined>,
        ),
      ],
    });
    editors.push(editor);
    return editor;
  }

  /** Drive the plugin's handlePaste the way ProseMirror does, and report
   *  whether any plugin claimed the event. */
  function paste(editor: Editor, opts: { text?: string; files?: File[] }) {
    const event = {
      clipboardData: {
        files: opts.files ?? [],
        getData: (type: string) => (type === "text/plain" ? (opts.text ?? "") : ""),
      },
    } as unknown as ClipboardEvent;
    let handled = false;
    editor.view.someProp("handlePaste", (fn) => {
      handled = fn(editor.view, event, Slice.empty) === true;
      return handled;
    });
    return handled;
  }

  it("uploads an over-threshold plain-text paste as pasted-text.txt and keeps it out of the body", async () => {
    const upload = deferred<UploadResult | null>();
    const handler = vi.fn((_file: File) => upload.promise);
    const editor = makePasteEditor({ handler, threshold: THRESHOLD });
    const long = "x".repeat(THRESHOLD + 1);

    expect(paste(editor, { text: long })).toBe(true);

    await vi.waitFor(() => expect(handler).toHaveBeenCalled());
    const file = handler.mock.calls[0]![0];
    expect(file.name).toBe(PASTED_TEXT_FILENAME);
    expect(file.type).toBe("text/plain");
    await expect(file.text()).resolves.toBe(long);

    // The text itself never lands in the document — only the upload card.
    expect(editor.getMarkdown()).not.toContain(long);

    upload.resolve(
      makeUpload({
        id: "attachment-9",
        link: "/api/attachments/attachment-9/download",
        filename: PASTED_TEXT_FILENAME,
      }),
    );
    await vi.waitFor(() =>
      expect(editor.getMarkdown()).toContain("/api/attachments/attachment-9/download"),
    );
  });

  it("leaves a paste at or below the threshold as ordinary text", () => {
    const handler = vi.fn(async () => null);
    const editor = makePasteEditor({ handler, threshold: THRESHOLD });

    expect(paste(editor, { text: "x".repeat(THRESHOLD) })).toBe(false);
    expect(handler).not.toHaveBeenCalled();
  });

  it("does not convert when the host passes no threshold", () => {
    const handler = vi.fn(async () => null);
    const editor = makePasteEditor({ handler, threshold: undefined });

    expect(paste(editor, { text: "x".repeat(10_000) })).toBe(false);
    expect(handler).not.toHaveBeenCalled();
  });

  it("does not convert when the host wired no uploader", () => {
    const editor = makePasteEditor({ handler: undefined, threshold: THRESHOLD });

    // Degrades to a plain-text paste rather than swallowing the event.
    expect(paste(editor, { text: "x".repeat(THRESHOLD + 1) })).toBe(false);
  });

  it("still takes the real-file path when the clipboard carries a file", async () => {
    const handler = vi.fn(async (_file: File) => null);
    const editor = makePasteEditor({ handler, threshold: THRESHOLD });
    const real = new File(["image"], "photo.png", { type: "image/png" });

    expect(paste(editor, { text: "x".repeat(THRESHOLD + 1), files: [real] })).toBe(true);

    await vi.waitFor(() => expect(handler).toHaveBeenCalled());
    expect(handler).toHaveBeenCalledTimes(1);
    expect(handler.mock.calls[0]![0]).toBe(real);
  });

  it("tags the synthesised file with its source text so the draft owner can restore it", async () => {
    const upload = deferred<UploadResult | null>();
    const handler = vi.fn((_file: File) => upload.promise);
    const editor = makePasteEditor({ handler, threshold: THRESHOLD });
    const long = "x".repeat(THRESHOLD + 1);

    paste(editor, { text: long });

    await vi.waitFor(() => expect(handler).toHaveBeenCalled());
    // The recovery path lives in useCoordinatedUploads (the only layer that
    // outlives the mount and owns the draft); this is the channel it reads.
    expect(pastedTextSource(handler.mock.calls[0]![0])).toBe(long);

    upload.resolve(null);
  });

  it("does not tag a real dropped file", async () => {
    const handler = vi.fn(async (_file: File) => null);
    const editor = makePasteEditor({ handler, threshold: THRESHOLD });
    const real = new File(["image"], "photo.png", { type: "image/png" });

    paste(editor, { files: [real] });

    await vi.waitFor(() => expect(handler).toHaveBeenCalled());
    expect(pastedTextSource(real)).toBeUndefined();
  });

  it("wins the paste over markdownPaste in the REAL extension order", async () => {
    // Both paste handlers are catch-alls, so which one ProseMirror consults
    // first is the whole feature. Tiptap reverses the extension array before
    // collecting plugins (@tiptap/core `get plugins()`), and neither extension
    // sets a priority — so the ordering that decides this lives in
    // createEditorExtensions, not in either file. Build the production array so
    // a reorder there fails HERE instead of silently disabling the feature.
    const upload = deferred<UploadResult | null>();
    const handler = vi.fn((_file: File) => upload.promise);
    const onUploadFileRef = { current: handler };
    const thresholdRef = { current: THRESHOLD };
    const element = document.createElement("div");
    document.body.appendChild(element);
    const editor = new Editor({
      element,
      extensions: createEditorExtensions({
        onUploadFileRef: onUploadFileRef as React.RefObject<
          ((file: File) => Promise<UploadResult | null>) | undefined
        >,
        pasteAsFileThresholdRef: thresholdRef as React.RefObject<number | undefined>,
        disableMentions: true,
      }),
    });
    editors.push(editor);

    const long = "# heading\n\n" + "x".repeat(THRESHOLD + 1);
    expect(paste(editor, { text: long })).toBe(true);

    await vi.waitFor(() => expect(handler).toHaveBeenCalled());
    expect(handler.mock.calls[0]![0].name).toBe(PASTED_TEXT_FILENAME);
    // markdownPaste did NOT get to parse it into the body.
    expect(editor.getMarkdown()).not.toContain("x".repeat(THRESHOLD + 1));

    upload.resolve(null);
  });

  it("keeps a long paste inline inside a code block", () => {
    const handler = vi.fn(async (_file: File) => null);
    const editor = makePasteEditor({ handler, threshold: THRESHOLD });
    editor.commands.setCodeBlock();

    // Opening a fence IS the request to show the thing inline.
    expect(paste(editor, { text: "x".repeat(THRESHOLD + 1) })).toBe(false);
    expect(handler).not.toHaveBeenCalled();
  });
});
