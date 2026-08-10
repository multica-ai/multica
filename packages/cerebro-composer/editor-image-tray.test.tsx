import { describe, it, expect, vi, beforeEach } from "vitest";
import {
  render,
  screen,
  fireEvent,
  cleanup,
  waitFor,
  act,
} from "@testing-library/react";
import { createRef } from "react";
import { EditorImageTray } from "./editor-image-tray";
import type { ContentEditorRef } from "@multica/views/editor";
import { MOVE_IMAGE_TO_TRAY_EVENT } from "@multica/cerebro-editor-image";

// Shared spy for the inner ContentEditor.uploadFile so tests can assert whether
// a file was routed inline (embed) vs diverted to the tray.
const {
  innerUploadFile,
  innerChooseImage,
  innerInsertContentAt,
  innerUploadAndInsertFile,
  innerEditor,
} = vi.hoisted(() => {
  const innerInsertContentAt = vi.fn();
  const chain = {
    focus: vi.fn(),
    insertContentAt: vi.fn(),
    run: vi.fn(() => true),
  };
  chain.focus.mockReturnValue(chain);
  chain.insertContentAt.mockImplementation((pos, content) => {
    innerInsertContentAt(pos, content);
    return chain;
  });
  const innerNodeDom = {
    getBoundingClientRect: vi.fn(() => ({ top: 10, bottom: 50 })),
  };
  return {
    innerUploadFile: vi.fn(),
    innerChooseImage: vi.fn(),
    innerInsertContentAt,
    innerUploadAndInsertFile: vi.fn(),
    innerNodeDom,
    innerEditor: {
      isDestroyed: false,
      state: {
        selection: { from: 2 },
        doc: {
          content: { size: 5 },
          forEach: (callback: (node: { nodeSize: number }, offset: number) => void) =>
            callback({ nodeSize: 5 }, 0),
        },
      },
      view: { nodeDOM: vi.fn(() => innerNodeDom) },
      chain: () => chain,
    },
  };
});

