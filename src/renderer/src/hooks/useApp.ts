/**
 * Main application state hook
 */
import { useState, useEffect, useCallback, useRef } from 'react'
import type {
  MulticaSession,
  StoredSessionUpdate,
} from '../../../shared/types'
import type { RunningSessionsStatus } from '../../../shared/electron-api'

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

  // Track streaming text for current message
  const streamingTextRef = useRef<string>('')

  // Load sessions on mount
  useEffect(() => {
    loadSessions()
    loadRunningStatus()
  }, [])

  // Subscribe to agent events
  useEffect(() => {
    const unsubMessage = window.electronAPI.onAgentMessage((message) => {
      // Only process messages for the current session
      // message.sessionId is ACP Agent Session ID, compare with currentSession.agentSessionId
      if (!currentSession || message.sessionId !== currentSession.agentSessionId) {
        return
      }

      const update = message.update
      const updateType = update?.sessionUpdate

      // Handle streaming text accumulation for agent messages
      if (updateType === 'agent_message_chunk' && update.content?.type === 'text') {
        streamingTextRef.current += update.content.text
      }

      // Add the update to session updates for real-time display
      setSessionUpdates((prev) => {
        const now = new Date().toISOString()

        // For agent_message_chunk, we want to show accumulated text
        if (updateType === 'agent_message_chunk') {
          const streamingUpdate: StoredSessionUpdate = {
            timestamp: now,
            update: {
              sessionId: message.sessionId,
              update: {
                sessionUpdate: 'agent_message_chunk',
                content: { type: 'text', text: streamingTextRef.current },
              },
            },
          }

          // Replace last streaming update if it was also agent_message_chunk
          const lastIdx = prev.length - 1
          if (lastIdx >= 0) {
            const last = prev[lastIdx]
            if (
              last.update.update &&
              'sessionUpdate' in last.update.update &&
              last.update.update.sessionUpdate === 'agent_message_chunk'
            ) {
              return [...prev.slice(0, lastIdx), streamingUpdate]
            }
          }
          return [...prev, streamingUpdate]
        }

        // For all other update types, add them directly
        const newUpdate = {
          timestamp: now,
          update: {
            sessionId: message.sessionId,
            update: update,
          },
        } as StoredSessionUpdate
        return [...prev, newUpdate]
      })

      if (message.done) {
        streamingTextRef.current = ''
      }
    })

    const unsubStatus = window.electronAPI.onAgentStatus((status) => {
      setRunningSessionsStatus(status)
    })

    const unsubError = window.electronAPI.onAgentError((err) => {
      setError(err.message)
    })

    return () => {
      unsubMessage()
      unsubStatus()
      unsubError()
    }
  }, [currentSession])

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

  const sendPrompt = useCallback(async (content: string) => {
    if (!currentSession) {
      setError('No active session')
      return
    }

    try {
      setError(null)
      streamingTextRef.current = ''

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
    sendPrompt,
    cancelRequest,
    clearError,
  }
}
