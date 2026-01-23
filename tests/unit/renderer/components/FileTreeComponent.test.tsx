/**
 * @vitest-environment jsdom
 */
import React, { act } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createRoot, type Root } from 'react-dom/client'
import { FileTree } from '../../../../src/renderer/src/components/FileTree'

const flushPromises = (): Promise<void> =>
  new Promise((resolve) => {
    setTimeout(resolve, 0)
  })

describe('FileTree', () => {
  let container: HTMLDivElement
  let root: Root

  const electronAPI = {
    listDirectory: vi.fn(),
    detectApps: vi.fn(),
    openWith: vi.fn()
  }

  beforeEach(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
    electronAPI.listDirectory.mockResolvedValue([])
    electronAPI.detectApps.mockResolvedValue([])
    ;(window as typeof window & { electronAPI: typeof electronAPI }).electronAPI = electronAPI
  })

  afterEach(() => {
    act(() => {
      root.unmount()
    })
    container.remove()
    globalThis.IS_REACT_ACT_ENVIRONMENT = false
    vi.clearAllMocks()
  })

  it('shows the root row even when the directory is empty', async () => {
    act(() => {
      root.render(<FileTree rootPath="/tmp/project" />)
    })

    await act(async () => {
      await flushPromises()
    })

    expect(electronAPI.listDirectory).toHaveBeenCalledWith('/tmp/project')
    expect(container.textContent).toContain('project')
    expect(container.textContent).toContain('(empty)')
    expect(container.textContent).not.toContain('Empty directory')
  })
})
