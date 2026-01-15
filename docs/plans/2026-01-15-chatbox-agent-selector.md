# Chatbox Agent Selector Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add an inline agent selector to MessageInput that lets users switch the current session's agent mid-conversation.

**Architecture:** Create a new AgentSelector component using Radix UI DropdownMenu. Add backend support to switch a session's agent (stop old agent, update agentId, start new agent). Wire up through useApp hook.

**Tech Stack:** React, TypeScript, Radix UI DropdownMenu, Tailwind CSS, lucide-react icons

---

### Task 1: Create DropdownMenu UI Component

**Files:**

- Create: `src/renderer/src/components/ui/dropdown-menu.tsx`

**Step 1: Install Radix UI dropdown-menu package**

Run: `npm install @radix-ui/react-dropdown-menu`
Expected: Package added to package.json

**Step 2: Create the DropdownMenu component**

```tsx
import * as React from 'react'
import * as DropdownMenuPrimitive from '@radix-ui/react-dropdown-menu'
import { CheckIcon } from 'lucide-react'

import { cn } from '@/lib/utils'

function DropdownMenu({ ...props }: React.ComponentProps<typeof DropdownMenuPrimitive.Root>) {
  return <DropdownMenuPrimitive.Root data-slot="dropdown-menu" {...props} />
}

function DropdownMenuTrigger({
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.Trigger>) {
  return <DropdownMenuPrimitive.Trigger data-slot="dropdown-menu-trigger" {...props} />
}

function DropdownMenuContent({
  className,
  sideOffset = 4,
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.Content>) {
  return (
    <DropdownMenuPrimitive.Portal>
      <DropdownMenuPrimitive.Content
        data-slot="dropdown-menu-content"
        sideOffset={sideOffset}
        className={cn(
          'bg-popover text-popover-foreground data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95 data-[side=bottom]:slide-in-from-top-2 data-[side=left]:slide-in-from-right-2 data-[side=right]:slide-in-from-left-2 data-[side=top]:slide-in-from-bottom-2 z-50 min-w-[8rem] overflow-hidden rounded-md border p-1 shadow-md',
          className
        )}
        {...props}
      />
    </DropdownMenuPrimitive.Portal>
  )
}

function DropdownMenuItem({
  className,
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.Item>) {
  return (
    <DropdownMenuPrimitive.Item
      data-slot="dropdown-menu-item"
      className={cn(
        'focus:bg-accent focus:text-accent-foreground relative flex cursor-default items-center gap-2 rounded-sm px-2 py-1.5 text-sm outline-hidden select-none data-[disabled]:pointer-events-none data-[disabled]:opacity-50',
        className
      )}
      {...props}
    />
  )
}

function DropdownMenuCheckboxItem({
  className,
  children,
  checked,
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.CheckboxItem>) {
  return (
    <DropdownMenuPrimitive.CheckboxItem
      data-slot="dropdown-menu-checkbox-item"
      className={cn(
        'focus:bg-accent focus:text-accent-foreground relative flex cursor-default items-center gap-2 rounded-sm py-1.5 pr-2 pl-8 text-sm outline-hidden select-none data-[disabled]:pointer-events-none data-[disabled]:opacity-50',
        className
      )}
      checked={checked}
      {...props}
    >
      <span className="pointer-events-none absolute left-2 flex size-3.5 items-center justify-center">
        <DropdownMenuPrimitive.ItemIndicator>
          <CheckIcon className="size-4" />
        </DropdownMenuPrimitive.ItemIndicator>
      </span>
      {children}
    </DropdownMenuPrimitive.CheckboxItem>
  )
}

function DropdownMenuSeparator({
  className,
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.Separator>) {
  return (
    <DropdownMenuPrimitive.Separator
      data-slot="dropdown-menu-separator"
      className={cn('bg-border -mx-1 my-1 h-px', className)}
      {...props}
    />
  )
}

export {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuCheckboxItem,
  DropdownMenuSeparator
}
```

**Step 3: Verify build passes**

Run: `npm run build`
Expected: Build succeeds without errors

**Step 4: Commit**

```bash
git add src/renderer/src/components/ui/dropdown-menu.tsx package.json package-lock.json
git commit -m "feat: add DropdownMenu UI component"
```

---

### Task 2: Add switchSessionAgent to Backend

**Files:**

