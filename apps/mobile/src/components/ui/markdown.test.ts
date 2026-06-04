import { describe, expect, it } from "vitest";
import {
  fillTableColumnWidths,
  getIssueMentionId,
  parseMobileIssueLink,
  parseFileCardLine,
  parseMobileFileCardHtml,
  preprocessMobileMarkdown,
  resolveMobileFileCardUrl,
} from "./markdown-utils";
import { buildMobileIssueWebHref } from "../../navigation/issue-links";

describe("mobile markdown table preview widths", () => {
  it("expands narrow columns to fill the viewport", () => {
    expect(fillTableColumnWidths({ maxWidth: 200, viewportWidth: 300, widths: [60, 90] })).toEqual([
      135,
      165,
    ]);
  });

  it("does not shrink tables that are already wider than the viewport", () => {
    expect(fillTableColumnWidths({ maxWidth: 300, viewportWidth: 300, widths: [180, 180] })).toEqual([
      180,
      180,
    ]);
  });

  it("respects max column width while expanding", () => {
    expect(fillTableColumnWidths({ maxWidth: 120, viewportWidth: 300, widths: [100, 80] })).toEqual([
      120,
      120,
    ]);
  });

  it("handles empty columns and invalid viewport widths", () => {
    expect(fillTableColumnWidths({ maxWidth: 200, viewportWidth: 300, widths: [] })).toEqual([]);
    expect(fillTableColumnWidths({ maxWidth: 200, viewportWidth: 0, widths: [80, 80] })).toEqual([
      80,
      80,
    ]);
  });
});

