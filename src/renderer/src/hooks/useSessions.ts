/**
 * Session management hook
 * Handles session state, selection, and lifecycle operations
 */
import { useState, useCallback, useRef } from 'react'
import type {
  MulticaSession,
  StoredSessionUpdate,
  SessionModeState,
  SessionModelState
} from '../../../shared/types'
import { useCommandStore } from '../stores/commandStore'
import { useDraftStore } from '../stores/draftStore'
import { toast } from 'sonner'
import { getErrorMessage } from '../utils/error'
import { mergeSessionUpdates } from '../utils/sessionUpdates'

export interface SessionsState {
  currentSession: MulticaSession | null
  sessionUpdates: StoredSessionUpdate[]
  isInitializing: boolean
  sessionModeState: SessionModeState | null
  sessionModelState: SessionModelState | null
}

export interface SessionsActions {
  updateCurrentSession: (session: MulticaSession | null) => void
  setSessionUpdates: React.Dispatch<React.SetStateAction<StoredSessionUpdate[]>>
  setIsInitializing: React.Dispatch<React.SetStateAction<boolean>>
  setSessionModeState: React.Dispatch<React.SetStateAction<SessionModeState | null>>
  setSessionModelState: React.Dispatch<React.SetStateAction<SessionModelState | null>>
  loadSessionModeModel: (sessionId: string) => Promise<void>
  createSession: (
    projectId: string,
    agentId: string,
    defaultModeId?: string,
    callbacks?: {
      loadProjectsAndSessions: () => Promise<void>
      loadRunningStatus: () => Promise<void>
    }
  ) => Promise<void>
  selectSession: (sessionId: string) => Promise<void>
  deleteSession: (
    sessionId: string,
    callbacks?: {
      loadProjectsAndSessions: () => Promise<void>
      loadRunningStatus: () => Promise<void>
    }
  ) => Promise<void>
  archiveSession: (
    sessionId: string,
    callbacks?: {
      loadProjectsAndSessions: () => Promise<void>
      loadRunningStatus: () => Promise<void>
    }
  ) => Promise<void>
  unarchiveSession: (
    sessionId: string,
    callbacks?: { loadProjectsAndSessions: () => Promise<void> }
  ) => Promise<void>
  clearCurrentSession: () => void
  updateSessionTitle: (
    sessionId: string,
    title: string,
    updateSessionInLists: (session: MulticaSession) => void
  ) => Promise<void>
  /** Ref to current session ID for synchronous access in callbacks */
  currentSessionIdRef: React.MutableRefObject<string | null>
  /** Ref to pending session for rapid switching protection */
  pendingSessionRef: React.MutableRefObject<string | null>
}

