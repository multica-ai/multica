// @vitest-environment node
import { describe,expect,it,vi } from "vitest";
import type { AgentMessageRowVM } from "../../../vendor/tutti/packages/agent/gui/shared/agentConversation/contracts/agentMessageRowVM";
import { projectAgentMessageFinalText } from "../../../vendor/tutti/packages/agent/gui/shared/agentConversation/projection/agentMessageFinalTextProjection";
import type { WorkspaceAgentSessionDetailViewModel } from "../../../vendor/tutti/packages/agent/gui/shared/workspaceAgentSessionDetailViewModel";
import {
createMessageDraftImage,
parseMessageImagePaste,
pasteMessageImageReferences,
prepareMessageDraftImage
} from "./message-image-clipboard";

const detail = {
  turns: [],
  session: {}
} as unknown as WorkspaceAgentSessionDetailViewModel;
const row: AgentMessageRowVM = {
  kind: "message",
  id: "row",
  turnId: "turn",
  speaker: "user",
  thinking: [],
  occurredAtUnixMs: null,
  messages: [
    {
      kind: "message-content",
      id: "images",
      turnId: "turn",
      body: "",
      presentationKind: "content",
      contentKind: "image-grid",
      occurredAtUnixMs: null,
      images: [
        {
          id: "image",
          workspaceId: "workspace-1",
          agentSessionId: "source-session",
          attachmentId: "image-1",
          mimeType: "image/png",
          name: "screen.png",
          data: "large-preview-must-not-be-copied"
        }
      ]
    },
    {
      kind: "message-content",
      id: "text",
      turnId: "turn",
      body: "Look at this",
      presentationKind: "content",
      contentKind: "text",
      occurredAtUnixMs: null
    }
  ]
};

