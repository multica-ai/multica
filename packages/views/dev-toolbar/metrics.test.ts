// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  apiHost,
  blockedRatio,
  formatBytes,
  jankTone,
  memoryTone,
  navDuration,
  navTone,
} from "./metrics";

const MB = 1024 * 1024;

describe("formatBytes", () => {
  it("shows megabytes below a gigabyte and gigabytes above", () => {
    expect(formatBytes(152.4 * MB)).toBe("152MB");
    expect(formatBytes(1536 * MB)).toBe("1.50GB");
  });
});

describe("memoryTone", () => {
  it("warns from 500MB and flags from 1GB", () => {
    expect(memoryTone(499 * MB)).toBe("normal");
    expect(memoryTone(500 * MB)).toBe("warn");
    expect(memoryTone(1024 * MB)).toBe("bad");
  });
});

describe("navDuration", () => {
  it("measures from the triggering input to the painted frame", () => {
    expect(navDuration(1_000, 1_542)).toBe(542);
  });

  it("ignores route changes no recent input explains", () => {
    expect(navDuration(null, 5_000)).toBeNull();
    expect(navDuration(1_000, 12_000)).toBeNull();
    expect(navDuration(2_000, 1_000)).toBeNull();
  });
});

describe("navTone", () => {
  it("warns from 300ms and flags from 1s", () => {
    expect(navTone(299)).toBe("normal");
    expect(navTone(300)).toBe("warn");
    expect(navTone(1_000)).toBe("bad");
  });
});

describe("blockedRatio", () => {
  it("counts only the part of each task inside the window", () => {
    const tasks = [
      { start: 0, end: 2_000 },
      { start: 9_000, end: 9_500 },
      { start: 19_000, end: 19_500 },
    ];
    // Window is [10_000, 20_000]: only the last task overlaps it.
    expect(blockedRatio(tasks, 20_000, 10_000)).toBeCloseTo(0.05);
    // Window is [1_000, 11_000]: 1_000ms of the first task plus the second.
    expect(blockedRatio(tasks, 11_000, 10_000)).toBeCloseTo(0.15);
  });

  it("is zero with no long tasks", () => {
    expect(blockedRatio([], 20_000)).toBe(0);
  });
});

describe("jankTone", () => {
  it("warns from 5% and flags from 20%", () => {
    expect(jankTone(0.049)).toBe("normal");
    expect(jankTone(0.05)).toBe("warn");
    expect(jankTone(0.2)).toBe("bad");
  });
});

describe("apiHost", () => {
  it("keeps host and port, and passes through what is not a URL", () => {
    expect(apiHost("http://localhost:18080")).toBe("localhost:18080");
    expect(apiHost("https://api.multica.ai/")).toBe("api.multica.ai");
    expect(apiHost("same-origin")).toBe("same-origin");
  });
});
