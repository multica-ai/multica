import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { forwardRef, useImperativeHandle, useRef } from "react";

let storedDraftMessage = "saved draft";
let liveEditorMarkdown = "";
const feedbackMocks = vi.hoisted(() => ({ mutateAsync: vi.fn() }));
// Deferred controlling the mock editor's in-flight upload: `reset` arms a new
// pending upload, `resolve` lands it so a test can watch the gate re-open.
const pendingUpload = vi.hoisted(() => {
  const deferred = {
    promise: Promise.resolve() as Promise<void>,
    resolve: () => {},
    reset() {
      deferred.promise = new Promise<void>((res) => {
        deferred.resolve = res;
      });
    },
  };
  return deferred;
});

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key, i18n: { changeLanguage: vi.fn() } }),
  Trans: ({ children }: { children: any }) => children,
  initReactI18next: { type: "3rdParty", init: vi.fn() },
}));

vi.mock("../i18n", () => ({
  useT: () => ({
    t: (selector: (resources: any) => string) =>
      selector({
        feedback: {
          title: "Feedback",
          github_hint_prefix: "Prefer GitHub? ",
          github_hint_link: "Open an issue",
          placeholder: "Tell us what happened",
          toast_uploading: "Uploading",
          toast_too_long: "Too long",
          toast_sent: "Sent",
          toast_failed: "Failed",
          sending: "Sending",
          send: "Send",
        },
        // The `editor` namespace's shared upload-gate copy. This mock ignores
        // the namespace argument, so both bundles live in one object.
        upload: {
          in_progress: "Uploading…",
        },
      }),
  }),
}));

vi.mock("@multica/core/paths", () => ({ useCurrentWorkspace: () => ({ id: "ws1" }) }));
vi.mock("@multica/core/hooks/use-file-upload", () => ({
  useFileUpload: () => ({ uploadWithToast: vi.fn() }),
}));
vi.mock("@multica/core/api", () => ({ api: {} }));
vi.mock("sonner", () => ({ toast: { info: vi.fn(), error: vi.fn(), success: vi.fn() } }));
vi.mock("@multica/core/feedback", () => ({
  FEEDBACK_KINDS: ["bug", "feature", "general", "praise"] as const,
  useCreateFeedback: () => ({ isPending: false, mutateAsync: feedbackMocks.mutateAsync }),
  useFeedbackDraftStore: (selector: any) =>
    selector({ draft: { message: storedDraftMessage }, setDraft: vi.fn(), clearDraft: vi.fn() }),
}));
vi.mock("../editor", async () => {
  // Real submit gate (pure React) driven by the mock editor's
  // `hasActiveUploads` / `onUploadingChange`.
  const uploadGate = await vi.importActual<typeof import("../editor/use-upload-gate")>(
    "../editor/use-upload-gate",
  );
  const ContentEditor = forwardRef(({ defaultValue, onSubmit, onUploadingChange }: any, ref) => {
    liveEditorMarkdown = defaultValue;
    // Mirrors the real editor: the placeholder node is in the doc from before
    // the await until the upload settles, and the host hears about it through
    // onUploadingChange rather than polling.
    const inFlightRef = useRef(0);
    useImperativeHandle(ref, () => ({
      hasActiveUploads: () => inFlightRef.current > 0,
      getMarkdown: () => liveEditorMarkdown,
      uploadFile: async () => {
        inFlightRef.current += 1;
        if (inFlightRef.current === 1) onUploadingChange?.(true);
        try {
          await pendingUpload.promise;
        } finally {
          inFlightRef.current -= 1;
          if (inFlightRef.current === 0) onUploadingChange?.(false);
        }
      },
    }));
    return (
      <textarea
        aria-label="feedback editor"
        defaultValue={defaultValue}
        onChange={(event) => { liveEditorMarkdown = event.currentTarget.value; }}
        onKeyDown={(event) => {
          if (event.key === "Enter" && event.metaKey) void onSubmit?.();
        }}
      />
    );
  });
  ContentEditor.displayName = "MockContentEditor";
  return {
    ...uploadGate,
    useEditorUpload: () => ({
      uploadWithToast: vi.fn(),
      upload: vi.fn(),
      uploading: false,
    }),
    ContentEditor,
    useFileDropZone: () => ({ isDragOver: false, dropZoneProps: {} }),
    FileDropOverlay: () => null,
    FileUploadButton: () => <button type="button">Upload</button>,
  };
});

