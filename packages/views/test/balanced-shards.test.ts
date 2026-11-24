// @vitest-environment node

import { describe, expect, it } from "vitest";
import { balancedShards } from "./balanced-shards";

const files = [
  { path: "a", cost: 12, test: "a" },
  { path: "b", cost: 10, test: "b" },
  { path: "c", cost: 4, test: "c" },
  { path: "d", cost: 3, test: "d" },
  { path: "e", cost: 1, test: "e" },
];

describe("balanced test shards", () => {
  it("balances work instead of file counts", () => {
    const shards = balancedShards(files, 2);
    const costs = shards.map((shard) =>
      files.filter((file) => shard.includes(file.test)).reduce((sum, file) => sum + file.cost, 0),
    );
    expect(costs).toEqual([15, 15]);
    expect(shards.map((shard) => shard.length).sort()).toEqual([2, 3]);
  });

  it.each([1, 2, 3, 7])("runs every discovered file once with %i shards, including new files", (count) => {
    const discovered = files.filter((file) => file.path !== "c").concat({ path: "new", cost: 5, test: "new" });
    const shards = balancedShards(discovered, count);
    expect(shards).toHaveLength(count);
    expect(shards.flat().sort()).toEqual(discovered.map((file) => file.test).sort());
  });

  it("assigns equal-cost files deterministically regardless of discovery order", () => {
    const tied = files.map((file) => ({ ...file, cost: 1 }));
    expect(balancedShards(tied, 2)).toEqual(balancedShards([...tied].reverse(), 2));
  });
});
