/**
 * Mode selector dropdown for MessageInput
 * Shows available session modes from the ACP server
 */
import { useState } from 'react'
import { ChevronDown, Check } from 'lucide-react'
import type { SessionModeState, SessionModeId } from '../../../shared/types'
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem
} from '@/components/ui/dropdown-menu'
import { Tooltip, TooltipTrigger, TooltipContent } from '@/components/ui/tooltip'
import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'

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
  const currentModeName = currentMode?.name || modeState.currentModeId

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
              <span className="max-w-[140px] truncate">{currentModeName}</span>
              <ChevronDown className="h-3 w-3" />
            </button>
          </DropdownMenuTrigger>
        </TooltipTrigger>
        <TooltipContent side="top">Select mode</TooltipContent>
      </Tooltip>
      <DropdownMenuContent
        side="top"
        align="start"
        className="min-w-[160px] max-h-[300px] overflow-y-auto p-1.5 data-[state=open]:animate-none data-[state=closed]:animate-none"
        onCloseAutoFocus={(e) => e.preventDefault()}
      >
        {modeState.availableModes.map((mode) => {
          const isSelected = mode.id === modeState.currentModeId

          return (
            <DropdownMenuItem
              key={mode.id}
              onClick={() => handleSelect(mode.id)}
              className="flex items-center justify-between gap-6 py-1.5"
            >
              <span
                className={cn(
                  'text-sm transition-colors',
                  isSelected ? 'text-foreground' : 'text-muted-foreground hover:text-foreground'
                )}
              >
                {mode.name}
              </span>
              {isSelected && <Check className="h-3.5 w-3.5 text-primary/70" />}
            </DropdownMenuItem>
          )
        })}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
