/**
 * App subscriptions hook
 * Handles all IPC event subscriptions for the app
 */
import { useEffect, useCallback, useRef } from 'react'
import type {
  MulticaSession,
  StoredSessionUpdate,
  ProjectWithSessions
} from '../../../shared/types'
import type { RunningSessionsStatus } from '../../../shared/electron-api'
import { usePermissionStore } from '../stores/permissionStore'
import { useFileChangeStore } from '../stores/fileChangeStore'
import { useCommandStore } from '../stores/commandStore'
import { useAgentStore } from '../stores/agentStore'
import { toast } from 'sonner'
import { mergeSessionUpdates } from '../utils/sessionUpdates'

function isGitHeadPath(filePath: string): boolean {
  const normalizedPath = filePath.replace(/\\/g, '/')
  return normalizedPath.includes('/.git/') && normalizedPath.endsWith('/HEAD')
}

export interface SubscriptionCallbacks {
  /** Ref to current session ID for synchronous access */
  currentSessionIdRef: React.MutableRefObject<string | null>
  /** Current session for file watching */
  currentSession: MulticaSession | null
  /** Update current session in state */
  updateCurrentSession: (session: MulticaSession | null) => void
  /** Update session in project lists */
  updateSessionInLists: (session: MulticaSession) => void
  /** Set session updates */
  setSessionUpdates: React.Dispatch<React.SetStateAction<StoredSessionUpdate[]>>
  /** Set running sessions status */
  setRunningSessionsStatus: React.Dispatch<React.SetStateAction<RunningSessionsStatus>>
  /** Set projects with sessions */
  setProjectsWithSessions: React.Dispatch<React.SetStateAction<ProjectWithSessions[]>>
  /** Load projects and sessions */
  loadProjectsAndSessions: () => Promise<void>
  /** Load sessions (delegates to loadProjectsAndSessions) */
  loadSessions: () => Promise<void>
  /** Validate current session directory */
  validateCurrentSessionDirectory: () => Promise<void>
}

/**
 * Hook that sets up all IPC event subscriptions
 */
export function useAppSubscriptions(callbacks: SubscriptionCallbacks): void {
  const {
    currentSessionIdRef,
    currentSession,
    updateCurrentSession,
    updateSessionInLists,
    setSessionUpdates,
    setRunningSessionsStatus,
    loadSessions
  } = callbacks

  // Debounce timer for git branch refresh
  const gitBranchRefreshTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  // Get current session ID for stable reference in effects
  const currentSessionId = currentSession?.id

  // Keep currentSessionIdRef synchronized with state
  useEffect(() => {
    currentSessionIdRef.current = currentSessionId ?? null
  }, [currentSessionId, currentSessionIdRef])

  // Git branch refresh helper
  const refreshGitBranchForSessions = useCallback(
    (sessionIds: string[]) => {
      if (sessionIds.length === 0) return

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
    [currentSessionIdRef, loadSessions, updateCurrentSession]
  )

  // Subscribe to session metadata updates
  useEffect(() => {
    const unsubSessionMeta = window.electronAPI.onSessionMetaUpdated((updatedSession) => {
      updateSessionInLists(updatedSession)

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
  }, [currentSessionId, updateCurrentSession, updateSessionInLists])

  // Start/stop file watching when current session changes
  useEffect(() => {
    const setWatchedSession = useFileChangeStore.getState().setWatchedSession

    if (currentSession && currentSession.workingDirectory) {
      setWatchedSession(currentSession.id)
      window.electronAPI
        .startFileWatch(currentSession.id, currentSession.workingDirectory)
        .catch((err) => {
          console.error('[useApp] Failed to start file watch:', err)
        })
    } else if (!currentSession) {
      setWatchedSession(null)
    }

    return () => {
      if (currentSession) {
        window.electronAPI.stopFileWatch(currentSession.id).catch((err) => {
          console.error('[useApp] Failed to stop file watch:', err)
        })
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [currentSession?.id, currentSession?.workingDirectory])

  // Subscribe to agent events (persistent subscription - only runs once on mount)
  useEffect(() => {
    const setAvailableCommands = useCommandStore.getState().setAvailableCommands

    const unsubMessage = window.electronAPI.onAgentMessage((message) => {
      const sessionId = currentSessionIdRef.current
      if (!sessionId || message.multicaSessionId !== sessionId) {
        return
      }

      const update = message.update

      if (update?.sessionUpdate === 'available_commands_update') {
        const commandsUpdate = update as { availableCommands?: unknown[] }
        if (commandsUpdate.availableCommands) {
          setAvailableCommands(
            commandsUpdate.availableCommands as Parameters<typeof setAvailableCommands>[0]
          )
        }
      }

      const frontendTimestamp = new Date().toISOString()

      setSessionUpdates((prev) => {
        const newUpdate = {
          timestamp: frontendTimestamp,
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
  }, [currentSessionIdRef, setRunningSessionsStatus, setSessionUpdates])

  // Subscribe to file system change events
  useEffect(() => {
    const handleFileChange = useFileChangeStore.getState().handleFileChange

    const unsubFileChange = window.electronAPI.onFileChanged((event) => {
      if (isGitHeadPath(event.path)) {
        void refreshGitBranchForSessions(event.sessionIds)
        return
      }

      for (const sessionId of event.sessionIds) {
        handleFileChange(sessionId)
      }
    })

    return () => {
      unsubFileChange()
    }
  }, [refreshGitBranchForSessions])

  // Subscribe to app focus event
  useEffect(() => {
    const unsubFocus = window.electronAPI.onAppFocus(async () => {
      const agentRefresh = useAgentStore.getState().loadAgents()

      if (currentSession) {
        await Promise.all([agentRefresh, callbacks.validateCurrentSessionDirectory()])

        // Re-sync session updates from DB to capture any IPC messages that were
        // lost during system sleep/wake or Chromium background throttling
        try {
          const sessionId = currentSession.id
          const freshData = await window.electronAPI.getSession(sessionId)
          if (freshData?.updates && currentSessionIdRef.current === sessionId) {
            setSessionUpdates((prev) => mergeSessionUpdates(prev, freshData.updates))
          }
        } catch (err) {
          console.error('[useAppSubscriptions] Failed to refresh session updates on focus:', err)
        }
      } else {
        await agentRefresh
      }
    })

    return () => {
      unsubFocus()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [currentSession?.id, callbacks.validateCurrentSessionDirectory])

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      if (gitBranchRefreshTimerRef.current) {
        clearTimeout(gitBranchRefreshTimerRef.current)
      }
    }
  }, [])
}
