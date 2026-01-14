/**
 * Message input component
 */
import { useState, useRef, useEffect } from 'react'
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
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      handleSubmit()
    }
  }

  return (
    <div className="border-t border-border p-4">
      <div className="mx-auto flex max-w-3xl gap-2">
        <textarea
          ref={textareaRef}
          value={value}
          onChange={(e) => setValue(e.target.value)}
          onKeyDown={handleKeyDown}
          placeholder={disabled ? 'Select or create a session first' : placeholder}
          disabled={disabled}
          rows={1}
          className="flex-1 resize-none rounded-lg border border-border bg-muted px-4 py-2 text-foreground outline-none placeholder:text-muted-foreground focus:border-primary disabled:cursor-not-allowed disabled:opacity-50"
        />

        {isProcessing ? (
          <Button variant="destructive" onClick={onCancel}>
            Cancel
          </Button>
        ) : (
          <Button onClick={handleSubmit} disabled={disabled || !value.trim()}>
            Send
          </Button>
        )}
      </div>

      {/* Hint */}
      <div className="mx-auto mt-1 max-w-3xl text-center text-xs text-muted-foreground">
        Press Enter to send, Shift+Enter for new line
      </div>
    </div>
  )
}
