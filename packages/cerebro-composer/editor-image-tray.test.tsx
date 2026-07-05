import { describe, it, expect, vi, beforeEach } from "vitest";
import {
  render,
  screen,
  fireEvent,
  cleanup,
  waitFor,
} from "@testing-library/react";
import { createRef } from "react";
import { EditorImageTray } from "./editor-image-tray";
import type { ContentEditorRef } from "@multica/views/editor";

// The real ContentEditor is a heavy Tiptap mount. EditorImageTray only drives
// its ref (getMarkdown) + onUpdate, so we stub it with a textarea that holds the
// markdown body and forwards edits — exactly the surface under test.
vi.mock("@multica/views/editor", async () => {
  const react = await import("react");
  const ContentEditor = react.forwardRef(function ContentEditor(
    props: { defaultValue?: string; onUpdate?: (md: string) => void },
    ref: React.Ref<unknown>,
  ) {
    const { defaultValue = "", onUpdate } = props;
    const contentRef = react.useRef(defaultValue);
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
      uploadFile: () => {},
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
    }: {
      filename: string;
      onRemove: () => void;
    }) =>
      react.createElement(
        "span",
        { "data-testid": "chip" },
        filename,
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
});

describe("EditorImageTray persistence round-trip", () => {
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
});
