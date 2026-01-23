/**
 * @vitest-environment jsdom
 */
import React, { act, useImperativeHandle } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createRoot, type Root } from 'react-dom/client'
import { useApp } from '../../../../src/renderer/src/hooks/useApp'
import { useDraftStore } from '../../../../src/renderer/src/stores/draftStore'
import type { StoredSessionUpdate, MulticaSession } from '../../../../src/shared/types'

type AppHandle = {
  deleteSession: (sessionId: string) => Promise<void>
  selectSession: (sessionId: string) => Promise<void>
  getSessionUpdates: () => StoredSessionUpdate[]
  getCurrentSession: () => MulticaSession | null
}

const AppHarness = React.forwardRef<AppHandle>((_, ref) => {
  const { deleteSession, selectSession, sessionUpdates, currentSession } = useApp()
  useImperativeHandle(
    ref,
    () => ({
      deleteSession,
      selectSession,
      getSessionUpdates: () => sessionUpdates,
      getCurrentSession: () => currentSession
    }),
    [deleteSession, selectSession, sessionUpdates, currentSession]
  )
  return null
})

AppHarness.displayName = 'AppHarness'

describe('useApp', () => {
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
    listProjectsWithSessions: vi.fn().mockResolvedValue([]),
    getAgentStatus: vi.fn().mockResolvedValue({
      runningSessions: 0,
      sessionIds: [],
      processingSessionIds: []
    }),
    loadSession: vi.fn().mockResolvedValue(null),
    getSession: vi.fn().mockResolvedValue({ session: null, updates: [] }),
    startSessionAgent: vi.fn().mockResolvedValue(null),
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

  it('merges reloaded updates with in-memory updates', async () => {
    let agentMessageHandler:
      | ((message: {
          sessionId: string
          multicaSessionId: string
          sequenceNumber?: number
          update: { sessionUpdate: string; content?: { type: string; text: string } }
          done: boolean
        }) => void)
      | undefined

    const session: MulticaSession = {
      id: 'session-a',
      agentSessionId: 'agent-1',
      projectId: 'project-1',
      agentId: 'codex',
      workingDirectory: '/tmp',
      createdAt: '2024-01-01T00:00:00.000Z',
      updatedAt: '2024-01-01T00:00:00.000Z',
      status: 'active',
      messageCount: 0
    }

    const update1: StoredSessionUpdate = {
      timestamp: '2024-01-01T00:00:01.000Z',
      sequenceNumber: 1,
      update: {
        sessionId: 'agent-1',
        update: {
          sessionUpdate: 'agent_message_chunk',
          content: { type: 'text', text: 'hello' }
        }
      }
    }

    const update2: StoredSessionUpdate = {
      timestamp: '2024-01-01T00:00:02.000Z',
      sequenceNumber: 2,
      update: {
        sessionId: 'agent-1',
        update: {
          sessionUpdate: 'agent_message_chunk',
          content: { type: 'text', text: 'world' }
        }
      }
    }

    const modesDeferred = createDeferred<null>()
    const modelsDeferred = createDeferred<null>()
    const commandsDeferred = createDeferred<unknown[]>()

    const electronAPI = createElectronApiMock()
    electronAPI.onAgentMessage.mockImplementation((cb) => {
      agentMessageHandler = cb
      return () => {}
    })
    const loadSessionDeferred = createDeferred<MulticaSession>()
    const getSessionDeferred = createDeferred<{
      session: MulticaSession
      updates: StoredSessionUpdate[]
    }>()
    electronAPI.loadSession.mockImplementation(() => loadSessionDeferred.promise)
    electronAPI.getAgentStatus.mockResolvedValue({
      runningSessions: 1,
      sessionIds: ['session-a'],
      processingSessionIds: []
    })
    electronAPI.getSession
      .mockImplementationOnce(() => getSessionDeferred.promise)
      .mockResolvedValueOnce({ session, updates: [update1, update2] })
    electronAPI.getSessionModes.mockImplementation(() => modesDeferred.promise)
    electronAPI.getSessionModels.mockImplementation(() => modelsDeferred.promise)
    electronAPI.getSessionCommands.mockImplementation(() => commandsDeferred.promise)
    ;(window as unknown as { electronAPI: typeof electronAPI }).electronAPI = electronAPI

    const ref = React.createRef<AppHandle>()

    await act(async () => {
      root.render(<AppHarness ref={ref} />)
    })

    await act(async () => {
      const selectPromise = ref.current?.selectSession('session-a')
      loadSessionDeferred.resolve(session)
      getSessionDeferred.resolve({ session, updates: [update1] })
      await waitForSessionUpdates(ref, 1)
      agentMessageHandler?.({
        sessionId: 'agent-1',
        multicaSessionId: 'session-a',
        sequenceNumber: 2,
        update: {
          sessionUpdate: 'agent_message_chunk',
          content: { type: 'text', text: 'world' }
        },
        done: false
      })
      modesDeferred.resolve(null)
      modelsDeferred.resolve(null)
      commandsDeferred.resolve([])
      await selectPromise
    })

    const updates = ref.current?.getSessionUpdates() ?? []
    const sequenceNumbers = updates
      .map((update) => update.sequenceNumber)
      .filter((seq): seq is number => seq !== undefined)
    expect(sequenceNumbers).toEqual([1, 2])
  })

  it('keeps in-memory updates when refreshed updates are empty', async () => {
    let agentMessageHandler:
      | ((message: {
          sessionId: string
          multicaSessionId: string
          sequenceNumber?: number
          update: { sessionUpdate: string; content?: { type: string; text: string } }
          done: boolean
        }) => void)
      | undefined

    const session: MulticaSession = {
      id: 'session-a',
      agentSessionId: 'agent-1',
      projectId: 'project-1',
      agentId: 'codex',
      workingDirectory: '/tmp',
      createdAt: '2024-01-01T00:00:00.000Z',
      updatedAt: '2024-01-01T00:00:00.000Z',
      status: 'active',
      messageCount: 0
    }

    const update1: StoredSessionUpdate = {
      timestamp: '2024-01-01T00:00:01.000Z',
      sequenceNumber: 1,
      update: {
        sessionId: 'agent-1',
        update: {
          sessionUpdate: 'agent_message_chunk',
          content: { type: 'text', text: 'hello' }
        }
      }
    }

    const modesDeferred = createDeferred<null>()
    const modelsDeferred = createDeferred<null>()
    const commandsDeferred = createDeferred<unknown[]>()

    const electronAPI = createElectronApiMock()
    electronAPI.onAgentMessage.mockImplementation((cb) => {
      agentMessageHandler = cb
      return () => {}
    })
    const loadSessionDeferred = createDeferred<MulticaSession>()
    const getSessionDeferred = createDeferred<{
      session: MulticaSession
      updates: StoredSessionUpdate[]
    }>()
    electronAPI.loadSession.mockImplementation(() => loadSessionDeferred.promise)
    electronAPI.getAgentStatus.mockResolvedValue({
      runningSessions: 1,
      sessionIds: ['session-a'],
      processingSessionIds: []
    })
    electronAPI.getSession
      .mockImplementationOnce(() => getSessionDeferred.promise)
      .mockResolvedValueOnce({ session, updates: [] })
    electronAPI.getSessionModes.mockImplementation(() => modesDeferred.promise)
    electronAPI.getSessionModels.mockImplementation(() => modelsDeferred.promise)
    electronAPI.getSessionCommands.mockImplementation(() => commandsDeferred.promise)
    ;(window as unknown as { electronAPI: typeof electronAPI }).electronAPI = electronAPI

    const ref = React.createRef<AppHandle>()

    await act(async () => {
      root.render(<AppHarness ref={ref} />)
    })

    await act(async () => {
      const selectPromise = ref.current?.selectSession('session-a')
      loadSessionDeferred.resolve(session)
      getSessionDeferred.resolve({ session, updates: [update1] })
      await waitForSessionUpdates(ref, 1)
      agentMessageHandler?.({
        sessionId: 'agent-1',
        multicaSessionId: 'session-a',
        sequenceNumber: 2,
        update: {
          sessionUpdate: 'agent_message_chunk',
          content: { type: 'text', text: 'world' }
        },
        done: false
      })
      modesDeferred.resolve(null)
      modelsDeferred.resolve(null)
      commandsDeferred.resolve([])
      await selectPromise
    })

    const updates = ref.current?.getSessionUpdates() ?? []
    const sequenceNumbers = updates
      .map((update) => update.sequenceNumber)
      .filter((seq): seq is number => seq !== undefined)
    expect(sequenceNumbers).toEqual([1, 2])
  })

  it('refreshes git branch when git HEAD changes for current session', async () => {
    let fileChangeHandler:
      | ((event: {
          directory: string
          eventType: 'change' | 'rename'
          path: string
          sessionIds: string[]
        }) => void)
      | undefined

    const session: MulticaSession = {
      id: 'session-a',
      projectId: 'project-1',
      agentSessionId: 'agent-1',
      agentId: 'codex',
      workingDirectory: '/repo',
      createdAt: '2024-01-01T00:00:00.000Z',
      updatedAt: '2024-01-01T00:00:00.000Z',
      status: 'active',
      messageCount: 0,
      gitBranch: 'main'
    }

    const refreshedSession: MulticaSession = {
      ...session,
      gitBranch: 'feature/new-branch'
    }

    const project = {
      id: 'project-1',
      name: 'repo',
      workingDirectory: '/repo',
      createdAt: '2024-01-01T00:00:00.000Z',
      updatedAt: '2024-01-01T00:00:00.000Z',
      isExpanded: true,
      sortOrder: 0
    }

    const electronAPI = createElectronApiMock()
    electronAPI.onFileChanged.mockImplementation((cb) => {
      fileChangeHandler = cb
      return () => {}
    })
    electronAPI.loadSession.mockResolvedValueOnce(session).mockResolvedValueOnce(refreshedSession)
    electronAPI.getSession.mockResolvedValue({ session, updates: [] })
    electronAPI.getAgentStatus.mockResolvedValue({
      runningSessions: 1,
      sessionIds: ['session-a'],
      processingSessionIds: []
    })
    electronAPI.getSessionModes.mockResolvedValue(null)
    electronAPI.getSessionModels.mockResolvedValue(null)
    electronAPI.getSessionCommands.mockResolvedValue([])
    electronAPI.listProjectsWithSessions
      .mockResolvedValueOnce([{ project, sessions: [session] }])
      .mockResolvedValueOnce([{ project, sessions: [refreshedSession] }])
    ;(window as unknown as { electronAPI: typeof electronAPI }).electronAPI = electronAPI

    const ref = React.createRef<AppHandle>()

    await act(async () => {
      root.render(<AppHarness ref={ref} />)
    })

    await act(async () => {
      await ref.current?.selectSession('session-a')
    })

    expect(fileChangeHandler).toBeDefined()

    await act(async () => {
      vi.useFakeTimers()
      fileChangeHandler?.({
        directory: '/repo',
        eventType: 'change',
        path: '/repo/.git/HEAD',
        sessionIds: ['session-a']
      })
      vi.advanceTimersByTime(150)
      vi.useRealTimers()
      await Promise.resolve()
    })

    expect(electronAPI.loadSession).toHaveBeenCalledTimes(2)
    expect(electronAPI.listProjectsWithSessions).toHaveBeenCalledTimes(2)
    expect(ref.current?.getCurrentSession()?.gitBranch).toBe('feature/new-branch')
  })
})

function createDeferred<T>(): {
  promise: Promise<T>
  resolve: (value: T) => void
  reject: (error: unknown) => void
} {
  let resolve: (value: T) => void = () => {}
  let reject: (error: unknown) => void = () => {}
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

async function waitForSessionUpdates(
  ref: React.RefObject<AppHandle>,
  expectedLength: number
): Promise<void> {
  for (let attempt = 0; attempt < 5; attempt += 1) {
    if ((ref.current?.getSessionUpdates().length ?? 0) >= expectedLength) {
      return
    }
    await act(async () => {
      await Promise.resolve()
    })
  }
}
