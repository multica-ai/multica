/**
 * @vitest-environment jsdom
 */
import React, { act } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createRoot, type Root } from 'react-dom/client'
import { Settings } from '../../../../src/renderer/src/components/Settings'

// Mock UI components
vi.mock('@/components/ui/dialog', () => ({
  Dialog: ({ children, open }: { children: React.ReactNode; open: boolean }) =>
    open ? <div data-testid="dialog">{children}</div> : null,
  DialogContent: ({ children }: { children: React.ReactNode }) => (
    <div data-testid="dialog-content">{children}</div>
  ),
  DialogHeader: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DialogTitle: ({ children }: { children: React.ReactNode }) => <h1>{children}</h1>
}))

vi.mock('@/components/ui/toggle-group', () => ({
  ToggleGroup: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  ToggleGroupItem: ({ children }: { children: React.ReactNode }) => <button>{children}</button>
}))

vi.mock('../../../../src/renderer/src/contexts/ThemeContext', () => ({
  useTheme: () => ({ mode: 'system', setMode: vi.fn() })
}))

vi.mock('@/lib/utils', () => ({
  cn: (...args: (string | boolean | undefined)[]) => args.filter(Boolean).join(' ')
}))

describe('Settings - Software Update Section', () => {
  let container: HTMLDivElement
  let root: Root
  let updateStatusCallback: ((status: unknown) => void) | null = null

  const mockElectronAPI = {
    checkAgent: vi.fn().mockResolvedValue(null),
    checkAgentLatestVersions: vi.fn().mockResolvedValue({ agentId: '', commands: [] }),
    getAppVersion: vi.fn().mockResolvedValue('0.1.5'),
    checkForUpdates: vi.fn().mockResolvedValue(undefined),
    downloadUpdate: vi.fn().mockResolvedValue(undefined),
    installUpdate: vi.fn().mockResolvedValue(undefined),
    onUpdateStatus: vi.fn((cb: (status: unknown) => void) => {
      updateStatusCallback = cb
      return () => {
        updateStatusCallback = null
      }
    }),
    installAgent: vi.fn(),
    updateCommand: vi.fn()
  }

  beforeEach(() => {
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
    ;(window as unknown as { electronAPI: typeof mockElectronAPI }).electronAPI = mockElectronAPI
    updateStatusCallback = null
  })

  afterEach(() => {
    act(() => root.unmount())
    document.body.removeChild(container)
    vi.clearAllMocks()
  })

  const renderSettings = (): void => {
    act(() => {
      root.render(
        <Settings
          isOpen={true}
          onClose={vi.fn()}
          defaultAgentId="claude-code"
          onSetDefaultAgent={vi.fn()}
        />
      )
    })
  }

  it('displays current app version', async () => {
    await act(async () => {
      renderSettings()
    })
    // Wait for async getAppVersion to resolve
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0))
    })

    expect(mockElectronAPI.getAppVersion).toHaveBeenCalled()
    expect(container.textContent).toContain('0.1.5')
    expect(container.textContent).toContain('Current Version:')
  })

  it('shows "Check for Updates" button when no update status', async () => {
    await act(async () => {
      renderSettings()
    })
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0))
    })

    expect(container.textContent).toContain('Check for Updates')
  })

  it('shows checking state when checking for updates', async () => {
    await act(async () => {
      renderSettings()
    })
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0))
    })

    act(() => {
      updateStatusCallback?.({ status: 'checking' })
    })

    expect(container.textContent).toContain('Checking...')
  })

  it('shows download button when update is available', async () => {
    await act(async () => {
      renderSettings()
    })
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0))
    })

    act(() => {
      updateStatusCallback?.({ status: 'available', info: { version: '0.2.0' } })
    })

    expect(container.textContent).toContain('v0.2.0 available')
    expect(container.textContent).toContain('Download')
  })

  it('shows downloading state with progress', async () => {
    await act(async () => {
      renderSettings()
    })
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0))
    })

    act(() => {
      updateStatusCallback?.({ status: 'downloading', progress: { percent: 45 } })
    })

    expect(container.textContent).toContain('Downloading...')
    expect(container.textContent).toContain('45%')
  })

  it('shows restart button when update is downloaded', async () => {
    await act(async () => {
      renderSettings()
    })
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0))
    })

    act(() => {
      updateStatusCallback?.({ status: 'downloaded', info: { version: '0.2.0' } })
    })

    expect(container.textContent).toContain('Restart to Update')
  })

  it('shows "Up to date" when no update available', async () => {
    await act(async () => {
      renderSettings()
    })
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0))
    })

    act(() => {
      updateStatusCallback?.({ status: 'not-available' })
    })

    expect(container.textContent).toContain('Up to date')
  })

  it('shows error state when update fails', async () => {
    await act(async () => {
      renderSettings()
    })
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0))
    })

    act(() => {
      updateStatusCallback?.({ status: 'error', error: 'Network error' })
    })

    expect(container.textContent).toContain('Update failed')
  })

  it('calls checkForUpdates when button is clicked', async () => {
    await act(async () => {
      renderSettings()
    })
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0))
    })

    const buttons = container.querySelectorAll('button')
    const checkButton = Array.from(buttons).find((b) =>
      b.textContent?.includes('Check for Updates')
    )
    expect(checkButton).toBeTruthy()

    act(() => {
      checkButton?.click()
    })

    expect(mockElectronAPI.checkForUpdates).toHaveBeenCalled()
  })

  it('calls downloadUpdate when download button is clicked', async () => {
    await act(async () => {
      renderSettings()
    })
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0))
    })

    act(() => {
      updateStatusCallback?.({ status: 'available', info: { version: '0.2.0' } })
    })

    const buttons = container.querySelectorAll('button')
    const downloadButton = Array.from(buttons).find((b) => b.textContent?.includes('Download'))
    expect(downloadButton).toBeTruthy()

    act(() => {
      downloadButton?.click()
    })

    expect(mockElectronAPI.downloadUpdate).toHaveBeenCalled()
  })

  it('calls installUpdate when restart button is clicked', async () => {
    await act(async () => {
      renderSettings()
    })
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0))
    })

    act(() => {
      updateStatusCallback?.({ status: 'downloaded', info: { version: '0.2.0' } })
    })

    const buttons = container.querySelectorAll('button')
    const restartButton = Array.from(buttons).find((b) =>
      b.textContent?.includes('Restart to Update')
    )
    expect(restartButton).toBeTruthy()

    act(() => {
      restartButton?.click()
    })

    expect(mockElectronAPI.installUpdate).toHaveBeenCalled()
  })

  it('subscribes to update status on mount', async () => {
    await act(async () => {
      renderSettings()
    })

    expect(mockElectronAPI.onUpdateStatus).toHaveBeenCalled()
  })
})
