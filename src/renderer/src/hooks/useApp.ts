/**
 * Main application state hook
 */
import { useState, useEffect, useCallback, useRef, useMemo } from 'react'
import type {
  MulticaSession,
  MulticaProject,
  ProjectWithSessions,
  StoredSessionUpdate,
  SessionModeState,
  SessionModelState,
  SessionModeId,
  ModelId
} from '../../../shared/types'
import type { RunningSessionsStatus } from '../../../shared/electron-api'
import type { MessageContent } from '../../../shared/types/message'
import { usePermissionStore } from '../stores/permissionStore'
import { useFileChangeStore } from '../stores/fileChangeStore'
import { useCommandStore } from '../stores/commandStore'
import { useDraftStore } from '../stores/draftStore'
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

function isGitHeadPath(filePath: string): boolean {
  const normalizedPath = filePath.replace(/\\/g, '/')
  return normalizedPath.includes('/.git/') && normalizedPath.endsWith('/HEAD')
}

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
  createSession: (projectId: string, agentId: string) => Promise<void>
  selectSession: (sessionId: string) => Promise<void>
  deleteSession: (sessionId: string) => Promise<void>
  clearCurrentSession: () => void

  // Agent actions (per-session)
  sendPrompt: (content: MessageContent) => Promise<void>
  cancelRequest: () => Promise<void>
  switchSessionAgent: (newAgentId: string) => Promise<void>

  // Mode/Model actions
  setSessionMode: (modeId: SessionModeId) => Promise<void>
  setSessionModel: (modelId: ModelId) => Promise<void>
}

function mergeSessionUpdates(
  existing: StoredSessionUpdate[],
  incoming: StoredSessionUpdate[]
): StoredSessionUpdate[] {
  if (incoming.length === 0) {
    return existing
  }

  const merged: Array<StoredSessionUpdate | null> = []
  const keyToIndex = new Map<string, number>()
  const payloadToKey = new Map<string, string>()

  const buildPayloadKey = (item: StoredSessionUpdate): string => {
    try {
      return JSON.stringify(item.update)
    } catch {
      return `unstringifiable:${item.timestamp}`
    }
  }

  const addItem = (item: StoredSessionUpdate): void => {
    const payloadKey = buildPayloadKey(item)
    if (item.sequenceNumber !== undefined) {
      const seqKey = `seq:${item.sequenceNumber}`
      const existingPayloadKey = payloadToKey.get(payloadKey)
      if (existingPayloadKey?.startsWith('payload:')) {
        const existingIndex = keyToIndex.get(existingPayloadKey)
        if (existingIndex !== undefined) {
          merged[existingIndex] = null
        }
        keyToIndex.delete(existingPayloadKey)
      }

      const existingIndex = keyToIndex.get(seqKey)
      if (existingIndex !== undefined) {
        merged[existingIndex] = item
      } else {
        merged.push(item)
        keyToIndex.set(seqKey, merged.length - 1)
      }
      payloadToKey.set(payloadKey, seqKey)
      return
    }

    const payloadKeyWithPrefix = `payload:${payloadKey}`
    const existingPayloadKey = payloadToKey.get(payloadKey)
    if (existingPayloadKey?.startsWith('seq:')) {
      return
    }

    const existingIndex = keyToIndex.get(payloadKeyWithPrefix)
    if (existingIndex !== undefined) {
      merged[existingIndex] = item
    } else {
      merged.push(item)
      keyToIndex.set(payloadKeyWithPrefix, merged.length - 1)
    }
    payloadToKey.set(payloadKey, payloadKeyWithPrefix)
  }

  for (const item of existing) {
    addItem(item)
  }
  for (const item of incoming) {
    addItem(item)
  }

  return merged.filter((item): item is StoredSessionUpdate => item !== null)
}

