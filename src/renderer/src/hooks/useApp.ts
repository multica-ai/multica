/**
 * Main application state hook
 */
import { useState, useEffect, useCallback } from 'react'
import type {
  MulticaSession,
  StoredSessionUpdate,
} from '../../../shared/types'
import type { RunningSessionsStatus } from '../../../shared/electron-api'
import { usePermissionStore } from '../stores/permissionStore'

export interface AppState {
  // Sessions
  sessions: MulticaSession[]
  currentSession: MulticaSession | null
  sessionUpdates: StoredSessionUpdate[]

  // Agent (per-session)
  runningSessionsStatus: RunningSessionsStatus
  isProcessing: boolean

  // UI
  error: string | null
}

export interface AppActions {
  // Session actions
  loadSessions: () => Promise<void>
  createSession: (cwd: string, agentId: string) => Promise<void>
  selectSession: (sessionId: string) => Promise<void>
  deleteSession: (sessionId: string) => Promise<void>
  clearCurrentSession: () => void

  // Agent actions (per-session)
  sendPrompt: (content: string) => Promise<void>
  cancelRequest: () => Promise<void>

  // UI actions
  clearError: () => void
}

export function useApp(): AppState & AppActions {
  // State
  const [sessions, setSessions] = useState<MulticaSession[]>([])
  const [currentSession, setCurrentSession] = useState<MulticaSession | null>(null)
  const [sessionUpdates, setSessionUpdates] = useState<StoredSessionUpdate[]>([])
  const [runningSessionsStatus, setRunningSessionsStatus] = useState<RunningSessionsStatus>({
    runningSessions: 0,
    sessionIds: [],
    processingSessionIds: [],
  })
  const [error, setError] = useState<string | null>(null)

  // Derive isProcessing from processingSessionIds (per-session isolation)
  const isProcessing = currentSession
    ? runningSessionsStatus.processingSessionIds.includes(currentSession.id)
    : false

  // Load sessions on mount
  useEffect(() => {
    loadSessions()
    loadRunningStatus()
  }, [])

  // Get current agentSessionId for stable reference in effect
  const currentAgentSessionId = currentSession?.agentSessionId

  // Subscribe to agent events
  useEffect(() => {
    const unsubMessage = window.electronAPI.onAgentMessage((message) => {
      // Only process messages for the current session
      // message.sessionId is ACP Agent Session ID, compare with currentAgentSessionId
      if (!currentAgentSessionId || message.sessionId !== currentAgentSessionId) {
        return
      }

      // Pass through original update without any accumulation
      // ChatView is responsible for accumulating chunks into complete messages
      setSessionUpdates((prev) => {
        const newUpdate = {
          timestamp: new Date().toISOString(),
          update: {
            sessionId: message.sessionId,
            update: message.update,
          },
        } as StoredSessionUpdate
        return [...prev, newUpdate]
      })
    })

    const unsubStatus = window.electronAPI.onAgentStatus((status) => {
      setRunningSessionsStatus(status)
    })

    const unsubError = window.electronAPI.onAgentError((err) => {
      setError(err.message)
    })

    // Subscribe to permission requests
    const setPendingRequest = usePermissionStore.getState().setPendingRequest
    const unsubPermission = window.electronAPI.onPermissionRequest((request) => {
      console.log('[useApp] Permission request received:', request)
      setPendingRequest(request)
    })

    return () => {
      unsubMessage()
      unsubStatus()
      unsubError()
      unsubPermission()
    }
  }, [currentAgentSessionId])

  // Actions
  const loadSessions = useCallback(async () => {
    try {
      const list = await window.electronAPI.listSessions()
      setSessions(list)
    } catch (err) {
      setError(`Failed to load sessions: ${err}`)
    }
  }, [])

  const loadRunningStatus = useCallback(async () => {
    try {
      const status = await window.electronAPI.getAgentStatus()
      setRunningSessionsStatus(status)
    } catch (err) {
      console.error('Failed to get running status:', err)
    }
  }, [])

  const createSession = useCallback(async (cwd: string, agentId: string) => {
    try {
      setError(null)
      const session = await window.electronAPI.createSession(cwd, agentId)
      setCurrentSession(session)
      setSessionUpdates([])
      await loadSessions()
      await loadRunningStatus()
    } catch (err) {
      setError(`Failed to create session: ${err}`)
    }
  }, [loadSessions, loadRunningStatus])

  const selectSession = useCallback(async (sessionId: string) => {
    try {
      setError(null)
      // Load session without starting agent (lazy loading)
      const session = await window.electronAPI.loadSession(sessionId)
      setCurrentSession(session)

      // Load session data for history
      const data = await window.electronAPI.getSession(sessionId)
      if (data) {
        setSessionUpdates(data.updates)
      }
    } catch (err) {
      setError(`Failed to select session: ${err}`)
    }
  }, [])

  const deleteSession = useCallback(async (sessionId: string) => {
    try {
      setError(null)
      await window.electronAPI.deleteSession(sessionId)
      if (currentSession?.id === sessionId) {
        setCurrentSession(null)
        setSessionUpdates([])
      }
      await loadSessions()
      await loadRunningStatus()
    } catch (err) {
      setError(`Failed to delete session: ${err}`)
    }
  }, [currentSession, loadSessions, loadRunningStatus])

  const clearCurrentSession = useCallback(() => {
    setCurrentSession(null)
    setSessionUpdates([])
  }, [])

  const sendPrompt = useCallback(async (content: string) => {
    if (!currentSession) {
      setError('No active session')
      return
    }

    try {
      setError(null)

      // Add user message to updates (use a custom marker for UI display)
      // 'user_message' is a custom type not in ACP SDK, used for UI purposes only
      const userUpdate = {
        timestamp: new Date().toISOString(),
        update: {
          sessionId: currentSession.agentSessionId,
          update: {
            sessionUpdate: 'user_message',
            content: { type: 'text', text: content },
          },
        },
      } as unknown as StoredSessionUpdate
      setSessionUpdates((prev) => [...prev, userUpdate])

      await window.electronAPI.sendPrompt(currentSession.id, content)
    } catch (err) {
      setError(`Failed to send prompt: ${err}`)
    }
  }, [currentSession])

  const cancelRequest = useCallback(async () => {
    if (!currentSession) return

    try {
      await window.electronAPI.cancelRequest(currentSession.id)
    } catch (err) {
      setError(`Failed to cancel: ${err}`)
    }
  }, [currentSession])

  const clearError = useCallback(() => {
    setError(null)
  }, [])

  return {
    // State
    sessions,
    currentSession,
    sessionUpdates,
    runningSessionsStatus,
    isProcessing,
    error,

    // Actions
    loadSessions,
    createSession,
    selectSession,
    deleteSession,
    clearCurrentSession,
    sendPrompt,
    cancelRequest,
    clearError,
  }
}
