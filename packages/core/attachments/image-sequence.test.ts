// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { Attachment } from "../types/attachment";
import {
  collectAttachmentSequence,
  collectImageSequence,
  indexOfImageKey,
  isImageAttachment,
  matchAttachmentByURL,
  orderStandaloneAttachments,
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

  it("matches historical local URLs after the backend aliases the storage URL", () => {
    const local = attachment({ id: UUID_A, url: `/api/attachments/${UUID_A}/download` });
    const url = `/uploads/workspaces/${UUID_B}/${UUID_A}.png`;
    expect(matchAttachmentByURL(url, [local])).toBe(local);
    expect(selectStandaloneAttachments(`![](${url})`, [local])).toEqual([]);
    expect(collectImageSequence([{ content: `![](${url})`, attachments: [local] }])).toHaveLength(1);
  });

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

  it("matches encoded Markdown destinations to raw metadata without changing signed URLs", () => {
    const legacy = attachment({
      id: UUID_B,
      url: "https://cdn.example/图 v2.png",
      download_url: "https://cdn.example/signed.png?Signature=raw%2Bvalue",
    });
    const content = "![shot](<https://cdn.example/图 v2.png>)";
    const items = collectImageSequence([{ content, attachments: [legacy], standalone: false }]);
    expect(items).toHaveLength(1);
    expect(items[0]?.attachment).toBe(legacy);
    expect(items[0]?.key).toBe(UUID_B);
    expect(items[0]?.url).toBe(legacy.download_url);
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
        imageByConstruction: true,
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

  it.each([
    ["full reference", "![A][shot]", "[shot]: /a.png", "/a.png"],
    ["collapsed reference", "![shot][]", "[shot]: /a.png", "/a.png"],
    ["shortcut reference", "![shot]", "[shot]: /a.png", "/a.png"],
    ["normalized reference", "![A][ShOt  Name]", "[shot name]: /a.png", "/a.png"],
    ["angle destination", "![A](</chart v2.png>)", "", "/chart%20v2.png"],
    ["Unicode destination", "![A](/图.png)", "", "/%E5%9B%BE.png"],
    ["balanced parentheses", "![A](/chart(v2).png)", "", "/chart(v2).png"],
    ["escaped destination", String.raw`![A](/chart\(v2\).png)`, "", "/chart(v2).png"],
    ["encoded destination", "![A](/chart.png?a=1&amp;b=2)", "", "/chart.png?a=1&b=2"],
  ])("collects %s using Markdown semantics", (_name, image, definition, url) => {
    const content = `${image}\n\n${definition}`;
    expect(collectImageSequence([{ content }]).map((item) => item.key)).toEqual([url]);
  });

  it("uses the first definition and keeps definitions local to each block", () => {
    const content = "![A][shot]\n\n[shot]: /first.png\n\n[shot]: /second.png";
    expect(collectImageSequence([{ content }, { content: "![B][shot]" }]).map((item) => item.key))
      .toEqual(["/first.png"]);
  });

  it("ignores HTML and file-card examples inside code", () => {
    const content = [
      "```html", '<img src="/hidden.png">', "!file[hidden.png](/hidden.png)", "```",
      "!file[visible.png](/visible.png)",
    ].join("\n");
    expect(collectImageSequence([{ content }]).map((item) => item.key)).toEqual(["/visible.png"]);
  });

  it("resolves reference attachments in occurrence order, not definition order", () => {
    const a = attachment({ id: UUID_A });
    const b = attachment({ id: UUID_B });
    const content = [
      "![B][second]", "![A][first]", "![again][second]",
      `[first]: /api/attachments/${UUID_A}/download`,
      `[second]: /api/attachments/${UUID_B}/download`,
    ].join("\n\n");
    const items = collectImageSequence([{ content, attachments: [a, b], standalone: false }]);
    expect(items.map((item) => item.key)).toEqual([UUID_B, UUID_A]);
    expect(items.map((item) => item.attachment)).toEqual([b, a]);
  });

  it.each([
    String.raw`\![literal](/hidden.png)`,
    "    ![indented](/hidden.png)",
    "> ```md\n> ![quoted](/hidden.png)\n> ```",
    "`![multiline](/hidden.png)\ncode`",
    "![undefined][missing]",
  ])("does not collect non-rendered images in %s", (content) => {
    expect(collectImageSequence([{ content }])).toEqual([]);
  });

  it("includes indented file cards that the renderer trims into images", () => {
    const content = "    !file[photo.png](https://cdn.example/photo.png)";
    expect(collectImageSequence([{ content }]).map((item) => item.key))
      .toEqual(["https://cdn.example/photo.png"]);
  });

  it("returns an empty sequence for empty input", () => {
    expect(collectImageSequence([])).toEqual([]);
    expect(collectImageSequence([null, undefined, {}])).toEqual([]);
  });
});

describe("collectAttachmentSequence", () => {
  it("resolves current attachments, filters, and block ids when reusing body refs", () => {
    const content = "!file[report.pdf](https://cdn.example/report.pdf)";
    const blocks = [{ id: "first", content, standalone: false }];
    expect(collectImageSequence(blocks)).toEqual([]);
    expect(collectAttachmentSequence(blocks, () => true)).toEqual([
      expect.objectContaining({
        key: "https://cdn.example/report.pdf",
        blockId: "first",
        filename: "report.pdf",
      }),
    ]);

    const record = attachment({
      id: UUID_A,
      filename: "report.pdf",
      content_type: "application/pdf",
      url: "https://cdn.example/report.pdf",
      download_url: "https://cdn.example/report.pdf?signature=first",
    });
    const withRecord = [{ ...blocks[0]!, id: "second", attachments: [record] }];
    expect(collectAttachmentSequence(withRecord, () => true)).toEqual([
      expect.objectContaining({ key: UUID_A, url: record.download_url, attachment: record, blockId: "second" }),
    ]);

    const refreshed = { ...record, download_url: "https://cdn.example/report.pdf?signature=refreshed" };
    const result = collectAttachmentSequence([{ ...withRecord[0]!, attachments: [refreshed] }], () => true);
    expect(result[0]?.url).toBe(refreshed.download_url);
    expect(result[0]?.attachment).toBe(refreshed);
    result[0]!.filename = "changed by caller";
    expect(collectAttachmentSequence(blocks, () => true)[0]?.filename).toBe("report.pdf");
    expect(collectImageSequence(withRecord)).toEqual([]);
  });

  // The web / desktop rule: anything the viewer can open.
  const previewable = ({ contentType, filename }: { contentType: string; filename: string }) =>
    isImageAttachment(contentType, filename) ||
    contentType === "application/pdf" ||
    filename.endsWith(".md");

  it("pages through every kind the rule accepts, in render order", () => {
    const pdf = attachment({
      id: UUID_A,
      filename: "spec.pdf",
      content_type: "application/pdf",
    });
    const image = attachment({ id: UUID_B });
    const zip = attachment({
      id: UUID_C,
      filename: "bundle.zip",
      content_type: "application/zip",
    });

    const sequence = collectAttachmentSequence(
      [
        {
          content: `![shot](/api/attachments/${UUID_B}/download)`,
          attachments: [image, pdf, zip],
        },
      ],
      previewable,
    );

    expect(sequence.map((i) => i.key)).toEqual([UUID_B, UUID_A]);
    expect(sequence.map((i) => i.imageByConstruction)).toEqual([true, false]);
  });

  it("asks the rule about file cards and says whether they resolved", () => {
    const seen: Array<{ filename: string; hasRecord: boolean }> = [];
    const notes = attachment({
      id: UUID_A,
      filename: "notes.md",
      content_type: "text/markdown",
    });

    const sequence = collectAttachmentSequence(
      [
        {
          content: [
            `!file[notes.md](/api/attachments/${UUID_A}/download)`,
            "!file[orphan.md](https://cdn/orphan.md)",
          ].join("\n\n"),
          attachments: [notes],
        },
      ],
      (candidate) => {
        seen.push({ filename: candidate.filename, hasRecord: candidate.hasRecord });
        return candidate.hasRecord;
      },
    );

    expect(seen).toEqual([
      { filename: "notes.md", hasRecord: true },
      { filename: "orphan.md", hasRecord: false },
    ]);
    expect(sequence.map((i) => i.key)).toEqual([UUID_A]);
  });

  it("resolves a non-standalone block's references without listing the rest", () => {
    const inline = attachment({ id: UUID_A });
    const elsewhere = attachment({ id: UUID_B, filename: "later.png" });
    const sequence = collectAttachmentSequence(
      [
        {
          content: `![](/api/attachments/${UUID_A}/download)`,
          attachments: [inline, elsewhere],
          standalone: false,
        },
        { attachments: [elsewhere] },
      ],
      previewable,
    );
    // `elsewhere` sits at its own block's position, not the first block's.
    expect(sequence.map((i) => [i.key, i.attachment])).toEqual([
      [UUID_A, inline],
      [UUID_B, elsewhere],
    ]);
  });

  it("tags each item with the block it first appeared in", () => {
    const shared = attachment({ id: UUID_A });
    const own = attachment({ id: UUID_B, filename: "own.png" });
    const sequence = collectAttachmentSequence(
      [
        {
          id: "description",
          content: `![](/api/attachments/${UUID_A}/download)`,
          attachments: [shared],
          standalone: false,
        },
        { id: "comment-1", content: `![](/api/attachments/${UUID_A}/download)`, attachments: [shared, own] },
        { content: "![untagged](https://cdn/x.png)" },
      ],
      previewable,
    );
    expect(sequence.map((i) => [i.key, i.blockId])).toEqual([
      [UUID_A, "description"],
      [UUID_B, "comment-1"],
      ["https://cdn/x.png", undefined],
    ]);
  });

  it("keeps markdown images even when the rule would reject their caption", () => {
    const sequence = collectAttachmentSequence(
      [{ content: "![报告图表](https://cdn/chart)" }],
      () => false,
    );
    expect(sequence.map((i) => i.key)).toEqual(["https://cdn/chart"]);
  });
});

describe("orderStandaloneAttachments", () => {
  it("puts images first, then every other file — HTML included — keeping order within each", () => {
    const md = attachment({ id: "md", filename: "notes.md", content_type: "text/markdown" });
    const shot1 = attachment({ id: "s1", filename: "a.png" });
    const page = attachment({ id: "h", filename: "report.html", content_type: "text/html; charset=utf-8" });
    const csv = attachment({ id: "csv", filename: "data.csv", content_type: "text/csv" });
    const shot2 = attachment({ id: "s2", filename: "b.jpg", content_type: "" });
    expect(orderStandaloneAttachments([md, shot1, page, csv, shot2]).map((a) => a.id)).toEqual([
      "s1", "s2", "md", "h", "csv",
    ]);
  });

  it("is the order the sequence walks a block's standalone files in", () => {
    const md = attachment({ id: UUID_A, filename: "notes.md", content_type: "text/markdown" });
    const shot = attachment({ id: UUID_B, filename: "a.png" });
    const sequence = collectAttachmentSequence([{ attachments: [md, shot] }], () => true);
    expect(sequence.map((i) => i.key)).toEqual([UUID_B, UUID_A]);
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
