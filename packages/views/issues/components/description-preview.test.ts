// @vitest-environment node
import { describe, expect, it } from "vitest";

import { descriptionPreview } from "./description-preview";

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

  // The editor serializes link brackets escaped. Without unescaping first, the
  // link rule leaves the leading `\` untouched and captures the trailing one,
  // so the preview reads `\repro script\`.
  it("unwraps a link whose brackets the editor escaped", () => {
    expect(descriptionPreview("See \\[repro script\\](https://example.com)")).toBe(
      "See repro script",
    );
  });

  it("still unwraps a link written without escapes", () => {
    expect(descriptionPreview("See [repro script](https://example.com)")).toBe(
      "See repro script",
    );
  });

  // The editor autolinks a raw URL inside a link label, producing a nested
  // link destination like `([https://x](https://x))`. A destination pattern
  // that stops at the first `)` leaves the outer paren stranded in the
  // preview.
  it("unwraps a link whose destination is itself a nested link", () => {
    expect(
      descriptionPreview(
        "This is **very** flaky and needs a \\[repro script\\]([https://example.com](https://example.com)) before we fix it.",
      ),
    ).toBe("This is very flaky and needs a repro script before we fix it.");
  });

  it("unwraps a link whose destination legitimately contains parentheses", () => {
    expect(
      descriptionPreview("See [wiki](https://en.wikipedia.org/wiki/Foo_(bar)) for details"),
    ).toBe("See wiki for details");
  });

  it("drops the backslashes from escaped emphasis and punctuation", () => {
    expect(descriptionPreview("it costs \\$5 \\*not\\* \\$10")).toBe("it costs $5 not $10");
  });

  // A backslash before a letter is not a Markdown escape, so prose keeps it.
  it("leaves a literal backslash in prose alone", () => {
    expect(descriptionPreview("open C:\\Users\\zain to continue")).toBe(
      "open C:\\Users\\zain to continue",
    );
  });
});
