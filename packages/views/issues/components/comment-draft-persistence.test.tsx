import {
  forwardRef,
  useImperativeHandle,
  useRef,
  act,
  type Ref,
} from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { Attachment, TimelineEntry } from "@multica/core/types";
import { useCommentDraftStore } from "@multica/core/issues/stores";
import { CommentInput } from "./comment-input";
import { useEditAttachmentState } from "./comment-card";

// One controllable trigger agent so a suppression chip renders. Hoisted +
// stable so the composer's "reconcile suppressed against visible agents" effect
// runs once and toggling doesn't churn it.
const mockAgents = vi.hoisted(() => [{ id: "ag-1", name: "Walt", source: "mention_agent" }]);

vi.mock("../hooks/use-comment-trigger-preview", () => ({
  useCommentTriggerPreview: () => ({ agents: mockAgents }),
}));

// Test double for the chip strip: one toggle button per agent.
vi.mock("./comment-trigger-chips", () => ({
  CommentTriggerChips: ({
    agents,
    suppressedAgentIds,
    onToggle,
  }: {
    agents: { id: string; name: string }[];
    suppressedAgentIds: Set<string>;
    onToggle: (id: string) => void;
  }) => (
    <div>
      {agents.map((a) => (
        <button
          key={a.id}
          data-testid={`chip-${a.id}`}
          aria-pressed={suppressedAgentIds.has(a.id)}
          onClick={() => onToggle(a.id)}
        >
          {a.name}
        </button>
      ))}
    </div>
  ),
}));

vi.mock("@multica/core/api", () => ({ api: {} }));

vi.mock("@multica/core/hooks/use-file-upload", () => ({
  useFileUpload: () => ({ uploadWithToast: vi.fn() }),
}));

vi.mock("../../i18n", () => ({
  useT: () => ({ t: () => "translated" }),
  useTimeAgo: () => () => "now",
}));

vi.mock("../../editor", () => ({
  useFileDropZone: () => ({
    isDragOver: false,
    dropZoneProps: { "data-testid": "drop-zone" },
  }),
  FileDropOverlay: () => null,
  ContentEditor: forwardRef(function MockContentEditor(
    {
      defaultValue,
      onUpdate,
    }: {
      defaultValue?: string;
      onUpdate?: (markdown: string) => void;
    },
    ref: Ref<unknown>,
  ) {
    const valueRef = useRef(defaultValue ?? "");
    useImperativeHandle(ref, () => ({
      getMarkdown: () => valueRef.current,
      clearContent: () => {
        valueRef.current = "";
      },
      focus: () => {},
      blur: () => {},
      uploadFile: async () => {},
      hasActiveUploads: () => false,
    }));
    return (
      <textarea
        data-testid="editor"
        defaultValue={defaultValue}
        onChange={(e) => {
          valueRef.current = e.target.value;
          onUpdate?.(e.target.value);
        }}
      />
    );
  }),
}));

function makeAttachment(id: string, url: string): Attachment {
  return {
    id,
    workspace_id: "ws-1",
    issue_id: "issue-1",
    comment_id: null,
    chat_session_id: null,
    chat_message_id: null,
    uploader_type: "member",
    uploader_id: "u1",
    filename: `${id}.png`,
    url,
    download_url: url,
    markdown_url: url,
    content_type: "image/png",
    size_bytes: 1,
    created_at: "2026-01-01T00:00:00Z",
  };
}

beforeEach(() => {
  localStorage.clear();
  useCommentDraftStore.setState({ drafts: {} });
});

