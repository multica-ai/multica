# M3 Global Search — Implementation Plan

## Prerequisites

Spec file: `openspec/changes/m3-global-search/spec.md`

---

## Task Breakdown

### Task 1: Create `features/search/store.ts`

**File:** `apps/workspace/src/features/search/store.ts`

```typescript
// Zustand store for global search dialog open/close state
import { create } from "zustand";

interface SearchStore {
  isOpen: boolean;
  open: () => void;
  close: () => void;
  toggle: () => void;
}

export const useSearchStore = create<SearchStore>((set) => ({
  isOpen: false,
  open: () => set({ isOpen: true }),
  close: () => set({ isOpen: false }),
  toggle: () => set((s) => ({ isOpen: !s.isOpen })),
}));
```

---

### Task 2: Create `features/search/use-search-results.ts`

**File:** `apps/workspace/src/features/search/use-search-results.ts`

Hook that aggregates search results from:
- `useQuery` calling `api.listIssues({ search: query, limit: 8 })` — debounced 200ms
- `useQueryClient().getQueryData(queryKeys.projects.list(...))` — filtered client-side
- `useWorkspaceStore(s => s.members)` — filtered client-side
- Static actions array

Return shape:
```typescript
interface SearchResults {
  issues: Issue[];        // max 8
  projects: Project[];    // max 5
  members: MemberWithUser[]; // max 5
  actions: SearchAction[];
  isLoading: boolean;
}

interface SearchAction {
  id: string;
  label: string;
  shortcut?: string;
  onSelect: () => void;
}
```

Implementation notes:
- Use `useQuery` with `enabled: query.length > 0` for issues
- Use `useDebounce(query, 200)` or manual debounce ref for issue query
- Project filter: `project.title.toLowerCase().includes(query.toLowerCase())`
- Member filter: `(m.name + m.email).toLowerCase().includes(query.toLowerCase())`
- When query is empty: issues=[], projects=[], members=[], actions=full list

---

### Task 3: Create `features/search/global-search-dialog.tsx`

**File:** `apps/workspace/src/features/search/global-search-dialog.tsx`

Component using `CommandDialog` from `@/components/ui/command`:

```tsx
<CommandDialog open={isOpen} onOpenChange={(open) => !open && close()}>
  <Command shouldFilter={false}>  {/* we handle filtering ourselves */}
    <CommandInput
      placeholder="Search issues, projects, members..."
      value={query}
      onValueChange={setQuery}
    />
    <CommandList>
      <CommandEmpty>No results for "{query}"</CommandEmpty>
      
      {/* Issues group - only when query non-empty or isLoading */}
      {issues.length > 0 && (
        <CommandGroup heading="Issues">
          {issues.map(issue => (
            <CommandItem key={issue.id} onSelect={() => navigate(issue)}>
              <StatusIcon status={issue.status} />
              <span>{issue.identifier}</span>
              <span>{issue.title}</span>
            </CommandItem>
          ))}
        </CommandGroup>
      )}

      {/* Projects group */}
      {projects.length > 0 && (
        <CommandGroup heading="Projects">
          {projects.map(project => (
            <CommandItem key={project.id} onSelect={() => navigate(project)}>
              <FolderKanban />
              <span>{project.title}</span>
            </CommandItem>
          ))}
        </CommandGroup>
      )}

      {/* Members group */}
      {members.length > 0 && (
        <CommandGroup heading="Members">
          {members.map(member => (
            <CommandItem key={member.id} onSelect={() => navigate(member)}>
              <CircleUser />
              <span>{member.name}</span>
              <span className="text-muted-foreground">{member.email}</span>
            </CommandItem>
          ))}
        </CommandGroup>
      )}

      {/* Actions - always visible */}
      <CommandGroup heading="Actions">
        {actions.map(action => (
          <CommandItem key={action.id} onSelect={action.onSelect}>
            <span>{action.label}</span>
            {action.shortcut && <CommandShortcut>{action.shortcut}</CommandShortcut>}
          </CommandItem>
        ))}
      </CommandGroup>
    </CommandList>
  </Command>
</CommandDialog>
```

On select: call `close()` then navigate/execute.

---

### Task 4: Create `features/search/index.ts`

**File:** `apps/workspace/src/features/search/index.ts`

```typescript
export { useSearchStore } from "./store";
export { GlobalSearchDialog } from "./global-search-dialog";
```

---

### Task 5: Edit `features/layout/components/dashboard-layout.tsx`

Add:
1. Import `GlobalSearchDialog` from `@/features/search`
2. `useEffect` for Cmd+K / Ctrl+K listener:
   ```typescript
   useEffect(() => {
     const handler = (e: KeyboardEvent) => {
       if ((e.metaKey || e.ctrlKey) && e.key === "k") {
         e.preventDefault();
         useSearchStore.getState().toggle();
       }
     };
     document.addEventListener("keydown", handler);
     return () => document.removeEventListener("keydown", handler);
   }, []);
   ```
3. Mount `<GlobalSearchDialog />` inside the return (alongside `ModalRegistry`)

---

### Task 6: Edit `features/layout/components/app-sidebar.tsx`

In `SidebarHeader`, add a Search trigger button next to the existing new-issue button:

```tsx
<Tooltip>
  <TooltipTrigger
    className="flex h-7 w-7 items-center justify-center rounded-lg bg-background text-foreground shadow-sm hover:bg-accent"
    aria-label="Search"
    onClick={() => useSearchStore.getState().open()}
  >
    <Search className="size-3.5" />
  </TooltipTrigger>
  <TooltipContent side="bottom">Search (⌘K)</TooltipContent>
</Tooltip>
```

Import `Search` from `lucide-react` and `useSearchStore` from `@/features/search`.

---

## Dependency Order

```
Task 1 (store) → Task 2 (use-search-results) → Task 3 (dialog) → Task 4 (index)
                                                                          ↓
                                              Task 5 (dashboard-layout) ←┘
                                              Task 6 (app-sidebar)      ←┘
```

Tasks 1-4 are sequential. Tasks 5 and 6 can be done in parallel after Task 4.

---

## Verification Steps

1. `pnpm typecheck` — no type errors
2. `pnpm test` — existing tests still pass
3. Manual smoke test:
   - Press Cmd+K → dialog opens
   - Type "fix" → issues with "fix" in title appear
   - Type "back" → project "Backend" appears (if it exists)
   - Click an issue → navigates to `/issues/:id`, dialog closes
   - Press Escape → dialog closes
   - Click search icon in sidebar → dialog opens

---

## Risk / Blockers

| 风险 | 说明 | 缓解 |
|------|------|------|
| Issue search debounce | 每次击键触发 API 请求 | 200ms debounce + `enabled: debouncedQuery.length > 0` |
| Project query cache | dialog 打开时 query cache 可能为空 | 若 cache miss，fallback 到 `api.listProjects()` 的 useQuery |
| `shouldFilter={false}` on Command | cmdk 默认会自动过滤，需关闭 | 传 `shouldFilter={false}`，手动过滤 |
| CommandDialog 尺寸 | 默认宽度可能过窄 | 在 `DialogContent` 上加 `className="max-w-xl"` |

---

STATUS: WAITING FOR USER APPROVAL
