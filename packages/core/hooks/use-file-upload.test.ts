/**
 * @vitest-environment jsdom
 */
import { describe, expect, it, vi } from "vitest";
import { renderHook, act } from "@testing-library/react";
import type { ApiClient } from "../api/client";
import type { Attachment } from "../types";
import { useFileUpload, type UploadResult } from "./use-file-upload";

// MUL-3192 — verifies that the URL chosen for markdown persistence is
// a durable, server-supplied absolute URL when available, and falls
// through to the legacy site-relative shape only when the server didn't
// populate `markdown_url` (older deployments) or when there's no
// attachment-row id at all (no-workspace avatar branch).

function makeAttachment(overrides: Partial<Attachment> = {}): Attachment {
  return {
    id: "att-1",
    workspace_id: "ws-1",
    issue_id: null,
    comment_id: null,
    chat_session_id: null,
    chat_message_id: null,
    uploader_type: "member",
    uploader_id: "u-1",
    filename: "shot.png",
    url: "/uploads/ws-1/shot.png",
    download_url: "/api/attachments/att-1/download",
    markdown_url: "https://api.multica.test/api/attachments/att-1/download",
    content_type: "image/png",
    size_bytes: 1,
    created_at: "2026-06-10T00:00:00Z",
    ...overrides,
  };
}

function makeApi(att: Attachment): ApiClient {
  return {
    uploadFile: vi.fn().mockResolvedValue(att),
  } as unknown as ApiClient;
}

async function runUpload(api: ApiClient): Promise<UploadResult | null> {
  const { result } = renderHook(() => useFileUpload(api));
  let upload: UploadResult | null = null;
  await act(async () => {
    upload = await result.current.upload(
      new File(["data"], "shot.png", { type: "image/png" }),
    );
  });
  return upload;
}

describe("useFileUpload — markdownLink picks the durable URL with three-layer fallback", () => {
  it("prefers att.markdown_url when the server populates it (modern deployment)", async () => {
    const att = makeAttachment({
      markdown_url: "https://cdn.multica.test/uploads/abc.png",
    });
    const upload = await runUpload(makeApi(att));
    expect(upload?.markdownLink).toBe("https://cdn.multica.test/uploads/abc.png");
    // `link` keeps its legacy semantics — same as att.url, used by avatar /
    // logo callers that persist into long-lived fields.
    expect(upload?.link).toBe(att.url);
  });

  it("falls back to the site-relative download path when the server omitted markdown_url (legacy server)", async () => {
    const att = makeAttachment({ markdown_url: "" });
    const upload = await runUpload(makeApi(att));
    // On web this is fine — Next.js rewrite proxies /api/* to the API
    // host server-side. Non-web surfaces hit attachment.tsx's legacy
    // absolutize fallback at render time.
    expect(upload?.markdownLink).toBe("/api/attachments/att-1/download");
  });

  it("falls back to att.url when there's no attachment-row id (no-workspace avatar branch)", async () => {
    const att = makeAttachment({
      id: "",
      markdown_url: "",
      url: "https://cdn.multica.test/avatars/u-1.png",
    });
    const upload = await runUpload(makeApi(att));
    expect(upload?.markdownLink).toBe("https://cdn.multica.test/avatars/u-1.png");
    expect(upload?.link).toBe("https://cdn.multica.test/avatars/u-1.png");
  });

  it("rejects oversize files before hitting the network", async () => {
    const att = makeAttachment();
    const api = makeApi(att);
    const huge = new File([new ArrayBuffer(1)], "big.bin", {
      type: "application/octet-stream",
    });
    Object.defineProperty(huge, "size", { value: 200 * 1024 * 1024 });

    const { result } = renderHook(() => useFileUpload(api));
    await expect(
      act(async () => {
        await result.current.upload(huge);
      }),
    ).rejects.toThrow(/100 MB/);
    expect(api.uploadFile as ReturnType<typeof vi.fn>).not.toHaveBeenCalled();
  });
});
