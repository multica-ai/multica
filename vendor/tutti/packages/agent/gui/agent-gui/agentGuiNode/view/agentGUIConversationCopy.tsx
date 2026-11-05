import { resolveWorkspaceImageMimeType } from "@tutti-os/workspace-file-preview";
import { useCallback, type ReactNode } from "react";
import { useOptionalAgentHostApi } from "../../../agentActivityHost";
import type { UiLanguage } from "../../../contexts/settings/domain/agentSettings";
import { useOptionalConversationServices } from "../../../conversationServices";
import type {
  AgentHostToastHandle,
  AgentHostWorkspaceApi
} from "../../../host/agentHostApi";
import { blobToBase64 } from "../../../shared/agentConversation/lib/copyImageToClipboard";
import { createAgentSessionMarkdownLink } from "../agentRichText/agentFileMentionExtension";
import {
  loadCompleteAgentConversationMessages,
  serializeAgentConversationForClipboard,
  type AgentGUIConversationAttachment,
  type AgentGUIConversationCopyAction
} from "../model/agentConversationCopy";
import { renderAgentConversationCopyHtml } from "../model/agentConversationCopyHtml";
import type { AgentGUIRailViewModel } from "../model/agentGuiNodeTypes";
import type { AgentGUIViewLabels } from "./AgentGUINodeView.types";
import { conversationPlainTitle } from "./agentGUIViewUtils";

const menuContentClassName =
  "w-max min-w-44 nodrag [-webkit-app-region:no-drag]";

type Conversation = AgentGUIRailViewModel["conversations"][number];

export interface AgentGUIClipboardPayload {
  html?: string;
  /** Overrides the default copied toast, e.g. omitted-image guidance. */
  successMessage?: string;
  text: string;
}

// Writes text/plain + text/html as one ClipboardItem so rich-paste targets
// (Word, Feishu docs, Notion, mail clients) receive the HTML flavor with real
// inline images. Returns false instead of throwing when the dual-format path
// is unavailable or denied, so the caller falls back to the host text-only
// clipboard and copying keeps working.
async function writeTextAndHtmlWithWebClipboard(
  text: string,
  html: string
): Promise<boolean> {
  if (
    typeof navigator === "undefined" ||
    typeof navigator.clipboard?.write !== "function" ||
    typeof ClipboardItem === "undefined"
  ) {
    return false;
  }
  try {
    await navigator.clipboard.write([
      new ClipboardItem({
        "text/html": new Blob([html], { type: "text/html" }),
        "text/plain": new Blob([text], { type: "text/plain" })
      })
    ]);
    return true;
  } catch {
    return false;
  }
}

// Hydrates a local workspace image path into base64 bytes for the transcript
// serializer, mirroring AgentGeneratedImagePreview's read and MIME resolution
// so the copied text/html flavor embeds the same bytes the GUI preview
// renders.
async function readWorkspaceImageAsAttachment(
  readFile: AgentHostWorkspaceApi["readFile"],
  input: { mimeType: string | null; path: string }
): Promise<AgentGUIConversationAttachment> {
  const result = await readFile({ path: input.path });
  const bytes =
    result.bytes instanceof Uint8Array
      ? result.bytes
      : new Uint8Array(result.bytes);
  const arrayBuffer = bytes.buffer.slice(
    bytes.byteOffset,
    bytes.byteOffset + bytes.byteLength
  ) as ArrayBuffer;
  const mimeType =
    input.mimeType?.trim() ||
    resolveWorkspaceImageMimeType(input.path) ||
    "image/png";
  return {
    data: await blobToBase64(new Blob([arrayBuffer], { type: mimeType })),
    mimeType
  };
}

export function useAgentGUIClipboardWriter(
  labels: Pick<AgentGUIViewLabels, "copiedToClipboard" | "copyFailed">
): (
  value: string | AgentGUIClipboardPayload,
  toastHandle?: AgentHostToastHandle
) => void {
  const agentHostApi = useOptionalAgentHostApi();
  return useCallback(
    (value, toastHandle) => {
      const payload = typeof value === "string" ? { text: value } : value;
      const clipboard = agentHostApi?.clipboard;
      if (typeof clipboard?.writeText !== "function") {
        (toastHandle?.reject ?? agentHostApi?.toast?.error)?.(
          labels.copyFailed
        );
        return;
      }
      // The write runs inside then() so a synchronous throw still lands in
      // catch instead of escaping the deferred menu-action callback.
      void Promise.resolve()
        .then(async () => {
          if (
            payload.html &&
            (await writeTextAndHtmlWithWebClipboard(payload.text, payload.html))
          ) {
            return;
          }
          await clipboard.writeText(payload.text);
        })
        .then(() => {
          if (payload.successMessage) {
            // Guidance-style success (e.g. omitted oversized images) settles
            // to the neutral/info tone instead of a plain success.
            (
              toastHandle?.info ??
              agentHostApi?.toast?.info ??
              toastHandle?.resolve ??
              agentHostApi?.toast?.success
            )?.(payload.successMessage);
          } else {
            (toastHandle?.resolve ?? agentHostApi?.toast?.success)?.(
              labels.copiedToClipboard
            );
          }
        })
        .catch(() => {
          (toastHandle?.reject ?? agentHostApi?.toast?.error)?.(
            labels.copyFailed
          );
        });
    },
    [agentHostApi, labels.copiedToClipboard, labels.copyFailed]
  );
}

