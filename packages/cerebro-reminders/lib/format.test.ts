import { describe, expect, it } from "vitest";

import { formatReminderDue, formatReminderSource, formatReminderWhen } from "./format";

// Fixed "now" so the relative labels are deterministic. 2026-06-19 10:00 local.
const NOW = new Date(2026, 5, 19, 10, 0, 0);

describe("formatReminderWhen", () => {
  it("labels today with the time", () => {
    const at = new Date(2026, 5, 19, 14, 0, 0).toISOString();
    expect(formatReminderWhen(at, NOW)).toMatch(/^I dag /);
  });

  it("labels tomorrow with the time", () => {
    const at = new Date(2026, 5, 20, 9, 0, 0).toISOString();
    expect(formatReminderWhen(at, NOW)).toMatch(/^I morgen /);
  });

  it("labels further out with weekday + date", () => {
    const at = new Date(2026, 5, 25, 9, 0, 0).toISOString();
    const label = formatReminderWhen(at, NOW);
    expect(label).not.toMatch(/^I dag|^I morgen/);
    expect(label).toMatch(/jun/);
  });

  it("returns empty string for an unparseable date", () => {
    expect(formatReminderWhen("not-a-date", NOW)).toBe("");
  });
});

describe("formatReminderDue", () => {
  it("prefixes with Forfalder", () => {
    const at = new Date(2026, 5, 19, 14, 0, 0).toISOString();
    expect(formatReminderDue(at, NOW)).toMatch(/^Forfalder i dag kl\. /);
  });
});

describe("formatReminderSource", () => {
  it("formats a DM", () => {
    expect(formatReminderSource("dm", "Filip")).toBe("DM · Filip");
  });
  it("formats a channel", () => {
    expect(formatReminderSource("channel", "kampagner")).toBe("#kampagner");
  });
  it("falls back to the title for a regular issue", () => {
    expect(formatReminderSource("issue", "Leverandør")).toBe("Leverandør");
  });
  it("has a sensible empty-title fallback", () => {
    expect(formatReminderSource("dm", "")).toBe("Direkte besked");
  });
});
