/**
 * Main application state hook
 * Composition layer that combines focused hooks for projects, sessions, and agent status
 */
import { useEffect, useCallback, useRef, useMemo } from 'react'
import type {
  MulticaSession,
  MulticaProject,
  StoredSessionUpdate,
  SessionModeState,
  SessionModelState,
  SessionModeId,
  ModelId
} from '../../../shared/types'
import type { RunningSessionsStatus } from '../../../shared/electron-api'
import type { MessageContent } from '../../../shared/types/message'
import { useProjects } from './useProjects'
import { useSessions } from './useSessions'
import { useAgentStatus } from './useAgentStatus'
import { useAppSubscriptions } from './useAppSubscriptions'

export interface AppState {
  // Projects
  projects: MulticaProject[]
  sessionsByProject: Map<string, MulticaSession[]>

  // Sessions
  sessions: MulticaSession[]
  currentSession: MulticaSession | null
  sessionUpdates: StoredSessionUpdate[]

  // Agent (per-session)
  runningSessionsStatus: RunningSessionsStatus
  isProcessing: boolean
  isInitializing: boolean

  // Mode/Model (from ACP server)
  sessionModeState: SessionModeState | null
  sessionModelState: SessionModelState | null

  // UI
  isSwitchingAgent: boolean
}

export interface AppActions {
  // Project actions
  loadProjectsAndSessions: () => Promise<void>
  createProject: (workingDirectory: string) => Promise<MulticaProject | null>
  toggleProjectExpanded: (projectId: string) => Promise<void>
  reorderProjects: (projectIds: string[]) => Promise<void>
  deleteProject: (projectId: string) => Promise<void>

  // Session actions
  loadSessions: () => Promise<void>
  createSession: (projectId: string, agentId: string, defaultModeId?: string) => Promise<void>
  selectSession: (sessionId: string) => Promise<void>
  deleteSession: (sessionId: string) => Promise<void>
  archiveSession: (sessionId: string) => Promise<void>
  unarchiveSession: (sessionId: string) => Promise<void>
  clearCurrentSession: () => void

  // Agent actions (per-session)
  sendPrompt: (content: MessageContent) => Promise<void>
  cancelRequest: () => Promise<void>
  switchSessionAgent: (newAgentId: string) => Promise<void>

  // Mode/Model actions
  setSessionMode: (modeId: SessionModeId) => Promise<void>
  setSessionModel: (modelId: ModelId) => Promise<void>

  // Session metadata actions
  updateSessionTitle: (sessionId: string, title: string) => Promise<void>
}

