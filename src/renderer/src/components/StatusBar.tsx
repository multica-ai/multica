/**
 * Status bar component - shows agent status and current session info
 */
import type { AgentStatus, MulticaSession } from '../../../shared/types'
import { Button } from '@/components/ui/button'
import { SidebarTrigger, useSidebar } from '@/components/ui/sidebar'
import { cn } from '@/lib/utils'
import { Settings } from 'lucide-react'

interface StatusBarProps {
  agentStatus: AgentStatus
  currentSession: MulticaSession | null
  onStartAgent: () => void
  onStopAgent: () => void
  onOpenSettings: () => void
}

export function StatusBar({
  agentStatus,
  currentSession,
  onStartAgent,
  onStopAgent,
  onOpenSettings,
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
            <span className="text-xs text-[var(--color-text-muted)]">
              {currentSession.workingDirectory}
            </span>
          </>
        ) : (
          <span className="text-sm text-[var(--color-text-muted)]">No session selected</span>
        )}
      </div>

      {/* Right: Agent status */}
      <div className="titlebar-no-drag flex items-center gap-3">
        <AgentStatusBadge status={agentStatus} />

        {agentStatus.state === 'stopped' ? (
          <Button size="sm" onClick={onStartAgent}>
            Start Agent
          </Button>
        ) : agentStatus.state === 'running' ? (
          <Button size="sm" variant="secondary" onClick={onStopAgent}>
            Stop
          </Button>
        ) : null}

        <Button variant="ghost" size="icon-sm" onClick={onOpenSettings} title="Settings">
          <Settings className="h-4 w-4" />
        </Button>
      </div>
    </div>
  )
}

interface AgentStatusBadgeProps {
  status: AgentStatus
}

function AgentStatusBadge({ status }: AgentStatusBadgeProps) {
  let dotColor = 'bg-gray-500'
  let text = 'Stopped'

  switch (status.state) {
    case 'starting':
      dotColor = 'bg-yellow-500 animate-pulse'
      text = `Starting ${status.agentId}...`
      break
    case 'running':
      dotColor = 'bg-green-500'
      text = status.agentId
      break
    case 'error':
      dotColor = 'bg-red-500'
      text = 'Error'
      break
    case 'stopped':
      dotColor = 'bg-gray-500'
      text = 'Stopped'
      break
  }

  return (
    <div className="flex items-center gap-2">
      <span className={`h-2 w-2 rounded-full ${dotColor}`} />
      <span className="text-xs text-[var(--color-text-muted)]">{text}</span>
    </div>
  )
}
