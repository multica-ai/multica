import { describe, expect, it } from "vitest";
import {
  resolveStartPageFromCookies,
  startPagePath,
} from "./start-page-redirect";

function cookies(values: Record<string, string>) {
  return {
    get(name: string) {
      const value = values[name];
      return value ? { value } : undefined;
    },
  };
}

describe("start page redirect", () => {
  it("uses desktop preference at desktop width", () => {
    const headers = new Headers({ "sec-ch-viewport-width": "1024" });
    expect(
      startPagePath(
        "firtal",
        cookies({ start_page_desktop: "inbox", start_page_mobile: "documents" }),
        headers,
      ),
    ).toBe("/firtal/inbox");
  });

  it("uses mobile preference below 768px", () => {
    const headers = new Headers({ "sec-ch-viewport-width": "767" });
    expect(
      startPagePath(
        "firtal",
        cookies({ start_page_desktop: "inbox", start_page_mobile: "documents" }),
        headers,
      ),
    ).toBe("/firtal/documents");
  });

  it("uses mobile preference when browser only sends mobile hint", () => {
    const headers = new Headers({ "sec-ch-ua-mobile": "?1" });
    expect(
      resolveStartPageFromCookies(
        cookies({ start_page_desktop: "inbox", start_page_mobile: "projects" }),
        headers,
      ),
    ).toBe("projects");
  });

  it("falls back to issues when preference is missing or invalid", () => {
    const headers = new Headers({ "sec-ch-viewport-width": "1024" });
    expect(
      startPagePath(
        "firtal",
        cookies({ start_page_desktop: "admin" }),
        headers,
      ),
    ).toBe("/firtal/issues");
  });
});
