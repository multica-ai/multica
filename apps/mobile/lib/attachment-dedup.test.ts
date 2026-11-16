import { describe, expect, it } from "vitest";
import type { Attachment } from "@multica/core/types";
import { standaloneAttachments } from "./attachment-dedup";

function att(over: Partial<Attachment> = {}): Attachment {
  return {
    id: "att-1",
    workspace_id: "ws-1",
    issue_id: null,
    comment_id: null,
    chat_session_id: null,
    chat_message_id: null,
    uploader_type: "agent",
    uploader_id: "agent-1",
    filename: "chart.png",
    url: "https://cdn.example/chart.png",
    download_url: "https://signed.example/chart.png?sig=x",
    markdown_url: "https://public.example/api/attachments/att-1/download",
    content_type: "image/png",
    size_bytes: 123,
    created_at: "2026-07-09T00:00:00Z",
    ...over,
  };
}

describe("standaloneAttachments", () => {
  it("excludes an attachment referenced inline via markdown_url (the CLI snippet form)", () => {
    const a = att();
    const content = `see the result\n\n![chart.png](${a.markdown_url})`;
    expect(standaloneAttachments([a], content)).toEqual([]);
  });

  it("excludes an attachment referenced inline via the stable download path", () => {
    const a = att();
    const content = `![chart.png](/api/attachments/${a.id}/download)`;
    expect(standaloneAttachments([a], content)).toEqual([]);
  });

  it("excludes an attachment referenced inline via raw url", () => {
    const a = att();
    expect(standaloneAttachments([a], `![x](${a.url})`)).toEqual([]);
  });

  it("excludes an attachment referenced inline via signed download_url", () => {
    const a = att();
    expect(standaloneAttachments([a], `![x](${a.download_url})`)).toEqual([]);
  });

  it("keeps an attachment that is not referenced anywhere in the body", () => {
    const a = att();
    expect(standaloneAttachments([a], "just some text, no image")).toEqual([a]);
  });

  it("renders every attachment when content is undefined (no body to reference them)", () => {
    const a = att();
    expect(standaloneAttachments([a], undefined)).toEqual([a]);
  });

  it("drops a duplicate upload whose same-identity sibling is inline via markdown_url", () => {
    const inline = att({ id: "att-inline" });
    const dup = att({ id: "att-dup", url: "https://cdn.example/other.png" });
    // Only the sibling's markdown_url appears in the body; the dup shares
    // filename/type/size, so it must be treated as the same file and dropped.
    const content = `![chart.png](${inline.markdown_url})`;
    expect(standaloneAttachments([inline, dup], content)).toEqual([]);
  });
});
