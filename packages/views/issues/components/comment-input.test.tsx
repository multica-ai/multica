import { describe, it, expect, vi, beforeEach } from "vitest";
import { render } from "@testing-library/react";
import { forwardRef, useImperativeHandle, type Ref } from "react";

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
        focus: () => {},
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

import { CommentInput } from "./comment-input";

beforeEach(() => {
  autoFocusEvents.length = 0;
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
