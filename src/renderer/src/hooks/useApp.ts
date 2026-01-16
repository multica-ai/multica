/**
 * Main application state hook
 */
import { useState, useEffect, useCallback, useRef } from 'react'
import type { MulticaSession, StoredSessionUpdate } from '../../../shared/types'
import type { RunningSessionsStatus } from '../../../shared/electron-api'
import type { MessageContent } from '../../../shared/types/message'
import { usePermissionStore } from '../stores/permissionStore'
import { useFileChangeStore } from '../stores/fileChangeStore'
import { toast } from 'sonner'
import { getErrorMessage } from '../utils/error'

// ACP standard tool kinds that modify files (used for Codex and other agents)
const FILE_MODIFYING_KINDS = new Set(['edit', 'write', 'delete', 'execute'])
// Actual tool names from _meta.claudeCode.toolName (used for Claude Code)
const FILE_MODIFYING_TOOL_NAMES = new Set(['write', 'edit', 'bash', 'notebookedit'])

// Auth commands for each agent
const AGENT_AUTH_COMMANDS: Record<string, string> = {
  'claude-code': 'claude login',
  opencode: 'opencode auth',
  codex: 'codex auth'
}

// Check if error is authentication related
function isAuthError(errorMessage: string): boolean {
  const authKeywords = ['authentication required', 'unauthorized', 'not authenticated', 'login required']
  const lowerMessage = errorMessage.toLowerCase()
  return authKeywords.some((keyword) => lowerMessage.includes(keyword))
}

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
  // Store toolCallId -> kind mapping for file tree refresh
  // This is needed because tool_call_update events don't include kind
  const toolKindMapRef = useRef<Map<string, string>>(new Map())

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

    const unsubMessage = window.electronAPI.onAgentMessage((message) => {
      // Only process messages for the current session
      // message.sessionId is ACP Agent Session ID, compare with currentAgentSessionId
      if (!currentAgentSessionId || message.sessionId !== currentAgentSessionId) {
        return
      }

      // Check for file-modifying tool completion to trigger FileTree refresh
      const update = message.update
      const status = update?.status?.toLowerCase() || ''
      const toolCallId = update?.toolCallId

      // Get tool name from _meta.claudeCode.toolName (Claude Code specific)
      const meta = update?._meta as { claudeCode?: { toolName?: string } } | undefined
      const toolName = meta?.claudeCode?.toolName?.toLowerCase() || ''

      // Handle tool_call event: store toolCallId -> kind mapping
      // This is needed because tool_call_update events don't include kind (for Codex etc.)
      if (update?.sessionUpdate === 'tool_call' && toolCallId && update?.kind) {
        const kind = update.kind.toLowerCase()
        toolKindMapRef.current.set(toolCallId, kind)
      }

      // Handle tool_call_update event: check if we should trigger refresh
      if (update?.sessionUpdate === 'tool_call_update' && toolCallId) {
        // Get kind from our stored mapping (for Codex) or from toolName (for Claude Code)
        const storedKind = toolKindMapRef.current.get(toolCallId) || ''
        const isFileModifying =
          FILE_MODIFYING_KINDS.has(storedKind) || FILE_MODIFYING_TOOL_NAMES.has(toolName)
        const isCompleted = status === 'completed' || status === 'failed'

        if (isFileModifying && isCompleted) {
          triggerRefresh()
          // Clean up the mapping
          toolKindMapRef.current.delete(toolCallId)
        }
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
      // Clear the toolKindMap when session changes to avoid stale data
      toolKindMapRef.current.clear()
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
        const errorMessage = getErrorMessage(err)

        // Check if this is an authentication error
        if (isAuthError(errorMessage)) {
          // Get the auth command for the current agent
          const authCommand = AGENT_AUTH_COMMANDS[currentSession.agentId] || 'Please authenticate'

          // Add error message to chat instead of toast
          const errorUpdate = {
            timestamp: new Date().toISOString(),
            update: {
              sessionId: currentSession.agentSessionId || currentSession.id,
              update: {
                sessionUpdate: 'error_message',
                errorType: 'auth',
                agentId: currentSession.agentId,
                authCommand: authCommand,
                message: errorMessage
              }
            }
          } as unknown as StoredSessionUpdate
          setSessionUpdates((prev) => [...prev, errorUpdate])
        } else {
          // For other errors, use toast
          toast.error(`Failed to send prompt: ${errorMessage}`)
        }
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
        toast.success(`Successfully switched to ${newAgentId}`)
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
