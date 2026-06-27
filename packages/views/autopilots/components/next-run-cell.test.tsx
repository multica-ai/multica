import { describe, it, expect, afterEach, vi } from "vitest";
import { screen } from "@testing-library/react";
import { renderWithI18n } from "../../test/i18n";
import type { Autopilot } from "@multica/core/types";

// The cell renders relative time via useViewingTimezone → useAuthStore; pin the
// viewer to UTC so next_run_at offsets read deterministically.
vi.mock("@multica/core/auth", () => {
  type AuthState = { user: { timezone?: string | null } };
  const user = { timezone: "UTC" };
  const useAuthStore = Object.assign(
    (sel: (s: AuthState) => unknown) => sel({ user }),
    { getState: () => ({ user }) },
  );
  return { useAuthStore };
});

import { NextRunCell } from "./autopilots-page";

// Pin "now" so next_run_at offsets are deterministic across the future/overdue
// boundary the cell branches on.
const NOW = "2026-06-26T12:00:00Z";

const autopilot = (overrides: Partial<Autopilot> = {}): Autopilot => ({
  id: "ap-1",
  workspace_id: "ws-1",
  title: "Daily digest",
  description: null,
  assignee_type: "agent",
  assignee_id: "agent-1",
  status: "active",
  execution_mode: "create_issue",
  issue_title_template: null,
  created_by_type: "user",
  created_by_id: "user-1",
  last_run_at: null,
  created_at: NOW,
  updated_at: NOW,
  ...overrides,
});

describe("NextRunCell", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  function freezeNow() {
    // Fake only Date so React/i18next scheduling keeps real timers.
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date(NOW));
  }

  it("renders relative time for a genuinely future next run", () => {
    freezeNow();
    renderWithI18n(
      <NextRunCell autopilot={autopilot({ next_run_at: "2026-06-26T15:00:00Z" })} />,
    );
    expect(screen.getByText("in 3h")).toBeTruthy();
  });

  it("shows 'soon' for an overdue slot the scheduler hasn't picked up yet", () => {
    freezeNow();
    // The slot fired a minute ago but next_run_at hasn't rolled forward — an
    // imminent run, not a past one, so relative "Xm ago" would mislead.
    renderWithI18n(
      <NextRunCell
        autopilot={autopilot({
          next_run_at: "2026-06-26T11:59:00Z",
          last_run_status: "completed",
        })}
      />,
    );
    expect(screen.getByText("soon")).toBeTruthy();
  });

  it("shows 'Paused' instead of a countdown for paused autopilots", () => {
    freezeNow();
    renderWithI18n(
      <NextRunCell
        autopilot={autopilot({
          status: "paused",
          next_run_at: "2026-06-26T15:00:00Z",
        })}
      />,
    );
    expect(screen.getByText("Paused")).toBeTruthy();
  });

  it("renders a dash when no next run is scheduled", () => {
    freezeNow();
    renderWithI18n(<NextRunCell autopilot={autopilot({ next_run_at: null })} />);
    expect(screen.getByText("—")).toBeTruthy();
  });
});
