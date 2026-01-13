/**
 * Settings dialog component
 * Allows switching agents and viewing installation status
 */
import { useState, useEffect } from 'react'
import type { AgentCheckResult } from '../../../shared/electron-api'

interface SettingsProps {
  isOpen: boolean
  onClose: () => void
  currentAgentId: string | null
  onSwitchAgent: (agentId: string) => Promise<void>
}

export function Settings({ isOpen, onClose, currentAgentId, onSwitchAgent }: SettingsProps) {
  const [agents, setAgents] = useState<AgentCheckResult[]>([])
  const [loading, setLoading] = useState(true)
  const [switching, setSwitching] = useState<string | null>(null)

  useEffect(() => {
    if (isOpen) {
      loadAgents()
    }
  }, [isOpen])

  async function loadAgents() {
    setLoading(true)
    try {
      const results = await window.electronAPI.checkAgents()
      setAgents(results)
    } catch (err) {
      console.error('Failed to check agents:', err)
    } finally {
      setLoading(false)
    }
  }

  async function handleSwitch(agentId: string) {
    if (switching || agentId === currentAgentId) return

    setSwitching(agentId)
    try {
      await onSwitchAgent(agentId)
      onClose()
    } catch (err) {
      console.error('Failed to switch agent:', err)
    } finally {
      setSwitching(null)
    }
  }

  if (!isOpen) return null

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
      <div className="w-full max-w-lg rounded-lg bg-[var(--color-surface)] p-6 shadow-xl">
        <div className="mb-6 flex items-center justify-between">
          <h2 className="text-lg font-semibold">Settings</h2>
          <button
            onClick={onClose}
            className="rounded p-1 text-[var(--color-text-muted)] transition-colors hover:bg-[var(--color-surface-hover)] hover:text-[var(--color-text)]"
          >
            <svg className="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth={2}
                d="M6 18L18 6M6 6l12 12"
              />
            </svg>
          </button>
        </div>

        {/* Agent Selection */}
        <div className="mb-6">
          <h3 className="mb-3 text-sm font-medium text-[var(--color-text-muted)]">
            Select Coding Agent
          </h3>

          {loading ? (
            <div className="flex items-center justify-center py-8">
              <div className="h-6 w-6 animate-spin rounded-full border-2 border-[var(--color-primary)] border-t-transparent" />
            </div>
          ) : (
            <div className="space-y-2">
              {agents.map((agent) => (
                <AgentItem
                  key={agent.id}
                  agent={agent}
                  isActive={agent.id === currentAgentId}
                  isSwitching={switching === agent.id}
                  onSelect={() => handleSwitch(agent.id)}
                />
              ))}
            </div>
          )}
        </div>

        {/* Refresh button */}
        <div className="flex justify-end gap-2">
          <button
            onClick={loadAgents}
            disabled={loading}
            className="rounded-lg px-4 py-2 text-[var(--color-text-muted)] transition-colors hover:bg-[var(--color-surface-hover)] disabled:opacity-50"
          >
            Refresh
          </button>
          <button
            onClick={onClose}
            className="rounded-lg bg-[var(--color-primary)] px-4 py-2 font-medium text-white transition-colors hover:bg-[var(--color-primary-dark)]"
          >
            Done
          </button>
        </div>
      </div>
    </div>
  )
}

interface AgentItemProps {
  agent: AgentCheckResult
  isActive: boolean
  isSwitching: boolean
  onSelect: () => void
}

function AgentItem({ agent, isActive, isSwitching, onSelect }: AgentItemProps) {
  const canSelect = agent.installed && !isActive && !isSwitching

  return (
    <div
      onClick={canSelect ? onSelect : undefined}
      className={`flex items-center gap-3 rounded-lg border p-3 transition-colors ${
        isActive
          ? 'border-[var(--color-primary)] bg-[var(--color-primary)]/10'
          : agent.installed
            ? 'cursor-pointer border-[var(--color-border)] hover:border-[var(--color-primary)] hover:bg-[var(--color-surface-hover)]'
            : 'border-[var(--color-border)] opacity-60'
      }`}
    >
      {/* Status indicator */}
      <div
        className={`h-3 w-3 flex-shrink-0 rounded-full ${
          agent.installed ? 'bg-green-500' : 'bg-gray-400'
        }`}
      />

      {/* Agent info */}
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="font-medium">{agent.name}</span>
          {isActive && (
            <span className="rounded bg-[var(--color-primary)] px-1.5 py-0.5 text-xs text-white">
              Active
            </span>
          )}
        </div>
        <div className="text-sm text-[var(--color-text-muted)]">
          {agent.installed ? (
            <>
              <span className="font-mono">{agent.command}</span>
              {agent.version && <span className="ml-2 text-xs">{agent.version}</span>}
            </>
          ) : (
            <span className="text-red-400">Not installed</span>
          )}
        </div>
        {!agent.installed && agent.installHint && (
          <div className="mt-1 text-xs text-[var(--color-text-muted)]">
            Install: <code className="rounded bg-[var(--color-background)] px-1">{agent.installHint}</code>
          </div>
        )}
      </div>

      {/* Action */}
      {isSwitching ? (
        <div className="h-5 w-5 animate-spin rounded-full border-2 border-[var(--color-primary)] border-t-transparent" />
      ) : agent.installed && !isActive ? (
        <button
          onClick={(e) => {
            e.stopPropagation()
            onSelect()
          }}
          className="flex-shrink-0 rounded-lg bg-[var(--color-surface-hover)] px-3 py-1.5 text-sm font-medium transition-colors hover:bg-[var(--color-border)]"
        >
          Switch
        </button>
      ) : null}
    </div>
  )
}
