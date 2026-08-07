// @vitest-environment jsdom

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { configStore } from "@multica/core/config";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";

const refs = vi.hoisted(() => ({
  user: {
    id: "user-1",
    name: "Ada",
    email: "ada@example.com",
    pushover_user_key_configured: false,
    pushover_login_codes_enabled: false,
  },
  setUser: vi.fn(),
  updatePushoverSettings: vi.fn(),
  sendPushoverTestNotification: vi.fn(),
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (state: unknown) => unknown) =>
    selector({ user: refs.user, setUser: refs.setUser }),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    updatePushoverSettings: refs.updatePushoverSettings,
    sendPushoverTestNotification: refs.sendPushoverTestNotification,
  },
}));

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

import { PushoverProfileSection } from "./pushover-profile-section";

afterEach(cleanup);

function renderSection() {
  return render(
    <I18nProvider locale="en" resources={{ en: { common: enCommon, settings: enSettings } }}>
      <PushoverProfileSection />
    </I18nProvider>,
  );
}

describe("PushoverProfileSection", () => {
  beforeEach(() => {
    refs.setUser.mockReset();
    refs.updatePushoverSettings.mockReset();
    refs.sendPushoverTestNotification.mockReset();
    refs.user.pushover_user_key_configured = false;
    refs.user.pushover_login_codes_enabled = false;
    configStore.getState().setAuthConfig({ allowSignup: true, pushoverAvailable: false });
  });

  it("is hidden when the instance has no application token", () => {
    renderSection();
    expect(screen.queryByText("User Key")).toBeNull();
  });

  it("autosaves a valid User Key and enables all notification preferences", async () => {
    const user = userEvent.setup();
    configStore.getState().setAuthConfig({ allowSignup: true, pushoverAvailable: true });
    refs.updatePushoverSettings.mockResolvedValue({
      ...refs.user,
      pushover_user_key_configured: true,
      pushover_login_codes_enabled: true,
    });
    renderSection();

    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
    await user.type(
      screen.getByLabelText("User Key"),
      "ZYXWVUTSRQPONMLKJIHGFEDCBA4321",
    );
    await user.tab();

    await waitFor(() => {
      expect(refs.updatePushoverSettings).toHaveBeenCalledWith({
        user_key: "ZYXWVUTSRQPONMLKJIHGFEDCBA4321",
        login_codes_enabled: true,
      });
    });
    expect(refs.setUser).toHaveBeenCalledOnce();
  });

  it("shows connected actions instead of the User Key field", () => {
    configStore.getState().setAuthConfig({ allowSignup: true, pushoverAvailable: true });
    refs.user.pushover_user_key_configured = true;
    refs.user.pushover_login_codes_enabled = true;

    renderSection();

    expect(screen.queryByLabelText("User Key")).toBeNull();
    expect(screen.getByRole("button", { name: "Disconnect Pushover" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Send Test Notification" })).toBeInTheDocument();
  });

  it("sends a test notification for a connected account", async () => {
    const user = userEvent.setup();
    configStore.getState().setAuthConfig({ allowSignup: true, pushoverAvailable: true });
    refs.user.pushover_user_key_configured = true;
    refs.sendPushoverTestNotification.mockResolvedValue(undefined);
    renderSection();

    await user.click(screen.getByRole("button", { name: "Send Test Notification" }));

    expect(refs.sendPushoverTestNotification).toHaveBeenCalledOnce();
  });

  it("disconnects Pushover and disables login codes", async () => {
    const user = userEvent.setup();
    configStore.getState().setAuthConfig({ allowSignup: true, pushoverAvailable: true });
    refs.user.pushover_user_key_configured = true;
    refs.user.pushover_login_codes_enabled = true;
    refs.updatePushoverSettings.mockResolvedValue({
      ...refs.user,
      pushover_user_key_configured: false,
      pushover_login_codes_enabled: false,
    });
    renderSection();

    await user.click(screen.getByRole("button", { name: "Disconnect Pushover" }));

    expect(refs.updatePushoverSettings).toHaveBeenCalledWith({
      user_key: "",
      login_codes_enabled: false,
    });
  });

  it("saves the login-code toggle immediately", async () => {
    const user = userEvent.setup();
    configStore.getState().setAuthConfig({ allowSignup: true, pushoverAvailable: true });
    refs.user.pushover_user_key_configured = true;
    refs.user.pushover_login_codes_enabled = true;
    refs.updatePushoverSettings.mockResolvedValue({
      ...refs.user,
      pushover_login_codes_enabled: false,
    });
    renderSection();

    await user.click(screen.getByRole("switch", { name: "Login codes" }));

    expect(refs.updatePushoverSettings).toHaveBeenCalledWith({
      login_codes_enabled: false,
    });
  });
});
