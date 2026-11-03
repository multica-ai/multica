// @vitest-environment node
import { describe, expect, it } from 'vitest'
import { findFencedCodeRanges, preprocessLinks } from './linkify'

describe('fenced code boundaries used by preprocessing and chunked loading', () => {
  it.each([
    ['````markdown', '```'],
    ['````markdown', '~~~'],
    ['````markdown', '````javascript'],
    ['```markdown', '- ```'],
    ['```markdown', '> ```'],
    ['~~~text', '```'],
    ['~~~~text', '~~~'],
    ['``````javascript', "const ticks = '`````';"],
  ])('keeps %s open across a literal %s line', (opening, inner) => {
    const closing = opening.match(/^[`~]+/)![0]
    const block = `${opening}\n${inner}\nhttps://example.com\n${closing}`
    const source = `Intro\n\n${block}\n\nhttps://outside.example`
    expect(findFencedCodeRanges(source)).toEqual([
      { start: 7, end: 7 + block.length },
    ])
    expect(preprocessLinks(source)).toBe(
      `Intro\n\n${block}\n\n[https://outside.example](https://outside.example)`,
    )
  })

  it('protects unclosed fences through the end of the input', () => {
    const source = 'Intro\n\n````\n```\nhttps://example.com'
    expect(findFencedCodeRanges(source)).toEqual([{ start: 7, end: source.length }])
    expect(preprocessLinks(source)).toBe(source)
  })

  it.each(['> ', '> > ', '   '])('protects fences with the prefix %j', (prefix) => {
    const source = `${prefix}\`\`\`\`\n${prefix}\`\`\`\n${prefix}https://example.com\n${prefix}\`\`\`\``
    expect(preprocessLinks(source)).toBe(source)
  })

  it('protects a list code block with indented continuation lines', () => {
    const source = '- ````\n  ```\n  https://example.com\n  ````'
    expect(preprocessLinks(source)).toBe(source)
  })

  it.each(['`', '``', '```'])('keeps matching %s inline spans protected', (ticks) => {
    const source = `before ${ticks}https://example.com${ticks} after`
    expect(preprocessLinks(source)).toBe(source)
  })
})
