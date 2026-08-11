import { describe, expect, it } from "vitest";
import { parseDeepLink } from "./deep-link";

describe("parseDeepLink", () => {
  it("parses the auth callback token", () => {
    expect(parseDeepLink("multica://auth/callback?token=jwt-123")).toEqual({
      kind: "auth-token",
      token: "jwt-123",
    });
  });

  it("rejects an auth callback with no token", () => {
    expect(parseDeepLink("multica://auth/callback")).toBeNull();
  });

  it("parses an invitation id", () => {
    expect(parseDeepLink("multica://invite/inv-42")).toEqual({
      kind: "invite",
      invitationId: "inv-42",
    });
  });

  it("parses a workspace issue link", () => {
    expect(parseDeepLink("multica://acme/issues/MUL-123")).toEqual({
      kind: "issue",
      slug: "acme",
      issueId: "MUL-123",
    });
  });

  it("accepts hyphenated and numeric slugs", () => {
    expect(parseDeepLink("multica://acme-web-2/issues/abc")).toMatchObject({
      slug: "acme-web-2",
    });
  });

  it("folds host case so an uppercased link still resolves", () => {
    expect(parseDeepLink("multica://Acme/issues/MUL-1")).toMatchObject({
      kind: "issue",
      slug: "acme",
    });
  });

  it("decodes a percent-encoded issue id", () => {
    expect(parseDeepLink("multica://acme/issues/MUL%20123")).toMatchObject({
      issueId: "MUL 123",
    });
  });

  it("ignores extra query params on an issue link", () => {
    expect(parseDeepLink("multica://acme/issues/MUL-1?comment=c1")).toEqual({
      kind: "issue",
      slug: "acme",
      issueId: "MUL-1",
    });
  });

  it("returns null for an issue link with no id", () => {
    expect(parseDeepLink("multica://acme/issues")).toBeNull();
    expect(parseDeepLink("multica://acme/issues/")).toBeNull();
  });

  it("returns null for a workspace route we do not handle yet", () => {
    expect(parseDeepLink("multica://acme/projects/p-1")).toBeNull();
    expect(parseDeepLink("multica://acme/issues/MUL-1/comments")).toBeNull();
  });

  it("returns null for a host that cannot be a workspace slug", () => {
    expect(parseDeepLink("multica://Not_A_Slug/issues/1")).toBeNull();
    expect(parseDeepLink("multica://-acme/issues/1")).toBeNull();
  });

  it("does not let path traversal escape the issues route", () => {
    expect(parseDeepLink("multica://acme/issues/../../evil")).toBeNull();
  });

  it("returns null for another scheme or a malformed URL", () => {
    expect(parseDeepLink("https://acme/issues/MUL-1")).toBeNull();
    expect(parseDeepLink("not a url")).toBeNull();
    expect(parseDeepLink("multica://acme/issues/%E0%A4%A")).toBeNull();
  });
});
