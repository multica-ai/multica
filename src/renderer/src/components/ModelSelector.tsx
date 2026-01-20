/**
 * Model selector dropdown for MessageInput
 * Shows available AI models from the ACP server
 */
import { useState } from 'react'
import { ChevronDown } from 'lucide-react'
import type { SessionModelState, ModelId } from '../../../shared/types'
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem
} from '@/components/ui/dropdown-menu'
import { cn } from '@/lib/utils'

interface ModelSelectorProps {
  modelState: SessionModelState | null
  onModelChange: (modelId: ModelId) => void
  disabled?: boolean
}

export function ModelSelector({
  modelState,
  onModelChange,
  disabled = false
}: ModelSelectorProps): React.JSX.Element | null {
  const [open, setOpen] = useState(false)

  // Don't render if no model state (agent doesn't support model selection)
  if (!modelState) {
    return null
  }

  const currentModel = modelState.availableModels.find(
    (m) => m.modelId === modelState.currentModelId
  )
  const currentModelName = currentModel?.name || modelState.currentModelId

  function handleSelect(modelId: ModelId): void {
    if (modelId !== modelState?.currentModelId) {
      onModelChange(modelId)
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
          <span className="max-w-[140px] truncate">{currentModelName}</span>
          <ChevronDown className="h-3 w-3" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent
        side="top"
        align="start"
        className="min-w-[160px] max-h-[300px] overflow-y-auto"
      >
        {modelState.availableModels.map((model) => {
          const isSelected = model.modelId === modelState.currentModelId

          return (
            <DropdownMenuItem
              key={model.modelId}
              onClick={() => handleSelect(model.modelId)}
              className="flex items-center justify-between gap-2"
            >
              <span>{model.name}</span>
              {isSelected && <span className="h-2 w-2 rounded-full bg-primary" />}
            </DropdownMenuItem>
          )
        })}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
