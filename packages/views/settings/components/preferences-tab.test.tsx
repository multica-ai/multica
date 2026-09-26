import type { ReactNode } from "react";
import {
  describe,
  it,
  expect,
  beforeAll,
  beforeEach,
  afterAll,
  afterEach,
  vi,
} from "vitest";
import { render, screen, act, cleanup, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enAuth from "../../locales/en/auth.json";
import enSettings from "../../locales/en/settings.json";

const navigationState = vi.hoisted(() => ({ search: "", replace: vi.fn() }));
vi.mock("../../navigation", () => ({
  useNavigation: () => ({
    pathname: "/acme/settings",
    searchParams: new URLSearchParams(navigationState.search),
    replace: navigationState.replace,
  }),
}));
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
  const actual = await vi.importActual<
    typeof import("@multica/core/i18n/react")
  >("@multica/core/i18n/react");
  return {
    ...actual,
    useLocaleAdapter: () => ({
      persist: mockPersist,
      getUserChoice: () => null,
      getSystemPreferences: () => [],
    }),
  };
});

// The chat store is registered by the app shell; a callable stand-in with the
// Zustand shape is enough for the one toggle this page owns.
const chatState = vi.hoisted(() => ({
  floatingChatEnabled: true,
  setFloatingChatEnabled: vi.fn(),
}));
vi.mock("@multica/core/chat", () => ({
  useChatStore: Object.assign(
    (selector: (state: typeof chatState) => unknown) => selector(chatState),
    { getState: () => chatState },
  ),
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "ws-1", name: "Acme" }),
}));

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
    (sel?: (s: AuthState) => unknown) => (sel ? sel(state()) : state()),
    { getState: state },
  );
  return { ...actual, useAuthStore };
});

