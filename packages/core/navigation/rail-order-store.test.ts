import { beforeEach, describe, expect, it } from "vitest";
import { RAIL_SPACES } from "../paths/spaces";
import { resolveRailOrder, useRailOrderStore } from "./rail-order-store";

describe("resolveRailOrder", () => {
  it("uses the default order until the user reorders", () => {
    expect(resolveRailOrder(null)).toEqual(RAIL_SPACES);
  });

  it("inserts Docs into a saved layout from before Docs existed", () => {
    expect(resolveRailOrder(["chat", "inbox", "work", "team", "autopilots"])).toEqual(["chat", "inbox", "work", "docs", "team", "autopilots"]);
  });

  it("keeps a saved order", () => {
    const saved = ["chat", "inbox", "work", "docs", "team", "autopilots"];
    expect(resolveRailOrder(saved)).toEqual(saved);
  });

  it("drops unknown and repeated ids", () => {
    expect(resolveRailOrder(["chat", "settings", 3, "chat", "inbox", "work", "docs", "team", "autopilots"])).toEqual([
      "chat",
      "inbox",
      "work",
      "docs",
      "team",
      "autopilots",
    ]);
  });

  it("puts a space missing from the saved order after its default neighbour", () => {
    expect(resolveRailOrder(["chat", "inbox", "autopilots", "work"])).toEqual([
      "chat",
      "inbox",
      "autopilots",
      "work",
      "docs",
      "team",
    ]);
    expect(resolveRailOrder(["chat", "work", "docs", "team", "autopilots"])).toEqual([
      "inbox",
      "chat",
      "work",
      "docs",
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
    const order = ["chat", "inbox", "work", "docs", "team", "autopilots"] as const;
    useRailOrderStore.getState().setOrder(order);
    expect(useRailOrderStore.getState().order).toEqual(order);
  });

  it("forgets the order once it is back to the default", () => {
    useRailOrderStore.getState().setOrder(["work", "inbox", "team", "chat", "autopilots"]);
    useRailOrderStore.getState().setOrder(RAIL_SPACES);
    expect(useRailOrderStore.getState().order).toBeNull();
  });
});
