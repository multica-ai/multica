import { beforeEach, describe, expect, it } from "vitest";
import { DEFAULT_RAIL_ORDER, resolveRailOrder, useRailOrderStore } from "./rail-order-store";

describe("resolveRailOrder", () => {
  it("uses the default order until the user reorders", () => {
    expect(resolveRailOrder(null)).toEqual(DEFAULT_RAIL_ORDER);
  });

  it("keeps a saved order", () => {
    const saved = ["chat", "inbox", "search", "new_issue", "work", "team", "autopilots"];
    expect(resolveRailOrder(saved)).toEqual(saved);
  });

  it("drops unknown and repeated ids", () => {
    expect(
      resolveRailOrder(["chat", "settings", 3, "chat", "inbox", "search", "new_issue", "work", "team", "autopilots"]),
    ).toEqual(["chat", "inbox", "search", "new_issue", "work", "team", "autopilots"]);
  });

  it("puts an item missing from the saved order after its default neighbour", () => {
    expect(resolveRailOrder(["inbox", "work", "new_issue", "team", "chat", "autopilots"])).toEqual([
      "inbox",
      "work",
      "new_issue",
      "search",
      "team",
      "chat",
      "autopilots",
    ]);
    expect(resolveRailOrder(["inbox", "search", "work", "team", "chat", "autopilots"])).toEqual([
      "new_issue",
      "inbox",
      "search",
      "work",
      "team",
      "chat",
      "autopilots",
    ]);
  });
});

describe("useRailOrderStore", () => {
  beforeEach(() => {
    useRailOrderStore.setState({ order: null });
  });

  it("saves a custom order", () => {
    const order = ["inbox", "work", "team", "chat", "autopilots", "new_issue", "search"] as const;
    useRailOrderStore.getState().setOrder(order);
    expect(useRailOrderStore.getState().order).toEqual(order);
  });

  it("forgets the order once it is back to the default", () => {
    useRailOrderStore.getState().setOrder(["search", "new_issue", "inbox", "work", "team", "chat", "autopilots"]);
    useRailOrderStore.getState().setOrder(DEFAULT_RAIL_ORDER);
    expect(useRailOrderStore.getState().order).toBeNull();
  });
});
