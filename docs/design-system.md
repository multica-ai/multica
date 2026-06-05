# Multica Design System — Built-in Agent UI Components

> Generated 2026-06-04. All patterns reference existing shadcn components and design tokens.
> This spec covers 5 new components for the built-in agent feature.

---

## Design Tokens Reference

All designs use the **existing** Tailwind semantic tokens from the project — no new colors or spacing values are introduced.

| Category | Token | Purpose |
|---|---|---|
| Background | `bg-card` | Card surfaces |
| Background | `bg-background` | Page / section backgrounds |
| Background | `bg-muted` | Skeleton, subtle areas |
| Background | `bg-muted/30` | Read-only banners |
| Background | `bg-accent/40` | Card hover states |
| Text | `text-foreground` | Primary text |
| Text | `text-muted-foreground` | Secondary text, descriptions |
| Text | `text-muted-foreground/70` | Counts, tertiary info |
| Text | `text-destructive` | Destructive actions, errors |
| Border | `border-border` | Default borders |
| Border | `border-primary/30` | Card hover border accent |
| Border | `border-dashed` | Read-only banner style |
| Interactive | `hover:bg-accent/40` | Hover background |
| Interactive | `hover:border-primary/30` | Hover border accent |
| Accent | `text-amber-500` | Built-in indicator (Zap icon) |
| Accent | `text-primary` | Active states, interactive icons |
| Accent | `bg-primary/5` | Selected visibility card |

---

## Component 1: BuiltinAgentCard

### Visual Design

Grid card rendered above the agent table. Follows the **workflow template card pattern exactly** from `workflows-page.tsx:273-290`.

```
+-------------------------------------------+
| [Zap]  Agent Name                    [Badge] |
|        Description text line-clamp-2...   |
|        [icon] Footer hint text            |
+-------------------------------------------+
```

### Exact Classes

**Card wrapper** (button element):
```
flex flex-col items-start gap-1.5 rounded-lg border px-4 py-3 text-left
transition-colors hover:bg-accent/40 hover:border-primary/30
```

**Header row** (icon + name):
```
flex items-center gap-2 w-full
```

**Icon**: `<Zap className="h-4 w-4 shrink-0 text-primary" />` — using `Zap` from lucide-react. Purple/primary color distinguishes built-in agents from regular agents (which use `Bot` icon).

**Agent name**:
```
text-sm font-medium truncate
```

**Description**:
```
text-xs text-muted-foreground line-clamp-2
```

**Footer** (optional, shows agent category or skill count):
```
flex items-center gap-1 text-[10px] text-muted-foreground mt-0.5
```

### Badge

Positioned at top-right of card. Use shadcn `Badge` with `variant="outline"`:
```
<Badge variant="outline" className="shrink-0 ml-auto">
  <Zap className="h-3 w-3 text-amber-500" />
  内置
</Badge>
```

Badge gets `ml-auto` to push to the right edge within the header row. Icon inside badge uses `text-amber-500` for the built-in indicator color (matches the workflow template section header pattern at `workflows-page.tsx:263`).

### Interaction States

| State | Visual |
|---|---|
| Default | `rounded-lg border bg-card` |
| Hover | `hover:bg-accent/40 hover:border-primary/30` |
| Focus-visible | `focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none` |
| Click | Navigates to `paths.agentDetail(agent.id)` |

### Component Signature

```typescript
interface BuiltinAgentCardProps {
  agent: Agent;                 // The built-in agent object
  onClick: (agentId: string) => void;  // Navigation handler
}
```

---

## Component 2: BuiltinAgentCardSection

### Visual Design

Section wrapper placed between `PageHeaderBar` and the search/filter toolbar (`ActiveToolbarRow`).

```
+---------------------------------------------------+
| [Zap] 内置 Agent  (3)                              |  <- Section label
|                                                    |
| +---------------+ +---------------+ +---------------+ |
| | BuiltinCard 1 | | BuiltinCard 2 | | BuiltinCard 3 | |
| +---------------+ +---------------+ +---------------+ |
+---------------------------------------------------+
```

