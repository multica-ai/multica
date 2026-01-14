/**
 * Message input component
 */
import { useState, useRef, useEffect } from 'react'
import { ArrowUp, Square } from 'lucide-react'
import { Button } from '@/components/ui/button'

interface MessageInputProps {
  onSend: (content: string) => void
  onCancel: () => void
  isProcessing: boolean
  disabled: boolean
  placeholder?: string
}

export function MessageInput({
  onSend,
  onCancel,
  isProcessing,
  disabled,
  placeholder = 'Type a message...',
}: MessageInputProps) {
  const [value, setValue] = useState('')
  const [isComposing, setIsComposing] = useState(false)
  const textareaRef = useRef<HTMLTextAreaElement>(null)

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

  return (
    <div className="p-4">
      <div className="mx-auto max-w-3xl">
        <div className="bg-secondary/50 hover:bg-secondary focus-within:bg-secondary transition-colors duration-200 rounded-md p-2 border border-border">
          <textarea
            ref={textareaRef}
            value={value}
            onChange={(e) => setValue(e.target.value)}
            onKeyDown={handleKeyDown}
            onCompositionStart={() => setIsComposing(true)}
            onCompositionEnd={() => setIsComposing(false)}
            placeholder={disabled ? 'Select or create a session first' : placeholder}
            disabled={disabled}
            rows={1}
            className="w-full resize-none bg-transparent px-2 py-1 text-foreground outline-none placeholder:text-muted-foreground disabled:cursor-not-allowed disabled:opacity-50"
          />
          <div className="flex justify-end pt-1">
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
          </div>
        </div>
        <div className="mt-1 text-center text-xs text-muted-foreground">
          Enter to send, Shift+Enter for new line
        </div>
      </div>
    </div>
  )
}
