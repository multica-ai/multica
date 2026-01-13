/**
 * Status bar component - shows agent status and current session info
 */
import type { AgentStatus, MulticaSession } from '../../../shared/types'

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

        {/* Settings button */}
        <button
          onClick={onOpenSettings}
          className="rounded p-1.5 text-[var(--color-text-muted)] transition-colors hover:bg-[var(--color-surface-hover)] hover:text-[var(--color-text)]"
          title="Settings"
        >
          <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth={2}
              d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z"
            />
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth={2}
              d="M15 12a3 3 0 11-6 0 3 3 0 016 0z"
            />
          </svg>
        </button>
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
