// @vitest-environment node
import { expect, it } from "vitest";
import { contentManifest } from "./manifest.js";
it("archives shared attachment identities once and preserves graph relationships", () => {
  const attachment = {
    originalId: "file",
    data: "aGVsbG8=",
    sha256: "checksum",
  };
  const manifest = contentManifest({
    conversations: ["parent", "child"].map((originalId) => ({
      originalId,
      snapshot: {
        conversation: {
          forked_from_id: originalId === "child" ? "parent" : null,
        },
        turns: [
          {
            id: `turn-${originalId}`,
            messages: [
              { id: `message-${originalId}`, attachment_ids: ["file"] },
            ],
          },
        ],
      },
      attachments: [attachment],
    })),
  });
  expect(manifest.attachments).toHaveLength(1);
  expect(manifest.attachments[0]?.bytes).toBe(5);
  expect(manifest.relationships).toContainEqual({
    from: "child",
    to: "parent",
    kind: "forked_from_id",
  });
  expect(manifest.relationships).toContainEqual({
    from: "message-child",
    to: "file",
    kind: "attachment",
  });
});
