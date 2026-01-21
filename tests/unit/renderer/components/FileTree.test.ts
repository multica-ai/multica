/**
 * Tests for FileTree helper functions
 */
import { describe, expect, it } from 'vitest'
import { excludeHiddenFiles } from '../../../../src/renderer/src/utils/fileTree'
import type { FileTreeNode } from '../../../../src/shared/electron-api'

describe('excludeHiddenFiles', () => {
  const createNode = (name: string, type: 'file' | 'directory' = 'file'): FileTreeNode => ({
    name,
    path: `/test/${name}`,
    type,
    extension: type === 'file' ? name.split('.').pop() : undefined
  })

  it('returns an empty array when given an empty array', () => {
    const result = excludeHiddenFiles([])
    expect(result).toEqual([])
  })

  it('removes files starting with a dot', () => {
    const nodes = [createNode('.gitignore'), createNode('README.md'), createNode('.env')]

    const result = excludeHiddenFiles(nodes)

    expect(result).toHaveLength(1)
    expect(result[0].name).toBe('README.md')
  })

  it('removes directories starting with a dot', () => {
    const nodes = [
      createNode('.git', 'directory'),
      createNode('src', 'directory'),
      createNode('.vscode', 'directory'),
      createNode('node_modules', 'directory')
    ]

    const result = excludeHiddenFiles(nodes)

    expect(result).toHaveLength(2)
    expect(result.map((n) => n.name)).toEqual(['src', 'node_modules'])
  })

  it('keeps all nodes when none are hidden', () => {
    const nodes = [
      createNode('index.ts'),
      createNode('package.json'),
      createNode('src', 'directory')
    ]

    const result = excludeHiddenFiles(nodes)

    expect(result).toHaveLength(3)
    expect(result).toEqual(nodes)
  })

  it('removes all nodes when all are hidden', () => {
    const nodes = [createNode('.gitignore'), createNode('.env'), createNode('.git', 'directory')]

    const result = excludeHiddenFiles(nodes)

    expect(result).toHaveLength(0)
  })

  it('handles mixed hidden files and directories', () => {
    const nodes = [
      createNode('.git', 'directory'),
      createNode('.gitignore'),
      createNode('src', 'directory'),
      createNode('README.md'),
      createNode('.env'),
      createNode('package.json'),
      createNode('.vscode', 'directory')
    ]

    const result = excludeHiddenFiles(nodes)

    expect(result).toHaveLength(3)
    expect(result.map((n) => n.name)).toEqual(['src', 'README.md', 'package.json'])
  })

  it('does not modify the original array', () => {
    const nodes = [createNode('.gitignore'), createNode('README.md')]
    const originalLength = nodes.length

    excludeHiddenFiles(nodes)

    expect(nodes).toHaveLength(originalLength)
  })

  it('handles files with dots in the middle of the name', () => {
    const nodes = [
      createNode('file.test.ts'),
      createNode('component.spec.tsx'),
      createNode('.hidden.file'),
      createNode('normal.config.js')
    ]

    const result = excludeHiddenFiles(nodes)

    expect(result).toHaveLength(3)
    expect(result.map((n) => n.name)).toEqual([
      'file.test.ts',
      'component.spec.tsx',
      'normal.config.js'
    ])
  })
})
