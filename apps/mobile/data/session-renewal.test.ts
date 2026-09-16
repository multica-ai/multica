// @vitest-environment node
import { beforeEach, describe, expect, it, vi } from "vitest";

// The Keychain is async, and that is the whole point of these tests: `defer`
// lets one operation be started and finished at a chosen moment, so a logout
// and a renewal can be interleaved in either order.
const keychain = vi.hoisted(() => ({
  value: null as string | null,
  // When held, the operation suspends on entry and stays there until the test
  // releases it. `onSetEnter` fires at that moment, so a test can await the
  // exact instant a write is in flight instead of guessing at microtask
  // counts — the difference between pinning an interleaving and hoping for one.
  holdSet: false,
  releaseSet: null as null | (() => void),
  onSetEnter: null as null | (() => void),
  holdClear: false,
  releaseClear: null as null | (() => void),
}));

// The real module is used, not a hand-written stand-in: its serialization is
// the mechanism under test here, so replacing it would test nothing. Only
// expo-secure-store underneath is faked, with hold/release so a test can pin
// an interleaving instead of guessing at microtask counts.
vi.mock("expo-secure-store", () => ({
  getItemAsync: vi.fn(async () => keychain.value),
  setItemAsync: vi.fn(async (_key: string, token: string) => {
    if (keychain.holdSet) {
      await new Promise<void>((resolve) => {
        keychain.releaseSet = resolve;
        keychain.onSetEnter?.();
      });
    }
    keychain.value = token;
  }),
  deleteItemAsync: vi.fn(async () => {
    if (keychain.holdClear) {
      await new Promise<void>((resolve) => {
        keychain.releaseClear = resolve;
      });
    }
    keychain.value = null;
  }),
}));

function resetKeychain() {
  keychain.value = "token-v1";
  keychain.holdSet = false;
  keychain.releaseSet = null;
  keychain.onSetEnter = null;
  keychain.holdClear = false;
  keychain.releaseClear = null;
}

const apiMock = vi.hoisted(() => ({
  refreshSession: vi.fn(),
  setToken: vi.fn(),
}));

vi.mock("./api", async () => {
  class ApiError extends Error {
    constructor(
      message: string,
      readonly status: number,
    ) {
      super(message);
      this.name = "ApiError";
    }
  }
  return { api: apiMock, ApiError };
});

import { ApiError } from "./api";
import {
  maybeRenewSession,
  renewSessionNow,
  resetSessionRenewalForTest,
} from "./session-renewal";
import { invalidateSessionEpoch } from "./session-epoch";
import { sessionActivityResponderConfig } from "./session-activity";
import * as SecureStore from "expo-secure-store";
import { clearToken, getToken, setToken } from "./secure-storage";

function renewed(token: string, checkAgainInSeconds = 3600) {
  return {
    token,
    expires_at: "2026-10-16T00:00:00Z",
    renewed: true,
    check_again_in_seconds: checkAgainInSeconds,
  };
}

function notYet(checkAgainInSeconds = 3600) {
  return {
    expires_at: "2026-10-16T00:00:00Z",
    renewed: false,
    check_again_in_seconds: checkAgainInSeconds,
  };
}

