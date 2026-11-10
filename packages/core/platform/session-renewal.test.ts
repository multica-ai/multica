/**
 * @vitest-environment jsdom
 */
// jsdom rather than node: watchSessionActivity binds to window/document, and
// under node it would take the `typeof window === "undefined"` branch and
// "pass" without ever touching the listeners this file is about.
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError, type ApiClient } from "../api/client";
import type { StorageAdapter } from "../types/storage";
import type { RefreshSessionResponse } from "../api/schemas";
import {
  createSessionRenewal,
  isRenewableSessionToken,
  watchSessionActivity,
} from "./session-renewal";

const TOKEN_KEY = "multica_token";
// Shaped like the server's session JWTs; renewal only runs for that shape.
const SESSION_V1 = "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ2MSJ9.c2lnLXYx";
const SESSION_V2 = "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ2MiJ9.c2lnLXYy";

function makeStorage(
  initial: Record<string, string> = {},
): StorageAdapter & { snapshot: () => Record<string, string> } {
  const values = { ...initial };
  return {
    getItem: (key) => values[key] ?? null,
    setItem: (key, value) => {
      values[key] = value;
    },
    removeItem: (key) => {
      delete values[key];
    },
    snapshot: () => ({ ...values }),
  };
}

function renewedResponse(
  token: string,
  checkAgainInSeconds = 3600,
): RefreshSessionResponse {
  return {
    token,
    expires_at: "2026-10-16T00:00:00Z",
    renewed: true,
    check_again_in_seconds: checkAgainInSeconds,
  };
}

function notYetResponse(checkAgainInSeconds = 3600): RefreshSessionResponse {
  return {
    expires_at: "2026-10-16T00:00:00Z",
    renewed: false,
    check_again_in_seconds: checkAgainInSeconds,
  };
}

interface Harness {
  api: { refreshSession: ReturnType<typeof vi.fn>; setToken: ReturnType<typeof vi.fn> };
  storage: ReturnType<typeof makeStorage>;
  authenticated: { value: boolean };
}

function makeHarness(initialToken: string | null = SESSION_V1): Harness {
  return {
    api: { refreshSession: vi.fn(), setToken: vi.fn() },
    storage: makeStorage(initialToken ? { [TOKEN_KEY]: initialToken } : {}),
    authenticated: { value: true },
  };
}

function renewalFor(h: Harness) {
  return createSessionRenewal({
    api: h.api as unknown as ApiClient,
    storage: h.storage,
    isAuthenticated: () => h.authenticated.value,
  });
}

