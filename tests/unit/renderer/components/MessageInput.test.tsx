/**
 * @vitest-environment jsdom
 */
import React, { act } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createRoot, type Root } from 'react-dom/client'
import { MessageInput } from '../../../../src/renderer/src/components/MessageInput'
import { useDraftStore } from '../../../../src/renderer/src/stores/draftStore'
import { useCommandStore } from '../../../../src/renderer/src/stores/commandStore'

vi.mock('@/components/ui/button', () => ({
  Button: ({ children, ...props }: React.ButtonHTMLAttributes<HTMLButtonElement>) => (
    <button {...props}>{children}</button>
  )
}))

vi.mock('@/components/ui/tooltip', () => ({
  Tooltip: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  TooltipTrigger: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  TooltipContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>
}))

vi.mock('../../../../src/renderer/src/components/AgentSelector', () => ({
  AgentSelector: () => null
}))

vi.mock('../../../../src/renderer/src/components/ModeSelector', () => ({
  ModeSelector: () => null
}))

vi.mock('../../../../src/renderer/src/components/ModelSelector', () => ({
  ModelSelector: () => null
}))

vi.mock('../../../../src/renderer/src/components/SlashCommandMenu', () => ({
  SlashCommandMenu: () => null
}))

describe('MessageInput', () => {
  let container: HTMLDivElement
  let root: Root

  const baseProps = {
    onSend: vi.fn(),
    onCancel: vi.fn(),
    isProcessing: false,
    disabled: false,
    workingDirectory: '/tmp'
  }

  const renderInput = (sessionId?: string): void => {
    act(() => {
      root.render(<MessageInput {...baseProps} sessionId={sessionId} />)
    })
  }

  const setNativeValue = (element: HTMLTextAreaElement, value: string): void => {
    const descriptor = Object.getOwnPropertyDescriptor(Object.getPrototypeOf(element), 'value')
    if (!descriptor?.set) {
      throw new Error('Missing value setter on textarea')
    }
    descriptor.set.call(element, value)
  }

  const setTextareaValue = (value: string): HTMLTextAreaElement => {
    const textarea = container.querySelector('textarea') as HTMLTextAreaElement | null
    if (!textarea) throw new Error('Textarea not found')
    act(() => {
      setNativeValue(textarea, value)
      textarea.dispatchEvent(new Event('input', { bubbles: true }))
    })
    return textarea
  }

  beforeEach(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true
    vi.useFakeTimers()
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
    useDraftStore.setState({ drafts: {} })
    useCommandStore.getState().clearCommands()
  })

  afterEach(() => {
    act(() => {
      root.unmount()
    })
    container.remove()
    globalThis.IS_REACT_ACT_ENVIRONMENT = false
    vi.runOnlyPendingTimers()
    vi.useRealTimers()
  })

  it('restores drafts when switching sessions', () => {
    useDraftStore.getState().setDraft('session-a', { text: 'hello', images: [] })
    useDraftStore.getState().setDraft('session-b', { text: 'world', images: [] })

    renderInput('session-a')
    expect((container.querySelector('textarea') as HTMLTextAreaElement).value).toBe('hello')

    renderInput('session-b')
    expect((container.querySelector('textarea') as HTMLTextAreaElement).value).toBe('world')
  })

  it('clears previous session draft when input is empty on switch', () => {
    useDraftStore.getState().setDraft('session-a', { text: 'hello', images: [] })

    renderInput('session-a')
    expect((container.querySelector('textarea') as HTMLTextAreaElement).value).toBe('hello')

    setTextareaValue('')
    renderInput('session-b')

    expect(useDraftStore.getState().drafts['session-a']).toBeUndefined()
  })
})
