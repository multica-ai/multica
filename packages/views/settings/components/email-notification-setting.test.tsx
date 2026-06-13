import type { ReactNode } from "react";
import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import { configStore } from "@multica/core/config";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";

// ---------------------------------------------------------------------------
// Preference data controlled per test
// ---------------------------------------------------------------------------
const prefsRef = vi.hoisted(() => ({
  current: {} as Record<string, string>,
}));

const mockMutate = vi.hoisted(() => vi.fn());

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({
    data: { workspace_id: "ws-1", preferences: prefsRef.current },
  }),
  useMutation: (_opts: unknown) => ({
    mutate: (
      prefs: Record<string, string>,
      callbacks?: { onError?: (e: unknown) => void },
    ) => {
      mockMutate(prefs, callbacks);
    },
    isPending: false,
  }),
  useQueryClient: () => ({
    cancelQueries: vi.fn(),
    getQueryData: vi.fn(),
    setQueryData: vi.fn(),
    invalidateQueries: vi.fn(),
  }),
  queryOptions: <T,>(opts: T) => opts,
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/notification-preferences/queries", () => ({
  notificationPreferenceOptions: (wsId: string) => ({
    queryKey: ["notification-preferences", wsId],
    queryFn: vi.fn(),
  }),
  notificationPreferenceKeys: {
    all: (wsId: string) => ["notification-preferences", wsId],
  },
}));

vi.mock("@multica/core/notification-preferences/mutations", () => ({
  useUpdateNotificationPreferences: () => ({
    mutate: (
      prefs: Record<string, string>,
      callbacks?: { onError?: (e: unknown) => void },
    ) => {
      mockMutate(prefs, callbacks);
    },
    isPending: false,
  }),
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

// ---------------------------------------------------------------------------
// Import component AFTER mocks are hoisted
// ---------------------------------------------------------------------------
import { EmailNotificationSetting } from "./email-notification-setting";

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------
const TEST_RESOURCES = { en: { common: enCommon, settings: enSettings } };

function Wrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

function resetState() {
  vi.clearAllMocks();
  prefsRef.current = {};
  configStore.setState({ emailConfigured: false });
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------
describe("EmailNotificationSetting", () => {
  beforeEach(resetState);

  it("shows Unavailable badge and no toggle when emailConfigured is false", () => {
    configStore.setState({ emailConfigured: false });
    render(<EmailNotificationSetting />, { wrapper: Wrapper });

    expect(screen.getByText("Email notifications")).toBeTruthy();
    expect(
      screen.getByText(
        "Email isn't configured on this server. Set SMTP_HOST or RESEND_API_KEY to enable.",
      ),
    ).toBeTruthy();
    expect(screen.getByText("Unavailable")).toBeTruthy();
    expect(screen.queryByRole("switch")).toBeNull();
  });

  it("renders toggle checked when emailConfigured is true and email_notifications is not muted", () => {
    configStore.setState({ emailConfigured: true });
    prefsRef.current = {}; // no key → default enabled
    render(<EmailNotificationSetting />, { wrapper: Wrapper });

    expect(
      screen.getByText("You'll get an email when there's new inbox activity."),
    ).toBeTruthy();
    const toggle = screen.getByRole("switch");
    expect(toggle).toBeTruthy();
    expect((toggle as HTMLInputElement).getAttribute("aria-checked") ?? (toggle as HTMLInputElement).checked).toBeTruthy();
  });

  it("renders toggle unchecked when emailConfigured is true and email_notifications is muted", () => {
    configStore.setState({ emailConfigured: true });
    prefsRef.current = { email_notifications: "muted" };
    render(<EmailNotificationSetting />, { wrapper: Wrapper });

    const toggle = screen.getByRole("switch");
    expect(toggle).toBeTruthy();
    // aria-checked should be "false" when preference is muted
    const ariaChecked = toggle.getAttribute("aria-checked");
    expect(ariaChecked).toBe("false");
  });

  it("calls mutate with email_notifications deleted when toggled on", async () => {
    configStore.setState({ emailConfigured: true });
    prefsRef.current = { email_notifications: "muted" };
    const user = userEvent.setup();
    render(<EmailNotificationSetting />, { wrapper: Wrapper });

    const toggle = screen.getByRole("switch");
    await user.click(toggle);

    expect(mockMutate).toHaveBeenCalledTimes(1);
    const [prefs] = mockMutate.mock.calls[0] as [Record<string, string>];
    expect("email_notifications" in prefs).toBe(false);
  });

  it("calls mutate with email_notifications=muted when toggled off", async () => {
    configStore.setState({ emailConfigured: true });
    prefsRef.current = {}; // currently enabled
    const user = userEvent.setup();
    render(<EmailNotificationSetting />, { wrapper: Wrapper });

    const toggle = screen.getByRole("switch");
    await user.click(toggle);

    expect(mockMutate).toHaveBeenCalledTimes(1);
    const [prefs] = mockMutate.mock.calls[0] as [Record<string, string>];
    expect(prefs.email_notifications).toBe("muted");
  });
});
