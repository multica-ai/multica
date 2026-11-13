import { describe, expect, it } from "vitest";
import {
  createMobileStorage,
  createSecureStore,
  forgetCredentialsFromPreviousInstall,
  type EnumerableKeyValueStore,
} from "./storage";

function memoryStore(initial: Record<string, string> = {}): EnumerableKeyValueStore & {
  data: Map<string, string>;
} {
  const data = new Map(Object.entries(initial));
  return {
    data,
    getItem: (key) => data.get(key) ?? null,
    setItem: (key, value) => void data.set(key, value),
    removeItem: (key) => void data.delete(key),
    keys: () => [...data.keys()],
  };
}

describe("createMobileStorage", () => {
  it("keeps the session token in the secure store and everything else in the database", () => {
    const general = memoryStore();
    const secure = memoryStore();
    const storage = createMobileStorage(general, secure);

    storage.setItem("multica_token", "jwt");
    storage.setItem("multica_issue_draft:acme", "draft");

    expect(secure.data.get("multica_token")).toBe("jwt");
    expect(general.data.has("multica_token")).toBe(false);
    expect(general.data.get("multica_issue_draft:acme")).toBe("draft");
    expect(storage.getItem("multica_token")).toBe("jwt");

    storage.removeItem("multica_token");
    expect(secure.data.size).toBe(0);
  });

  it("lists keys from both stores without the install marker", () => {
    const general = memoryStore({ multica_mobile_installed: "1", "multica_issue_draft:acme": "x" });
    const storage = createMobileStorage(general, memoryStore({ multica_token: "jwt" }));
    expect(storage.keys()).toEqual(["multica_issue_draft:acme", "multica_token"]);
  });
});

describe("createSecureStore", () => {
  it("reports a removed key as gone before the async deletion finishes", () => {
    const native = memoryStore({ multica_token: "jwt" });
    let finishDelete = () => {};
    const store = createSecureStore({
      getItem: native.getItem,
      setItem: native.setItem,
      deleteItemAsync: (key) =>
        new Promise<void>((resolve) => {
          finishDelete = () => {
            native.removeItem(key);
            resolve();
          };
        }),
    });

    store.removeItem("multica_token");
    expect(store.getItem("multica_token")).toBeNull();
    expect(native.data.get("multica_token")).toBe("jwt");

    finishDelete();
    store.setItem("multica_token", "renewed");
    expect(store.getItem("multica_token")).toBe("renewed");
    expect(native.data.get("multica_token")).toBe("renewed");
  });
});

describe("forgetCredentialsFromPreviousInstall", () => {
  it("drops a token the Keychain kept across a reinstall", () => {
    const general = memoryStore();
    const secure = memoryStore({ multica_token: "old" });

    forgetCredentialsFromPreviousInstall(general, secure);

    expect(secure.data.has("multica_token")).toBe(false);
    expect(general.data.has("multica_mobile_installed")).toBe(true);
  });

  it("keeps the token on every later launch", () => {
    const general = memoryStore({ multica_mobile_installed: "1" });
    const secure = memoryStore({ multica_token: "current" });

    forgetCredentialsFromPreviousInstall(general, secure);

    expect(secure.data.get("multica_token")).toBe("current");
  });
});
