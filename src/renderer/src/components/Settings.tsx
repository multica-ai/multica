/**
 * Settings component - simplified agent selector
 * Using Linear-style design: minimal UI, direct interactions
 */
import React, { useState, useEffect, useRef, useCallback } from 'react'
import type { AgentCheckResult } from '../../../shared/electron-api'
import { useTheme } from '../contexts/ThemeContext'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { Sun, Moon, Monitor, ChevronRight, ChevronDown, Loader2 } from 'lucide-react'
import { cn } from '@/lib/utils'

// Agent icons
import claudeIcon from '../assets/agents/claude-color.svg'
import openaiIcon from '../assets/agents/openai.svg'
import opencodeIcon from '../assets/agents/opencode.png'

const AGENT_ICONS: Record<string, string> = {
  'claude-code': claudeIcon,
  codex: openaiIcon,
  opencode: opencodeIcon
}

// Icons that need dark mode inversion (monochrome black icons)
const INVERT_IN_DARK = new Set(['codex'])

interface SettingsProps {
  isOpen: boolean
  onClose: () => void
  defaultAgentId: string
  onSetDefaultAgent: (agentId: string) => void
  highlightAgent?: string // Agent to highlight (when opened due to missing dependency)
}

type ThemeMode = 'light' | 'dark' | 'system'

interface InstallStatus {
  agentId: string | null
  state: 'idle' | 'waiting' | 'success' | 'error'
  error?: string
}

// Static agent list - always visible
const AGENT_LIST = [
  { id: 'claude-code', name: 'Claude Code' },
  { id: 'opencode', name: 'opencode' },
  { id: 'codex', name: 'Codex CLI (ACP)' }
]

// Polling interval for checking installation status (ms)
const INSTALL_CHECK_INTERVAL = 2500

