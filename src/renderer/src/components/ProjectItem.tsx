/**
 * ProjectItem component - renders a project with its sessions in sidebar
 */
import { useState } from 'react'
import type { MulticaProject, MulticaSession } from '../../../shared/types'
import {
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarMenuSub
} from '@/components/ui/sidebar'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'
import {
  AlertTriangle,
  Archive,
  ChevronDown,
  ChevronRight,
  CirclePause,
  Folder,
  Loader2,
  MoreHorizontal,
  Plus,
  Trash2
} from 'lucide-react'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from '@/components/ui/dropdown-menu'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { useSortable } from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'

interface ProjectItemProps {
  project: MulticaProject
  sessions: MulticaSession[]
  currentSessionId: string | null
  processingSessionIds: string[]
  permissionPendingSessionId: string | null
  onSelectSession: (sessionId: string) => void
  onNewSession: (projectId: string) => void
  onToggleExpanded: (projectId: string) => void
  onDeleteProject: (project: MulticaProject) => void
  onArchiveSession: (session: MulticaSession) => void
  onViewArchivedSessions: (project: MulticaProject) => void
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
  // Use short ID as default title
  const shortId = session.id.slice(0, 6)
  return `Session · ${shortId}`
}

// Session item component (nested under project)
interface SessionItemProps {
  session: MulticaSession
  isActive: boolean
  isProcessing: boolean
  needsPermission: boolean
  onSelect: () => void
  onArchive: () => void
}

function SessionItem({
  session,
  isActive,
  isProcessing,
  needsPermission,
  onSelect,
  onArchive
}: SessionItemProps): React.JSX.Element {
  const [isHovered, setIsHovered] = useState(false)

  return (
    <SidebarMenuItem
      onMouseEnter={(): void => setIsHovered(true)}
      onMouseLeave={(): void => setIsHovered(false)}
    >
      <SidebarMenuButton
        isActive={isActive}
        onClick={onSelect}
        className={cn(
          'h-auto py-1 pl-10 transition-colors duration-150',
          'hover:bg-sidebar-accent/50',
          isActive && 'bg-sidebar-accent'
        )}
      >
        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
          {/* Title + Status */}
          <div className="flex items-center gap-1.5">
            <span className="truncate text-sm">{getSessionTitle(session)}</span>
            {needsPermission ? (
              <CirclePause className="h-3 w-3 shrink-0 text-amber-500" />
            ) : isProcessing ? (
              <Loader2 className="h-3 w-3 shrink-0 animate-spin text-primary" />
            ) : null}
          </div>

          {/* Git branch + Timestamp */}
          <span className="text-xs text-muted-foreground/60">
            {session.gitBranch && (
              <>
                <span className="font-medium">{session.gitBranch}</span>
                <span className="mx-1">·</span>
              </>
            )}
            {formatDate(session.updatedAt)}
          </span>
        </div>

        {/* Archive button */}
        <div
          role="button"
          tabIndex={0}
          onClick={(e): void => {
            e.stopPropagation()
            onArchive()
          }}
          onKeyDown={(e): void => {
            if (e.key === 'Enter' || e.key === ' ') {
              e.preventDefault()
              e.stopPropagation()
              onArchive()
            }
          }}
          className={cn(
            'shrink-0 cursor-pointer rounded p-0.5 transition-opacity duration-150',
            'hover:bg-muted active:bg-muted',
            isHovered ? 'opacity-50 hover:opacity-100' : 'opacity-0'
          )}
        >
          <Archive className="h-3 w-3 text-muted-foreground" />
        </div>
      </SidebarMenuButton>
    </SidebarMenuItem>
  )
}

interface ProjectItemInnerProps extends ProjectItemProps {
  dragProps?: React.HTMLAttributes<HTMLLIElement>
  isDragging?: boolean
}

