/**
 * Session list sidebar component
 */
import type { MulticaSession } from '../../../shared/types'

interface SessionListProps {
  sessions: MulticaSession[]
  currentSessionId: string | null
  onSelect: (sessionId: string) => void
  onDelete: (sessionId: string) => void
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
  // Use last part of working directory
  const parts = session.workingDirectory.split('/')
  return parts[parts.length - 1] || session.workingDirectory
}

export function SessionList({
  sessions,
  currentSessionId,
  onSelect,
  onDelete,
  onNewSession,
}: SessionListProps) {
  return (
    <aside className="flex h-full w-64 flex-shrink-0 flex-col border-r border-[var(--color-border)] bg-[var(--color-surface)]">
      {/* Header */}
      <div className="flex items-center justify-between border-b border-[var(--color-border)] p-3">
        <h2 className="text-sm font-semibold text-[var(--color-text-muted)]">Sessions</h2>
        <button
          onClick={onNewSession}
          className="rounded p-1 text-[var(--color-text-muted)] transition-colors hover:bg-[var(--color-surface-hover)] hover:text-[var(--color-text)]"
          title="New Session"
        >
          <svg className="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
          </svg>
        </button>
      </div>

      {/* Session list */}
      <div className="flex-1 overflow-y-auto p-2">
        {sessions.length === 0 ? (
          <p className="p-2 text-center text-sm text-[var(--color-text-muted)]">
            No sessions yet
          </p>
        ) : (
          <ul className="space-y-1">
            {sessions.map((session) => (
              <SessionItem
                key={session.id}
                session={session}
                isActive={session.id === currentSessionId}
                onSelect={() => onSelect(session.id)}
                onDelete={() => onDelete(session.id)}
              />
            ))}
          </ul>
        )}
      </div>
    </aside>
  )
}

interface SessionItemProps {
  session: MulticaSession
  isActive: boolean
  onSelect: () => void
  onDelete: () => void
}

function SessionItem({ session, isActive, onSelect, onDelete }: SessionItemProps) {
  return (
    <li
      onClick={onSelect}
      className={`group flex cursor-pointer items-start gap-2 rounded-lg p-2 transition-colors ${
        isActive
          ? 'bg-[var(--color-primary)] text-white'
          : 'hover:bg-[var(--color-surface-hover)]'
      }`}
    >
      {/* Status indicator */}
      <span
        className={`mt-1.5 h-2 w-2 flex-shrink-0 rounded-full ${
          session.status === 'active'
            ? 'bg-green-500'
            : session.status === 'error'
              ? 'bg-red-500'
              : 'bg-gray-500'
        }`}
      />

      {/* Content */}
      <div className="min-w-0 flex-1">
        <div className="truncate text-sm font-medium">
          {getSessionTitle(session)}
        </div>
        <div
          className={`truncate text-xs ${
            isActive ? 'text-white/70' : 'text-[var(--color-text-muted)]'
          }`}
        >
          {session.agentId} · {formatDate(session.updatedAt)}
        </div>
      </div>

      {/* Delete button */}
      <button
        onClick={(e) => {
          e.stopPropagation()
          onDelete()
        }}
        className={`flex-shrink-0 rounded p-1 opacity-0 transition-opacity group-hover:opacity-100 ${
          isActive
            ? 'hover:bg-white/20'
            : 'hover:bg-[var(--color-surface-hover)]'
        }`}
        title="Delete session"
      >
        <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path
            strokeLinecap="round"
            strokeLinejoin="round"
            strokeWidth={2}
            d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"
          />
        </svg>
      </button>
    </li>
  )
}
