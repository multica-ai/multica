import { afterEach, describe, expect, it, vi } from "vitest";
import {
  PendingDownloadAuthorizations,
  sanitizeBearerAuthorization,
  type DownloadRequestDetails,
} from "./download-authorization";

const API_BASE_URL = "https://api.example.test";
const WEB_CONTENTS_ID = 7;
const ATTACHMENT_URL =
  "https://api.example.test/api/attachments/11111111-2222-3333-4444-555555555555/download?workspace_slug=acme";

function request(
  id: number,
  url: string,
  requestHeaders: Record<string, string> = {},
  webContentsId = WEB_CONTENTS_ID,
): DownloadRequestDetails {
  return { id, url, webContentsId, requestHeaders };
}

afterEach(() => {
  vi.useRealTimers();
});

describe("sanitizeBearerAuthorization", () => {
  it("accepts a non-empty bearer header and trims outer whitespace", () => {
    expect(sanitizeBearerAuthorization(" Bearer jwt-token ")).toBe(
      "Bearer jwt-token",
    );
  });

  it("rejects malformed, newline-bearing, and oversized values", () => {
    expect(sanitizeBearerAuthorization(undefined)).toBeNull();
    expect(sanitizeBearerAuthorization("Bearer ")).toBeNull();
    expect(sanitizeBearerAuthorization("Token jwt-token")).toBeNull();
    expect(
      sanitizeBearerAuthorization("Bearer jwt\r\nX-Evil: value"),
    ).toBeNull();
    expect(sanitizeBearerAuthorization("Bearer token\tpart")).toBeNull();
    expect(sanitizeBearerAuthorization("Bearer töken")).toBeNull();
    expect(
      sanitizeBearerAuthorization(`Bearer ${"a".repeat(8_192)}`),
    ).toBeNull();
  });
});

