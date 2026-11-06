// @vitest-environment node
import { writeFileSync } from "node:fs";
import { performance } from "node:perf_hooks";

// Timings are opt-in; default CI checks parser call counts instead.
// MULTICA_MARKDOWN_BENCHMARK=1 pnpm --filter @multica/core test attachments/image-sequence.performance.test.ts --maxWorkers=1 -t benchmarks
import { describe, expect, it, vi } from "vitest";

const { stats } = vi.hoisted(() => ({ stats: { calls: 0 } }));
vi.mock("mdast-util-from-markdown", async (importOriginal) => {
  const actual = await importOriginal<typeof import("mdast-util-from-markdown")>();
  return {
    ...actual,
    fromMarkdown: (...args: Parameters<typeof actual.fromMarkdown>) => {
      stats.calls++;
      return actual.fromMarkdown(...args);
    },
  };
});

import { collectAttachmentSequence } from "./image-sequence";

const BLOCK_COUNT = 300;
const BLOCK_BYTES = 3072;
const PROSE = "The task is progressing. Review the implementation and tests before updating the issue.\n";

function timeline(imageEvery: number) {
  return Array.from({ length: BLOCK_COUNT }, (_, i) => {
    const prefix = `Comment ${i}\n\n`;
    const suffix = imageEvery && i % imageEvery === 0
      ? `\n\n![shot](https://cdn.example/${i}.png)`
      : "\n\nNo attachments in this update.\n";
    const prose = PROSE.repeat(40).slice(0, BLOCK_BYTES - prefix.length - suffix.length);
    return { id: `comment-${i}`, content: prefix + prose + suffix, attachments: [] };
  });
}

const scenarios = [
  { name: "no-images", imageEvery: 0, expected: 0 },
  { name: "sparse-images", imageEvery: 30, expected: 10 },
  { name: "all-images", imageEvery: 1, expected: 300 },
];

describe("large preview timelines", () => {
  it("does not parse unchanged or appended image-free comments", () => {
    const blocks = timeline(0);
    stats.calls = 0;
    expect(collectAttachmentSequence(blocks, () => true)).toEqual([]);
    expect(collectAttachmentSequence([...blocks, { content: "Another update" }], () => true)).toEqual([]);
    expect(stats.calls).toBe(0);
  });

  it("only parses blocks that might contain images or file cards", () => {
    const blocks = timeline(30);
    stats.calls = 0;
    expect(collectAttachmentSequence(blocks, () => true)).toHaveLength(10);
    expect(stats.calls).toBeLessThanOrEqual(10);
  });

  it.skipIf(process.env.MULTICA_MARKDOWN_BENCHMARK !== "1")("benchmarks 300 unique 3 KiB comments", () => {
    const results = scenarios.map(({ name, imageEvery, expected }) => {
      const blocks = timeline(imageEvery);
      expect(blocks.every((block) => Buffer.byteLength(block.content) === BLOCK_BYTES)).toBe(true);
      for (let i = 0; i < 3; i++) collectAttachmentSequence(blocks, () => true);
      const samples: number[] = [];
      for (let i = 0; i < 15; i++) {
        const start = performance.now();
        const items = collectAttachmentSequence(blocks, () => true);
        samples.push(performance.now() - start);
        expect(items).toHaveLength(expected);
      }
      samples.sort((a, b) => a - b);
      return { name, median_ms: samples[7], p95_ms: samples[14], min_ms: samples[0] };
    });
    const report = { node: process.version, blocks: BLOCK_COUNT, bytes_per_block: BLOCK_BYTES, warmups: 3, samples: 15, results };
    process.stdout.write("MARKDOWN_SEQUENCE_BENCHMARK " + JSON.stringify(report) + "\n");
    if (process.env.MULTICA_MARKDOWN_BENCHMARK_OUTPUT) {
      writeFileSync(process.env.MULTICA_MARKDOWN_BENCHMARK_OUTPUT, JSON.stringify(report, null, 2) + "\n");
    }
  }, 60_000);
});
