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

const chatLink = (url: string) => `multica://open/chat?url=${encodeURIComponent(url)}`;

describe("parseIssueDeepLink for chats", () => {
  it("opens the chat session named in the link", () => {
    expect(parseIssueDeepLink(chatLink("https://tasks.example/acme/chat?session=01a1f1b6-b59b-7a48-beae-5a459ff72200"), "https://tasks.example")).toEqual({
      kind: "chat",
      workspaceSlug: "acme",
      path: "/acme/chat?session=01a1f1b6-b59b-7a48-beae-5a459ff72200",
    });
  });

  it("opens the chat page without a session, behind a base path", () => {
    expect(parseIssueDeepLink(chatLink("http://localhost:3000/multica/acme/chat"), "http://localhost:3000/multica/")).toEqual({
      kind: "chat", workspaceSlug: "acme", path: "/acme/chat",
    });
  });

  it("keeps only the session from the chat address", () => {
    expect(parseIssueDeepLink(chatLink("https://tasks.example/acme/chat?session=s-1&agent=a-1#x"), "https://tasks.example")).toEqual({
      kind: "chat", workspaceSlug: "acme", path: "/acme/chat?session=s-1",
    });
  });

  it("rejects a chat on a different instance", () => {
    expect(parseIssueDeepLink(chatLink("https://other.example/acme/chat?session=s-1"), "https://tasks.example")).toEqual({ kind: "other-instance" });
  });

  it.each([
    chatLink("https://tasks.example/acme/issues/ACM-42"),
    link("https://tasks.example/acme/chat?session=s-1"),
    chatLink("https://tasks.example/acme/chat/extra"),
    chatLink("https://tasks.example/acme%2Fevil/chat"),
    chatLink("https://tasks.example/acme/chat?session=../settings"),
    chatLink("https://tasks.example/acme/chat?session="),
    "multica://open/settings?url=https%3A%2F%2Ftasks.example%2Facme%2Fchat",
  ])("ignores a link whose kind and address do not match: %s", (url) => {
    expect(parseIssueDeepLink(url, "https://tasks.example")).toBeNull();
  });
});
