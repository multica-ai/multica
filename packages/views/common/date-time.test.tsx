import { type ReactNode } from "react";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../locales/en/common.json";
import zhCommon from "../locales/zh-Hans/common.json";

const userRef = vi.hoisted(
  () => ({ current: { timezone: "Asia/Shanghai" } as { timezone?: string | null } }),
);

vi.mock("@multica/core/auth", () => {
  type AuthState = { user: typeof userRef.current };
  const useAuthStore = Object.assign(
    (sel: (s: AuthState) => unknown) => sel({ user: userRef.current }),
    { getState: () => ({ user: userRef.current }) },
  );
  return { useAuthStore };
});

vi.mock("./timezone-select", () => ({
  browserTimezone: () => "America/New_York",
}));

// Render the tooltip eagerly so its content is assertable without a hover/portal
// (Base UI mounts the real popup only while open).
vi.mock("@multica/ui/components/ui/tooltip", () => ({
  Tooltip: ({ children }: { children: ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ render }: { render: ReactNode }) => <>{render}</>,
  TooltipContent: ({ children }: { children: ReactNode }) => (
    <div role="tooltip">{children}</div>
  ),
}));

import { DateTime } from "./date-time";

const RESOURCES = {
  en: { common: enCommon },
  "zh-Hans": { common: zhCommon },
};

function renderDateTime(ui: ReactNode, locale: "en" | "zh-Hans" = "en") {
  return render(
    <I18nProvider locale={locale} resources={RESOURCES}>
      {ui}
    </I18nProvider>,
  );
}

// 2026-03-01T06:30:45Z == 14:30:45 in Asia/Shanghai (+8).
const VALUE = "2026-03-01T06:30:45Z";

describe("DateTime", () => {
  beforeEach(() => {
    userRef.current = { timezone: "Asia/Shanghai" };
  });

  it("shows the datetime in Viewing tz with a full time + offset tooltip", () => {
    renderDateTime(<DateTime value={VALUE} />);
    expect(screen.getByText("Mar 1, 2026, 02:30 PM")).toBeTruthy();
    expect(screen.getByRole("tooltip").textContent).toBe(
      "Mar 1, 2026, 02:30:45 PM (GMT+8)",
    );
  });

  it("renders the date and time variants", () => {
    // Pin "now" so the date variant's current-year suppression is deterministic.
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date("2026-06-26T12:00:00Z"));
    try {
      const { rerender } = renderDateTime(<DateTime value={VALUE} variant="date" />);
      // Same calendar year as "now" → the year is dropped; tooltip keeps it.
      expect(screen.getByText("Mar 1")).toBeTruthy();
      expect(screen.getByRole("tooltip").textContent).toContain("(GMT+8)");

      rerender(
        <I18nProvider locale="en" resources={RESOURCES}>
          <DateTime value={VALUE} variant="time" />
        </I18nProvider>,
      );
      expect(screen.getByText("02:30:45 PM")).toBeTruthy();
    } finally {
      vi.useRealTimers();
    }
  });

  it("gives a calendar day a date-only tooltip with no timezone (spec §3.5)", () => {
    renderDateTime(<DateTime value="2026-03-01" variant="calendarDate" />);
    expect(screen.getByText("Mar 1")).toBeTruthy();
    const tooltip = screen.getByRole("tooltip").textContent ?? "";
    expect(tooltip).toBe("March 1, 2026");
    expect(tooltip).not.toContain("GMT");
  });

  it("renders nothing for an empty value when hideWhenEmpty", () => {
    const { container } = renderDateTime(<DateTime value={null} />);
    expect(container.innerHTML).toBe("");
  });

  describe("relative variant", () => {
    beforeEach(() => {
      vi.useFakeTimers({ toFake: ["Date"] });
      vi.setSystemTime(new Date("2026-03-01T09:30:45Z"));
    });

    afterEach(() => {
      vi.useRealTimers();
    });

    it("shows relative text with a full time + offset tooltip", () => {
      renderDateTime(<DateTime value={VALUE} variant="relative" />);
      expect(screen.getByText("3h ago")).toBeTruthy();
      expect(screen.getByRole("tooltip").textContent).toBe(
        "Mar 1, 2026, 02:30:45 PM (GMT+8)",
      );
    });
  });
});
