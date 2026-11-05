import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ConversationServices } from "../../../conversationServices";
import {
  dispatchAgentGuiWorkbenchCommand,
  type AgentGuiWorkbenchSessionAction
} from "../../../workbench/commands";
import { AGENT_CONVERSATION_COPY_MAX_EMBEDDED_IMAGE_BYTES } from "../model/agentConversationCopy";
import type {
  AgentGUIComposerViewModel,
  AgentGUIDetailViewModel,
  AgentGUIInteractionViewModel,
  AgentGUIOperationsViewModel,
  AgentGUIRailViewModel,
  AgentGUIReadinessViewModel,
  AgentGUIShellViewModel,
  AgentGUIViewFixture
} from "../model/agentGuiNodeTypes";
import { useAgentGUIExternalRequests } from "./useAgentGUIExternalRequests";
type AgentGUIViewFixture = {
  shell: AgentGUIShellViewModel;
  rail: AgentGUIRailViewModel;
  detail: AgentGUIDetailViewModel;
  composer: AgentGUIComposerViewModel;
  interaction: AgentGUIInteractionViewModel;
  readiness: AgentGUIReadinessViewModel;
  operations: AgentGUIOperationsViewModel;
};

type Conversation = AgentGUIRailViewModel["conversations"][number];

const LABELS = {
  conversationCopyFile: "File",
  conversationCopyImage: "Image",
  conversationCopyImagesOmitted:
    "{{count}} image(s) omitted — copy them individually",
  conversationCopyInProgress: "Copying conversation…",
  conversationCopyMentionPrefix: "@",
  conversationCopyPreviousMessages: "{{count}} previous messages",
  copiedToClipboard: "Copied",
  copyFailed: "Copy failed",
  sessionActionUnavailable: "Session action unavailable",
  untitledConversationTitle: "Untitled"
};

const originalAgentHostApi = window.agentHostApi;
const runtimeWindow = window as unknown as {
  agentGUIRuntime?: ConversationServices;
};
const originalConversationServices = runtimeWindow.agentGUIRuntime;

afterEach(() => {
  window.agentHostApi = originalAgentHostApi;
  runtimeWindow.agentGUIRuntime = originalConversationServices;
  vi.unstubAllGlobals();
  delete (window.navigator as { clipboard?: unknown }).clipboard;
});

function makeConversation(overrides: Partial<Record<string, unknown>> = {}) {
  return {
    agentTargetId: "target-1",
    cwd: "/workspace/project",
    id: "session-1",
    title: "Session 1",
    ...overrides
  } as unknown as Conversation;
}

function makeViewModel(input: {
  activeConversation?: Conversation | null;
  conversations?: Conversation[];
}) {
  return {
    rail: {
      activeConversation: input.activeConversation ?? null,
      conversations: input.conversations ?? []
    },
    shell: { workspaceId: "workspace-1" }
  } as unknown as AgentGUIViewFixture;
}

// Mirrors the desktop Toast.Loading contract: opens one toast and returns a
// handle whose resolve/info/reject settle that SAME toast in place, so tests
// can assert the loading toast and its eventual settlement are one thing.
function installHostApi() {
  const writeText = vi.fn().mockResolvedValue(undefined);
  const toastError = vi.fn();
  const toastInfo = vi.fn();
  const toastSuccess = vi.fn();
  const toastHandleInfo = vi.fn();
  const toastHandleReject = vi.fn();
  const toastHandleResolve = vi.fn();
  const toastLoading = vi.fn(() => ({
    info: toastHandleInfo,
    reject: toastHandleReject,
    resolve: toastHandleResolve
  }));
  window.agentHostApi = {
    ...(window.agentHostApi ?? {}),
    clipboard: { writeText },
    toast: {
      error: toastError,
      info: toastInfo,
      loading: toastLoading,
      success: toastSuccess
    }
  } as unknown as typeof window.agentHostApi;
  return {
    toastError,
    toastHandleInfo,
    toastHandleReject,
    toastHandleResolve,
    toastInfo,
    toastLoading,
    toastSuccess,
    writeText
  };
}

// For the "host without the loading capability" fallback path.
function installHostApiWithoutLoadingToast() {
  const { toastLoading: _toastLoading, ...rest } = installHostApi();
  const toast = (
    window.agentHostApi as unknown as { toast: Record<string, unknown> }
  ).toast;
  delete toast.loading;
  return rest;
}

