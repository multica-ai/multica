/**
 * Combined Agent and Model selector with vertical layout
 * Shows Agent icon + Model name, with vertical list for model selection
 */
import { useState, useEffect } from 'react'
import { ChevronDown, Loader2, Check } from 'lucide-react'
import type { AgentCheckResult } from '../../../shared/electron-api'
import type { SessionModelState, ModelId } from '../../../shared/types'

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

import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator
} from '@/components/ui/dropdown-menu'
import { Tooltip, TooltipTrigger, TooltipContent } from '@/components/ui/tooltip'
import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'

interface AgentModelSelectorProps {
  currentAgentId: string
  onAgentChange: (agentId: string) => void
  modelState: SessionModelState | null
  onModelChange: (modelId: ModelId) => void
  disabled?: boolean
  isSwitching?: boolean
  isInitializing?: boolean
}

export function AgentModelSelector({
  currentAgentId,
  onAgentChange,
  modelState,
  onModelChange,
  disabled = false,
  isSwitching = false,
  isInitializing = false
}: AgentModelSelectorProps): React.JSX.Element {
  const [agents, setAgents] = useState<AgentCheckResult[]>([])
  const [loading, setLoading] = useState(true)
  const [open, setOpen] = useState(false)
  const [isHovered, setIsHovered] = useState(false)

  useEffect(() => {
    loadAgents()
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

  const currentAgent = agents.find((a) => a.id === currentAgentId)
  const currentAgentName = currentAgent?.name || currentAgentId
  const otherAgents = agents.filter((a) => a.id !== currentAgentId)

  // Get current model name (or fall back to agent name)
  const currentModel = modelState?.availableModels.find(
    (m) => m.modelId === modelState.currentModelId
  )
  const displayName = currentModel?.name || currentAgentName

  function handleAgentSelect(agentId: string): void {
    if (agentId !== currentAgentId) {
      onAgentChange(agentId)
    }
    setOpen(false)
  }

  function handleModelSelect(modelId: ModelId): void {
    if (modelId !== modelState?.currentModelId) {
      onModelChange(modelId)
    }
    setOpen(false)
  }

  // Show skeleton during initialization
  if (isInitializing) {
    return (
      <div className="flex items-center gap-1.5 px-2 py-1">
        <Skeleton className="h-3.5 w-3.5 rounded-full" />
        <Skeleton className="h-3.5 w-20" />
      </div>
    )
  }

  const currentAgentIcon = AGENT_ICONS[currentAgentId]
  const currentAgentNeedsInvert = INVERT_IN_DARK.has(currentAgentId)

  return (
    <DropdownMenu open={open} onOpenChange={setOpen}>
      <Tooltip open={isHovered && !open}>
        <TooltipTrigger asChild>
          <DropdownMenuTrigger asChild disabled={disabled || loading || isSwitching}>
            <button
              onMouseEnter={() => setIsHovered(true)}
              onMouseLeave={() => setIsHovered(false)}
              className={cn(
                'flex items-center gap-1.5 text-xs text-muted-foreground transition-colors px-2 py-1 rounded-md',
                'hover:bg-accent hover:text-accent-foreground',
                'data-[state=open]:bg-accent data-[state=open]:text-accent-foreground',
                (disabled || loading || isSwitching) && 'opacity-50 cursor-not-allowed'
              )}
            >
              {isSwitching ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : currentAgentIcon ? (
                <img
                  src={currentAgentIcon}
                  alt={currentAgentName}
                  className={cn('h-3.5 w-3.5', currentAgentNeedsInvert && 'dark:invert')}
                />
              ) : (
                <span className="h-3.5 w-3.5" />
              )}
              <span className="max-w-24 truncate">
                {isSwitching ? 'Switching...' : displayName}
              </span>
              <ChevronDown className="h-3 w-3" />
            </button>
          </DropdownMenuTrigger>
        </TooltipTrigger>
        <TooltipContent side="top">Select agent and model</TooltipContent>
      </Tooltip>
      <DropdownMenuContent side="top" align="start" className="min-w-[220px] p-1.5">
        {/* Current Agent Header */}
        <div className="flex items-center justify-between gap-3 px-2 py-2">
          <div className="flex items-center gap-2">
            {currentAgentIcon && (
              <img
                src={currentAgentIcon}
                alt={currentAgentName}
                className={cn('h-4 w-4', currentAgentNeedsInvert && 'dark:invert')}
              />
            )}
            <span className="text-sm">{currentAgentName}</span>
          </div>
          <span className="text-xs text-green-600/80">Active</span>
        </div>

        {/* Current Agent's Models */}
        {modelState && modelState.availableModels.length > 0 ? (
          modelState.availableModels.map((model) => {
            const isSelectedModel = model.modelId === modelState.currentModelId
            return (
              <DropdownMenuItem
                key={model.modelId}
                onClick={() => handleModelSelect(model.modelId)}
                className="flex items-center justify-between gap-6 pl-8 py-1.5"
              >
                <span
                  className={cn(
                    'text-sm transition-colors',
                    isSelectedModel
                      ? 'text-foreground'
                      : 'text-muted-foreground hover:text-foreground'
                  )}
                >
                  {model.name}
                </span>
                {isSelectedModel && <Check className="h-3.5 w-3.5 text-primary/70" />}
              </DropdownMenuItem>
            )
          })
        ) : (
          <div className="pl-8 pr-2 py-2 text-xs text-muted-foreground/70">No models available</div>
        )}

        {/* Separator if there are other agents */}
        {otherAgents.length > 0 && <DropdownMenuSeparator />}

        {/* Other Agents */}
        {otherAgents.map((agent) => {
          const icon = AGENT_ICONS[agent.id]
          const needsInvert = INVERT_IN_DARK.has(agent.id)
          const isInstalled = agent.installed

          if (!isInstalled) {
            // Not installed - show disabled state
            return (
              <div
                key={agent.id}
                className="flex items-center justify-between gap-3 px-2 py-2 opacity-40"
              >
                <div className="flex items-center gap-2">
                  {icon && (
                    <img
                      src={icon}
                      alt={agent.name}
                      className={cn('h-4 w-4', needsInvert && 'dark:invert')}
                    />
                  )}
                  <span className="text-sm">{agent.name}</span>
                </div>
                <span className="text-xs text-muted-foreground">Setup required</span>
              </div>
            )
          }

          // Installed - show Switch button
          return (
            <div
              key={agent.id}
              role="button"
              tabIndex={0}
              className="flex items-center justify-between gap-3 px-2 py-2 hover:bg-accent/50 rounded-sm cursor-pointer transition-colors"
              onClick={() => handleAgentSelect(agent.id)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault()
                  handleAgentSelect(agent.id)
                }
              }}
            >
              <div className="flex items-center gap-2">
                {icon && (
                  <img
                    src={icon}
                    alt={agent.name}
                    className={cn('h-4 w-4', needsInvert && 'dark:invert')}
                  />
                )}
                <span className="text-sm">{agent.name}</span>
              </div>
              <span className="text-xs px-2 py-0.5 rounded bg-muted text-muted-foreground hover:text-foreground transition-colors">
                Switch
              </span>
            </div>
          )
        })}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
