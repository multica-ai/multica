// @vitest-environment node
import { describe, expect, it } from "vitest";
import { expandLinkUrl, validateLinkUrl } from "./url";

describe("link URLs", () => {
  it("encodes substituted path and query values", () => {
    expect(
      expandLinkUrl("https://example.com/{workspace_slug}?ws={workspace_id}&p={project_id}", {
        workspaceSlug: "a/b",
        workspaceId: "a&b",
        projectId: "a b",
      }),
    ).toBe("https://example.com/a%2Fb?ws=a%26b&p=a%20b");
    expect(() =>
      expandLinkUrl("https://example.com/{project_id}", {
        workspaceSlug: "ws",
        workspaceId: "id",
      }),
    ).toThrow("Unresolved");
  });

  it.each([
    "http://example.com",
    "javascript:alert(1)",
    "data:text/html,x",
    "/docs",
    "//host/x",
    "https:\\host",
    String.raw`https:\\host`,
    "https://example.com/\tx",
    "https://example.com/\nx",
    "https://@host/",
    "https://:443/",
    "https://example.com/{unknown}",
    "https://example.com/{project_id}",
    "https://{workspace_slug}.example.com",
    "https://user:pass@example.com",
    "https://example.com/#{workspace_id}",
    "https://example.com/" + "a".repeat(2048),
  ])("rejects %s", (url) => expect(validateLinkUrl(url)).not.toBeNull());

  it("accepts allowed variables in path and query", () => {
    expect(validateLinkUrl("https://example.com/{workspace_slug}?ws={workspace_id}")).toBeNull();
    expect(validateLinkUrl("https://example.com/{project_id}", true)).toBeNull();
  });

  it("accepts encoded braces without treating them as template variables", () => {
    const template = "https://example.com/%7Bworkspace_slug%7D";
    expect(validateLinkUrl(template)).toBeNull();
    expect(expandLinkUrl(template, { workspaceSlug: "acme", workspaceId: "workspace" })).toBe(template);
  });
});
