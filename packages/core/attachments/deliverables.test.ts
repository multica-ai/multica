// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { Attachment } from "../types/attachment";
import {
  collectDeliverableFiles,
  deliverableKey,
  findDeliverableVersion,
} from "./deliverables";

function attachment(over: Partial<Attachment> & { id: string }): Attachment {
  return {
    workspace_id: "ws",
    issue_id: "issue",
    comment_id: "c1",
    chat_session_id: null,
    chat_message_id: null,
    uploader_type: "agent",
    uploader_id: "agent-1",
    filename: "report.md",
    url: `https://cdn.example.com/${over.id}`,
    download_url: `/api/attachments/${over.id}/download`,
    markdown_url: `https://api.example.com/api/attachments/${over.id}/download`,
    content_type: "text/markdown",
    size_bytes: 100,
    created_at: "2026-09-20T10:00:00Z",
    ...over,
  } as Attachment;
}

describe("deliverableKey", () => {
  it("ignores content-type parameters and case", () => {
    expect(
      deliverableKey({ filename: "index.html", content_type: "text/html; charset=utf-8" }),
    ).toBe(deliverableKey({ filename: "index.html", content_type: "TEXT/HTML" }));
  });

  it("separates same-named files of different types", () => {
    expect(deliverableKey({ filename: "out", content_type: "image/png" })).not.toBe(
      deliverableKey({ filename: "out", content_type: "text/plain" }),
    );
  });
});

describe("collectDeliverableFiles", () => {
  it("merges re-uploads of the same file into versions, oldest first", () => {
    const v1 = attachment({ id: "a1", comment_id: "c1", created_at: "2026-09-20T10:00:00Z" });
    const v2 = attachment({ id: "a2", comment_id: "c2", created_at: "2026-09-21T10:00:00Z" });
    const files = collectDeliverableFiles([
      { id: "c2", type: "comment", attachments: [v2] },
      { id: "c1", type: "comment", attachments: [v1] },
    ]);
    expect(files).toHaveLength(1);
    expect(files[0]!.versions.map((a) => a.id)).toEqual(["a1", "a2"]);
    expect(files[0]!.latest.id).toBe("a2");
  });

  it("lists files newest first by their latest version", () => {
    const shot = attachment({
      id: "s1",
      filename: "shot.png",
      content_type: "image/png",
      created_at: "2026-09-20T09:00:00Z",
    });
    const reportV1 = attachment({ id: "r1", created_at: "2026-09-20T08:00:00Z" });
    const reportV2 = attachment({ id: "r2", created_at: "2026-09-22T08:00:00Z" });
    const files = collectDeliverableFiles([
      { id: "c1", type: "comment", attachments: [reportV1, shot] },
      { id: "c2", type: "comment", attachments: [reportV2] },
    ]);
    expect(files.map((f) => f.latest.id)).toEqual(["r2", "s1"]);
  });

  it("breaks a created_at tie by upload order (UUIDv7 ids)", () => {
    const first = attachment({ id: "01a0-0001" });
    const second = attachment({ id: "01a0-0002" });
    const [file] = collectDeliverableFiles([
      { id: "c1", attachments: [second, first] },
    ]);
    expect(file!.versions.map((a) => a.id)).toEqual(["01a0-0001", "01a0-0002"]);
  });

  it("skips activities, tombstoned comments and repeated ids", () => {
    const a = attachment({ id: "a1" });
    const files = collectDeliverableFiles([
      { id: "act", type: "activity", attachments: [attachment({ id: "x" })] },
      {
        id: "gone",
        type: "comment",
        deleted_at: "2026-09-21T00:00:00Z",
        attachments: [attachment({ id: "y", filename: "y.md" })],
      },
      { id: "c1", type: "comment", attachments: [a] },
      { id: "c1-copy", type: "comment", attachments: [a] },
      null,
    ]);
    expect(files).toHaveLength(1);
    expect(files[0]!.versions.map((v) => v.id)).toEqual(["a1"]);
  });
});

describe("findDeliverableVersion", () => {
  const v1 = attachment({ id: "a1", created_at: "2026-09-20T10:00:00Z" });
  const v2 = attachment({ id: "a2", created_at: "2026-09-21T10:00:00Z" });
  const files = collectDeliverableFiles([{ id: "c1", attachments: [v1, v2] }]);

  it("returns the file and the 1-based version", () => {
    expect(findDeliverableVersion(files, "a1")?.version).toBe(1);
    expect(findDeliverableVersion(files, "a2")?.version).toBe(2);
  });

  it("returns undefined for files that are not deliverables", () => {
    expect(findDeliverableVersion(files, "description-file")).toBeUndefined();
    expect(findDeliverableVersion(files, "")).toBeUndefined();
  });
});
