import { describe, expect, it } from "vitest";
import { render, waitFor } from "@testing-library/react";
import {
  formatEntityPageTitle,
  formatIssuePageTitle,
  truncatePageTitle,
} from "./page-title";
import {
  PUBLIC_DEFAULT_TITLE,
  PUBLIC_TITLE_TEMPLATE,
} from "./public-page-title";
import { dashboardRouteTitle } from "@/components/dashboard-page-title";
import { PageTitle } from "@/components/page-title";

describe("browser page title formatting", () => {
  it("keeps the issue identifier first and truncates only the issue title", () => {
    const title = formatIssuePageTitle(
      "SCA-240",
      "Fix browser tab titles so every open issue remains easy to distinguish at a glance",
    );

    expect(title).toMatch(/^SCA-240 /);
    expect(title).toContain("…");
    expect(title).not.toMatch(/^Multica\s[-—]/);
  });

  it("uses concise non-issue route titles without a brand prefix", () => {
    expect(formatEntityPageTitle("Settings", "Repositories")).toBe("Settings · Repositories");
    expect(formatEntityPageTitle("Issues")).toBe("Issues");
    expect(dashboardRouteTitle("/acme/settings", "repositories").fallback).toBe(
      "Settings · Repositories",
    );
  });

  it("normalizes whitespace before truncating", () => {
    expect(truncatePageTitle("  One\n  title  ")).toBe("One title");
  });

  it("classifies an issue detail path so the first paint can leave the public brand default", () => {
    const route = dashboardRouteTitle(
      "/scaling-forever/issues/SCA-286",
      null,
    );
    expect(route).toEqual({
      fallback: "SCA-286",
      detail: { kind: "issue", id: "SCA-286" },
    });
    expect(formatIssuePageTitle(route.detail?.id, undefined)).toBe("SCA-286");
    expect(
      formatIssuePageTitle(
        "SCA-286",
        "fix(desktop): eliminate recurring Gemini timeout blind spots",
      ),
    ).toMatch(/^SCA-286 /);
  });

  it("maps legacy settings ?tab=lark to Integrations and unknown tabs to Profile", () => {
    expect(dashboardRouteTitle("/acme/settings", "lark").fallback).toBe(
      "Settings · Integrations",
    );
    expect(dashboardRouteTitle("/acme/settings", "not-a-tab").fallback).toBe(
      "Settings · Profile",
    );
    expect(dashboardRouteTitle("/acme/settings", null).fallback).toBe(
      "Settings · Profile",
    );
  });
});

describe("public brand title policy", () => {
  it("keeps Multica in the public home absolute title", () => {
    expect(PUBLIC_DEFAULT_TITLE).toContain("Multica");
    expect(PUBLIC_DEFAULT_TITLE).not.toBe("Project workspace");
  });

  it("keeps a Multica suffix template for root-template public pages", () => {
    // About/changelog/use-cases set a relative title like "About" and rely on
    // the root metadata template for the brand suffix.
    expect(PUBLIC_TITLE_TEMPLATE).toBe("%s | Multica");
    expect(PUBLIC_TITLE_TEMPLATE.replace("%s", "About")).toBe("About | Multica");
  });
});

describe("PageTitle", () => {
  it("renders a title element and syncs document.title on first paint", () => {
    document.title = PUBLIC_DEFAULT_TITLE;
    const label = "SCA-286 fix(desktop): eliminate recurring Gemini…";
    render(<PageTitle title={label} />);
    expect(document.title).toBe(label);
  });

  it("restores its title when another owner resets the public brand default", async () => {
    const label = "SCA-286 fix(desktop): eliminate recurring Gemini…";
    render(<PageTitle title={label} />);

    document.title = PUBLIC_DEFAULT_TITLE;

    await waitFor(() => expect(document.title).toBe(label));
  });
});
