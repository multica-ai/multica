import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { forwardRef, useImperativeHandle } from "react";

let storedDraftMessage = "saved draft";
let liveEditorMarkdown = "";
const feedbackMocks = vi.hoisted(() => ({ mutateAsync: vi.fn() }));

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
vi.mock("../editor", () => {
  const ContentEditor = forwardRef(({ defaultValue, onSubmit }: any, ref) => {
    liveEditorMarkdown = defaultValue;
    useImperativeHandle(ref, () => ({
      hasActiveUploads: () => false,
      getMarkdown: () => liveEditorMarkdown,
      uploadFile: vi.fn(),
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
});
