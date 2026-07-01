import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiClient, setApiInstance } from "@multica/core/api";
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

/**
 * Default tests run with reduced-motion *on* so the modal's entrance
 * delay short-circuits and the dialog opens synchronously — keeps the
 * behavioural tests focused on selection / submit / skip semantics
 * rather than fighting timers. The dedicated entrance-delay test below
 * overrides this to assert the deferred-open path.
 */
function mockPrefersReducedMotion(matches: boolean) {
  Object.defineProperty(window, "matchMedia", {
    writable: true,
    configurable: true,
    value: (q: string) => ({
      matches: q.includes("reduce") ? matches : false,
      media: q,
      onchange: null,
      addListener: () => {},
      removeListener: () => {},
      addEventListener: () => {},
      removeEventListener: () => {},
      dispatchEvent: () => false,
    }),
  });
}

beforeEach(() => {
  mockSaveQuestionnaire.mockReset();
  mockSaveQuestionnaire.mockResolvedValue(undefined);
  mockCaptureEvent.mockReset();
  setApiInstance(new ApiClient("https://api.multica.ai"));
  setUser(null);
  wipeDismissCounters();
  mockPrefersReducedMotion(true);
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

  it("shows reporting controls after a source is selected on a non-official API URL", async () => {
    setApiInstance(new ApiClient("https://api.customer.example"));
    setUser({
      id: "u1",
      onboarded_at: "2026-01-01T00:00:00Z",
      onboarding_questionnaire: { source: [] },
    });
    const user = userEvent.setup();
    renderModal();
    expect(
      screen.queryByText(
        "Help us understand how you heard about Multica. No extra information is sent.",
      ),
    ).not.toBeInTheDocument();
    await user.click(await screen.findByText("Friends or colleagues"));

    expect(
      screen.getByText(
        "Help us understand how you heard about Multica. No extra information is sent.",
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("switch", { name: /allow sending domain/i }),
    ).toBeChecked();
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
    expect(sent.source_domain_consent).toBe(true);
    expect(sent.role).toBe("engineer");
    expect(sent.use_case).toEqual(["ship_code", "plan_research"]);
    expect(sent.version).toBe(2);
    expect(mockCaptureEvent).toHaveBeenCalledWith(
      "source_backfill_submitted",
      expect.objectContaining({ source: ["friends_colleagues"] }),
    );
  });

  it("persists false when the user disables plaintext domain reporting", async () => {
    setApiInstance(new ApiClient("https://api.customer.example"));
    setUser({
      id: "u1",
      onboarded_at: "2026-01-01T00:00:00Z",
      onboarding_questionnaire: {
        source: [],
        role: "engineer",
        use_case: ["ship_code"],
        version: 2,
      },
    });
    const user = userEvent.setup();
    renderModal();
    await user.click(await screen.findByText("Friends or colleagues"));
    await user.click(
      screen.getByRole("switch", { name: /allow sending domain/i }),
    );
    await user.click(screen.getByRole("button", { name: "Submit" }));

    await waitFor(() => {
      expect(mockSaveQuestionnaire).toHaveBeenCalledTimes(1);
    });
    expect(mockSaveQuestionnaire.mock.calls[0]![0].source_domain_consent).toBe(
      false,
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
    expect(sent.source_domain_consent).toBe(true);
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

  it("renders the GitHub channel rebased from origin/main", async () => {
    setUser({
      id: "u1",
      onboarded_at: "2026-01-01T00:00:00Z",
      onboarding_questionnaire: { source: [] },
    });
    renderModal();
    expect(await screen.findByText("GitHub")).toBeInTheDocument();
  });

  it("picking a second option replaces the first (single-select primary source)", async () => {
    // The modal is now a single-select radio. Industry default for
    // HDYHAU is to capture the primary acquisition source, so picking
    // a second option must replace the first — never accumulate.
    setUser({
      id: "u1",
      onboarded_at: "2026-01-01T00:00:00Z",
      onboarding_questionnaire: { source: [] },
    });
    const user = userEvent.setup();
    renderModal();
    await screen.findByText("Friends or colleagues");
    const radios = screen.getAllByRole("radio");
    const friends = radios[0]!;
    const search = radios[1]!;

    await user.click(friends);
    expect(friends).toHaveAttribute("aria-checked", "true");
    expect(search).toHaveAttribute("aria-checked", "false");

    // Pick a second option — the first must clear and Submit stays
    // enabled with exactly one pick in the payload.
    await user.click(search);
    expect(friends).toHaveAttribute("aria-checked", "false");
    expect(search).toHaveAttribute("aria-checked", "true");

    await user.click(screen.getByRole("button", { name: "Submit" }));
    await waitFor(() => {
      expect(mockSaveQuestionnaire).toHaveBeenCalledTimes(1);
    });
    const sent = mockSaveQuestionnaire.mock.calls[0]![0];
    // Server schema is still `source: string[]` for back-compat with
    // v2 rows; the client always sends a single-element array.
    expect(sent.source).toEqual(["search"]);
    expect(sent.source).not.toContain("friends_colleagues");
  });

  it("defers the entrance by ~700ms when the user has not opted into reduced motion", async () => {
    mockPrefersReducedMotion(false);
    vi.useFakeTimers();
    try {
      setUser({
        id: "u1",
        onboarded_at: "2026-01-01T00:00:00Z",
        onboarding_questionnaire: { source: [] },
      });
      renderModal();
      // Immediately after mount: still hidden — the workspace gets a
      // beat to render before the modal floats in.
      expect(
        screen.queryByText(/How did you hear about Multica/i),
      ).not.toBeInTheDocument();
      await act(async () => {
        await vi.advanceTimersByTimeAsync(699);
      });
      expect(
        screen.queryByText(/How did you hear about Multica/i),
      ).not.toBeInTheDocument();
      await act(async () => {
        await vi.advanceTimersByTimeAsync(50);
      });
      expect(
        screen.queryByText(/How did you hear about Multica/i),
      ).toBeInTheDocument();
    } finally {
      vi.useRealTimers();
    }
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
