import { afterEach, describe, expect, it, vi } from "vitest";
import type { User } from "../types";
import { markWhatsNewSeen, parseVersion, unreadReleases, visibleReleases } from "./index";

const api = vi.hoisted(() => ({ markWhatsNewSeen: vi.fn() }));
const authStore = vi.hoisted(() => {
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  const { create } = require("zustand") as typeof import("zustand");
  return create<{ user: User | null; setUser: (user: User) => void }>((set) => ({
    user: null,
    setUser: (user) => set({ user }),
  }));
});

vi.mock("../api", () => ({ api }));
vi.mock("../auth", () => ({ useAuthStore: authStore }));

const releases = [
  { version: "0.7.0", date: "2026-10-20" },
  { version: "0.7.10", date: "2026-11-02" },
  { version: "0.7.2", date: "2026-10-30" },
  { version: "0.7.9", date: "2026-11-01" },
];

const versions = (rs: { version: string }[]) => rs.map((r) => r.version);

describe("parseVersion", () => {
  it("accepts a leading v and build suffixes", () => {
    expect(parseVersion("v0.7.2-12-gabc")).toEqual([0, 7, 2]);
    expect(parseVersion("1.2.3")).toEqual([1, 2, 3]);
  });

  it("rejects empty and partial versions", () => {
    expect(parseVersion("")).toBeNull();
    expect(parseVersion("0.7")).toBeNull();
    expect(parseVersion(null)).toBeNull();
  });
});

describe("visibleReleases", () => {
  it("sorts newest first numerically", () => {
    expect(versions(visibleReleases(releases, ""))).toEqual(["0.7.10", "0.7.9", "0.7.2", "0.7.0"]);
  });

  it("caps at the connected server version", () => {
    expect(versions(visibleReleases(releases, "v0.7.2"))).toEqual(["0.7.2", "0.7.0"]);
  });

  it("applies no cap when the server version is unparseable", () => {
    expect(visibleReleases(releases, "dev")).toHaveLength(4);
  });
});

describe("unreadReleases", () => {
  const visible = visibleReleases(releases, "");

  it("returns nothing when the server cannot track the marker", () => {
    expect(unreadReleases(visible, { created_at: "2026-01-01T00:00:00Z" })).toEqual([]);
    expect(unreadReleases(visible, null)).toEqual([]);
  });

  it("returns releases newer than the seen marker", () => {
    const user = { whats_new_seen_version: "0.7.2", created_at: "2026-01-01T00:00:00Z" };
    expect(versions(unreadReleases(visible, user))).toEqual(["0.7.10", "0.7.9"]);
  });

  it("returns nothing once the newest release is seen", () => {
    const user = { whats_new_seen_version: "0.7.10", created_at: "2026-01-01T00:00:00Z" };
    expect(unreadReleases(visible, user)).toEqual([]);
  });

  it("counts only releases after signup before the first dismissal", () => {
    const user = { whats_new_seen_version: null, created_at: "2026-10-30T09:00:00Z" };
    expect(versions(unreadReleases(visible, user))).toEqual(["0.7.10", "0.7.9"]);
  });
});

describe("markWhatsNewSeen", () => {
  const ada = { id: "u-ada", whats_new_seen_version: "0.7.0" } as User;

  function deferred<T>() {
    let resolve!: (value: T) => void;
    let reject!: (err: unknown) => void;
    const promise = new Promise<T>((res, rej) => {
      resolve = res;
      reject = rej;
    });
    return { promise, resolve, reject };
  }

  afterEach(() => {
    api.markWhatsNewSeen.mockReset();
    authStore.setState({ user: null });
  });

  it("applies the marker before the server answers and keeps the server's value", async () => {
    authStore.setState({ user: ada });
    const pending = deferred<User>();
    api.markWhatsNewSeen.mockReturnValue(pending.promise);

    const done = markWhatsNewSeen("0.7.2");
    expect(authStore.getState().user?.whats_new_seen_version).toBe("0.7.2");

    pending.resolve({ ...ada, whats_new_seen_version: "0.7.3" });
    await done;
    expect(authStore.getState().user?.whats_new_seen_version).toBe("0.7.3");
  });

  it("rolls the marker back when the write fails so the next open retries", async () => {
    authStore.setState({ user: ada });
    api.markWhatsNewSeen.mockRejectedValue(new Error("offline"));

    await expect(markWhatsNewSeen("0.7.2")).rejects.toThrow("offline");
    expect(authStore.getState().user?.whats_new_seen_version).toBe("0.7.0");
  });

  it("does not sign a logged-out session back in when the response is late", async () => {
    authStore.setState({ user: ada });
    const pending = deferred<User>();
    api.markWhatsNewSeen.mockReturnValue(pending.promise);

    const done = markWhatsNewSeen("0.7.2");
    authStore.setState({ user: null });
    pending.resolve({ ...ada, whats_new_seen_version: "0.7.2" });
    await done;
    expect(authStore.getState().user).toBeNull();
  });

  it("leaves another account untouched after a switch", async () => {
    authStore.setState({ user: ada });
    const pending = deferred<User>();
    api.markWhatsNewSeen.mockReturnValue(pending.promise);

    const done = markWhatsNewSeen("0.7.2");
    const bob = { id: "u-bob", whats_new_seen_version: null } as User;
    authStore.setState({ user: bob });
    pending.reject(new Error("aborted"));
    await expect(done).rejects.toThrow("aborted");
    expect(authStore.getState().user).toBe(bob);
  });
});
