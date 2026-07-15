import { existsSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

const viewsDirectory = fileURLToPath(new URL(".", import.meta.url));
const pageSource = readFileSync(new URL("./dashboard-page.tsx", import.meta.url), "utf8");
const messageDetailSource = readFileSync(new URL("./components/message-detail-sheet.tsx", import.meta.url), "utf8");

const legacyComponents = [
  "kpi-cards",
  "activity-chart",
  "issues-donut",
  "issues-on-behalf-of",
  "top-actors",
  "activity-feed",
  "recent-tasks-list",
  "message-activity-chart",
  "message-spend-table",
  "message-tracker",
  "message-flow",
  "all-messages-table",
  "message-search-panel",
  "actor-message-panel",
] as const;

describe("Dashboard legacy purge", () => {
  it("composes every tab from a control room", () => {
    expect(pageSource).toContain("OverviewControlRoom");
    expect(pageSource).toContain("RunsControlRoom");
    expect(pageSource).toContain("MessagesControlRoom");
  });

  it.each(legacyComponents)("removes the legacy %s component", (component) => {
    expect(pageSource).not.toContain(`./components/${component}`);
    expect(existsSync(`${viewsDirectory}components/${component}.tsx`)).toBe(false);
  });

  it("keeps the remaining message detail UI in English", () => {
    for (const legacyCopy of ["Fra", "Til", "Pris", "beskeder i denne session", "Åbn hele samtalen"]) {
      expect(messageDetailSource).not.toContain(legacyCopy);
    }
  });
});