describe("mobile session renewal", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    resetKeychain();
    resetSessionRenewalForTest();
  });

  it("writes a renewed token to the Keychain and the API client", async () => {
    apiMock.refreshSession.mockResolvedValue(renewed("token-v2"));

    await renewSessionNow();

    expect(await getToken()).toBe("token-v2");
    expect(apiMock.setToken).toHaveBeenCalledWith("token-v2");
  });

  it("leaves the session alone when the server says it is not time yet", async () => {
    apiMock.refreshSession.mockResolvedValue(notYet());

    await renewSessionNow();

    expect(await getToken()).toBe("token-v1");
    expect(vi.mocked(SecureStore.setItemAsync)).not.toHaveBeenCalled();
  });

  // Launch and a foreground transition land together all the time on iOS.
  it("coalesces concurrent renewals into one request", async () => {
    let resolve!: (value: unknown) => void;
    apiMock.refreshSession.mockReturnValue(
      new Promise((r) => {
        resolve = r;
      }),
    );

    const first = renewSessionNow();
    const second = renewSessionNow();
    resolve(renewed("token-v2"));
    await Promise.all([first, second]);

    expect(apiMock.refreshSession).toHaveBeenCalledTimes(1);
  });

  // Phones background constantly. A timer would keep the session of someone
  // who stopped opening the app alive forever; the interval gate is what
  // makes foreground checks safe to fire on every activation.
  it("declines a repeat check until the server-supplied interval elapses", async () => {
    apiMock.refreshSession.mockResolvedValue(notYet(3600));

    await renewSessionNow();
    for (let i = 0; i < 20; i++) maybeRenewSession();
    await Promise.resolve();

    expect(apiMock.refreshSession).toHaveBeenCalledTimes(1);
  });

  it.each([
    ["offline", new TypeError("Network request failed")],
    ["server error", new ApiError("boom", 500)],
  ])("keeps the current session when renewal fails (%s)", async (_name, err) => {
    apiMock.refreshSession.mockRejectedValue(err);

    await renewSessionNow();

    expect(await getToken()).toBe("token-v1");
    expect(apiMock.setToken).not.toHaveBeenCalled();
  });

  // A failure must not consume the normal cadence. With the 30-day default
  // that cadence is three days, so a single 503 near the end of a session
  // would otherwise push the next attempt past the expiry it was trying to
  // prevent.
  it("retries within seconds after a failure, not after the server cadence", async () => {
    apiMock.refreshSession.mockResolvedValue(notYet(3 * 24 * 60 * 60));
    await renewSessionNow();
    expect(apiMock.refreshSession).toHaveBeenCalledTimes(1);

    apiMock.refreshSession.mockRejectedValue(new TypeError("Network request failed"));
    const start = Date.now();
    vi.spyOn(Date, "now").mockReturnValue(start + 3 * 24 * 60 * 60 * 1000);
    await renewSessionNow();
    expect(apiMock.refreshSession).toHaveBeenCalledTimes(2);

    // Connectivity returns a second later and the user is still working.
    apiMock.refreshSession.mockResolvedValue(notYet(3 * 24 * 60 * 60));
    vi.mocked(Date.now).mockReturnValue(start + 3 * 24 * 60 * 60 * 1000 + 1_500);
    maybeRenewSession();
    await vi.waitFor(() =>
      expect(apiMock.refreshSession).toHaveBeenCalledTimes(3),
    );
    vi.mocked(Date.now).mockRestore();
  });

  // A 200 whose body failed schema validation comes back as zeroes rather
  // than as a throw; without treating it as a failure the deadline never
  // moves and every touch fires another request.
  it("backs off on a response that carries no cadence", async () => {
    apiMock.refreshSession.mockResolvedValue({
      expires_at: "",
      renewed: false,
      check_again_in_seconds: 0,
    });

    const start = Date.now();
    vi.spyOn(Date, "now").mockReturnValue(start);
    await renewSessionNow();
    expect(apiMock.refreshSession).toHaveBeenCalledTimes(1);

    for (let i = 0; i < 20; i++) maybeRenewSession();
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(apiMock.refreshSession).toHaveBeenCalledTimes(1);

    vi.mocked(Date.now).mockReturnValue(start + 1_100);
    maybeRenewSession();
    await vi.waitFor(() =>
      expect(apiMock.refreshSession).toHaveBeenCalledTimes(2),
    );
    vi.mocked(Date.now).mockRestore();
  });

  it("discards a response that arrives after sign-out", async () => {
    let resolve!: (value: unknown) => void;
    apiMock.refreshSession.mockReturnValue(
      new Promise((r) => {
        resolve = r;
      }),
    );

    const pending = renewSessionNow();
    keychain.value = null; // logout cleared the Keychain mid-flight

    resolve(renewed("token-v2"));
    await pending;

    expect(await getToken()).toBeNull();
    expect(apiMock.setToken).not.toHaveBeenCalled();
  });

  it("does nothing when there is no session to extend", async () => {
    keychain.value = null;

    await renewSessionNow();

    expect(apiMock.refreshSession).not.toHaveBeenCalled();
  });
});

// Logging out is not instantaneous on mobile: `clearToken` is a Keychain
// write, so between "the user tapped Sign out" and "the token is gone" there
// is a window where a read still returns it. A renewal that sampled storage in
// that window would write its replacement back and sign the user straight
// in again, on the device they just signed out of.
//
// Both completion orders are covered, because the two guards catch different
// halves: the epoch is true the instant logout starts, the token comparison
// only once the delete lands.
/**
 * Starts a renewal and returns once its request is genuinely in flight — i.e.
 * once the early "is this session still current?" checks have already passed.
 *
 * Without waiting for that, a test that logs out immediately is caught by
 * those early checks and never reaches the commit, so it would pass whether
 * or not the commit guards the write at all.
 */
