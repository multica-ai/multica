/**
 * @vitest-environment jsdom
 */
/* eslint-disable react/prop-types */
import React, { act } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createRoot, type Root } from 'react-dom/client'
import type { MulticaSession } from '../../../../src/shared/types'
import { SessionItem } from '../../../../src/renderer/src/components/ProjectItem'

vi.mock('@/components/ui/sidebar', () => ({
  SidebarMenuItem: ({ children, ...props }: React.HTMLAttributes<HTMLDivElement>) => (
    <div {...props}>{children}</div>
  ),
  SidebarMenuButton: ({
    children,
    onClick,
    isActive,
    ...props
  }: React.ButtonHTMLAttributes<HTMLButtonElement> & { isActive?: boolean }) => {
    void isActive
    return (
      <button onClick={onClick} {...props}>
        {children}
      </button>
    )
  },
  SidebarMenuSub: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SidebarMenu: ({ children }: { children: React.ReactNode }) => <div>{children}</div>
}))

vi.mock('@/components/ui/tooltip', () => ({
  Tooltip: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  TooltipTrigger: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  TooltipContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>
}))

vi.mock('@/components/ui/dropdown-menu', () => ({
  DropdownMenu: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DropdownMenuContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DropdownMenuItem: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DropdownMenuSeparator: () => <div />,
  DropdownMenuTrigger: ({ children }: { children: React.ReactNode }) => <div>{children}</div>
}))

vi.mock('@/components/ui/collapsible', () => ({
  Collapsible: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  CollapsibleContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  CollapsibleTrigger: ({ children }: { children: React.ReactNode }) => <div>{children}</div>
}))

vi.mock('lucide-react', () => ({
  AlertTriangle: (props: React.SVGProps<SVGSVGElement>) => <svg {...props} />,
  Archive: (props: React.SVGProps<SVGSVGElement>) => <svg {...props} />,
  ChevronDown: (props: React.SVGProps<SVGSVGElement>) => <svg {...props} />,
  ChevronRight: (props: React.SVGProps<SVGSVGElement>) => <svg {...props} />,
  CirclePause: (props: React.SVGProps<SVGSVGElement>) => <svg {...props} />,
  Folder: (props: React.SVGProps<SVGSVGElement>) => <svg {...props} />,
  Loader2: (props: React.SVGProps<SVGSVGElement>) => <svg {...props} />,
  MoreHorizontal: (props: React.SVGProps<SVGSVGElement>) => <svg {...props} />,
  Plus: (props: React.SVGProps<SVGSVGElement>) => <svg {...props} />,
  Trash2: (props: React.SVGProps<SVGSVGElement>) => <svg {...props} />
}))

const buildSession = (title: string): MulticaSession => ({
  id: 'session-1',
  projectId: 'project-1',
  workingDirectory: '/tmp',
  agentId: 'opencode',
  agentSessionId: 'agent-session-1',
  createdAt: new Date().toISOString(),
  updatedAt: new Date().toISOString(),
  status: 'active',
  title,
  messageCount: 0,
  isArchived: false
})

const setNativeValue = (element: HTMLInputElement, value: string): void => {
  const descriptor = Object.getOwnPropertyDescriptor(Object.getPrototypeOf(element), 'value')
  if (!descriptor?.set) {
    throw new Error('Missing value setter on input')
  }
  descriptor.set.call(element, value)
}

describe('SessionItem', () => {
  let container: HTMLDivElement
  let root: Root

  beforeEach(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
  })

  afterEach(() => {
    act(() => {
      root.unmount()
    })
    container.remove()
    globalThis.IS_REACT_ACT_ENVIRONMENT = false
  })

  it('does not save when edit is cancelled with Escape', () => {
    const onUpdateTitle = vi.fn()

    act(() => {
      root.render(
        <SessionItem
          session={buildSession('Old Title')}
          isActive={false}
          isProcessing={false}
          needsPermission={false}
          onSelect={vi.fn()}
          onArchive={vi.fn()}
          onUpdateTitle={onUpdateTitle}
        />
      )
    })

    const titleSpan = Array.from(container.querySelectorAll('span')).find(
      (span) => span.textContent === 'Old Title'
    )
    if (!titleSpan) throw new Error('Title element not found')

    act(() => {
      titleSpan.dispatchEvent(new MouseEvent('dblclick', { bubbles: true }))
    })

    const input = container.querySelector('input') as HTMLInputElement | null
    if (!input) throw new Error('Input not found after double click')

    act(() => {
      setNativeValue(input, 'New Title')
      input.dispatchEvent(new Event('input', { bubbles: true }))
      input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
      input.dispatchEvent(new FocusEvent('blur', { bubbles: true }))
    })

    expect(onUpdateTitle).not.toHaveBeenCalled()
  })

  it('saves when Enter is pressed', () => {
    const onUpdateTitle = vi.fn()

    act(() => {
      root.render(
        <SessionItem
          session={buildSession('Old Title')}
          isActive={false}
          isProcessing={false}
          needsPermission={false}
          onSelect={vi.fn()}
          onArchive={vi.fn()}
          onUpdateTitle={onUpdateTitle}
        />
      )
    })

    const titleSpan = Array.from(container.querySelectorAll('span')).find(
      (span) => span.textContent === 'Old Title'
    )
    if (!titleSpan) throw new Error('Title element not found')

    act(() => {
      titleSpan.dispatchEvent(new MouseEvent('dblclick', { bubbles: true }))
    })

    const input = container.querySelector('input') as HTMLInputElement | null
    if (!input) throw new Error('Input not found after double click')

    act(() => {
      setNativeValue(input, 'New Title')
      input.dispatchEvent(new Event('input', { bubbles: true }))
      input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    })

    expect(onUpdateTitle).toHaveBeenCalledWith('New Title')
  })
})
