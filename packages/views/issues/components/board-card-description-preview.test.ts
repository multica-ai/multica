import { describe, expect, it } from "vitest";

import { descriptionPreview } from "./board-card";

const ATTACHMENT_ID = "0f8a1b2c-3d4e-4f50-9a6b-7c8d9e0f1a2b";
const IMAGE = `![](/api/attachments/${ATTACHMENT_ID}/download)`;
const MARKER = `<!-- multica:channel-media:${ATTACHMENT_ID} -->`;

describe("descriptionPreview", () => {
  // A `/issue` message carrying an image materializes into image Markdown plus
  // an invisible provenance marker. The image pass drops the picture, so the
  // marker must be stripped too or it becomes the card's visible preview text.
  it("hides the channel-media marker that outlives the image it annotates", () => {
    expect(descriptionPreview(`Ordered rich text\n\n${IMAGE}\n\n${MARKER}`)).toBe(
      "Ordered rich text",
    );
  });

  it("previews nothing for an image-only channel description", () => {
    expect(descriptionPreview(`${IMAGE}\n\n${MARKER}`)).toBe("");
  });

  it("still flattens ordinary markdown to a single line", () => {
    expect(
      descriptionPreview("# Title\n\n**bold** text and [a link](https://example.com)"),
    ).toBe("Title bold text and a link");
  });
});
