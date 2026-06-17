import { describe, it, expect } from "vitest";
import { extractChatUploadAttachmentIds } from "./chat-upload-attachment-ids";

describe("extractChatUploadAttachmentIds", () => {
  // The upload path is /uploads/workspaces/<workspaceId>/<attachmentId>.<ext>
  // — BOTH segments are UUIDs, so these fixtures use a real workspace UUID to
  // guard against the capture group grabbing the workspace id by mistake.
  const WS = "11bd8321-b6ac-4bee-ae41-6659a5064608";

  it("extracts the attachment id (not the workspace id) from a !file token", () => {
    const content = `!file[note.txt](/uploads/workspaces/${WS}/019ed4dd-ea95-7e0f-a233-6e479863c54c.txt)\n\nLæs filen.`;
    expect(extractChatUploadAttachmentIds(content)).toEqual([
      "019ed4dd-ea95-7e0f-a233-6e479863c54c",
    ]);
  });

  it("extracts multiple ids and de-duplicates, preserving first-seen order", () => {
    const content = [
      `!file[a.txt](/uploads/workspaces/${WS}/019ed4dd-ea95-7e0f-a233-6e479863c54c.txt)`,
      `!file[b.json](/uploads/workspaces/${WS}/019ed500-1111-7222-8333-444455556666.json)`,
      `![img](/uploads/workspaces/${WS}/019ed4dd-ea95-7e0f-a233-6e479863c54c.png)`,
    ].join("\n");
    expect(extractChatUploadAttachmentIds(content)).toEqual([
      "019ed4dd-ea95-7e0f-a233-6e479863c54c",
      "019ed500-1111-7222-8333-444455556666",
    ]);
  });

  it("matches inline image embeds too", () => {
    const content = `![chart](/uploads/workspaces/${WS}/019ed600-aaaa-7bbb-8ccc-ddddeeeeffff.png)`;
    expect(extractChatUploadAttachmentIds(content)).toEqual([
      "019ed600-aaaa-7bbb-8ccc-ddddeeeeffff",
    ]);
  });

  it("returns an empty array when no uploads are referenced", () => {
    expect(extractChatUploadAttachmentIds("Just a plain message.")).toEqual([]);
    expect(
      extractChatUploadAttachmentIds("[external](https://example.com/x.txt)"),
    ).toEqual([]);
  });

  it("ignores upload URLs without a UUID file basename", () => {
    expect(
      extractChatUploadAttachmentIds(`/uploads/workspaces/${WS}/not-a-uuid.txt`),
    ).toEqual([]);
    // A bare workspace folder path (UUID followed by `/`, not `.<ext>`) is not an attachment.
    expect(
      extractChatUploadAttachmentIds(`/uploads/workspaces/${WS}/`),
    ).toEqual([]);
  });
});
