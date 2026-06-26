import type { ReactNode } from "react";
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { renderHook } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import enRuntimes from "../locales/en/runtimes.json";
import { useFormatLastSeen } from "./use-format-last-seen";

function wrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={{ en: { runtimes: enRuntimes } }}>
      {children}
    </I18nProvider>
  );
}

const NOW = new Date("2026-03-15T12:00:00Z").getTime();
const ago = (ms: number) => new Date(NOW - ms).toISOString();
const SEC = 1000;
const MIN = 60 * SEC;
const HOUR = 60 * MIN;
const DAY = 24 * HOUR;

describe("useFormatLastSeen", () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date(NOW));
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("keeps seconds-level precision and compound units (liveness)", () => {
    const { result } = renderHook(() => useFormatLastSeen(), { wrapper });
    const f = result.current;
    expect(f(null)).toBe("Never");
    expect(f(ago(3 * SEC))).toBe("Just now"); // < 5s
    expect(f(ago(6 * SEC))).toBe("6s ago");
    expect(f(ago(90 * SEC))).toBe("1m 30s ago"); // compound
    expect(f(ago(2 * MIN))).toBe("2m ago");
    expect(f(ago(HOUR + 1 * MIN))).toBe("1h 1m ago"); // compound
    expect(f(ago(2 * HOUR))).toBe("2h ago");
    expect(f(ago(2 * DAY + 3 * HOUR))).toBe("2d 3h ago"); // compound
    expect(f(ago(3 * DAY))).toBe("3d ago");
  });
});
