import { act, renderHook, waitFor } from "@testing-library/react";
import type { WorkspaceFileReference } from "@tutti-os/workspace-file-reference/contracts";
import type { PropsWithChildren } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  resetAgentHostApiForTests,
  setAgentHostApiForTests
} from "../../../agentActivityHost";
import {
  ConversationServicesProvider,
  type ConversationServices
} from "../../../conversationServices";
import { createAgentComposerFileMentionMarkdown } from "../agentRichText/agentMentionMarkdown";
import type { AgentRichTextEditorHandle } from "../agentRichText/AgentRichTextEditor";
import { buildAgentComposerDraft } from "../model/agentComposerDraft";
import type {
  AgentExternalPromptFilePreparationResult,
  AgentExternalPromptFilePreparer
} from "../model/agentExternalPromptFiles";
import type { AgentComposerDraft } from "../model/agentGuiNodeTypes";
import { useComposerDraftAttachments } from "./useComposerDraftAttachments";

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((next) => {
    resolve = next;
  });
  return { promise, resolve };
}

describe("useComposerDraftAttachments", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    resetAgentHostApiForTests();
  });
  it("publishes an uploading file before applying prepared locators", async () => {
    const preparation =
      deferred<readonly AgentExternalPromptFilePreparationResult[]>();
    const prepareExternalPromptFiles = vi.fn<AgentExternalPromptFilePreparer>(
      () => preparation.promise
    );
    const draft = buildAgentComposerDraft({ prompt: "" });
    const onDraftContentChange =
      vi.fn<(draft: AgentComposerDraft, sourceScopeKey?: string) => void>();
    const insertComposerFiles = vi.fn();
    const input = createInput({
      draft,
      editorHandle: { insertComposerFiles },
      onDraftContentChange,
      prepareExternalPromptFiles
    });
    const rendered = renderHook(() => useComposerDraftAttachments(input));
    const file = new File(["hello"], "hello.txt", { type: "text/plain" });

    act(() => rendered.result.current.addDraftFiles([file]));
    const inserted = insertComposerFiles.mock.calls[0]![0]![0];
    act(() =>
      rendered.result.current.handleDraftChange(
        createAgentComposerFileMentionMarkdown(inserted)
      )
    );

    expect(onDraftContentChange).toHaveBeenCalledWith([
      { type: "text", text: "" },
      expect.objectContaining({
        type: "file",
        kind: "file",
        name: "hello.txt",
        uploading: true
      })
    ]);

    act(() => {
      preparation.resolve([
        {
          sourceIndex: 0,
          status: "prepared",
          file: {
            name: "hello.txt",
            path: "/runtime/hello.txt",
            sizeBytes: 5
          }
        }
      ]);
    });

    await waitFor(() =>
      expect(onDraftContentChange).toHaveBeenLastCalledWith([
        expect.objectContaining({
          type: "text",
          text: expect.stringContaining("mention://composer-file/")
        }),
        expect.objectContaining({
          type: "file",
          kind: "file",
          path: "/runtime/hello.txt",
          uploading: false
        })
      ])
    );
  });

  it("preserves an uploading file when prompt text changes", async () => {
    const preparation =
      deferred<readonly AgentExternalPromptFilePreparationResult[]>();
    const prepareExternalPromptFiles = vi.fn<AgentExternalPromptFilePreparer>(
      () => preparation.promise
    );
    const onDraftContentChange =
      vi.fn<(draft: AgentComposerDraft, sourceScopeKey?: string) => void>();
    const insertComposerFiles = vi.fn();
    const input = createInput({
      draft: buildAgentComposerDraft({ prompt: "" }),
      editorHandle: { insertComposerFiles },
      onDraftContentChange,
      prepareExternalPromptFiles
    });
    const rendered = renderHook(() => useComposerDraftAttachments(input));

    act(() => {
      rendered.result.current.addDraftFiles([
        new File(["pdf"], "report.pdf", { type: "application/pdf" })
      ]);
      const inserted = insertComposerFiles.mock.calls[0]![0]![0];
      rendered.result.current.handleDraftChange(
        `summarize ${createAgentComposerFileMentionMarkdown(inserted)} this pdf`
      );
    });

    expect(onDraftContentChange).toHaveBeenLastCalledWith([
      expect.objectContaining({
        type: "text",
        text: expect.stringContaining("mention://composer-file/")
      }),
      expect.objectContaining({
        type: "file",
        kind: "file",
        name: "report.pdf",
        uploading: true
      })
    ]);

    act(() => {
      preparation.resolve([
        {
          sourceIndex: 0,
          status: "prepared",
          file: {
            name: "report.pdf",
            path: "/runtime/report.pdf",
            sizeBytes: 3
          }
        }
      ]);
    });

    await waitFor(() =>
      expect(onDraftContentChange).toHaveBeenLastCalledWith([
        expect.objectContaining({
          type: "text",
          text: expect.stringContaining("status=ready")
        }),
        expect.objectContaining({
          type: "file",
          kind: "file",
          path: "/runtime/report.pdf",
          uploading: false
        })
      ])
    );
  });

  it("preserves a newly inserted file across a stale editor update", () => {
    const input = createInput({
      draft: buildAgentComposerDraft({ prompt: "" }),
      prepareExternalPromptFiles: () => new Promise(() => undefined)
    });
    const rendered = renderHook(() => useComposerDraftAttachments(input));

    act(() => {
      rendered.result.current.addDraftFiles([
        new File(["pdf"], "report.pdf", { type: "application/pdf" })
      ]);
      rendered.result.current.handleDraftChange("");
    });

    expect(input.draftByScopeKeyRef.current.home).toEqual([
      { type: "text", text: "" },
      expect.objectContaining({
        type: "file",
        name: "report.pdf",
        uploading: true
      })
    ]);
  });

  it("reports external file preparation lifecycle without file metadata", async () => {
    const hostToastError = vi.fn();
    setAgentHostApiForTests({ toast: { error: hostToastError } } as never);
    const reportDiagnostic = vi.fn();
    const runtime = {
      origin: "test",
      diagnostics: { reportDiagnostic }
    } as unknown as ConversationServices;
    const wrapper = ({ children }: PropsWithChildren) => (
      <ConversationServicesProvider runtime={runtime}>
        {children}
      </ConversationServicesProvider>
    );
    const prepareExternalPromptFiles = vi.fn<AgentExternalPromptFilePreparer>(
      async () => [
        {
          sourceIndex: 0,
          status: "error",
          errorCode: "file_too_large",
          retryable: false
        }
      ]
    );
    const input = createInput({
      draft: buildAgentComposerDraft({ prompt: "" }),
      prepareExternalPromptFiles
    });
    const rendered = renderHook(() => useComposerDraftAttachments(input), {
      wrapper
    });

    act(() =>
      rendered.result.current.addDraftFiles([
        new File(["secret"], "private.txt", { type: "text/plain" })
      ])
    );
    const inserted = input.editorHandleRef.current
      .insertComposerFiles as ReturnType<typeof vi.fn>;
    const mention = inserted.mock.calls[0]![0]![0];
    act(() =>
      rendered.result.current.handleDraftChange(
        createAgentComposerFileMentionMarkdown(mention)
      )
    );

    await waitFor(() => expect(reportDiagnostic).toHaveBeenCalledTimes(2));
    expect(hostToastError).toHaveBeenCalledWith("File is too large");
    expect(reportDiagnostic).toHaveBeenNthCalledWith(
      1,
      expect.objectContaining({
        event: "agent.gui.composer.file_preparation.requested",
        details: {
          acceptedFileCount: 1,
          existingFileCount: 0,
          requestedFileCount: 1
        }
      })
    );
    expect(reportDiagnostic).toHaveBeenNthCalledWith(
      2,
      expect.objectContaining({
        event: "agent.gui.composer.file_preparation.settled",
        level: "warn",
        details: {
          draftUpdateApplied: true,
          errorCount: 1,
          settledFileCount: 1,
          visibleFileCount: 1
        }
      })
    );
    expect(JSON.stringify(reportDiagnostic.mock.calls)).not.toContain(
      "private.txt"
    );
    expect(JSON.stringify(reportDiagnostic.mock.calls)).not.toContain("secret");
  });

  it("does not revive a file deleted before preparation settles", async () => {
    const preparation =
      deferred<readonly AgentExternalPromptFilePreparationResult[]>();
    const insertComposerFiles = vi.fn();
    const onDraftContentChange = vi.fn();
    const input = createInput({
      draft: buildAgentComposerDraft({ prompt: "" }),
      editorHandle: { insertComposerFiles },
      onDraftContentChange,
      prepareExternalPromptFiles: () => preparation.promise
    });
    const rendered = renderHook(() => useComposerDraftAttachments(input));

    act(() =>
      rendered.result.current.addDraftFiles([
        new File(["pdf"], "report.pdf", { type: "application/pdf" })
      ])
    );
    const inserted = insertComposerFiles.mock.calls[0]![0]![0];
    act(() => {
      rendered.result.current.handleDraftChange(
        createAgentComposerFileMentionMarkdown(inserted)
      );
      rendered.result.current.handleDraftChange("");
      preparation.resolve([
        {
          sourceIndex: 0,
          status: "prepared",
          file: { name: "report.pdf", path: "/runtime/report.pdf" }
        }
      ]);
    });

    await waitFor(() => expect(onDraftContentChange).toHaveBeenCalled());
    expect(input.draftByScopeKeyRef.current.home).toEqual([
      { type: "text", text: "" }
    ]);
  });

  it("keeps picker files as workspace references", async () => {
    const reference = {
      kind: "file",
      path: "/workspace/notes.md",
      displayName: "notes.md"
    } as WorkspaceFileReference;
    const insertWorkspaceReferences = vi.fn();
    const prepareExternalPromptFiles = vi.fn<AgentExternalPromptFilePreparer>();
    const input = createInput({
      draft: buildAgentComposerDraft({ prompt: "" }),
      editorHandle: { insertWorkspaceReferences },
      prepareExternalPromptFiles,
      onRequestWorkspaceReferences: async () => ({
        files: [reference],
        mentionItems: []
      })
    });
    const rendered = renderHook(() => useComposerDraftAttachments(input));

    await act(() => rendered.result.current.handleWorkspaceReferencePicker());

    expect(insertWorkspaceReferences).toHaveBeenCalledWith([reference]);
    expect(prepareExternalPromptFiles).not.toHaveBeenCalled();
  });
  it("opens a ready composer-file mention from the draft registry", () => {
    const onLinkAction = vi.fn();
    const draftFilesRef = {
      current: [
        {
          id: "file-1",
          name: "archive.zip",
          path: "/workspace/room-1/archive.zip"
        }
      ]
    };
    const input = {
      ...createInput({
        draft: buildAgentComposerDraft({ prompt: "" }),
        prepareExternalPromptFiles: vi.fn(async () => [])
      }),
      draftFilesRef,
      onLinkAction
    };
    const rendered = renderHook(() => useComposerDraftAttachments(input));

    act(() => {
      rendered.result.current.handleLinkClick(
        createAgentComposerFileMentionMarkdown({
          id: "file-1",
          name: "archive.zip",
          status: "ready"
        }).match(/\(([^)]+)\)/)?.[1] ?? ""
      );
    });

    expect(onLinkAction).toHaveBeenCalledWith(
      expect.objectContaining({
        type: "open-workspace-file",
        path: "/workspace/room-1/archive.zip"
      })
    );
  });

  it("toasts when a composer-file mention is still uploading", () => {
    const hostToastInfo = vi.fn();
    const hostToastError = vi.fn();
    setAgentHostApiForTests({
      toast: { error: hostToastError, info: hostToastInfo }
    } as never);
    const onLinkAction = vi.fn();
    const draftFilesRef = {
      current: [
        {
          id: "file-1",
          name: "pending.pdf",
          path: "/runtime/pending.pdf",
          uploading: true
        }
      ]
    };
    const input = {
      ...createInput({
        draft: buildAgentComposerDraft({ prompt: "" }),
        prepareExternalPromptFiles: vi.fn(async () => [])
      }),
      draftFilesRef,
      onLinkAction
    };
    const rendered = renderHook(() => useComposerDraftAttachments(input));

    act(() => {
      rendered.result.current.handleLinkClick(
        createAgentComposerFileMentionMarkdown({
          id: "file-1",
          name: "pending.pdf",
          status: "uploading"
        }).match(/\(([^)]+)\)/)?.[1] ?? ""
      );
    });

    expect(onLinkAction).not.toHaveBeenCalled();
    expect(hostToastInfo).toHaveBeenCalledWith(
      "This attachment is still being prepared. Open it after preparation finishes."
    );
    expect(hostToastError).not.toHaveBeenCalled();
  });

  it("toasts when a composer-file mention failed preparation", () => {
    const hostToastError = vi.fn();
    setAgentHostApiForTests({ toast: { error: hostToastError } } as never);
    const onLinkAction = vi.fn();
    const draftFilesRef = {
      current: [
        {
          id: "file-1",
          name: "broken.pdf",
          uploadError: "file_too_large"
        }
      ]
    };
    const input = {
      ...createInput({
        draft: buildAgentComposerDraft({ prompt: "" }),
        prepareExternalPromptFiles: vi.fn(async () => [])
      }),
      draftFilesRef,
      onLinkAction
    };
    const rendered = renderHook(() => useComposerDraftAttachments(input));

    act(() => {
      rendered.result.current.handleLinkClick(
        createAgentComposerFileMentionMarkdown({
          id: "file-1",
          name: "broken.pdf",
          status: "error"
        }).match(/\(([^)]+)\)/)?.[1] ?? ""
      );
    });

    expect(onLinkAction).not.toHaveBeenCalled();
    expect(hostToastError).toHaveBeenCalledWith(
      "This attachment failed to prepare. Remove it and add the file again."
    );
  });
});

