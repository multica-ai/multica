import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor, act } from "@testing-library/react";

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

// CEREBRO-PATCH(web-push-flag-gate-test): bypass the cerebro_web_push gate so
// the test renders the inner subscription UI without needing a QueryClient.
vi.mock("@multica/cerebro-feature-flags", () => ({
  useFeatureFlag: () => true,
}));

import { PushNotificationsSection } from "./push-notifications-section";

// 88-char string passes the VAPID_KEY_LENGTH_HINT defensive check.
const VAPID_PUB_KEY = "B".repeat(88);

// Build a mutable navigator.serviceWorker stub. Each test installs its own
// `ready` promise so we can simulate hangs, immediate resolutions, etc.
function installServiceWorkerStub(ready: Promise<unknown>) {
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

  // jsdom doesn't ship Notification or PushManager by default. Stub the
  // detect-feature triple so detectUnsupportedReason() returns null.
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

afterEach(() => {
  vi.useRealTimers();
});

describe("PushNotificationsSection — hung service-worker recovery", () => {
  it("falls out of the spinner into a Retry state when serviceWorker.ready never resolves", async () => {
    vi.useFakeTimers();

    mockGetPushPublicKey.mockResolvedValue({ enabled: true, publicKey: VAPID_PUB_KEY });
    // Hung promise — never resolves. The 8s timeout should kick in.
    installServiceWorkerStub(new Promise(() => {}));

    render(<PushNotificationsSection />);

    // Initially in the loading "Checking…" state.
    expect(screen.getByText(/Checking…/i)).toBeInTheDocument();

    // Advance past the SW_READY_TIMEOUT_MS (8000ms). advanceTimersByTimeAsync
    // flushes microtasks between timer ticks so the rejection's catch block
    // gets a chance to setState before we assert.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(8500);
    });
    vi.useRealTimers();

    await waitFor(() => {
      expect(screen.getByRole("button", { name: /retry/i })).toBeInTheDocument();
    });
    // Spinner gone.
    expect(screen.queryByText(/Checking…/i)).not.toBeInTheDocument();
    // Hint message surfaces the recoverable cause to the user.
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
    installServiceWorkerStub(new Promise(() => {})); // shouldn't be touched

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
    installServiceWorkerStub(new Promise(() => {})); // never reached

    render(<PushNotificationsSection />);

    expect(
      await screen.findByText(/You've blocked notifications/i),
    ).toBeInTheDocument();
  });
});