describe("createSessionRenewal", () => {
  beforeEach(() => {
    vi.useRealTimers();
  });

  it("persists a renewed token to storage and to the API client", async () => {
    const h = makeHarness();
    h.api.refreshSession.mockResolvedValue(renewedResponse(SESSION_V2));

    await renewalFor(h).renewNow();

    expect(h.storage.snapshot()[TOKEN_KEY]).toBe(SESSION_V2);
    expect(h.api.setToken).toHaveBeenCalledWith(SESSION_V2);
  });

  // The common answer. Asking before the window opens must change nothing —
  // in particular it must not clear or rewrite the token in play.
  it("leaves the session alone when the server says it is not time yet", async () => {
    const h = makeHarness();
    h.api.refreshSession.mockResolvedValue(notYetResponse());

    await renewalFor(h).renewNow();

    expect(h.storage.snapshot()[TOKEN_KEY]).toBe(SESSION_V1);
    expect(h.api.setToken).not.toHaveBeenCalled();
  });

  // A foreground event and a click can land in the same tick. Two renewals
  // would produce two valid tokens racing to be the one stored.
  it("coalesces concurrent renewals into one request", async () => {
    const h = makeHarness();
    let resolve!: (value: RefreshSessionResponse) => void;
    h.api.refreshSession.mockReturnValue(
      new Promise<RefreshSessionResponse>((r) => {
        resolve = r;
      }),
    );

    const renewal = renewalFor(h);
    const first = renewal.renewNow();
    const second = renewal.renewNow();
    renewal.maybeRenew();

    resolve(renewedResponse(SESSION_V2));
    await Promise.all([first, second]);

    expect(h.api.refreshSession).toHaveBeenCalledTimes(1);
    expect(h.storage.snapshot()[TOKEN_KEY]).toBe(SESSION_V2);
  });

  // The whole point of the design: an app nobody is touching does not keep
  // its own session alive. maybeRenew is wired to activity, so it has to
  // decline until the server-supplied interval has actually elapsed.
  it("declines a repeat check until the server-supplied interval elapses", async () => {
    const h = makeHarness();
    h.api.refreshSession.mockResolvedValue(notYetResponse(3600));

    const renewal = renewalFor(h);
    await renewal.renewNow();
    expect(h.api.refreshSession).toHaveBeenCalledTimes(1);

    // Simulate a busy user: many activity events, well inside the interval.
    for (let i = 0; i < 50; i++) renewal.maybeRenew();
    await Promise.resolve();

    expect(h.api.refreshSession).toHaveBeenCalledTimes(1);
  });

  // A failure must not consume the normal cadence. This is the ordinary-case
  // version of the bug, not a short-TTL curiosity: a client told "check again
  // in 3 days", returning with one day of session left and hitting a single
  // 503, would otherwise not be allowed to retry until two days after the
  // session it was trying to save had expired.
  it("retries within seconds after a failure, not after the server cadence", async () => {
    const h = makeHarness();
    // Establish a long cadence the way a real client would: one good check.
    h.api.refreshSession.mockResolvedValue(notYetResponse(3 * 24 * 60 * 60));
    const renewal = renewalFor(h);
    await renewal.renewNow();
    expect(h.api.refreshSession).toHaveBeenCalledTimes(1);

    // Now the network blips.
    h.api.refreshSession.mockRejectedValue(new TypeError("Failed to fetch"));
    const now = Date.now();
    vi.spyOn(Date, "now").mockReturnValue(now + 3 * 24 * 60 * 60 * 1000);
    await renewal.renewNow();
    expect(h.api.refreshSession).toHaveBeenCalledTimes(2);

    // It recovers a second later and the user is still working. The next
    // attempt must go out now, not in another three days.
    h.api.refreshSession.mockResolvedValue(notYetResponse(3 * 24 * 60 * 60));
    vi.mocked(Date.now).mockReturnValue(now + 3 * 24 * 60 * 60 * 1000 + 1_500);
    renewal.maybeRenew();
    await vi.waitFor(() =>
      expect(h.api.refreshSession).toHaveBeenCalledTimes(3),
    );
    vi.mocked(Date.now).mockRestore();
  });

  // A 200 whose body failed schema validation comes back through
  // parseWithFallback as zeroes rather than as a throw. Without treating that
  // as a failure the deadline never moves, and every click fires another
  // request at a server that is already answering badly.
  it("backs off on a response that carries no cadence", async () => {
    const h = makeHarness();
    h.api.refreshSession.mockResolvedValue({
      expires_at: "",
      renewed: false,
      check_again_in_seconds: 0,
    } satisfies RefreshSessionResponse);

    const renewal = renewalFor(h);
    const start = Date.now();
    vi.spyOn(Date, "now").mockReturnValue(start);
    await renewal.renewNow();
    expect(h.api.refreshSession).toHaveBeenCalledTimes(1);

    // Busy user, well inside the backoff: no further requests.
    for (let i = 0; i < 20; i++) renewal.maybeRenew();
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(h.api.refreshSession).toHaveBeenCalledTimes(1);

    // And it does come back, on the backoff rather than never.
    vi.mocked(Date.now).mockReturnValue(start + 1_100);
    renewal.maybeRenew();
    await vi.waitFor(() =>
      expect(h.api.refreshSession).toHaveBeenCalledTimes(2),
    );
    vi.mocked(Date.now).mockRestore();
  });

  // Recovering fast must not mean hammering a server that is genuinely down.
  it("backs off across consecutive failures and resets on success", async () => {
    const h = makeHarness();
    h.api.refreshSession.mockRejectedValue(new TypeError("Failed to fetch"));
    const renewal = renewalFor(h);

    const start = Date.now();
    const at = (ms: number) => vi.mocked(Date.now).mockReturnValue(start + ms);
    // Let the promise chain — including the finally that clears `inFlight` —
    // settle, so the next step is not blocked by the previous one.
    const settle = async () => {
      await new Promise((resolve) => setTimeout(resolve, 0));
    };
    vi.spyOn(Date, "now").mockReturnValue(start);

    at(0);
    await renewal.renewNow(); // failure 1 → retry allowed 1s later
    expect(h.api.refreshSession).toHaveBeenCalledTimes(1);

    at(999);
    renewal.maybeRenew();
    await settle();
    expect(h.api.refreshSession).toHaveBeenCalledTimes(1);

    at(1_000);
    await renewal.renewNow(); // failure 2 → the delay has doubled to 2s
    expect(h.api.refreshSession).toHaveBeenCalledTimes(2);

    at(2_999);
    renewal.maybeRenew();
    await settle();
    expect(h.api.refreshSession).toHaveBeenCalledTimes(2);

    at(3_000);
    renewal.maybeRenew();
    await settle();
    expect(h.api.refreshSession).toHaveBeenCalledTimes(3);

    // A success clears the streak and installs the server's cadence...
    h.api.refreshSession.mockResolvedValue(notYetResponse(3600));
    at(10_000);
    await renewal.renewNow();
    expect(h.api.refreshSession).toHaveBeenCalledTimes(4);

    // ...so the next failure starts from one second again, not from where the
    // earlier streak left off.
    h.api.refreshSession.mockRejectedValue(new TypeError("Failed to fetch"));
    at(4_000_000);
    await renewal.renewNow();
    expect(h.api.refreshSession).toHaveBeenCalledTimes(5);

    at(4_000_999);
    renewal.maybeRenew();
    await settle();
    expect(h.api.refreshSession).toHaveBeenCalledTimes(5);

    at(4_001_000);
    renewal.maybeRenew();
    await settle();
    expect(h.api.refreshSession).toHaveBeenCalledTimes(6);

    vi.mocked(Date.now).mockRestore();
  });

  // The server computes a cadence that fits inside the renewal window, which
  // for a short AUTH_TOKEN_TTL can be seconds. A client-side floor above that
  // value would silently override the server and let an actively used session
  // expire, so the floor has to stay below anything the server can send.
  it("honours a short server-supplied cadence instead of flooring it", async () => {
    const h = makeHarness();
    h.api.refreshSession.mockResolvedValue(notYetResponse(10));

    const renewal = renewalFor(h);
    await renewal.renewNow();

    const now = Date.now();
    vi.spyOn(Date, "now").mockReturnValue(now + 11_000);
    renewal.maybeRenew();
    await vi.waitFor(() =>
      expect(h.api.refreshSession).toHaveBeenCalledTimes(2),
    );
    vi.mocked(Date.now).mockRestore();
  });

  it("checks again once the interval has passed", async () => {
    const h = makeHarness();
    h.api.refreshSession.mockResolvedValue(notYetResponse(60));

    const renewal = renewalFor(h);
    await renewal.renewNow();

    const now = Date.now();
    vi.spyOn(Date, "now").mockReturnValue(now + 61_000);
    renewal.maybeRenew();
    await vi.waitFor(() =>
      expect(h.api.refreshSession).toHaveBeenCalledTimes(2),
    );
    vi.mocked(Date.now).mockRestore();
  });

  // MUL-7028 made session teardown clear every client-side store — drafts,
  // tab layout, the Query cache. A renewal that treats a network blip as the
  // end of the session would fire all of that on a flaky connection.
  it.each([
    ["offline", new TypeError("Failed to fetch")],
    ["server error", new ApiError("boom", 500, "Internal Server Error")],
    ["gateway error", new ApiError("bad gateway", 502, "Bad Gateway")],
  ])("keeps the current session when renewal fails (%s)", async (_name, err) => {
    const h = makeHarness();
    h.api.refreshSession.mockRejectedValue(err);

    await renewalFor(h).renewNow();

    expect(h.storage.snapshot()[TOKEN_KEY]).toBe(SESSION_V1);
    expect(h.api.setToken).not.toHaveBeenCalled();
  });

  // A 401 IS handled — by ApiClient, before this code sees the rejection.
  // What matters here is that the renewal path does not additionally clear
  // anything, which would race the teardown already under way.
  it("does not tear anything down itself on a 401", async () => {
    const h = makeHarness();
    h.api.refreshSession.mockRejectedValue(
      new ApiError("invalid token", 401, "Unauthorized"),
    );

    await renewalFor(h).renewNow();

    expect(h.api.setToken).not.toHaveBeenCalled();
    // Storage is the auth store's to clear, and it already did.
    expect(h.storage.snapshot()[TOKEN_KEY]).toBe(SESSION_V1);
  });

  // Log out while a renewal is in flight and the response lands afterwards.
  // Writing the token back would resurrect a session the app has torn down.
  it("discards a response that arrives after logout", async () => {
    const h = makeHarness();
    let resolve!: (value: RefreshSessionResponse) => void;
    h.api.refreshSession.mockReturnValue(
      new Promise<RefreshSessionResponse>((r) => {
        resolve = r;
      }),
    );

    const pending = renewalFor(h).renewNow();

    // Logout, exactly as the auth store does it.
    h.storage.removeItem(TOKEN_KEY);
    h.authenticated.value = false;

    resolve(renewedResponse(SESSION_V2));
    await pending;

    expect(h.storage.snapshot()[TOKEN_KEY]).toBeUndefined();
    expect(h.api.setToken).not.toHaveBeenCalled();
  });

  // Same shape, different cause: a second account signed in during the round
  // trip. The late token belongs to the previous user.
  it("discards a response that arrives after an account switch", async () => {
    const h = makeHarness();
    let resolve!: (value: RefreshSessionResponse) => void;
    h.api.refreshSession.mockReturnValue(
      new Promise<RefreshSessionResponse>((r) => {
        resolve = r;
      }),
    );

    const pending = renewalFor(h).renewNow();
    h.storage.setItem(TOKEN_KEY, "someone-elses-token");

    resolve(renewedResponse(SESSION_V2));
    await pending;

    expect(h.storage.snapshot()[TOKEN_KEY]).toBe("someone-elses-token");
    expect(h.api.setToken).not.toHaveBeenCalled();
  });

  it("does nothing while signed out or still booting", async () => {
    const h = makeHarness();
    h.authenticated.value = false;

    await renewalFor(h).renewNow();

    expect(h.api.refreshSession).not.toHaveBeenCalled();
  });

  it("does nothing when there is no stored session", async () => {
    const h = makeHarness(null);

    await renewalFor(h).renewNow();

    expect(h.api.refreshSession).not.toHaveBeenCalled();
  });

  // The server refuses to refresh anything but a session (400). A PAT in
  // storage — how the local dev environment signs a browser in — would
  // otherwise fail on every launch and every backoff retry, and each failure
  // shows up as an error in the Next.js dev overlay.
  it.each([
    ["personal access token", `mul_${"a".repeat(40)}`],
    ["agent task token", `mat_${"b".repeat(40)}`],
    ["cloud-node PAT", `mcn_${"c".repeat(40)}`],
  ])("never asks to renew a %s", async (_name, token) => {
    const h = makeHarness(token);
    h.api.refreshSession.mockResolvedValue(renewedResponse(SESSION_V2));

    const renewal = renewalFor(h);
    await renewal.renewNow();
    for (let i = 0; i < 5; i++) renewal.maybeRenew();
    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(h.api.refreshSession).not.toHaveBeenCalled();
    expect(h.api.setToken).not.toHaveBeenCalled();
    expect(h.storage.snapshot()[TOKEN_KEY]).toBe(token);
  });

  // The decision is made per attempt, not once: signing in with a real session
  // after a PAT was in play must start renewing it.
  it("renews once the stored credential becomes a session", async () => {
    const h = makeHarness(`mul_${"a".repeat(40)}`);
    h.api.refreshSession.mockResolvedValue(renewedResponse(SESSION_V2));
    const renewal = renewalFor(h);

    await renewal.renewNow();
    expect(h.api.refreshSession).not.toHaveBeenCalled();

    h.storage.setItem(TOKEN_KEY, SESSION_V1);
    await renewal.renewNow();

    expect(h.api.refreshSession).toHaveBeenCalledTimes(1);
    expect(h.storage.snapshot()[TOKEN_KEY]).toBe(SESSION_V2);
  });

  // A skipped attempt must not consume the interval, or a check that landed
  // one tick before the boot probe finished would postpone the real one.
  it("does not consume the interval when it declines to run", async () => {
    const h = makeHarness();
    h.authenticated.value = false;
    h.api.refreshSession.mockResolvedValue(notYetResponse());

    const renewal = renewalFor(h);
    renewal.maybeRenew();
    expect(h.api.refreshSession).not.toHaveBeenCalled();

    h.authenticated.value = true;
    renewal.maybeRenew();
    await vi.waitFor(() =>
      expect(h.api.refreshSession).toHaveBeenCalledTimes(1),
    );
  });
});

