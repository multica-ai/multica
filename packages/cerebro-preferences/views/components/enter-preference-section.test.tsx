import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

// ---------------------------------------------------------------------------
// Hoisted mocks — allow per-test mutation of the auth-store user blob and
// observation of api/setUser calls.
// ---------------------------------------------------------------------------

const authState = vi.hoisted(() => {
  const setUser = vi.fn((u: unknown) => {
    state.user = u as { preferences?: Record<string, unknown> } | null;
  });
  const state = {
    user: null as { preferences?: Record<string, unknown> } | null,
    setUser,
  };
  return state;
});

const mockUpdateMyPreferences = vi.hoisted(() => vi.fn());
const mockToastSuccess = vi.hoisted(() => vi.fn());
const mockToastError = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/auth", () => ({
  useAuthStore: Object.assign(
    (selector?: (s: typeof authState) => unknown) =>
      selector ? selector(authState) : authState,
    { getState: () => authState },
  ),
}));

vi.mock("@multica/core/api", () => ({
  api: { updateMyPreferences: mockUpdateMyPreferences },
}));

vi.mock("sonner", () => ({
  toast: { success: mockToastSuccess, error: mockToastError },
}));

import { EnterPreferenceSection } from "./enter-preference-section";
import { ISSUE_LINK_OPEN_MODE_KEY } from "./use-submit-on-enter";

beforeEach(() => {
  authState.user = {
    preferences: {},
  };
  authState.setUser.mockClear();
  mockUpdateMyPreferences.mockReset();
  mockToastSuccess.mockClear();
  mockToastError.mockClear();
});

describe("EnterPreferenceSection", () => {
  it("defaults the switch to off when preference is unset", () => {
    render(<EnterPreferenceSection />);
    expect(screen.getByRole("switch", { name: /send messages with enter/i })).toHaveAttribute(
      "aria-checked",
      "false",
    );
    // Default copy mentions Enter inserts a newline.
    expect(screen.getByText(/Enter inserts a new line/i)).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: /use modifier-click/i })).toHaveAttribute(
      "aria-checked",
      "true",
    );
  });

  it("reflects an opted-in preference as a checked switch with chat-style copy", () => {
    authState.user = { preferences: { submit_on_enter: true } };
    render(<EnterPreferenceSection />);
    expect(screen.getByRole("switch", { name: /send messages with enter/i })).toHaveAttribute(
      "aria-checked",
      "true",
    );
    expect(screen.getByText(/Enter sends/i)).toBeInTheDocument();
  });

  it("PATCHes preferences and updates the auth store when toggled on", async () => {
    const user = userEvent.setup();
    const updatedUser = {
      id: "u1",
      preferences: { submit_on_enter: true },
    };
    mockUpdateMyPreferences.mockResolvedValue(updatedUser);

    render(<EnterPreferenceSection />);
    await user.click(screen.getByRole("switch", { name: /send messages with enter/i }));

    await waitFor(() => {
      expect(mockUpdateMyPreferences).toHaveBeenCalledTimes(1);
    });
    expect(mockUpdateMyPreferences).toHaveBeenCalledWith({ submit_on_enter: true });
    expect(authState.setUser).toHaveBeenCalledWith(updatedUser);
    expect(mockToastSuccess).toHaveBeenCalledWith("Enter now sends");
  });

  it("PATCHes false when toggled off and shows mod-key copy in the toast", async () => {
    const user = userEvent.setup();
    authState.user = { preferences: { submit_on_enter: true } };
    mockUpdateMyPreferences.mockResolvedValue({
      id: "u1",
      preferences: { submit_on_enter: false },
    });

    render(<EnterPreferenceSection />);
    await user.click(screen.getByRole("switch", { name: /send messages with enter/i }));

    await waitFor(() => {
      expect(mockUpdateMyPreferences).toHaveBeenCalledTimes(1);
    });
    expect(mockUpdateMyPreferences).toHaveBeenCalledWith({ submit_on_enter: false });
    // Toast string starts with the modifier key (⌘ or Ctrl depending on platform);
    // assert on the unambiguous suffix instead of the platform-dependent prefix.
    expect(mockToastSuccess).toHaveBeenCalledWith(expect.stringMatching(/\+Enter now sends$/));
  });

  it("PATCHes issue link mode and updates the auth store", async () => {
    const user = userEvent.setup();
    const updatedUser = {
      id: "u1",
      preferences: { [ISSUE_LINK_OPEN_MODE_KEY]: "always_new_tab" },
    };
    mockUpdateMyPreferences.mockResolvedValue(updatedUser);

    render(<EnterPreferenceSection />);
    await user.click(screen.getByRole("radio", { name: /always open a new tab/i }));

    await waitFor(() => {
      expect(mockUpdateMyPreferences).toHaveBeenCalledTimes(1);
    });
    expect(mockUpdateMyPreferences).toHaveBeenCalledWith({
      [ISSUE_LINK_OPEN_MODE_KEY]: "always_new_tab",
    });
    expect(authState.setUser).toHaveBeenCalledWith(updatedUser);
    expect(mockToastSuccess).toHaveBeenCalledWith("Issue links now open in a new tab");
  });

  it("reflects an always-new-tab issue link preference", () => {
    authState.user = { preferences: { [ISSUE_LINK_OPEN_MODE_KEY]: "always_new_tab" } };
    render(<EnterPreferenceSection />);
    const alwaysNewTab = screen.getByText("Always open a new tab").closest("button");
    const modifierClick = screen.getByText("Use modifier-click").closest("button");
    expect(alwaysNewTab).toHaveAttribute(
      "aria-checked",
      "true",
    );
    expect(modifierClick).toHaveAttribute(
      "aria-checked",
      "false",
    );
  });

  it("surfaces API errors via toast.error and leaves the user unchanged", async () => {
    const user = userEvent.setup();
    mockUpdateMyPreferences.mockRejectedValue(new Error("boom"));

    render(<EnterPreferenceSection />);
    await user.click(screen.getByRole("switch", { name: /send messages with enter/i }));

    await waitFor(() => {
      expect(mockToastError).toHaveBeenCalledWith("boom");
    });
    expect(authState.setUser).not.toHaveBeenCalled();
  });

  it("disables the switch when no user is loaded", () => {
    authState.user = null;
    render(<EnterPreferenceSection />);
    // Base UI's Switch renders aria-disabled / data-disabled rather than the
    // HTML `disabled` attribute, so toBeDisabled() doesn't apply.
    expect(screen.getByRole("switch", { name: /send messages with enter/i })).toHaveAttribute(
      "aria-disabled",
      "true",
    );
  });
});