describe("message image clipboard references", () => {
  it("copies a complete user message as text and a stable image address", () => {
    const projected = projectAgentMessageFinalText(
      [row],
      detail
    )[0] as AgentMessageRowVM;
    const copies = projected.messages.filter((message) => message.copyText);
    expect(copies).toHaveLength(1);
    expect(copies[0]!.copyText).toContain("Look at this");
    expect(copies[0]!.copyText).toContain(
      "multica-image://attachment/workspace-1/image-1"
    );
    expect(copies[0]!.copyText).not.toContain(
      "large-preview-must-not-be-copied"
    );
  });

  it("keeps pure text copying unchanged", () => {
    const textRow = { ...row, messages: row.messages.slice(1) };
    const projected = projectAgentMessageFinalText(
      [textRow],
      detail
    )[0] as AgentMessageRowVM;
    expect(projected.messages[0]!.copyText).toBe("Look at this");
    expect(parseMessageImagePaste("ordinary text")).toBeNull();
  });

  it.each([false, true])(
    "preserves ordered images and the full text (image only: %s)",
    (imageOnly) => {
      const original = structuredClone(row);
      original.messages[0]!.images!.push({
        ...original.messages[0]!.images![0]!,
        id: "second",
        attachmentId: "image-2",
        name: "空 格 & ?.png"
      });
      original.messages[1]!.body = "paragraph 1\n\nparagraph 2\n\n";
      if (imageOnly) original.messages.pop();
      const projected = projectAgentMessageFinalText(
        [original],
        detail
      )[0] as AgentMessageRowVM;
      const copies = projected.messages.filter((message) => message.copyText);
      expect(copies).toHaveLength(1);
      const parsed = parseMessageImagePaste(copies[0]!.copyText!)!;
      expect(parsed.text).toBe(
        imageOnly ? "" : "paragraph 1\n\nparagraph 2\n\n"
      );
      expect(
        parsed.images.map((image) => image.reference!.attachmentId)
      ).toEqual(["image-1", "image-2"]);
      expect(parsed.images[1]!.name).toBe("空 格 & ?.png");
    }
  );

  it("routes a mixed paste once and creates separate draft instances for repeated pastes", () => {
    const projected = projectAgentMessageFinalText(
      [row],
      detail
    )[0] as AgentMessageRowVM;
    const text = projected.messages.find(
      (message) => message.copyText
    )!.copyText!;
    const onImages = vi.fn();
    const insertText = vi.fn();
    expect(
      pasteMessageImageReferences({
        text,
        supported: true,
        onImages,
        insertText
      })
    ).toBe(true);
    expect(onImages).toHaveBeenCalledTimes(1);
    expect(insertText).toHaveBeenCalledExactlyOnceWith("Look at this");
    const image = onImages.mock.calls[0]![0][0];
    const first = createMessageDraftImage(image, true).image;
    const second = createMessageDraftImage(image, true).image;
    expect(first.id).not.toBe(second.id);
    expect(first).toMatchObject({
      attachmentId: "image-1",
      uploading: true,
      previewUrl: ""
    });
    expect(first.data).toBeUndefined();
  });

  it("retains reference text when the target does not support images and leaves malformed addresses as text", () => {
    const projected = projectAgentMessageFinalText(
      [row],
      detail
    )[0] as AgentMessageRowVM;
    const text = projected.messages.find(
      (message) => message.copyText
    )!.copyText!;
    const insertText = vi.fn(),
      onUnsupported = vi.fn(),
      onImages = vi.fn();
    expect(
      pasteMessageImageReferences({
        text,
        supported: false,
        onImages,
        onUnsupported,
        insertText
      })
    ).toBe(true);
    expect(insertText).toHaveBeenCalledWith(text);
    expect(onUnsupported).toHaveBeenCalledOnce();
    expect(onImages).not.toHaveBeenCalled();
    for (const malformed of [
      "multica-image://attachment/%/id?session=s&mime=image/png",
      "multica-image://attachment/ws/id?session=s&mime=text/html",
      "multica-image://attachment/ws/id",
      "https://example.test/image.png"
    ]) {
      expect(parseMessageImagePaste(malformed)).toBeNull();
    }
  });

  it("reads image previews from the source attachment without uploading another copy", async () => {
    const projected = projectAgentMessageFinalText(
      [row],
      detail
    )[0] as AgentMessageRowVM;
    const pasted = parseMessageImagePaste(
      projected.messages.find((message) => message.copyText)!.copyText!
    )!;
    const { image, reference } = createMessageDraftImage(
      pasted.images[0]!,
      true
    );
    const readSessionAttachment = vi.fn(async () => ({
      attachmentId: "image-1",
      name: "screen.png",
      mimeType: "image/png",
      data: btoa("pixels")
    }));
    const uploadPromptContent = vi.fn();
    const result = await prepareMessageDraftImage({
      workspaceId: "workspace-1",
      image,
      reference,
      runtime: { attachments: { readSessionAttachment } },
      uploadPromptContent
    });
    expect(readSessionAttachment).toHaveBeenCalledWith({
      workspaceId: "workspace-1",
      agentSessionId: "source-session",
      attachmentId: "image-1"
    });
    expect(result.content[0]).toMatchObject({
      attachmentId: "image-1",
      previewUrl: `data:image/png;base64,${btoa("pixels")}`
    });
    expect(result.content[0]!.data).toBeUndefined();
    expect(uploadPromptContent).not.toHaveBeenCalled();
    await expect(
      prepareMessageDraftImage({
        workspaceId: "other-workspace",
        image,
        reference,
        runtime: { attachments: { readSessionAttachment } },
        uploadPromptContent
      })
    ).rejects.toThrow("unavailable in this workspace");
    expect(readSessionAttachment).toHaveBeenCalledTimes(1);
    readSessionAttachment.mockRejectedValueOnce(
      new Error("Attachment no longer exists")
    );
    await expect(
      prepareMessageDraftImage({
        workspaceId: "workspace-1",
        image,
        reference,
        runtime: { attachments: { readSessionAttachment } },
        uploadPromptContent
      })
    ).rejects.toThrow("no longer exists");
    expect(image.attachmentId).toBe("image-1");
  });
});
