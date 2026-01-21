/**
 * @vitest-environment jsdom
 */
import React, { act, useImperativeHandle } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createRoot, type Root } from 'react-dom/client'
import { useApp } from '../../../../src/renderer/src/hooks/useApp'
import { useDraftStore } from '../../../../src/renderer/src/stores/draftStore'

type AppHandle = {
  deleteSession: (sessionId: string) => Promise<void>
}

const AppHarness = React.forwardRef<AppHandle>((_, ref) => {
  const { deleteSession } = useApp()
  useImperativeHandle(ref, () => ({ deleteSession }), [deleteSession])
  return null
})

AppHarness.displayName = 'AppHarness'

describe('useApp deleteSession', () => {
  let container: HTMLDivElement
  let root: Root

  const createElectronApiMock = (): Record<string, ReturnType<typeof vi.fn>> => ({
    onSessionMetaUpdated: vi.fn(() => () => {}),
    onFileChanged: vi.fn(() => () => {}),
    onAgentMessage: vi.fn(() => () => {}),
    onAgentStatus: vi.fn(() => () => {}),
    onAgentError: vi.fn(() => () => {}),
    onPermissionRequest: vi.fn(() => () => {}),
    onAppFocus: vi.fn(() => () => {}),
    listSessions: vi.fn().mockResolvedValue([]),
    getAgentStatus: vi.fn().mockResolvedValue({
      runningSessions: 0,
      sessionIds: [],
      processingSessionIds: []
    }),
    getSessionModes: vi.fn().mockResolvedValue(null),
    getSessionModels: vi.fn().mockResolvedValue(null),
    getSessionCommands: vi.fn().mockResolvedValue([]),
    startFileWatch: vi.fn().mockResolvedValue(undefined),
    stopFileWatch: vi.fn().mockResolvedValue(undefined),
    deleteSession: vi.fn().mockResolvedValue(undefined)
  })

  beforeEach(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
    useDraftStore.setState({ drafts: {} })
    ;(window as unknown as { electronAPI: ReturnType<typeof createElectronApiMock> }).electronAPI =
      createElectronApiMock()
  })

  afterEach(() => {
    act(() => {
      root.unmount()
    })
    container.remove()
    globalThis.IS_REACT_ACT_ENVIRONMENT = false
    vi.clearAllMocks()
  })

  it('clears draft when deleting a session', async () => {
    useDraftStore.setState({
      drafts: { 'session-a': { text: 'draft', images: [] } }
    })

    const ref = React.createRef<AppHandle>()

    await act(async () => {
      root.render(<AppHarness ref={ref} />)
    })

    await act(async () => {
      await ref.current?.deleteSession('session-a')
    })

    const { drafts } = useDraftStore.getState()
    expect(drafts['session-a']).toBeUndefined()
  })
})