function installRuntime(
  options: {
    messages?: unknown[];
    attachments?: {
      readSessionAttachment?: (input: {
        agentSessionId: string;
        attachmentId: string;
        workspaceId: string;
      }) => Promise<{ data: string; mimeType: string }>;
    };
    reject?: boolean;
  } = {}
) {
  const listSessionMessages = options.reject
    ? vi.fn().mockRejectedValue(new Error("load failed"))
    : vi.fn().mockResolvedValue({
        hasMore: false,
        latestVersion: 1,
        messages: options.messages ?? [
          {
            agentSessionId: "session-1",
            kind: "message",
            messageId: "message-1",
            occurredAtUnixMs: 1,
            payload: { content: "Full answer" },
            role: "assistant",
            sequence: 1,
            turnId: "turn-1",
            version: 1
          }
        ]
      });
  runtimeWindow.agentGUIRuntime = {
    conversations: {
      loadAll: async (input: unknown) =>
        (await listSessionMessages(input)).messages
    },
    ...(options.attachments ? { attachments: options.attachments } : {})
  } as unknown as ConversationServices;
  return { listSessionMessages };
}

class ClipboardItemStub {
  constructor(public readonly items: Record<string, Blob>) {}
}

// jsdom's Blob has no .text(), so specs read flavors through FileReader.
function readBlobText(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onerror = () => reject(reader.error ?? new Error("read failed"));
    reader.onload = () => resolve(String(reader.result ?? ""));
    reader.readAsText(blob);
  });
}

function installWebClipboard(options: { reject?: boolean } = {}) {
  const write = options.reject
    ? vi.fn().mockRejectedValue(new Error("write denied"))
    : vi.fn().mockResolvedValue(undefined);
  vi.stubGlobal("ClipboardItem", ClipboardItemStub);
  Object.defineProperty(window.navigator, "clipboard", {
    configurable: true,
    value: { write }
  });
  return { write };
}

function baseProps(
  overrides: Partial<Parameters<typeof useAgentGUIExternalRequests>[0]> = {}
): Parameters<typeof useAgentGUIExternalRequests>[0] {
  return {
    createConversationDisabled: true,
    labels: LABELS,
    requestCreateConversation: vi.fn(),
    requestRenameConversation: vi.fn(),
    uiLanguage: "en",
    ...makeViewModel({}),
    workbenchCommandBridge: { instanceId: "instance-1" },
    ...overrides
  };
}

function dispatchSessionAction(
  action: AgentGuiWorkbenchSessionAction,
  agentSessionId: string | null = null,
  instanceId = "instance-1"
): void {
  act(() => {
    dispatchAgentGuiWorkbenchCommand({
      type: "session-action",
      action,
      agentSessionId,
      instanceId
    });
  });
}

describe("useAgentGUIExternalRequests workbench commands", () => {
  it("routes commands only to the matching workbench instance", () => {
    const requestCreateConversation = vi.fn();
    renderHook(useAgentGUIExternalRequests, {
      initialProps: baseProps({
        createConversationDisabled: false,
        requestCreateConversation
      })
    });

    act(() => {
      dispatchAgentGuiWorkbenchCommand({
        instanceId: "other-instance",
        type: "new-conversation"
      });
      dispatchAgentGuiWorkbenchCommand({
        instanceId: "instance-1",
        type: "new-conversation"
      });
    });

    expect(requestCreateConversation).toHaveBeenCalledTimes(1);
    expect(requestCreateConversation).toHaveBeenCalledWith({
      source: "external_request"
    });
  });

  it("honors the current disabled state for new conversations", () => {
    const requestCreateConversation = vi.fn();
    const { rerender } = renderHook(useAgentGUIExternalRequests, {
      initialProps: baseProps({ requestCreateConversation })
    });

    act(() => {
      dispatchAgentGuiWorkbenchCommand({
        instanceId: "instance-1",
        type: "new-conversation"
      });
    });
    expect(requestCreateConversation).not.toHaveBeenCalled();

    rerender(
      baseProps({
        createConversationDisabled: false,
        requestCreateConversation
      })
    );
    act(() => {
      dispatchAgentGuiWorkbenchCommand({
        instanceId: "instance-1",
        type: "new-conversation"
      });
    });
    expect(requestCreateConversation).toHaveBeenCalledTimes(1);
  });

  it("delegates standalone rail persistence through the bridge callback", () => {
    const onConversationRailToggle = vi.fn();
    renderHook(useAgentGUIExternalRequests, {
      initialProps: baseProps({
        workbenchCommandBridge: {
          instanceId: "instance-1",
          onConversationRailToggle
        }
      })
    });

    act(() => {
      dispatchAgentGuiWorkbenchCommand({
        conversationRailCollapsed: true,
        instanceId: "instance-1",
        type: "conversation-rail-toggle"
      });
    });

    expect(onConversationRailToggle).toHaveBeenCalledWith(true);
  });
});

