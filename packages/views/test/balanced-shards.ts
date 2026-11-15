interface WeightedTest<T> {
  path: string;
  cost: number;
  test: T;
}

/** Longest files first, each assigned to the currently lightest shard. */
export function balancedShards<T>(files: WeightedTest<T>[], count: number): T[][] {
  const shards = Array.from({ length: count }, () => ({ cost: 0, tests: [] as T[] }));
  const ordered = [...files].sort((a, b) =>
    b.cost - a.cost || (a.path < b.path ? -1 : a.path > b.path ? 1 : 0),
  );
  for (const file of ordered) {
    let target = shards[0]!;
    for (const shard of shards) {
      if (shard.cost < target.cost ||
          (shard.cost === target.cost && shard.tests.length < target.tests.length)) {
        target = shard;
      }
    }
    target.tests.push(file.test);
    target.cost += file.cost;
  }
  return shards.map((shard) => shard.tests);
}
