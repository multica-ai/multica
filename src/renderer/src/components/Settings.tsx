/**
 * Agent setup / Settings component
 * Minimalist design with bordered cards
 */
import { useState, useEffect } from 'react'
import type { AgentCheckResult } from '../../../shared/electron-api'
import { useTheme } from '../contexts/ThemeContext'

interface SettingsProps {
  isOpen: boolean
  onClose: () => void
  currentAgentId: string | null
  onSwitchAgent: (agentId: string) => Promise<void>
}

// Agent icons mapping
const AGENT_ICONS: Record<string, string> = {
  'claude-code': '◉',
  opencode: '⌘',
  codex: '◈',
  gemini: '✦',
}

type ThemeMode = 'light' | 'dark' | 'system'

export function Settings({ isOpen, onClose, currentAgentId, onSwitchAgent }: SettingsProps) {
  const [agents, setAgents] = useState<AgentCheckResult[]>([])
  const [loading, setLoading] = useState(true)
  const [switching, setSwitching] = useState<string | null>(null)
  const [selectedAgent, setSelectedAgent] = useState<string | null>(null)
  const { mode, setMode } = useTheme()

  useEffect(() => {
    if (isOpen) {
      // Reset selection when opening
      setSelectedAgent(currentAgentId)
      loadAgents()
    }
  }, [isOpen, currentAgentId])

  // Auto-select first installed agent if none selected
  useEffect(() => {
    if (agents.length > 0 && !selectedAgent) {
      const firstInstalled = agents.find(a => a.installed)
      if (firstInstalled) {
        setSelectedAgent(firstInstalled.id)
      }
    }
  }, [agents, selectedAgent])

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

  async function handleContinue() {
    if (!selectedAgent || switching) return

    // If already using this agent, just close
    if (selectedAgent === currentAgentId) {
      onClose()
      return
    }

    setSwitching(selectedAgent)
    try {
      await onSwitchAgent(selectedAgent)
      onClose()
    } catch (err) {
      console.error('Failed to switch agent:', err)
    } finally {
      setSwitching(null)
    }
  }

  if (!isOpen) return null

  const installedCount = agents.filter(a => a.installed).length

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60">
      <div className="w-full max-w-md rounded-xl bg-[var(--color-background)] p-8 max-h-[90vh] overflow-y-auto">
        {/* Header */}
        <div className="mb-6">
          <h1 className="text-2xl font-semibold text-[var(--color-text)]">Settings</h1>
        </div>

        {/* Appearance Section */}
        <div className="mb-8">
          <h2 className="mb-3 text-sm font-medium text-[var(--color-text-muted)]">Appearance</h2>
          <ThemeSelector value={mode} onChange={setMode} />
        </div>

        {/* Agent Section */}
        <div className="mb-6">
          <h2 className="mb-3 text-sm font-medium text-[var(--color-text-muted)]">Coding Agent</h2>
          <p className="mb-4 text-xs text-[var(--color-text-muted)]">
            Select a coding agent. Agents use local authentication.
          </p>

          {/* Agent Cards */}
          {loading ? (
            <div className="flex items-center justify-center py-8">
              <div className="h-6 w-6 animate-spin rounded-full border-2 border-[var(--color-accent)] border-t-transparent" />
            </div>
          ) : (
            <div className="space-y-3">
              {agents.map((agent) => (
                <AgentCard
                  key={agent.id}
                  agent={agent}
                  isSelected={agent.id === selectedAgent}
                  onSelect={() => agent.installed && setSelectedAgent(agent.id)}
                />
              ))}
            </div>
          )}
        </div>

        {/* Footer */}
        <div className="flex items-center justify-between border-t border-[var(--color-border)] pt-6">
          <button
            onClick={loadAgents}
            disabled={loading}
            className="text-sm text-[var(--color-text-muted)] hover:text-[var(--color-text)] disabled:opacity-50"
          >
            Refresh agents
          </button>

          <div className="flex gap-2">
            <button
              onClick={onClose}
              className="rounded-lg px-4 py-2 text-[var(--color-text-muted)] hover:bg-[var(--color-surface)]"
            >
              Cancel
            </button>
            <button
              onClick={handleContinue}
              disabled={!selectedAgent || !!switching || installedCount === 0}
              className="flex items-center gap-2 rounded-lg bg-[var(--color-primary)] px-5 py-2 font-medium text-[var(--color-primary-text)] transition-colors hover:bg-[var(--color-primary-dark)] disabled:opacity-50"
            >
              {switching ? (
                <>
                  <span className="h-4 w-4 animate-spin rounded-full border-2 border-current border-t-transparent" />
                  Switching...
                </>
              ) : (
                'Done'
              )}
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}

