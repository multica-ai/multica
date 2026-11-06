// @vitest-environment node
import { describe, expect, it } from "vitest";
import { localV2SnapshotSchema } from "@multica/core/local-conversation/v2";
import { ConversationModel, TranscriptModel } from "@multica/conversation-client/models";
import { CommittedConversationMessages } from "../../../../vendor/tutti/packages/agent/gui/state/CommittedConversationMessages";

import { projectConversationUserRow } from "../../../../vendor/tutti/packages/agent/gui/shared/agentConversation/projection/agentConversationUserProjection";

const at = "2026-11-03T10:00:00Z";

describe("V2 user attachment projection", () => {
  it.each(["snapshot", "events"])("uses metadata for TXT, MP4, PNG and unknown attachments from %s", (source) => {
    const attachments = new Map([
      ["text", { name: "pasted.txt", mimeType: "text/plain", sizeBytes: 5000, url: "https://example.test/text" }],
      ["video", { name: "clip.mp4", mimeType: "video/mp4", sizeBytes: 10000, url: "https://example.test/video" }],
      ["image", { name: "screen.png", mimeType: "image/png", sizeBytes: 42, url: "https://example.test/image" }],
      ["unknown", { name: "unknown.png", mimeType: "", sizeBytes: 1, url: "https://example.test/unknown" }],
    ]);
    const ids = [...attachments.keys(), "unavailable"];
    const content = "[pasted.txt](https://example.test/text)\n\n[clip.mp4](https://example.test/video)";
    const conversation = ConversationModel.fromSnapshot(localV2SnapshotSchema.parse({
      version: 2, store_generation: "g", snapshot_seq: 0,
      conversation: { conversation_id: "c", summary_seq: 1, provider: "codex", title: "", status: "idle", pinned: false, archived: false, updated_at: at },
      turns: source === "snapshot" ? [{
        id: "turn", turn_index: 0, request_id: "r", status: "completed", started_at: at,
        messages: [{ id: "message", role: "user", content, attachment_ids: ids, created_at: at }], events: [],
      }] : [],
    }));
    if (source === "events") conversation.applyEvents([{
      seq: 1, turn_id: "turn", turn_seq: 1, item_id: null, type: "user.message", final: true, created_at: at,
      payload: { message_id: "message", request_id: "r", content, attachment_ids: ids },
    }]);
    const payload = new CommittedConversationMessages(new TranscriptModel(), "ws", "c", "en", undefined, undefined, attachments).update(conversation, "all")[0]!.payload;
    expect(payload.content).toEqual([
      { type: "text", text: content },
      { type: "file", attachmentId: "text", ...attachments.get("text") },
      { type: "file", attachmentId: "video", ...attachments.get("video") },
      { type: "image", attachmentId: "image", ...attachments.get("image"), url: undefined },
      { type: "file", attachmentId: "unknown", ...attachments.get("unknown") },
      { type: "file", attachmentId: "unavailable", metadataUnavailable: true },
    ]);
    expect(payload.attachments).toEqual(ids.map((attachmentId) => ({
      attachmentId, ...attachments.get(attachmentId), ...(attachmentId === "unavailable" ? { metadataUnavailable: true } : {}), ...(attachmentId === "image" ? { url: undefined } : {}),
    })));
  });
  it("updates a previously unknown attachment when metadata arrives and keeps unchanged rows stable", () => {
    const conversation = ConversationModel.fromSnapshot(localV2SnapshotSchema.parse({
      version: 2, store_generation: "g", snapshot_seq: 0,
      conversation: { conversation_id: "c", summary_seq: 1, provider: "codex", title: "", status: "idle", pinned: false, archived: false, updated_at: at },
      turns: [{ id: "turn", turn_index: 0, request_id: "r", status: "completed", started_at: at,
        messages: [{ id: "message", role: "user", content: "text", attachment_ids: ["image"], created_at: at }], events: [] }],
    }));
    const metadata = new Map<string, { name: string; mimeType: string; sizeBytes: number; url: string }>();
    const transcript = new CommittedConversationMessages(new TranscriptModel(), "ws", "c", "en", undefined, undefined, metadata);
    const initial = transcript.update(conversation, "all")[0]!;
    expect(initial.payload.content).toMatchObject([{ type: "text" }, { type: "file" }]);
    expect(transcript.update(conversation, "all")[0]).toBe(initial);
    metadata.set("image", { name: "screen.png", mimeType: "image/png", sizeBytes: 42, url: "/image" });
    const hydrated = transcript.update(conversation, "all")[0]!;
    expect(hydrated.payload.content).toMatchObject([{ type: "text" }, { type: "image", mimeType: "image/png" }]);
    expect(hydrated).not.toBe(initial);
    expect(transcript.update(conversation, "all")[0]).toBe(hydrated);
  });

  it.each(["snapshot", "events"])("keeps image-only %s visible on metadata failure and restores its final image row", (input) => {
    const conversation = ConversationModel.fromSnapshot(localV2SnapshotSchema.parse({
      version: 2, store_generation: "g", snapshot_seq: 0,
      conversation: { conversation_id: "c", summary_seq: 1, provider: "codex", title: "", status: "idle", pinned: false, archived: false, updated_at: at },
      turns: input === "snapshot" ? [{ id: "turn", turn_index: 0, request_id: "r", status: "completed", started_at: at,
        messages: [{ id: "message", role: "user", content: "[Image attachment: screen.png]", attachment_ids: ["image"], created_at: at }], events: [] }] : [],
    }));
    if (input === "events") conversation.applyEvents([{ seq: 1, turn_id: "turn", turn_seq: 1, item_id: null, type: "user.message", final: true, created_at: at,
      payload: { message_id: "message", request_id: "r", content: "[Image attachment: screen.png]", attachment_ids: ["image"] } }]);
    const metadata = new Map<string, { name: string; mimeType: string; sizeBytes: number; url: string }>();
    const transcript = new CommittedConversationMessages(new TranscriptModel(), "ws", "c", "en", undefined, undefined, metadata);
    const row = () => {
      const message = transcript.update(conversation, "all")[0]!;
      return projectConversationUserRow({ id: "message", body: String(message.payload.text ?? ""), turnId: "turn", sourceTimelineItems: [message as never] }, "turn", "ws");
    };
    expect(row().messages).toMatchObject([{ contentKind: "file-list", files: [{ attachmentId: "image", metadataUnavailable: true }] }]);
    metadata.set("image", { name: "screen.png", mimeType: "", sizeBytes: 42, url: "https://example.test/image" });
    expect(row().messages).toMatchObject([{ contentKind: "file-list", files: [{ name: "screen.png", url: "https://example.test/image", metadataUnavailable: false }] }]);
    metadata.set("image", { name: "screen.png", mimeType: "image/png", sizeBytes: 42, url: "https://example.test/image" });
    expect(row().messages).toMatchObject([{ contentKind: "image-grid", images: [{ attachmentId: "image" }] }]);
  });
  it("renders files absent from the body and does not repeat existing file links", () => {
    const message = { messageId: "m", workspaceId: "ws", agentSessionId: "c", payload: { content: [
      { type: "text", text: "[pasted.txt](https://example.test/text)" },
      { type: "file", attachmentId: "text", name: "pasted.txt", url: "https://example.test/text" },
      { type: "file", attachmentId: "video", name: "clip.mp4", url: "https://example.test/video" },
      { type: "image", attachmentId: "image", mimeType: "image/png" },
    ] } };
    const projected = projectConversationUserRow({ id: "m", body: "", sourceTimelineItems: [message as never] }, "turn", "ws");
    expect(projected.messages.map((part) => part.contentKind)).toEqual(["image-grid", "file-list", "text"]);
    expect(projected.messages.find((part) => part.contentKind === "file-list")?.files).toMatchObject([{ name: "clip.mp4" }]);
  });

});
