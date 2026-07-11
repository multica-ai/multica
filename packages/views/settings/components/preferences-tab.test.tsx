import type { ReactNode } from "react";
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, screen, act, cleanup, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enAuth from "../../locales/en/auth.json";
import enSettings from "../../locales/en/settings.json";

const mockPersist = vi.hoisted(() => vi.fn());
const mockUpdateMe = vi.hoisted(() => vi.fn());
const mockReload = vi.hoisted(() => vi.fn());
const mockToastWarning = vi.hoisted(() => vi.fn());
const mockToastError = vi.hoisted(() => vi.fn());
const mockToastSuccess = vi.hoisted(() => vi.fn());
const mockSetTheme = vi.hoisted(() => vi.fn());
const mockSetUser = vi.hoisted(() => vi.fn());
const userRef = vi.hoisted(() => ({
  current: null as { id: string; timezone?: string | null } | null,
}));

vi.mock("@multica/ui/components/common/theme-provider", () => ({
  useTheme: () => ({ theme: "light", setTheme: mockSetTheme }),
}));

vi.mock("@multica/core/i18n/react", async () => {
  const actual =
    await vi.importActual<typeof import("@multica/core/i18n/react")>(
      "@multica/core/i18n/react",
    );
  return {
    ...actual,
    useLocaleAdapter: () => ({
      persist: mockPersist,
      getUserChoice: () => null,
      getSystemPreferences: () => [],
    }),
  };
});

vi.mock("@multica/core/api", () => ({
  api: { updateMe: mockUpdateMe },
}));

vi.mock("sonner", () => ({
  toast: {
    warning: mockToastWarning,
    error: mockToastError,
    success: mockToastSuccess,
  },
}));

vi.mock("@multica/core/auth", async () => {
  const actual =
    await vi.importActual<typeof import("@multica/core/auth")>(
      "@multica/core/auth",
    );
  type AuthState = {
    user: typeof userRef.current;
    setUser: typeof mockSetUser;
  };
  const state = (): AuthState => ({
    user: userRef.current,
    setUser: mockSetUser,
  });
  const useAuthStore = Object.assign(
    (sel?: (s: AuthState) => unknown) =>
      sel ? sel(state()) : state(),
    { getState: state },
  );
  return { ...actual, useAuthStore };
});

import { PreferencesTab } from "./preferences-tab";

const TEST_RESOURCES = {
  en: { common: enCommon, auth: enAuth, settings: enSettings },
};

function I18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

describe("PreferencesTab — Language switcher", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    userRef.current = null;
    vi.useFakeTimers({ shouldAdvanceTime: true });
    Object.defineProperty(window, "location", {
      writable: true,
      configurable: true,
      value: { reload: mockReload },
    });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  async function pickLanguage(
    user: ReturnType<typeof userEvent.setup>,
    name: string,
  ) {
    await user.click(screen.getByRole("combobox", { name: "Language" }));
    await user.click(await screen.findByRole("option", { name }));
  }

  it("does nothing when clicking the current locale", async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<PreferencesTab />, { wrapper: I18nWrapper });

    await pickLanguage(user, "English");

    expect(mockPersist).not.toHaveBeenCalled();
    expect(mockUpdateMe).not.toHaveBeenCalled();
    expect(mockReload).not.toHaveBeenCalled();
  });

  it("shows a confirmation toast when the theme is saved locally", async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<PreferencesTab />, { wrapper: I18nWrapper });

    await user.click(screen.getByRole("combobox", { name: "Theme" }));
    await user.click(await screen.findByRole("option", { name: "Dark" }));

    expect(mockSetTheme).toHaveBeenCalledWith("dark");
    expect(mockToastSuccess).toHaveBeenCalledTimes(1);
  });

  it("when not logged in: persists + reloads, no PATCH", async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<PreferencesTab />, { wrapper: I18nWrapper });

    await pickLanguage(user, "한국어");

    expect(mockPersist).toHaveBeenCalledWith("ko");
    expect(mockUpdateMe).not.toHaveBeenCalled();
    expect(mockToastSuccess).toHaveBeenCalledTimes(1);
    expect(mockReload).not.toHaveBeenCalled();
    act(() => vi.advanceTimersByTime(900));
    expect(mockReload).toHaveBeenCalledTimes(1);
    expect(mockToastWarning).not.toHaveBeenCalled();
  });

  it("when not logged in: selecting Japanese persists ja + reloads, no PATCH", async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<PreferencesTab />, { wrapper: I18nWrapper });

    await pickLanguage(user, "日本語");

    expect(mockPersist).toHaveBeenCalledWith("ja");
    expect(mockUpdateMe).not.toHaveBeenCalled();
    expect(mockToastSuccess).toHaveBeenCalledTimes(1);
    expect(mockReload).not.toHaveBeenCalled();
    act(() => vi.advanceTimersByTime(900));
    expect(mockReload).toHaveBeenCalledTimes(1);
    expect(mockToastWarning).not.toHaveBeenCalled();
  });

  it("when logged in + PATCH success: confirms the save before reloading", async () => {
    userRef.current = { id: "user-1" };
    mockUpdateMe.mockResolvedValueOnce({});
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<PreferencesTab />, { wrapper: I18nWrapper });

    await pickLanguage(user, "中文");

    expect(mockPersist).toHaveBeenCalledWith("zh-Hans");
    expect(mockUpdateMe).toHaveBeenCalledWith({ language: "zh-Hans" });
    expect(mockToastWarning).not.toHaveBeenCalled();
    expect(mockToastSuccess).toHaveBeenCalledTimes(1);
    expect(mockReload).not.toHaveBeenCalled();
    act(() => vi.advanceTimersByTime(900));
    expect(mockReload).toHaveBeenCalledTimes(1);
  });

  it("when logged in + PATCH fails: shows toast and delays reload by 2.5s", async () => {
    userRef.current = { id: "user-1" };
    mockUpdateMe.mockRejectedValueOnce(new Error("network"));
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<PreferencesTab />, { wrapper: I18nWrapper });

    await pickLanguage(user, "中文");

    // Local persist still happened so the reload below sees the new locale.
    expect(mockPersist).toHaveBeenCalledWith("zh-Hans");
    expect(mockUpdateMe).toHaveBeenCalledWith({ language: "zh-Hans" });
    // Toast surfaced the sync failure.
    expect(mockToastWarning).toHaveBeenCalledTimes(1);
    // Reload deferred so the toast is visible.
    expect(mockReload).not.toHaveBeenCalled();

    act(() => {
      vi.advanceTimersByTime(2500);
    });
    expect(mockReload).toHaveBeenCalledTimes(1);
  });
});

