import { act, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { ConversationServices } from "../../../conversationServices";
import {
  agentComposerDraftFiles,
  agentComposerDraftImages,
  agentComposerDraftLargeTexts,
  agentComposerDraftPrompt,
  buildAgentComposerDraft
} from "../model/agentComposerDraft";
import {
  buildContinueInNewConversationDraft,
  useAgentGUIContinueConversation
} from "./useAgentGUIContinueConversation";

describe("buildContinueInNewConversationDraft", () => {
  it("preserves every attachment block while replacing the continuation prompt", () => {
    const sourceDraft = buildAgentComposerDraft({
      prompt: "unsent follow-up",
      images: [
        {
          id: "image-1",
          name: "screen.png",
          mimeType: "image/png",
          previewUrl: "blob:image-1",
          uploading: true
        }
      ],
      files: [{ id: "file-1", name: "notes.md", path: "/workspace/notes.md" }],
      largeTexts: [
        {
          id: "paste-1",
          name: "pasted-text.txt",
          text: "pasted body",
          path: "/archive/paste-1.txt"
        }
      ]
    });

    const continued = buildContinueInNewConversationDraft({
      sourceDraft,
      prompt: "continue mention\nunsent follow-up"
    });

    expect(agentComposerDraftPrompt(continued)).toBe(
      "continue mention\nunsent follow-up"
    );
    expect(agentComposerDraftImages(continued)).toEqual(
      agentComposerDraftImages(sourceDraft)
    );
    expect(agentComposerDraftFiles(continued)).toEqual(
      agentComposerDraftFiles(sourceDraft)
    );
    expect(agentComposerDraftLargeTexts(continued)).toEqual(
      agentComposerDraftLargeTexts(sourceDraft)
    );
  });

  it("restores the canonical source project before moving the continuation draft to Home", () => {
    const selectedProjectPathRef = { current: null as string | null };
    const setSelectedProjectPath = vi.fn();
    const activeConversationIdRef = { current: "session-1" as string | null };
    const isComposerHomeRef = { current: false };
    const activeConversation = {
      id: "session-1",
      provider: "codex" as const,
      title: "Source",
      status: "ready" as const,
      cwd: "/state/task-worktrees/issue/task-run",
      railSectionKey: "project:/workspace/project-a",
      updatedAtUnixMs: 1
    };
    const { result } = renderHook(() =>
      useAgentGUIContinueConversation({
        accountProfilesByUserId: {},
        activeConversationIdRef,
        activePendingActivation: null,
        agentActivityRuntime: {} as ConversationServices,
        conversations: [activeConversation],
        currentUserId: "user-1",
        draftByScopeKey: {},
        isComposerHomeRef,
        loadDraftComposerOptions: vi.fn(),
        persistActiveConversation: vi.fn(),
        selectedProjectPathRef,
        setActiveConversationId: vi.fn(),
        setDetailError: vi.fn(),
        setDraftByScopeKey: vi.fn(),
        setIntent: vi.fn(),
        setIsComposerHome: vi.fn(),
        setIsLoadingMessages: vi.fn(),
        setSelectedProjectPath,
        transientConversation: null,
        unactivate: vi.fn(async () => undefined),
        userProjectsRef: {
          current: [
            {
              id: "project-a",
              label: "Project A",
              path: "/workspace/project-a",
              pinnedAtUnixMs: 0,
              sectionKey: "project:/workspace/project-a"
            }
          ]
        },
        workspaceId: "workspace-1"
      })
    );

    act(() => result.current());

    expect(selectedProjectPathRef.current).toBe("/workspace/project-a");
    expect(setSelectedProjectPath).toHaveBeenCalledWith("/workspace/project-a");
    expect(activeConversationIdRef.current).toBeNull();
    expect(isComposerHomeRef.current).toBe(true);
  });
});