- Modify: `src/main/conductor/Conductor.ts:468-476` (add new method after updateSessionMeta)
- Modify: `src/main/ipc/handlers.ts:106-111` (add IPC handler after SESSION_UPDATE)
- Modify: `src/shared/ipc-channels.ts` (add new channel)
- Modify: `src/shared/electron-api.d.ts:106` (add new API method)
- Modify: `src/preload/preload.ts` (add preload binding)

**Step 1: Add IPC channel constant**

In `src/shared/ipc-channels.ts`, add after `SESSION_UPDATE`:

```typescript
SESSION_SWITCH_AGENT: 'session:switch-agent',
```

**Step 2: Add Conductor method**

In `src/main/conductor/Conductor.ts`, add after `updateSessionMeta` method (around line 476):

```typescript
/**
 * Switch a session's agent (stops current, updates, starts new)
 */
async switchSessionAgent(sessionId: string, newAgentId: string): Promise<MulticaSession> {
  if (!this.sessionStore) {
    throw new Error('Session agent switch not available in CLI mode')
  }

  const data = await this.sessionStore.get(sessionId)
  if (!data) {
    throw new Error(`Session not found: ${sessionId}`)
  }

  // Get new agent config
  const newAgentConfig = DEFAULT_AGENTS[newAgentId]
  if (!newAgentConfig) {
    throw new Error(`Unknown agent: ${newAgentId}`)
  }

  console.log(`[Conductor] Switching session ${sessionId} from ${data.session.agentId} to ${newAgentId}`)

  // Stop current agent if running
  await this.stopSession(sessionId)

  // Update session's agentId
  let updatedSession = await this.sessionStore.updateMeta(sessionId, {
    agentId: newAgentId,
  })

  // Start new agent (isResumed = true to replay history)
  const { agentSessionId } = await this.startAgentForSession(
    sessionId,
    newAgentConfig,
    data.session.workingDirectory,
    true
  )

  // Update agentSessionId
  updatedSession = await this.sessionStore.updateMeta(sessionId, {
    agentSessionId,
    status: 'active',
  })

  // Notify frontend
  if (this.events.onSessionMetaUpdated) {
    this.events.onSessionMetaUpdated(updatedSession)
  }

  console.log(`[Conductor] Session ${sessionId} switched to ${newAgentId} (agent session: ${agentSessionId})`)

  return updatedSession
}
```

**Step 3: Add IPC handler**

In `src/main/ipc/handlers.ts`, add after the SESSION_UPDATE handler (around line 111):

```typescript
ipcMain.handle(
  IPC_CHANNELS.SESSION_SWITCH_AGENT,
  async (_event, sessionId: string, newAgentId: string) => {
    return conductor.switchSessionAgent(sessionId, newAgentId)
  }
)
```

**Step 4: Add ElectronAPI type**

In `src/shared/electron-api.d.ts`, add after `updateSession` (around line 106):

```typescript
switchSessionAgent(sessionId: string, newAgentId: string): Promise<MulticaSession>
```

**Step 5: Add preload binding**

In `src/preload/preload.ts`, add to the electronAPI object:

```typescript
switchSessionAgent: (sessionId: string, newAgentId: string) =>
  ipcRenderer.invoke(IPC_CHANNELS.SESSION_SWITCH_AGENT, sessionId, newAgentId),
```

**Step 6: Verify build passes**

Run: `npm run build`
Expected: Build succeeds without errors

**Step 7: Commit**

```bash
git add src/main/conductor/Conductor.ts src/main/ipc/handlers.ts src/shared/ipc-channels.ts src/shared/electron-api.d.ts src/preload/preload.ts
git commit -m "feat: add switchSessionAgent backend support"
```

---

### Task 3: Add switchSessionAgent to useApp Hook

**Files:**

- Modify: `src/renderer/src/hooks/useApp.ts:27-41` (add to AppActions interface)
- Modify: `src/renderer/src/hooks/useApp.ts:293-297` (add implementation before clearError)
- Modify: `src/renderer/src/hooks/useApp.ts:299-317` (add to return object)

**Step 1: Add to AppActions interface**

In `src/renderer/src/hooks/useApp.ts`, add to AppActions interface (around line 36):

```typescript
switchSessionAgent: (newAgentId: string) => Promise<void>
```

**Step 2: Add implementation**

In `src/renderer/src/hooks/useApp.ts`, add before `clearError` (around line 295):

