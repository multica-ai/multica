import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";

// ---------------------------------------------------------------------------
// Hoisted mocks — api + sonner
// ---------------------------------------------------------------------------

const mockGetPushPublicKey = vi.hoisted(() => vi.fn());
const mockSubscribePush = vi.hoisted(() => vi.fn());
const mockUnsubscribePush = vi.hoisted(() => vi.fn());
const mockToastSuccess = vi.hoisted(() => vi.fn());
const mockToastError = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: {
    getPushPublicKey: mockGetPushPublicKey,
    subscribePush: mockSubscribePush,
    unsubscribePush: mockUnsubscribePush,
  },
}));

vi.mock("sonner", () => ({
  toast: { success: mockToastSuccess, error: mockToastError },
}));

import { PushNotificationsSection } from "./push-notifications-section";

// 88-char string passes the VAPID_KEY_LENGTH_HINT defensive check.
const VAPID_PUB_KEY = "B".repeat(88);

// Build a mutable navigator.serviceWorker stub. Each test installs its own
// `ready` promise so we can simulate the various branches the component
// walks through. NB: we don't use vi.useFakeTimers() anywhere — fake timers
// leak across files in this worker layout and break unrelated suites that
// rely on userEvent's internal clock. To exercise the timeout branch we
// hand the component an already-rejected promise carrying the same error
// the production withTimeout would throw.
function installServiceWorkerStub(ready: Promise<unknown>) {
  // Pre-attach a swallow handler so jsdom doesn't complain about an
  // unhandled rejection while the component is still mounting.
  ready.catch(() => undefined);
  Object.defineProperty(navigator, "serviceWorker", {
    configurable: true,
    value: { ready },
  });
}

beforeEach(() => {
  mockGetPushPublicKey.mockReset();
  mockSubscribePush.mockReset();
  mockUnsubscribePush.mockReset();
  mockToastSuccess.mockClear();
  mockToastError.mockClear();

  // jsdom doesn't ship Notification or PushManager. Stub the detect-feature
  // triple so detectUnsupportedReason() returns null.
  Object.defineProperty(window, "Notification", {
    configurable: true,
    value: Object.assign(vi.fn(), {
      permission: "default",
      requestPermission: vi.fn(),
    }),
  });
  Object.defineProperty(window, "PushManager", {
    configurable: true,
    value: vi.fn(),
  });
});

describe("PushNotificationsSection", () => {
  it("falls out of the spinner into a Retry state when the SW-ready check fails", async () => {
    mockGetPushPublicKey.mockResolvedValue({ enabled: true, publicKey: VAPID_PUB_KEY });
    // Stand in for the timeout firing. The bug was: serviceWorker.ready
    // never settled at all → state stuck on the "Checking…" spinner. The
    // component now wraps that await in withTimeout, so a rejection (from
    // either the underlying call or the timeout itself) lands in the catch
    // and renders a recoverable Retry state.
    installServiceWorkerStub(
      Promise.reject(
        new Error("Background service for this page didn't start. Reload and try again."),
      ),
    );

    render(<PushNotificationsSection />);

    expect(screen.getByText(/Checking…/i)).toBeInTheDocument();

    expect(
      await screen.findByRole("button", { name: /retry/i }),
    ).toBeInTheDocument();
    expect(screen.queryByText(/Checking…/i)).not.toBeInTheDocument();
    expect(
      screen.getByText(/Background service for this page didn't start/i),
    ).toBeInTheDocument();
  });

  it("settles into Turn on when getSubscription resolves with no existing subscription", async () => {
    mockGetPushPublicKey.mockResolvedValue({ enabled: true, publicKey: VAPID_PUB_KEY });
    installServiceWorkerStub(
      Promise.resolve({
        pushManager: {
          getSubscription: vi.fn(() => Promise.resolve(null)),
        },
      }),
    );

    render(<PushNotificationsSection />);

    expect(
      await screen.findByRole("button", { name: /^turn on$/i }),
    ).toBeInTheDocument();
  });

  it("surfaces server-disabled when VAPID keys are missing", async () => {
    mockGetPushPublicKey.mockResolvedValue({ enabled: false });
    installServiceWorkerStub(Promise.resolve({})); // shouldn't be touched

    render(<PushNotificationsSection />);

    expect(
      await screen.findByText(/Push notifications are not configured/i),
    ).toBeInTheDocument();
  });

  it("surfaces denied when the browser permission is already denied", async () => {
    mockGetPushPublicKey.mockResolvedValue({ enabled: true, publicKey: VAPID_PUB_KEY });
    Object.defineProperty(window, "Notification", {
      configurable: true,
      value: Object.assign(vi.fn(), {
        permission: "denied",
        requestPermission: vi.fn(),
      }),
    });
    installServiceWorkerStub(Promise.resolve({})); // never reached

    render(<PushNotificationsSection />);

    expect(
      await screen.findByText(/You've blocked notifications/i),
    ).toBeInTheDocument();
  });
});
