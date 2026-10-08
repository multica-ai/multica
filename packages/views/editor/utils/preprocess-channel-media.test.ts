import { describe, expect, it } from "vitest";

import { preprocessMarkdown } from "./preprocess";

describe("preprocessMarkdown channel-media provenance", () => {
  it("keeps the image visible while hiding its merge marker", () => {
    const id = "22222222-2222-4222-8222-222222222222";
    const image = `![](/api/attachments/${id}/download)`;
    const marker = `<!-- multica:channel-media:${id} -->`;

    const result = preprocessMarkdown(`${image}\n\n${marker}`, { cdnDomain: "" });

    expect(result).toContain(image);
    expect(result).not.toContain("multica:channel-media");
  });
});
