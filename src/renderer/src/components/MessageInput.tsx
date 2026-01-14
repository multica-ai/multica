/**
 * Message input component
 */
import { useState, useRef, useEffect } from 'react'
import { ArrowUp, Square, Folder } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Tooltip, TooltipTrigger, TooltipContent } from '@/components/ui/tooltip'

interface MessageInputProps {
  onSend: (content: string) => void
  onCancel: () => void
  isProcessing: boolean
  disabled: boolean
  placeholder?: string
  workingDirectory?: string | null
  onSelectFolder: () => Promise<void>
}

export function MessageInput({
  onSend,
  onCancel,
  isProcessing,
  disabled,
  placeholder = 'Type a message...',
  workingDirectory,
  onSelectFolder,
}: MessageInputProps) {
  const [value, setValue] = useState('')
  const [isComposing, setIsComposing] = useState(false)
  const textareaRef = useRef<HTMLTextAreaElement>(null)

  // Get folder name from path
  const folderName = workingDirectory
    ? workingDirectory.split('/').filter(Boolean).pop() || workingDirectory
    : null

  // Auto-resize textarea
  useEffect(() => {
    const textarea = textareaRef.current
    if (textarea) {
      textarea.style.height = 'auto'
      textarea.style.height = `${Math.min(textarea.scrollHeight, 200)}px`
    }
  }, [value])

  // Focus on mount
  useEffect(() => {
    textareaRef.current?.focus()
  }, [])

  const handleSubmit = () => {
    const trimmed = value.trim()
    if (!trimmed || disabled || isProcessing) return

    onSend(trimmed)
    setValue('')
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    // Don't submit while IME is composing
    if (e.key === 'Enter' && !e.shiftKey && !isComposing) {
      e.preventDefault()
      handleSubmit()
    }
  }

  const canSubmit = !disabled && value.trim().length > 0
  const hasFolder = !!workingDirectory

  // Render folder selection mode when no folder is selected
  if (!hasFolder) {
    return (
      <div className="p-4">
        <div className="mx-auto max-w-3xl">
          <div className="bg-secondary/50 hover:bg-secondary transition-colors duration-200 rounded-xl p-3 border border-border">
            {/* Folder selection prompt */}
            <div className="flex items-center gap-3">
              <Folder className="h-5 w-5 text-muted-foreground flex-shrink-0" />
              <span className="text-muted-foreground flex-1">Select a folder to start...</span>
              <Button
                variant="outline"
                size="sm"
                onClick={onSelectFolder}
                className="flex-shrink-0"
              >
                Browse
              </Button>
            </div>

            {/* Bottom toolbar */}
            <div className="flex items-center justify-end pt-3 mt-3 border-t border-border/50">
              <Button size="icon" disabled className="h-8 w-8 rounded-full">
                <ArrowUp className="h-4 w-4" />
              </Button>
            </div>
          </div>
        </div>
      </div>
    )
  }

  // Normal chat input mode
  return (
    <div className="p-4">
      <div className="mx-auto max-w-3xl">
        <div className="bg-secondary/50 hover:bg-secondary focus-within:bg-secondary transition-colors duration-200 rounded-xl p-3 border border-border">
          {/* Text input */}
          <textarea
            ref={textareaRef}
            value={value}
            onChange={(e) => setValue(e.target.value)}
            onKeyDown={handleKeyDown}
            onCompositionStart={() => setIsComposing(true)}
            onCompositionEnd={() => setIsComposing(false)}
            placeholder={placeholder}
            disabled={disabled}
            rows={2}
            className="w-full resize-none bg-transparent px-1 py-1 text-sm text-foreground outline-none placeholder:text-muted-foreground disabled:cursor-not-allowed disabled:opacity-50"
          />

          {/* Bottom toolbar */}
          <div className="flex items-center justify-between pt-2">
            {/* Folder indicator */}
            <Tooltip>
              <TooltipTrigger asChild>
                <button
                  onClick={onSelectFolder}
                  className="flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors px-2 py-1 rounded-md hover:bg-background/50"
                >
                  <Folder className="h-3.5 w-3.5" />
                  <span className="max-w-[150px] truncate">{folderName}</span>
                </button>
              </TooltipTrigger>
              <TooltipContent side="top">Change folder</TooltipContent>
            </Tooltip>

            {/* Send button */}
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  size="icon"
                  onClick={isProcessing ? onCancel : handleSubmit}
                  disabled={!canSubmit && !isProcessing}
                  className="h-8 w-8 rounded-full"
                >
                  {isProcessing ? (
                    <Square className="h-3.5 w-3.5" fill="currentColor" />
                  ) : (
                    <ArrowUp className="h-4 w-4" />
                  )}
                </Button>
              </TooltipTrigger>
              <TooltipContent side="top">
                {isProcessing ? 'Stop' : 'Send message'}
              </TooltipContent>
            </Tooltip>
          </div>
        </div>
      </div>
    </div>
  )
}
