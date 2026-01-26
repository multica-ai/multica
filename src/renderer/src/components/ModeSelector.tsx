/**
 * Mode selector dropdown for MessageInput
 * Shows available session modes from the ACP server
 * Filters modes and provides semantic UI differentiation
 */
import { useEffect, useMemo, useState } from 'react'
import { ChevronDown, Check, Shield, Circle, Zap, PenLine, type LucideIcon } from 'lucide-react'
import type { SessionModeState, SessionModeId } from '../../../shared/types'
import {
  filterVisibleModes,
  getSemanticType,
  getNextModeId,
  getModeDisplayName,
  type SemanticType
} from '../../../shared/mode-semantic'
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem
} from '@/components/ui/dropdown-menu'
import { Tooltip, TooltipTrigger, TooltipContent } from '@/components/ui/tooltip'
import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'

/**
 * Semantic indicator config
 */
interface SemanticIndicator {
  icon: LucideIcon
  color: string // text color class
  label: string
}

/**
 * Get indicator style for semantic type
 * - plan: green with pen icon
 * - readonly: green with shield icon
 * - auto: purple with zap icon
 * - default: gray with circle icon (hidden in trigger)
 */
function getSemanticIndicator(semantic: SemanticType): SemanticIndicator {
  switch (semantic) {
    case 'plan':
      return { icon: PenLine, color: 'text-emerald-500', label: 'Plan' }
    case 'readonly':
      return { icon: Shield, color: 'text-emerald-500', label: 'Safe' }
    case 'auto':
      return { icon: Zap, color: 'text-violet-500', label: 'Auto' }
    case 'default':
    default:
      return { icon: Circle, color: 'text-zinc-400', label: 'Default' }
  }
}

interface ModeSelectorProps {
  modeState: SessionModeState | null
  onModeChange: (modeId: SessionModeId) => void
  disabled?: boolean
  isInitializing?: boolean
  onSelectionComplete?: () => void
}

export function ModeSelector({
  modeState,
  onModeChange,
  disabled = false,
  isInitializing = false,
  onSelectionComplete
}: ModeSelectorProps): React.JSX.Element | null {
  const [open, setOpen] = useState(false)
  const [isHovered, setIsHovered] = useState(false)

  // Filter to show only user-facing modes (must be before early returns for hooks rules)
  const visibleModes = useMemo(
    () => (modeState ? filterVisibleModes(modeState.availableModes) : []),
    [modeState]
  )

  // Global Shift+Tab keyboard shortcut to cycle through modes
  useEffect(() => {
    if (!modeState || disabled) return

    const handleKeyDown = (e: KeyboardEvent): void => {
      if (e.key === 'Tab' && e.shiftKey) {
        e.preventDefault()
        const nextModeId = getNextModeId(modeState.availableModes, modeState.currentModeId)
        if (nextModeId) {
          onModeChange(nextModeId)
        }
      }
    }

    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [modeState, disabled, onModeChange])

  // Show skeleton during initialization
  if (isInitializing) {
    return (
      <div className="flex items-center gap-1.5 px-2 py-1">
        <Skeleton className="h-3.5 w-16" />
      </div>
    )
  }

  // Don't render if no mode state (agent doesn't support modes)
  if (!modeState) {
    return null
  }

  const currentMode = modeState.availableModes.find((m) => m.id === modeState.currentModeId)
  // Use displayName from MODE_CONFIG for consistency with Settings
  const currentModeName = getModeDisplayName(modeState.currentModeId)
  const currentSemantic = getSemanticType(modeState.currentModeId)
  const currentIndicator = getSemanticIndicator(currentSemantic)

  function handleSelect(modeId: SessionModeId): void {
    if (modeId !== modeState?.currentModeId) {
      onModeChange(modeId)
    }
    setOpen(false)
    onSelectionComplete?.()
  }

  return (
    <DropdownMenu open={open} onOpenChange={setOpen}>
      <Tooltip open={isHovered && !open}>
        <TooltipTrigger asChild>
          <DropdownMenuTrigger asChild disabled={disabled}>
            <button
              onMouseEnter={() => setIsHovered(true)}
              onMouseLeave={() => setIsHovered(false)}
              className={cn(
                'flex items-center gap-1.5 text-xs text-muted-foreground transition-colors px-2 py-1 rounded-md',
                'hover:bg-accent hover:text-accent-foreground',
                'data-[state=open]:bg-accent data-[state=open]:text-accent-foreground',
                'outline-none focus-visible:ring-1 focus-visible:ring-ring',
                disabled && 'opacity-50 cursor-not-allowed'
              )}
            >
              {currentSemantic !== 'default' && (
                <currentIndicator.icon
                  className={cn('h-3 w-3 shrink-0', currentIndicator.color)}
                  strokeWidth={2}
                />
              )}
              <span className="max-w-[120px] truncate">{currentModeName}</span>
              <ChevronDown className="h-2.5 w-2.5 opacity-50" />
            </button>
          </DropdownMenuTrigger>
        </TooltipTrigger>
        <TooltipContent side="top">
          {currentMode?.description && <div>{currentMode.description}</div>}
          <div className="text-xs text-muted-foreground">Shift+Tab to switch modes</div>
        </TooltipContent>
      </Tooltip>
      <DropdownMenuContent
        side="top"
        align="start"
        className="min-w-[200px] max-h-[280px] overflow-y-auto p-1 data-[state=open]:animate-none data-[state=closed]:animate-none"
        onCloseAutoFocus={(e) => e.preventDefault()}
      >
        {visibleModes.map((mode) => {
          const isSelected = mode.id === modeState.currentModeId
          const semantic = getSemanticType(mode.id)
          const indicator = getSemanticIndicator(semantic)
          const Icon = indicator.icon

          return (
            <DropdownMenuItem
              key={mode.id}
              onClick={() => handleSelect(mode.id)}
              className="flex items-start justify-between gap-3 py-1.5 px-2 cursor-pointer"
            >
              <div className="flex items-start gap-2 min-w-0 flex-1">
                <Icon
                  className={cn('h-3.5 w-3.5 mt-0.5 shrink-0', indicator.color)}
                  strokeWidth={2}
                />
                <div className="flex flex-col min-w-0">
                  <span
                    className={cn(
                      'text-sm leading-tight',
                      isSelected ? 'text-foreground font-medium' : 'text-foreground'
                    )}
                  >
                    {getModeDisplayName(mode.id)}
                  </span>
                  {mode.description && (
                    <span className="text-xs text-muted-foreground/80 leading-tight mt-0.5 line-clamp-2">
                      {mode.description}
                    </span>
                  )}
                </div>
              </div>
              {isSelected && <Check className="h-3 w-3 text-primary/60 shrink-0 mt-0.5" />}
            </DropdownMenuItem>
          )
        })}
        {/* Keyboard shortcut hint */}
        <div className="px-2 py-1.5 text-xs text-muted-foreground/60 border-t border-border/50 mt-1">
          Shift+Tab to switch modes
        </div>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
