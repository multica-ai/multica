import { statSync, writeFileSync } from "node:fs";
import { relative, resolve } from "node:path";
import type { Reporter, TestModule, TestRunEndReason, Vitest } from "vitest/node";

/** Explicit opt-in reporter for refreshing the versioned shard weights. */
export default class TimingReporter implements Reporter {
  private root = "";

  onInit(ctx: Vitest) {
    if (ctx.config.shard) {
      throw new Error("Refresh shard timings with an unsharded run so every file is recorded.");
    }
    this.root = ctx.config.root;
  }

  onTestRunEnd(modules: readonly TestModule[], errors: readonly unknown[], reason: TestRunEndReason) {
    if (reason !== "passed" || errors.length || modules.some((module) => !module.ok())) {
      throw new Error("Shard timings are only refreshed after a successful full run.");
    }
    let totalCost = 0;
    let totalBytes = 0;
    const files: Record<string, number> = {};
    for (const module of [...modules].sort((a, b) => a.moduleId < b.moduleId ? -1 : 1)) {
      const diagnostic = module.diagnostic();
      const cost = Math.max(1, Math.round(
        diagnostic.environmentSetupDuration + diagnostic.prepareDuration +
        diagnostic.setupDuration + diagnostic.collectDuration + diagnostic.duration,
      ));
      files[relative(this.root, module.moduleId).replaceAll("\\", "/")] = cost;
      totalCost += cost;
      totalBytes += statSync(module.moduleId).size;
    }
    writeFileSync(resolve(this.root, "test/shard-timings.json"), JSON.stringify({
      millisecondsPerByte: totalCost / totalBytes,
      files,
    }, null, 2) + "\n");
  }
}
