import { describe, expect, it } from "vitest";
import {
  PendingDownloadAuthorizations,
  sanitizeBearerAuthorization,
} from "./download-authorization";

const ATTACHMENT_URL =
  "https://api.example.test/api/attachments/11111111-2222-3333-4444-555555555555/download?workspace_slug=acme";

describe("sanitizeBearerAuthorization", () => {
  it("accepts non-empty bearer headers and trims whitespace", () => {
    expect(sanitizeBearerAuthorization(" Bearer jwt-token ")).toBe(
      "Bearer jwt-token",
    );
  });

  it("rejects missing, empty, and newline-bearing values", () => {
    expect(sanitizeBearerAuthorization(undefined)).toBeNull();
    expect(sanitizeBearerAuthorization("Bearer ")).toBeNull();
    expect(sanitizeBearerAuthorization("Token jwt-token")).toBeNull();
    expect(sanitizeBearerAuthorization("Bearer jwt\r\nX-Evil: 1")).toBeNull();
  });
});

describe("PendingDownloadAuthorizations", () => {
  it("consumes auth once for the exact stable attachment download URL", () => {
    const pending = new PendingDownloadAuthorizations();

    expect(pending.register(ATTACHMENT_URL, "Bearer jwt-token")).toBe(true);
    expect(pending.consume(ATTACHMENT_URL)).toBe("Bearer jwt-token");
    expect(pending.consume(ATTACHMENT_URL)).toBeNull();
  });

  it("ignores non-attachment and non-http URLs", () => {
    const pending = new PendingDownloadAuthorizations();

    expect(
      pending.register("https://api.example.test/api/me", "Bearer jwt-token"),
    ).toBe(false);
    expect(
      pending.register(
        "file:///api/attachments/11111111-2222-3333-4444-555555555555/download",
        "Bearer jwt-token",
      ),
    ).toBe(false);
  });

  it("ignores malformed attachment ids", () => {
    const pending = new PendingDownloadAuthorizations();

    expect(
      pending.register(
        "https://api.example.test/api/attachments/not-a-uuid/download",
        "Bearer jwt-token",
      ),
    ).toBe(false);
  });
});
