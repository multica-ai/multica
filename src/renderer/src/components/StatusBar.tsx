/**
 * Status bar component - shows session info and running status
 */
import type { MulticaSession } from '../../../shared/types'
import { SidebarTrigger, useSidebar } from '@/components/ui/sidebar'
import { cn } from '@/lib/utils'

interface StatusBarProps {
  runningSessionsCount: number
  currentSession: MulticaSession | null
  isCurrentSessionRunning: boolean
}

export function StatusBar({
  runningSessionsCount,
  currentSession,
  isCurrentSessionRunning,
}: StatusBarProps) {
  const { state, isMobile } = useSidebar()

  // Need left padding for traffic lights when sidebar is not visible
  const needsTrafficLightPadding = state === 'collapsed' || isMobile

  return (
    <div className={cn(
      "titlebar-drag-region flex h-11 items-center justify-between px-4",
      needsTrafficLightPadding && "pl-20"
    )}>
      {/* Left: Sidebar trigger + Session info */}
      <div className="titlebar-no-drag flex items-center gap-3">
        <SidebarTrigger className="-ml-1" />
        {currentSession ? (
          <>
            <span className="text-sm font-medium">
              {currentSession.title || currentSession.workingDirectory.split('/').pop()}
            </span>
            <span className="text-xs text-muted-foreground">
              {currentSession.workingDirectory}
            </span>
          </>
        ) : (
          <span className="text-sm text-muted-foreground">No session selected</span>
        )}
      </div>

      {/* Right: Status */}
      <div className="titlebar-no-drag flex items-center gap-3">
        <SessionStatusBadge
          isRunning={isCurrentSessionRunning}
          runningCount={runningSessionsCount}
        />
      </div>
    </div>
  )
}

interface SessionStatusBadgeProps {
  isRunning: boolean
  runningCount: number
}

function SessionStatusBadge({ isRunning, runningCount }: SessionStatusBadgeProps) {
  const dotColor = isRunning ? 'bg-green-500' : 'bg-gray-500'
  const text = isRunning
    ? `Running (${runningCount} session${runningCount !== 1 ? 's' : ''})`
    : runningCount > 0
      ? `${runningCount} running`
      : 'No sessions'

  return (
    <div className="flex items-center gap-2">
      <span className={`h-2 w-2 rounded-full ${dotColor}`} />
      <span className="text-xs text-muted-foreground">{text}</span>
    </div>
  )
}
