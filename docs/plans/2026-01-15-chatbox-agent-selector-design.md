# Chatbox Agent Selector Design

## Overview

Add an inline agent selector to the MessageInput chatbox, allowing users to switch the current session's agent mid-conversation.

## Behavior

- **Scope**: Changes the current session's agent (restarts agent process)
- **Disabled during**: Active processing only (users can switch when agent is idle)
- **No session**: Changes default agent for next session (syncs with localStorage)

## UI Design

### Trigger Button

- Position: Bottom-left of MessageInput, left of folder indicator
- Layout: `[Sparkles icon] [Agent name] [ChevronDown icon]`
- Style: Ghost button (transparent bg, subtle hover)
- Text: `text-sm text-muted-foreground`
- Icons: 14px, muted color

### Dropdown Menu

- Type: Radix UI DropdownMenu
- Position: Above trigger (`side="top"`)
- Width: Auto, min-width matches trigger

### Dropdown Items

- Layout: `[Agent name] [status dot]`
- Status dot: 8px circle
  - Green (`bg-green-500`): Installed
  - Grey (`bg-muted`): Needs setup
- Disabled items: Reduced opacity, tooltip explaining setup needed
- Selected item: Highlighted background

## Component Structure

### New Component: `AgentSelector.tsx`

```typescript
interface AgentSelectorProps {
  currentAgentId: string
  onAgentChange: (agentId: string) => void
  disabled?: boolean
}
```

### Integration

Add to MessageInput.tsx in the bottom toolbar, positioned left of folder indicator.

## Data Flow

1. Fetch agents via `window.electronAPI.checkAgents()` on mount
2. Store in local component state
3. On agent change:
   - Call `onAgentChange(newAgentId)`
   - Parent stops current agent process
   - Parent updates session's agentId
   - Parent starts new agent process (same working directory)
   - Show toast: "Switched to [Agent Name]"

## Edge Cases

| Scenario               | Behavior                               |
| ---------------------- | -------------------------------------- |
| No session active      | Changes default agent for next session |
| Agent switch fails     | Error toast, revert to previous agent  |
| All agents unavailable | Disabled selector with tooltip         |
| Loading agents         | Show spinner instead of chevron        |

## Files to Modify

1. **Create**: `src/renderer/src/components/AgentSelector.tsx`
2. **Modify**: `src/renderer/src/components/MessageInput.tsx` - Add AgentSelector to bottom toolbar
3. **Modify**: `src/renderer/src/hooks/useApp.ts` - Add `switchAgent` function
4. **Modify**: `src/renderer/src/App.tsx` - Wire up agent switching to session management