import { FeedbackModal } from "./feedback";

describe("FeedbackModal", () => {
  beforeEach(() => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    feedbackMocks.mutateAsync.mockReset().mockResolvedValue(undefined);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("uses a crash-report initialMessage when there is no saved draft", () => {
    storedDraftMessage = "";

    render(<FeedbackModal onClose={vi.fn()} initialMessage="kind: desktop_route_error" />);

    expect(screen.getByLabelText("feedback editor")).toHaveValue("kind: desktop_route_error");
  });

  it("does not overwrite an existing feedback draft when crash report context is provided", () => {
    storedDraftMessage = "saved draft";

    render(<FeedbackModal onClose={vi.fn()} initialMessage="kind: desktop_route_error" />);

    expect(screen.getByLabelText("feedback editor")).toHaveValue(
      "saved draft\n\n---\n\nkind: desktop_route_error",
    );
  });

  it("submits the editor's latest markdown before debounced state catches up", async () => {
    storedDraftMessage = "";
    render(<FeedbackModal onClose={vi.fn()} />);

    const editor = screen.getByLabelText("feedback editor");
    fireEvent.change(editor, { target: { value: "fresh feedback" } });
    fireEvent.keyDown(editor, { key: "Enter", metaKey: true });

    await waitFor(() => {
      expect(feedbackMocks.mutateAsync).toHaveBeenCalledWith(
        expect.objectContaining({ message: "fresh feedback" }),
      );
    });
  });

  // MUL-4808 — Feedback refused to submit mid-upload inside the handler, but
  // the Send button stayed enabled, so the only signal was a toast fired after
  // a click that looked like it should have worked.
  describe("upload submit gate", () => {
    function startPendingUpload() {
      pendingUpload.reset();
      // The modal renders through a portal, so its file input lives on
      // document.body rather than under render()'s container.
      const input = document.body.querySelector('input[type="file"]');
      if (!input) throw new Error("Expected a file input to render");
      fireEvent.change(input, {
        target: { files: [new File(["x"], "shot.png", { type: "image/png" })] },
      });
    }

    it("disables Send and shows Uploading… while an upload is in flight", async () => {
      // Seeded through the draft so `message` is non-empty on mount — that
      // isolates the disabled state to the upload gate rather than the
      // empty-content check.
      storedDraftMessage = "here's a screenshot";
      render(<FeedbackModal onClose={vi.fn()} />);

      startPendingUpload();

      const send = await screen.findByRole("button", { name: "Uploading…" });
      await waitFor(() => expect(send).toBeDisabled());
      expect(send).toHaveAttribute("aria-busy", "true");

      await act(async () => { pendingUpload.resolve(); });
      await waitFor(() =>
        expect(screen.getByRole("button", { name: "Send" })).not.toBeDisabled(),
      );
    });

    it("blocks the Cmd+Enter path while an upload is in flight", async () => {
      storedDraftMessage = "here's a screenshot";
      render(<FeedbackModal onClose={vi.fn()} />);
      const editor = screen.getByLabelText("feedback editor");

      startPendingUpload();

      fireEvent.keyDown(editor, { key: "Enter", metaKey: true });
      await Promise.resolve();
      expect(feedbackMocks.mutateAsync).not.toHaveBeenCalled();

      await act(async () => { pendingUpload.resolve(); });
      fireEvent.keyDown(editor, { key: "Enter", metaKey: true });
      await waitFor(() => expect(feedbackMocks.mutateAsync).toHaveBeenCalled());
    });
  });
});
