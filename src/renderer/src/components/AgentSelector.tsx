/**
 * Agent selector dropdown for MessageInput
 */
import { useState, useEffect } from 'react'
import { ChevronDown, Loader2 } from 'lucide-react'
import type { AgentCheckResult } from '../../../shared/electron-api'

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
  DropdownMenuItem
} from '@/components/ui/dropdown-menu'
import { Tooltip, TooltipTrigger, TooltipContent } from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'

interface AgentSelectorProps {
  currentAgentId: string
  onAgentChange: (agentId: string) => void
  disabled?: boolean
  isSwitching?: boolean
}

export function AgentSelector({
  currentAgentId,
  onAgentChange,
  disabled = false,
  isSwitching = false
}: AgentSelectorProps) {
  const [agents, setAgents] = useState<AgentCheckResult[]>([])
  const [loading, setLoading] = useState(true)
  const [open, setOpen] = useState(false)

  useEffect(() => {
    loadAgents()
  }, [])

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

  const currentAgent = agents.find((a) => a.id === currentAgentId)
  const currentAgentName = currentAgent?.name || currentAgentId

  function handleSelect(agentId: string) {
    if (agentId !== currentAgentId) {
      onAgentChange(agentId)
    }
    setOpen(false)
  }

  const isLoading = loading || isSwitching

  return (
    <DropdownMenu open={open} onOpenChange={setOpen}>
      <DropdownMenuTrigger asChild disabled={disabled || isLoading}>
        <button
          className={cn(
            'flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors px-2 py-1 rounded-md hover:bg-background/50',
            (disabled || isLoading) && 'opacity-50 cursor-not-allowed'
          )}
        >
          {AGENT_ICONS[currentAgentId] ? (
            <img
              src={AGENT_ICONS[currentAgentId]}
              alt=""
              className={cn('h-3.5 w-3.5', INVERT_IN_DARK.has(currentAgentId) && 'dark:invert')}
            />
          ) : (
            <span className="h-3.5 w-3.5" />
          )}
          <span className="max-w-[100px] truncate">{currentAgentName}</span>
          {isLoading ? (
            <Loader2 className="h-3 w-3 animate-spin" />
          ) : (
            <ChevronDown className="h-3 w-3" />
          )}
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent side="top" align="start" className="min-w-[140px]">
        {agents.map((agent) => {
          const isSelected = agent.id === currentAgentId
          const isDisabled = !agent.installed

          const icon = AGENT_ICONS[agent.id]

          const needsInvert = INVERT_IN_DARK.has(agent.id)

          if (isDisabled) {
            return (
              <Tooltip key={agent.id}>
                <TooltipTrigger asChild>
                  <div>
                    <DropdownMenuItem disabled className="flex items-center justify-between gap-2">
                      <div className="flex items-center gap-2">
                        {icon && (
                          <img
                            src={icon}
                            alt=""
                            className={cn('h-4 w-4', needsInvert && 'dark:invert')}
                          />
                        )}
                        <span>{agent.name}</span>
                      </div>
                      <span className="h-2 w-2 rounded-full bg-muted" />
                    </DropdownMenuItem>
                  </div>
                </TooltipTrigger>
                <TooltipContent side="right">Setup required in Settings</TooltipContent>
              </Tooltip>
            )
          }

          return (
            <DropdownMenuItem
              key={agent.id}
              onClick={() => handleSelect(agent.id)}
              className="flex items-center justify-between gap-2"
            >
              <div className="flex items-center gap-2">
                {icon && (
                  <img src={icon} alt="" className={cn('h-4 w-4', needsInvert && 'dark:invert')} />
                )}
                <span>{agent.name}</span>
              </div>
              <span
                className={cn(
                  'h-2 w-2 rounded-full',
                  isSelected ? 'bg-green-500' : 'bg-green-500/50'
                )}
              />
            </DropdownMenuItem>
          )
        })}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
