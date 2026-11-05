import type {
AgentActivityRuntimePromptContentBlock,
ConversationServices
} from "@tutti-os/agent-gui";
import type { AgentRichTextPastedImage } from "../../../vendor/tutti/packages/agent/gui/agent-gui/agentGuiNode/agentRichText/AgentRichTextEditor.types";
import type { AgentComposerDraftImage } from "../../../vendor/tutti/packages/agent/gui/agent-gui/agentGuiNode/model/agentGuiNodeTypes";
import type {
AgentMessageImageVM,
AgentMessageRowVM
} from "../../../vendor/tutti/packages/agent/gui/shared/agentConversation/contracts/agentMessageRowVM";

interface ImageReference {
  workspaceId: string;
  agentSessionId: string;
  attachmentId: string;
}
interface ReferencedImage extends AgentRichTextPastedImage {
  reference?: ImageReference;
}
type PreparedImage = AgentActivityRuntimePromptContentBlock & {
  previewUrl?: string;
};

function supportedImageType(
  value: string
): value is AgentRichTextPastedImage["mimeType"] {
  return (
    value === "image/png" || value === "image/jpeg" || value === "image/webp"
  );
}

function imageReferenceAddress(image: AgentMessageImageVM): string | null {
  if (
    !image.workspaceId ||
    !image.attachmentId ||
    !supportedImageType(image.mimeType)
  )
    return null;
  const query = new URLSearchParams({
    session: image.agentSessionId,
    name: image.name || "image",
    mime: image.mimeType
  });
  return `multica-image://attachment/${encodeURIComponent(image.workspaceId)}/${encodeURIComponent(image.attachmentId)}?${query}`;
}

export function projectMessageImageCopies(
  row: AgentMessageRowVM
): AgentMessageRowVM {
  if (row.speaker !== "user") return row;
  const images = row.messages.flatMap((message) => message.images ?? []);
  if (!images.length) return row;
  const addresses = images.map(imageReferenceAddress);
  // Only the host's durable attachment locators can be pasted as image references.
  if (addresses.some((address) => !address)) return row;
  const textMessages = row.messages.filter((message) => message.copyText);
  const text = textMessages.map((message) => message.copyText).join("\n\n");
  const target =
    textMessages.at(-1) ??
    row.messages.find((message) => message.images?.length);
  const copyText = (text ? `${text}\n\n` : "") + addresses.join("\n");
  return {
    ...row,
    messages: row.messages.map((message) => ({
      ...message,
      copyText: message === target ? copyText : null
    }))
  };
}

function parseImageAddress(value: string): ReferencedImage | null {
  if (!value.startsWith("multica-image://attachment/")) return null;
  try {
    const url = new URL(value);
    const parts = url.pathname.slice(1).split("/").map(decodeURIComponent);
    const [workspaceId, attachmentId] = parts;
    const agentSessionId = url.searchParams.get("session");
    const mimeType = url.searchParams.get("mime") ?? "";
    if (
      parts.length !== 2 ||
      !workspaceId ||
      !attachmentId ||
      !agentSessionId ||
      !supportedImageType(mimeType)
    )
      return null;
    return {
      name: url.searchParams.get("name") || "image",
      mimeType,
      data: "",
      reference: { workspaceId, attachmentId, agentSessionId }
    };
  } catch {
    return null;
  }
}

export function parseMessageImagePaste(
  value: string
): { text: string; images: ReferencedImage[] } | null {
  const boundary = value.lastIndexOf("\n\n");
  const suffix = boundary < 0 ? value : value.slice(boundary + 2);
  const images = suffix.split("\n").map(parseImageAddress);
  if (!images.length || images.some((image) => !image)) return null;
  return {
    text: boundary < 0 ? "" : value.slice(0, boundary),
    images: images.filter((image): image is ReferencedImage => image !== null)
  };
}

export function pasteMessageImageReferences(input: {
  text: string;
  supported: boolean;
  onImages?: (images: AgentRichTextPastedImage[]) => void;
  onUnsupported?: () => void;
  insertText: (text: string) => void;
}): boolean {
  const paste = parseMessageImagePaste(input.text);
  if (!paste || !input.onImages) return false;
  if (!input.supported) {
    input.onUnsupported?.();
    input.insertText(input.text);
    return true;
  }
  // Attachment preparation publishes a draft immediately, so commit text first.
  if (paste.text) input.insertText(paste.text);
  input.onImages(paste.images);
  return true;
}

export function createMessageDraftImage(
  image: ReferencedImage,
  uploading: boolean
): { image: AgentComposerDraftImage; reference?: ImageReference } {
  return {
    image: {
      id: crypto.randomUUID(),
      name: image.name,
      mimeType: image.mimeType,
      ...(image.reference
        ? { attachmentId: image.reference.attachmentId }
        : { data: image.data }),
      previewUrl: image.reference
        ? ""
        : `data:${image.mimeType};base64,${image.data}`,
      uploading
    },
    reference: image.reference
  };
}

export function prepareMessageDraftImage(input: {
  workspaceId: string;
  image: AgentComposerDraftImage;
  reference?: ImageReference;
  runtime: Pick<ConversationServices, "attachments"> | null;
  uploadPromptContent: NonNullable<
    NonNullable<ConversationServices["attachments"]>["uploadPromptContent"]
  >;
}): Promise<{ content: PreparedImage[] }> {
  const { image, reference, runtime, workspaceId, uploadPromptContent } = input;
  if (!reference) {
    return uploadPromptContent({
      workspaceId,
      content: [
        {
          type: "image",
          mimeType: image.mimeType,
          data: image.data,
          name: image.name
        }
      ]
    });
  }
  if (
    reference.workspaceId !== workspaceId ||
    !runtime?.attachments?.readSessionAttachment
  ) {
    return Promise.reject(
      new Error("The image reference is unavailable in this workspace.")
    );
  }
  return runtime.attachments
    ?.readSessionAttachment(reference)
    .then((attachment) => {
      if (
        !supportedImageType(attachment.mimeType) ||
        !attachment.data ||
        attachment.mimeType !== image.mimeType
      ) {
        throw new Error("The referenced image could not be read.");
      }
      return {
        content: [
          {
            type: "image",
            attachmentId: reference.attachmentId,
            name: image.name,
            mimeType: image.mimeType,
            previewUrl: `data:${attachment.mimeType};base64,${attachment.data}`
          }
        ]
      };
    });
}