```typescript
const switchSessionAgent = useCallback(
  async (newAgentId: string) => {
    if (!currentSession) {
      setError('No active session')
      return
    }

    try {
      setError(null)
      const updatedSession = await window.electronAPI.switchSessionAgent(
        currentSession.id,
        newAgentId
      )
      setCurrentSession(updatedSession)
      await loadRunningStatus()
    } catch (err) {
      setError(`Failed to switch agent: ${err}`)
    }
  },
  [currentSession, loadRunningStatus]
)
```

**Step 3: Add to return object**

In the return statement, add `switchSessionAgent` to the Actions section.

**Step 4: Verify build passes**

Run: `npm run build`
Expected: Build succeeds without errors

**Step 5: Commit**

```bash
git add src/renderer/src/hooks/useApp.ts
git commit -m "feat: add switchSessionAgent to useApp hook"
```

---

### Task 4: Create AgentSelector Component

**Files:**

- Create: `src/renderer/src/components/AgentSelector.tsx`

**Step 1: Create the component**

```tsx
/**
 * Agent selector dropdown for MessageInput
 */
import { useState, useEffect } from 'react'
import { Sparkles, ChevronDown, Loader2 } from 'lucide-react'
import type { AgentCheckResult } from '../../../shared/electron-api'
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem
} from '@/components/ui/dropdown-menu'
import { Tooltip, TooltipTrigger, TooltipContent } from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'

interface AgentSelectorProps {
  currentAgentId: string
  onAgentChange: (agentId: string) => void
  disabled?: boolean
}

export function AgentSelector({
  currentAgentId,
  onAgentChange,
  disabled = false
}: AgentSelectorProps) {
  const [agents, setAgents] = useState<AgentCheckResult[]>([])
  const [loading, setLoading] = useState(true)
  const [open, setOpen] = useState(false)

  useEffect(() => {
    loadAgents()
  }, [])

  async function loadAgents() {
    setLoading(true)
    try {
      const results = await window.electronAPI.checkAgents()
      setAgents(results)
    } catch (err) {
      console.error('Failed to check agents:', err)
    } finally {
      setLoading(false)
    }
  }

  const currentAgent = agents.find((a) => a.id === currentAgentId)
  const currentAgentName = currentAgent?.name || currentAgentId

  function handleSelect(agentId: string) {
    if (agentId !== currentAgentId) {
      onAgentChange(agentId)
    }
    setOpen(false)
  }

  return (
    <DropdownMenu open={open} onOpenChange={setOpen}>
      <DropdownMenuTrigger asChild disabled={disabled || loading}>
        <button
          className={cn(
            'flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors px-2 py-1 rounded-md hover:bg-background/50',
            disabled && 'opacity-50 cursor-not-allowed'
          )}
        >
          <Sparkles className="h-3.5 w-3.5" />
          <span className="max-w-[100px] truncate">{currentAgentName}</span>
          {loading ? (
            <Loader2 className="h-3 w-3 animate-spin" />
          ) : (
            <ChevronDown className="h-3 w-3" />
          )}
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent side="top" align="start" className="min-w-[140px]">
        {agents.map((agent) => {
          const isSelected = agent.id === currentAgentId
          const isDisabled = !agent.installed

          if (isDisabled) {
            return (
              <Tooltip key={agent.id}>
                <TooltipTrigger asChild>
                  <div>
                    <DropdownMenuItem disabled className="flex items-center justify-between">
                      <span>{agent.name}</span>
                      <span className="h-2 w-2 rounded-full bg-muted" />
                    </DropdownMenuItem>
                  </div>
                </TooltipTrigger>
                <TooltipContent side="right">Setup required in Settings</TooltipContent>
              </Tooltip>
            )
          }

          return (
            <DropdownMenuItem
              key={agent.id}
              onClick={() => handleSelect(agent.id)}
              className="flex items-center justify-between"
            >
              <span>{agent.name}</span>
              <span
                className={cn(
                  'h-2 w-2 rounded-full',
                  isSelected ? 'bg-green-500' : 'bg-green-500/50'
                )}
              />
            </DropdownMenuItem>
          )
        })}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
```

**Step 2: Verify build passes**

Run: `npm run build`
Expected: Build succeeds without errors

**Step 3: Commit**

```bash
git add src/renderer/src/components/AgentSelector.tsx
git commit -m "feat: create AgentSelector component"
```

---

### Task 5: Integrate AgentSelector into MessageInput

**Files:**

