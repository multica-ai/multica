import {
  act,
  fireEvent,
  render,
  renderHook,
  screen,
  waitFor
} from "@testing-library/react";
import { createTestRuntimeState } from "../../../shared/testing/createTestRuntimeState";

import type { AgentQueuedPrompt } from "@tutti-os/agent-activity-core";
import { RichTextMentionServiceProvider } from "@tutti-os/ui-rich-text/editor";
import { createRichTextMentionService } from "@tutti-os/ui-rich-text/service";
import type { RichTextTriggerProvider } from "@tutti-os/ui-rich-text/types";
import { useRef, useState } from "react";
import { describe, expect, it, vi } from "vitest";
import type { ConversationServices } from "../../../conversationServices";
import { AgentQueuedPromptPanelView as AgentQueuedPromptPanel } from "../AgentQueuedPromptPanel";
import { AgentRichTextEditor } from "../agentRichText/AgentRichTextEditor";
import {
  agentComposerDraftImages,
  agentComposerDraftPrompt,
  agentComposerDraftToPromptContent,
  updateAgentComposerDraft
} from "../model/agentComposerDraft";
import type { AgentComposerDraft } from "../model/agentGuiNodeTypes";
import { useAgentGUIQueueActions } from "./useAgentGUIQueueActions";

describe("useAgentGUIQueueActions", () => {
  it("rehydrates a path-backed queued image when editing it", async () => {
    let resolveRead:
      | ((asset: { data: string; mimeType: string }) => void)
      | null = null;
    const readPromptAsset = vi.fn(
      () =>
        new Promise<{ data: string; mimeType: string }>((resolve) => {
          resolveRead = resolve;
        })
    );
    const conversationServices = createQueueTestEngine({
      id: "queued-1",
      content: [
        {
          type: "image",
          mimeType: "image/png",
          name: "image.png",
          path: "/agent-prompt-assets/image.png"
        }
      ],
      createdAtUnixMs: 1
    });

    const rendered = renderHook(() => {
      const [drafts, setDrafts] = useState<Record<string, AgentComposerDraft>>(
        {}
      );
      return {
        drafts,
        actions: useAgentGUIQueueActions({
          activeConversationIdRef: { current: "session-1" },
          agentActivityRuntime: {
            attachments: {
              readPromptAsset
            }
          } as unknown as ConversationServices,
          conversationServices,
          setDraftByScopeKey: setDrafts,
          workspaceId: "workspace-1"
        })
      };
    });

    act(() => rendered.result.current.actions.editQueuedPrompt("queued-1"));

    expect(
      agentComposerDraftImages(
        rendered.result.current.drafts["session:session-1"]!
      )[0]
    ).toMatchObject({
      path: "/agent-prompt-assets/image.png",
      previewUrl: ""
    });
    expect(readPromptAsset).toHaveBeenCalledWith({
      workspaceId: "workspace-1",
      agentSessionId: "session-1",
      mimeType: "image/png",
      name: "image.png",
      path: "/agent-prompt-assets/image.png"
    });
    expect(
      conversationServices.prompts.queuedPrompt("session-1", "queued-1")
    ).toBeNull();

    await act(async () => {
      resolveRead?.({ data: "cmVzdG9yZWQ=", mimeType: "image/png" });
      await Promise.resolve();
    });
    await waitFor(() =>
      expect(
        agentComposerDraftImages(
          rendered.result.current.drafts["session:session-1"]!
        )[0]?.previewUrl
      ).toBe("data:image/png;base64,cmVzdG9yZWQ=")
    );
  });

  it("hydrates mention icons after a queued prompt is edited into the Composer", async () => {
    const prompt = [
      "Ask",
      "[@Old Agent](mention://agent-target/shared-agent:agent-1?workspaceId=workspace-1)",
      "then",
      "[@Old App](mention://workspace-app/app-1?workspaceId=workspace-1)",
      "and",
      "[@Old Issue](mention://workspace-issue/issue-1?workspaceId=workspace-1)"
    ].join(" ");
    const resolvedPresentations: Record<
      string,
      {
        entityId: string;
        iconUrl: string;
        label: string;
        presentation?: { agentProviderId: string };
      }
    > = {
      "agent-target": {
        entityId: "shared-agent:agent-1",
        iconUrl: "https://icons.example/shared-agent.png",
        label: "Lin · Review Agent",
        presentation: { agentProviderId: "codex" }
      },
      "workspace-app": {
        entityId: "app-1",
        iconUrl: "https://icons.example/group-chat.png",
        label: "Project Group"
      },
      "workspace-issue": {
        entityId: "issue-1",
        iconUrl: "https://icons.example/task-center.png",
        label: "Task Center"
      }
    };
    const providers = Object.entries(resolvedPresentations).map(
      ([providerId, resolved]): RichTextTriggerProvider<string> => ({
        id: providerId,
        trigger: "@",
        query: () => [],
        getItemKey: (item) => item,
        getItemLabel: (item) => item,
        toInsertResult: (item) => ({ kind: "text", text: item }),
        resolveMention: async (identity) =>
          identity.entityId === resolved.entityId
            ? {
                label: resolved.label,
                presentation: {
                  iconUrl: resolved.iconUrl,
                  ...(resolved.presentation ?? {})
                }
              }
            : null
      })
    );
    const mentionService = createRichTextMentionService({ providers });
    const conversationServices = createQueueTestEngine({
      id: "queued-mentions",
      content: [{ type: "text", text: prompt }],
      createdAtUnixMs: 1
    });
    const onEditorChange = vi.fn();
    const rendered = render(
      <RichTextMentionServiceProvider service={mentionService}>
        <QueuedMentionEditHarness
          onEditorChange={onEditorChange}
          prompt={prompt}
          conversationServices={conversationServices}
        />
      </RichTextMentionServiceProvider>
    );

    const moreButton = screen.getByRole("button", { name: "More" });
    fireEvent.pointerDown(moreButton, { button: 0, ctrlKey: false });
    fireEvent.click(moreButton);
    const editItem = await screen.findByRole("menuitem", { name: "Edit" });
    fireEvent.pointerDown(editItem, { button: 0, ctrlKey: false });
    fireEvent.click(editItem);

    for (const [kind, presentation] of Object.entries(resolvedPresentations)) {
      const mentionImage = await waitFor(() => {
        const image = rendered.container.querySelector<HTMLImageElement>(
          `[data-agent-mention-kind="${kind}"] img`
        );
        expect(image).not.toBeNull();
        return image!;
      });
      expect(mentionImage).toHaveAttribute("src", presentation.iconUrl);
      expect(
        rendered.container.querySelector(`[data-agent-mention-kind="${kind}"]`)
      ).toHaveTextContent(presentation.label);
      const hrefElement = rendered.container.querySelector<HTMLElement>(
        `[data-agent-mention-href^="mention://${kind}/"]`
      );
      expect(hrefElement?.dataset.agentMentionHref).not.toContain("icon");
      expect(hrefElement?.dataset.agentMentionHref).not.toContain("data:");
    }

    expect(screen.getByTestId("restored-draft-prompt")).toHaveTextContent(
      prompt
    );
    expect(screen.getByTestId("provider-prompt")).toHaveTextContent(prompt);
    expect(screen.getByTestId("provider-prompt").textContent).not.toContain(
      "icons.example"
    );
    expect(onEditorChange).not.toHaveBeenCalled();
    expect(
      conversationServices.prompts.queuedPrompt("session-1", "queued-mentions")
    ).toBeNull();

    rendered.unmount();
    mentionService.dispose();
  });
});