### Exact Classes

**Section container**:
```
px-5 py-3 border-b
```

(Matches the workflow template section container at `workflows-page.tsx:261`: `px-5 py-3 border-b`)

**Section header row**:
```
flex items-center gap-2 mb-2
```

**Section label icon**:
```tsx
<Zap className="h-3.5 w-3.5 text-amber-500" />
```
(Exact match with `workflows-page.tsx:263`)

**Section label text**:
```
text-xs font-medium text-muted-foreground uppercase tracking-wider
```
(Exact match with `workflows-page.tsx:264`)

**Count badge** (optional, shows number of built-in agents):
```
font-mono text-[10px] tabular-nums text-muted-foreground/70
```
(Same pattern as skill counts in `agent-detail-inspector.tsx:209`)

**Grid**:
```
grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3
```
(Exact match with `workflows-page.tsx:266`)

### Conditional Rendering

- **Hidden** when `builtinAgents.length === 0` (return `null`)
- **Loading**: Show `Skeleton` cards matching grid dimensions:
  ```tsx
  <Skeleton className="h-[88px] w-full rounded-lg" />
  ```
  (88px height matches: 4px top padding + ~24px header row + ~36px description (2 lines) + 16px bottom padding + borders)

### Placement in AgentsPage

In `agents-page.tsx`, this section renders between `PageHeaderBar` (line 398-401) and the toolbar container (line 403). Exact insertion point:

```tsx
<PageHeaderBar ... />

{/* Built-in agent cards — between header and toolbar */}
<BuiltinAgentCardSection
  agents={builtinAgents}
  loading={builtinLoading}
  onCardClick={(id) => navigation.push(paths.agentDetail(id))}
/>

<div className="flex flex-1 min-h-0 flex-col gap-4 p-6">
```

### Component Signature

```typescript
interface BuiltinAgentCardSectionProps {
  agents: Agent[];                        // Filtered list of built-in agents
  loading: boolean;                       // Show skeleton cards
  onCardClick: (agentId: string) => void; // Navigation handler
}
```

---

## Component 3: RuntimeSelectDialog

### Visual Design

Dialog for selecting a runtime when executing a built-in agent.

```
+---------------------------------------------------+
| 选择运行时                                         |  [X]
|                                                    |
| 为 Agent Name 选择一个运行时来执行操作。             |
|                                                    |
| +---------------------------------------------------+ |
| | (o) My MacBook Pro                          [green]| |
| |     online · local                                  | |
| +---------------------------------------------------+ |
| | ( ) Windows Desktop                      [gray]   | |
| |     offline · local                                 | |
| +---------------------------------------------------+ |
|                                                    |
|                          [取消]        [确认执行]    |
+---------------------------------------------------+
```

### Exact Classes

**Dialog**: Use shadcn `Dialog` with `DialogContent`:
```
<DialogContent className="sm:max-w-md" showCloseButton={false}>
```

**Header**:
```tsx
<DialogHeader>
  <DialogTitle className="text-sm font-semibold">
    选择运行时
  </DialogTitle>
  <DialogDescription className="text-xs">
    为 <strong>{agentName}</strong> 选择一个运行时来执行操作。
  </DialogDescription>
</DialogHeader>
```

**Runtime list**: Use shadcn `RadioGroup`:
```
<RadioGroup className="grid w-full gap-2" value={selectedRuntimeId} onValueChange={setSelectedRuntimeId}>
```

**Radio item** (each runtime row):
```
flex items-center gap-3 rounded-lg border px-4 py-3 transition-colors
cursor-pointer hover:bg-accent/40
```

**Radio item — selected state**:
```
border-primary bg-primary/5
```
(Matches visibility selector pattern in `create-agent-dialog.tsx:300-303`)

