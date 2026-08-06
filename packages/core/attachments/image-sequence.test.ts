import { describe, expect, it } from "vitest";
import type { Attachment } from "../types/attachment";
import {
  collectImageSequence,
  indexOfImageKey,
  isImageAttachment,
  matchAttachmentByURL,
  selectStandaloneAttachments,
} from "./image-sequence";

function attachment(over: Partial<Attachment> & { id: string }): Attachment {
  return {
    workspace_id: "ws",
    issue_id: null,
    comment_id: null,
    chat_session_id: null,
    chat_message_id: null,
    uploader_type: "member",
    uploader_id: "u1",
    filename: "shot.png",
    url: `https://cdn.example.com/${over.id}.png`,
    download_url: `/api/attachments/${over.id}/download`,
    markdown_url: `https://api.example.com/api/attachments/${over.id}/download`,
    content_type: "image/png",
    size_bytes: 100,
    created_at: "2026-08-05T00:00:00Z",
    ...over,
  } as Attachment;
}

const UUID_A = "11111111-1111-4111-8111-111111111111";
const UUID_B = "22222222-2222-4222-8222-222222222222";
const UUID_C = "33333333-3333-4333-8333-333333333333";

describe("isImageAttachment", () => {
  it("accepts image content types and image extensions", () => {
    expect(isImageAttachment("image/png", "")).toBe(true);
    expect(isImageAttachment("image/svg+xml; charset=utf-8", "a")).toBe(true);
    expect(isImageAttachment("", "diagram.WEBP")).toBe(true);
    expect(isImageAttachment("", "https://cdn/x/y.jpg?sig=abc")).toBe(true);
  });

  it("rejects everything else", () => {
    expect(isImageAttachment("application/pdf", "report.pdf")).toBe(false);
    expect(isImageAttachment("video/mp4", "clip.mp4")).toBe(false);
    expect(isImageAttachment("", "notes")).toBe(false);
    expect(isImageAttachment("", "archive.tar.gz")).toBe(false);
  });
});

describe("matchAttachmentByURL", () => {
  const a = attachment({ id: UUID_A });

  it("matches the stable download path regardless of host and query", () => {
    expect(
      matchAttachmentByURL(
        `https://other-host/api/attachments/${UUID_A}/download?x=1`,
        [a],
      ),
    ).toBe(a);
  });

  it("falls back to legacy full-URL equality", () => {
    const legacy = attachment({
      id: UUID_B,
      download_url: "",
      markdown_url: "",
      url: "/uploads/k?exp=1&sig=2",
    });
    expect(matchAttachmentByURL("/uploads/k?exp=9&sig=9", [legacy])).toBe(legacy);
  });

  it("returns undefined for unrelated URLs", () => {
    expect(matchAttachmentByURL("https://example.com/x.png", [a])).toBeUndefined();
  });
});

describe("selectStandaloneAttachments", () => {
  it("drops attachments already referenced inline", () => {
    const inline = attachment({ id: UUID_A, filename: "inline.png" });
    const alone = attachment({ id: UUID_B, filename: "alone.png" });
    const content = `![](/api/attachments/${UUID_A}/download)`;
    expect(
      selectStandaloneAttachments(content, [inline, alone]).map((a) => a.id),
    ).toEqual([UUID_B]);
  });

  it("drops a duplicate upload of a file that is already inline", () => {
    const inline = attachment({ id: UUID_A, filename: "same.png" });
    const dupe = attachment({ id: UUID_B, filename: "same.png" });
    const content = `![](/api/attachments/${UUID_A}/download)`;
    expect(selectStandaloneAttachments(content, [inline, dupe])).toEqual([]);
  });

  it("keeps everything when there is no content", () => {
    const a = attachment({ id: UUID_A });
    expect(selectStandaloneAttachments("", [a])).toEqual([a]);
  });
});