describe("useAgentGUIExternalRequests session actions", () => {
  it("executes rename against the conversation the request targets", () => {
    installHostApi();
    const active = makeConversation({ id: "session-1" });
    const other = makeConversation({ id: "session-2" });
    const requestRenameConversation = vi.fn();
    const props = baseProps({
      requestRenameConversation,
      ...makeViewModel({
        activeConversation: active,
        conversations: [active, other]
      })
    });
    renderHook(useAgentGUIExternalRequests, {
      initialProps: props
    });

    dispatchSessionAction("rename", "session-2");

    expect(requestRenameConversation).toHaveBeenCalledTimes(1);
    expect(requestRenameConversation).toHaveBeenCalledWith(other);
  });

  it("falls back to the active conversation when the request carries no session id", () => {
    installHostApi();
    const active = makeConversation({ id: "session-1" });
    const requestRenameConversation = vi.fn();
    const props = baseProps({
      requestRenameConversation,
      ...makeViewModel({
        activeConversation: active,
        conversations: [active]
      })
    });
    renderHook(useAgentGUIExternalRequests, {
      initialProps: props
    });

    dispatchSessionAction("rename");

    expect(requestRenameConversation).toHaveBeenCalledWith(active);
  });

  it("consumes each dispatched command, including repeated actions", () => {
    installHostApi();
    const active = makeConversation();
    const requestRenameConversation = vi.fn();
    const props = baseProps({
      requestRenameConversation,
      ...makeViewModel({
        activeConversation: active,
        conversations: [active]
      })
    });
    renderHook(useAgentGUIExternalRequests, {
      initialProps: props
    });

    dispatchSessionAction("rename");
    dispatchSessionAction("rename");
    expect(requestRenameConversation).toHaveBeenCalledTimes(2);
  });

  it("opens one loading toast immediately instead of a separate info toast", async () => {
    const { toastInfo, toastLoading } = installHostApi();
    installRuntime();
    const active = makeConversation({ id: "session-1" });
    const props = baseProps({
      ...makeViewModel({
        activeConversation: active,
        conversations: [active]
      })
    });
    renderHook(useAgentGUIExternalRequests, {
      initialProps: props
    });

    dispatchSessionAction("copy-markdown", "session-1");

    expect(toastLoading).toHaveBeenCalledWith(
      LABELS.conversationCopyInProgress
    );
    expect(toastInfo).not.toHaveBeenCalled();
  });

  it("falls back to the plain info toast when the host has no loading toast", async () => {
    const { toastInfo } = installHostApiWithoutLoadingToast();
    installRuntime();
    const active = makeConversation({ id: "session-1" });
    const props = baseProps({
      ...makeViewModel({
        activeConversation: active,
        conversations: [active]
      })
    });
    renderHook(useAgentGUIExternalRequests, {
      initialProps: props
    });

    dispatchSessionAction("copy-markdown", "session-1");

    expect(toastInfo).toHaveBeenCalledWith(LABELS.conversationCopyInProgress);
  });

  it("settles the loading toast to success instead of firing a separate toast", async () => {
    const { toastHandleResolve, toastSuccess, writeText } = installHostApi();
    installRuntime();
    const active = makeConversation({ id: "session-1" });
    const props = baseProps({
      ...makeViewModel({
        activeConversation: active,
        conversations: [active]
      })
    });
    renderHook(useAgentGUIExternalRequests, {
      initialProps: props
    });

    dispatchSessionAction("copy-markdown", "session-1");

    await waitFor(() => {
      expect(toastHandleResolve).toHaveBeenCalledWith(LABELS.copiedToClipboard);
    });
    expect(toastSuccess).not.toHaveBeenCalled();
    expect(writeText).toHaveBeenCalledWith("# Session 1\n\nFull answer");
  });

  it("writes text/plain markdown plus text/html with inline images when ClipboardItem is available", async () => {
    const { toastHandleResolve, toastSuccess, writeText } = installHostApi();
    const { write } = installWebClipboard();
    installRuntime({
      messages: [
        {
          agentSessionId: "session-1",
          kind: "message",
          messageId: "message-1",
          occurredAtUnixMs: 1,
          payload: {
            content: [
              { text: "Full answer", type: "text" },
              {
                data: "QUFB",
                mimeType: "image/png",
                name: "shot.png",
                type: "image"
              }
            ]
          },
          role: "assistant",
          sequence: 1,
          turnId: "turn-1",
          version: 1
        }
      ]
    });
    const active = makeConversation({ id: "session-1" });
    const props = baseProps({
      ...makeViewModel({
        activeConversation: active,
        conversations: [active]
      })
    });
    renderHook(useAgentGUIExternalRequests, {
      initialProps: props
    });

    dispatchSessionAction("copy-markdown", "session-1");

    await waitFor(() => {
      expect(toastHandleResolve).toHaveBeenCalledWith(LABELS.copiedToClipboard);
    });
    expect(toastSuccess).not.toHaveBeenCalled();
    expect(writeText).not.toHaveBeenCalled();
    expect(write).toHaveBeenCalledTimes(1);
    const [items] = write.mock.calls[0] as [ClipboardItemStub[]];
    expect(items).toHaveLength(1);
    const flavors = items[0]!.items;
    // The plain flavor stays lean (no base64); only the HTML flavor embeds
    // the image bytes.
    expect(await readBlobText(flavors["text/plain"]!)).toBe(
      "# Session 1\n\nFull answer\n\n**shot.png**"
    );
    const html = await readBlobText(flavors["text/html"]!);
    expect(html).toContain(
      '<img src="data:image/png;base64,QUFB" alt="shot.png"'
    );
    expect(html).toContain("Full answer");
  });

  it("keeps attachment references in text/plain while text/html embeds the bytes", async () => {
    const { toastHandleResolve, toastSuccess, writeText } = installHostApi();
    const { write } = installWebClipboard();
    const readSessionAttachment = vi.fn().mockResolvedValue({
      data: "QkJC",
      mimeType: "image/png"
    });
    installRuntime({
      attachments: { readSessionAttachment },
      messages: [
        {
          agentSessionId: "session-1",
          kind: "tool_call",
          messageId: "tool-1",
          occurredAtUnixMs: 1,
          payload: {
            toolName: "ImageGeneration",
            status: "completed",
            output: [
              {
                type: "image",
                attachmentId: "attachment-9",
                mimeType: "image/png"
              }
            ]
          },
          role: "assistant",
          sequence: 1,
          turnId: "turn-1",
          version: 1
        }
      ]
    });
    const active = makeConversation({ id: "session-1" });
    const props = baseProps({
      ...makeViewModel({
        activeConversation: active,
        conversations: [active]
      })
    });
    renderHook(useAgentGUIExternalRequests, {
      initialProps: props
    });

    dispatchSessionAction("copy-markdown", "session-1");

    await waitFor(() => {
      expect(toastHandleResolve).toHaveBeenCalledWith(LABELS.copiedToClipboard);
    });
    expect(toastSuccess).not.toHaveBeenCalled();
    expect(readSessionAttachment).toHaveBeenCalledWith({
      agentSessionId: "session-1",
      attachmentId: "attachment-9",
      workspaceId: "workspace-1"
    });
    expect(writeText).not.toHaveBeenCalled();
    const [items] = write.mock.calls[0] as [ClipboardItemStub[]];
    const text = await readBlobText(items[0]!.items["text/plain"]!);
    expect(text).toContain("![Image](<attachment:attachment-9>)");
    expect(text).not.toContain("data:");
    const html = await readBlobText(items[0]!.items["text/html"]!);
    expect(html).toContain('<img src="data:image/png;base64,QkJC"');
  });

  it("reports omitted oversized images through the info toast instead of the plain copied toast", async () => {
    const { toastHandleInfo, toastInfo, toastSuccess, writeText } =
      installHostApi();
    // Smallest unpadded base64 payload above the 2 MiB per-image embed cap.
    const oversized = "A".repeat(
      4 * Math.ceil((AGENT_CONVERSATION_COPY_MAX_EMBEDDED_IMAGE_BYTES + 1) / 3)
    );
    installRuntime({
      attachments: {
        readSessionAttachment: vi.fn().mockResolvedValue({
          data: oversized,
          mimeType: "image/png"
        })
      },
      messages: [
        {
          agentSessionId: "session-1",
          kind: "tool_call",
          messageId: "tool-1",
          occurredAtUnixMs: 1,
          payload: {
            toolName: "ImageGeneration",
            status: "completed",
            output: [
              {
                type: "image",
                attachmentId: "attachment-9",
                mimeType: "image/png"
              }
            ]
          },
          role: "assistant",
          sequence: 1,
          turnId: "turn-1",
          version: 1
        }
      ]
    });
    const active = makeConversation({ id: "session-1" });
    const props = baseProps({
      ...makeViewModel({
        activeConversation: active,
        conversations: [active]
      })
    });
    renderHook(useAgentGUIExternalRequests, {
      initialProps: props
    });

    dispatchSessionAction("copy-markdown", "session-1");

    await waitFor(() => {
      expect(toastHandleInfo).toHaveBeenCalledWith(
        "1 image(s) omitted — copy them individually"
      );
    });
    expect(toastInfo).not.toHaveBeenCalled();
    expect(toastSuccess).not.toHaveBeenCalled();
    expect(writeText).toHaveBeenCalledWith(
      "# Session 1\n\n![Image](<attachment:attachment-9>)"
    );
  });

  it("falls back to the host text clipboard when the dual-format write is denied", async () => {
    const { toastError, toastHandleResolve, toastSuccess, writeText } =
      installHostApi();
    installWebClipboard({ reject: true });
    installRuntime();
    const active = makeConversation({ id: "session-1" });
    const props = baseProps({
      ...makeViewModel({
        activeConversation: active,
        conversations: [active]
      })
    });
    renderHook(useAgentGUIExternalRequests, {
      initialProps: props
    });

    dispatchSessionAction("copy-markdown", "session-1");

    await waitFor(() => {
      expect(toastHandleResolve).toHaveBeenCalledWith(LABELS.copiedToClipboard);
    });
    expect(toastSuccess).not.toHaveBeenCalled();
    expect(writeText).toHaveBeenCalledWith("# Session 1\n\nFull answer");
    expect(toastError).not.toHaveBeenCalled();
  });

  it("copies the session mention link for the quote variant without loading history", async () => {
    const { toastSuccess, writeText } = installHostApi();
    const { listSessionMessages } = installRuntime();
    const active = makeConversation({ id: "session-1" });
    const props = baseProps({
      ...makeViewModel({
        activeConversation: active,
        conversations: [active]
      })
    });
    renderHook(useAgentGUIExternalRequests, {
      initialProps: props
    });

    dispatchSessionAction("copy-reference", "session-1");

    await waitFor(() => {
      expect(toastSuccess).toHaveBeenCalledWith(LABELS.copiedToClipboard);
    });
    expect(writeText).toHaveBeenCalledWith(
      "[@Session 1](mention://agent-session/session-1?agentTargetId=target-1&workspaceId=workspace-1)"
    );
    expect(listSessionMessages).not.toHaveBeenCalled();
  });

  it("reports copy failure instead of writing when history loading fails", async () => {
    const { toastError, toastHandleReject, writeText } = installHostApi();
    installRuntime({ reject: true });
    const active = makeConversation();
    const props = baseProps({
      ...makeViewModel({
        activeConversation: active,
        conversations: [active]
      })
    });
    renderHook(useAgentGUIExternalRequests, {
      initialProps: props
    });

    dispatchSessionAction("copy-markdown");

    await waitFor(() => {
      expect(toastHandleReject).toHaveBeenCalledWith(LABELS.copyFailed);
    });
    expect(toastError).not.toHaveBeenCalled();
    expect(writeText).not.toHaveBeenCalled();
  });

  it("reports copy failure when the host clipboard cannot write", async () => {
    const toastError = vi.fn();
    window.agentHostApi = {
      ...(window.agentHostApi ?? {}),
      clipboard: {},
      toast: { error: toastError, success: vi.fn() }
    } as unknown as typeof window.agentHostApi;
    installRuntime();
    const active = makeConversation();
    const props = baseProps({
      ...makeViewModel({
        activeConversation: active,
        conversations: [active]
      })
    });
    renderHook(useAgentGUIExternalRequests, {
      initialProps: props
    });

    dispatchSessionAction("copy-markdown");

    await waitFor(() => {
      expect(toastError).toHaveBeenCalledWith(LABELS.copyFailed);
    });
  });
});
