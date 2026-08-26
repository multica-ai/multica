import { beforeEach, describe, expect, it, vi } from "vitest";
import type { User } from "@multica/core/types";

/**
 * The persisted session is the one place where a purely local problem — a
 * half-finished write, a schema drift, a corrupted value — can either sign a
 * user out or, worse, show them somebody else's identity. These tests run the
 * real serialization logic against an in-memory SecureStore so those paths are
 * actually exercised; auth-store.test.ts mocks this module out entirely and
 * therefore cannot see any of them.
 */
const store = new Map<string, string>();

vi.mock("expo-secure-store", () => ({
  getItemAsync: vi.fn(async (k: string) => store.get(k) ?? null),
  setItemAsync: vi.fn(async (k: string, v: string) => {
    store.set(k, v);
  }),
  deleteItemAsync: vi.fn(async (k: string) => {
    store.delete(k);
  }),
}));

const {
  clearSession,
  getSession,
  getToken,
  saveSession,
  saveSessionUser,
} = await import("./secure-storage");

const SESSION_KEY = "multica_session";
const LEGACY_TOKEN_KEY = "multica_token";
const LEGACY_USER_KEY = "multica_user";

const USER = {
  id: "user-1",
  name: "Ada",
  email: "ada@example.com",
  avatar_url: null,
  onboarded_at: null,
  onboarding_questionnaire: {},
  starter_content_state: null,
  language: null,
  profile_description: "",
  timezone: null,
  created_at: "",
  updated_at: "",
} as User;

const OTHER_USER = { ...USER, id: "user-2", email: "grace@example.com" };

beforeEach(() => {
  store.clear();
  vi.clearAllMocks();
});

describe("session record", () => {
  it("round-trips the credential and its snapshot", async () => {
    await saveSession("token-1", USER);

    expect(await getSession()).toEqual({ token: "token-1", user: USER });
  });

  it("writes the credential and the snapshot in a single store write", async () => {
    const SecureStore = await import("expo-secure-store");
    await saveSession("token-1", USER);

    // The whole point of the single-key format: there is no instant at which
    // the device holds a new credential next to the previous snapshot.
    expect(SecureStore.setItemAsync).toHaveBeenCalledTimes(1);
    expect(store.has(LEGACY_TOKEN_KEY)).toBe(false);
    expect(store.has(LEGACY_USER_KEY)).toBe(false);
  });

  it("never restores one account's snapshot under another's credential", async () => {
    await saveSession("token-a", USER);
    await saveSession("token-b", OTHER_USER);

    const session = await getSession();
    expect(session?.token).toBe("token-b");
    expect(session?.user?.id).toBe("user-2");
  });

  it("keeps the credential when only the snapshot fails to parse", async () => {
    store.set(
      SESSION_KEY,
      JSON.stringify({ v: 1, token: "token-1", user: { foo: "bar" } }),
    );

    // A drifted snapshot degrades to the /offline upgrade screen. Dropping
    // the token here would reintroduce "a local problem logs you out".
    expect(await getSession()).toEqual({ token: "token-1", user: null });
  });

  it("rejects a snapshot whose id is the empty-string drift sentinel", async () => {
    store.set(
      SESSION_KEY,
      JSON.stringify({ v: 1, token: "token-1", user: { ...USER, id: "" } }),
    );

    expect(await getSession()).toEqual({ token: "token-1", user: null });
  });

  it("clears an unreadable record instead of trusting a token out of it", async () => {
    store.set(SESSION_KEY, "{not json");

    expect(await getSession()).toBeNull();
    expect(store.has(SESSION_KEY)).toBe(false);
  });

  it("clears a record from an unknown future version", async () => {
    store.set(SESSION_KEY, JSON.stringify({ v: 2, token: "token-1" }));

    expect(await getSession()).toBeNull();
    expect(store.has(SESSION_KEY)).toBe(false);
  });
});

describe("legacy migration", () => {
  it("adopts a bare legacy token so an upgrade does not sign the user out", async () => {
    store.set(LEGACY_TOKEN_KEY, "legacy-token");

    expect(await getSession()).toEqual({ token: "legacy-token", user: null });
    expect(store.has(LEGACY_TOKEN_KEY)).toBe(false);
    expect(JSON.parse(store.get(SESSION_KEY)!)).toMatchObject({
      v: 1,
      token: "legacy-token",
    });
  });

  it("carries a valid legacy snapshot across with its token", async () => {
    store.set(LEGACY_TOKEN_KEY, "legacy-token");
    store.set(LEGACY_USER_KEY, JSON.stringify(USER));

    expect(await getSession()).toEqual({ token: "legacy-token", user: USER });
    expect(store.has(LEGACY_USER_KEY)).toBe(false);
  });

  it("drops a corrupted legacy snapshot but keeps the legacy token", async () => {
    store.set(LEGACY_TOKEN_KEY, "legacy-token");
    store.set(LEGACY_USER_KEY, "{not json");

    expect(await getSession()).toEqual({ token: "legacy-token", user: null });
  });

  it("deletes an orphaned legacy snapshot that has no credential", async () => {
    store.set(LEGACY_USER_KEY, JSON.stringify(USER));

    expect(await getSession()).toBeNull();
    // Left on disk it would be adopted by whatever token is written next.
    expect(store.has(LEGACY_USER_KEY)).toBe(false);
  });

  it("is idempotent when the previous migration was interrupted mid-way", async () => {
    // Killed after SESSION_KEY was written but before the legacy keys went.
    await saveSession("token-1", USER);
    store.set(LEGACY_TOKEN_KEY, "legacy-token");

    expect(await getSession()).toEqual({ token: "token-1", user: USER });
  });
});

describe("clearSession", () => {
  it("removes the legacy keys too, so nothing resurrects the session", async () => {
    store.set(LEGACY_TOKEN_KEY, "legacy-token");
    store.set(LEGACY_USER_KEY, JSON.stringify(USER));
    await saveSession("token-1", USER);

    await clearSession();

    expect(store.size).toBe(0);
    expect(await getSession()).toBeNull();
  });

  it("deletes the legacy token before the current record", async () => {
    const SecureStore = await import("expo-secure-store");
    await clearSession();

    const order = vi
      .mocked(SecureStore.deleteItemAsync)
      .mock.calls.map(([key]) => key);
    expect(order.indexOf(LEGACY_TOKEN_KEY)).toBeLessThan(
      order.indexOf(SESSION_KEY),
    );
  });
});

describe("saveSessionUser", () => {
  it("replaces the snapshot without disturbing the credential", async () => {
    await saveSession("token-1", USER);

    await saveSessionUser({ ...USER, name: "Ada L." });

    expect(await getSession()).toEqual({
      token: "token-1",
      user: { ...USER, name: "Ada L." },
    });
  });

  it("writes nothing when there is no session to bind the snapshot to", async () => {
    await saveSessionUser(USER);

    expect(store.size).toBe(0);
  });
});

describe("getToken", () => {
  it("reads through the session record", async () => {
    await saveSession("token-1", USER);

    expect(await getToken()).toBe("token-1");
  });

  it("returns null when there is no session", async () => {
    expect(await getToken()).toBeNull();
  });
});
