/**
 * Message input component with image upload and slash command support
 */
import { useState, useRef, useEffect, useCallback, useMemo } from 'react'
import { ArrowUp, Square, Plus, X, AlertTriangle } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Tooltip, TooltipTrigger, TooltipContent } from '@/components/ui/tooltip'
import { AgentModelSelector } from './AgentModelSelector'
import { ModeSelector } from './ModeSelector'
import { SlashCommandMenu } from './SlashCommandMenu'
import { parseSlashCommand, validateCommand } from '../utils/slashCommand'
import { useCommandStore } from '../stores/commandStore'
import { useDraftStore } from '../stores/draftStore'
import type { MessageContent, ImageContentItem } from '../../../shared/types/message'
import type {
  SessionModeState,
  SessionModelState,
  SessionModeId,
  ModelId,
  AvailableCommand
} from '../../../shared/types'

interface MessageInputProps {
  sessionId?: string
  onSend: (content: MessageContent) => void
  onCancel: () => void
  isProcessing: boolean
  disabled: boolean
  placeholder?: string
  workingDirectory?: string | null
  currentAgentId?: string
  onAgentChange?: (agentId: string) => void
  isSwitchingAgent?: boolean
  isInitializing?: boolean
  directoryExists?: boolean
  onDeleteSession?: () => void
  // Mode/Model props
  sessionModeState?: SessionModeState | null
  sessionModelState?: SessionModelState | null
  onModeChange?: (modeId: SessionModeId) => void
  onModelChange?: (modelId: ModelId) => void
}

const MAX_IMAGE_SIZE = 10 * 1024 * 1024 // 10MB
const SUPPORTED_IMAGE_TYPES = ['image/png', 'image/jpeg', 'image/gif', 'image/webp']
// Minimal delay for focusing textarea after dropdown closes
const DROPDOWN_CLOSE_DELAY_MS = 0

// Warning banner component for missing directory
function DirectoryWarningBanner({
  onDeleteSession
}: {
  onDeleteSession?: () => void
}): React.JSX.Element {
  return (
    <div className="mb-2">
      <div className="bg-amber-500/10 border border-amber-500/30 rounded-xl px-4 py-3">
        <div className="flex items-center justify-between gap-4">
          {/* Left: Icon + Text */}
          <div className="flex items-center gap-3 min-w-0">
            <AlertTriangle className="h-4 w-4 text-amber-500 shrink-0" />
            <div className="min-w-0">
              <p className="text-sm font-medium text-foreground truncate">Directory not found</p>
              <p className="text-xs text-muted-foreground truncate hidden sm:block">
                The directory may have been moved or deleted.
              </p>
            </div>
          </div>

          {/* Right: Delete button */}
          {onDeleteSession && (
            <Button variant="outline" size="sm" onClick={onDeleteSession}>
              Delete Session
            </Button>
          )}
        </div>
      </div>
    </div>
  )
}