import { PreferencesTab } from "./preferences-tab";
import { useCommentComposerStore } from "@multica/core/issues/stores";
import {
  DEFAULT_MANUAL_CREATE_FIELDS,
  DEFAULT_QUICK_CREATE_FIELDS,
  useIssueCreateSettingsStore,
} from "@multica/core/issues/stores/issue-create-settings-store";


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

  it("applies the theme on this device without announcing success", async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<PreferencesTab />, { wrapper: I18nWrapper });

    const group = screen.getByRole("group", { name: "Theme" });
    expect(within(group).getByRole("button", { name: "Light" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    await user.click(within(group).getByRole("button", { name: "Dark" }));

    expect(mockSetTheme).toHaveBeenCalledWith("dark");
    expect(mockToastSuccess).not.toHaveBeenCalled();
  });

  it.each([
    { name: "한국어", locale: "ko" },
    { name: "日本語", locale: "ja" },
  ])("when not logged in: persists $locale and reloads, no PATCH", async ({ name, locale }) => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<PreferencesTab />, { wrapper: I18nWrapper });

    await pickLanguage(user, name);

    expect(mockPersist).toHaveBeenCalledWith(locale);
    expect(mockUpdateMe).not.toHaveBeenCalled();
    // The reload is the confirmation; no success toast precedes it.
    expect(mockReload).toHaveBeenCalledTimes(1);
    expect(mockToastSuccess).not.toHaveBeenCalled();
    expect(mockToastWarning).not.toHaveBeenCalled();
  });

  it.each([
    { name: "中文", locale: "zh-Hans" },
    { name: "Français", locale: "fr" },
  ])("when logged in: saves $locale before reloading", async ({ name, locale }) => {
    userRef.current = { id: "user-1" };
    mockUpdateMe.mockResolvedValueOnce({});
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<PreferencesTab />, { wrapper: I18nWrapper });

    await pickLanguage(user, name);

    expect(mockPersist).toHaveBeenCalledWith(locale);
    expect(mockUpdateMe).toHaveBeenCalledWith({ language: locale });
    expect(mockToastWarning).not.toHaveBeenCalled();
    await waitFor(() => expect(mockReload).toHaveBeenCalledTimes(1));
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

describe("PreferencesTab — Time zone", () => {
  // Shrink the picker to the curated COMMON_TIMEZONES fallback so the list
  // stays small enough for fast userEvent traversal (MUL-4427). Everything
  // these tests pick — Asia/Tokyo and the "(browser)" entry — exists in the
  // fallback list too.
  const intlWithValues = Intl as typeof Intl & {
    supportedValuesOf?: (key: "timeZone") => string[];
  };
  const realSupportedValuesOf = intlWithValues.supportedValuesOf;
  beforeAll(() => {
    intlWithValues.supportedValuesOf = () => [];
  });
  afterAll(() => {
    intlWithValues.supportedValuesOf = realSupportedValuesOf;
  });

  beforeEach(() => {
    vi.clearAllMocks();
    userRef.current = null;
  });

  afterEach(() => {
    cleanup();
  });

  async function pickTimezone(
    user: ReturnType<typeof userEvent.setup>,
    search: string,
    name: RegExp | string,
  ) {
    await user.click(screen.getByRole("button", { name: "Time zone" }));
    await user.type(
      await screen.findByPlaceholderText("Search time zones..."),
      search,
    );
    await user.click(await screen.findByRole("option", { name }));
  }

  it("renders the stored time zone in the trigger", () => {
    userRef.current = { id: "user-1", timezone: "Asia/Shanghai" };
    render(<PreferencesTab />, { wrapper: I18nWrapper });

    expect(
      screen.getByRole("button", { name: "Time zone" }).textContent,
    ).toContain("Asia/Shanghai");
  });

  it("searches the list and saves the chosen zone to the account", async () => {
    userRef.current = { id: "user-1", timezone: "Asia/Shanghai" };
    const updatedUser = { id: "user-1", timezone: "Asia/Tokyo" };
    mockUpdateMe.mockResolvedValueOnce(updatedUser);
    const user = userEvent.setup();
    render(<PreferencesTab />, { wrapper: I18nWrapper });

    await pickTimezone(user, "tokyo", /Asia\/Tokyo/);

    await waitFor(() => {
      expect(mockUpdateMe).toHaveBeenCalledWith({ timezone: "Asia/Tokyo" });
      expect(mockSetUser).toHaveBeenCalledWith(updatedUser);
    });
    expect(mockToastSuccess).not.toHaveBeenCalled();
  });

  it("surfaces a toast when the PATCH fails", async () => {
    userRef.current = { id: "user-1", timezone: "Asia/Shanghai" };
    mockUpdateMe.mockRejectedValueOnce(new Error("network down"));
    const user = userEvent.setup();
    render(<PreferencesTab />, { wrapper: I18nWrapper });

    await pickTimezone(user, "tokyo", /Asia\/Tokyo/);

    await waitFor(() => {
      expect(mockUpdateMe).toHaveBeenCalledWith({ timezone: "Asia/Tokyo" });
      expect(mockToastError).toHaveBeenCalledTimes(1);
    });
    expect(mockSetUser).not.toHaveBeenCalled();
  });

  it("clearing the preference sends an empty-string timezone", async () => {
    userRef.current = { id: "user-1", timezone: "Asia/Shanghai" };
    const clearedUser = { id: "user-1", timezone: null };
    mockUpdateMe.mockResolvedValueOnce(clearedUser);
    const user = userEvent.setup();
    render(<PreferencesTab />, { wrapper: I18nWrapper });

    // The "(browser)" entry resets the preference to NULL; the wire payload
    // is an empty string the backend translates to NULL.
    await pickTimezone(user, "browser", /browser/i);

    await waitFor(() => {
      expect(mockUpdateMe).toHaveBeenCalledWith({ timezone: "" });
      // The PATCH response (timezone: null) is pushed into the auth store
      // so the picker switches back to "(browser)" without a refetch.
      expect(mockSetUser).toHaveBeenCalledWith(clearedUser);
    });
  });
});

describe("PreferencesTab — Comments & chat", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    userRef.current = null;
    useCommentComposerStore.setState({ runningAgentReply: "steer", sticky: true });
    chatState.floatingChatEnabled = true;
  });

  afterEach(() => {
    cleanup();
  });

  it("defaults to adding the reply to the current run and saves starting after it", async () => {
    const user = userEvent.setup();
    render(<PreferencesTab />, { wrapper: I18nWrapper });

    const select = screen.getByRole("combobox", { name: "When replying to a running agent" });
    expect(select).toHaveTextContent("Add to current run");

    await user.click(select);
    await user.click(await screen.findByRole("option", { name: "Start after this run" }));

    expect(useCommentComposerStore.getState().runningAgentReply).toBe("after_run");
    expect(select).toHaveTextContent("Start after this run");
    expect(mockToastSuccess).not.toHaveBeenCalled();
  });

  it("toggles the sticky comment bar and the floating chat silently", async () => {
    const user = userEvent.setup();
    render(<PreferencesTab />, { wrapper: I18nWrapper });

    const sticky = screen.getByRole("switch", { name: "Pin comment bar to bottom" });
    expect(sticky).toHaveAttribute("aria-checked", "true");
    await user.click(sticky);
    expect(useCommentComposerStore.getState().sticky).toBe(false);

    await user.click(screen.getByRole("switch", { name: enSettings.chat.floating_label }));
    expect(chatState.setFloatingChatEnabled).toHaveBeenCalledWith(false);
    expect(mockToastSuccess).not.toHaveBeenCalled();
  });
});