interface ThemeSelectorProps {
  value: ThemeMode
  onChange: (mode: ThemeMode) => void
}

function ThemeSelector({ value, onChange }: ThemeSelectorProps) {
  const options: { mode: ThemeMode; label: string; icon: React.ReactNode }[] = [
    {
      mode: 'light',
      label: 'Light',
      icon: (
        <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
          <path strokeLinecap="round" strokeLinejoin="round" d="M12 3v1m0 16v1m9-9h-1M4 12H3m15.364 6.364l-.707-.707M6.343 6.343l-.707-.707m12.728 0l-.707.707M6.343 17.657l-.707.707M16 12a4 4 0 11-8 0 4 4 0 018 0z" />
        </svg>
      ),
    },
    {
      mode: 'dark',
      label: 'Dark',
      icon: (
        <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
          <path strokeLinecap="round" strokeLinejoin="round" d="M20.354 15.354A9 9 0 018.646 3.646 9.003 9.003 0 0012 21a9.003 9.003 0 008.354-5.646z" />
        </svg>
      ),
    },
    {
      mode: 'system',
      label: 'System',
      icon: (
        <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
          <path strokeLinecap="round" strokeLinejoin="round" d="M9.75 17L9 20l-1 1h8l-1-1-.75-3M3 13h18M5 17h14a2 2 0 002-2V5a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z" />
        </svg>
      ),
    },
  ]

  return (
    <div className="flex gap-2">
      {options.map(({ mode, label, icon }) => (
        <button
          key={mode}
          onClick={() => onChange(mode)}
          className={`flex flex-1 items-center justify-center gap-2 rounded-lg border px-3 py-2.5 text-sm transition-all ${
            value === mode
              ? 'border-[var(--color-accent)] bg-[var(--color-accent-muted)] text-[var(--color-text)]'
              : 'border-[var(--color-border)] text-[var(--color-text-muted)] hover:border-[var(--color-text-muted)]'
          }`}
        >
          {icon}
          {label}
        </button>
      ))}
    </div>
  )
}

interface AgentCardProps {
  agent: AgentCheckResult
  isSelected: boolean
  onSelect: () => void
}

function AgentCard({ agent, isSelected, onSelect }: AgentCardProps) {
  const icon = AGENT_ICONS[agent.id] || '◇'

  return (
    <div
      onClick={onSelect}
      className={`flex items-start gap-4 rounded-lg border p-4 transition-all ${
        !agent.installed
          ? 'border-[var(--color-border)] opacity-50 cursor-not-allowed'
          : isSelected
            ? 'border-[var(--color-accent)] bg-[var(--color-accent-muted)] cursor-pointer'
            : 'border-[var(--color-border)] hover:border-[var(--color-text-muted)] cursor-pointer'
      }`}
    >
      {/* Icon */}
      <div className="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-lg bg-[var(--color-surface)] text-lg">
        {icon}
      </div>

      {/* Content */}
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="font-medium text-[var(--color-text)]">{agent.name}</span>
          {agent.installed && (
            <span className="flex items-center gap-1 rounded-full bg-[var(--color-accent-muted)] px-2 py-0.5 text-xs text-[var(--color-accent)]">
              <svg className="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={3}>
                <path strokeLinecap="round" strokeLinejoin="round" d="M5 13l4 4L19 7" />
              </svg>
              installed
            </span>
          )}
        </div>
        <p className="mt-0.5 text-sm text-[var(--color-text-muted)]">
          {getAgentDescription(agent.id)}
        </p>
        {!agent.installed && agent.installHint && (
          <p className="mt-2 font-mono text-xs text-[var(--color-text-muted)]">
            {agent.installHint}
          </p>
        )}
      </div>

      {/* Selection indicator */}
      <div className={`flex h-5 w-5 flex-shrink-0 items-center justify-center rounded-full border-2 transition-colors ${
        isSelected
          ? 'border-[var(--color-accent)] bg-[var(--color-accent)]'
          : 'border-[var(--color-border)]'
      }`}>
        {isSelected && (
          <svg className="h-3 w-3 text-white" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={3}>
            <path strokeLinecap="round" strokeLinejoin="round" d="M5 13l4 4L19 7" />
          </svg>
        )}
      </div>
    </div>
  )
}

function getAgentDescription(agentId: string): string {
  const descriptions: Record<string, string> = {
    'claude-code': 'Anthropic\'s Claude Code via ACP',
    opencode: 'Terminal-based coding assistant',
    codex: 'OpenAI\'s Codex CLI via ACP',
    gemini: 'Google\'s Gemini CLI agent',
  }
  return descriptions[agentId] || 'Coding assistant'
}
