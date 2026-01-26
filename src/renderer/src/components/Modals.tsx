/**
 * Global modals registry
 * All app modals are rendered here and controlled via modalStore
 */
import { useState, useEffect, useCallback } from 'react'
import { useModalStore, useModal } from '../stores/modalStore'
import { Settings } from './Settings'
import type { MulticaSession, MulticaProject } from '../../../shared/types'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Undo2 } from 'lucide-react'

interface ModalsProps {
  // Settings props
  defaultAgentId: string
  onSetDefaultAgent: (agentId: string) => void
  defaultModes: Record<string, string>
  onSetDefaultMode: (agentId: string, modeId: string) => void
  // NewSession props
  onCreateSession: (cwd: string) => Promise<void>
  // DeleteSession props
  onDeleteSession: (sessionId: string) => void
  // ArchiveSession props
  onArchiveSession: (sessionId: string) => Promise<void>
  // UnarchiveSession props
  onUnarchiveSession: (sessionId: string) => Promise<void>
  // DeleteProject props
  onDeleteProject: (projectId: string) => Promise<void>
}

export function Modals({
  defaultAgentId,
  onSetDefaultAgent,
  defaultModes,
  onSetDefaultMode,
  onCreateSession,
  onDeleteSession,
  onArchiveSession,
  onUnarchiveSession,
  onDeleteProject
}: ModalsProps): React.JSX.Element {
  const closeModal = useModalStore((s) => s.closeModal)

  return (
    <>
      <SettingsModal
        defaultAgentId={defaultAgentId}
        onSetDefaultAgent={onSetDefaultAgent}
        defaultModes={defaultModes}
        onSetDefaultMode={onSetDefaultMode}
        onCreateSession={onCreateSession}
        onClose={() => closeModal('settings')}
      />
      <NewSessionModal onCreateSession={onCreateSession} onClose={() => closeModal('newSession')} />
      <ArchiveSessionModal
        onArchiveSession={onArchiveSession}
        onClose={() => closeModal('archiveSession')}
      />
      <DeleteSessionModal
        onDeleteSession={onDeleteSession}
        onClose={() => closeModal('deleteSession')}
      />
      <DeleteProjectModal
        onDeleteProject={onDeleteProject}
        onClose={() => closeModal('deleteProject')}
      />
      <ArchivedSessionsModal
        onUnarchiveSession={onUnarchiveSession}
        onClose={() => closeModal('archivedSessions')}
      />
    </>
  )
}

// Settings Modal
interface SettingsModalProps {
  defaultAgentId: string
  onSetDefaultAgent: (agentId: string) => void
  defaultModes: Record<string, string>
  onSetDefaultMode: (agentId: string, modeId: string) => void
  onCreateSession: (cwd: string) => Promise<void>
  onClose: () => void
}

function SettingsModal({
  defaultAgentId,
  onSetDefaultAgent,
  defaultModes,
  onSetDefaultMode,
  onCreateSession,
  onClose
}: SettingsModalProps): React.JSX.Element {
  const { isOpen, data } = useModal('settings')

  const handleClose = async (): Promise<void> => {
    const pendingFolder = data?.pendingFolder
    onClose()

    // If there's a pending folder, check if agent is now installed and create session
    if (pendingFolder) {
      const agentCheck = await window.electronAPI.checkAgent(defaultAgentId)
      if (agentCheck?.installed) {
        await onCreateSession(pendingFolder)
      }
    }
  }

  return (
    <Settings
      isOpen={isOpen}
      onClose={handleClose}
      defaultAgentId={defaultAgentId}
      onSetDefaultAgent={onSetDefaultAgent}
      defaultModes={defaultModes}
      onSetDefaultMode={onSetDefaultMode}
      highlightAgent={data?.highlightAgent}
    />
  )
}

// New Session Modal
interface NewSessionModalProps {
  onCreateSession: (cwd: string) => Promise<void>
  onClose: () => void
}