export function useSessions(): SessionsState & SessionsActions {
  // Track pending session selection to handle rapid switching
  const pendingSessionRef = useRef<string | null>(null)

  // Track current session ID synchronously (avoids race condition with subscription)
  const currentSessionIdRef = useRef<string | null>(null)

  // State
  const [currentSession, setCurrentSession] = useState<MulticaSession | null>(null)
  const [sessionUpdates, setSessionUpdates] = useState<StoredSessionUpdate[]>([])
  const [isInitializing, setIsInitializing] = useState(false)
  const [sessionModeState, setSessionModeState] = useState<SessionModeState | null>(null)
  const [sessionModelState, setSessionModelState] = useState<SessionModelState | null>(null)

  // Helper to update current session with synchronous ref update
  const updateCurrentSession = useCallback((session: MulticaSession | null) => {
    currentSessionIdRef.current = session?.id ?? null // Sync update FIRST
    setCurrentSession(session)
  }, [])

  // Load mode/model state for current session
  const loadSessionModeModel = useCallback(async (sessionId: string) => {
    try {
      const [modes, models, commands] = await Promise.all([
        window.electronAPI.getSessionModes(sessionId),
        window.electronAPI.getSessionModels(sessionId),
        window.electronAPI.getSessionCommands(sessionId)
      ])
      setSessionModeState(modes)
      setSessionModelState(models)
      useCommandStore.getState().setAvailableCommands(commands)
    } catch (err) {
      console.error('Failed to load session mode/model:', err)
      setSessionModeState(null)
      setSessionModelState(null)
      useCommandStore.getState().clearCommands()
    }
  }, [])

  const createSession = useCallback(
    async (
      projectId: string,
      agentId: string,
      defaultModeId?: string,
      callbacks?: {
        loadProjectsAndSessions: () => Promise<void>
        loadRunningStatus: () => Promise<void>
      }
    ) => {
      try {
        setIsInitializing(true)
        const session = await window.electronAPI.createSession(projectId, agentId)
        updateCurrentSession(session)
        setSessionUpdates([])

        // Parallel fetch if callbacks provided
        if (callbacks) {
          await Promise.all([callbacks.loadProjectsAndSessions(), callbacks.loadRunningStatus()])
        }

        // Agent starts immediately, load mode/model state
        await loadSessionModeModel(session.id)

        // Apply user's default mode if set
        if (defaultModeId) {
          await window.electronAPI.setSessionMode(session.id, defaultModeId)
          setSessionModeState((prev) => (prev ? { ...prev, currentModeId: defaultModeId } : null))
        }
      } catch (err) {
        toast.error(`Failed to create session: ${getErrorMessage(err)}`)
      } finally {
        setIsInitializing(false)
      }
    },
    [loadSessionModeModel, updateCurrentSession]
  )

  const selectSession = useCallback(
    async (sessionId: string) => {
      try {
        // Mark this as the pending session (for rapid switching protection)
        pendingSessionRef.current = sessionId

        // Load session and history in parallel
        const [session, data] = await Promise.all([
          window.electronAPI.loadSession(sessionId),
          window.electronAPI.getSession(sessionId)
        ])

        // Verify: user might have switched to another session while loading
        if (pendingSessionRef.current !== sessionId) {
          return
        }

        // Update both states together
        updateCurrentSession(session)
        setSessionUpdates(data?.updates ?? [])

        // Start agent if not already running
        const status = await window.electronAPI.getAgentStatus()
        if (!status.sessionIds.includes(sessionId)) {
          setIsInitializing(true)
          try {
            const updatedSession = await window.electronAPI.startSessionAgent(sessionId)
            if (pendingSessionRef.current !== sessionId) return
            updateCurrentSession(updatedSession)
          } finally {
            setIsInitializing(false)
          }
        }

        // Agent now guaranteed running, load mode/model
        await loadSessionModeModel(sessionId)

        // Reload session updates to catch any messages that arrived during async operations
        if (pendingSessionRef.current === sessionId) {
          const freshData = await window.electronAPI.getSession(sessionId)
          if (pendingSessionRef.current === sessionId && freshData?.updates) {
            setSessionUpdates((prev) => mergeSessionUpdates(prev, freshData.updates))
          }
        }
      } catch (err) {
        if (pendingSessionRef.current === sessionId) {
          toast.error(`Failed to select session: ${getErrorMessage(err)}`)
        }
      }
    },
    [loadSessionModeModel, updateCurrentSession]
  )

  const deleteSession = useCallback(
    async (
      sessionId: string,
      callbacks?: {
        loadProjectsAndSessions: () => Promise<void>
        loadRunningStatus: () => Promise<void>
      }
    ) => {
      try {
        await window.electronAPI.deleteSession(sessionId)
        useDraftStore.getState().clearDraft(sessionId)
        if (currentSessionIdRef.current === sessionId) {
          updateCurrentSession(null)
          setSessionUpdates([])
        }
        if (callbacks) {
          await callbacks.loadProjectsAndSessions()
          await callbacks.loadRunningStatus()
        }
      } catch (err) {
        toast.error(`Failed to delete session: ${getErrorMessage(err)}`)
      }
    },
    [updateCurrentSession]
  )

  const archiveSession = useCallback(
    async (
      sessionId: string,
      callbacks?: {
        loadProjectsAndSessions: () => Promise<void>
        loadRunningStatus: () => Promise<void>
      }
    ) => {
      try {
        await window.electronAPI.archiveSession(sessionId)
        useDraftStore.getState().clearDraft(sessionId)
        if (currentSessionIdRef.current === sessionId) {
          updateCurrentSession(null)
          setSessionUpdates([])
        }
        if (callbacks) {
          await callbacks.loadProjectsAndSessions()
          await callbacks.loadRunningStatus()
        }
        toast.success('Session archived')
      } catch (err) {
        toast.error(`Failed to archive session: ${getErrorMessage(err)}`)
      }
    },
    [updateCurrentSession]
  )

  const unarchiveSession = useCallback(
    async (sessionId: string, callbacks?: { loadProjectsAndSessions: () => Promise<void> }) => {
      try {
        await window.electronAPI.unarchiveSession(sessionId)
        if (callbacks) {
          await callbacks.loadProjectsAndSessions()
        }
        toast.success('Session restored')
      } catch (err) {
        toast.error(`Failed to restore session: ${getErrorMessage(err)}`)
      }
    },
    []
  )

  const clearCurrentSession = useCallback(() => {
    updateCurrentSession(null)
    setSessionUpdates([])
    setSessionModeState(null)
    setSessionModelState(null)
    useCommandStore.getState().clearCommands()
  }, [updateCurrentSession])

  const updateSessionTitle = useCallback(
    async (
      sessionId: string,
      title: string,
      updateSessionInLists: (session: MulticaSession) => void
    ) => {
      try {
        const updatedSession = await window.electronAPI.updateSession(sessionId, { title })
        updateSessionInLists(updatedSession)
        if (currentSessionIdRef.current === sessionId) {
          updateCurrentSession(updatedSession)
        }
      } catch (err) {
        toast.error(`Failed to update title: ${getErrorMessage(err)}`)
      }
    },
    [updateCurrentSession]
  )

  return {
    // State
    currentSession,
    sessionUpdates,
    isInitializing,
    sessionModeState,
    sessionModelState,

    // Actions
    updateCurrentSession,
    setSessionUpdates,
    setIsInitializing,
    setSessionModeState,
    setSessionModelState,
    loadSessionModeModel,
    createSession,
    selectSession,
    deleteSession,
    archiveSession,
    unarchiveSession,
    clearCurrentSession,
    updateSessionTitle,

    // Refs
    currentSessionIdRef,
    pendingSessionRef
  }
}