describe("collectImageSequence", () => {
  it("orders inline images by position, then the standalone cards", () => {
    const inlineA = attachment({ id: UUID_A, filename: "a.png" });
    const inlineB = attachment({ id: UUID_B, filename: "b.png" });
    const standalone = attachment({ id: UUID_C, filename: "c.png" });

    const sequence = collectImageSequence([
      {
        content: [
          `![b](/api/attachments/${UUID_B}/download)`,
          "text between",
          `![a](/api/attachments/${UUID_A}/download)`,
        ].join("\n\n"),
        attachments: [inlineA, inlineB, standalone],
      },
    ]);

    expect(sequence.map((i) => i.key)).toEqual([UUID_B, UUID_A, UUID_C]);
    expect(sequence[0]?.attachment).toBe(inlineB);
  });

  it("walks blocks in order and de-duplicates repeats", () => {
    const shared = attachment({ id: UUID_A });
    const sequence = collectImageSequence([
      { content: `![](/api/attachments/${UUID_A}/download)`, attachments: [shared] },
      { content: "no images here" },
      { content: `![again](/api/attachments/${UUID_A}/download)`, attachments: [shared] },
      { content: "![external](https://example.com/pic.png)" },
    ]);
    expect(sequence.map((i) => i.key)).toEqual([
      UUID_A,
      "https://example.com/pic.png",
    ]);
  });

  it("keeps unresolved markdown images, keyed by their URL", () => {
    const sequence = collectImageSequence([
      { content: '![shot](https://cdn/x.png "title")' },
    ]);
    expect(sequence).toEqual([
      {
        key: "https://cdn/x.png",
        url: "https://cdn/x.png",
        filename: "shot",
        attachment: undefined,
      },
    ]);
  });

  it("excludes non-image attachments entirely", () => {
    const pdf = attachment({
      id: UUID_A,
      filename: "report.pdf",
      content_type: "application/pdf",
    });
    const video = attachment({
      id: UUID_B,
      filename: "clip.mp4",
      content_type: "video/mp4",
    });
    const image = attachment({ id: UUID_C });
    expect(
      collectImageSequence([{ attachments: [pdf, video, image] }]).map(
        (i) => i.key,
      ),
    ).toEqual([UUID_C]);
  });

  it("ignores image syntax inside fenced blocks and code spans", () => {
    const sequence = collectImageSequence([
      {
        content: [
          "How to embed one:",
          "```md",
          "![example](https://cdn/never.png)",
          "```",
          "and inline `![also](https://cdn/nope.png)` too",
          "![real](https://cdn/yes.png)",
        ].join("\n"),
      },
    ]);
    expect(sequence.map((i) => i.key)).toEqual(["https://cdn/yes.png"]);
  });

  it("reads raw <img> tags and image-named file cards", () => {
    const sequence = collectImageSequence([
      {
        content: [
          '<img src="https://cdn/raw.png" alt="raw">',
          `!file[chart.png](/api/attachments/${UUID_A}/download)`,
          `!file[notes.txt](/api/attachments/${UUID_B}/download)`,
        ].join("\n\n"),
      },
    ]);
    expect(sequence.map((i) => i.key)).toEqual([
      "https://cdn/raw.png",
      `/api/attachments/${UUID_A}/download`,
    ]);
  });

  it("returns an empty sequence for empty input", () => {
    expect(collectImageSequence([])).toEqual([]);
    expect(collectImageSequence([null, undefined, {}])).toEqual([]);
  });
});

describe("indexOfImageKey", () => {
  it("finds a key and reports -1 for anything else", () => {
    const items = collectImageSequence([
      { content: "![a](https://cdn/a.png)\n\n![b](https://cdn/b.png)" },
    ]);
    expect(indexOfImageKey(items, "https://cdn/b.png")).toBe(1);
    expect(indexOfImageKey(items, "https://cdn/missing.png")).toBe(-1);
    expect(indexOfImageKey(items, "")).toBe(-1);
  });
});