export function useApp(): AppState & AppActions {
  // Track previous isProcessing state to detect when processing completes
  const prevIsProcessingRef = useRef(false)

  // Use focused hooks
  const sessions = useSessions()
  const projects = useProjects({
    currentSessionProjectId: sessions.currentSession?.projectId
  })
  const agentStatus = useAgentStatus()

  // Derive flat sessions list from projectsWithSessions
  const flatSessions = useMemo(
    () => projects.projectsWithSessions.flatMap((p) => p.sessions),
    [projects.projectsWithSessions]
  )

  // Derive isProcessing from processingSessionIds
  const isProcessing = sessions.currentSession
    ? agentStatus.runningSessionsStatus.processingSessionIds.includes(sessions.currentSession.id)
    : false

  // Helper to update session in lists
  const updateSessionInLists = useCallback(
    (updatedSession: MulticaSession) => {
      projects.setProjectsWithSessions((prev) => {
        let didUpdate = false
        const next = prev.map((entry) => {
          if (entry.project.id !== updatedSession.projectId) {
            return entry
          }
          const nextSessions = entry.sessions.map((session) => {
            if (session.id !== updatedSession.id) {
              return session
            }
            didUpdate = true
            return updatedSession
          })
          return didUpdate ? { ...entry, sessions: nextSessions } : entry
        })
        return didUpdate ? next : prev
      })
    },
    [projects]
  )

  // Validate current session directory exists
  const validateCurrentSessionDirectory = useCallback(async () => {
    if (!sessions.currentSession) return

    try {
      const session = await window.electronAPI.loadSession(sessions.currentSession.id)
      if (session.directoryExists !== sessions.currentSession.directoryExists) {
        sessions.updateCurrentSession(session)
        await projects.loadProjectsAndSessions()
      }
    } catch (err) {
      console.error('Failed to validate session directory:', err)
    }
  }, [sessions, projects])

  // Load on mount
  useEffect(() => {
    projects.loadProjectsAndSessions()
    agentStatus.loadRunningStatus()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // Set up IPC subscriptions
  useAppSubscriptions({
    currentSessionIdRef: sessions.currentSessionIdRef,
    currentSession: sessions.currentSession,
    updateCurrentSession: sessions.updateCurrentSession,
    updateSessionInLists,
    setSessionUpdates: sessions.setSessionUpdates,
    setRunningSessionsStatus: agentStatus.setRunningSessionsStatus,
    setProjectsWithSessions: projects.setProjectsWithSessions,
    loadProjectsAndSessions: projects.loadProjectsAndSessions,
    loadSessions: projects.loadProjectsAndSessions,
    validateCurrentSessionDirectory
  })

  // Reset processing ref when session changes
  const currentSessionId = sessions.currentSession?.id
  useEffect(() => {
    prevIsProcessingRef.current = false
  }, [currentSessionId])

  // Refresh git branch when agent processing completes
  useEffect(() => {
    const wasProcessing = prevIsProcessingRef.current
    prevIsProcessingRef.current = isProcessing

    if (wasProcessing && !isProcessing && currentSessionId) {
      // Trigger refresh via loadSessions
      void projects.loadProjectsAndSessions()
    }
  }, [isProcessing, currentSessionId, projects])

  // Composed actions that wire hooks together
  const deleteProject = useCallback(
    async (projectId: string) => {
      await projects.deleteProject(projectId, () => {
        sessions.updateCurrentSession(null)
        sessions.setSessionUpdates([])
      })
      await agentStatus.loadRunningStatus()
    },
    [projects, sessions, agentStatus]
  )

  const createSession = useCallback(
    async (projectId: string, agentId: string, defaultModeId?: string) => {
      await sessions.createSession(projectId, agentId, defaultModeId, {
        loadProjectsAndSessions: projects.loadProjectsAndSessions,
        loadRunningStatus: agentStatus.loadRunningStatus
      })
    },
    [sessions, projects, agentStatus]
  )

  const deleteSession = useCallback(
    async (sessionId: string) => {
      await sessions.deleteSession(sessionId, {
        loadProjectsAndSessions: projects.loadProjectsAndSessions,
        loadRunningStatus: agentStatus.loadRunningStatus
      })
    },
    [sessions, projects, agentStatus]
  )

  const archiveSession = useCallback(
    async (sessionId: string) => {
      await sessions.archiveSession(sessionId, {
        loadProjectsAndSessions: projects.loadProjectsAndSessions,
        loadRunningStatus: agentStatus.loadRunningStatus
      })
    },
    [sessions, projects, agentStatus]
  )

  const unarchiveSession = useCallback(
    async (sessionId: string) => {
      await sessions.unarchiveSession(sessionId, {
        loadProjectsAndSessions: projects.loadProjectsAndSessions
      })
    },
    [sessions, projects]
  )

  const sendPrompt = useCallback(
    async (content: MessageContent) => {
      await agentStatus.sendPrompt(
        content,
        sessions.currentSession,
        sessions.setSessionUpdates,
        sessions.loadSessionModeModel
      )
    },
    [agentStatus, sessions]
  )

  const cancelRequest = useCallback(async () => {
    await agentStatus.cancelRequest(sessions.currentSession)
  }, [agentStatus, sessions])

  const switchSessionAgent = useCallback(
    async (newAgentId: string) => {
      await agentStatus.switchSessionAgent(
        newAgentId,
        sessions.currentSession,
        sessions.updateCurrentSession,
        sessions.loadSessionModeModel
      )
    },
    [agentStatus, sessions]
  )

  const setSessionMode = useCallback(
    async (modeId: SessionModeId) => {
      await agentStatus.setSessionMode(
        modeId,
        sessions.currentSession,
        sessions.setSessionModeState
      )
    },
    [agentStatus, sessions]
  )

  const setSessionModel = useCallback(
    async (modelId: ModelId) => {
      await agentStatus.setSessionModel(
        modelId,
        sessions.currentSession,
        sessions.setSessionModelState
      )
    },
    [agentStatus, sessions]
  )

  const updateSessionTitle = useCallback(
    async (sessionId: string, title: string) => {
      await sessions.updateSessionTitle(sessionId, title, updateSessionInLists)
    },
    [sessions, updateSessionInLists]
  )

  return {
    // State
    projects: projects.projects,
    sessionsByProject: projects.sessionsByProject,
    sessions: flatSessions,
    currentSession: sessions.currentSession,
    sessionUpdates: sessions.sessionUpdates,
    runningSessionsStatus: agentStatus.runningSessionsStatus,
    isProcessing,
    isInitializing: sessions.isInitializing,
    sessionModeState: sessions.sessionModeState,
    sessionModelState: sessions.sessionModelState,
    isSwitchingAgent: agentStatus.isSwitchingAgent,

    // Actions
    loadProjectsAndSessions: projects.loadProjectsAndSessions,
    createProject: projects.createProject,
    toggleProjectExpanded: projects.toggleProjectExpanded,
    reorderProjects: projects.reorderProjects,
    deleteProject,
    loadSessions: projects.loadProjectsAndSessions,
    createSession,
    selectSession: sessions.selectSession,
    deleteSession,
    archiveSession,
    unarchiveSession,
    clearCurrentSession: sessions.clearCurrentSession,
    sendPrompt,
    cancelRequest,
    switchSessionAgent,
    setSessionMode,
    setSessionModel,
    updateSessionTitle
  }
}