function NewSessionModal({ onCreateSession, onClose }: NewSessionModalProps): React.JSX.Element {
  const { isOpen } = useModal('newSession')
  const [cwd, setCwd] = useState('')

  const handleCreate = async (): Promise<void> => {
    if (!cwd.trim()) return
    await onCreateSession(cwd.trim())
    setCwd('')
    onClose()
  }

  const handleOpenChange = (open: boolean): void => {
    if (!open) {
      setCwd('')
      onClose()
    }
  }

  return (
    <Dialog open={isOpen} onOpenChange={handleOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>New Session</DialogTitle>
        </DialogHeader>

        <div className="space-y-4">
          <div className="space-y-2">
            <label className="text-sm text-muted-foreground">Working Directory</label>
            <div className="flex gap-2">
              <Input
                value={cwd}
                onChange={(e) => setCwd(e.target.value)}
                placeholder="Select a directory..."
                onKeyDown={(e) => {
                  if (e.key === 'Enter') handleCreate()
                }}
              />
              <Button
                variant="outline"
                onClick={async () => {
                  const dir = await window.electronAPI.selectDirectory()
                  if (dir) setCwd(dir)
                }}
              >
                Browse...
              </Button>
            </div>
          </div>
        </div>

        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button onClick={handleCreate} disabled={!cwd.trim()}>
            Create
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// Delete Session Modal
interface DeleteSessionModalProps {
  onDeleteSession: (sessionId: string) => void
  onClose: () => void
}

function DeleteSessionModal({
  onDeleteSession,
  onClose
}: DeleteSessionModalProps): React.JSX.Element {
  const { isOpen, data: session } = useModal('deleteSession')

  const handleConfirm = (): void => {
    if (session) {
      onDeleteSession(session.id)
      onClose()
    }
  }

  const getSessionTitle = (s: MulticaSession): string => {
    if (s.title) return s.title
    const workingDir = s.workingDirectory ?? ''
    const parts = workingDir.split('/')
    return parts[parts.length - 1] || workingDir
  }

  return (
    <Dialog open={isOpen} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle>Delete Task</DialogTitle>
          <DialogDescription>
            Are you sure you want to delete &quot;{session && getSessionTitle(session)}&quot;? This
            action cannot be undone.
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="destructive" onClick={handleConfirm}>
            Delete
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// Delete Project Modal
interface DeleteProjectModalProps {
  onDeleteProject: (projectId: string) => Promise<void>
  onClose: () => void
}

function DeleteProjectModal({
  onDeleteProject,
  onClose
}: DeleteProjectModalProps): React.JSX.Element {
  const { isOpen, data: project } = useModal('deleteProject')

  const handleConfirm = async (): Promise<void> => {
    if (project) {
      await onDeleteProject(project.id)
      onClose()
    }
  }

  const getProjectName = (p: MulticaProject): string => {
    return p.name || p.workingDirectory.split('/').pop() || p.workingDirectory
  }

  return (
    <Dialog open={isOpen} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle>Delete Project</DialogTitle>
          <DialogDescription>
            Are you sure you want to delete &quot;{project && getProjectName(project)}&quot;? This
            will also delete all sessions in this project. This action cannot be undone.
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="destructive" onClick={handleConfirm}>
            Delete
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// Archive Session Modal
interface ArchiveSessionModalProps {
  onArchiveSession: (sessionId: string) => Promise<void>
  onClose: () => void
}

function ArchiveSessionModal({
  onArchiveSession,
  onClose
}: ArchiveSessionModalProps): React.JSX.Element {
  const { isOpen, data: session } = useModal('archiveSession')

  const handleConfirm = async (): Promise<void> => {
    if (session) {
      await onArchiveSession(session.id)
      onClose()
    }
  }

  const getSessionTitle = (s: MulticaSession): string => {
    if (s.title) return s.title
    const workingDir = s.workingDirectory ?? ''
    const parts = workingDir.split('/')
    return parts[parts.length - 1] || workingDir
  }

  return (
    <Dialog open={isOpen} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle>Archive Task</DialogTitle>
          <DialogDescription>
            Are you sure you want to archive &quot;{session && getSessionTitle(session)}&quot;? You
            can restore it later from the project menu.
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button onClick={handleConfirm}>Archive</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// Archived Sessions Modal
interface ArchivedSessionsModalProps {
  onUnarchiveSession: (sessionId: string) => Promise<void>
  onClose: () => void
}

function ArchivedSessionsModal({
  onUnarchiveSession,
  onClose
}: ArchivedSessionsModalProps): React.JSX.Element {
  const { isOpen, data } = useModal('archivedSessions')
  const [archivedSessions, setArchivedSessions] = useState<MulticaSession[]>([])
  const [isLoading, setIsLoading] = useState(false)

  // Load archived sessions when modal opens
  const loadArchivedSessions = useCallback(async (): Promise<void> => {
    if (!data?.projectId) return
    setIsLoading(true)
    try {
      const sessions = await window.electronAPI.listArchivedSessions(data.projectId)
      setArchivedSessions(sessions)
    } finally {
      setIsLoading(false)
    }
  }, [data?.projectId])

  useEffect(() => {
    if (isOpen && data?.projectId) {
      loadArchivedSessions()
    }
  }, [isOpen, data?.projectId, loadArchivedSessions])

  const handleUnarchive = async (sessionId: string): Promise<void> => {
    await onUnarchiveSession(sessionId)
    // Reload the list
    await loadArchivedSessions()
  }

  const getSessionTitle = (s: MulticaSession): string => {
    if (s.title) return s.title
    return `Session · ${s.id.slice(0, 6)}`
  }

  const formatDate = (iso: string): string => {
    const date = new Date(iso)
    const now = new Date()
    const diffMs = now.getTime() - date.getTime()
    const diffDays = Math.floor(diffMs / 86400000)

    if (diffDays < 1) return 'Today'
    if (diffDays === 1) return 'Yesterday'
    if (diffDays < 7) return `${diffDays} days ago`
    if (diffDays < 30) return `${Math.floor(diffDays / 7)} weeks ago`
    return date.toLocaleDateString()
  }

  return (
    <Dialog open={isOpen} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>Archived Sessions</DialogTitle>
          <DialogDescription>{data?.projectName}</DialogDescription>
        </DialogHeader>

        <div className="max-h-64 overflow-y-auto">
          {isLoading ? (
            <div className="py-8 text-center text-muted-foreground">Loading...</div>
          ) : archivedSessions.length === 0 ? (
            <div className="py-8 text-center text-muted-foreground">No archived sessions</div>
          ) : (
            <div className="space-y-2">
              {archivedSessions.map((session) => (
                <div
                  key={session.id}
                  className="flex items-center justify-between rounded-md border p-3"
                >
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-sm font-medium">{getSessionTitle(session)}</div>
                    <div className="text-xs text-muted-foreground">
                      Archived {formatDate(session.updatedAt)}
                    </div>
                  </div>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => handleUnarchive(session.id)}
                    title="Restore session"
                  >
                    <Undo2 className="h-4 w-4" />
                  </Button>
                </div>
              ))}
            </div>
          )}
        </div>

        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>
            Close
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
