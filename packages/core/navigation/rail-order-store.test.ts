import { beforeEach, describe, expect, it } from "vitest";
import { RAIL_SPACES } from "../paths/spaces";
import { resolveRailOrder, useRailOrderStore } from "./rail-order-store";

describe("resolveRailOrder", () => {
  it("uses the default order until the user reorders", () => {
    expect(resolveRailOrder(null)).toEqual(RAIL_SPACES);
  });

  it("keeps a saved order", () => {
    const saved = ["chat", "work", "team", "autopilots"];
    expect(resolveRailOrder(saved)).toEqual(saved);
  });

  it("drops unknown and repeated ids", () => {
    expect(resolveRailOrder(["chat", "settings", 3, "chat", "work", "team", "autopilots"])).toEqual([
      "chat",
      "work",
      "team",
      "autopilots",
    ]);
  });

  // The Inbox moved into Work; orders saved while it was a space keep the rest.
  it("drops the retired inbox space from a saved order", () => {
    expect(resolveRailOrder(["chat", "inbox", "work", "team", "autopilots"])).toEqual([
      "chat",
      "work",
      "team",
      "autopilots",
    ]);
  });

  it("puts a space missing from the saved order after its default neighbour", () => {
    expect(resolveRailOrder(["chat", "autopilots", "work"])).toEqual([
      "chat",
      "autopilots",
      "work",
      "team",
    ]);
    expect(resolveRailOrder(["chat", "team", "autopilots"])).toEqual([
      "work",
      "chat",
      "team",
      "autopilots",
    ]);
  });
});

describe("useRailOrderStore", () => {
  beforeEach(() => {
    useRailOrderStore.setState({ order: null });
  });

  it("saves a custom order", () => {
    const order = ["chat", "work", "team", "autopilots"] as const;
    useRailOrderStore.getState().setOrder(order);
    expect(useRailOrderStore.getState().order).toEqual(order);
  });

  it("forgets the order once it is back to the default", () => {
    useRailOrderStore.getState().setOrder(["team", "work", "chat", "autopilots"]);
    useRailOrderStore.getState().setOrder(RAIL_SPACES);
    expect(useRailOrderStore.getState().order).toBeNull();
  });
});