- Modify: `src/renderer/src/components/MessageInput.tsx:5-6` (add import)
- Modify: `src/renderer/src/components/MessageInput.tsx:9-17` (update props interface)
- Modify: `src/renderer/src/components/MessageInput.tsx:122-135` (add AgentSelector to toolbar)

**Step 1: Add import**

At top of file, add:

```typescript
import { AgentSelector } from './AgentSelector'
```

**Step 2: Update props interface**

Add new props to MessageInputProps:

```typescript
currentAgentId?: string
onAgentChange?: (agentId: string) => void
```

**Step 3: Update component signature**

Add the new props to the function parameters with defaults:

```typescript
currentAgentId,
onAgentChange,
```

**Step 4: Add AgentSelector to bottom toolbar**

In the normal chat input mode, modify the bottom toolbar (around line 122) to include AgentSelector before the folder indicator:

```tsx
{
  /* Bottom toolbar */
}
;<div className="flex items-center justify-between pt-2">
  <div className="flex items-center gap-1">
    {/* Agent selector */}
    {currentAgentId && onAgentChange && (
      <AgentSelector
        currentAgentId={currentAgentId}
        onAgentChange={onAgentChange}
        disabled={isProcessing}
      />
    )}

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
  </div>

  {/* Send button */}
  <Tooltip>{/* ... existing send button code ... */}</Tooltip>
</div>
```

**Step 5: Verify build passes**

Run: `npm run build`
Expected: Build succeeds without errors

**Step 6: Commit**

```bash
git add src/renderer/src/components/MessageInput.tsx
git commit -m "feat: integrate AgentSelector into MessageInput"
```

---

### Task 6: Wire Up AgentSelector in App.tsx

**Files:**

- Modify: `src/renderer/src/App.tsx:31-38` (add switchSessionAgent to useApp destructuring)
- Modify: `src/renderer/src/App.tsx:126-133` (add props to MessageInput)

**Step 1: Add switchSessionAgent to useApp destructuring**

In the useApp() destructuring, add `switchSessionAgent` to the Actions section.

**Step 2: Pass props to MessageInput**

Update the MessageInput component to include the new props:

```tsx
<MessageInput
  onSend={sendPrompt}
  onCancel={cancelRequest}
  isProcessing={isProcessing}
  disabled={!currentSession}
  workingDirectory={currentSession?.workingDirectory}
  onSelectFolder={handleSelectFolder}
  currentAgentId={currentSession?.agentId}
  onAgentChange={switchSessionAgent}
/>
```

**Step 3: Verify build passes**

Run: `npm run build`
Expected: Build succeeds without errors

**Step 4: Commit**

```bash
git add src/renderer/src/App.tsx
git commit -m "feat: wire up AgentSelector in App.tsx"
```

---

### Task 7: Add Toast Notification on Agent Switch

**Files:**

- Modify: `src/renderer/src/hooks/useApp.ts` (add toast import and notification)

**Step 1: Add toast import**

At top of file, add:

```typescript
import { toast } from 'sonner'
```

**Step 2: Add toast to switchSessionAgent**

In the switchSessionAgent function, after successful switch, add:

```typescript
toast.success(`Switched to ${newAgentId}`)
```

**Step 3: Verify build passes**

Run: `npm run build`
Expected: Build succeeds without errors

**Step 4: Manual test**

1. Start the app: `npm run dev`
2. Create a session
3. Click on the agent selector in the bottom left of the chat input
4. Select a different agent
5. Verify toast notification appears
6. Send a message to verify new agent responds

**Step 5: Commit**

```bash
git add src/renderer/src/hooks/useApp.ts
git commit -m "feat: add toast notification on agent switch"
```

---

### Task 8: Export AgentSelector from Components Index

**Files:**

- Modify: `src/renderer/src/components/index.ts` (add export)

**Step 1: Add export**

Add to the exports:

```typescript
export { AgentSelector } from './AgentSelector'
```

**Step 2: Verify build passes**

Run: `npm run build`
Expected: Build succeeds without errors

**Step 3: Commit**

```bash
git add src/renderer/src/components/index.ts
git commit -m "feat: export AgentSelector from components index"
```

---

## Summary

After completing all tasks, the chatbox will have an inline agent selector that:

- Shows current agent name with sparkle icon and dropdown chevron
- Displays available agents with green status dots (installed) or grey dots (needs setup)
- Disables uninstalled agents with tooltip
- Switches the current session's agent when selected
- Shows toast notification on successful switch
- Disables during processing to prevent mid-response switching
