import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../locales/en/common.json";
import enOnboarding from "../locales/en/onboarding.json";

const TEST_RESOURCES = { en: { common: enCommon, onboarding: enOnboarding } };

const { mockUser, mockSaveQuestionnaire, mockCaptureEvent } = vi.hoisted(() => ({
  mockUser: { value: null as null | Record<string, unknown> },
  mockSaveQuestionnaire: vi.fn(),
  mockCaptureEvent: vi.fn(),
}));

vi.mock("@multica/core/auth", async () => {
  const actual =
    await vi.importActual<typeof import("@multica/core/auth")>(
      "@multica/core/auth",
    );
  const useAuthStore = Object.assign(
    (selector: (s: { user: unknown }) => unknown) =>
      selector({ user: mockUser.value }),
    { getState: () => ({ user: mockUser.value }) },
  );
  return { ...actual, useAuthStore };
});

vi.mock("@multica/core/onboarding", async () => {
  const actual =
    await vi.importActual<typeof import("@multica/core/onboarding")>(
      "@multica/core/onboarding",
    );
  return { ...actual, saveQuestionnaire: mockSaveQuestionnaire };
});

vi.mock("@multica/core/analytics", () => ({
  captureEvent: mockCaptureEvent,
  setPersonProperties: vi.fn(),
}));

import { SourceBackfillModal } from "./source-backfill-modal";

function setUser(partial: Record<string, unknown> | null) {
  mockUser.value = partial;
}

function wipeDismissCounters() {
  for (let i = window.localStorage.length - 1; i >= 0; i--) {
    const k = window.localStorage.key(i);
    if (k && k.startsWith("multica.source_backfill.dismiss.")) {
      window.localStorage.removeItem(k);
    }
  }
}

beforeEach(() => {
  mockSaveQuestionnaire.mockReset();
  mockSaveQuestionnaire.mockResolvedValue(undefined);
  mockCaptureEvent.mockReset();
  setUser(null);
  wipeDismissCounters();
});

afterEach(() => {
  wipeDismissCounters();
});

function renderModal() {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <SourceBackfillModal />
    </I18nProvider>,
  );
}

describe("SourceBackfillModal", () => {
  it("does not render when there is no user", () => {
    renderModal();
    expect(
      screen.queryByText(/How did you hear about Multica/i),
    ).not.toBeInTheDocument();
  });

  it("does not render when the user already recorded a source", () => {
    setUser({
      id: "u1",
      onboarded_at: "2026-01-01T00:00:00Z",
      onboarding_questionnaire: { source: ["search"] },
    });
    renderModal();
    expect(
      screen.queryByText(/How did you hear about Multica/i),
    ).not.toBeInTheDocument();
  });

  it("opens for an onboarded user with empty source and fires the shown event", async () => {
    setUser({
      id: "u1",
      onboarded_at: "2026-01-01T00:00:00Z",
      onboarding_questionnaire: { source: [] },
    });
    renderModal();
    await waitFor(() => {
      expect(
        screen.getByText(/How did you hear about Multica/i),
      ).toBeInTheDocument();
    });
    expect(mockCaptureEvent).toHaveBeenCalledWith("source_backfill_shown");
  });

  it("Submit PATCHes the merged questionnaire preserving role / use_case", async () => {
    setUser({
      id: "u1",
      onboarded_at: "2026-01-01T00:00:00Z",
      onboarding_questionnaire: {
        source: [],
        role: "engineer",
        role_skipped: false,
        use_case: ["ship_code", "plan_research"],
        use_case_skipped: false,
        version: 2,
      },
    });
    const user = userEvent.setup();
    renderModal();
    await user.click(await screen.findByText("Friends or colleagues"));
    await user.click(screen.getByRole("button", { name: "Submit" }));

    await waitFor(() => {
      expect(mockSaveQuestionnaire).toHaveBeenCalledTimes(1);
    });
    const sent = mockSaveQuestionnaire.mock.calls[0]![0];
    expect(sent.source).toEqual(["friends_colleagues"]);
    expect(sent.source_skipped).toBe(false);
    expect(sent.role).toBe("engineer");
    expect(sent.use_case).toEqual(["ship_code", "plan_research"]);
    expect(sent.version).toBe(2);
    expect(mockCaptureEvent).toHaveBeenCalledWith(
      "source_backfill_submitted",
      expect.objectContaining({ source: ["friends_colleagues"] }),
    );
  });

  it("Skip PATCHes source_skipped=true preserving role / use_case", async () => {
    setUser({
      id: "u1",
      onboarded_at: "2026-01-01T00:00:00Z",
      onboarding_questionnaire: {
        source: [],
        role: "founder",
        use_case: ["manage_team"],
        version: 2,
      },
    });
    const user = userEvent.setup();
    renderModal();
    await user.click(
      await screen.findByRole("button", { name: "Skip" }),
    );
    await waitFor(() => {
      expect(mockSaveQuestionnaire).toHaveBeenCalledTimes(1);
    });
    const sent = mockSaveQuestionnaire.mock.calls[0]![0];
    expect(sent.source).toEqual([]);
    expect(sent.source_skipped).toBe(true);
    expect(sent.role).toBe("founder");
    expect(sent.use_case).toEqual(["manage_team"]);
    expect(mockCaptureEvent).toHaveBeenCalledWith("source_backfill_skipped");
  });

  it("treats a legacy single-string source as already answered", () => {
    setUser({
      id: "u1",
      onboarded_at: "2026-01-01T00:00:00Z",
      onboarding_questionnaire: { source: "search" },
    });
    renderModal();
    expect(
      screen.queryByText(/How did you hear about Multica/i),
    ).not.toBeInTheDocument();
  });

  it("does not open once the per-user dismiss cap is reached on this browser", () => {
    window.localStorage.setItem("multica.source_backfill.dismiss.u1", "3");
    setUser({
      id: "u1",
      onboarded_at: "2026-01-01T00:00:00Z",
      onboarding_questionnaire: { source: [] },
    });
    renderModal();
    expect(
      screen.queryByText(/How did you hear about Multica/i),
    ).not.toBeInTheDocument();
  });
});