async function renewalInFlight(): Promise<{
  pending: Promise<void>;
  respond: (value: unknown) => void;
}> {
  let respond!: (value: unknown) => void;
  let requestStarted!: () => void;
  const started = new Promise<void>((resolve) => {
    requestStarted = resolve;
  });
  apiMock.refreshSession.mockImplementation(() => {
    requestStarted();
    return new Promise((resolve) => {
      respond = resolve;
    });
  });

  const pending = renewSessionNow();
  await started;
  return { pending, respond };
}

describe("mobile session renewal — logout races", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    resetKeychain();
    resetSessionRenewalForTest();
  });

  it("refuses the result when logout has STARTED but its delete has not landed", async () => {
    const { pending, respond } = await renewalInFlight();

    // Logout begins: the epoch moves synchronously and the Keychain delete is
    // left in flight, so a plain read would still return the old token.
    keychain.holdClear = true;
    invalidateSessionEpoch();
    const logout = clearToken();

    respond(renewed("token-v2"));

    // The renewal's commit queues behind the in-flight delete — that is the
    // serialization doing its job. Release the delete so both can finish.
    keychain.holdClear = false;
    keychain.releaseClear?.();
    await Promise.all([logout, pending]);

    expect(await getToken()).toBeNull();
    expect(apiMock.setToken).not.toHaveBeenCalledWith("token-v2");
  });

  // The three-party case: a stale renewal must not disturb a session that
  // started AFTER it. Refusing has to mean "write nothing" — deleting instead
  // would wipe the new account's credential and leave the app logged in in
  // memory but signed out on the next launch.
  it("leaves a newer sign-in untouched when a stale renewal lands last", async () => {
    const { pending, respond } = await renewalInFlight();

    // Logout, then a fresh sign-in as someone else — both complete while the
    // renewal is still waiting on its response.
    invalidateSessionEpoch();
    await clearToken();
    invalidateSessionEpoch();
    await setToken("new-account-token");

    respond(renewed("stale-token-v2"));
    await pending;

    expect(await getToken()).toBe("new-account-token");
    expect(apiMock.setToken).not.toHaveBeenCalledWith("stale-token-v2");
  });

  // Same shape, but the stale renewal's own write is the one still pending
  // when the newer session arrives. Serialization means it cannot land in the
  // middle of the sign-in; it is simply refused when its turn comes.
  it("refuses a stale renewal even when its write is the last to be queued", async () => {
    const { pending, respond } = await renewalInFlight();

    invalidateSessionEpoch();
    const logout = clearToken();
    const login = setToken("new-account-token");
    respond(renewed("stale-token-v2"));

    await Promise.all([logout, login, pending]);

    expect(await getToken()).toBe("new-account-token");
    expect(apiMock.setToken).not.toHaveBeenCalledWith("stale-token-v2");
  });

  it("still applies the renewal when no logout happened", async () => {
    apiMock.refreshSession.mockResolvedValue(renewed("token-v2"));

    await renewSessionNow();

    expect(await getToken()).toBe("token-v2");
    expect(apiMock.setToken).toHaveBeenCalledWith("token-v2");
  });
});

// Launch and foreground alone leave a gap: an app opened once and then used
// continuously in the foreground for longer than the check interval would
// never check again and could expire while the user was mid-sentence.
describe("session activity responder", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    resetKeychain();
    resetSessionRenewalForTest();
  });

  it("checks on a touch and never claims the gesture", async () => {
    apiMock.refreshSession.mockResolvedValue(notYet(3600));

    const claimed =
      sessionActivityResponderConfig.onStartShouldSetPanResponderCapture();

    // Returning true would make the wrapper the responder and swallow every
    // tap, scroll and swipe in the app.
    expect(claimed).toBe(false);
    await vi.waitFor(() =>
      expect(apiMock.refreshSession).toHaveBeenCalledTimes(1),
    );
  });

  it("throttles to the server-supplied cadence, so touches are not requests", async () => {
    apiMock.refreshSession.mockResolvedValue(notYet(3600));

    sessionActivityResponderConfig.onStartShouldSetPanResponderCapture();
    await vi.waitFor(() =>
      expect(apiMock.refreshSession).toHaveBeenCalledTimes(1),
    );

    for (let i = 0; i < 100; i++) {
      expect(
        sessionActivityResponderConfig.onStartShouldSetPanResponderCapture(),
      ).toBe(false);
    }
    await Promise.resolve();

    expect(apiMock.refreshSession).toHaveBeenCalledTimes(1);
  });
});
