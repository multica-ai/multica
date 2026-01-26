/**
 * Combined Agent and Model selector with vertical layout
 * Shows Agent icon + Model name, with vertical list for model selection
 */
import { useState, useEffect, useMemo, useRef, useCallback } from 'react'
import { ChevronDown, Loader2, Check, Search } from 'lucide-react'
import type { SessionModelState, ModelId } from '../../../shared/types'
import { useAgentStore } from '../stores/agentStore'

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

// Minimum number of models to show search input
const MIN_MODELS_FOR_SEARCH = 5

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
  onSelectionComplete?: () => void
}

export function AgentModelSelector({
  currentAgentId,
  onAgentChange,
  modelState,
  onModelChange,
  disabled = false,
  isSwitching = false,
  isInitializing = false,
  onSelectionComplete
}: AgentModelSelectorProps): React.JSX.Element {
  // Use global agent store for shared state (syncs with Settings)
  const { getAllAgents, isLoading: loading, loadAgents, lastLoadedAt } = useAgentStore()
  const agents = getAllAgents()

  const [open, setOpen] = useState(false)
  const [isHovered, setIsHovered] = useState(false)
  const [searchQuery, setSearchQuery] = useState('')
  const [highlightedIndex, setHighlightedIndex] = useState(0)
  const listRef = useRef<HTMLDivElement>(null)

  // Load agents on mount if not already loaded
  useEffect(() => {
    if (!lastLoadedAt) {
      loadAgents()
    }
  }, [lastLoadedAt, loadAgents])

  const currentAgent = agents.find((a) => a.id === currentAgentId)
  const currentAgentName = currentAgent?.name || currentAgentId
  const otherAgents = agents.filter((a) => a.id !== currentAgentId)

  // Get current model name (or fall back to agent name)
  const currentModel = modelState?.availableModels.find(
    (m) => m.modelId === modelState.currentModelId
  )
  const displayName = currentModel?.name || currentAgentName

  // Show search when there are many models
  const showSearch = (modelState?.availableModels.length ?? 0) >= MIN_MODELS_FOR_SEARCH

  // Filter models based on search query
  const availableModels = modelState?.availableModels
  const filteredModels = useMemo(() => {
    if (!availableModels) return []
    if (!searchQuery.trim()) return availableModels
    const query = searchQuery.toLowerCase()
    return availableModels.filter((m) => m.name.toLowerCase().includes(query))
  }, [availableModels, searchQuery])

  // Reset search, highlight, and scroll position when dropdown opens
  // Use -1 to indicate no item is highlighted until user interacts
  useEffect(() => {
    if (open) {
      setSearchQuery('')
      setHighlightedIndex(-1)
      // Reset scroll position after DOM updates
      requestAnimationFrame(() => {
        if (listRef.current) {
          listRef.current.scrollTop = 0
        }
      })
    }
  }, [open])

  // Reset highlight when filtered results change
  useEffect(() => {
    setHighlightedIndex(-1)
  }, [filteredModels.length])

  const currentModelId = modelState?.currentModelId
  const handleAgentSelect = useCallback(
    (agentId: string): void => {
      if (agentId !== currentAgentId) {
        onAgentChange(agentId)
      }
      setOpen(false)
      onSelectionComplete?.()
    },
    [currentAgentId, onAgentChange, onSelectionComplete, setOpen]
  )

  const handleModelSelect = useCallback(
    (modelId: ModelId): void => {
      if (modelId !== currentModelId) {
        onModelChange(modelId)
      }
      setOpen(false)
      onSelectionComplete?.()
    },
    [currentModelId, onModelChange, onSelectionComplete, setOpen]
  )

  // Keyboard navigation handler for search input
  const handleSearchKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLInputElement>) => {
      const modelCount = filteredModels.length
      if (modelCount === 0) return

      switch (e.key) {
        case 'ArrowDown':
          e.preventDefault()
          // If nothing highlighted, start from first item; otherwise move down
          setHighlightedIndex((prev) => (prev < 0 ? 0 : (prev + 1) % modelCount))
          break
        case 'ArrowUp':
          e.preventDefault()
          // If nothing highlighted, start from last item; otherwise move up
          setHighlightedIndex((prev) =>
            prev < 0 ? modelCount - 1 : (prev - 1 + modelCount) % modelCount
          )
          break
        case 'Tab':
          e.preventDefault()
          if (e.shiftKey) {
            setHighlightedIndex((prev) =>
              prev < 0 ? modelCount - 1 : (prev - 1 + modelCount) % modelCount
            )
          } else {
            setHighlightedIndex((prev) => (prev < 0 ? 0 : (prev + 1) % modelCount))
          }
          break
        case 'Enter':
          e.preventDefault()
          // Only select if there's a valid highlighted item
          if (highlightedIndex >= 0 && filteredModels[highlightedIndex]) {
            handleModelSelect(filteredModels[highlightedIndex].modelId)
          }
          break
        case 'Escape':
          e.preventDefault()
          setOpen(false)
          break
      }
    },
    [filteredModels, highlightedIndex, handleModelSelect, setOpen]
  )

  // Scroll highlighted item into view (only when user actively highlights an item)
  useEffect(() => {
    if (!listRef.current || highlightedIndex < 0) return
    const items = listRef.current.querySelectorAll('[data-model-item]')
    const highlightedItem = items[highlightedIndex] as HTMLElement | undefined
    if (highlightedItem) {
      highlightedItem.scrollIntoView({ block: 'nearest' })
    }
  }, [highlightedIndex])

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
                'outline-none focus-visible:ring-1 focus-visible:ring-ring',
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
      <DropdownMenuContent
        side="top"
        align="start"
        className="min-w-[220px] p-0 data-[state=open]:animate-none data-[state=closed]:animate-none"
        onCloseAutoFocus={(e) => e.preventDefault()}
      >
        {/* Top section with padding: header + search */}
        <div className="px-1.5 pt-1.5">
          {/* Current Agent Header */}
          <div className="flex items-center justify-between gap-3 px-2 py-1.5">
            <div className="flex items-center gap-1.5">
              {currentAgentIcon && (
                <img
                  src={currentAgentIcon}
                  alt={currentAgentName}
                  className={cn('h-3.5 w-3.5', currentAgentNeedsInvert && 'dark:invert')}
                />
              )}
              <span className="text-xs text-muted-foreground">{currentAgentName}</span>
            </div>
            {currentAgent?.installed !== false ? (
              <span className="text-xs text-green-600/70">Active</span>
            ) : (
              <span className="text-xs text-amber-600">Not installed</span>
            )}
          </div>

          {/* Search input for many models */}
          {showSearch && (
            <div className="px-2 py-1.5">
              <div className="relative">
                <Search className="absolute left-2 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-muted-foreground/50" />
                <input
                  type="text"
                  autoFocus
                  value={searchQuery}
                  onChange={(e) => setSearchQuery(e.target.value)}
                  placeholder="Search models..."
                  className="w-full h-7 pl-7 pr-2 text-xs bg-muted/50 border-none rounded-md outline-none focus:ring-1 focus:ring-ring placeholder:text-muted-foreground/50"
                  onClick={(e) => e.stopPropagation()}
                  onKeyDown={handleSearchKeyDown}
                />
              </div>
            </div>
          )}
        </div>

        {/* Full-width scrollable section - scrollbar flush to edge */}
        {modelState && modelState.availableModels.length > 0 ? (
          <div ref={listRef} className="max-h-[200px] overflow-y-auto scrollbar-thin">
            {filteredModels.length > 0 ? (
              filteredModels.map((model, index) => {
                const isSelectedModel = model.modelId === modelState.currentModelId
                const isHighlighted = showSearch && index === highlightedIndex

                // When search is active, use plain div to avoid Radix's roving focus
                // which steals focus from the search input on hover
                if (showSearch) {
                  return (
                    <div
                      key={model.modelId}
                      data-model-item
                      role="option"
                      aria-selected={isSelectedModel}
                      onClick={() => handleModelSelect(model.modelId)}
                      onMouseEnter={() => setHighlightedIndex(index)}
                      className={cn(
                        'flex items-center gap-6 pl-6 pr-2 py-1.5 cursor-pointer text-sm rounded-sm',
                        'hover:bg-accent hover:text-accent-foreground',
                        isHighlighted && 'bg-accent text-accent-foreground'
                      )}
                    >
                      <span className="flex-1">{model.name}</span>
                      <span className="w-3.5 flex-shrink-0">
                        {isSelectedModel && <Check className="h-3.5 w-3.5 text-primary/70" />}
                      </span>
                    </div>
                  )
                }

                // Standard DropdownMenuItem when no search (preserves full accessibility)
                return (
                  <DropdownMenuItem
                    key={model.modelId}
                    data-model-item
                    onClick={() => handleModelSelect(model.modelId)}
                    className="flex items-center gap-6 pl-6 pr-2 py-1.5"
                  >
                    <span className="flex-1 text-sm">{model.name}</span>
                    <span className="w-3.5 flex-shrink-0">
                      {isSelectedModel && <Check className="h-3.5 w-3.5 text-primary/70" />}
                    </span>
                  </DropdownMenuItem>
                )
              })
            ) : (
              <div className="pl-6 pr-2 py-2 text-xs text-muted-foreground/70">
                No models match &ldquo;{searchQuery}&rdquo;
              </div>
            )}
          </div>
        ) : (
          <div className="px-1.5">
            <div className="pl-6 pr-2 py-2 text-xs text-muted-foreground/70">
              No models available
            </div>
          </div>
        )}

        {/* Bottom section: separator + other agents */}
        {otherAgents.length > 0 && (
          <>
            <DropdownMenuSeparator className="mx-1.5" />
            <div className="px-1.5 pb-1.5">
              {otherAgents.map((agent) => {
                const icon = AGENT_ICONS[agent.id]
                const needsInvert = INVERT_IN_DARK.has(agent.id)
                const isInstalled = agent.installed

                if (!isInstalled) {
                  // Not installed - show disabled state
                  return (
                    <div
                      key={agent.id}
                      className="flex items-center justify-between gap-3 px-2 py-1.5 opacity-40"
                    >
                      <div className="flex items-center gap-1.5">
                        {icon && (
                          <img
                            src={icon}
                            alt={agent.name}
                            className={cn('h-3.5 w-3.5', needsInvert && 'dark:invert')}
                          />
                        )}
                        <span className="text-xs text-muted-foreground">{agent.name}</span>
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
                    className="flex items-center justify-between gap-3 px-2 py-1.5 hover:bg-accent/50 rounded-sm cursor-pointer transition-colors"
                    onClick={() => handleAgentSelect(agent.id)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter' || e.key === ' ') {
                        e.preventDefault()
                        handleAgentSelect(agent.id)
                      }
                    }}
                  >
                    <div className="flex items-center gap-1.5">
                      {icon && (
                        <img
                          src={icon}
                          alt={agent.name}
                          className={cn('h-3.5 w-3.5', needsInvert && 'dark:invert')}
                        />
                      )}
                      <span className="text-xs text-muted-foreground">{agent.name}</span>
                    </div>
                    <span className="text-xs px-2 py-0.5 rounded bg-muted text-muted-foreground hover:text-foreground transition-colors">
                      Switch
                    </span>
                  </div>
                )
              })}
            </div>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