describe("PreferencesTab — Create-issue fields", () => {
  function resetStore() {
    useIssueCreateSettingsStore.setState({
      quickCreateFields: DEFAULT_QUICK_CREATE_FIELDS,
      manualCreateFields: DEFAULT_MANUAL_CREATE_FIELDS,
    });
  }
  beforeEach(resetStore);
  afterEach(() => {
    cleanup();
    resetStore();
  });

  it("shows one row per field with a column per create dialog", () => {
    render(<PreferencesTab />, { wrapper: I18nWrapper });
    const table = screen.getByRole("table");
    // 7 fields; quick create supports 3 of them, manual create all 7.
    expect(within(table).getAllByRole("checkbox")).toHaveLength(10);
    expect(
      within(table).getByRole("checkbox", { name: "Project · Create with agent" }),
    ).toBeChecked();
    expect(
      within(table).getByRole("checkbox", { name: "Due date · Manual create" }),
    ).not.toBeChecked();
    // Unsupported cells explain themselves instead of rendering a dead box.
    expect(
      within(table).getAllByLabelText(enSettings.preferences.issue_fields.unsupported),
    ).toHaveLength(4);
  });

  it("persists each dialog's fields independently", async () => {
    const user = userEvent.setup();
    render(<PreferencesTab />, { wrapper: I18nWrapper });

    await user.click(screen.getByRole("checkbox", { name: "Priority · Create with agent" }));
    await user.click(screen.getByRole("checkbox", { name: "Labels · Manual create" }));

    expect(useIssueCreateSettingsStore.getState().quickCreateFields).toEqual([
      "project",
      "priority",
    ]);
    expect(useIssueCreateSettingsStore.getState().manualCreateFields).toEqual([
      "status",
      "priority",
      "assignee",
      "project",
    ]);
  });
});

describe("PreferencesTab — Scope", () => {
  afterEach(() => {
    cleanup();
  });

  it("labels where each group of settings is stored", () => {
    render(<PreferencesTab />, { wrapper: I18nWrapper });
    expect(screen.getByText("Account · synced")).toBeInTheDocument();
    expect(screen.getAllByText("This device only")).toHaveLength(2);
  });
});
