/**
 * Settings component - simplified agent selector
 * Using Linear-style design: minimal UI, direct interactions
 */
import React, { useState, useEffect } from 'react'
import type {
  AgentCheckResult,
  InstallProgressEvent,
  InstallStep
} from '../../../shared/electron-api'
import { useTheme } from '../contexts/ThemeContext'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { Sun, Moon, Monitor, ChevronRight, ChevronDown, Loader2 } from 'lucide-react'
import { cn } from '@/lib/utils'

interface SettingsProps {
  isOpen: boolean
  onClose: () => void
  defaultAgentId: string
  onSetDefaultAgent: (agentId: string) => void
}

type ThemeMode = 'light' | 'dark' | 'system'

interface InstallStatus {
  agentId: string | null
  state: 'idle' | 'installing' | 'success' | 'error'
  currentStep?: InstallStep
  error?: string
}

export function Settings({
  isOpen,
  onClose,
  defaultAgentId,
  onSetDefaultAgent
}: SettingsProps): React.ReactElement {
  const [agents, setAgents] = useState<AgentCheckResult[]>([])
  const [loading, setLoading] = useState(true)
  const [installStatus, setInstallStatus] = useState<InstallStatus>({
    agentId: null,
    state: 'idle'
  })
  const { mode, setMode } = useTheme()

  useEffect(() => {
    if (isOpen) {
      loadAgents()
    }
  }, [isOpen])

  // Listen to install progress events
  useEffect(() => {
    const unsubscribe = window.electronAPI.onInstallProgress((event: InstallProgressEvent) => {
      if (event.status === 'error') {
        setInstallStatus({
          agentId: event.agentId,
          state: 'error',
          currentStep: event.step,
          error: event.error
        })
      } else if (event.status === 'completed' && isLastInstallStep(event.agentId, event.step)) {
        setInstallStatus({ agentId: event.agentId, state: 'success' })
        // Refresh agent list after installation
        loadAgents()
      } else {
        setInstallStatus({
          agentId: event.agentId,
          state: 'installing',
          currentStep: event.step
        })
      }
    })

    return unsubscribe
  }, [])

  async function loadAgents(): Promise<void> {
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

  // Direct selection - Linear style, no confirm button needed
  function handleSelectAgent(agentId: string): void {
    if (agentId !== defaultAgentId) {
      onSetDefaultAgent(agentId)
    }
  }

  // Handle agent installation
  async function handleInstall(agentId: string): Promise<void> {
    setInstallStatus({ agentId, state: 'installing' })
    try {
      const result = await window.electronAPI.installAgent(agentId)
      if (!result.success) {
        setInstallStatus({ agentId, state: 'error', error: result.error })
      }
    } catch (err) {
      setInstallStatus({
        agentId,
        state: 'error',
        error: err instanceof Error ? err.message : 'Unknown error'
      })
    }
  }

  return (
    <Dialog open={isOpen} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-5xl h-[85vh] max-h-[85vh] overflow-y-auto content-start">
        <DialogHeader>
          <DialogTitle>Settings</DialogTitle>
        </DialogHeader>

        {/* Appearance Section */}
        <div className="space-y-3">
          <h2 className="text-sm font-medium text-muted-foreground">Appearance</h2>
          <ToggleGroup
            type="single"
            value={mode}
            onValueChange={(value) => value && setMode(value as ThemeMode)}
          >
            <ToggleGroupItem value="light" className="gap-2">
              <Sun className="h-4 w-4" />
              Light
            </ToggleGroupItem>
            <ToggleGroupItem value="dark" className="gap-2">
              <Moon className="h-4 w-4" />
              Dark
            </ToggleGroupItem>
            <ToggleGroupItem value="system" className="gap-2">
              <Monitor className="h-4 w-4" />
              System
            </ToggleGroupItem>
          </ToggleGroup>
        </div>

        {/* Separator */}
        <div className="border-t" />

        {/* Agent Section */}
        <div className="space-y-3">
          <h2 className="text-sm font-medium text-muted-foreground">AI Agent</h2>

          {loading ? (
            <div className="flex items-center justify-center py-4">
              <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
            </div>
          ) : (
            <div className="space-y-1">
              {agents.map((agent) => (
                <AgentItem
                  key={agent.id}
                  agent={agent}
                  isSelected={agent.id === defaultAgentId}
                  onSelect={handleSelectAgent}
                  installStatus={installStatus}
                  onInstall={handleInstall}
                />
              ))}
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}

interface AgentItemProps {
  agent: AgentCheckResult
  isSelected: boolean
  onSelect: (agentId: string) => void
  installStatus: InstallStatus
  onInstall: (agentId: string) => void
}

function AgentItem({
  agent,
  isSelected,
  onSelect,
  installStatus,
  onInstall
}: AgentItemProps): React.ReactElement {
  const [expanded, setExpanded] = useState(false)

  const isInstalling = installStatus.agentId === agent.id && installStatus.state === 'installing'
  const hasInstallError = installStatus.agentId === agent.id && installStatus.state === 'error'
  const canInstall = ['claude-code', 'opencode', 'codex'].includes(agent.id)

  const status = !agent.installed ? 'setup' : isSelected ? 'selected' : 'ready'

  // Auto-expand when installing
  useEffect(() => {
    if (isInstalling) {
      setExpanded(true)
    }
  }, [isInstalling])

  // Click row to expand/collapse only
  const handleRowClick = (): void => {
    setExpanded(!expanded)
  }

  const handleSelectClick = (e: React.MouseEvent): void => {
    e.stopPropagation()
    onSelect(agent.id)
  }

  return (
    <div
      className={cn(
        'rounded-md transition-colors duration-150 text-secondary-foreground',
        status === 'selected'
          ? 'bg-muted text-foreground'
          : 'hover:bg-muted/50 hover:text-foreground',
        status === 'setup' && 'opacity-60 hover:opacity-100'
      )}
    >
      {/* Main row - click to expand */}
      <div className="flex items-center gap-2 px-3 py-2 cursor-pointer" onClick={handleRowClick}>
        <span className="p-0.5 text-muted-foreground">
          {expanded ? <ChevronDown className="h-4 w-4" /> : <ChevronRight className="h-4 w-4" />}
        </span>

        <span className="flex-1 font-medium text-sm">{agent.name}</span>

        {/* Right side: status or button */}
        {status === 'selected' ? (
          <span className="text-xs text-green-600">Selected</span>
        ) : status === 'ready' ? (
          <button
            onClick={handleSelectClick}
            className="text-xs px-2 py-0.5 rounded bg-primary/10 text-primary hover:bg-primary/20 transition-colors"
          >
            Use
          </button>
        ) : canInstall ? (
          <button
            onClick={(e) => {
              e.stopPropagation()
              onInstall(agent.id)
            }}
            disabled={isInstalling}
            className={cn(
              'text-xs px-2 py-0.5 rounded transition-colors flex items-center gap-1',
              isInstalling
                ? 'bg-muted text-muted-foreground cursor-not-allowed'
                : 'bg-primary/10 text-primary hover:bg-primary/20'
            )}
          >
            {isInstalling && <Loader2 className="h-3 w-3 animate-spin" />}
            {isInstalling ? 'Installing...' : 'Install'}
          </button>
        ) : (
          <span className="text-xs text-muted-foreground">Setup required</span>
        )}
      </div>

      {/* Expanded content */}
      {expanded && (
        <div className="pl-9 pr-3 pb-2 text-sm text-muted-foreground">
          {status === 'setup' ? (
            hasInstallError ? (
              <p className="text-xs text-destructive">Installation failed: {installStatus.error}</p>
            ) : isInstalling ? (
              <p className="text-xs">{getStepDescription(installStatus.currentStep, agent.id)}</p>
            ) : canInstall ? (
              <p className="text-xs">Click Install to set up {agent.name} automatically.</p>
            ) : agent.installHint ? (
              <p className="text-xs">
                To install, run in Terminal:{' '}
                <code className="font-mono bg-muted px-1 py-0.5 rounded">{agent.installHint}</code>
              </p>
            ) : null
          ) : (
            <p className="text-xs">{getAgentDescription(agent.id)}</p>
          )}
        </div>
      )}
    </div>
  )
}

function getAgentDescription(agentId: string): string {
  const descriptions: Record<string, string> = {
    'claude-code': 'Best for complex reasoning tasks. By Anthropic.',
    opencode: 'Fast and lightweight. Open source.',
    codex: "OpenAI's code assistant with ACP support.",
    gemini: "Google's AI assistant. By Google."
  }
  return descriptions[agentId] || 'AI coding assistant'
}

function isLastInstallStep(agentId: string, step: InstallStep): boolean {
  if (agentId === 'claude-code' || agentId === 'codex') {
    return step === 'install-acp'
  }
  // opencode and others: install-cli is the last step
  return step === 'install-cli'
}

function getStepDescription(step?: InstallStep, agentId?: string): string {
  switch (step) {
    case 'check-npm':
      return 'Checking npm installation...'
    case 'install-cli':
      if (agentId === 'opencode') {
        return 'Installing opencode...'
      }
      if (agentId === 'codex') {
        return 'Installing Codex CLI...'
      }
      return 'Installing Claude Code CLI...'
    case 'install-acp':
      if (agentId === 'codex') {
        return 'Installing codex-acp...'
      }
      return 'Installing claude-code-acp...'
    default:
      return 'Preparing installation...'
  }
}