export function Settings({
  isOpen,
  onClose,
  defaultAgentId,
  onSetDefaultAgent,
  highlightAgent
}: SettingsProps): React.ReactElement {
  const [agentResults, setAgentResults] = useState<Map<string, AgentCheckResult>>(new Map())
  const [checkingAgents, setCheckingAgents] = useState<Set<string>>(new Set())
  const [installStatus, setInstallStatus] = useState<InstallStatus>({
    agentId: null,
    state: 'idle'
  })
  const { mode, setMode } = useTheme()
  const pollingIntervalRef = useRef<ReturnType<typeof setInterval> | null>(null)

  useEffect((): void => {
    if (isOpen) {
      loadAgents()
    }
  }, [isOpen])

  // Cleanup polling on unmount or when dialog closes
  useEffect((): (() => void) => {
    return (): void => {
      if (pollingIntervalRef.current) {
        clearInterval(pollingIntervalRef.current)
        pollingIntervalRef.current = null
      }
    }
  }, [])

  // Stop polling when dialog closes
  useEffect((): void => {
    if (!isOpen && pollingIntervalRef.current) {
      clearInterval(pollingIntervalRef.current)
      pollingIntervalRef.current = null
      // Reset install status when closing
      setInstallStatus({ agentId: null, state: 'idle' })
    }
  }, [isOpen])

  // Polling logic for checking installation
  const startPolling = useCallback(
    (agentId: string): void => {
      // Clear any existing polling
      if (pollingIntervalRef.current) {
        clearInterval(pollingIntervalRef.current)
      }

      pollingIntervalRef.current = setInterval(async (): Promise<void> => {
        try {
          const result = await window.electronAPI.checkAgent(agentId)
          if (result?.installed) {
            // Installation detected! Stop polling and update state
            if (pollingIntervalRef.current) {
              clearInterval(pollingIntervalRef.current)
              pollingIntervalRef.current = null
            }
            setAgentResults(
              (prev): Map<string, AgentCheckResult> => new Map(prev).set(agentId, result)
            )
            setInstallStatus({ agentId, state: 'success' })

            // Clear success state after a moment
            setTimeout((): void => {
              setInstallStatus({ agentId: null, state: 'idle' })
            }, 2000)
          }
        } catch (err) {
          console.error(`Failed to check agent ${agentId}:`, err)
        }
      }, INSTALL_CHECK_INTERVAL)
    },
    [setAgentResults, setInstallStatus]
  )

  // Load all agents concurrently
  async function loadAgents(): Promise<void> {
    const allIds = AGENT_LIST.map((a) => a.id)
    setCheckingAgents(new Set(allIds))

    // Check all agents concurrently
    await Promise.all(
      allIds.map(async (id): Promise<void> => {
        try {
          const result = await window.electronAPI.checkAgent(id)
          if (result) {
            setAgentResults((prev): Map<string, AgentCheckResult> => new Map(prev).set(id, result))
          }
        } catch (err) {
          console.error(`Failed to check agent ${id}:`, err)
        } finally {
          setCheckingAgents((prev): Set<string> => {
            const next = new Set(prev)
            next.delete(id)
            return next
          })
        }
      })
    )
  }

  // Direct selection - Linear style, no confirm button needed
  function handleSelectAgent(agentId: string): void {
    if (agentId !== defaultAgentId) {
      onSetDefaultAgent(agentId)
    }
  }

  // Handle agent installation - opens Terminal
  async function handleInstall(agentId: string): Promise<void> {
    // Set waiting state immediately to prevent double-clicks
    setInstallStatus({ agentId, state: 'waiting' })

    try {
      const result = await window.electronAPI.installAgent(agentId)
      if (result.success) {
        // Terminal opened successfully, start polling for installation
        startPolling(agentId)
      } else {
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

  // Cancel waiting/polling for a specific agent
  function handleCancelWaiting(): void {
    if (pollingIntervalRef.current) {
      clearInterval(pollingIntervalRef.current)
      pollingIntervalRef.current = null
    }
    setInstallStatus({ agentId: null, state: 'idle' })
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
            onValueChange={(value): void => {
              value && setMode(value as ThemeMode)
            }}
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

          {/* Missing dependency prompt */}
          {highlightAgent && (
            <div className="rounded-md bg-amber-500/10 border border-amber-500/20 px-3 py-2 text-sm text-amber-600 dark:text-amber-400">
              To start a conversation, please install at least one AI agent below.
            </div>
          )}

          <div className="space-y-1">
            {AGENT_LIST.map(({ id, name }): React.ReactElement => {
              const agent = agentResults.get(id)
              const isChecking = checkingAgents.has(id)
              return (
                <AgentItem
                  key={id}
                  agentId={id}
                  agentName={name}
                  agent={agent}
                  isChecking={isChecking}
                  isSelected={id === defaultAgentId}
                  onSelect={handleSelectAgent}
                  installStatus={installStatus}
                  onInstall={handleInstall}
                  onCancelWaiting={handleCancelWaiting}
                  isHighlighted={id === highlightAgent}
                />
              )
            })}
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}

interface AgentItemProps {
  agentId: string
  agentName: string
  agent?: AgentCheckResult
  isChecking: boolean
  isSelected: boolean
  onSelect: (agentId: string) => void
  installStatus: InstallStatus
  onInstall: (agentId: string) => void
  onCancelWaiting: () => void
  isHighlighted?: boolean // When true, auto-expand and highlight this agent
}

function AgentItem({
  agentId,
  agentName,
  agent,
  isChecking,
  isSelected,
  onSelect,
  installStatus,
  onInstall,
  onCancelWaiting,
  isHighlighted
}: AgentItemProps): React.ReactElement {
  const [expanded, setExpanded] = useState(false)

  // Auto-expand when highlighted
  useEffect((): void => {
    if (isHighlighted) {
      // eslint-disable-next-line react-hooks/set-state-in-effect -- Intentional: sync UI state with prop
      setExpanded(true)
    }
  }, [isHighlighted])

  const isWaiting = installStatus.agentId === agentId && installStatus.state === 'waiting'
  const hasInstallError = installStatus.agentId === agentId && installStatus.state === 'error'
  const hasInstallSuccess = installStatus.agentId === agentId && installStatus.state === 'success'
  const canInstall = ['claude-code', 'opencode', 'codex'].includes(agentId)

  // Determine status: checking -> setup/selected/ready
  const status = isChecking
    ? 'checking'
    : agent === undefined
      ? 'checking' // Still loading agent status
      : !agent?.installed
        ? 'setup'
        : isSelected
          ? 'selected'
          : 'ready'

  // Auto-expand when waiting for install
  useEffect((): void => {
    if (isWaiting) {
      // eslint-disable-next-line react-hooks/set-state-in-effect -- Intentional: sync UI state with install status
      setExpanded(true)
    }
  }, [isWaiting])

  // Click row to expand/collapse only
  const handleRowClick = (): void => {
    setExpanded(!expanded)
  }

  const handleSelectClick = (e: React.MouseEvent): void => {
    e.stopPropagation()
    onSelect(agentId)
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

        {AGENT_ICONS[agentId] && (
          <img
            src={AGENT_ICONS[agentId]}
            alt=""
            className={cn('h-4 w-4', INVERT_IN_DARK.has(agentId) && 'dark:invert')}
          />
        )}

        <span className="flex-1 font-medium text-sm">{agentName}</span>

        {/* Right side: status or button */}
        {status === 'checking' ? (
          <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
        ) : status === 'selected' ? (
          <span className="text-xs text-green-600">Selected</span>
        ) : status === 'ready' ? (
          <button
            onClick={handleSelectClick}
            className="text-xs px-2 py-0.5 rounded bg-primary/10 text-primary hover:bg-primary/20 transition-colors"
          >
            Use
          </button>
        ) : hasInstallSuccess ? (
          <span className="text-xs text-green-600">Installed</span>
        ) : isWaiting ? (
          <span className="text-xs text-muted-foreground flex items-center gap-1">
            <Loader2 className="h-3 w-3 animate-spin" />
            Waiting...
          </span>
        ) : canInstall ? (
          <button
            onClick={(e: React.MouseEvent): void => {
              e.stopPropagation()
              onInstall(agentId)
            }}
            className="text-xs px-2 py-0.5 rounded transition-colors bg-primary/10 text-primary hover:bg-primary/20"
          >
            Install
          </button>
        ) : (
          <span className="text-xs text-muted-foreground">Setup required</span>
        )}
      </div>

      {/* Expanded content */}
      {expanded && (
        <div className="pl-9 pr-3 pb-2 text-sm text-muted-foreground">
          {status === 'checking' ? (
            <p className="text-xs">Checking installation status...</p>
          ) : status === 'setup' ? (
            hasInstallError ? (
              <p className="text-xs text-destructive">
                Failed to open Terminal: {installStatus.error}
              </p>
            ) : hasInstallSuccess ? (
              <p className="text-xs text-green-600">Installation detected!</p>
            ) : isWaiting ? (
              <div className="space-y-2">
                <p className="text-xs">
                  Terminal opened. Please complete the installation in the terminal window.
                </p>
                <p className="text-xs text-muted-foreground/70">
                  This will automatically detect when installation is complete.
                </p>
                <button
                  onClick={(e: React.MouseEvent): void => {
                    e.stopPropagation()
                    onCancelWaiting()
                  }}
                  className="text-xs px-2 py-0.5 rounded transition-colors bg-muted text-muted-foreground hover:bg-muted/80"
                >
                  Cancel
                </button>
              </div>
            ) : canInstall ? (
              <p className="text-xs">
                Click Install to open Terminal with the installation command.
              </p>
            ) : agent?.installHint ? (
              <p className="text-xs">
                To install, run in Terminal:{' '}
                <code className="font-mono bg-muted px-1 py-0.5 rounded">{agent.installHint}</code>
              </p>
            ) : null
          ) : (
            <div className="space-y-1">
              <p className="text-xs">{getAgentDescription(agentId)}</p>
              {agent?.commands && agent.commands.length > 0 && (
                <div className="mt-2 space-y-0.5">
                  {agent.commands.map(
                    (cmd): React.ReactElement => (
                      <div key={cmd.command} className="text-xs font-mono text-muted-foreground/70">
                        <span className="text-muted-foreground">{cmd.command}:</span>{' '}
                        {cmd.path || <span className="italic">not installed</span>}
                      </div>
                    )
                  )}
                </div>
              )}
            </div>
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
