import { describe, expect, it, vi } from "vitest";
import { Editor } from "@tiptap/core";
import StarterKit from "@tiptap/starter-kit";
import { Markdown } from "@tiptap/markdown";
import { createImageExtension } from "./image-extension";
import { ImageCaption } from "./image-caption";
import {
  MOVE_IMAGE_TO_TRAY_EVENT,
  deleteInlineImageAndCaption,
  moveInlineImageToTray,
} from "./image-placement";

const imageExtension = createImageExtension({
  imageView: () => null,
  escapeMarkdownLabel: (value) => value,
});

describe("moveInlineImageToTray", () => {
  it("hands the existing URL and caption to the tray, then removes both nodes", () => {
    const editor = new Editor({
      extensions: [StarterKit, imageExtension, ImageCaption, Markdown],
      content: {
        type: "doc",
        content: [
          {
            type: "image",
            attrs: {
              src: "https://cdn/a.png",
              alt: "a.png",
              placement: "inline",
            },
          },
          {
            type: "imageCaption",
            content: [{ type: "text", text: "Quarterly result" }],
          },
          { type: "paragraph", content: [{ type: "text", text: "After" }] },
        ],
      },
    });
    const target = new EventTarget();
    const listener = vi.fn((event: Event) => event.preventDefault());
    target.addEventListener(MOVE_IMAGE_TO_TRAY_EVENT, listener);
    const image = editor.state.doc.nodeAt(0)!;

    moveInlineImageToTray(target, editor, image, () => 0);

    expect(listener).toHaveBeenCalledOnce();
    expect((listener.mock.calls[0]![0] as CustomEvent).detail).toEqual({
      src: "https://cdn/a.png",
      filename: "a.png",
      caption: "Quarterly result",
    });
    expect(editor.getText()).toBe("After");
    expect(editor.state.doc.firstChild?.type.name).toBe("paragraph");
    editor.destroy();
  });

  it("keeps the image when no tray accepts the move", () => {
    const editor = new Editor({
      extensions: [StarterKit, imageExtension, Markdown],
      content: {
        type: "doc",
        content: [
          { type: "image", attrs: { src: "https://cdn/a.png", alt: "a.png" } },
        ],
      },
    });
    const target = new EventTarget();
    const image = editor.state.doc.nodeAt(0)!;

    moveInlineImageToTray(target, editor, image, () => 0);

    expect(editor.state.doc.firstChild?.type.name).toBe("image");
    editor.destroy();
  });

  it("deletes the image and its owned caption together", () => {
    const editor = new Editor({
      extensions: [StarterKit, imageExtension, ImageCaption, Markdown],
      content: {
        type: "doc",
        content: [
          { type: "image", attrs: { src: "https://cdn/a.png", alt: "a.png" } },
          {
            type: "imageCaption",
            content: [{ type: "text", text: "Quarterly result" }],
          },
          { type: "paragraph", content: [{ type: "text", text: "After" }] },
        ],
      },
    });
    const image = editor.state.doc.nodeAt(0)!;

    deleteInlineImageAndCaption(editor, image, () => 0);

    expect(editor.getText()).toBe("After");
    expect(editor.state.doc.firstChild?.type.name).toBe("paragraph");
    editor.destroy();
  });
});
