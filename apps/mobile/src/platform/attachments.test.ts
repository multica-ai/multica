import { describe, expect, it } from "vitest";
import { attachmentOpenUrl, imageRequest } from "./attachments";

const API = "https://api.multica.ai";
const ID = "0198f0a2-1b2c-7d3e-8f40-123456789abc";

describe("imageRequest", () => {
  it("sends attachment links to this server with the token, whatever host they name", () => {
    expect(imageRequest(`https://multica.ai/api/attachments/${ID}/download`, API, "t")).toEqual({
      uri: `${API}/api/attachments/${ID}/download`,
      headers: { Authorization: "Bearer t" },
    });
    expect(imageRequest(`/api/attachments/${ID}/download`, API, null)).toEqual({ uri: `${API}/api/attachments/${ID}/download`, headers: undefined });
  });

  it("keeps the token off other hosts", () => {
    expect(imageRequest("https://cdn.example.com/a.png", API, "t")).toEqual({ uri: "https://cdn.example.com/a.png" });
    expect(imageRequest(`${API}/uploads/a.png`, API, "t")).toEqual({ uri: `${API}/uploads/a.png`, headers: { Authorization: "Bearer t" } });
  });

  it("refuses what it cannot load", () => {
    expect(imageRequest("data:image/png;base64,AAAA", API, "t")).toBeNull();
    expect(imageRequest("//evil.example/a.png", API, "t")).toBeNull();
  });
});

describe("attachmentOpenUrl", () => {
  it("prefers a signed storage URL", () => {
    expect(attachmentOpenUrl({ download_url: "https://bucket.s3.amazonaws.com/a?sig=1", attachment_download_url: "/x" }, API)).toBe("https://bucket.s3.amazonaws.com/a?sig=1");
  });

  it("falls back to the credential-free route", () => {
    expect(attachmentOpenUrl({ download_url: `/api/attachments/${ID}/download`, attachment_download_url: `/api/attachments/${ID}/signed-download?s=1` }, API)).toBe(
      `${API}/api/attachments/${ID}/signed-download?s=1`,
    );
    expect(attachmentOpenUrl({ download_url: `/api/attachments/${ID}/download` }, API)).toBeNull();
  });
});
