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

// MUL-3339 — `uploading` is an in-flight counter, not a single boolean.
// The single-boolean shape silently regressed the quick-create multi-image
// attach flow: callers fire N concurrent uploads (drag-drop, multi-image
// paste), the first upload's `finally` would flip `uploading` back to false
// while N-1 are still in flight, and the submit gate (which only reads
// `uploading`) would unblock — `stripBlobUrls` then erased the still-pending
// images from the markdown and their attachment ids never reached the
// server. The fix tracks an in-flight counter and exposes
// `uploading = count > 0`, so callers see "uploading" as long as ANY upload
// is in flight.
describe("useFileUpload — concurrent uploads (MUL-3339 regression)", () => {
  it("keeps uploading=true until ALL concurrent uploads resolve", async () => {
    // Hand-rolled deferreds so the test controls resolve order.
    const att1 = makeAttachment({ id: "att-1" });
    const att2 = makeAttachment({ id: "att-2" });
    let resolve1: (v: Attachment) => void = () => {};
    let resolve2: (v: Attachment) => void = () => {};
    const p1 = new Promise<Attachment>((r) => {
      resolve1 = r;
    });
    const p2 = new Promise<Attachment>((r) => {
      resolve2 = r;
    });
    const uploadFile = vi
      .fn<(file: File) => Promise<Attachment>>()
      .mockReturnValueOnce(p1)
      .mockReturnValueOnce(p2);
    const api = { uploadFile } as unknown as ApiClient;

    const { result } = renderHook(() => useFileUpload(api));
    expect(result.current.uploading).toBe(false);

    // Fire both uploads concurrently — same shape as the quick-create
    // drag-drop path (`files.forEach((f) => editorRef.current?.uploadFile(f))`).
    let pending1: Promise<UploadResult | null> = Promise.resolve(null);
    let pending2: Promise<UploadResult | null> = Promise.resolve(null);
    await act(async () => {
      pending1 = result.current.upload(
        new File(["1"], "a.png", { type: "image/png" }),
      );
      pending2 = result.current.upload(
        new File(["2"], "b.png", { type: "image/png" }),
      );
    });
    expect(result.current.uploading).toBe(true);

    // Resolve the FIRST upload only. With the old single-boolean shape this
    // would flip `uploading` back to false — that's the production bug.
    // With the in-flight counter, `uploading` stays true because upload 2
    // is still pending.
    await act(async () => {
      resolve1(att1);
      await pending1;
    });
    expect(result.current.uploading).toBe(true);

    // Now resolve the second upload — only at this point should the gate open.
    await act(async () => {
      resolve2(att2);
      await pending2;
    });
    expect(result.current.uploading).toBe(false);
  });

  it("decrements correctly when one of the concurrent uploads throws", async () => {
    // The `finally` block runs on rejection too — the counter must still
    // decrement so a failed upload never leaves the flag stuck "uploading".
    const att = makeAttachment();
    let resolveOk: (v: Attachment) => void = () => {};
    let rejectBad: (e: Error) => void = () => {};
    const ok = new Promise<Attachment>((r) => {
      resolveOk = r;
    });
    const bad = new Promise<Attachment>((_, j) => {
      rejectBad = j;
    });
    const uploadFile = vi
      .fn<(file: File) => Promise<Attachment>>()
      .mockReturnValueOnce(ok)
      .mockReturnValueOnce(bad);
    const api = { uploadFile } as unknown as ApiClient;

    const { result } = renderHook(() => useFileUpload(api));
    let okPending: Promise<UploadResult | null> = Promise.resolve(null);
    let badPending: Promise<UploadResult | null> = Promise.resolve(null);
    await act(async () => {
      okPending = result.current.upload(
        new File(["a"], "a.png", { type: "image/png" }),
      );
      // uploadWithToast swallows errors via onError; we test the raw `upload`
      // so the caller sees the rejection. Wrap in a catch so vitest doesn't
      // surface an unhandled rejection from the act() boundary.
      badPending = result.current.upload(
        new File(["b"], "b.png", { type: "image/png" }),
      ).catch(() => null);
    });
    expect(result.current.uploading).toBe(true);

    await act(async () => {
      rejectBad(new Error("boom"));
      await badPending;
    });
    // One still in flight — must remain uploading.
    expect(result.current.uploading).toBe(true);

    await act(async () => {
      resolveOk(att);
      await okPending;
    });
    expect(result.current.uploading).toBe(false);
  });
});
