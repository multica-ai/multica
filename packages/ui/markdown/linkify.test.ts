// @vitest-environment node
import { describe, expect, it } from 'vitest'
import { performance } from 'node:perf_hooks'
import { findCodeRanges, findFencedCodeRanges, preprocessLinks } from './linkify'

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

  it.each([
    ['a quote at a blank line', '> ```\n> https://inside.example', '\n\n'],
    ['a quote at a line without >', '> ```\n> https://inside.example', '\n'],
    ['a list item at a blank line', '- ```\n  https://inside.example', '\n\n'],
    ['a list item at an unindented line', '- ```\n  https://inside.example', '\n'],
    ['a list nested in a quote', '> - ```\n>   https://inside.example', '\n> '],
  ])('ends an unclosed fence with %s', (_, code, separator) => {
    const source = `${code}${separator}https://outside.example`
    const [range, ...rest] = findFencedCodeRanges(source)
    expect(rest).toEqual([])
    expect(range!.start).toBe(0)
    expect(range!.end).toBeGreaterThanOrEqual(code.length)
    expect(range!.end).toBeLessThan(source.indexOf('https://outside.example'))
    expect(preprocessLinks(source)).toBe(
      `${code}${separator}[https://outside.example](https://outside.example)`,
    )
  })

  it('keeps blank lines inside a list item fence', () => {
    const source = '- ```\n  a\n\n  https://example.com\n  ```'
    expect(findFencedCodeRanges(source)).toEqual([{ start: 0, end: source.length }])
  })

  it('lets a line that ends the container open a new fence', () => {
    const source = '> ```\n> a\n```\nhttps://example.com'
    expect(findFencedCodeRanges(source)).toEqual([
      { start: 0, end: 9 },
      { start: 10, end: source.length },
    ])
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

it('keeps the URL inside a multiline triple-backtick inline span literal', () => {
  const source = 'before ```https://example.com\ncontinuation``` after'
  expect(preprocessLinks(source)).toBe(source)
  expect(findCodeRanges(source)).toContainEqual({ start: 7, end: source.indexOf(' after') })
})

it('protects a URL on the second line of a multiline inline span', () => {
  const source = 'before ```first\nhttps://example.com\ncontinuation``` after'
  expect(preprocessLinks(source)).toBe(source)
})

it.each([
  ['- > ', '  > '],
  ['> - ', '>   '],
  ['- > - ', '  >   '],
  ['> - > ', '>   > '],
  ['1. > ', '   > '],
  ['> 12) > ', '>     > '],
])('protects mixed container fences starting with %j', (opening, continuation) => {
  const source = `${opening}\`\`\`text\n${continuation}https://example.com\n${continuation}\`\`\``
  expect(preprocessLinks(source)).toBe(source)
})

it('does not join inline code across a blank paragraph boundary', () => {
  const source = 'before `unmatched\n\nhttps://example.com\n` after'
  expect(preprocessLinks(source)).toBe(
    'before `unmatched\n\n[https://example.com](https://example.com)\n` after',
  )
})

it('does not join inline code across a CRLF blank line', () => {
  const source = 'before `unmatched\r\n\r\nhttps://example.com\r\n` after'
  expect(preprocessLinks(source)).toBe(
    'before `unmatched\r\n\r\n[https://example.com](https://example.com)\r\n` after',
  )
})

it('protects inline code immediately after a longer fenced closer', () => {
  const source = '```text\nliteral\n````\nbefore ``` https://example.com ``` after'
  expect(preprocessLinks(source)).toBe(source)
})

it('does not extend inline code across a fenced block', () => {
  const source = 'before `unmatched https://outside.example\n~~~text\n`literal\n~~~'
  expect(preprocessLinks(source)).toBe(
    'before `unmatched [https://outside.example](https://outside.example)\n~~~text\n`literal\n~~~',
  )
})

it.each(['x', ''])('scans repeated list prefixes without backtracking (%j)', (suffix) => {
  const source = '-  '.repeat(25) + suffix + '\n\nhttps://example.com'
  const start = performance.now()
  expect(preprocessLinks(source)).toBe(
    '-  '.repeat(25) + suffix + '\n\n[https://example.com](https://example.com)',
  )
  expect(performance.now() - start).toBeLessThan(250)
})
