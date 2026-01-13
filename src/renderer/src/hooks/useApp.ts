/**
 * Main application state hook
 */
import { useState, useEffect, useCallback, useRef } from 'react'
import type {
  MulticaSession,
  AgentStatus,
  StoredSessionUpdate,
} from '../../../shared/types'

export interface AppState {
  // Sessions
  sessions: MulticaSession[]
  currentSession: MulticaSession | null
  sessionUpdates: StoredSessionUpdate[]

  // Agent
  agentStatus: AgentStatus
  isProcessing: boolean

  // UI
  error: string | null
}

export interface AppActions {
  // Session actions
  loadSessions: () => Promise<void>
  createSession: (cwd: string) => Promise<void>
  selectSession: (sessionId: string) => Promise<void>
  deleteSession: (sessionId: string) => Promise<void>

  // Agent actions
  startAgent: (agentId: string) => Promise<void>
  stopAgent: () => Promise<void>
  switchAgent: (agentId: string) => Promise<void>
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
  const [agentStatus, setAgentStatus] = useState<AgentStatus>({ state: 'stopped' })
  const [isProcessing, setIsProcessing] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Track streaming text for current message
  const streamingTextRef = useRef<string>('')

  // Load sessions on mount
  useEffect(() => {
    loadSessions()
    loadAgentStatus()
  }, [])

  // Subscribe to agent events
  useEffect(() => {
    const unsubMessage = window.electronAPI.onAgentMessage((message) => {
      // Accumulate streaming text
      for (const content of message.content) {
        if (content.type === 'text') {
          streamingTextRef.current += content.text
        }
      }

      // Create/update the streaming message in updates
      setSessionUpdates((prev) => {
        const now = new Date().toISOString()
        // Find or create streaming message update
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

        // Replace last streaming update or add new one
        const lastIdx = prev.length - 1
        if (lastIdx >= 0 && !message.done) {
          const last = prev[lastIdx]
          if (
            last.update.update &&
            'sessionUpdate' in last.update.update &&
            last.update.update.sessionUpdate === 'agent_message_chunk'
          ) {
            return [...prev.slice(0, lastIdx), streamingUpdate]
          }
        }

        if (message.done) {
          streamingTextRef.current = ''
          setIsProcessing(false)
        }

        return [...prev, streamingUpdate]
      })
    })

    const unsubStatus = window.electronAPI.onAgentStatus((status) => {
      setAgentStatus(status)
    })

    const unsubError = window.electronAPI.onAgentError((err) => {
      setError(err.message)
      setIsProcessing(false)
    })

    return () => {
      unsubMessage()
      unsubStatus()
      unsubError()
    }
  }, [])

  // Actions
  const loadSessions = useCallback(async () => {
    try {
      const list = await window.electronAPI.listSessions()
      setSessions(list)
    } catch (err) {
      setError(`Failed to load sessions: ${err}`)
    }
  }, [])

  const loadAgentStatus = useCallback(async () => {
    try {
      const status = await window.electronAPI.getAgentStatus()
      setAgentStatus(status)
    } catch (err) {
      console.error('Failed to get agent status:', err)
    }
  }, [])

  const createSession = useCallback(async (cwd: string) => {
    try {
      setError(null)
      const session = await window.electronAPI.createSession(cwd)
      setCurrentSession(session)
      setSessionUpdates([])
      await loadSessions()
    } catch (err) {
      setError(`Failed to create session: ${err}`)
    }
  }, [loadSessions])

  const selectSession = useCallback(async (sessionId: string) => {
    try {
      setError(null)
      // Resume session (creates new ACP session, loads history)
      const session = await window.electronAPI.resumeSession(sessionId)
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
    } catch (err) {
      setError(`Failed to delete session: ${err}`)
    }
  }, [currentSession, loadSessions])

  const startAgent = useCallback(async (agentId: string) => {
    try {
      setError(null)
      await window.electronAPI.startAgent(agentId)
      await loadAgentStatus()
    } catch (err) {
      setError(`Failed to start agent: ${err}`)
    }
  }, [loadAgentStatus])

  const stopAgent = useCallback(async () => {
    try {
      setError(null)
      await window.electronAPI.stopAgent()
      await loadAgentStatus()
    } catch (err) {
      setError(`Failed to stop agent: ${err}`)
    }
  }, [loadAgentStatus])

  const switchAgent = useCallback(async (agentId: string) => {
    try {
      setError(null)
      // Stop current agent first
      await window.electronAPI.stopAgent()
      // Clear current session (it's bound to the old agent)
      setCurrentSession(null)
      setSessionUpdates([])
      // Start new agent
      await window.electronAPI.startAgent(agentId)
      await loadAgentStatus()
    } catch (err) {
      setError(`Failed to switch agent: ${err}`)
    }
  }, [loadAgentStatus])

  const sendPrompt = useCallback(async (content: string) => {
    if (!currentSession) {
      setError('No active session')
      return
    }

    try {
      setError(null)
      setIsProcessing(true)
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
      setIsProcessing(false)
    } catch (err) {
      setError(`Failed to send prompt: ${err}`)
      setIsProcessing(false)
    }
  }, [currentSession])

  const cancelRequest = useCallback(async () => {
    if (!currentSession) return

    try {
      await window.electronAPI.cancelRequest(currentSession.id)
      setIsProcessing(false)
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
    agentStatus,
    isProcessing,
    error,

    // Actions
    loadSessions,
    createSession,
    selectSession,
    deleteSession,
    startAgent,
    stopAgent,
    switchAgent,
    sendPrompt,
    cancelRequest,
    clearError,
  }
}
