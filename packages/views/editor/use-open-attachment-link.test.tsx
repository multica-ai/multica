import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import type { Attachment } from "@multica/core/types";

const { getAttachmentForDownload, openAt, tryOpen, download, toastError } = vi.hoisted(() => ({
  getAttachmentForDownload: vi.fn(),
  openAt: vi.fn(() => false),
  tryOpen: vi.fn(() => true),
  download: vi.fn(),
  toastError: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({
  api: { getBaseUrl: () => "https://api.example", getAttachmentForDownload },
}));
vi.mock("@multica/core/config", () => ({
  useConfigStore: (select: (s: { attachmentPublicUrl: string; cdnDomain: string }) => unknown) =>
    select({ attachmentPublicUrl: "https://files.example", cdnDomain: "static.example" }),
}));
vi.mock("../navigation", () => ({ useAppOrigin: () => "https://app.example" }));
vi.mock("../i18n", () => ({ useT: () => ({ t: () => "Download failed" }) }));
vi.mock("sonner", () => ({ toast: { error: toastError } }));
vi.mock("./preview-sequence-context", () => ({ usePreviewSequence: () => ({ openAt }) }));
vi.mock("./attachment-preview-modal", () => ({
  useAttachmentPreview: () => ({ open: vi.fn(), tryOpen, modal: null }),
}));
vi.mock("./use-download-attachment", () => ({ useDownloadAttachment: () => download }));

import { resolveAttachmentLinkId, useOpenAttachmentLink } from "./use-open-attachment-link";

const ID = "01a13179-08aa-7898-ae86-3a1994d2147e";
const WS = "0e0e0e0e-0000-4000-8000-000000000000";

const CTX = {
  appOrigin: "https://app.example",
  attachmentPublicUrl: "https://files.example",
  cdnDomain: "static.example",
};

function record(overrides: Partial<Attachment> = {}): Attachment {
  return {
    id: ID,
    filename: "demo.mp4",
    content_type: "video/mp4",
    url: `https://legacy-bucket.s3.amazonaws.com/${WS}/demo.mp4`,
    download_url: "",
    size_bytes: 1,
    ...overrides,
  } as Attachment;
}

describe("resolveAttachmentLinkId", () => {
  it.each([
    ["a site-relative download URL", `/api/attachments/${ID}/download`],
    ["a download URL on the app origin", `https://app.example/api/attachments/${ID}/download`],
    ["a download URL on the public attachment origin", `https://files.example/api/attachments/${ID}/download`],
    ["a download URL on the API origin", `https://api.example/api/attachments/${ID}/download`],
    ["a storage URL on the CDN", `https://static.example/workspaces/${WS}/${ID}.mp4`],
    ["a signed storage URL on the CDN", `https://static.example/workspaces/${WS}/${ID}.mp4?Signature=x&Key-Pair-Id=k`],
    ["a local upload URL", `/uploads/workspaces/${WS}/${ID}.mp4`],
  ])("resolves %s", (_label, href) => {
    expect(resolveAttachmentLinkId(href, CTX)).toBe(ID);
  });

  it.each([
    ["another deployment's download URL", `https://other.example/api/attachments/${ID}/download`],
    ["a credentialed download URL", `https://user:pw@app.example/api/attachments/${ID}/download`],
    ["a storage path on another host", `https://other.example/workspaces/${WS}/${ID}.mp4`],
    ["a non-attachment path on the CDN", "https://static.example/marketing/launch.mp4"],
    ["an external link", "https://github.com/multica-ai/multica"],
    ["an empty href", ""],
  ])("leaves %s alone", (_label, href) => {
    expect(resolveAttachmentLinkId(href, CTX)).toBeUndefined();
  });

  it("matches a legacy storage URL against the surface's records", () => {
    const att = record();
    expect(resolveAttachmentLinkId(att.url, { ...CTX, attachments: [att] })).toBe(ID);
    expect(resolveAttachmentLinkId(att.url, CTX)).toBeUndefined();
  });
});

describe("useOpenAttachmentLink", () => {
  const cdnLink = `https://static.example/workspaces/${WS}/${ID}.mp4`;

  beforeEach(() => {
    getAttachmentForDownload.mockReset();
    openAt.mockReset().mockReturnValue(false);
    tryOpen.mockReset().mockReturnValue(true);
    download.mockReset();
    toastError.mockReset();
  });

  it("resolves a legacy storage URL against records the caller passes in", async () => {
    const att = record();
    getAttachmentForDownload.mockResolvedValue(att);
    const { result } = renderHook(() => useOpenAttachmentLink([att]));

    await act(async () => {
      expect(result.current.open(att.url)).toBe(true);
    });

    expect(getAttachmentForDownload).toHaveBeenCalledWith(ID);
  });

  it("does nothing for a regular link", () => {
    const { result } = renderHook(() => useOpenAttachmentLink());
    expect(result.current.open("https://github.com/multica-ai/multica")).toBe(false);
    expect(openAt).not.toHaveBeenCalled();
    expect(getAttachmentForDownload).not.toHaveBeenCalled();
  });

  it("opens the file at its place in the surface's viewer, pinned to it", async () => {
    openAt.mockReturnValue(true);
    const { result } = renderHook(() => useOpenAttachmentLink());

    await act(async () => {
      expect(result.current.open(cdnLink)).toBe(true);
    });

    expect(openAt).toHaveBeenCalledWith(ID, { stayOnError: true });
    expect(getAttachmentForDownload).not.toHaveBeenCalled();
  });

  it("opens a file outside the surface in the standalone viewer with fresh metadata", async () => {
    const fresh = record({ download_url: `${cdnLink}?Signature=fresh` });
    getAttachmentForDownload.mockResolvedValue(fresh);
    const { result } = renderHook(() => useOpenAttachmentLink());

    await act(async () => {
      result.current.open(cdnLink);
    });

    expect(tryOpen).toHaveBeenCalledWith({ kind: "full", attachment: fresh });
    expect(download).not.toHaveBeenCalled();
  });

  it("downloads a file the viewer cannot show", async () => {
    getAttachmentForDownload.mockResolvedValue(
      record({ filename: "bundle.zip", content_type: "application/zip" }),
    );
    tryOpen.mockReturnValue(false);
    const { result } = renderHook(() => useOpenAttachmentLink());

    await act(async () => {
      result.current.open(cdnLink);
    });

    expect(download).toHaveBeenCalledWith(ID);
  });

  it("reports a failed lookup", async () => {
    getAttachmentForDownload.mockRejectedValue(new Error("403"));
    const { result } = renderHook(() => useOpenAttachmentLink());

    await act(async () => {
      result.current.open(cdnLink);
    });

    expect(toastError).toHaveBeenCalledWith("Download failed");
    expect(tryOpen).not.toHaveBeenCalled();
    expect(download).not.toHaveBeenCalled();
  });
});
