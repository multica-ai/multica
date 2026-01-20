/**
 * Mode selector dropdown for MessageInput
 * Shows available session modes from the ACP server
 */
import { useState } from 'react'
import { ChevronDown } from 'lucide-react'
import type { SessionModeState, SessionModeId } from '../../../shared/types'
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem
} from '@/components/ui/dropdown-menu'
import { cn } from '@/lib/utils'

interface ModeSelectorProps {
  modeState: SessionModeState | null
  onModeChange: (modeId: SessionModeId) => void
  disabled?: boolean
}

export function ModeSelector({
  modeState,
  onModeChange,
  disabled = false
}: ModeSelectorProps): React.JSX.Element | null {
  const [open, setOpen] = useState(false)

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
  }

  return (
    <DropdownMenu open={open} onOpenChange={setOpen}>
      <DropdownMenuTrigger asChild disabled={disabled}>
        <button
          className={cn(
            'flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors px-2 py-1 rounded-md hover:bg-background/50',
            disabled && 'opacity-50 cursor-not-allowed'
          )}
        >
          <span className="max-w-[140px] truncate">{currentModeName}</span>
          <ChevronDown className="h-3 w-3" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent
        side="top"
        align="start"
        className="min-w-[140px] max-h-[300px] overflow-y-auto"
      >
        {modeState.availableModes.map((mode) => {
          const isSelected = mode.id === modeState.currentModeId

          return (
            <DropdownMenuItem
              key={mode.id}
              onClick={() => handleSelect(mode.id)}
              className="flex items-center justify-between gap-2"
            >
              <span>{mode.name}</span>
              {isSelected && <span className="h-2 w-2 rounded-full bg-primary" />}
            </DropdownMenuItem>
          )
        })}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
