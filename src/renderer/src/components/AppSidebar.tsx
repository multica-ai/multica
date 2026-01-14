/**
 * App Sidebar component - session list using shadcn sidebar
 */
import { useState } from 'react'
import type { MulticaSession } from '../../../shared/types'
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from '@/components/ui/sidebar'
import { Button } from '@/components/ui/button'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'
import { Plus, Settings, Trash2 } from 'lucide-react'
import { useModalStore } from '../stores/modalStore'

interface AppSidebarProps {
  sessions: MulticaSession[]
  currentSessionId: string | null
  onSelect: (sessionId: string) => void
  onNewSession: () => void
}

function formatDate(iso: string): string {
  const date = new Date(iso)
  const now = new Date()
  const diffMs = now.getTime() - date.getTime()
  const diffMins = Math.floor(diffMs / 60000)
  const diffHours = Math.floor(diffMs / 3600000)
  const diffDays = Math.floor(diffMs / 86400000)

  if (diffMins < 1) return 'Just now'
  if (diffMins < 60) return `${diffMins}m ago`
  if (diffHours < 24) return `${diffHours}h ago`
  if (diffDays < 7) return `${diffDays}d ago`
  return date.toLocaleDateString()
}

function getSessionTitle(session: MulticaSession): string {
  if (session.title) return session.title
  const parts = session.workingDirectory.split('/')
  return parts[parts.length - 1] || session.workingDirectory
}

// Session list item component
interface SessionItemProps {
  session: MulticaSession
  isActive: boolean
  onSelect: () => void
  onDelete: () => void
}

function SessionItem({ session, isActive, onSelect, onDelete }: SessionItemProps) {
  const [isHovered, setIsHovered] = useState(false)

  return (
    <SidebarMenuItem
      onMouseEnter={() => setIsHovered(true)}
      onMouseLeave={() => setIsHovered(false)}
    >
      <Tooltip delayDuration={600}>
        <TooltipTrigger asChild>
          <SidebarMenuButton
            isActive={isActive}
            onClick={onSelect}
            className={cn(
              "h-auto py-2 transition-colors duration-150",
              "hover:bg-sidebar-accent/50",
              isActive && "bg-sidebar-accent"
            )}
          >
            {/* Error indicator - only show on error */}
            {session.status === 'error' && (
              <span className="h-2 w-2 flex-shrink-0 rounded-full bg-red-500" />
            )}

            {/* Content */}
            <div className="min-w-0 flex-1">
              <span className="truncate text-sm">
                {getSessionTitle(session)}
              </span>
            </div>

            {/* Delete button - always rendered, visibility via opacity */}
            <button
              onClick={(e) => {
                e.stopPropagation()
                onDelete()
              }}
              className={cn(
                "flex-shrink-0 rounded p-1 transition-opacity duration-150",
                "hover:bg-muted active:bg-muted",
                isHovered ? "opacity-50 hover:opacity-100" : "opacity-0"
              )}
            >
              <Trash2 className="h-3.5 w-3.5 text-muted-foreground" />
            </button>
          </SidebarMenuButton>
        </TooltipTrigger>
        <TooltipContent side="right">
          <p>{session.agentId} · {formatDate(session.updatedAt)}</p>
        </TooltipContent>
      </Tooltip>
    </SidebarMenuItem>
  )
}

// Session list component
interface SessionListProps {
  sessions: MulticaSession[]
  currentSessionId: string | null
  onSelect: (sessionId: string) => void
  onDeleteRequest: (session: MulticaSession) => void
}

function SessionList({ sessions, currentSessionId, onSelect, onDeleteRequest }: SessionListProps) {
  if (sessions.length === 0) {
    return (
      <p className="px-2 py-4 text-center text-sm text-muted-foreground">
        No tasks yet
      </p>
    )
  }

  return (
    <SidebarMenu>
      {sessions.map((session) => (
        <SessionItem
          key={session.id}
          session={session}
          isActive={session.id === currentSessionId}
          onSelect={() => onSelect(session.id)}
          onDelete={() => onDeleteRequest(session)}
        />
      ))}
    </SidebarMenu>
  )
}

export function AppSidebar({
  sessions,
  currentSessionId,
  onSelect,
  onNewSession,
}: AppSidebarProps) {
  const openModal = useModalStore((s) => s.openModal)

  return (
    <Sidebar>
      {/* Header - just for traffic lights spacing */}
      <SidebarHeader className="titlebar-drag-region h-11 pl-20" />

      <SidebarContent className="px-2">
        {/* New task button */}
        <Button
          variant="ghost"
          className="w-full justify-start gap-2"
          onClick={onNewSession}
        >
          <Plus className="h-4 w-4 text-primary" />
          New task
        </Button>

        {/* Recent label */}
        <p className="px-2 py-2 text-xs text-muted-foreground/60">Recent</p>

        {/* Session list */}
        <SessionList
          sessions={sessions}
          currentSessionId={currentSessionId}
          onSelect={onSelect}
          onDeleteRequest={(session) => openModal('deleteSession', session)}
        />
      </SidebarContent>

      <SidebarFooter className="px-2 pb-2">
        <Button
          variant="secondary"
          size="sm"
          onClick={() => openModal('settings')}
          className="w-full justify-center gap-2"
        >
          <Settings className="h-4 w-4" />
          Setting
        </Button>
      </SidebarFooter>
    </Sidebar>
  )
}
