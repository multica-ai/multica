import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { ConversationModel, TranscriptModel } from "@multica/conversation-client/models";
import { localV2SnapshotSchema } from "@multica/core/local-conversation/v2";
import { expect, it, vi } from "vitest";
import { ConversationServicesProvider, type ConversationServices } from "../../../conversationServices";
import { CommittedConversationMessages, type ConversationAttachmentMetadata } from "../../../state/CommittedConversationMessages";
import { projectConversationUserRow } from "../projection/agentConversationUserProjection";
import { AgentMessageBlock } from "./AgentMessageBlock";

vi.mock("../../../i18n/index", async (original) => ({
  ...await original<typeof import("../../../i18n/index")>(),
  useTranslation: () => ({ t: (key: string) => key.endsWith("mentionKindFile") ? "File" : key.endsWith("retryActivation") ? "Retry" : key })
}));

function conversation() {
  const at = "2026-11-03T10:00:00Z";
  return ConversationModel.fromSnapshot(localV2SnapshotSchema.parse({
    version: 2, store_generation: "g", snapshot_seq: 0,
    conversation: { conversation_id: "c", summary_seq: 1, provider: "codex", title: "", status: "idle", pinned: false, archived: false, updated_at: at },
    turns: [{ id: "t", turn_index: 0, request_id: "r", status: "completed", started_at: at,
      messages: [{ id: "m", role: "user", content: "[Image attachment: screen.png]", attachment_ids: ["image"], created_at: at }], events: [] }]
  }));
}

it("renders the real failed-metadata projection as a readable file with retry, then restores its image preview", async () => {
  const source = conversation();
  const metadata = new Map<string, ConversationAttachmentMetadata>();
  const transcript = new CommittedConversationMessages(new TranscriptModel(), "ws", "c", "en", undefined, undefined, metadata);
  const readSource = vi.fn().mockResolvedValue({ url: "https://example.test/image.png" });
  const load = vi.fn().mockImplementation(async () => {
    metadata.set("image", { name: "screen.png", mimeType: "image/png", sizeBytes: 1, url: "https://example.test/image.png" });
  });
  const runtime = { conversations: { load }, attachments: { readSessionAttachmentSource: readSource, readSessionAttachment: vi.fn().mockResolvedValue({ data: "AA==", mimeType: "image/png", name: "screen.png" }) } } as unknown as ConversationServices;
  const view = () => {
    const message = transcript.update(source, "all")[0]!;
    const row = projectConversationUserRow({ id: "m", body: String(message.payload.text ?? ""), sourceTimelineItems: [message as never] }, "t", "ws");
    return <ConversationServicesProvider runtime={runtime}><AgentMessageBlock workspaceRoot={null} basePath="/repo" row={row} thinkingLabel="Thinking" /></ConversationServicesProvider>;
  };
  const result = render(view());
  expect(result.container.querySelector("img")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "File" }));
  await waitFor(() => expect(readSource).toHaveBeenCalledWith({ workspaceId: "ws", attachmentId: "image" }));
  await waitFor(() => expect(screen.getByRole("button", { name: "Retry" })).toBeEnabled());
  fireEvent.click(screen.getByRole("button", { name: "Retry" }));
  await waitFor(() => expect(load).toHaveBeenCalledWith("ws", "c"));
  result.rerender(view());
  await waitFor(() => expect(result.container.querySelector("img")).not.toBeNull());
  expect(result.container.querySelector("[data-agent-file-attachment]")).toBeNull();
});

it("shows a named file rather than an image or a retry control when MIME is unknown", () => {
  const source = conversation();
  const metadata = new Map([["image", { name: "unknown.png", mimeType: "", sizeBytes: 1, url: "https://example.test/unknown" }]]);
  const message = new CommittedConversationMessages(new TranscriptModel(), "ws", "c", "en", undefined, undefined, metadata).update(source, "all")[0]!;
  const row = projectConversationUserRow({ id: "m", body: "", sourceTimelineItems: [message as never] }, "t", "ws");
  const { container } = render(<AgentMessageBlock workspaceRoot={null} basePath="/repo" row={row} thinkingLabel="Thinking" />);
  expect(screen.getByRole("button", { name: "unknown.png" })).toBeEnabled();
  expect(screen.queryByRole("button", { name: "Retry" })).toBeNull();
  expect(container.querySelector("img")).toBeNull();
});
