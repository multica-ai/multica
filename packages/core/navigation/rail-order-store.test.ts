// @vitest-environment node
import { describe, expect, it } from "vitest";
import { RAIL_SPACES } from "../paths";
import { orderedRailSpaces, useRailOrderStore } from "./rail-order-store";

const REGISTRY_ORDER = [...RAIL_SPACES];

describe("orderedRailSpaces", () => {
  it("returns the registry order for missing or unusable input", () => {
    expect(orderedRailSpaces([])).toEqual(REGISTRY_ORDER);
    expect(orderedRailSpaces(null)).toEqual(REGISTRY_ORDER);
    expect(orderedRailSpaces("chat")).toEqual(REGISTRY_ORDER);
    expect(orderedRailSpaces({ 0: "chat" })).toEqual(REGISTRY_ORDER);
    expect(orderedRailSpaces(["deleted-space"])).toEqual(REGISTRY_ORDER);
  });

  it("keeps a saved order as given", () => {
    expect(orderedRailSpaces(["chat", "inbox", "work", "autopilots", "team"])).toEqual([
      "chat",
      "inbox",
      "work",
      "autopilots",
      "team",
    ]);
  });

  it("drops repeats and spaces removed since the order was saved", () => {
    expect(orderedRailSpaces(["chat", "chat", "removed", "team"])).toEqual([
      "chat",
      "team",
      "inbox",
      "work",
      "autopilots",
    ]);
  });

  it("appends spaces shipped after the order was saved, in registry order", () => {
    expect(orderedRailSpaces(["chat", "team"])).toEqual([
      "chat",
      "team",
      "inbox",
      "work",
      "autopilots",
    ]);
  });
});

describe("useRailOrderStore", () => {
  it("starts at the registry order", () => {
    expect(useRailOrderStore.getState().order).toEqual(REGISTRY_ORDER);
  });

  it("stores a new order", () => {
    useRailOrderStore.getState().setRailOrder(["chat", "team", "inbox", "work", "autopilots"]);
    expect(useRailOrderStore.getState().order).toEqual([
      "chat",
      "team",
      "inbox",
      "work",
      "autopilots",
    ]);
    useRailOrderStore.getState().setRailOrder(REGISTRY_ORDER);
  });
});
