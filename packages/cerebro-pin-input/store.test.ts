import { beforeEach, describe, expect, it } from "vitest";
import { usePinInputStore } from "./store";

describe("usePinInputStore", () => {
  beforeEach(() => {
    usePinInputStore.getState().unpinAll();
  });

  it("starts with no pinned key", () => {
    expect(usePinInputStore.getState().pinnedKey).toBeNull();
  });

  it("pinning a key replaces the previous pinned key (one at a time)", () => {
    const { pin } = usePinInputStore.getState();
    pin("reply-a");
    expect(usePinInputStore.getState().pinnedKey).toBe("reply-a");
    pin("reply-b");
    expect(usePinInputStore.getState().pinnedKey).toBe("reply-b");
  });

  it("unpin only clears state when the matching key is unpinned", () => {
    const { pin, unpin } = usePinInputStore.getState();
    pin("reply-a");
    unpin("reply-b");
    expect(usePinInputStore.getState().pinnedKey).toBe("reply-a");
    unpin("reply-a");
    expect(usePinInputStore.getState().pinnedKey).toBeNull();
  });

  it("unpinAll clears state", () => {
    const { pin, unpinAll } = usePinInputStore.getState();
    pin("reply-x");
    unpinAll();
    expect(usePinInputStore.getState().pinnedKey).toBeNull();
  });
});