describe("isRenewableSessionToken", () => {
  it("accepts a three-part JWT", () => {
    expect(isRenewableSessionToken(SESSION_V1)).toBe(true);
  });

  it.each([
    ["a personal access token", `mul_${"a".repeat(40)}`],
    ["an agent task token", `mat_${"b".repeat(40)}`],
    ["a cloud-node PAT", `mcn_${"c".repeat(40)}`],
    ["an empty string", ""],
    ["two segments", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ2MSJ9"],
    ["an empty segment", "eyJhbGciOiJIUzI1NiJ9..c2ln"],
    ["a segment outside base64url", "eyJhbGciOiJIUzI1NiJ9.eyJz dWIi.c2ln"],
  ])("rejects %s", (_name, token) => {
    expect(isRenewableSessionToken(token)).toBe(false);
  });
});

describe("watchSessionActivity", () => {
  it("checks on foreground and on interaction, never on a timer", async () => {
    vi.useFakeTimers();
    const renewal = {
      maybeRenew: vi.fn(),
      renewNow: vi.fn().mockResolvedValue(undefined),
      reset: vi.fn(),
    };

    const stop = watchSessionActivity(renewal);

    // No timer anywhere: an untouched app must not renew itself, however
    // long it is left open.
    vi.advanceTimersByTime(7 * 24 * 60 * 60 * 1000);
    expect(renewal.maybeRenew).not.toHaveBeenCalled();

    window.dispatchEvent(new Event("focus"));
    expect(renewal.maybeRenew).toHaveBeenCalledTimes(1);

    window.dispatchEvent(new Event("pointerdown"));
    expect(renewal.maybeRenew).toHaveBeenCalledTimes(2);

    window.dispatchEvent(new Event("keydown"));
    expect(renewal.maybeRenew).toHaveBeenCalledTimes(3);

    document.dispatchEvent(new Event("visibilitychange"));
    expect(renewal.maybeRenew).toHaveBeenCalledTimes(4);

    stop();
    window.dispatchEvent(new Event("focus"));
    window.dispatchEvent(new Event("pointerdown"));
    expect(renewal.maybeRenew).toHaveBeenCalledTimes(4);

    vi.useRealTimers();
  });
});
