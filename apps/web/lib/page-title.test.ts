import { describe, expect, it } from "vitest";
import {
  formatEntityPageTitle,
  formatIssuePageTitle,
  truncatePageTitle,
} from "./page-title";
import { dashboardRouteTitle } from "@/components/dashboard-page-title";

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
});
