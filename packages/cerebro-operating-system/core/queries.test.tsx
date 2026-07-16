import { describe, expect, it } from "vitest";

import { connectionsOptions, operatingSystemKeys, periodsOptions, rocksOptions, strategyOptions } from "./queries";

describe("operating system query keys", () => {
  it("scopes every list by workspace", () => {
    expect(strategyOptions("ws-a").queryKey).toEqual(["cerebro", "operating-system", "ws-a", "strategy"]);
    expect(rocksOptions("ws-b").queryKey).toEqual(["cerebro", "operating-system", "ws-b", "rocks"]);
    expect(periodsOptions("ws-b").queryKey).toEqual(["cerebro", "operating-system", "ws-b", "periods"]);
    expect(operatingSystemKeys.all("ws-a")).not.toEqual(operatingSystemKeys.all("ws-b"));
    expect(connectionsOptions("ws-a", "strategy_item", "s1").queryKey).toEqual([
      "cerebro", "operating-system", "ws-a", "connections", "strategy_item", "s1",
    ]);
  });
});