export function useAgentGUIConversationCopyAction(
  labels: Pick<
    AgentGUIViewLabels,
    | "conversationCopyFile"
    | "conversationCopyImage"
    | "conversationCopyImagesOmitted"
    | "conversationCopyInProgress"
    | "conversationCopyMentionPrefix"
    | "conversationCopyPreviousMessages"
    | "copiedToClipboard"
    | "copyFailed"
    | "untitledConversationTitle"
  >
): (
  action: AgentGUIConversationCopyAction,
  input: {
    conversation: Conversation;
    uiLanguage: UiLanguage;
    workspaceId: string;
  }
) => void {
  const agentHostApi = useOptionalAgentHostApi();
  const agentActivityRuntime = useOptionalConversationServices();
  const writeClipboardValue = useAgentGUIClipboardWriter(labels);
  return useCallback(
    (action, { conversation, uiLanguage, workspaceId }) => {
      if (action === "copy-reference") {
        // Same serialized form as an @-panel session mention, so pasting into
        // any Tutti composer reconstructs the session chip.
        writeClipboardValue(
          createAgentSessionMarkdownLink({
            agentSessionId: conversation.id,
            agentTargetId: conversation.agentTargetId,
            label: conversationPlainTitle(conversation, labels, uiLanguage),
            workspaceId,
            withAtPrefix: true
          })
        );
        return;
      }
      if (!agentActivityRuntime) {
        agentHostApi?.toast?.error(labels.copyFailed);
        return;
      }
      const readSessionAttachment =
        agentActivityRuntime.attachments?.readSessionAttachment;
      const readWorkspaceFile = agentHostApi?.workspace?.readFile;
      // History load + image hydration can take a noticeable moment on long
      // conversations. One toast opens busy immediately and settles in
      // place once the copy lands, instead of a separate toast per phase.
      // Hosts without the loading capability get the plain info toast they
      // always had, and the eventual result falls back to a second toast.
      const toastHandle = agentHostApi?.toast?.loading?.(
        labels.conversationCopyInProgress
      );
      if (!toastHandle) {
        agentHostApi?.toast?.info?.(labels.conversationCopyInProgress);
      }
      void (async () => {
        const messages = await loadCompleteAgentConversationMessages({
          agentSessionId: conversation.id,
          runtime: agentActivityRuntime,
          workspaceId
        });
        const transcript = await serializeAgentConversationForClipboard({
          labels: {
            file: labels.conversationCopyFile,
            image: labels.conversationCopyImage,
            mentionPrefix: labels.conversationCopyMentionPrefix,
            previousMessages: labels.conversationCopyPreviousMessages
          },
          messages,
          ...(readSessionAttachment
            ? {
                readAttachment: (attachmentId: string) =>
                  readSessionAttachment({
                    agentSessionId: conversation.id,
                    attachmentId,
                    workspaceId
                  })
              }
            : {}),
          ...(typeof readWorkspaceFile === "function"
            ? {
                readLocalImage: (target: {
                  mimeType: string | null;
                  path: string;
                }) => readWorkspaceImageAsAttachment(readWorkspaceFile, target)
              }
            : {}),
          title: conversationPlainTitle(conversation, labels, uiLanguage)
        });
        let html: string | undefined;
        try {
          html =
            renderAgentConversationCopyHtml(transcript.hydratedMarkdown) ||
            undefined;
        } catch {
          // A rendering failure must degrade to the text-only copy instead of
          // failing the whole action.
          html = undefined;
        }
        // Oversized/unreadable images stay out of the copy; the success toast
        // tells the user how many and points at per-image copy instead.
        const successMessage =
          transcript.omittedImages > 0
            ? labels.conversationCopyImagesOmitted.replace(
                "{{count}}",
                `${transcript.omittedImages}`
              )
            : undefined;
        writeClipboardValue(
          {
            ...(html ? { html } : {}),
            ...(successMessage ? { successMessage } : {}),
            text: transcript.markdown
          },
          toastHandle
        );
      })().catch(() => {
        (toastHandle?.reject ?? agentHostApi?.toast?.error)?.(
          labels.copyFailed
        );
      });
    },
    [agentActivityRuntime, agentHostApi, labels, writeClipboardValue]
  );
}

type ConversationActionEntry = {
  disabled?: boolean;
  icon: ReactNode;
  id: string;
  label: string;
  onSelect: () => void;
};
