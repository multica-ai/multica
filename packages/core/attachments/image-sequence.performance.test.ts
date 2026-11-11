// @vitest-environment node
import { writeFileSync } from "node:fs";
import { performance } from "node:perf_hooks";

// Timings are opt-in; default CI checks parser call counts instead.
// MULTICA_MARKDOWN_BENCHMARK=1 pnpm --filter @multica/core test attachments/image-sequence.performance.test.ts --maxWorkers=1 -t benchmarks
import { beforeEach, describe, expect, it, vi } from "vitest";

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

let collectAttachmentSequence: typeof import("./image-sequence").collectAttachmentSequence;

beforeEach(async () => {
  vi.resetModules();
  ({ collectAttachmentSequence } = await import("./image-sequence"));
  stats.calls = 0;
});

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
    expect(collectAttachmentSequence(blocks, () => true)).toEqual([]);
    expect(collectAttachmentSequence([...blocks, { content: "Another update" }], () => true)).toEqual([]);
    expect(stats.calls).toBe(0);
  });

  it("only parses blocks that might contain images or file cards", () => {
    const blocks = timeline(30);
    expect(collectAttachmentSequence(blocks, () => true)).toHaveLength(10);
    expect(stats.calls).toBeLessThanOrEqual(10);
  });

  it("only parses new or edited content when rebuilding an image-dense timeline", () => {
    const blocks = timeline(1);
    const initial = collectAttachmentSequence(blocks, () => true);
    expect(initial).toHaveLength(BLOCK_COUNT);
    expect(stats.calls).toBe(BLOCK_COUNT);

    const updated = [
      ...blocks.map((block) => ({ ...block })),
      { id: "new-comment", content: "![new](https://cdn.example/new.png)" },
    ];
    expect(collectAttachmentSequence(updated, () => true)).toEqual([
      ...initial,
      expect.objectContaining({ key: "https://cdn.example/new.png", blockId: "new-comment" }),
    ]);
    expect(stats.calls).toBe(BLOCK_COUNT + 1);

    updated[0] = { ...updated[0]!, content: "![edited](https://cdn.example/edited.png)" };
    const edited = collectAttachmentSequence(updated, () => true);
    expect(edited[0]?.key).toBe("https://cdn.example/edited.png");
    expect(edited.slice(1, BLOCK_COUNT)).toEqual(initial.slice(1));
    expect(stats.calls).toBe(BLOCK_COUNT + 2);
  });

  it("reuses content whose image markers only appear in code", () => {
    const blocks = [{ content: "`![example](https://cdn.example/code.png)`" }];
    expect(collectAttachmentSequence(blocks, () => true)).toEqual([]);
    expect(collectAttachmentSequence(blocks, () => true)).toEqual([]);
    expect(stats.calls).toBe(1);
  });

  it("evicts the least recently used content after 500 entries", () => {
    const blocks = Array.from({ length: 501 }, (_, i) => ({
      content: `![shot](https://cdn.example/lru-${i}.png)`,
    }));
    collectAttachmentSequence(blocks.slice(0, 500), () => true);
    expect(stats.calls).toBe(500);

    collectAttachmentSequence([blocks[0]], () => true);
    collectAttachmentSequence([blocks[500]], () => true);
    expect(stats.calls).toBe(501);
    collectAttachmentSequence([blocks[0]], () => true);
    expect(stats.calls).toBe(501);

    expect(collectAttachmentSequence([blocks[1]], () => true)[0]?.key)
      .toBe("https://cdn.example/lru-1.png");
    expect(stats.calls).toBe(502);
  });

  it.skipIf(process.env.MULTICA_MARKDOWN_BENCHMARK !== "1")("benchmarks 300 unique 3 KiB comments", () => {
    const results = scenarios.map(({ name, imageEvery, expected }) => {
      const blocks = timeline(imageEvery);
      expect(blocks.every((block) => Buffer.byteLength(block.content) === BLOCK_BYTES)).toBe(true);
      const coldStart = performance.now();
      const initial = collectAttachmentSequence(blocks, () => true);
      const cold_ms = performance.now() - coldStart;
      expect(initial).toHaveLength(expected);
      for (let i = 0; i < 3; i++) collectAttachmentSequence(blocks, () => true);
      const samples: number[] = [];
      for (let i = 0; i < 15; i++) {
        const start = performance.now();
        const items = collectAttachmentSequence(blocks, () => true);
        samples.push(performance.now() - start);
        expect(items).toHaveLength(expected);
      }
      samples.sort((a, b) => a - b);
      const appendedSamples: number[] = [];
      for (let i = 0; i < 15; i++) {
        const updated = [...blocks, { content: `![new](https://cdn.example/${name}-new-${i}.png)` }];
        const start = performance.now();
        const items = collectAttachmentSequence(updated, () => true);
        appendedSamples.push(performance.now() - start);
        expect(items).toHaveLength(expected + 1);
      }
      appendedSamples.sort((a, b) => a - b);
      return { name, cold_ms, median_ms: samples[7], p95_ms: samples[14], min_ms: samples[0], append_median_ms: appendedSamples[7] };
    });
    const report = { node: process.version, blocks: BLOCK_COUNT, bytes_per_block: BLOCK_BYTES, warmups: 3, samples: 15, results };
    process.stdout.write("MARKDOWN_SEQUENCE_BENCHMARK " + JSON.stringify(report) + "\n");
    if (process.env.MULTICA_MARKDOWN_BENCHMARK_OUTPUT) {
      writeFileSync(process.env.MULTICA_MARKDOWN_BENCHMARK_OUTPUT, JSON.stringify(report, null, 2) + "\n");
    }
  }, 60_000);
});
