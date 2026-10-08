// @vitest-environment jsdom
/**
 * MUL-6222 — the web half of the tab-title contract.
 *
 * What a URL is *named* is resolved by `@multica/core/paths` +
 * `useTabPresentation` and tested there; mocked out here so these cover only
 * the platform wiring web owns: the site suffix, the unknown-route and
 * navigation behavior of `document.title`.
 */
import { describe, expect, it, vi, beforeEach } from "vitest";
import { render } from "@testing-library/react";

const route = vi.hoisted(() => ({ pathname: "/acme/issues", search: "" }));
vi.mock("next/navigation", () => ({
  usePathname: () => route.pathname,
  useSearchParams: () => new URLSearchParams(route.search),
}));

const presentation = vi.hoisted(() => ({
  title: "Issues",
  urls: [] as string[],
}));
vi.mock("@multica/views/layout", () => ({
  useTabPresentation: (url: string) => {
    presentation.urls.push(url);
    return { title: presentation.title, visual: { kind: "icon", icon: "ListTodo" } };
  },
}));

import {
  MAX_PAGE_TITLE_LENGTH,
  SITE_TITLE,
  formatDocumentTitle,
} from "./document-title";
import { WorkspaceDocumentTitle } from "./workspace-document-title";

function open(pathname: string, search = "") {
  route.pathname = pathname;
  route.search = search;
}

beforeEach(() => {
  presentation.urls = [];
  presentation.title = "Issues";
  document.title = SITE_TITLE;
});

describe("formatDocumentTitle", () => {
  it("puts the page name in front of the product name", () => {
    expect(formatDocumentTitle("MUL-123: Fix login")).toBe(
      "MUL-123: Fix login | Multica",
    );
  });

  it("falls back to the site title rather than a bare suffix", () => {
    expect(formatDocumentTitle("")).toBe(SITE_TITLE);
    expect(formatDocumentTitle("   ")).toBe(SITE_TITLE);
    expect(formatDocumentTitle(null)).toBe(SITE_TITLE);
    expect(formatDocumentTitle(undefined)).toBe(SITE_TITLE);
  });

  it("clips an over-long title and keeps the identifying prefix", () => {
    const title = `MUL-123: ${"long ".repeat(60)}`;
    const formatted = formatDocumentTitle(title);

    expect(formatted.startsWith("MUL-123: long")).toBe(true);
    expect(formatted.endsWith("… | Multica")).toBe(true);
    // Ellipsis replaces the clipped remainder, and no trailing space survives.
    expect(formatted).not.toContain(" … ");
  });

  it("clips on code points so an emoji is never cut in half", () => {
    const formatted = formatDocumentTitle("🎯".repeat(MAX_PAGE_TITLE_LENGTH + 10));
    const pageTitle = formatted.slice(0, formatted.indexOf(" | Multica"));

    expect(Array.from(pageTitle)).toHaveLength(MAX_PAGE_TITLE_LENGTH + 1);
    expect(pageTitle).not.toContain("�");
    expect(pageTitle.endsWith("🎯…")).toBe(true);
  });

  it("leaves a title at the limit untouched", () => {
    const exact = "x".repeat(MAX_PAGE_TITLE_LENGTH);
    expect(formatDocumentTitle(exact)).toBe(`${exact} | Multica`);
  });
});

describe("WorkspaceDocumentTitle", () => {
  it("names the tab after the open issue", () => {
    open("/acme/issues/MUL-123");
    presentation.title = "MUL-123: Fix login";

    render(<WorkspaceDocumentTitle />);

    expect(document.title).toBe("MUL-123: Fix login | Multica");
  });

  it("resolves against the full URL so a container's selection titles the tab", () => {
    open("/acme/inbox", "issue=abc&view=archived");
    presentation.title = "MUL-9: Ping";

    render(<WorkspaceDocumentTitle />);

    expect(presentation.urls).toContain("/acme/inbox?issue=abc&view=archived");
    expect(document.title).toBe("MUL-9: Ping | Multica");
  });

  it("keeps the site title on an unrecognized route", () => {
    open("/acme/not-a-route");
    presentation.title = "Unknown page";

    render(<WorkspaceDocumentTitle />);

    expect(document.title).toBe(SITE_TITLE);
  });

  it("restores the site title when the dashboard unmounts", () => {
    open("/acme/projects/p1");
    presentation.title = "Website redesign";

    const view = render(<WorkspaceDocumentTitle />);
    expect(document.title).toBe("Website redesign | Multica");

    view.unmount();
    expect(document.title).toBe(SITE_TITLE);
  });

  it("re-asserts the title when a navigation keeps the same page name", () => {
    open("/acme/inbox");
    presentation.title = "Inbox";
    const view = render(<WorkspaceDocumentTitle />);
    expect(document.title).toBe("Inbox | Multica");

    // A route change re-renders the target's metadata, resetting the title to
    // the root default before our effect gets to run again.
    document.title = SITE_TITLE;
    open("/acme/inbox", "view=archived");
    view.rerender(<WorkspaceDocumentTitle />);

    expect(document.title).toBe("Inbox | Multica");
  });
});