function createInput(input: {
  draft: ReturnType<typeof buildAgentComposerDraft>;
  editorHandle?: Partial<AgentRichTextEditorHandle>;
  onDraftContentChange?: (
    draft: AgentComposerDraft,
    sourceScopeKey?: string
  ) => void;
  onRequestWorkspaceReferences?: () => Promise<{
    files: readonly WorkspaceFileReference[];
    mentionItems: [];
  }>;
  prepareExternalPromptFiles: AgentExternalPromptFilePreparer;
}) {
  const draftByScopeKeyRef = { current: { home: input.draft } };
  const editorHandle = {
    focusAtStart: vi.fn(),
    focusAtEnd: vi.fn(),
    getPromptTextBeforeSelection: vi.fn(() => ""),
    openMentionPalette: vi.fn(),
    insertWorkspaceReferences: vi.fn(),
    insertMentionItems: vi.fn(),
    insertComposerFiles: vi.fn(),
    updateComposerFiles: vi.fn(() => false),
    replaceTextBeforeSelection: vi.fn(() => null),
    ...input.editorHandle
  } as AgentRichTextEditorHandle;
  return {
    workspaceId: "workspace-1",
    workspacePath: "/workspace",
    draftContent: input.draft,
    draftScopeKey: "home",
    draftByScopeKeyRef,
    goalDraftObjective: null,
    isGoalModeActive: false,
    promptImagesSupported: true,
    promptFilesSupported: true,
    promptAssetLimit: 16,
    pastedTextStagingSupported: false,
    editorHandleRef: { current: editorHandle },
    draftPromptRef: { current: "" },
    draftImagesRef: { current: [] },
    draftFilesRef: { current: [] },
    draftLargeTextsRef: { current: [] },
    setPaletteDraftPrompt: vi.fn(),
    setIsPaletteOpen: vi.fn(),
    clearActiveFileMentionTrigger: vi.fn(),
    onDraftContentChange: input.onDraftContentChange ?? vi.fn(),
    onRequestWorkspaceReferences: input.onRequestWorkspaceReferences,
    prepareExternalPromptFiles: input.prepareExternalPromptFiles
  };
}
