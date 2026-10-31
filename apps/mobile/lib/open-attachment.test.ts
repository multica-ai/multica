/**
 * The 401 this guards against: a list response's `download_url` is the
 * auth-gated `/api/attachments/{id}/download` path, and a bare
 * `Linking.openURL` sends no credential. `openAttachment` must exchange it
 * for the freshly signed URL from the metadata endpoint before opening.
 */
import { beforeEach, describe, expect, it, vi } from "vitest";

const linkingOpenURL = vi.fn(async () => true);
const alertSpy = vi.fn();
const getAttachmentForDownload = vi.fn();

vi.mock("react-native", () => ({
  Linking: { openURL: linkingOpenURL },
  Alert: { alert: alertSpy },
}));
vi.mock("@/data/api", () => ({
  api: { getAttachmentForDownload: (...args: unknown[]) => getAttachmentForDownload(...args) },
}));
vi.mock("@/lib/i18n/singleton", () => ({ i18n: { t: (key: string) => key } }));

vi.hoisted(() => {
  process.env.EXPO_PUBLIC_API_URL = "https://api.example.test";
});

const { openAttachment } = await import("./open-attachment");
const { EMPTY_ATTACHMENT } = await import("@/data/schemas");

const ATT_ID = "11111111-2222-3333-4444-555555555555";

function freshRecord(downloadUrl: string) {
  return { id: ATT_ID, download_url: downloadUrl };
}

describe("openAttachment", () => {
  beforeEach(() => {
    linkingOpenURL.mockClear();
    alertSpy.mockClear();
    getAttachmentForDownload.mockReset();
  });

  it("re-signs an auth-gated download path before opening it", async () => {
    getAttachmentForDownload.mockResolvedValue(
      freshRecord("/api/attachments/" + ATT_ID + "/signed-download?exp=1&sig=abc"),
    );

    await openAttachment(`/api/attachments/${ATT_ID}/download`);

    expect(getAttachmentForDownload).toHaveBeenCalledWith(ATT_ID);
    expect(linkingOpenURL).toHaveBeenCalledWith(
      `https://api.example.test/api/attachments/${ATT_ID}/signed-download?exp=1&sig=abc`,
    );
  });

  it("passes a CloudFront / presigned URL through without a metadata round-trip", async () => {
    const signed = "https://cdn.example.test/a.pdf?Signature=s";

    await openAttachment(signed);

    expect(getAttachmentForDownload).not.toHaveBeenCalled();
    expect(linkingOpenURL).toHaveBeenCalledWith(signed);
  });

  it.each([
    ["the metadata request fails", () => getAttachmentForDownload.mockRejectedValue(new Error("404"))],
    ["the response parses to the empty fallback", () => getAttachmentForDownload.mockResolvedValue(EMPTY_ATTACHMENT)],
    ["the fresh record has no download_url", () => getAttachmentForDownload.mockResolvedValue(freshRecord(""))],
  ])("shows the in-app error and opens nothing when %s", async (_case, arrange) => {
    arrange();

    await openAttachment(`/api/attachments/${ATT_ID}/download`);

    expect(alertSpy).toHaveBeenCalledWith("common:attachments.open_failed");
    expect(linkingOpenURL).not.toHaveBeenCalled();
  });

  it("does nothing without a URL", async () => {
    await openAttachment(null);
    await openAttachment("");

    expect(getAttachmentForDownload).not.toHaveBeenCalled();
    expect(linkingOpenURL).not.toHaveBeenCalled();
  });

  it("reports a failure to open instead of throwing", async () => {
    linkingOpenURL.mockRejectedValueOnce(new Error("no handler"));

    await expect(openAttachment("https://example.test/a.pdf")).resolves.toBeUndefined();
    expect(alertSpy).toHaveBeenCalledWith("common:attachments.open_failed");
  });
});