export function MessageInput({
  sessionId,
  onSend,
  onCancel,
  isProcessing,
  disabled,
  placeholder = 'Type a message, or / for commands',
  workingDirectory,
  currentAgentId,
  onAgentChange,
  isSwitchingAgent = false,
  isInitializing = false,
  directoryExists,
  onDeleteSession,
  sessionModeState,
  sessionModelState,
  onModeChange,
  onModelChange
}: MessageInputProps): React.JSX.Element | null {
  // Draft store for per-session input persistence
  const getDraft = useDraftStore((state) => state.getDraft)
  const setDraft = useDraftStore((state) => state.setDraft)
  const clearDraft = useDraftStore((state) => state.clearDraft)

  // State is initialized empty; session drafts are restored via useEffect below
  const [value, setValue] = useState('')
  const [isComposing, setIsComposing] = useState(false)
  const [images, setImages] = useState<ImageContentItem[]>([])
  const [menuDismissed, setMenuDismissed] = useState(false)
  const [commandMenuIndex, setCommandMenuIndex] = useState(0)
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const inputContainerRef = useRef<HTMLDivElement>(null)
  // Initialize to undefined so the first useEffect run will restore the draft
  const prevSessionIdRef = useRef<string | undefined>(undefined)

  // Get available commands from store
  const availableCommands = useCommandStore((state) => state.availableCommands)

  // Parse current slash command state
  const parsedCommand = useMemo(() => parseSlashCommand(value), [value])
  const commandFilter = parsedCommand?.command || ''
  const isInCommandMode = parsedCommand !== null && parsedCommand.argument === undefined
  const showCommandMenu = isInCommandMode && availableCommands.length > 0 && !menuDismissed
  const commandError = useMemo(() => {
    if (!parsedCommand || !parsedCommand.command) return null
    if (availableCommands.length === 0 || isInCommandMode) return null
    return validateCommand(value, availableCommands)
  }, [parsedCommand, availableCommands, isInCommandMode, value])

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

  // Handle session switch: save/clear current draft and restore new session's draft
  useEffect(() => {
    const prevSessionId = prevSessionIdRef.current

    if (prevSessionId !== sessionId) {
      // Save or clear draft for previous session
      if (prevSessionId) {
        if (value.trim() || images.length > 0) {
          setDraft(prevSessionId, { text: value, images })
        } else {
          clearDraft(prevSessionId)
        }
      }

      // Restore draft for new session
      if (sessionId) {
        const draft = getDraft(sessionId)
        setValue(draft.text)
        setImages(draft.images)
      } else {
        setValue('')
        setImages([])
      }
      setMenuDismissed(false)
      setCommandMenuIndex(0)

      prevSessionIdRef.current = sessionId
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- Intentional: only react to session changes; draft changes handled elsewhere
  }, [sessionId, getDraft, setDraft, clearDraft])

  // Auto-save draft with debounce
  useEffect(() => {
    if (!sessionId) return

    const timer = setTimeout(() => {
      if (value.trim() || images.length > 0) {
        setDraft(sessionId, { text: value, images })
      } else {
        clearDraft(sessionId)
      }
    }, 300)

    return () => clearTimeout(timer)
  }, [sessionId, value, images, setDraft, clearDraft])

  // Convert file to base64
  const fileToBase64 = useCallback((file: File): Promise<string> => {
    return new Promise((resolve, reject) => {
      const reader = new FileReader()
      reader.onload = () => {
        const result = reader.result as string
        // Remove data URL prefix (e.g., "data:image/png;base64,")
        const base64 = result.split(',')[1]
        resolve(base64)
      }
      reader.onerror = reject
      reader.readAsDataURL(file)
    })
  }, [])

  // Process and add image
  const addImage = useCallback(
    async (file: File) => {
      // Validate file type
      if (!SUPPORTED_IMAGE_TYPES.includes(file.type)) {
        console.warn('Unsupported image type:', file.type)
        return
      }

      // Validate file size
      if (file.size > MAX_IMAGE_SIZE) {
        console.warn('Image too large:', file.size)
        return
      }

      try {
        const base64 = await fileToBase64(file)
        const imageItem: ImageContentItem = {
          type: 'image',
          data: base64,
          mimeType: file.type
        }
        setImages((prev) => [...prev, imageItem])
      } catch (err) {
        console.error('Failed to process image:', err)
      }
    },
    [fileToBase64]
  )

  // Handle paste event
  const handlePaste = useCallback(
    async (e: React.ClipboardEvent) => {
      const items = e.clipboardData?.items
      if (!items) return

      for (const item of items) {
        if (item.type.startsWith('image/')) {
          e.preventDefault()
          const file = item.getAsFile()
          if (file) {
            await addImage(file)
          }
          break
        }
      }
    },
    [addImage]
  )

  // Handle file selection
  const handleFileSelect = useCallback(
    async (e: React.ChangeEvent<HTMLInputElement>) => {
      const files = e.target.files
      if (!files) return

      for (const file of files) {
        await addImage(file)
      }

      // Reset input
      if (fileInputRef.current) {
        fileInputRef.current.value = ''
      }
    },
    [addImage]
  )

  // Remove image
  const removeImage = useCallback((index: number) => {
    setImages((prev) => prev.filter((_, i) => i !== index))
  }, [])

  // Handle slash command selection from menu
  const handleCommandSelect = useCallback((command: AvailableCommand) => {
    // Replace the current "/" text with the selected command
    const hasInput = command.input
    setValue(`/${command.name}${hasInput ? ' ' : ''}`)
    setMenuDismissed(true)
    // Focus back on textarea
    textareaRef.current?.focus()
  }, [])

  // Focus textarea after selector completion (model/agent/mode selection)
  const handleSelectorComplete = useCallback(() => {
    // Delay to allow dropdown close animation and Radix auto-focus to complete
    setTimeout(() => {
      textareaRef.current?.focus()
    }, DROPDOWN_CLOSE_DELAY_MS)
  }, [])

  // Handle submit
  const handleSubmit = useCallback(() => {
    const trimmed = value.trim()
    if ((!trimmed && images.length === 0) || disabled || isProcessing) return

    // Check for command errors before sending
    if (commandError) {
      return
    }

    // Build message content array
    const content: MessageContent = []

    // Add images first
    for (const img of images) {
      content.push(img)
    }

    // Add text if present
    if (trimmed) {
      content.push({ type: 'text', text: trimmed })
    }

    onSend(content)
    setValue('')
    setImages([])
    // Clear draft after sending
    if (sessionId) {
      clearDraft(sessionId)
    }
  }, [value, images, disabled, isProcessing, onSend, commandError, sessionId, clearDraft])

  const handleKeyDown = (e: React.KeyboardEvent): void => {
    // Don't handle Enter/Tab/arrows while command menu is open (SlashCommandMenu handles these)
    if (showCommandMenu && ['Enter', 'Tab', 'ArrowUp', 'ArrowDown', 'Escape'].includes(e.key)) {
      return
    }
    // Don't submit while IME is composing
    if (e.key === 'Enter' && !e.shiftKey && !isComposing) {
      e.preventDefault()
      handleSubmit()
    }
  }

  const canSubmit = !disabled && !commandError && (value.trim().length > 0 || images.length > 0)
  const hasFolder = !!workingDirectory

  // Don't render when no folder is selected
  if (!hasFolder) {
    return null
  }

  // Normal chat input mode
  return (
    <div className="pb-2">
      {directoryExists === false && <DirectoryWarningBanner onDeleteSession={onDeleteSession} />}
      <div ref={inputContainerRef} className="relative">
        {/* Slash command autocomplete menu */}
        <SlashCommandMenu
          commands={availableCommands}
          filter={commandFilter}
          selectedIndex={commandMenuIndex}
          onSelect={handleCommandSelect}
          onIndexChange={setCommandMenuIndex}
          onClose={() => setMenuDismissed(true)}
          visible={showCommandMenu}
        />

        <div className="bg-card transition-colors duration-200 rounded-xl p-3 border border-border">
          {/* Image previews */}
          {images.length > 0 && (
            <div className="flex flex-wrap gap-2 mb-3 pb-3 border-b border-border/50">
              {images.map((img, index) => (
                <div
                  key={index}
                  className="relative group w-16 h-16 rounded-lg overflow-hidden border border-border bg-background"
                >
                  <img
                    src={`data:${img.mimeType};base64,${img.data}`}
                    alt={`Upload ${index + 1}`}
                    className="w-full h-full object-cover"
                  />
                  <button
                    onClick={() => removeImage(index)}
                    className="absolute top-0.5 right-0.5 p-0.5 rounded-full bg-background/80 hover:bg-background text-muted-foreground hover:text-foreground opacity-0 group-hover:opacity-100 transition-opacity"
                  >
                    <X className="h-3 w-3" />
                  </button>
                </div>
              ))}
            </div>
          )}

          {/* Text input */}
          <textarea
            ref={textareaRef}
            value={value}
            onChange={(e) => {
              const nextValue = e.target.value
              if (menuDismissed) {
                setMenuDismissed(false)
              }
              const nextParsed = parseSlashCommand(nextValue)
              const nextIsCommandMode = nextParsed !== null && nextParsed.argument === undefined
              if (nextIsCommandMode && availableCommands.length > 0) {
                setCommandMenuIndex(0)
              }
              setValue(nextValue)
            }}
            onKeyDown={handleKeyDown}
            onPaste={handlePaste}
            onCompositionStart={() => setIsComposing(true)}
            onCompositionEnd={() => setIsComposing(false)}
            placeholder={placeholder}
            disabled={disabled}
            rows={2}
            className="w-full resize-none bg-transparent px-1 py-1 text-sm text-foreground outline-none placeholder:text-muted-foreground disabled:cursor-not-allowed disabled:opacity-50"
          />

          {/* Command error message */}
          {commandError && <div className="px-1 py-1 text-xs text-destructive">{commandError}</div>}

          {/* Hidden file input */}
          <input
            ref={fileInputRef}
            type="file"
            accept={SUPPORTED_IMAGE_TYPES.join(',')}
            multiple
            onChange={handleFileSelect}
            className="hidden"
          />

          {/* Bottom toolbar */}
          <div className="flex items-center justify-between pt-2">
            {/* Left side: agent/model selector, mode selector */}
            <div className="flex items-center gap-1">
              {/* Combined Agent and Model selector */}
              {currentAgentId && onAgentChange && onModelChange && (
                <AgentModelSelector
                  currentAgentId={currentAgentId}
                  onAgentChange={onAgentChange}
                  modelState={sessionModelState ?? null}
                  onModelChange={onModelChange}
                  disabled={isProcessing}
                  isSwitching={isSwitchingAgent}
                  isInitializing={isInitializing}
                  onSelectionComplete={handleSelectorComplete}
                />
              )}

              {/* Mode selector (only shown if agent supports modes, or skeleton during init) */}
              {(sessionModeState || isInitializing) && onModeChange && (
                <ModeSelector
                  modeState={sessionModeState ?? null}
                  onModeChange={onModeChange}
                  disabled={isProcessing}
                  isInitializing={isInitializing}
                  onSelectionComplete={handleSelectorComplete}
                />
              )}
            </div>

            {/* Right side: image upload and send button */}
            <div className="flex items-center gap-1">
              {/* Image upload button */}
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    onClick={() => fileInputRef.current?.click()}
                    disabled={disabled}
                    className="rounded-lg"
                  >
                    <Plus className="h-3.5 w-3.5" />
                  </Button>
                </TooltipTrigger>
                <TooltipContent side="top">Attach image (or paste with Cmd+V)</TooltipContent>
              </Tooltip>

              {/* Send button */}
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    size="icon-xs"
                    onClick={isProcessing ? onCancel : handleSubmit}
                    disabled={!canSubmit && !isProcessing}
                    className="rounded-lg ml-1"
                  >
                    {isProcessing ? (
                      <Square className="h-3 w-3" fill="currentColor" />
                    ) : (
                      <ArrowUp className="h-3.5 w-3.5" />
                    )}
                  </Button>
                </TooltipTrigger>
                <TooltipContent side="top">{isProcessing ? 'Stop' : 'Send message'}</TooltipContent>
              </Tooltip>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}
