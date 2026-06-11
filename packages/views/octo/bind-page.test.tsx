import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import type { ReactElement, ReactNode } from "react";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../locales/en/common.json";
import { OctoBindPage } from "./bind-page";

const TEST_RESOURCES = { en: { common: enCommon } };

function I18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

function renderWithI18n(ui: ReactElement) {
  return render(ui, { wrapper: I18nWrapper });
}

// ---------------------------------------------------------------------------
// Hoisted mocks
// ---------------------------------------------------------------------------

const mockRedeem = vi.hoisted(() => vi.fn());
const mockNavigate = vi.hoisted(() => vi.fn());
const mockUser = vi.hoisted(() => ({ value: null as null | { id: string } }));

// Minimal ApiError stub carrying the status code — bind-page now classifies
// redemption failures by err.status, so the mock must expose the same class.
const ApiError = vi.hoisted(() => {
  return class ApiError extends Error {
    status: number;
    constructor(message: string, status: number) {
      super(message);
      this.name = "ApiError";
      this.status = status;
    }
  };
});

vi.mock("@multica/core/api", () => ({
  api: { redeemOctoBindingToken: mockRedeem },
  ApiError,
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: Object.assign(
    (selector?: (s: unknown) => unknown) => {
      const state = { user: mockUser.value };
      return selector ? selector(state) : state;
    },
    { getState: () => ({ user: mockUser.value }) },
  ),
}));

vi.mock("../navigation", () => ({
  useNavigation: () => ({ push: mockNavigate, replace: mockNavigate }),
}));

describe("OctoBindPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockUser.value = { id: "u1" };
  });

  it("redeems the token and shows the bound state when logged in", async () => {
    mockRedeem.mockResolvedValue({
      workspace_id: "ws1",
      installation_id: "inst1",
      octo_uid: "uid1",
    });

    renderWithI18n(<OctoBindPage token="raw-token" />);

    await waitFor(() => {
      expect(screen.getByText(enCommon.octo_bind.done_title)).toBeInTheDocument();
    });
    expect(mockRedeem).toHaveBeenCalledWith("raw-token");
  });

  it("shows the missing-token error when no token is present", async () => {
    renderWithI18n(<OctoBindPage token={null} />);

    await waitFor(() => {
      expect(
        screen.getByText(enCommon.octo_bind.error_missing_token),
      ).toBeInTheDocument();
    });
    expect(mockRedeem).not.toHaveBeenCalled();
  });

  it("prompts for sign-in when the user is not authenticated", async () => {
    mockUser.value = null;
    renderWithI18n(<OctoBindPage token="raw-token" />);

    await waitFor(() => {
      expect(
        screen.getByText(enCommon.octo_bind.needs_auth_description),
      ).toBeInTheDocument();
    });
    // Must not attempt redemption without an identity (the redeemer is the
    // session, not the token).
    expect(mockRedeem).not.toHaveBeenCalled();
  });

  it.each([
    [410, "octo_bind.error_expired"],
    [409, "octo_bind.error_already_bound"],
    [403, "octo_bind.error_not_member"],
  ])("maps backend HTTP %s to specific copy", async (status, key) => {
    mockRedeem.mockRejectedValue(new ApiError("redeem failed", status));
    renderWithI18n(<OctoBindPage token="raw-token" />);

    const expected =
      key === "octo_bind.error_expired"
        ? enCommon.octo_bind.error_expired
        : key === "octo_bind.error_already_bound"
          ? enCommon.octo_bind.error_already_bound
          : enCommon.octo_bind.error_not_member;

    await waitFor(() => {
      expect(screen.getByText(expected)).toBeInTheDocument();
    });
  });

  it("falls back to the generic error for an unexpected status", async () => {
    mockRedeem.mockRejectedValue(new ApiError("boom", 500));
    renderWithI18n(<OctoBindPage token="raw-token" />);
    await waitFor(() => {
      expect(screen.getByText(enCommon.octo_bind.error_unknown)).toBeInTheDocument();
    });
  });
});
