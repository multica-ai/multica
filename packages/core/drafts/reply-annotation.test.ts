import { describe, expect, it } from "vitest";
import { parseMentions } from "../issues/comment-trigger-outcomes";
import { composeAnnotatedReply, hasReplyIntent, locateReplyAnnotation, normalizeReplyAnnotations, type ReplyAnnotation } from "./reply-annotation";

const annotation: ReplyAnnotation = {
  id: "a", sourceCommentId: "source", sourceActorName: "Emacs", quote: "first\nsecond",
  start: 0, prefix: "", suffix: "", note: "Please revise.",
};

describe("annotated reply serialization", () => {
  it("publishes only the user body, quotes and notes, without source links or labels", () => {
    expect(composeAnnotatedReply("Overall reply", [annotation, { ...annotation, id: "b", quote: "other", note: "" }]))
      .toBe("Overall reply\n\n> first\n> second\n\nPlease revise.\n\n&nbsp;\n\n> other");
  });

  it("preserves user-authored note Markdown and links without list indentation", () => {
    expect(composeAnnotatedReply("", [{ ...annotation, note: "See [details](https://example.com).\n\n- First change\n- Second change" }]))
      .toBe("> first\n> second\n\nSee [details](https://example.com).\n\n- First change\n- Second change");
  });

  it("keeps quoted raw mentions and markup inert, while preserving deliberate mentions", () => {
    const mention = "[@Agent](mention://agent/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa)";
    const content = composeAnnotatedReply("", [{ ...annotation, quote: `\`\`\`\n${mention}\n<script>x</script>\n&colon;\n\`\`\``, sourceActorName: mention, note: mention }]);
    expect(parseMentions(content)).toHaveLength(1);
    expect(content.match(/mention:\/\//g)).toHaveLength(1);
    expect(content).toContain("&#60;script&#62;");
    expect(content).toContain("&amp;colon;");
    expect(content).toContain(mention);
  });

  it("retains /note behavior and does not send quotes alone", () => {
    expect(composeAnnotatedReply("/note Private", [annotation])).toMatch(/^\/note Private/);
    expect(hasReplyIntent("  ", [{ ...annotation, note: " " }])).toBe(false);
    expect(hasReplyIntent("", [annotation])).toBe(true);
    expect(hasReplyIntent("Overall", [{ ...annotation, note: "" }])).toBe(true);
  });

  it("normalizes old and corrupt drafts without inventing annotations", () => {
    expect(normalizeReplyAnnotations(undefined)).toEqual([]);
    expect(normalizeReplyAnnotations([null, {}, { ...annotation, quote: "x".repeat(4001) }, annotation])).toEqual([annotation]);
  });
});

describe("quote anchors", () => {
  const anchor = { ...annotation, quote: "same", prefix: "left ", suffix: " right", start: 5 };
  it("relocates a unique quote with unchanged context", () => {
    expect(locateReplyAnnotation("left same right", anchor)).toBe(5);
    expect(locateReplyAnnotation("new paragraph left same right", anchor)).toBe(19);
  });
  it("does not guess when source text or context changed or the match is ambiguous", () => {
    expect(locateReplyAnnotation("left different right", anchor)).toBeNull();
    expect(locateReplyAnnotation("other same right", anchor)).toBeNull();
    expect(locateReplyAnnotation("left same right / left same right", anchor)).toBeNull();
  });
});
