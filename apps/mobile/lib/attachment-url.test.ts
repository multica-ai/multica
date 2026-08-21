/**
 * Pure-function tests for the mobile attachment URL resolver. We exercise
 * the with-base form because `resolveAttachmentUrl` reads the active API
 * base from `server-store` at call time (RUYI-4) — that store owns
 * AsyncStorage + build-time env and is out of scope for this node-env
 * suite. The with-base helper is the same code path with the API base
 * passed in explicitly; the store is mocked below so the bound form's
 * pass-through contract stays covered.
 *
 * Coverage target: every branch the call sites in the app rely on —
 *   - `comment-attachment-list.tsx`         → file chip Linking.openURL
 *   - `markdown-image.tsx`                  → mc:// + RN image loader
 *   - `composer-attachment-row.tsx`         → completed non-image chip
 *                                             tap → Linking.openURL
 */
import { describe, expect, it, vi } from "vitest";

// server-store 在模块加载期从构建期 env 合成内置默认项,并 import
// AsyncStorage 原生模块 —— 两者在 node 环境里都不可用。这里只需要
// getApiUrl 的返回值。
vi.mock("@/data/server-store", () => ({
  getApiUrl: () => "https://api.example.test",
}));

import {
  resolveAttachmentUrl,
  resolveAttachmentUrlWithBase,
} from "./attachment-url";

describe("resolveAttachmentUrlWithBase", () => {
  const BASE = "https://api.example.test";

  it("prepends the API base for a server-relative path", () => {
    expect(
      resolveAttachmentUrlWithBase("/api/attachments/att-1/download", BASE),
    ).toBe("https://api.example.test/api/attachments/att-1/download");
  });

  it("trims a trailing slash on the API base before joining", () => {
    expect(
      resolveAttachmentUrlWithBase(
        "/api/attachments/att-1/download",
        "https://api.example.test/",
      ),
    ).toBe("https://api.example.test/api/attachments/att-1/download");
  });

  it("passes an absolute https URL through unchanged (CloudFront / presigned)", () => {
    const signed =
      "https://cdn.example.test/att-1.bin?Policy=p&Signature=s&Key-Pair-Id=k";
    expect(resolveAttachmentUrlWithBase(signed, BASE)).toBe(signed);
  });

  it("passes an absolute http URL through unchanged (self-hosted dev)", () => {
    expect(
      resolveAttachmentUrlWithBase("http://localhost:8080/file.bin", BASE),
    ).toBe("http://localhost:8080/file.bin");
  });

  it("returns null for nullish or empty input", () => {
    expect(resolveAttachmentUrlWithBase(null, BASE)).toBeNull();
    expect(resolveAttachmentUrlWithBase(undefined, BASE)).toBeNull();
    expect(resolveAttachmentUrlWithBase("", BASE)).toBeNull();
  });

  it("keeps a relative path unchanged when the base is empty (web same-origin convention)", () => {
    // Mirrors `packages/core/workspace/avatar-url.ts` semantics for the
    // empty-base case — the host platform resolves the path against its
    // own document/page origin. RN doesn't have one, but exercising this
    // branch keeps the contract explicit.
    expect(
      resolveAttachmentUrlWithBase("/api/attachments/att-1/download", ""),
    ).toBe("/api/attachments/att-1/download");
  });
});

describe("composer file chip — completed non-image attachment", () => {
  // MUL-2976 (PR #3747 follow-up): when `api.uploadFile(...)` finishes on
  // a non-CloudFront deployment the returned `attachment.download_url` is
  // a server-relative path. `composer-attachment-row.tsx` taps that value
  // straight into `Linking.openURL` — and iOS rejects relative URLs with
  // "Cannot open URL". The fix wraps the value with `resolveAttachmentUrl`
  // before handing it to Linking; this test pins the behaviour we rely on.
  const BASE = "https://api.example.test";
  // Mirrors `ComposerAttachmentItem` after a successful non-image upload.
  const completedFileChip = {
    localId: "local-1",
    localUri: "file:///private/var/.../IMG_0001.pdf",
    filename: "report.pdf",
    mimeType: "application/pdf",
    status: "completed" as const,
    id: "att-42",
    url: "mc://file/att-42",
    downloadUrl: "/api/attachments/att-42/download",
  };

  it("resolves a server-relative downloadUrl against the API base", () => {
    expect(
      resolveAttachmentUrlWithBase(completedFileChip.downloadUrl, BASE),
    ).toBe("https://api.example.test/api/attachments/att-42/download");
  });

  it("preserves an absolute downloadUrl returned by CloudFront / presign", () => {
    const cloudFront = {
      ...completedFileChip,
      downloadUrl:
        "https://cdn.example.test/att-42.pdf?Signature=s&Key-Pair-Id=k",
    };
    expect(
      resolveAttachmentUrlWithBase(cloudFront.downloadUrl, BASE),
    ).toBe(cloudFront.downloadUrl);
  });

  it("returns null when the upload hasn't populated downloadUrl yet (no Linking call)", () => {
    // Mirrors a `completed` chip that arrived before the server response
    // (defensive; in practice `completed` implies downloadUrl is set).
    const partial = { ...completedFileChip, downloadUrl: undefined };
    expect(resolveAttachmentUrlWithBase(partial.downloadUrl, BASE)).toBeNull();
  });
});

describe("resolveAttachmentUrl (store-bound)", () => {
  it("matches the with-base form for an absolute URL regardless of the active server", () => {
    // For absolute URLs the base is irrelevant — guarantees pass-through
    // stays stable.
    const absolute = "https://cdn.example.test/file.pdf?Signature=s";
    expect(resolveAttachmentUrl(absolute)).toBe(absolute);
  });

  it("resolves a server-relative path against the ACTIVE server's API base", () => {
    // RUYI-4: 这是应用内切换服务器后附件必须跟着走的那条路径 —— 地址在
    // 调用时从 store 现取,不是模块加载期绑死的。
    expect(resolveAttachmentUrl("/api/attachments/att-1/download")).toBe(
      "https://api.example.test/api/attachments/att-1/download",
    );
  });

  it("returns null for empty input", () => {
    expect(resolveAttachmentUrl(undefined)).toBeNull();
    expect(resolveAttachmentUrl(null)).toBeNull();
    expect(resolveAttachmentUrl("")).toBeNull();
  });
});