**Radio indicator**: Use shadcn `RadioGroupItem`:
```tsx
<RadioGroupItem value={runtime.id} className="shrink-0" />
```

**Runtime info stack**:
```
flex flex-col min-w-0 gap-0.5
```

**Device name**:
```
text-sm font-medium truncate
```

**Status line** (device type + online/offline):
```
flex items-center gap-1.5 text-xs text-muted-foreground
```

**Status dot** (online/offline indicator):
```
h-1.5 w-1.5 rounded-full
```
- Online: `bg-emerald-500` (matches `STATUS_COLOR.active` from workflows)
- Offline: `bg-muted-foreground/40`

**Status text**: `text-xs text-muted-foreground`

**Device type icon** (right-aligned, shows monitor/laptop based on device_info):
```
h-4 w-4 text-muted-foreground shrink-0 ml-auto
```

### Footer

Use inline footer (NOT `DialogFooter` — same pattern as `create-agent-dialog.tsx:384`):
```
flex items-center justify-end gap-2 border-t bg-background px-5 py-3
```

**Cancel button**:
```tsx
<Button variant="ghost" size="sm" onClick={onClose}>取消</Button>
```

**Confirm button**:
```tsx
<Button size="sm" disabled={!selectedRuntimeId} onClick={onConfirm}>
  确认执行
</Button>
```

### Empty State

When no runtimes available:
```
flex flex-col items-center justify-center gap-3 py-10 text-center
```

```tsx
<Monitor className="h-8 w-8 text-muted-foreground/40" />
<p className="text-sm text-muted-foreground">没有可用的运行时</p>
<p className="text-xs text-muted-foreground/70 max-w-xs">
  请先连接一个运行时设备后再执行内置 Agent。
</p>
```

### Loading State

```tsx
<div className="grid w-full gap-2">
  {Array.from({ length: 3 }).map((_, i) => (
    <Skeleton key={i} className="h-[60px] w-full rounded-lg" />
  ))}
</div>
```

### Component Signature

```typescript
interface RuntimeSelectDialogProps {
  agentName: string;
  runtimes: AgentRuntime[];
  loading: boolean;
  onConfirm: (runtimeId: string) => void;
  onClose: () => void;
}
```

---

## Component 4: Promote/Demote Flow

### 4a. Promote Confirm Dialog

```
+---------------------------------------------------+
| [!]  提升为内置 Agent                               |
|                                                    |
| 将 "Agent Name" 提升为内置 Agent 后，它将：          |
|                                                    |
| +---------------------------------------------------+ |
| | 全局可见 · 在所有工作区中作为模板显示             | |
| | 移除运行时绑定 · 内置 Agent 不与特定运行时绑定    | |
| +---------------------------------------------------+ |
|                                                    |
| 此操作仅管理员可执行，提升后不可撤销。               |
|                          [取消]          [确认提升]  |
+---------------------------------------------------+
```

**Exact classes** — follows the archive confirmation dialog pattern from `agent-detail-page.tsx:301-342`:

```tsx
<Dialog open onOpenChange={onClose}>
  <DialogContent className="max-w-sm" showCloseButton={false}>
    <div className="flex items-center gap-3">
      <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-amber-500/10">
        <Zap className="h-5 w-5 text-amber-500" />
      </div>
      <DialogHeader className="flex-1 gap-1">
        <DialogTitle className="text-sm font-semibold">
          提升为内置 Agent
        </DialogTitle>
        <DialogDescription className="text-xs">
          此操作将使该 Agent 全局可见，并移除其运行时绑定。
        </DialogDescription>
      </DialogHeader>
    </div>

    {/* Warning points */}
    <div className="flex flex-col gap-2 rounded-md border bg-muted/30 px-3 py-2.5 text-xs text-muted-foreground">
      <div className="flex items-center gap-2">
        <Globe className="h-3.5 w-3.5 shrink-0" />
        <span>全局可见 — 在所有工作区中作为模板显示</span>
      </div>
      <div className="flex items-center gap-2">
        <Unplug className="h-3.5 w-3.5 shrink-0" />
        <span>移除运行时绑定 — 内置 Agent 不与特定运行时绑定</span>
      </div>
    </div>

    <p className="text-xs text-muted-foreground">
      此操作仅管理员可执行，提升后不可撤销。
    </p>

    <DialogFooter>
      <Button variant="ghost" size="sm" onClick={onClose}>取消</Button>
      <Button variant="default" size="sm" onClick={onConfirm}>确认提升</Button>
    </DialogFooter>
  </DialogContent>
</Dialog>
```