describe("PreferencesTab — Timezone section", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    userRef.current = null;
  });

  // Base UI Select portals its popup onto document.body; unmount each
  // render fully between tests so a prior test's trigger/popup can't
  // shadow the next one's.
  afterEach(() => {
    cleanup();
  });

  // Opens the Select popup and clicks the option whose accessible name
  // matches. Re-queries the trigger each call so it operates on the
  // current render, never a stale node.
  async function pickTimezone(
    user: ReturnType<typeof userEvent.setup>,
    name: RegExp | string,
  ) {
    await user.click(screen.getByRole("combobox", { name: "Viewing Timezone" }));
    await user.click(await screen.findByRole("option", { name }));
  }

  it("renders the stored timezone in the trigger", () => {
    userRef.current = { id: "user-1", timezone: "Asia/Shanghai" };
    render(<PreferencesTab />, { wrapper: I18nWrapper });

    expect(
      screen.getByRole("combobox", { name: "Viewing Timezone" }).textContent,
    ).toContain("Asia/Shanghai");
  });

  // handleChange PATCHes then updates the store asynchronously, so the
  // post-pick assertions must waitFor it to settle. The extended timeout
  // covers querying the Select's full ~600-option IANA list on slow CI.
  it("saving a new timezone PATCHes /api/me and updates the auth store", async () => {
    userRef.current = { id: "user-1", timezone: "Asia/Shanghai" };
    const updatedUser = { id: "user-1", timezone: "Asia/Tokyo" };
    mockUpdateMe.mockResolvedValueOnce(updatedUser);
    const user = userEvent.setup();
    render(<PreferencesTab />, { wrapper: I18nWrapper });

    await pickTimezone(user, "Asia/Tokyo");

    await waitFor(() => {
      expect(mockUpdateMe).toHaveBeenCalledWith({ timezone: "Asia/Tokyo" });
      expect(mockSetUser).toHaveBeenCalledWith(updatedUser);
      expect(mockToastSuccess).toHaveBeenCalledTimes(1);
    });
  }, 20000);

  it("surfaces a toast when the PATCH fails", async () => {
    userRef.current = { id: "user-1", timezone: "Asia/Shanghai" };
    mockUpdateMe.mockRejectedValueOnce(new Error("network down"));
    const user = userEvent.setup();
    render(<PreferencesTab />, { wrapper: I18nWrapper });

    await pickTimezone(user, "Asia/Tokyo");

    await waitFor(() => {
      expect(mockUpdateMe).toHaveBeenCalledWith({ timezone: "Asia/Tokyo" });
      expect(mockToastError).toHaveBeenCalledTimes(1);
    });
    expect(mockSetUser).not.toHaveBeenCalled();
  }, 20000);

  it("clearing the preference sends an empty-string timezone", async () => {
    userRef.current = { id: "user-1", timezone: "Asia/Shanghai" };
    const clearedUser = { id: "user-1", timezone: null };
    mockUpdateMe.mockResolvedValueOnce(clearedUser);
    const user = userEvent.setup();
    render(<PreferencesTab />, { wrapper: I18nWrapper });

    // The "(browser)" sentinel option resets the preference to NULL; the
    // wire payload is an empty string the backend translates to NULL.
    await pickTimezone(user, /browser/i);

    await waitFor(() => {
      expect(mockUpdateMe).toHaveBeenCalledWith({ timezone: "" });
      // The PATCH response (timezone: null) is pushed into the auth store
      // so the picker switches back to "(browser)" without a refetch.
      expect(mockSetUser).toHaveBeenCalledWith(clearedUser);
    });
  }, 20000);
});