describe("PendingDownloadAuthorizations", () => {
  it("attaches bearer auth once for the exact trusted attachment URL", () => {
    const authorizations = new PendingDownloadAuthorizations();
    const details = request(1, ATTACHMENT_URL);

    expect(
      authorizations.register(
        ATTACHMENT_URL,
        "Bearer jwt-token",
        API_BASE_URL,
        WEB_CONTENTS_ID,
      ),
    ).toBe(true);
    expect(authorizations.apply(details)).toBe("attached");
    expect(details.requestHeaders.Authorization).toBe("Bearer jwt-token");

    const secondRequest = request(2, ATTACHMENT_URL);
    expect(authorizations.apply(secondRequest)).toBe("unchanged");
    expect(secondRequest.requestHeaders.Authorization).toBeUndefined();
    authorizations.clear();
  });

  it("rejects lookalike paths on foreign origins", () => {
    const authorizations = new PendingDownloadAuthorizations();
    const foreignURL =
      "https://evil.example/api/attachments/11111111-2222-3333-4444-555555555555/download";

    expect(
      authorizations.register(
        foreignURL,
        "Bearer jwt-token",
        API_BASE_URL,
        WEB_CONTENTS_ID,
      ),
    ).toBe(false);
    expect(authorizations.apply(request(1, foreignURL))).toBe("unchanged");
    authorizations.clear();
  });

  it("rejects subdomains, port changes, credentials, and malformed ids", () => {
    const authorizations = new PendingDownloadAuthorizations();
    const urls = [
      "https://cdn.api.example.test/api/attachments/11111111-2222-3333-4444-555555555555/download",
      "https://api.example.test:8443/api/attachments/11111111-2222-3333-4444-555555555555/download",
      "https://user@api.example.test/api/attachments/11111111-2222-3333-4444-555555555555/download",
      "https://api.example.test/api/attachments/not-a-uuid/download",
      "https://api.example.test/api/me",
      "file:///api/attachments/11111111-2222-3333-4444-555555555555/download",
    ];

    for (const url of urls) {
      expect(
        authorizations.register(
          url,
          "Bearer jwt-token",
          API_BASE_URL,
          WEB_CONTENTS_ID,
        ),
      ).toBe(false);
    }
    expect(
      authorizations.register(
        ATTACHMENT_URL,
        "Bearer jwt-token",
        API_BASE_URL,
        Number.NaN,
      ),
    ).toBe(false);
    authorizations.clear();
  });

  it("supports an API base URL mounted below the origin root", () => {
    const authorizations = new PendingDownloadAuthorizations();
    const url =
      "https://example.test/multica/api/attachments/11111111-2222-3333-4444-555555555555/download";

    expect(
      authorizations.register(
        url,
        "Bearer jwt-token",
        "https://example.test/multica/",
        WEB_CONTENTS_ID,
      ),
    ).toBe(true);
    const details = request(1, url);
    expect(authorizations.apply(details)).toBe("attached");
    expect(details.requestHeaders.Authorization).toBe("Bearer jwt-token");
    authorizations.clear();
  });

  it("binds pending auth to the source web contents", () => {
    const authorizations = new PendingDownloadAuthorizations();
    expect(
      authorizations.register(
        ATTACHMENT_URL,
        "Bearer jwt-token",
        API_BASE_URL,
        WEB_CONTENTS_ID,
      ),
    ).toBe(true);

    const otherWindow = request(1, ATTACHMENT_URL, {}, 99);
    expect(authorizations.apply(otherWindow)).toBe("unchanged");
    expect(otherWindow.requestHeaders.Authorization).toBeUndefined();

    const sourceWindow = request(2, ATTACHMENT_URL);
    expect(authorizations.apply(sourceWindow)).toBe("attached");
    expect(sourceWindow.requestHeaders.Authorization).toBe("Bearer jwt-token");
    authorizations.clear();
  });

  it("blanks inherited auth on a redirect before it reaches a CDN", () => {
    const authorizations = new PendingDownloadAuthorizations();
    expect(
      authorizations.register(
        ATTACHMENT_URL,
        "Bearer jwt-token",
        API_BASE_URL,
        WEB_CONTENTS_ID,
      ),
    ).toBe(true);

    const initial = request(77, ATTACHMENT_URL);
    expect(authorizations.apply(initial)).toBe("attached");

    const redirect = request(77, "https://cdn.example.test/signed-file", {
      authorization: "Bearer jwt-token",
    });
    expect(authorizations.apply(redirect)).toBe("stripped");
    expect(redirect.requestHeaders.authorization).toBe("");
    expect(Object.values(redirect.requestHeaders)).not.toContain(
      "Bearer jwt-token",
    );

    const secondRedirect = request(77, "https://storage.example.test/file", {
      Authorization: "Bearer jwt-token",
    });
    expect(authorizations.apply(secondRedirect)).toBe("stripped");
    expect(secondRedirect.requestHeaders.Authorization).toBe("");
    authorizations.clear();
  });

  it("expires unused and active authorizations", () => {
    vi.useFakeTimers();
    const authorizations = new PendingDownloadAuthorizations(100);

    expect(
      authorizations.register(
        ATTACHMENT_URL,
        "Bearer jwt-token",
        API_BASE_URL,
        WEB_CONTENTS_ID,
      ),
    ).toBe(true);
    vi.advanceTimersByTime(101);
    expect(authorizations.apply(request(1, ATTACHMENT_URL))).toBe("unchanged");

    expect(
      authorizations.register(
        ATTACHMENT_URL,
        "Bearer jwt-token",
        API_BASE_URL,
        WEB_CONTENTS_ID,
      ),
    ).toBe(true);
    expect(authorizations.apply(request(2, ATTACHMENT_URL))).toBe("attached");
    vi.advanceTimersByTime(101);
    const later = request(2, "https://cdn.example.test/signed-file", {
      Authorization: "Bearer unrelated",
    });
    expect(authorizations.apply(later)).toBe("unchanged");
    expect(later.requestHeaders.Authorization).toBe("Bearer unrelated");
    authorizations.clear();
  });

  it("finishes active request tracking without changing unrelated headers", () => {
    const authorizations = new PendingDownloadAuthorizations();
    expect(
      authorizations.register(
        ATTACHMENT_URL,
        "Bearer jwt-token",
        API_BASE_URL,
        WEB_CONTENTS_ID,
      ),
    ).toBe(true);
    expect(authorizations.apply(request(42, ATTACHMENT_URL))).toBe("attached");
    authorizations.finish(42);

    const unrelated = request(42, "https://api.example.test/api/me", {
      Authorization: "Bearer normal-api-request",
    });
    expect(authorizations.apply(unrelated)).toBe("unchanged");
    expect(unrelated.requestHeaders.Authorization).toBe(
      "Bearer normal-api-request",
    );
    authorizations.clear();
  });
});