**Note**: For promote, the confirm button uses `variant="default"` (not destructive — promote is a positive action), but the icon container uses `bg-amber-500/10` with `text-amber-500` to indicate "caution / important change."

### 4b. Demote Confirm Dialog

```
+---------------------------------------------------+
| [!]  降级为普通 Agent                               |
|                                                    |
| 将 "Agent Name" 降级后，它将：                       |
|                                                    |
| +---------------------------------------------------+ |
| | 失去全局可见性 · 仅当前工作区可见                 | |
| | 需要绑定运行时 · 降级后需手动分配运行时           | |
| +---------------------------------------------------+ |
|                                                    |
| 当前正在使用此模板的工作区中的副本不受影响。         |
|                          [取消]          [确认降级]  |
+---------------------------------------------------+
```

**Exact classes** — same pattern, different icon/colors:

- Icon container: `bg-destructive/10` with `text-destructive` (demote is a destructive/reductive action)
- Icon: `<ArrowDown className="h-5 w-5 text-destructive" />`
- Confirm button: `variant="destructive"`
- Warning points: replace Globe/Unplug with `EyeOff` (lose visibility) + `Plug` (need runtime)

### 4c. Read-only Banner (Non-admin viewing built-in agent)

Shown at top of agent detail page for non-admins. Amber/warning style — distinct from the existing `CapabilityBanner` (which uses dashed border + muted background).

```tsx
<div
  role="status"
  className="flex items-center gap-2 rounded-md border border-amber-500/20 bg-amber-500/5 px-3 py-2 text-xs"
>
  <Info className="h-3.5 w-3.5 shrink-0 text-amber-500" />
  <span className="text-amber-700 dark:text-amber-400">
    内置 Agent — 仅管理员可编辑
  </span>
</div>
```

**Distinction from `CapabilityBanner`**:
| Aspect | CapabilityBanner | Built-in Read-only Banner |
|---|---|---|
| Border | `border-dashed` | `border border-amber-500/20` |
| Background | `bg-muted/30` | `bg-amber-500/5` |
| Icon | `Lock` (muted) | `Info` (amber) |
| Text color | `text-muted-foreground` | `text-amber-700 dark:text-amber-400` |
| Purpose | Permission gate | Built-in agent awareness |

### Placement in AgentDetailPage

The banner renders **after** `DetailHeader` and **before** the `canEdit` `CapabilityBanner` (since they convey different things):

```tsx
<DetailHeader ... />

{/* Built-in read-only banner — amber, always shown for non-admins */}
{agent.is_builtin && !currentUserIsAdmin && (
  <BuiltinReadOnlyBanner />
)}

{/* Permission gate — only when canEdit fails */}
{!canEdit.allowed && (
  <CapabilityBanner reason={canEdit.reason} ... />
)}
```

---

## Component 5: Agent Detail Page Read-only Mode

### Visual Design

When a non-admin views a built-in agent, the entire detail page becomes read-only.

### 5a. Banner

As defined in Component 4c above. Rendered at the top of the detail page content area (between header and inspector+overview grid).

### 5b. Form Fields — Disabled State

