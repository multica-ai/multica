/**
 * Status bar component - shows agent status and current session info
 */
import type { AgentStatus, MulticaSession } from '../../../shared/types'

interface StatusBarProps {
  agentStatus: AgentStatus
  currentSession: MulticaSession | null
  onStartAgent: () => void
  onStopAgent: () => void
}

export function StatusBar({
  agentStatus,
  currentSession,
  onStartAgent,
  onStopAgent,
}: StatusBarProps) {
  return (
    <div className="flex items-center justify-between border-b border-[var(--color-border)] bg-[var(--color-surface)] px-4 py-2">
      {/* Left: Session info */}
      <div className="flex items-center gap-3">
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
      <div className="flex items-center gap-3">
        <AgentStatusBadge status={agentStatus} />

        {agentStatus.state === 'stopped' ? (
          <button
            onClick={onStartAgent}
            className="rounded bg-[var(--color-primary)] px-3 py-1 text-xs font-medium text-white transition-colors hover:bg-[var(--color-primary-dark)]"
          >
            Start Agent
          </button>
        ) : agentStatus.state === 'running' ? (
          <button
            onClick={onStopAgent}
            className="rounded bg-[var(--color-surface-hover)] px-3 py-1 text-xs font-medium transition-colors hover:bg-red-600 hover:text-white"
          >
            Stop
          </button>
        ) : null}
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
