import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent, act, waitFor } from "@testing-library/react";

// The flag gate is unit-tested in cerebro-feature-flags; here we drive it
// directly so we can assert both states.
const { flagState } = vi.hoisted(() => ({ flagState: { enabled: true } }));
vi.mock("@multica/cerebro-feature-flags", () => ({
  useFeatureFlag: () => flagState.enabled,
}));

import { CerebroInstallPwaBanner } from "./cerebro-install-pwa-banner";

const ANDROID_UA =
  "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36";
const IPHONE_UA =
  "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1";
const DESKTOP_UA =
  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36";

function setUserAgent(ua: string) {
  Object.defineProperty(window.navigator, "userAgent", {
    value: ua,
    configurable: true,
  });
}

function fireBeforeInstallPrompt(outcome: "accepted" | "dismissed") {
  const event = new Event("beforeinstallprompt") as Event & {
    prompt?: () => Promise<void>;
    userChoice?: Promise<{ outcome: string; platform: string }>;
  };
  event.prompt = vi.fn().mockResolvedValue(undefined);
  event.userChoice = Promise.resolve({ outcome, platform: "web" });
  act(() => {
    window.dispatchEvent(event);
  });
  return event;
}

beforeEach(() => {
  flagState.enabled = true;
  window.localStorage.clear();
});

afterEach(() => {
  setUserAgent(DESKTOP_UA);
});

describe("CerebroInstallPwaBanner", () => {
  it("renders nothing when the feature flag is off", () => {
    flagState.enabled = false;
    setUserAgent(ANDROID_UA);
    render(<CerebroInstallPwaBanner />);
    expect(screen.queryByTestId("install-pwa-banner")).toBeNull();
  });

  it("renders nothing on desktop", () => {
    setUserAgent(DESKTOP_UA);
    render(<CerebroInstallPwaBanner />);
    expect(screen.queryByTestId("install-pwa-banner")).toBeNull();
  });

  it("shows the banner with menu guidance on Android before the prompt fires", () => {
    setUserAgent(ANDROID_UA);
    render(<CerebroInstallPwaBanner />);
    expect(screen.getByTestId("install-pwa-banner")).toBeTruthy();
    const how = screen.getByTestId("install-pwa-banner-how");
    fireEvent.click(how);
    const guide = screen.getByTestId("install-pwa-banner-guide");
    expect(guide.textContent).toContain("Install app");
  });

  it("fires the native install prompt on Android once beforeinstallprompt is captured", async () => {
    setUserAgent(ANDROID_UA);
    render(<CerebroInstallPwaBanner />);
    const event = fireBeforeInstallPrompt("accepted");
    const installBtn = await screen.findByTestId("install-pwa-banner-install");
    await act(async () => {
      fireEvent.click(installBtn);
    });
    expect(event.prompt).toHaveBeenCalledTimes(1);
    await waitFor(() =>
      expect(screen.queryByTestId("install-pwa-banner")).toBeNull(),
    );
  });

  it("shows iOS Share → Add to Home Screen steps", () => {
    setUserAgent(IPHONE_UA);
    render(<CerebroInstallPwaBanner />);
    fireEvent.click(screen.getByTestId("install-pwa-banner-how"));
    const guide = screen.getByTestId("install-pwa-banner-guide");
    expect(guide.textContent).toContain("Add to Home Screen");
  });

  it("can be dismissed and stays hidden on re-render", () => {
    setUserAgent(ANDROID_UA);
    const { unmount } = render(<CerebroInstallPwaBanner />);
    fireEvent.click(screen.getByTestId("install-pwa-banner-dismiss"));
    expect(screen.queryByTestId("install-pwa-banner")).toBeNull();
    unmount();
    render(<CerebroInstallPwaBanner />);
    expect(screen.queryByTestId("install-pwa-banner")).toBeNull();
  });
});