All form fields in `AgentDetailInspector` receive disabled styling. The inspector already supports `canEdit={false}` mode (see `agent-detail-inspector.tsx:70`), which renders static read-only displays. For built-in agents, the same `canEdit` prop is set to `false`.

**Existing patterns already in place** (no new classes needed):

- **Avatar**: Static display, no hover overlay, no click handler (`agent-detail-inspector.tsx:270-281`)
- **Name/Description**: Static text display, no pencil icon, no popover trigger (`agent-detail-inspector.tsx:343-360`)
- **Pickers** (RuntimePicker, ModelPicker, etc.): Each already accepts `canEdit` prop and renders as text/badge when false

**Additional disabled styling for built-in read-only mode** (to visually distinguish from regular permission-based read-only):

The wrapper `aside` element gets:
```
opacity-70 cursor-not-allowed
```

Applied to the inspector's root container when the agent is built-in AND the user is non-admin:
```tsx
<aside
  className={cn(
    "flex w-full flex-col rounded-lg border bg-background md:h-full md:min-h-0 md:overflow-y-auto",
    isBuiltinReadOnly && "opacity-70"
  )}
>
```

Individual interactive elements within the disabled inspector also get:
```
pointer-events-none
```
(applied via the existing `canEdit={false}` prop — no additional work needed)

### 5c. Kebab Menu — Restricted

When `isBuiltinReadOnly`, the kebab menu in `DetailHeader` shows only "View details" action:

```tsx
{!isArchived && (canArchive || isBuiltinReadOnly) && (
  <DropdownMenu>
    <DropdownMenuTrigger render={<Button variant="ghost" size="icon-sm" />}>
      <MoreHorizontal className="h-4 w-4 text-muted-foreground" />
    </DropdownMenuTrigger>
    <DropdownMenuContent align="end" className="w-auto">
      {isBuiltinReadOnly ? (
        <DropdownMenuItem onClick={() => setIsViewDetailsOpen(true)}>
          <Eye className="h-3.5 w-3.5" />
          查看详情
        </DropdownMenuItem>
      ) : (
        <>
          <DropdownMenuItem ...>编辑</DropdownMenuItem>
          <DropdownMenuItem className="text-destructive" onClick={onArchive}>
            <Trash2 className="h-3.5 w-3.5" />
            归档
          </DropdownMenuItem>
        </>
      )}
    </DropdownMenuContent>
  </DropdownMenu>
)}
```

### 5d. Save/Cancel Buttons — Hidden

The overview pane tabs (Instructions, Env, Custom Args) each have Save/Cancel buttons. When `isBuiltinReadOnly` is true, these buttons are **not rendered**. Each tab component already accepts a `readOnly` or similar prop; pass `isBuiltinReadOnly` to suppress the save actions.

### 5e. Complete Decision Matrix

