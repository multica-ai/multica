import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import { forwardRef, useImperativeHandle, type Ref } from "react";
import { renderWithI18n } from "../../test/i18n";

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

// CEREBRO-PATCH(comment-drafts): this unit test focuses on CommentInput's
// editor wiring; draft persistence is covered in cerebro-comment-drafts.
vi.mock("@multica/cerebro-comment-drafts", () => ({
  DraftSavedHint: () => null,
  useCommentDraft: () => ({
    defaultValue: "",
    save: vi.fn(),
    clear: vi.fn(),
    saved: false,
  }),
}));

// JEH-1065: pin-input is a cerebro affordance gated by `pinnable`. Stub the
// package so this test stays focused on autoFocus wiring without spinning
// up the feature-flags store or anchor measurement.
vi.mock("@multica/cerebro-pin-input", () => ({
  PinButton: () => null,
  useFloatPosition: () => null,
  useInputPin: () => ({ enabled: false, isPinned: false, togglePin: () => {}, unpin: () => {} }),
}));

// FIR-2392: the trigger-target bar reaches for the auth + workspace stores
// and the agent/squad/member lists. None of that is in scope for these
// autofocus / click-to-focus tests — stub the bar so CommentInput renders
// without spinning up the platform layer.
vi.mock("./trigger-target-bar", () => ({
  TriggerTargetBar: () => null,
  memberMentionMarkdown: () => "",
}));

// CEREBRO-PATCH(private-agent-send-confirm): FIR-32 — stub the send-confirm hook
// (reaches the flag/query/auth stores) so these focus-wiring tests stay platform-free.
vi.mock("@multica/cerebro-access/views", () => ({
  usePrivateAgentSendConfirm: () => ({
    confirmBeforeSend: async () => true,
    confirmDialog: null,
  }),
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

// CEREBRO-PATCH(composer-height-cap): TECH-3536 — mobile height cap + expand pill.
describe("CommentInput — TECH-3536 mobile height cap + expand pill", () => {
  let origInnerWidth: number;
  let origVisualViewport: typeof window.visualViewport;

  function setViewport(innerWidth: number, vvHeight: number | null) {
    Object.defineProperty(window, "innerWidth", {
      configurable: true,
      value: innerWidth,
    });
    Object.defineProperty(window, "visualViewport", {
      configurable: true,
      value:
        vvHeight == null
          ? null
          : { height: vvHeight, addEventListener: () => {}, removeEventListener: () => {} },
    });
  }

  beforeEach(() => {
    origInnerWidth = window.innerWidth;
    origVisualViewport = window.visualViewport;
  });

  afterEach(() => {
    Object.defineProperty(window, "innerWidth", {
      configurable: true,
      value: origInnerWidth,
    });
    Object.defineProperty(window, "visualViewport", {
      configurable: true,
      value: origVisualViewport,
    });
  });

  it("caps the editor at 50% of the viewport and grows to 80% on toggle", () => {
    setViewport(400, 800); // mobile width, 800px of space above the keyboard
    const { getByTestId, getByRole } = renderWithI18n(
      <CommentInput issueId="c1" onSubmit={vi.fn()} />,
    );

    const scrollContainer = getByTestId("content-editor").parentElement!;
    // Collapsed: a maxHeight cap so the field auto-grows up to 50%.
    expect(scrollContainer.style.maxHeight).toBe("400px"); // 50% of 800
    expect(scrollContainer.style.height).toBe("");

    // Expanded: a concrete height so the field visibly jumps even when empty.
    fireEvent.click(getByRole("button", { name: "Expand" }));
    expect(scrollContainer.style.height).toBe("640px"); // 80% of 800
    expect(scrollContainer.style.maxHeight).toBe("");

    fireEvent.click(getByRole("button", { name: "Collapse" }));
    expect(scrollContainer.style.maxHeight).toBe("400px");
    expect(scrollContainer.style.height).toBe("");
  });

  it("caps at 240px on desktop and still shows the unified pill", () => {
    setViewport(1280, 900); // desktop width
    const { getByTestId, getByRole } = renderWithI18n(
      <CommentInput issueId="c1" onSubmit={vi.fn()} />,
    );

    const scrollContainer = getByTestId("content-editor").parentElement!;
    expect(scrollContainer.style.maxHeight).toBe("240px");

    // The expand control is identical on every surface now (mobile + desktop).
    fireEvent.click(getByRole("button", { name: "Expand" }));
    expect(scrollContainer.style.height).toBe("630px"); // 70% of 900
  });
});
