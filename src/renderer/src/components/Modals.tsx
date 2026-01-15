/**
 * Global modals registry
 * All app modals are rendered here and controlled via modalStore
 */
import { useState } from 'react'
import { useModalStore, useModal } from '../stores/modalStore'
import { Settings } from './Settings'
import type { MulticaSession } from '../../../shared/types'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

interface ModalsProps {
  // Settings props
  defaultAgentId: string
  onSetDefaultAgent: (agentId: string) => void
  // NewSession props
  onCreateSession: (cwd: string) => Promise<void>
  // DeleteSession props
  onDeleteSession: (sessionId: string) => void
}

export function Modals({
  defaultAgentId,
  onSetDefaultAgent,
  onCreateSession,
  onDeleteSession,
}: ModalsProps) {
  const closeModal = useModalStore((s) => s.closeModal)

  return (
    <>
      <SettingsModal
        defaultAgentId={defaultAgentId}
        onSetDefaultAgent={onSetDefaultAgent}
        onCreateSession={onCreateSession}
        onClose={() => closeModal('settings')}
      />
      <NewSessionModal
        onCreateSession={onCreateSession}
        onClose={() => closeModal('newSession')}
      />
      <DeleteSessionModal
        onDeleteSession={onDeleteSession}
        onClose={() => closeModal('deleteSession')}
      />
    </>
  )
}

// Settings Modal
interface SettingsModalProps {
  defaultAgentId: string
  onSetDefaultAgent: (agentId: string) => void
  onCreateSession: (cwd: string) => Promise<void>
  onClose: () => void
}

function SettingsModal({ defaultAgentId, onSetDefaultAgent, onCreateSession, onClose }: SettingsModalProps) {
  const { isOpen, data } = useModal('settings')

  const handleClose = async () => {
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
      highlightAgent={data?.highlightAgent}
    />
  )
}

// New Session Modal
interface NewSessionModalProps {
  onCreateSession: (cwd: string) => Promise<void>
  onClose: () => void
}

function NewSessionModal({ onCreateSession, onClose }: NewSessionModalProps) {
  const { isOpen } = useModal('newSession')
  const [cwd, setCwd] = useState('')

  const handleCreate = async () => {
    if (!cwd.trim()) return
    await onCreateSession(cwd.trim())
    setCwd('')
    onClose()
  }

  const handleOpenChange = (open: boolean) => {
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

function DeleteSessionModal({ onDeleteSession, onClose }: DeleteSessionModalProps) {
  const { isOpen, data: session } = useModal('deleteSession')

  const handleConfirm = () => {
    if (session) {
      onDeleteSession(session.id)
      onClose()
    }
  }

  const getSessionTitle = (s: MulticaSession): string => {
    if (s.title) return s.title
    const parts = s.workingDirectory.split('/')
    return parts[parts.length - 1] || s.workingDirectory
  }

  return (
    <Dialog open={isOpen} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle>Delete Task</DialogTitle>
          <DialogDescription>
            Are you sure you want to delete "{session && getSessionTitle(session)}"? This action
            cannot be undone.
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
