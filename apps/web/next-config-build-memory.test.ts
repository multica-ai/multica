import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(__dirname, "next.config.ts"), "utf8");

describe("next build memory config", () => {
  it("enables supported low-memory build graph options", () => {
    expect(source).toContain("optimizePackageImports");
    expect(source).toContain('"@multica/views"');
    expect(source).toContain('"@multica/ui"');
    expect(source).toContain('"lucide-react"');
    expect(source).toContain("cpus: 1");
    expect(source).toContain("memoryBasedWorkersCount: false");
    expect(source).toContain("staticGenerationMaxConcurrency: 1");
    expect(source).toContain("staticGenerationMinPagesPerWorker: 1000");
    expect(source).toContain("parallelServerCompiles: false");
    expect(source).toContain("parallelServerBuildTraces: false");
    expect(source).toContain("webpackBuildWorker: true");
    expect(source).toContain("webpackMemoryOptimizations: true");
    expect(source).toContain("serverSourceMaps: false");
    expect(source).toContain("productionBrowserSourceMaps: false");
  });
});
