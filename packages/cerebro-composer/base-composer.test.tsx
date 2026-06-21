import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, cleanup } from "@testing-library/react";
import { BaseComposer, type ComposerDraftHandle } from "./base-composer";

// The real ContentEditor is a heavy Tiptap mount; the composer's submit-enable
// logic does not depend on it, so we stub it with a textarea that holds the
// markdown (seeded from defaultValue) and forwards edits via onUpdate. This is
// exactly the surface BaseComposer drives: getMarkdown() at submit + onUpdate()
// while typing. Critically, the stub honours `key={editorKey}` remounts, so the
// draft re-seed on key change is exercised too.
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
      insertText: (t: string) => {
        contentRef.current += t;
      },
      focus: () => {},
      blur: () => {},
      uploadFile: () => {},
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
    useFileDropZone: () => ({ isDragOver: false, dropZoneProps: {} }),
    FileDropOverlay: () => null,
  };
});

vi.mock("@multica/core/api", () => ({ api: {} }));
vi.mock("@multica/core/hooks/use-file-upload", () => ({
  useFileUpload: () => ({ uploadWithToast: vi.fn() }),
}));
vi.mock("@multica/cerebro-preferences/views", () => ({
  useSubmitOnEnter: () => true,
}));
vi.mock("@multica/cerebro-pin-input", () => ({
  PinButton: () => null,
  useFloatPosition: () => null,
  useInputPin: () => ({
    enabled: false,
    isPinned: false,
    togglePin: vi.fn(),
    unpin: vi.fn(),
  }),
}));
vi.mock("@multica/cerebro-ui", () => ({
  ComposerExpandToggle: () => null,
  useComposerHeight: () => ({ showExpandToggle: false, containerStyle: undefined }),
}));
vi.mock("@multica/cerebro-comment-drafts", () => ({ DraftSavedHint: () => null }));
vi.mock("@multica/views/common/actor-avatar", () => ({ ActorAvatar: () => null }));
vi.mock("@multica/ui/components/common/file-upload-button", () => ({
  FileUploadButton: () => null,
}));
vi.mock("@multica/ui/lib/utils", () => ({
  cn: (...c: unknown[]) => c.filter(Boolean).join(" "),
}));
vi.mock("@multica/ui/components/ui/button", async () => {
  const react = await import("react");
  return {
    Button: ({
      children,
      ...rest
    }: { children?: unknown } & Record<string, unknown>) =>
      react.createElement("button", rest, children as never),
  };
});

function makeDraft(defaultValue: string): ComposerDraftHandle {
  return { defaultValue, save: vi.fn(), clear: vi.fn(), saved: false };
}

const submit = () => screen.getByRole("button", { name: "Submit" });

beforeEach(() => cleanup());

describe("BaseComposer draft parity", () => {
  it("enables Submit on first render when a draft is restored, and sends it", async () => {
    const onSubmit = vi.fn();
    render(
      <BaseComposer draft={makeDraft("hello")} onSubmit={onSubmit} editorKey="s1" />,
    );

    // Regression guard (FIR-1762): a restored draft must be sendable without an
    // extra keypress — Submit enabled at first render, not disabled.
    expect(submit()).toBeEnabled();

    fireEvent.click(submit());
    expect(onSubmit).toHaveBeenCalledWith("hello", undefined);
  });

  it("keeps Submit disabled for an empty draft until the user types", () => {
    const onSubmit = vi.fn();
    render(
      <BaseComposer draft={makeDraft("")} onSubmit={onSubmit} editorKey="s1" />,
    );

    expect(submit()).toBeDisabled();

    fireEvent.change(screen.getByTestId("editor"), { target: { value: "hi" } });
    expect(submit()).toBeEnabled();
    fireEvent.click(submit());
    expect(onSubmit).toHaveBeenCalledWith("hi", undefined);
  });

  it("re-seeds Submit-enable when editorKey switches to a key with a draft", () => {
    const onSubmit = vi.fn();
    const { rerender } = render(
      <BaseComposer draft={makeDraft("")} onSubmit={onSubmit} editorKey="s1" />,
    );
    expect(submit()).toBeDisabled();

    // Switching session/thread changes editorKey + supplies the new key's draft.
    // ContentEditor remounts; BaseComposer does not — the gate must still open.
    rerender(
      <BaseComposer draft={makeDraft("restored")} onSubmit={onSubmit} editorKey="s2" />,
    );
    expect(submit()).toBeEnabled();
  });
});

describe("BaseComposer framing (FIR-1790)", () => {
  it("draws the boxed chrome by default (Channels/DMs/Chat keep the box)", () => {
    render(<BaseComposer draft={makeDraft("")} onSubmit={vi.fn()} editorKey="s1" />);
    const field = screen.getByTestId("composer-input");
    expect(field.className).toContain("ring-border");
    expect(field.className).toContain("bg-card");
  });

  it("drops the box when frame=false (issue comment fields)", () => {
    render(
      <BaseComposer draft={makeDraft("")} onSubmit={vi.fn()} editorKey="s1" frame={false} />,
    );
    const field = screen.getByTestId("composer-input");
    expect(field.className).not.toContain("ring-border");
    expect(field.className).not.toContain("bg-card");
  });
});
