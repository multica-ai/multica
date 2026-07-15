import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, cleanup, waitFor, act } from "@testing-library/react";
import { BaseComposer, type ComposerDraftHandle } from "./base-composer";

const apiMock = vi.hoisted(() => ({
  getMe: vi.fn(),
}));

const toastMock = vi.hoisted(() => ({
  error: vi.fn(),
}));

const composerFeatureFlags = vi.hoisted(() => ({
  imageTray: false,
}));

const imageTrayMock = vi.hoisted(() => ({
  items: [] as unknown[],
  addFiles: vi.fn(),
  remove: vi.fn(),
  takeForEmbed: vi.fn(),
  clear: vi.fn(),
  hasUploading: false,
  hasCompleted: false,
}));

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
    useAttachmentPreview: () => ({ open: vi.fn(), tryOpen: vi.fn(), modal: null }),
  };
});

vi.mock("@multica/cerebro-feature-flags", () => ({
  useFeatureFlag: (key: string) =>
    key === "cerebro_composer_image_tray" && composerFeatureFlags.imageTray,
}));

vi.mock("./use-image-tray", () => ({
  useImageTray: () => imageTrayMock,
  serializeTrayImages: () => ({ markdown: "", attachmentIds: [] }),
}));
vi.mock("./composer-image-tray", () => ({ ComposerImageTray: () => null }));

vi.mock("@multica/core/api", () => ({ api: apiMock }));
vi.mock("sonner", () => ({ toast: toastMock }));
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
// The dictation mic resolves a workspace-scoped transcribe endpoint (needs a
// QueryClient + workspace provider) — out of scope for the composer's
// submit-enable/layout tests. Stub it to inert nodes.
vi.mock("@multica/cerebro-dictation", () => ({
  EditorDictationMic: () => null,
  TranscribingSkeleton: () => null,
}));
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
beforeEach(() => {
  vi.useRealTimers();
  apiMock.getMe.mockReset();
  apiMock.getMe.mockResolvedValue({});
  toastMock.error.mockClear();
  composerFeatureFlags.imageTray = false;
  imageTrayMock.hasUploading = false;
  imageTrayMock.hasCompleted = false;
});

describe("BaseComposer draft parity", () => {
  it("disables scheduling while an image is uploading", () => {
    composerFeatureFlags.imageTray = true;
    imageTrayMock.hasUploading = true;

    render(
      <BaseComposer
        draft={makeDraft("message with upload")}
        onSubmit={vi.fn()}
        onSchedule={vi.fn()}
        scheduleControl={({ disabled }) => (
          <button aria-label="Schedule message" disabled={disabled} />
        )}
        editorKey="s1"
      />,
    );

    expect(screen.getByRole("button", { name: "Schedule message" })).toBeDisabled();
  });

  it("enables Submit on first render when a draft is restored, and sends it", async () => {
    const onSubmit = vi.fn();
    render(
      <BaseComposer draft={makeDraft("hello")} onSubmit={onSubmit} editorKey="s1" />,
    );

    // Regression guard (FIR-1762): a restored draft must be sendable without an
    // extra keypress — Submit enabled at first render, not disabled.
    expect(submit()).toBeEnabled();

    await act(async () => {
      fireEvent.click(submit());
    });
    expect(onSubmit).toHaveBeenCalledWith("hello", undefined);
  });

  it("keeps Submit disabled for an empty draft until the user types", async () => {
    const onSubmit = vi.fn();
    render(
      <BaseComposer draft={makeDraft("")} onSubmit={onSubmit} editorKey="s1" />,
    );

    expect(submit()).toBeDisabled();

    fireEvent.change(screen.getByTestId("editor"), { target: { value: "hi" } });
    expect(submit()).toBeEnabled();
    await act(async () => {
      fireEvent.click(submit());
    });
    expect(onSubmit).toHaveBeenCalledWith("hi", undefined);
  });

  it("keeps the draft when submit rejects", async () => {
    const draft = makeDraft("unsent comment");
    let rejectSubmit!: (error: Error) => void;
    const onSubmit = vi.fn().mockReturnValue(
      new Promise<void>((_resolve, reject) => {
        rejectSubmit = reject;
      }),
    );
    render(
      <BaseComposer draft={draft} onSubmit={onSubmit} editorKey="s1" />,
    );

    fireEvent.click(submit());

    await waitFor(() => expect(onSubmit).toHaveBeenCalledWith("unsent comment", undefined));
    expect(submit()).toBeDisabled();

    await act(async () => {
      rejectSubmit(new Error("Session expired"));
    });

    expect(draft.clear).not.toHaveBeenCalled();
    await waitFor(() => expect(submit()).toBeEnabled());
  });

  it("warns early when a saved draft exists but the connection check fails", async () => {
    vi.useFakeTimers();
    apiMock.getMe.mockRejectedValue(new Error("Cloudflare session expired"));
    const draft = makeDraft("");
    render(
      <BaseComposer draft={draft} onSubmit={vi.fn()} editorKey="s1" />,
    );

    fireEvent.change(screen.getByTestId("editor"), { target: { value: "half-written" } });
    await act(async () => {
      vi.advanceTimersByTime(1200);
      await Promise.resolve();
    });

    expect(toastMock.error).toHaveBeenCalledWith(
      "Connection lost. Your draft is saved locally. Sign in again before submitting.",
    );
    expect(draft.save).toHaveBeenCalledWith("half-written");
    expect(submit()).toBeEnabled();
  });

  it("does not repeat the connection warning on every keystroke while still disconnected", async () => {
    vi.useFakeTimers();
    apiMock.getMe.mockRejectedValue(new Error("Cloudflare session expired"));
    render(
      <BaseComposer draft={makeDraft("")} onSubmit={vi.fn()} editorKey="s1" />,
    );

    fireEvent.change(screen.getByTestId("editor"), { target: { value: "one" } });
    await act(async () => {
      vi.advanceTimersByTime(1200);
      await Promise.resolve();
    });
    fireEvent.change(screen.getByTestId("editor"), { target: { value: "one two" } });
    await act(async () => {
      vi.advanceTimersByTime(1200);
      await Promise.resolve();
    });

    expect(toastMock.error).toHaveBeenCalledTimes(1);
    expect(apiMock.getMe).toHaveBeenCalledTimes(1);
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
