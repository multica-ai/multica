import { type ReactNode } from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../locales/en/common.json";

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

vi.mock("@multica/ui/components/ui/tooltip", () => ({
  Tooltip: ({ children }: { children: ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ render }: { render: ReactNode }) => <>{render}</>,
  TooltipContent: ({ children }: { children: ReactNode }) => (
    <div role="tooltip">{children}</div>
  ),
}));

import { InstantTooltip } from "./instant-tooltip";

function renderIt(ui: ReactNode) {
  return render(
    <I18nProvider locale="en" resources={{ en: { common: enCommon } }}>
      {ui}
    </I18nProvider>,
  );
}

const VALUE = "2026-03-01T06:30:45Z"; // 14:30:45 Asia/Shanghai (+8)

describe("InstantTooltip", () => {
  beforeEach(() => {
    userRef.current = { timezone: "Asia/Shanghai" };
  });

  it("keeps the localized phrase visible and adds a full-time tooltip", () => {
    renderIt(<InstantTooltip value={VALUE}>Updated 2d ago</InstantTooltip>);
    expect(screen.getByText("Updated 2d ago")).toBeTruthy();
    expect(screen.getByRole("tooltip").textContent).toBe(
      "Mar 1, 2026, 02:30:45 PM (GMT+8)",
    );
  });

  it("renders the phrase without a tooltip when value is empty", () => {
    renderIt(<InstantTooltip value={null}>never</InstantTooltip>);
    expect(screen.getByText("never")).toBeTruthy();
    expect(screen.queryByRole("tooltip")).toBeNull();
  });
});