const queueLabels = {
  queuedLabel: "Queued",
  queuePausedByUserLabel: "Queue paused",
  sendQueuedPromptNext: "Send next",
  editQueuedPrompt: "Edit",
  deleteQueuedPrompt: "Delete",
  queuedPromptMoreActions: "More"
};

/** An engine as the prompt service presents it: holding one queued prompt until it is removed. */
function createQueueTestEngine(prompt: AgentQueuedPrompt) {
  const state = createTestRuntimeState("workspace-1");
  state.seedQueue("session-1", [prompt]);
  return state;
}

function QueuedMentionEditHarness({
  onEditorChange,
  prompt,
  conversationServices
}: {
  onEditorChange: (value: string) => void;
  prompt: string;
  conversationServices: ReturnType<typeof createQueueTestEngine>;
}): React.JSX.Element {
  const draftScopeKey = "session:session-1";
  const activeConversationIdRef = useRef<string | null>("session-1");
  const [drafts, setDrafts] = useState<Record<string, AgentComposerDraft>>({});
  const actions = useAgentGUIQueueActions({
    activeConversationIdRef,
    agentActivityRuntime: {} as ConversationServices,
    conversationServices,
    setDraftByScopeKey: setDrafts,
    workspaceId: "workspace-1"
  });
  const draft = drafts[draftScopeKey];
  const updatePrompt = (nextPrompt: string): void => {
    onEditorChange(nextPrompt);
    setDrafts((current) => {
      const currentDraft = current[draftScopeKey];
      return currentDraft
        ? {
            ...current,
            [draftScopeKey]: updateAgentComposerDraft(currentDraft, {
              prompt: nextPrompt
            })
          }
        : current;
    });
  };
  const providerPrompt = draft
    ? (agentComposerDraftToPromptContent({ draft, skills: [] }).find(
        (block) => block.type === "text"
      )?.text ?? "")
    : "";

  return (
    <>
      <AgentQueuedPromptPanel
        queuedPrompts={[
          {
            id: "queued-mentions",
            content: [{ type: "text", text: prompt }],
            createdAtUnixMs: 1
          }
        ]}
        drainingQueuedPromptId={null}
        labels={queueLabels}
        onEditQueuedPrompt={actions.editQueuedPrompt}
        onRemoveQueuedPrompt={actions.removeQueuedPrompt}
        onSendQueuedPromptNext={actions.sendQueuedPromptNext}
      />
      <AgentRichTextEditor
        contentScopeKey={draftScopeKey}
        disabled={false}
        onChange={updatePrompt}
        onSubmit={vi.fn()}
        placeholder="Prompt"
        value={draft ? agentComposerDraftPrompt(draft) : ""}
      />
      <output data-testid="restored-draft-prompt">
        {draft ? agentComposerDraftPrompt(draft) : ""}
      </output>
      <output data-testid="provider-prompt">{providerPrompt}</output>
    </>
  );
}