// The real ContentEditor is a heavy Tiptap mount. EditorImageTray only drives
// its ref (getMarkdown) + onUpdate, so we stub it with a textarea that holds the
// markdown body and forwards edits — exactly the surface under test.
vi.mock("@multica/views/editor", async () => {
  const react = await import("react");
  const ContentEditor = react.forwardRef(function ContentEditor(
    props: {
      defaultValue?: string;
      onUpdate?: (md: string) => void;
      onEditorReady?: (editor: unknown) => void;
    },
    ref: React.Ref<unknown>,
  ) {
    const { defaultValue = "", onUpdate, onEditorReady } = props;
    const contentRef = react.useRef(defaultValue);
    // The real ContentEditor announces its editor instance once Tiptap has
    // built it. EditorImageTray refuses to persist before that, so the stub has
    // to make the same announcement or nothing is ever saved.
    react.useEffect(() => {
      onEditorReady?.(innerEditor);
      // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);
    react.useImperativeHandle(ref, () => ({
      getMarkdown: () => contentRef.current,
      clearContent: () => {
        contentRef.current = "";
      },
      insertText: () => {},
      replaceDictationPreview: () => {},
      commitDictationPreview: () => {},
      clearDictationPreview: () => {},
      focus: () => {},
      blur: () => {},
      uploadFile: (file: File, options?: { embedImage?: boolean }) =>
        innerUploadFile(file, options),
      chooseImage: innerChooseImage,
      hasActiveUploads: () => false,
    }));
    return react.createElement("textarea", {
      "data-testid": "editor",
      defaultValue,
      onChange: (e: { target: { value: string } }) => {
        contentRef.current = e.target.value;
        onUpdate?.(e.target.value);
      },
    });
  });
  return {
    ContentEditor,
    FileDropOverlay: () => null,
    useAttachmentPreview: () => ({ open: vi.fn(), modal: null }),
    uploadAndInsertFile: innerUploadAndInsertFile,
  };
});

// Flag on → exercise the tray path.
vi.mock("@multica/cerebro-feature-flags", () => ({
  useFeatureFlag: () => true,
}));

// AttachmentChip pulls in heavy UI; stub it to a labelled remove button.
vi.mock("@multica/cerebro-ui", async () => {
  const react = await import("react");
  return {
    AttachmentChip: ({
      filename,
      onRemove,
      onActivate,
    }: {
      filename: string;
      onRemove: () => void;
      onActivate?: () => void;
    }) =>
      react.createElement(
        "span",
        { "data-testid": "chip" },
        react.createElement("button", { onClick: onActivate }, filename),
        react.createElement(
          "button",
          { "aria-label": `remove ${filename}`, onClick: () => onRemove() },
          "x",
        ),
      ),
  };
});

const WITH_TWO =
  "Body text\n\n![image 1](https://cdn/a.png)\n![image 2](https://cdn/b.png)";

beforeEach(() => {
  cleanup();
  innerUploadFile.mockClear();
  innerInsertContentAt.mockClear();
  innerUploadAndInsertFile.mockClear();
  // jsdom has no object-URL impl; the tray guards on typeof, so stub it.
  if (typeof URL.createObjectURL !== "function") {
    URL.createObjectURL = () => "blob:stub";
    URL.revokeObjectURL = () => {};
  }
});

describe("EditorImageTray persistence round-trip", () => {
  it("contains tray overflow inside a 390 px writing pane", () => {
    Object.defineProperty(window, "innerWidth", { configurable: true, value: 390 });
    const { container } = render(
      <EditorImageTray defaultValue={WITH_TWO} onUploadFile={vi.fn()} />,
    );

    expect(container.querySelector("[data-editor-image-tray]")).toHaveClass(
      "min-w-0",
      "max-w-full",
    );
    expect(container.querySelector("[data-editor-image-box]")).toHaveClass(
      "min-w-0",
      "max-w-full",
    );
    expect(screen.getByRole("list", { name: "Attached images" })).toHaveClass(
      "min-w-0",
      "max-w-full",
      "overflow-x-auto",
    );
  });

  it("lifts trailing tray images out of the body into the numbered row", () => {
    render(
      <EditorImageTray defaultValue={WITH_TWO} onUploadFile={vi.fn()} />,
    );
    // Body seeds the editor without the trailing image block.
    expect((screen.getByTestId("editor") as HTMLTextAreaElement).value).toBe(
      "Body text",
    );
    // Two thumbnails with the recovered filenames.
    const chips = screen.getAllByTestId("chip");
    expect(chips).toHaveLength(2);
    expect(chips[0]).toHaveTextContent("a.png");
    expect(chips[1]).toHaveTextContent("b.png");
  });

  it("getMarkdown() recombines the body and the tray images", () => {
    const ref = createRef<ContentEditorRef>();
    render(
      <EditorImageTray ref={ref} defaultValue={WITH_TWO} onUploadFile={vi.fn()} />,
    );
    expect(ref.current?.getMarkdown()).toBe(WITH_TWO);
  });

  it("does not emit onUpdate on mount (unchanged content)", () => {
    const onUpdate = vi.fn();
    render(
      <EditorImageTray
        defaultValue={WITH_TWO}
        onUpdate={onUpdate}
        onUploadFile={vi.fn()}
      />,
    );
    expect(onUpdate).not.toHaveBeenCalled();
  });

  it("re-emits body + tray when the editor body changes", () => {
    const onUpdate = vi.fn();
    render(
      <EditorImageTray
        defaultValue={WITH_TWO}
        onUpdate={onUpdate}
        onUploadFile={vi.fn()}
      />,
    );
    fireEvent.change(screen.getByTestId("editor"), {
      target: { value: "New body" },
    });
    expect(onUpdate).toHaveBeenLastCalledWith(
      "New body\n\n![image 1](https://cdn/a.png)\n![image 2](https://cdn/b.png)",
    );
  });

  // FIR-2714: an external attach button calls ref.uploadFile directly. Image
  // files must divert to the tray (land at the top), not go inline.
  it("routes an image passed to uploadFile() into the tray, not inline", async () => {
    const onUploadFile = vi
      .fn()
      .mockResolvedValue({ link: "https://cdn/c.png", id: "att-c", filename: "c.png" });
    const ref = createRef<ContentEditorRef>();
    render(<EditorImageTray ref={ref} onUploadFile={onUploadFile} />);

    const file = new File(["x"], "c.png", { type: "image/png" });
    await act(async () => {
      ref.current?.uploadFile(file);
    });

    await waitFor(() => {
      expect(onUploadFile).toHaveBeenCalledWith(file);
      expect(screen.getByTestId("chip")).toHaveTextContent("c.png");
    });
    // Shows as a tray chip and never went inline through the editor.
    expect(innerUploadFile).not.toHaveBeenCalled();
  });

  it("keeps an explicit embed (embedImage) inline via the editor", () => {
    const ref = createRef<ContentEditorRef>();
    render(<EditorImageTray ref={ref} onUploadFile={vi.fn()} />);

    const file = new File(["x"], "c.png", { type: "image/png" });
    ref.current?.uploadFile(file, { embedImage: true });

    expect(innerUploadFile).toHaveBeenCalledWith(file, { embedImage: true });
    expect(screen.queryByTestId("chip")).toBeNull();
  });

  it("routes a non-image file passed to uploadFile() inline", () => {
    const ref = createRef<ContentEditorRef>();
    render(<EditorImageTray ref={ref} onUploadFile={vi.fn()} />);

    const file = new File(["x"], "notes.pdf", { type: "application/pdf" });
    ref.current?.uploadFile(file);

    expect(innerUploadFile).toHaveBeenCalledWith(file, undefined);
    expect(screen.queryByTestId("chip")).toBeNull();
  });

  it("forwards the shared image picker command to the inner editor", () => {
    const ref = createRef<ContentEditorRef>();
    render(<EditorImageTray ref={ref} />);

    ref.current?.chooseImage();

    expect(innerChooseImage).toHaveBeenCalledOnce();
  });

  it("re-emits without a removed image and renumbers the rest", async () => {
    const onUpdate = vi.fn();
    render(
      <EditorImageTray
        defaultValue={WITH_TWO}
        onUpdate={onUpdate}
        onUploadFile={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByLabelText("remove a.png"));
    await waitFor(() => {
      expect(onUpdate).toHaveBeenLastCalledWith(
        "Body text\n\n![image 1](https://cdn/b.png)",
      );
    });
    expect(screen.getAllByTestId("chip")).toHaveLength(1);
  });

  it("places a tray image and its caption inline without uploading again", async () => {
    render(
      <EditorImageTray
        defaultValue={'Body\n\n![image 1](https://cdn/a.png "Quarterly result")'}
        onUploadFile={vi.fn()}
      />,
    );

    const placeInText = screen
      .getAllByRole("button", { name: "Place image 1 in text" })
      .find((button) => button.classList.contains("size-11"));
    expect(placeInText).toBeDefined();
    if (!placeInText) throw new Error("Place in text action not found");
    fireEvent.click(placeInText);

    expect(innerInsertContentAt).toHaveBeenCalledWith(2, [
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
    ]);
    expect(screen.queryByTestId("chip")).toBeNull();
    expect(innerUploadAndInsertFile).not.toHaveBeenCalled();
  });

  it("accepts Move to bottom from an inline image without uploading again", async () => {
    render(<EditorImageTray defaultValue="Body" onUploadFile={vi.fn()} />);
    const editor = screen.getByTestId("editor");
    const event = new CustomEvent(MOVE_IMAGE_TO_TRAY_EVENT, {
      bubbles: true,
      cancelable: true,
      detail: {
        src: "https://cdn/a.png",
        filename: "a.png",
        caption: "Quarterly result",
      },
    });

    editor.dispatchEvent(event);

    expect(event.defaultPrevented).toBe(true);
    await waitFor(() => {
      expect(screen.getByTestId("chip")).toHaveTextContent("a.png");
    });
  });

  it("draws the landing line and drops an image inline at the selected block edge", () => {
    render(<EditorImageTray defaultValue="Body" onUploadFile={vi.fn()} />);
    const editor = screen.getByTestId("editor");
    const file = new File(["x"], "a.png", { type: "image/png" });
    const dataTransfer = { types: ["Files"], files: [file] };

    fireEvent.dragOver(editor, { dataTransfer, clientX: 20, clientY: 40 });
    expect(screen.getByTestId("image-drop-landing-line")).toBeInTheDocument();

    fireEvent.drop(editor, { dataTransfer, clientX: 20, clientY: 40 });
    expect(innerUploadAndInsertFile).toHaveBeenCalledWith(
      innerEditor,
      file,
      expect.any(Function),
      5,
      { embedImage: true },
    );
  });

  it("routes a drop on the thumbnail tray to the tray instead of inline", async () => {
    const onUploadFile = vi.fn().mockResolvedValue({
      link: "https://cdn/c.png",
      id: "att-c",
      filename: "c.png",
    });
    render(
      <EditorImageTray
        defaultValue={WITH_TWO}
        onUploadFile={onUploadFile}
      />,
    );
    const file = new File(["x"], "c.png", { type: "image/png" });

    fireEvent.drop(screen.getByRole("list", { name: "Attached images" }), {
      dataTransfer: { types: ["Files"], files: [file] },
      clientX: 20,
      clientY: 40,
    });

    await waitFor(() => expect(onUploadFile).toHaveBeenCalledWith(file));
    expect(innerUploadAndInsertFile).not.toHaveBeenCalled();
  });
});
