/**
 * App Sidebar component - project list with nested sessions using shadcn sidebar
 */
import { useMemo } from 'react'
import type { MulticaSession, MulticaProject } from '../../../shared/types'
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarHeader,
  SidebarMenu
} from '@/components/ui/sidebar'
import { Button } from '@/components/ui/button'
import { FolderPlus, Settings } from 'lucide-react'
import { useModalStore } from '../stores/modalStore'
import { SortableProjectItem } from './ProjectItem'
import {
  DndContext,
  closestCenter,
  KeyboardSensor,
  PointerSensor,
  useSensor,
  useSensors,
  type DragEndEvent
} from '@dnd-kit/core'
import {
  SortableContext,
  sortableKeyboardCoordinates,
  verticalListSortingStrategy
} from '@dnd-kit/sortable'

interface AppSidebarProps {
  projects: MulticaProject[]
  sessionsByProject: Map<string, MulticaSession[]>
  currentSessionId: string | null
  processingSessionIds: string[]
  permissionPendingSessionId: string | null
  onSelectSession: (sessionId: string) => void
  onNewProject: () => void
  onNewSession: (projectId: string) => void
  onToggleProjectExpanded: (projectId: string) => void
  onReorderProjects: (projectIds: string[]) => void
}

export function AppSidebar({
  projects,
  sessionsByProject,
  currentSessionId,
  processingSessionIds,
  permissionPendingSessionId,
  onSelectSession,
  onNewProject,
  onNewSession,
  onToggleProjectExpanded,
  onReorderProjects
}: AppSidebarProps): React.JSX.Element {
  const openModal = useModalStore((s) => s.openModal)

  // Configure sensors for drag and drop
  const sensors = useSensors(
    useSensor(PointerSensor, {
      activationConstraint: {
        distance: 8 // Require 8px movement to start drag
      }
    }),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates
    })
  )

  // Get project IDs for SortableContext
  const projectIds = useMemo(() => projects.map((p) => p.id), [projects])

  // Handle drag end - reorder projects
  const handleDragEnd = (event: DragEndEvent): void => {
    const { active, over } = event

    if (over && active.id !== over.id) {
      const oldIndex = projects.findIndex((p) => p.id === active.id)
      const newIndex = projects.findIndex((p) => p.id === over.id)

      if (oldIndex !== -1 && newIndex !== -1) {
        // Create new order array
        const newOrder = [...projectIds]
        newOrder.splice(oldIndex, 1)
        newOrder.splice(newIndex, 0, active.id as string)

        onReorderProjects(newOrder)
      }
    }
  }

  return (
    <Sidebar>
      {/* Header - just for traffic lights spacing */}
      <SidebarHeader className="titlebar-drag-region h-11 pl-20" />

      <SidebarContent className="px-1">
        {/* New Project button */}
        <Button
          variant="ghost"
          size="sm"
          className="w-full justify-start gap-2 hover:bg-sidebar-accent"
          onClick={onNewProject}
        >
          <FolderPlus className="h-4 w-4 text-primary" />
          New Project
        </Button>

        {/* Projects list */}
        {projects.length === 0 ? (
          <p className="px-2 py-4 text-center text-sm text-muted-foreground">No projects yet</p>
        ) : (
          <DndContext
            sensors={sensors}
            collisionDetection={closestCenter}
            onDragEnd={handleDragEnd}
          >
            <SortableContext items={projectIds} strategy={verticalListSortingStrategy}>
              <SidebarMenu className="mt-2">
                {projects.map((project) => (
                  <SortableProjectItem
                    key={project.id}
                    project={project}
                    sessions={sessionsByProject.get(project.id) ?? []}
                    currentSessionId={currentSessionId}
                    processingSessionIds={processingSessionIds}
                    permissionPendingSessionId={permissionPendingSessionId}
                    onSelectSession={onSelectSession}
                    onNewSession={onNewSession}
                    onToggleExpanded={onToggleProjectExpanded}
                    onDeleteProject={(p): void => openModal('deleteProject', p)}
                    onArchiveSession={(s): void => openModal('archiveSession', s)}
                    onViewArchivedSessions={(p): void =>
                      openModal('archivedSessions', { projectId: p.id, projectName: p.name })
                    }
                  />
                ))}
              </SidebarMenu>
            </SortableContext>
          </DndContext>
        )}
      </SidebarContent>

      <SidebarFooter className="px-1 pb-1">
        <Button
          variant="ghost"
          size="sm"
          onClick={(): void => openModal('settings')}
          className="w-full justify-center gap-2 hover:bg-sidebar-accent"
        >
          <Settings className="h-4 w-4" />
          Settings
        </Button>
      </SidebarFooter>
    </Sidebar>
  )
}
