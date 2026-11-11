// @vitest-environment node
import { writeFileSync } from 'node:fs'
import { performance } from 'node:perf_hooks'

// MULTICA_MARKDOWN_BENCHMARK=1 pnpm --filter @multica/ui test markdown/linkify.performance.test.ts --maxWorkers=1 -t benchmarks
import { describe, expect, it, vi } from 'vitest'

const { stats } = vi.hoisted(() => ({ stats: { calls: 0 } }))
vi.mock('mdast-util-from-markdown', async (importOriginal) => {
  const actual = await importOriginal<typeof import('mdast-util-from-markdown')>()
  return {
    ...actual,
    fromMarkdown: (...args: Parameters<typeof actual.fromMarkdown>) => {
      stats.calls++
      return actual.fromMarkdown(...args)
    },
  }
})

import { findMarkdownLinkRanges, preprocessLinks } from './linkify'

function comments(definitions: boolean) {
  return Array.from({ length: 300 }, (_, i) => {
    const prefix = `Comment ${i}\n\n`
    const suffix = definitions ? `\n\n[shot]: https://cdn.example/${i}.png` : '\n\nhttps://outside.example'
    return prefix + 'Review the implementation and tests.\n'.repeat(100).slice(0, 3072 - prefix.length - suffix.length) + suffix
  })
}

describe('linkify reference-definition precheck', () => {
  it('does not parse ordinary comments or existing inline links for definitions', () => {
    stats.calls = 0
    for (const content of comments(false)) findMarkdownLinkRanges(content)
    expect(preprocessLinks('[existing](https://cdn.example/a.png)')).toBe('[existing](https://cdn.example/a.png)')
    expect(stats.calls).toBe(0)
  })

  it.each([
    '[shot]: https://cdn.example/a.png',
    '[shot\nname]: https://cdn.example/a.png',
    '> [shot]: https://cdn.example/a.png',
    '- [shot]: https://cdn.example/a.png',
    '[a\\]b]: https://cdn.example/a.png',
  ])('still protects the definition in %s', (content) => {
    stats.calls = 0
    expect(preprocessLinks(content)).toBe(content)
    expect(stats.calls).toBeLessThanOrEqual(1)
  })

  it.skipIf(process.env.MULTICA_MARKDOWN_BENCHMARK !== '1')('benchmarks link-range discovery for 300 unique 3 KiB comments', () => {
    const results = [false, true].map((definitions) => {
      const blocks = comments(definitions)
      for (let i = 0; i < 3; i++) blocks.forEach(findMarkdownLinkRanges)
      const samples: number[] = []
      for (let i = 0; i < 15; i++) {
        const start = performance.now()
        blocks.forEach(findMarkdownLinkRanges)
        samples.push(performance.now() - start)
      }
      samples.sort((a, b) => a - b)
      return { name: definitions ? 'with-definitions' : 'no-definitions', median_ms: samples[7], p95_ms: samples[14], min_ms: samples[0] }
    })
    const report = { node: process.version, blocks: 300, bytes_per_block: 3072, warmups: 3, samples: 15, results }
    process.stdout.write("MARKDOWN_LINKIFY_BENCHMARK " + JSON.stringify(report) + "\n")
    if (process.env.MULTICA_MARKDOWN_BENCHMARK_OUTPUT) {
      writeFileSync(process.env.MULTICA_MARKDOWN_BENCHMARK_OUTPUT, JSON.stringify(report, null, 2) + "\n")
    }
  }, 60_000)
})
