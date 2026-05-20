import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import { forwardRef, useImperativeHandle, type Ref } from "react";

const focusSpy = vi.hoisted(() => vi.fn());

// JEH-756: focus is now Tiptap-native, configured via the `autoFocus` prop
// on ContentEditor. The comment-input layer only forwards the prop and
// remounts the editor on `issueId` change so Tiptap re-applies it for the
// new context.

const autoFocusEvents = vi.hoisted(() => [] as Array<boolean | undefined>);

vi.mock("@multica/core/hooks/use-file-upload", () => ({
  useFileUpload: () => ({ uploadWithToast: vi.fn() }),
}));

vi.mock("@multica/core/api", () => ({ api: {} }));

vi.mock("@multica/cerebro-preferences/views", () => ({
  useSubmitOnEnter: () => false,
}));

// JEH-1065: pin-input is a cerebro affordance gated by `pinnable`. Stub the
// package so this test stays focused on autoFocus wiring without spinning
// up the feature-flags store or anchor measurement.
vi.mock("@multica/cerebro-pin-input", () => ({
  PinButton: () => null,
  useFloatPosition: () => null,
  useInputPin: () => ({ enabled: false, isPinned: false, togglePin: () => {}, unpin: () => {} }),
}));

vi.mock("../../editor", () => {
  const ContentEditor = forwardRef(
    (
      props: { autoFocus?: boolean },
      ref: Ref<{
        getMarkdown: () => string;
        clearContent: () => void;
        uploadFile: () => void;
        focus: () => void;
      }>,
    ) => {
      autoFocusEvents.push(props.autoFocus);
      useImperativeHandle(ref, () => ({
        getMarkdown: () => "",
        clearContent: () => {},
        uploadFile: () => {},
        focus: focusSpy,
      }));
      return <div data-testid="content-editor" />;
    },
  );
  ContentEditor.displayName = "ContentEditor";
  return {
    ContentEditor,
    useFileDropZone: () => ({ isDragOver: false, dropZoneProps: {} }),
    FileDropOverlay: () => null,
  };
});

// CEREBRO-PATCH(editor-direct-path-mocks): cerebro's b787595c moved
// `comment-input.tsx` from the `../../editor` barrel to direct sub-module
// imports. Re-export from the barrel mock so the test mocks still apply.
vi.mock("../../editor/content-editor", async () => {
  const barrel = await import("../../editor");
  return {
    ContentEditor: (barrel as { ContentEditor: unknown }).ContentEditor,
  };
});
vi.mock("../../editor/file-drop-overlay", async () => {
  const barrel = await import("../../editor");
  return {
    FileDropOverlay: (barrel as { FileDropOverlay: unknown }).FileDropOverlay,
  };
});
vi.mock("../../editor/use-file-drop-zone", async () => {
  const barrel = await import("../../editor");
  return {
    useFileDropZone: (barrel as { useFileDropZone: unknown }).useFileDropZone,
  };
});

import { CommentInput } from "./comment-input";

beforeEach(() => {
  autoFocusEvents.length = 0;
  focusSpy.mockClear();
});

describe("CommentInput — JEH-756 autofocus prop wiring", () => {
  it("forwards autoFocus={true} to ContentEditor when the prop is set", () => {
    render(<CommentInput issueId="c1" onSubmit={vi.fn()} autoFocus />);
    expect(autoFocusEvents).toContain(true);
  });

  it("forwards autoFocus={false} when the prop is omitted", () => {
    render(<CommentInput issueId="c1" onSubmit={vi.fn()} />);
    expect(autoFocusEvents.every((v) => v === false)).toBe(true);
  });

  it("remounts the editor when issueId changes (channel switch)", () => {
    const { rerender } = render(
      <CommentInput issueId="c1" onSubmit={vi.fn()} autoFocus />,
    );
    const mountsBeforeSwitch = autoFocusEvents.length;

    rerender(<CommentInput issueId="c2" onSubmit={vi.fn()} autoFocus />);

    // The `key={issueId}` on ContentEditor unmounts the previous instance
    // and mounts a new one — that re-evaluation is what re-applies
    // Tiptap's `autofocus` for the new channel/DM.
    expect(autoFocusEvents.length).toBeGreaterThan(mountsBeforeSwitch);
    expect(autoFocusEvents[autoFocusEvents.length - 1]).toBe(true);
  });
});

describe("CommentInput — JEH-1200 click-to-focus", () => {
  it("focuses the editor when the user clicks the card padding", () => {
    const { getByTestId } = render(<CommentInput issueId="c1" onSubmit={vi.fn()} />);
    fireEvent.mouseDown(getByTestId("comment-input"));
    expect(focusSpy).toHaveBeenCalledTimes(1);
  });

  it("does not steal focus when the click target is itself interactive", () => {
    const { container } = render(<CommentInput issueId="c1" onSubmit={vi.fn()} />);
    const submitButton = container.querySelector("button[aria-label='Submit comment']");
    expect(submitButton).not.toBeNull();
    fireEvent.mouseDown(submitButton!);
    expect(focusSpy).not.toHaveBeenCalled();
  });
});
