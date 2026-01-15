/**
 * Main application state hook
 */
import { useState, useEffect, useCallback } from 'react'
import type { MulticaSession, StoredSessionUpdate } from '../../../shared/types'
import type { RunningSessionsStatus } from '../../../shared/electron-api'
import type { MessageContent } from '../../../shared/types/message'
import { usePermissionStore } from '../stores/permissionStore'
import { useFileChangeStore } from '../stores/fileChangeStore'
import { toast } from 'sonner'
import { getErrorMessage } from '../utils/error'

export interface AppState {
  // Sessions
  sessions: MulticaSession[]
  currentSession: MulticaSession | null
  sessionUpdates: StoredSessionUpdate[]

  // Agent (per-session)
  runningSessionsStatus: RunningSessionsStatus
  isProcessing: boolean
  isInitializing: boolean

  // UI
  isSwitchingAgent: boolean
}

export interface AppActions {
  // Session actions
  loadSessions: () => Promise<void>
  createSession: (cwd: string, agentId: string) => Promise<void>
  selectSession: (sessionId: string) => Promise<void>
  deleteSession: (sessionId: string) => Promise<void>
  clearCurrentSession: () => void

  // Agent actions (per-session)
  sendPrompt: (content: MessageContent) => Promise<void>
  cancelRequest: () => Promise<void>
  switchSessionAgent: (newAgentId: string) => Promise<void>
}