function ProjectItemInner({
  project,
  sessions,
  currentSessionId,
  processingSessionIds,
  permissionPendingSessionId,
  onSelectSession,
  onNewSession,
  onToggleExpanded,
  onDeleteProject,
  onArchiveSession,
  onViewArchivedSessions,
  dragProps,
  isDragging
}: ProjectItemInnerProps): React.JSX.Element {
  const [isHovered, setIsHovered] = useState(false)
  const isInvalid = project.directoryExists === false

  return (
    <Collapsible open={project.isExpanded} className="group/collapsible">
      <SidebarMenuItem
        onMouseEnter={(): void => setIsHovered(true)}
        onMouseLeave={(): void => setIsHovered(false)}
        className={cn(isDragging && 'opacity-50')}
        {...dragProps}
      >
        <Tooltip delayDuration={600}>
          <TooltipTrigger asChild>
            <CollapsibleTrigger asChild>
              <SidebarMenuButton
                onClick={(): void => onToggleExpanded(project.id)}
                className={cn(
                  'h-auto py-1.5 transition-colors duration-150 cursor-grab active:cursor-grabbing',
                  'hover:bg-sidebar-accent/50'
                )}
              >
                {/* Expand/collapse icon */}
                {project.isExpanded ? (
                  <ChevronDown className="h-4 w-4 shrink-0 text-muted-foreground" />
                ) : (
                  <ChevronRight className="h-4 w-4 shrink-0 text-muted-foreground" />
                )}

                {/* Folder icon */}
                <Folder className="h-4 w-4 shrink-0 text-muted-foreground" />

                {/* Project name */}
                <span className="flex-1 truncate text-sm font-medium">{project.name}</span>

                {/* Invalid directory indicator */}
                {isInvalid && <AlertTriangle className="h-3.5 w-3.5 shrink-0 text-amber-500" />}

                {/* Add session button */}
                <div
                  role="button"
                  tabIndex={0}
                  onClick={(e): void => {
                    e.stopPropagation()
                    onNewSession(project.id)
                  }}
                  onKeyDown={(e): void => {
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.preventDefault()
                      e.stopPropagation()
                      onNewSession(project.id)
                    }
                  }}
                  className={cn(
                    'shrink-0 cursor-pointer rounded p-0.5 transition-opacity duration-150',
                    'hover:bg-muted active:bg-muted',
                    isHovered ? 'opacity-70 hover:opacity-100' : 'opacity-0'
                  )}
                >
                  <Plus className="h-4 w-4 text-primary" />
                </div>

                {/* Project menu (... button) */}
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <div
                      role="button"
                      tabIndex={0}
                      onClick={(e): void => e.stopPropagation()}
                      className={cn(
                        'shrink-0 cursor-pointer rounded p-0.5 transition-opacity duration-150',
                        'hover:bg-muted active:bg-muted',
                        isHovered ? 'opacity-50 hover:opacity-100' : 'opacity-0'
                      )}
                    >
                      <MoreHorizontal className="h-3.5 w-3.5 text-muted-foreground" />
                    </div>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end" className="w-48">
                    <DropdownMenuItem
                      onClick={(e): void => {
                        e.stopPropagation()
                        onViewArchivedSessions(project)
                      }}
                    >
                      <Archive className="h-4 w-4 mr-2" />
                      View Archived Sessions
                    </DropdownMenuItem>
                    <DropdownMenuSeparator />
                    <DropdownMenuItem
                      onClick={(e): void => {
                        e.stopPropagation()
                        onDeleteProject(project)
                      }}
                      className="text-destructive focus:text-destructive"
                    >
                      <Trash2 className="h-4 w-4 mr-2" />
                      Delete Project
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              </SidebarMenuButton>
            </CollapsibleTrigger>
          </TooltipTrigger>
          <TooltipContent side="right">
            {isInvalid ? (
              <p className="text-amber-500">Directory not found: {project.workingDirectory}</p>
            ) : (
              <p>{project.workingDirectory}</p>
            )}
          </TooltipContent>
        </Tooltip>

        {/* Sessions list (collapsible) */}
        <CollapsibleContent>
          <SidebarMenuSub className="mx-0 px-0 border-l-0">
            {sessions.length === 0 ? (
              <div className="py-1 pl-10 text-xs text-muted-foreground">No sessions</div>
            ) : (
              <SidebarMenu>
                {sessions.map((session) => (
                  <SessionItem
                    key={session.id}
                    session={session}
                    isActive={session.id === currentSessionId}
                    isProcessing={processingSessionIds.includes(session.id)}
                    needsPermission={session.id === permissionPendingSessionId}
                    onSelect={(): void => onSelectSession(session.id)}
                    onArchive={(): void => onArchiveSession(session)}
                  />
                ))}
              </SidebarMenu>
            )}
          </SidebarMenuSub>
        </CollapsibleContent>
      </SidebarMenuItem>
    </Collapsible>
  )
}

export function ProjectItem(props: ProjectItemProps): React.JSX.Element {
  return <ProjectItemInner {...props} />
}

/**
 * Sortable wrapper for ProjectItem - used in AppSidebar for drag-and-drop reordering
 */
export function SortableProjectItem(props: ProjectItemProps): React.JSX.Element {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: props.project.id
  })

  const style = {
    transform: CSS.Transform.toString(transform),
    transition
  }

  return (
    <div ref={setNodeRef} style={style}>
      <ProjectItemInner
        {...props}
        dragProps={{ ...attributes, ...listeners } as React.HTMLAttributes<HTMLLIElement>}
        isDragging={isDragging}
      />
    </div>
  )
}