| User Role | Agent Type | canEdit | Builtin Banner | Capability Banner | Kebab Menu | Fields |
|---|---|---|---|---|---|---|
| Admin/Owner | Built-in | `true` | Hidden | Hidden | Edit + Archive | Editable |
| Admin/Owner | Regular | `true` | Hidden | Hidden | Edit + Archive | Editable |
| Member | Built-in | `false` | **Amber banner** | Hidden | View details only | Disabled (70% opacity) |
| Member | Regular (own) | `true` | Hidden | Hidden | Edit + Archive | Editable |
| Member | Regular (other's private) | `false` | Hidden | Shown | Hidden | Disabled (via canEdit) |

---

## Typography Scale (Existing)

All text sizes conform to the existing project scale:

| Size | Class | Usage |
|---|---|---|
| `text-base` | `font-semibold` | Agent name in inspector (read-only) |
| `text-sm` | `font-medium` | Card names, dialog titles, section headers |
| `text-sm` | (regular) | Card descriptions |
| `text-xs` | `text-muted-foreground` | Descriptions, hints, field labels |
| `text-[10px]` | `uppercase tracking-wider` | Section labels (Properties, Details) |
| `text-[10px]` | `font-mono tabular-nums` | Counts, badges |

## Spacing Scale (Existing)

| Spacing | Class | Usage |
|---|---|---|
| `p-6` / `px-6` | Page padding | Main content area |
| `p-5` / `px-5` | Section padding | Inspector sections, headers |
| `py-4` / `px-4` | Inner section | Inspector property groups |
| `p-3` / `py-3` | Compact section | Toolbars, banners |
| `gap-4` | Large gap | Between major sections |
| `gap-3` | Medium gap | Grid cards, content stacks |
| `gap-2` | Standard gap | Form rows, list items |
| `gap-1.5` | Tight gap | Card internals, icon+text pairs |
| `gap-1` | Minimal gap | Badge internals |

---

## Interaction States Summary

| Component | Default | Hover | Active/Selected | Disabled | Loading |
|---|---|---|---|---|---|
| BuiltinAgentCard | `border bg-card` | `bg-accent/40 border-primary/30` | Navigate to detail | N/A | `<Skeleton className="h-[88px] rounded-lg" />` |
| Runtime radio item | `border` | `bg-accent/40` | `border-primary bg-primary/5` | `opacity-50 cursor-not-allowed` | `<Skeleton className="h-[60px] rounded-lg" />` |
| Read-only banner | `border-amber-500/20 bg-amber-500/5` | Static | N/A | N/A | N/A |
| Disabled inspector | `opacity-70` | Static | N/A | `cursor-not-allowed pointer-events-none` | N/A |
| Promote button | `variant="default"` | `hover:bg-primary/80` | Opens confirm dialog | `disabled:opacity-50` | Spinner in button |
| Demote button | `variant="destructive"` | `hover:bg-destructive/80` | Opens confirm dialog | `disabled:opacity-50` | Spinner in button |

---

## Icons Reference

| Icon | Lucide Import | Usage | Color |
|---|---|---|---|
| `Zap` | `lucide-react` | Built-in agent indicator | `text-amber-500` (section header), `text-primary` (card icon) |
| `Bot` | `lucide-react` | Regular agent icon | `text-muted-foreground` |
| `Monitor` | `lucide-react` | Runtime device type | `text-muted-foreground` |
| `Info` | `lucide-react` | Read-only banner | `text-amber-500` |
| `Eye` | `lucide-react` | "View details" menu item | `text-muted-foreground` |
| `Globe` | `lucide-react` | Promote — global visibility point | `text-muted-foreground` |
| `Unplug` | `lucide-react` | Promote — runtime removal point | `text-muted-foreground` |
| `EyeOff` | `lucide-react` | Demote — lose visibility point | `text-muted-foreground` |
| `Plug` | `lucide-react` | Demote — need runtime point | `text-muted-foreground` |
| `ArrowDown` | `lucide-react` | Demote icon | `text-destructive` |
| `MoreHorizontal` | `lucide-react` | Kebab menu trigger | `text-muted-foreground` |
| `Trash2` | `lucide-react` | Archive menu item | `text-destructive` |
| `AlertCircle` | `lucide-react` | Error states | `text-destructive` |

---

## Component File Organization

Following existing patterns:
- `packages/views/agents/components/builtin-agent-card.tsx` — `BuiltinAgentCard`
- `packages/views/agents/components/builtin-agent-card-section.tsx` — `BuiltinAgentCardSection`
- `packages/views/agents/components/runtime-select-dialog.tsx` — `RuntimeSelectDialog`
- `packages/views/agents/components/promote-dialog.tsx` — `PromoteConfirmDialog`
- `packages/views/agents/components/demote-dialog.tsx` — `DemoteConfirmDialog`
- `packages/views/agents/components/builtin-read-only-banner.tsx` — `BuiltinReadOnlyBanner`

All components are `"use client"` and follow the existing import patterns (lucide-react icons, `@multica/ui` shadcn components, `@multica/core` types/queries/stores).