describe("comment composer suppression persistence", () => {
  it("writes suppressedAgentIds:[] on un-suppress (no stale array, no empty draft) and a remount stays un-suppressed", async () => {
    const view = render(<CommentInput issueId="issue-1" onSubmit={vi.fn().mockResolvedValue(true)} />);

    // A content draft must exist for suppression to persist.
    fireEvent.change(screen.getByTestId("editor"), { target: { value: "ping @walt" } });

    fireEvent.click(screen.getByTestId("chip-ag-1")); // suppress
    await waitFor(() =>
      expect(
        useCommentDraftStore.getState().getDraft("new:issue-1")?.suppressedAgentIds,
      ).toEqual(["ag-1"]),
    );

    fireEvent.click(screen.getByTestId("chip-ag-1")); // un-suppress
    await waitFor(() =>
      expect(
        useCommentDraftStore.getState().getDraft("new:issue-1")?.suppressedAgentIds,
      ).toEqual([]),
    );

    // The content draft is preserved (not wiped, not turned into an empty draft).
    expect(useCommentDraftStore.getState().getDraft("new:issue-1")?.content).toBe(
      "ping @walt",
    );

    // Remount: the composer hydrates from the draft — the agent must NOT come
    // back suppressed off a stale array.
    view.unmount();
    render(<CommentInput issueId="issue-1" onSubmit={vi.fn().mockResolvedValue(true)} />);
    expect(screen.getByTestId("chip-ag-1")).toHaveAttribute("aria-pressed", "false");
  });

  it("never creates a content-only empty draft just from toggling suppression", () => {
    render(<CommentInput issueId="issue-1" onSubmit={vi.fn().mockResolvedValue(true)} />);
    // No content typed → toggling suppression must not persist anything.
    fireEvent.click(screen.getByTestId("chip-ag-1"));
    fireEvent.click(screen.getByTestId("chip-ag-1"));
    expect(useCommentDraftStore.getState().getDraft("new:issue-1")).toBeUndefined();
  });
});

// ---------------------------------------------------------------------------
// Edit composer: attachments survive a remount (blocker 3, edit path)
// ---------------------------------------------------------------------------

type EditApi = ReturnType<typeof useEditAttachmentState>;

const MockEditEditor = forwardRef(function MockEditEditor(
  { defaultValue }: { defaultValue?: string },
  ref: Ref<unknown>,
) {
  const valueRef = useRef(defaultValue ?? "");
  useImperativeHandle(ref, () => ({
    getMarkdown: () => valueRef.current,
    clearContent: () => {
      valueRef.current = "";
    },
    focus: () => {},
    blur: () => {},
    uploadFile: async () => {},
    hasActiveUploads: () => false,
  }));
  return <textarea data-testid="edit-editor" defaultValue={defaultValue} />;
});

function EditHarness({
  entry,
  onEdit,
  capture,
}: {
  entry: TimelineEntry;
  onEdit: (
    commentId: string,
    content: string,
    attachmentIds: string[],
    suppressAgentIds?: string[],
  ) => Promise<void>;
  capture: (api: EditApi) => void;
}) {
  const edit = useEditAttachmentState("issue-1", entry, onEdit);
  capture(edit);
  return edit.editing ? (
    <MockEditEditor ref={edit.editorRef} defaultValue={edit.initialValue} />
  ) : null;
}

describe("edit composer attachment persistence", () => {
  it("re-hydrates an uploaded attachment on startEdit so save keeps its attachment id", async () => {
    const att = makeAttachment("att-1", "http://x/att-1.png");
    // A prior edit session uploaded att-1 (its URL is in the body) then the tab
    // switched — the edit draft persisted content + attachments.
    useCommentDraftStore.getState().setDraft("edit:issue-1:c-1", {
      content: "edited body http://x/att-1.png",
      attachments: [att],
    });

    const entry = {
      id: "c-1",
      content: "original body",
      attachments: [],
      parent_id: null,
    } as unknown as TimelineEntry;
    const onEdit = vi.fn().mockResolvedValue(undefined);

    let api: EditApi | null = null;
    render(
      <EditHarness
        entry={entry}
        onEdit={onEdit}
        capture={(a) => {
          api = a;
        }}
      />,
    );

    act(() => {
      api!.startEdit();
    });
    await act(async () => {
      await api!.saveEdit();
    });

    expect(onEdit).toHaveBeenCalledWith(
      "c-1",
      "edited body http://x/att-1.png",
      ["att-1"],
      undefined,
    );
  });
});
