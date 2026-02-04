/**
 * Agent status hook
 * Handles agent running/processing state and agent-related actions
 */
import { useState, useCallback } from 'react'
import type {
  MulticaSession,
  StoredSessionUpdate,
  SessionModeState,
  SessionModelState,
  SessionModeId,
  ModelId
} from '../../../shared/types'
import type { RunningSessionsStatus } from '../../../shared/electron-api'
import type { MessageContent } from '../../../shared/types/message'
import { useAgentStore } from '../stores/agentStore'
import { toast } from 'sonner'
import { getErrorMessage } from '../utils/error'

// Auth commands for each agent
const AGENT_AUTH_COMMANDS: Record<string, string> = {
  'claude-code': 'claude login',
  opencode: 'opencode auth',
  codex: 'codex auth'
}

// Check if error is authentication related
function isAuthError(errorMessage: string): boolean {
  const authKeywords = [
    'authentication required',
    'unauthorized',
    'not authenticated',
    'login required'
  ]
  const lowerMessage = errorMessage.toLowerCase()
  return authKeywords.some((keyword) => lowerMessage.includes(keyword))
}

export interface AgentStatusState {
  runningSessionsStatus: RunningSessionsStatus
  isSwitchingAgent: boolean
}

export interface AgentStatusActions {
  setRunningSessionsStatus: React.Dispatch<React.SetStateAction<RunningSessionsStatus>>
  loadRunningStatus: () => Promise<void>
  sendPrompt: (
    content: MessageContent,
    currentSession: MulticaSession | null,
    setSessionUpdates: React.Dispatch<React.SetStateAction<StoredSessionUpdate[]>>,
    loadSessionModeModel: (sessionId: string) => Promise<void>
  ) => Promise<void>
  cancelRequest: (currentSession: MulticaSession | null) => Promise<void>
  switchSessionAgent: (
    newAgentId: string,
    currentSession: MulticaSession | null,
    updateCurrentSession: (session: MulticaSession | null) => void,
    loadSessionModeModel: (sessionId: string) => Promise<void>
  ) => Promise<void>
  setSessionMode: (
    modeId: SessionModeId,
    currentSession: MulticaSession | null,
    setSessionModeState: React.Dispatch<React.SetStateAction<SessionModeState | null>>
  ) => Promise<void>
  setSessionModel: (
    modelId: ModelId,
    currentSession: MulticaSession | null,
    setSessionModelState: React.Dispatch<React.SetStateAction<SessionModelState | null>>
  ) => Promise<void>
}

export function useAgentStatus(): AgentStatusState & AgentStatusActions {
  const [runningSessionsStatus, setRunningSessionsStatus] = useState<RunningSessionsStatus>({
    runningSessions: 0,
    sessionIds: [],
    processingSessionIds: []
  })
  const [isSwitchingAgent, setIsSwitchingAgent] = useState(false)

  const loadRunningStatus = useCallback(async () => {
    try {
      const status = await window.electronAPI.getAgentStatus()
      setRunningSessionsStatus(status)
    } catch (err) {
      console.error('Failed to get running status:', err)
    }
  }, [])

  const sendPrompt = useCallback(
    async (
      content: MessageContent,
      currentSession: MulticaSession | null,
      setSessionUpdates: React.Dispatch<React.SetStateAction<StoredSessionUpdate[]>>,
      loadSessionModeModel: (sessionId: string) => Promise<void>
    ) => {
      if (!currentSession) {
        toast.error('No active session')
        return
      }

      try {
        // Add user message to updates
        const userUpdate = {
          timestamp: new Date().toISOString(),
          update: {
            sessionId: currentSession.agentSessionId,
            update: {
              sessionUpdate: 'user_message',
              content: content
            }
          }
        } as unknown as StoredSessionUpdate
        setSessionUpdates((prev) => [...prev, userUpdate])

        await window.electronAPI.sendPrompt(currentSession.id, content)

        // Reload mode/model state
        await loadSessionModeModel(currentSession.id)
      } catch (err) {
        const errorMessage = getErrorMessage(err)

        if (isAuthError(errorMessage)) {
          const authCommand = AGENT_AUTH_COMMANDS[currentSession.agentId] || 'Please authenticate'
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
          toast.error(`Failed to send prompt: ${errorMessage}`)
        }
      }
    },
    []
  )

  const cancelRequest = useCallback(async (currentSession: MulticaSession | null) => {
    if (!currentSession) return

    try {
      await window.electronAPI.cancelRequest(currentSession.id)
    } catch (err) {
      toast.error(`Failed to cancel: ${getErrorMessage(err)}`)
    }
  }, [])

  const switchSessionAgent = useCallback(
    async (
      newAgentId: string,
      currentSession: MulticaSession | null,
      updateCurrentSession: (session: MulticaSession | null) => void,
      loadSessionModeModel: (sessionId: string) => Promise<void>
    ) => {
      if (!currentSession) {
        toast.error('No active session')
        return
      }

      if (!useAgentStore.getState().isAgentInstalled(newAgentId)) {
        toast.error('Agent is not installed. Please install it in Settings.')
        return
      }

      try {
        setIsSwitchingAgent(true)
        const updatedSession = await window.electronAPI.switchSessionAgent(
          currentSession.id,
          newAgentId
        )
        updateCurrentSession(updatedSession)
        await loadRunningStatus()
        await loadSessionModeModel(currentSession.id)
        toast.success(`Successfully switched to ${newAgentId}`)
      } catch (err) {
        toast.error(`Failed to switch agent: ${getErrorMessage(err)}`)
      } finally {
        setIsSwitchingAgent(false)
      }
    },
    [loadRunningStatus]
  )

  const setSessionMode = useCallback(
    async (
      modeId: SessionModeId,
      currentSession: MulticaSession | null,
      setSessionModeState: React.Dispatch<React.SetStateAction<SessionModeState | null>>
    ) => {
      if (!currentSession) {
        toast.error('No active session')
        return
      }

      try {
        await window.electronAPI.setSessionMode(currentSession.id, modeId)
        setSessionModeState((prev) => (prev ? { ...prev, currentModeId: modeId } : null))
      } catch (err) {
        toast.error(`Failed to set mode: ${getErrorMessage(err)}`)
      }
    },
    []
  )

  const setSessionModel = useCallback(
    async (
      modelId: ModelId,
      currentSession: MulticaSession | null,
      setSessionModelState: React.Dispatch<React.SetStateAction<SessionModelState | null>>
    ) => {
      if (!currentSession) {
        toast.error('No active session')
        return
      }

      try {
        await window.electronAPI.setSessionModel(currentSession.id, modelId)
        setSessionModelState((prev) => (prev ? { ...prev, currentModelId: modelId } : null))
      } catch (err) {
        toast.error(`Failed to set model: ${getErrorMessage(err)}`)
      }
    },
    []
  )

  return {
    // State
    runningSessionsStatus,
    isSwitchingAgent,

    // Actions
    setRunningSessionsStatus,
    loadRunningStatus,
    sendPrompt,
    cancelRequest,
    switchSessionAgent,
    setSessionMode,
    setSessionModel
  }
}
