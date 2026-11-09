import { statSync } from "node:fs";
import { relative } from "node:path";
import { BaseSequencer, type TestSpecification } from "vitest/node";
import { balancedShards } from "./balanced-shards";
import timings from "./shard-timings.json";

export default class BalancedSequencer extends BaseSequencer {
  async shard(files: TestSpecification[]) {
    const { shard, root } = this.ctx.config;
    if (!shard) return files;

    // A versioned input gives every runner the same assignment, independent of cache state.
    const weights: Record<string, number> = timings.files;
    return balancedShards(files.map((file) => {
      const path = relative(root, file.moduleId).replaceAll("\\", "/");
      return {
        path,
        cost: weights[path] ?? Math.max(1, statSync(file.moduleId).size * timings.millisecondsPerByte),
        test: file,
      };
    }), shard.count)[shard.index - 1]!;
  }
}