describe("mobile markdown preprocessing", () => {
  it("recognizes upload file cards", () => {
    expect(parseFileCardLine("!file[report.pdf](/uploads/report.pdf)")).toEqual({
      filename: "report.pdf",
      href: "/uploads/report.pdf",
    });
  });

  it("recognizes absolute http file cards", () => {
    expect(parseFileCardLine("!file[report.pdf](https://cdn.example.com/report.pdf)")).toEqual({
      filename: "report.pdf",
      href: "https://cdn.example.com/report.pdf",
    });
  });

  it.each([
    "javascript:alert(1)",
    "data:text/html,xss",
    "//evil.com/x",
    "/api/x",
  ])("does not create a clickable file card for %s", (href) => {
    expect(parseFileCardLine(`!file[evil.txt](${href})`)).toBeNull();
    expect(preprocessMobileMarkdown(`!file[evil.txt](${href})`)).toBe(`!file[evil.txt](${href})`);
    expect(preprocessMobileMarkdown(`before !file[evil.txt](${href}) after`)).toBe(
      `before !file[evil.txt](${href}) after`,
    );
  });

  it("preprocesses inline file syntax into an internal markdown link", () => {
    const result = preprocessMobileMarkdown("Mac: 执行!file[Multica.dmg](/uploads/Multica.dmg) 后继续");

    expect(result).toBe('Mac: 执行[Multica.dmg](/uploads/Multica.dmg "multica-file") 后继续');
    expect(result).not.toContain("!file");
  });

  it("preprocesses absolute inline file links", () => {
    expect(
      preprocessMobileMarkdown(
        "before !file[report.pdf](https://cdn.example.com/report.pdf) after",
      ),
    ).toBe('before [report.pdf](https://cdn.example.com/report.pdf "multica-file") after');
  });

  it("preprocesses file-card syntax into mobile file-card html", () => {
    const html = preprocessMobileMarkdown("before\n!file[report.pdf](/uploads/report.pdf)\nafter");

    expect(html).toBe(
      'before\n<div data-type="fileCard" data-href="/uploads/report.pdf" data-filename="report.pdf"></div>\nafter',
    );
    expect(parseMobileFileCardHtml(html.split("\n")[1] ?? "")).toEqual({
      filename: "report.pdf",
      href: "/uploads/report.pdf",
    });
  });

  it("resolves relative upload file cards against the mobile API base URL", () => {
    expect(resolveMobileFileCardUrl("/uploads/report.pdf", "https://api.example.com/")).toBe(
      "https://api.example.com/uploads/report.pdf",
    );
    expect(resolveMobileFileCardUrl("https://cdn.example.com/report.pdf", "https://api.example.com")).toBe(
      "https://cdn.example.com/report.pdf",
    );
    expect(resolveMobileFileCardUrl("/api/report.pdf", "https://api.example.com")).toBeNull();
  });

  it("extracts issue mention ids for renderer navigation", () => {
    expect(getIssueMentionId("mention://issue/issue-1")).toBe("issue-1");
    expect(getIssueMentionId("mention://member/user-1")).toBeNull();
  });

  it("parses trusted Multica issue links for native navigation", () => {
    expect(
      parseMobileIssueLink(
        "https://multica.wujieai.com/openharness/issues/OPE-744",
        ["https://multica.wujieai.com"],
      ),
    ).toEqual({
      workspaceSlug: "openharness",
      issueId: "OPE-744",
      commentId: undefined,
    });
  });

  it("parses trusted issue links with comment deep-link params", () => {
    expect(
      parseMobileIssueLink(
        "https://multica.wujieai.com/openharness/issues/OPE-744?comment=27a21862-583c-4680-a736-ae29f97f5e38",
        ["https://multica.wujieai.com"],
      ),
    ).toEqual({
      workspaceSlug: "openharness",
      issueId: "OPE-744",
      commentId: "27a21862-583c-4680-a736-ae29f97f5e38",
    });
  });

  it("parses workspace-aware mobile scheme issue links", () => {
    expect(
      parseMobileIssueLink(
        "wujieai-multicam://openharness/issues/OPE-744?comment=27a21862-583c-4680-a736-ae29f97f5e38",
        ["https://multica.wujieai.com"],
      ),
    ).toEqual({
      workspaceSlug: "openharness",
      issueId: "OPE-744",
      commentId: "27a21862-583c-4680-a736-ae29f97f5e38",
    });
  });

  it("builds mobile issue web links with encoded comment params", () => {
    expect(
      buildMobileIssueWebHref({
        baseUrl: "https://multica.wujieai.com/",
        workspaceSlug: "open harness",
        issueId: "OPE-744",
        commentId: "comment id",
      }),
    ).toBe("https://multica.wujieai.com/open%20harness/issues/OPE-744?comment=comment%20id");
  });

  it.each([
    "https://example.com/openharness/issues/OPE-744",
    "https://multica.wujieai.com/issues/OPE-744",
    "https://multica.wujieai.com/openharness/issues/OPE-744/properties",
    "wujieai-multicam://issues/issue-1?commentId=comment-1",
    "multica://openharness/issues/OPE-744",
  ])("does not parse untrusted or unsupported issue links: %s", (href) => {
    expect(parseMobileIssueLink(href, ["https://multica.wujieai.com"])).toBeNull();
  });

  it("leaves file syntax inside fenced and inline code unchanged", () => {
    const fenced = "```\n!file[report.pdf](/uploads/report.pdf)\n```";
    const inline = "before `!file[report.pdf](/uploads/report.pdf)` after";

    expect(preprocessMobileMarkdown(fenced)).toBe(fenced);
    expect(preprocessMobileMarkdown(inline)).toBe(inline);
  });

  it("separates standalone markdown images from adjacent text lines", () => {
    expect(preprocessMobileMarkdown("before\n![screenshot](/uploads/screenshot.png)\nafter")).toBe(
      "before\n\n![screenshot](/uploads/screenshot.png)\n\nafter",
    );
  });

  it("does not add extra spacing around already separated markdown images", () => {
    const content = "before\n\n![screenshot](/uploads/screenshot.png)\n\nafter";

    expect(preprocessMobileMarkdown(content)).toBe(content);
  });

  it("leaves inline markdown images unchanged", () => {
    const content = "before ![screenshot](/uploads/screenshot.png) after";

    expect(preprocessMobileMarkdown(content)).toBe(content);
  });

  it("leaves markdown image syntax inside fenced code unchanged", () => {
    const content = "```\n![screenshot](/uploads/screenshot.png)\n```";

    expect(preprocessMobileMarkdown(content)).toBe(content);
  });

  it("handles long pasted URLs without recursive parsing", () => {
    const url = `https://example.com/${"a".repeat(5000)}?q=${"b".repeat(5000)}`;

    expect(preprocessMobileMarkdown(url)).toBe(url);
  });
});
