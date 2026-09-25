import { expect, it } from "vitest";
import { formatWakeupTime } from "./wakeup-presentation";

it("uses the configured timezone to distinguish today from the next day", () => {
  const now = new Date("2026-09-16T15:00:00Z");
  expect(formatWakeupTime("2026-09-16T15:30:00Z", "en-US", "Asia/Shanghai", now)).not.toContain("Sep");
  expect(formatWakeupTime("2026-09-16T16:30:00Z", "en-US", "Asia/Shanghai", now)).toContain("Sep 17");
});

it("includes the year across the configured timezone's new year", () => {
  const now = new Date("2026-12-31T15:00:00Z");
  expect(formatWakeupTime("2026-12-31T16:30:00Z", "en-US", "Asia/Shanghai", now)).toContain("2027");
});