export function useApp(): AppState & AppActions {
  // State
  const [sessions, setSessions] = useState<MulticaSession[]>([])
  const [currentSession, setCurrentSession] = useState<MulticaSession | null>(null)
  const [sessionUpdates, setSessionUpdates] = useState<StoredSessionUpdate[]>([])
  const [runningSessionsStatus, setRunningSessionsStatus] = useState<RunningSessionsStatus>({
    runningSessions: 0,
    sessionIds: [],
    processingSessionIds: []
  })
  const [isInitializing, setIsInitializing] = useState(false)
  const [isSwitchingAgent, setIsSwitchingAgent] = useState(false)

  // Derive isProcessing from processingSessionIds (per-session isolation)
  const isProcessing = currentSession
    ? runningSessionsStatus.processingSessionIds.includes(currentSession.id)
    : false

  // Note: File tree refresh is triggered by tool completion (see onAgentMessage handler below)
  // No need for periodic refresh - it causes performance issues

  // Load sessions on mount
  useEffect(() => {
    loadSessions()
    loadRunningStatus()
  }, [])

  // Get current session ID for stable reference in effect
  const currentSessionId = currentSession?.id

  // Subscribe to session metadata updates (e.g., when agentSessionId changes after lazy start)
  // This is critical for receiving messages after app restart
  useEffect(() => {
    const unsubSessionMeta = window.electronAPI.onSessionMetaUpdated((updatedSession) => {
      // Only update if this is the current session
      if (currentSessionId && updatedSession.id === currentSessionId) {
        console.log(
          '[useApp] Session meta updated:',
          updatedSession.id,
          'agentSessionId:',
          updatedSession.agentSessionId
        )
        setCurrentSession(updatedSession)
      }
    })

    return () => {
      unsubSessionMeta()
    }
  }, [currentSessionId])

  // Get current agentSessionId for stable reference in effect
  const currentAgentSessionId = currentSession?.agentSessionId

  // Subscribe to agent events
  useEffect(() => {
    // Get triggerRefresh from store for file change detection
    const triggerRefresh = useFileChangeStore.getState().triggerRefresh

    // Tool kinds that modify files (case-insensitive)
    const FILE_MODIFYING_TOOLS = new Set(['write', 'edit', 'notebookedit', 'bash'])

    const unsubMessage = window.electronAPI.onAgentMessage((message) => {
      // Only process messages for the current session
      // message.sessionId is ACP Agent Session ID, compare with currentAgentSessionId
      if (!currentAgentSessionId || message.sessionId !== currentAgentSessionId) {
        return
      }

      // Check for file-modifying tool completion to trigger FileTree refresh
      const update = message.update
      const kind = update?.kind?.toLowerCase() || ''
      const status = update?.status?.toLowerCase() || ''

      // Debug logging for tool updates
      if (update?.sessionUpdate === 'tool_call_update' || update?.sessionUpdate === 'tool_call') {
        console.log('[FileChange] Tool event:', {
          sessionUpdate: update.sessionUpdate,
          kind,
          status,
          title: update?.title,
          rawUpdate: update
        })
      }

      // Trigger refresh when a file-modifying tool completes
      // More lenient: trigger on any status that isn't "running" or "pending" or "in_progress"
      const isCompleted = status && !['running', 'pending', 'in_progress', ''].includes(status)
      if (
        update?.sessionUpdate === 'tool_call_update' &&
        FILE_MODIFYING_TOOLS.has(kind) &&
        isCompleted
      ) {
        console.log('[FileChange] Triggering refresh for:', { kind, status })
        triggerRefresh()
      }

      // Pass through original update without any accumulation
      // ChatView is responsible for accumulating chunks into complete messages
      // Include sequence number for proper ordering of concurrent updates
      setSessionUpdates((prev) => {
        const newUpdate = {
          timestamp: new Date().toISOString(),
          sequenceNumber: message.sequenceNumber,
          update: {
            sessionId: message.sessionId,
            update: message.update
          }
        } as StoredSessionUpdate
        return [...prev, newUpdate]
      })
    })

    const unsubStatus = window.electronAPI.onAgentStatus((status) => {
      setRunningSessionsStatus(status)
    })

    const unsubError = window.electronAPI.onAgentError((err) => {
      toast.error(err.message)
    })

    // Subscribe to permission requests
    const addPendingRequest = usePermissionStore.getState().addPendingRequest
    const unsubPermission = window.electronAPI.onPermissionRequest((request) => {
      console.log('[useApp] Permission request received:', request)
      addPendingRequest(request)
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
      toast.error(`Failed to load sessions: ${getErrorMessage(err)}`)
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

  const createSession = useCallback(
    async (cwd: string, agentId: string) => {
      try {
        setIsInitializing(true)
        const session = await window.electronAPI.createSession(cwd, agentId)
        setCurrentSession(session)
        setSessionUpdates([])
        await loadSessions()
        await loadRunningStatus()
      } catch (err) {
        toast.error(`Failed to create session: ${getErrorMessage(err)}`)
      } finally {
        setIsInitializing(false)
      }
    },
    [loadSessions, loadRunningStatus]
  )

  const selectSession = useCallback(async (sessionId: string) => {
    try {
      // Load session without starting agent (lazy loading)
      const session = await window.electronAPI.loadSession(sessionId)
      setCurrentSession(session)

      // Load session data for history
      const data = await window.electronAPI.getSession(sessionId)
      if (data) {
        setSessionUpdates(data.updates)
      }
    } catch (err) {
      toast.error(`Failed to select session: ${getErrorMessage(err)}`)
    }
  }, [])

  const deleteSession = useCallback(
    async (sessionId: string) => {
      try {
        await window.electronAPI.deleteSession(sessionId)
        if (currentSession?.id === sessionId) {
          setCurrentSession(null)
          setSessionUpdates([])
        }
        await loadSessions()
        await loadRunningStatus()
      } catch (err) {
        toast.error(`Failed to delete session: ${getErrorMessage(err)}`)
      }
    },
    [currentSession, loadSessions, loadRunningStatus]
  )

  const clearCurrentSession = useCallback(() => {
    setCurrentSession(null)
    setSessionUpdates([])
  }, [])

  const sendPrompt = useCallback(
    async (content: MessageContent) => {
      if (!currentSession) {
        toast.error('No active session')
        return
      }

      try {
        // Add user message to updates (use a custom marker for UI display)
        // 'user_message' is a custom type not in ACP SDK, used for UI purposes only
        const userUpdate = {
          timestamp: new Date().toISOString(),
          update: {
            sessionId: currentSession.agentSessionId,
            update: {
              sessionUpdate: 'user_message',
              content: content // Now stores full MessageContent array
            }
          }
        } as unknown as StoredSessionUpdate
        setSessionUpdates((prev) => [...prev, userUpdate])

        await window.electronAPI.sendPrompt(currentSession.id, content)
      } catch (err) {
        toast.error(`Failed to send prompt: ${getErrorMessage(err)}`)
      }
    },
    [currentSession]
  )

  const cancelRequest = useCallback(async () => {
    if (!currentSession) return

    try {
      await window.electronAPI.cancelRequest(currentSession.id)
    } catch (err) {
      toast.error(`Failed to cancel: ${getErrorMessage(err)}`)
    }
  }, [currentSession])

  const switchSessionAgent = useCallback(
    async (newAgentId: string) => {
      if (!currentSession) {
        toast.error('No active session')
        return
      }

      try {
        setIsSwitchingAgent(true)
        const updatedSession = await window.electronAPI.switchSessionAgent(
          currentSession.id,
          newAgentId
        )
        setCurrentSession(updatedSession)
        await loadRunningStatus()
        toast.success(`Switched to ${newAgentId}`)
      } catch (err) {
        toast.error(`Failed to switch agent: ${getErrorMessage(err)}`)
      } finally {
        setIsSwitchingAgent(false)
      }
    },
    [currentSession, loadRunningStatus]
  )

  return {
    // State
    sessions,
    currentSession,
    sessionUpdates,
    runningSessionsStatus,
    isProcessing,
    isInitializing,
    isSwitchingAgent,

    // Actions
    loadSessions,
    createSession,
    selectSession,
    deleteSession,
    clearCurrentSession,
    sendPrompt,
    cancelRequest,
    switchSessionAgent
  }
}
