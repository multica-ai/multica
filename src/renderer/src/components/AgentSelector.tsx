/**
 * Agent selector dropdown for MessageInput
 */
import { useState, useEffect } from 'react'
import { Sparkles, ChevronDown, Loader2 } from 'lucide-react'
import type { AgentCheckResult } from '../../../shared/electron-api'
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from '@/components/ui/dropdown-menu'
import { Tooltip, TooltipTrigger, TooltipContent } from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'

interface AgentSelectorProps {
  currentAgentId: string
  onAgentChange: (agentId: string) => void
  disabled?: boolean
}

export function AgentSelector({
  currentAgentId,
  onAgentChange,
  disabled = false,
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

  return (
    <DropdownMenu open={open} onOpenChange={setOpen}>
      <DropdownMenuTrigger asChild disabled={disabled || loading}>
        <button
          className={cn(
            "flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors px-2 py-1 rounded-md hover:bg-background/50",
            disabled && "opacity-50 cursor-not-allowed"
          )}
        >
          <Sparkles className="h-3.5 w-3.5" />
          <span className="max-w-[100px] truncate">{currentAgentName}</span>
          {loading ? (
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

          if (isDisabled) {
            return (
              <Tooltip key={agent.id}>
                <TooltipTrigger asChild>
                  <div>
                    <DropdownMenuItem
                      disabled
                      className="flex items-center justify-between"
                    >
                      <span>{agent.name}</span>
                      <span className="h-2 w-2 rounded-full bg-muted" />
                    </DropdownMenuItem>
                  </div>
                </TooltipTrigger>
                <TooltipContent side="right">
                  Setup required in Settings
                </TooltipContent>
              </Tooltip>
            )
          }

          return (
            <DropdownMenuItem
              key={agent.id}
              onClick={() => handleSelect(agent.id)}
              className="flex items-center justify-between"
            >
              <span>{agent.name}</span>
              <span
                className={cn(
                  "h-2 w-2 rounded-full",
                  isSelected ? "bg-green-500" : "bg-green-500/50"
                )}
              />
            </DropdownMenuItem>
          )
        })}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
