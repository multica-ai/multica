// @vitest-environment node
import { describe, expect, it } from "vitest";
import { createWindowTabStorage, mergeTabGroups } from "./tab-store-storage";

const KEY = "multica_tabs";

function memoryStorage() {
  const items = new Map<string, string>();
  return {
    getItem: (key: string) => items.get(key) ?? null,
    setItem: (key: string, value: string) => void items.set(key, value),
    removeItem: (key: string) => void items.delete(key),
  };
}

function envelope(
  byWorkspace: Record<string, unknown>,
  activeWorkspaceSlug: string | null = Object.keys(byWorkspace)[0] ?? null,
) {
  return JSON.stringify({ state: { activeWorkspaceSlug, byWorkspace }, version: 5 });
}

function storedGroups(inner: ReturnType<typeof memoryStorage>) {
  return JSON.parse(inner.getItem(KEY) ?? "null").state.byWorkspace;
}

/** Two windows that hydrated the same persisted tabs. */
function twoWindows(initial: Record<string, unknown>) {
  const inner = memoryStorage();
  inner.setItem(KEY, envelope(initial));
  const work = createWindowTabStorage(inner);
  const personal = createWindowTabStorage(inner);
  work.setBaseline(initial);
  personal.setBaseline(initial);
  return { inner, work, personal };
}

describe("createWindowTabStorage", () => {
  it("keeps a workspace's tabs that another window opened after hydration", () => {
    const { inner, work, personal } = twoWindows({ work: { tabs: [1] }, personal: { tabs: [1] } });

    personal.setItem(KEY, envelope({ work: { tabs: [1] }, personal: { tabs: [1, 2] } }));
    work.setItem(KEY, envelope({ work: { tabs: [1, 3] }, personal: { tabs: [1] } }));

    expect(storedGroups(inner)).toEqual({
      work: { tabs: [1, 3] },
      personal: { tabs: [1, 2] },
    });
  });

  it("adds a workspace group created in another window", () => {
    const { inner, work, personal } = twoWindows({ work: { tabs: [1] } });

    personal.setItem(KEY, envelope({ work: { tabs: [1] }, side: { tabs: [9] } }));
    work.setItem(KEY, envelope({ work: { tabs: [1, 2] } }));

    expect(storedGroups(inner)).toEqual({ work: { tabs: [1, 2] }, side: { tabs: [9] } });
  });

  it("drops a group this window removed, such as a workspace that is gone", () => {
    const { inner, work } = twoWindows({ work: { tabs: [1] }, gone: { tabs: [1] } });

    work.setItem(KEY, envelope({ work: { tabs: [1] } }));

    expect(storedGroups(inner)).toEqual({ work: { tabs: [1] } });
  });

  it("writes an empty state as-is so logout clears every window's tabs", () => {
    const { inner, work, personal } = twoWindows({ work: { tabs: [1] }, personal: { tabs: [1] } });

    personal.setItem(KEY, envelope({ work: { tabs: [1] }, personal: { tabs: [1, 2] } }));
    work.setItem(KEY, envelope({}, null));

    expect(storedGroups(inner)).toEqual({});
  });

  it("writes through before a baseline exists (hydration migrations)", () => {
    const inner = memoryStorage();
    inner.setItem(KEY, envelope({ other: { tabs: [1] } }));
    const storage = createWindowTabStorage(inner);

    storage.setItem(KEY, envelope({ work: { tabs: [1] } }));

    expect(storedGroups(inner)).toEqual({ work: { tabs: [1] } });
  });

  it("reads one workspace's persisted group", () => {
    const { work, personal } = twoWindows({ work: { tabs: [1] } });
    personal.setItem(KEY, envelope({ work: { tabs: [1] }, personal: { tabs: [4] } }));

    expect(work.readStoredGroup(KEY, "personal")).toEqual({ tabs: [4] });
    expect(work.readStoredGroup(KEY, "missing")).toBeUndefined();
    expect(work.readStoredGroup(KEY, "constructor")).toBeUndefined();
  });
});

describe("mergeTabGroups", () => {
  it("prefers this window's copy when both windows changed a group", () => {
    const baseline = new Map([["work", JSON.stringify({ tabs: [1] })]]);

    expect(
      mergeTabGroups({ work: { tabs: [1, 2] } }, { work: { tabs: [1, 3] } }, baseline),
    ).toEqual({ work: { tabs: [1, 2] } });
  });
});
