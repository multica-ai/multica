import { describe, expect, it } from "vitest";
import { parseDeepLink } from "./deep-link";

const WEB = "https://multica.ai";
const open = (url: string, scheme = "multica") =>
  `${scheme}://open/issue?url=${encodeURIComponent(url)}`;

describe("parseDeepLink", () => {
  it("opens an issue from the web app's open-in-app link", () => {
    expect(parseDeepLink(open("https://multica.ai/acme/issues/MUL-12"), WEB, "multica")).toEqual({
      kind: "open",
      workspaceSlug: "acme",
      subject: { kind: "issue", id: "MUL-12" },
    });
  });

  it("opens a plain web URL", () => {
    expect(parseDeepLink("https://multica.ai/acme/inbox?issue=abc", WEB, "multica")).toEqual({
      kind: "open",
      workspaceSlug: "acme",
      subject: { kind: "inbox", selectedKey: "abc", archived: false },
    });
  });

  it("only answers to this build's own scheme", () => {
    const link = open("https://multica.ai/acme/issues/MUL-12", "multica-staging");
    expect(parseDeepLink(link, WEB, "multica")).toBeNull();
    expect(parseDeepLink(link, WEB, "multica-staging")?.kind).toBe("open");
  });

  it("refuses links into another server", () => {
    expect(parseDeepLink(open("https://evil.example/acme/issues/MUL-12"), WEB, "multica")).toEqual({
      kind: "other-instance",
    });
  });

  it("respects a web app served under a base path", () => {
    const web = "https://example.com/multica";
    expect(parseDeepLink("https://example.com/multica/acme/issues/1", web, "multica")?.kind).toBe(
      "open",
    );
    expect(parseDeepLink("https://example.com/other/acme/issues/1", web, "multica")).toEqual({
      kind: "other-instance",
    });
  });

  it("rejects malformed, credentialed, or non-workspace links", () => {
    expect(parseDeepLink("not a url", WEB, "multica")).toBeNull();
    expect(parseDeepLink("multica://open/project?url=x", WEB, "multica")).toBeNull();
    expect(parseDeepLink("https://user:pw@multica.ai/acme/issues/1", WEB, "multica")).toBeNull();
    expect(parseDeepLink("https://multica.ai/login", WEB, "multica")).toBeNull();
    expect(parseDeepLink("https://multica.ai/acme/nonsense", WEB, "multica")).toBeNull();
  });
});