export function useApp(): AppState & AppActions {
  // Track pending session selection to handle rapid switching
  const pendingSessionRef = useRef<string | null>(null)

  // Track current session ID synchronously (avoids race condition with subscription)
  // This ref is updated synchronously when state changes, ensuring the subscription
  // callback always has access to the latest session ID without needing to re-subscribe
  const currentSessionIdRef = useRef<string | null>(null)

  // State
  const [projectsWithSessions, setProjectsWithSessions] = useState<ProjectWithSessions[]>([])
  const [sessions, setSessions] = useState<MulticaSession[]>([])
  const [currentSession, setCurrentSession] = useState<MulticaSession | null>(null)
  const [sessionUpdates, setSessionUpdates] = useState<StoredSessionUpdate[]>([])

  // Derive projects and sessionsByProject from projectsWithSessions
  const projects = useMemo(() => projectsWithSessions.map((p) => p.project), [projectsWithSessions])
  const sessionsByProject = useMemo(() => {
    const map = new Map<string, MulticaSession[]>()
    for (const { project, sessions } of projectsWithSessions) {
      map.set(project.id, sessions)
    }
    return map
  }, [projectsWithSessions])

  // Helper to update current session with synchronous ref update
  // This ensures currentSessionIdRef is always up-to-date before any async operations
  // Critical for avoiding race conditions where messages arrive before useEffect updates the ref
  const updateCurrentSession = useCallback((session: MulticaSession | null) => {
    currentSessionIdRef.current = session?.id ?? null // Sync update FIRST
    setCurrentSession(session)
  }, [])
  const [runningSessionsStatus, setRunningSessionsStatus] = useState<RunningSessionsStatus>({
    runningSessions: 0,
    sessionIds: [],
    processingSessionIds: []
  })
  const [isInitializing, setIsInitializing] = useState(false)
  const [isSwitchingAgent, setIsSwitchingAgent] = useState(false)
  const [sessionModeState, setSessionModeState] = useState<SessionModeState | null>(null)
  const [sessionModelState, setSessionModelState] = useState<SessionModelState | null>(null)

  // Derive isProcessing from processingSessionIds (per-session isolation)
  const isProcessing = currentSession
    ? runningSessionsStatus.processingSessionIds.includes(currentSession.id)
    : false

  // Track previous isProcessing state to detect when processing completes
  const prevIsProcessingRef = useRef(false)

  // Debounce timer for git branch refresh
  const gitBranchRefreshTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  // Note: File tree refresh is handled by file system watcher (see fs:file-changed handler below)
  // No need for periodic refresh - it causes performance issues

  // Load projects and sessions on mount
  useEffect(() => {
    loadProjectsAndSessions()
    loadRunningStatus()

    return () => {
      if (gitBranchRefreshTimerRef.current) {
        clearTimeout(gitBranchRefreshTimerRef.current)
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- Intentional: only load on mount
  }, [])

  // Get current session ID for stable reference in effect
  const currentSessionId = currentSession?.id

  // Keep currentSessionIdRef synchronized with state
  // This runs synchronously during render, ensuring the ref is always up-to-date
  // before any event handlers can fire
  useEffect(() => {
    currentSessionIdRef.current = currentSessionId ?? null
  }, [currentSessionId])

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
        updateCurrentSession(updatedSession)
      }
    })

    return () => {
      unsubSessionMeta()
    }
  }, [currentSessionId, updateCurrentSession])

  // Start/stop file watching when current session changes
  useEffect(() => {
    const setWatchedSession = useFileChangeStore.getState().setWatchedSession

    if (currentSession && currentSession.workingDirectory) {
      // Start watching the session's working directory
      setWatchedSession(currentSession.id)
      window.electronAPI
        .startFileWatch(currentSession.id, currentSession.workingDirectory)
        .catch((err) => {
          console.error('[useApp] Failed to start file watch:', err)
        })
    } else if (!currentSession) {
      setWatchedSession(null)
    }

    // Cleanup: stop watching when session changes or component unmounts
    return () => {
      if (currentSession) {
        window.electronAPI.stopFileWatch(currentSession.id).catch((err) => {
          console.error('[useApp] Failed to stop file watch:', err)
        })
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- Intentional: only re-run when id or workingDirectory changes
  }, [currentSession?.id, currentSession?.workingDirectory])

  // Subscribe to agent events (persistent subscription - only runs once on mount)
  // Uses refs to access current session ID, avoiding race condition where subscription
  // is being recreated while events are arriving
  useEffect(() => {
    // Get setAvailableCommands from store for command updates
    const setAvailableCommands = useCommandStore.getState().setAvailableCommands

    const unsubMessage = window.electronAPI.onAgentMessage((message) => {
      // Only process messages for the current session
      // Use ref (currentSessionIdRef) to get the latest session ID synchronously
      // This avoids race condition where useEffect hasn't re-subscribed after session change
      const sessionId = currentSessionIdRef.current
      if (!sessionId || message.multicaSessionId !== sessionId) {
        return
      }

      const update = message.update

      // Handle available_commands_update event: update slash commands store
      if (update?.sessionUpdate === 'available_commands_update') {
        const commandsUpdate = update as { availableCommands?: unknown[] }
        if (commandsUpdate.availableCommands) {
          setAvailableCommands(
            commandsUpdate.availableCommands as Parameters<typeof setAvailableCommands>[0]
          )
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
    }
  }, [])

  // Actions
  const loadProjectsAndSessions = useCallback(async () => {
    try {
      const list = await window.electronAPI.listProjectsWithSessions()
      setProjectsWithSessions(list)
      // Also update flat sessions list for backward compatibility
      const allSessions = list.flatMap((p) => p.sessions)
      setSessions(allSessions)
    } catch (err) {
      toast.error(`Failed to load projects: ${getErrorMessage(err)}`)
    }
  }, [])

  const loadSessions = useCallback(async () => {
    // Delegate to loadProjectsAndSessions to keep both in sync
    await loadProjectsAndSessions()
  }, [loadProjectsAndSessions])

  const loadRunningStatus = useCallback(async () => {
    try {
      const status = await window.electronAPI.getAgentStatus()
      setRunningSessionsStatus(status)
    } catch (err) {
      console.error('Failed to get running status:', err)
    }
  }, [])

  const refreshGitBranchForSessions = useCallback(
    (sessionIds: string[]) => {
      if (sessionIds.length === 0) return

      // Cancel any pending refresh to debounce rapid changes
      if (gitBranchRefreshTimerRef.current) {
        clearTimeout(gitBranchRefreshTimerRef.current)
      }

      gitBranchRefreshTimerRef.current = setTimeout(async () => {
        gitBranchRefreshTimerRef.current = null

        const activeSessionId = currentSessionIdRef.current
        const shouldRefreshCurrent =
          activeSessionId !== null && sessionIds.includes(activeSessionId)

        if (shouldRefreshCurrent) {
          try {
            const session = await window.electronAPI.loadSession(activeSessionId)
            if (currentSessionIdRef.current === activeSessionId) {
              updateCurrentSession(session)
            }
          } catch (err) {
            console.error('[useApp] Failed to refresh git branch:', err)
          }
        }

        await loadSessions()
      }, 100)
    },
    [loadSessions, updateCurrentSession]
  )

  // Load mode/model state for current session (if agent supports it)
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
      // Reset to null on error (agent may not support modes/models)
      setSessionModeState(null)
      setSessionModelState(null)
      useCommandStore.getState().clearCommands()
    }
  }, [])

  // Subscribe to file system change events (runs once on mount)
  useEffect(() => {
    const handleFileChange = useFileChangeStore.getState().handleFileChange

    const unsubFileChange = window.electronAPI.onFileChanged((event) => {
      if (isGitHeadPath(event.path)) {
        void refreshGitBranchForSessions(event.sessionIds)
        return
      }

      // Notify the store of file changes for each affected session
      for (const sessionId of event.sessionIds) {
        handleFileChange(sessionId)
      }
    })

    return () => {
      unsubFileChange()
    }
  }, [refreshGitBranchForSessions])

  // Validate current session directory exists (called on app focus)
  const validateCurrentSessionDirectory = useCallback(async () => {
    if (!currentSession) return

    try {
      // Reload session to get latest directoryExists state from backend
      const session = await window.electronAPI.loadSession(currentSession.id)

      // Only update if state changed to avoid unnecessary re-renders
      if (session.directoryExists !== currentSession.directoryExists) {
        updateCurrentSession(session)
        // Also refresh sidebar list to update directory status indicators
        await loadProjectsAndSessions()
      }
    } catch (err) {
      console.error('Failed to validate session directory:', err)
    }
  }, [currentSession, loadProjectsAndSessions, updateCurrentSession])

  // Subscribe to app focus event to validate directory existence
  useEffect(() => {
    const unsubFocus = window.electronAPI.onAppFocus(async () => {
      // Only validate if we have a current session
      if (currentSession) {
        await validateCurrentSessionDirectory()
      }
    })

    return () => {
      unsubFocus()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- Intentional: only re-subscribe on session ID change
  }, [currentSession?.id, validateCurrentSessionDirectory])

  useEffect(() => {
    prevIsProcessingRef.current = false
  }, [currentSessionId])

  // Refresh git branch when agent processing completes
  // This ensures the UI reflects any branch changes made by the agent (e.g., git checkout)
  useEffect(() => {
    const wasProcessing = prevIsProcessingRef.current
    prevIsProcessingRef.current = isProcessing

    // Only refresh when processing transitions from true to false
    if (wasProcessing && !isProcessing && currentSessionId) {
      void refreshGitBranchForSessions([currentSessionId])
    }
  }, [isProcessing, currentSessionId, refreshGitBranchForSessions])

  const createProject = useCallback(
    async (workingDirectory: string): Promise<MulticaProject | null> => {
      try {
        const project = await window.electronAPI.createProject(workingDirectory)
        await loadProjectsAndSessions()
        return project
      } catch (err) {
        toast.error(`Failed to create project: ${getErrorMessage(err)}`)
        return null
      }
    },
    [loadProjectsAndSessions]
  )

  const toggleProjectExpanded = useCallback(
    async (projectId: string) => {
      try {
        await window.electronAPI.toggleProjectExpanded(projectId)
        await loadProjectsAndSessions()
      } catch (err) {
        toast.error(`Failed to toggle project: ${getErrorMessage(err)}`)
      }
    },
    [loadProjectsAndSessions]
  )

  const reorderProjects = useCallback(
    async (projectIds: string[]) => {
      try {
        // Optimistic update: immediately reorder the local state
        setProjectsWithSessions((prev) => {
          const projectMap = new Map(prev.map((p) => [p.project.id, p]))
          return projectIds.map((id) => projectMap.get(id)!).filter(Boolean)
        })

        // Persist to backend
        await window.electronAPI.reorderProjects(projectIds)
      } catch (err) {
        toast.error(`Failed to reorder projects: ${getErrorMessage(err)}`)
        // Reload to restore correct order on error
        await loadProjectsAndSessions()
      }
    },
    [loadProjectsAndSessions]
  )

  const deleteProject = useCallback(
    async (projectId: string) => {
      try {
        // Check if current session belongs to this project
        if (currentSession?.projectId === projectId) {
          updateCurrentSession(null)
          setSessionUpdates([])
        }
        await window.electronAPI.deleteProject(projectId)
        await loadProjectsAndSessions()
        await loadRunningStatus()
      } catch (err) {
        toast.error(`Failed to delete project: ${getErrorMessage(err)}`)
      }
    },
    [currentSession?.projectId, loadProjectsAndSessions, loadRunningStatus, updateCurrentSession]
  )

  const createSession = useCallback(
    async (projectId: string, agentId: string) => {
      try {
        setIsInitializing(true)
        const session = await window.electronAPI.createSession(projectId, agentId)
        updateCurrentSession(session)
        setSessionUpdates([])
        await loadProjectsAndSessions()
        await loadRunningStatus()
        // Agent starts immediately now, load mode/model state
        await loadSessionModeModel(session.id)
      } catch (err) {
        toast.error(`Failed to create session: ${getErrorMessage(err)}`)
      } finally {
        setIsInitializing(false)
      }
    },
    [loadProjectsAndSessions, loadRunningStatus, loadSessionModeModel, updateCurrentSession]
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
          return // Discard, user already switched elsewhere
        }

        // Update both states together - React will batch them into one render
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
        // This is critical because messages may have arrived via IPC while we were:
        // 1. Loading initial history
        // 2. Starting the agent
        // 3. Loading mode/model state
        // Without this reload, those messages would be lost (overwritten by initial setSessionUpdates)
        if (pendingSessionRef.current === sessionId) {
          const freshData = await window.electronAPI.getSession(sessionId)
          if (pendingSessionRef.current === sessionId && freshData?.updates) {
            setSessionUpdates((prev) => mergeSessionUpdates(prev, freshData.updates))
          }
        }
      } catch (err) {
        // Only show error if this is still the pending session
        if (pendingSessionRef.current === sessionId) {
          toast.error(`Failed to select session: ${getErrorMessage(err)}`)
        }
      }
    },
    [loadSessionModeModel, updateCurrentSession]
  )

  const deleteSession = useCallback(
    async (sessionId: string) => {
      try {
        await window.electronAPI.deleteSession(sessionId)
        useDraftStore.getState().clearDraft(sessionId)
        if (currentSession?.id === sessionId) {
          updateCurrentSession(null)
          setSessionUpdates([])
        }
        await loadProjectsAndSessions()
        await loadRunningStatus()
      } catch (err) {
        toast.error(`Failed to delete session: ${getErrorMessage(err)}`)
      }
    },
    [currentSession, loadProjectsAndSessions, loadRunningStatus, updateCurrentSession]
  )

  const clearCurrentSession = useCallback(() => {
    updateCurrentSession(null)
    setSessionUpdates([])
    setSessionModeState(null)
    setSessionModelState(null)
    useCommandStore.getState().clearCommands()
  }, [updateCurrentSession])

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

        // After successful prompt, agent is guaranteed to be running
        // Reload mode/model state (important for lazy-started sessions)
        await loadSessionModeModel(currentSession.id)
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
    [currentSession, loadSessionModeModel]
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
        updateCurrentSession(updatedSession)
        await loadRunningStatus()
        // Reload mode/model state for the new agent
        await loadSessionModeModel(currentSession.id)
        toast.success(`Successfully switched to ${newAgentId}`)
      } catch (err) {
        toast.error(`Failed to switch agent: ${getErrorMessage(err)}`)
      } finally {
        setIsSwitchingAgent(false)
      }
    },
    [currentSession, loadRunningStatus, loadSessionModeModel, updateCurrentSession]
  )

  // Mode/Model actions
  const setSessionMode = useCallback(
    async (modeId: SessionModeId) => {
      if (!currentSession) {
        toast.error('No active session')
        return
      }

      try {
        await window.electronAPI.setSessionMode(currentSession.id, modeId)
        // Optimistic update
        setSessionModeState((prev) => (prev ? { ...prev, currentModeId: modeId } : null))
      } catch (err) {
        toast.error(`Failed to set mode: ${getErrorMessage(err)}`)
      }
    },
    [currentSession]
  )

  const setSessionModel = useCallback(
    async (modelId: ModelId) => {
      if (!currentSession) {
        toast.error('No active session')
        return
      }

      try {
        await window.electronAPI.setSessionModel(currentSession.id, modelId)
        // Optimistic update
        setSessionModelState((prev) => (prev ? { ...prev, currentModelId: modelId } : null))
      } catch (err) {
        toast.error(`Failed to set model: ${getErrorMessage(err)}`)
      }
    },
    [currentSession]
  )

  return {
    // State
    projects,
    sessionsByProject,
    sessions,
    currentSession,
    sessionUpdates,
    runningSessionsStatus,
    isProcessing,
    isInitializing,
    sessionModeState,
    sessionModelState,
    isSwitchingAgent,

    // Actions
    loadProjectsAndSessions,
    createProject,
    toggleProjectExpanded,
    reorderProjects,
    deleteProject,
    loadSessions,
    createSession,
    selectSession,
    deleteSession,
    clearCurrentSession,
    sendPrompt,
    cancelRequest,
    switchSessionAgent,
    setSessionMode,
    setSessionModel
  }
}
