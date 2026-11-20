// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseIssueDeepLink } from "./issue-deep-link";

const link = (url: string) => `multica://open/issue?url=${encodeURIComponent(url)}`;

describe("parseIssueDeepLink", () => {
  it("keeps the workspace, query and comment on a self-hosted instance", () => {
    expect(parseIssueDeepLink(link("https://tasks.example/acme/issues/ACM-42?view=activity#comment-c1"), "https://tasks.example")).toEqual({
      kind: "issue",
      workspaceSlug: "acme",
      path: "/acme/issues/ACM-42?view=activity#comment-c1",
    });
  });

  it("accepts UUID issue links and an app URL with a base path", () => {
    const path = "/acme/issues/01a1b5e5-e4c5-78d1-ab0e-9c6e708a9939";
    expect(parseIssueDeepLink(link(`http://localhost:3000/multica${path}`), "http://localhost:3000/multica/")).toEqual({
      kind: "issue", workspaceSlug: "acme", path,
    });
  });

  it.each([
    "http://tasks.example/acme/issues/ACM-42",
    "https://tasks.example:8443/acme/issues/ACM-42",
    "https://tasks.example.evil.test/acme/issues/ACM-42",
    "https://other.example/acme/issues/ACM-42",
  ])("rejects a different instance: %s", (url) => {
    expect(parseIssueDeepLink(link(url), "https://tasks.example")).toEqual({ kind: "other-instance" });
  });

  it("does not mistake another deployment on the same origin for the configured base path", () => {
    expect(parseIssueDeepLink(link("https://tasks.example/app-two/acme/issues/ACM-42"), "https://tasks.example/app")).toEqual({ kind: "other-instance" });
  });

  it.each([
    "multica://open/issue",
    "https://open/issue?url=https://tasks.example/acme/issues/ACM-42",
    "multica://auth/callback?token=secret",
    link("javascript:alert(1)"),
    link("file:///acme/issues/ACM-42"),
    link("https://user:secret@tasks.example/acme/issues/ACM-42"),
    link("https://tasks.example/acme/settings"),
    link("https://tasks.example/acme/issues/ACM-42/extra"),
    link("https://tasks.example/acme%2Fevil/issues/ACM-42"),
    "invalid",
  ])("ignores malformed or non-issue links: %s", (url) => {
    expect(parseIssueDeepLink(url, "https://tasks.example")).toBeNull();
  });
});
