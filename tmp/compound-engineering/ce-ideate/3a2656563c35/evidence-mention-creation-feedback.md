# Evidence Dossier: Mention Creation Feedback

## Mention Insertion
`/Users/fengzhao/multica/node_modules/@tiptap/extension-mention/dist/index.js:12-25`
```javascript
command: ({ editor, range, props }) => {
  const nodeAfter = editor.view.state.selection.$to.nodeAfter;
  const overrideSpace = nodeAfter?.text?.startsWith(" ");
  if (overrideSpace) range.to += 1;
  editor.chain().focus().insertContentAt(range, [
    { type: extensionName, attrs: { ...props, mentionSuggestionChar: char } },
    { type: "text", text: " " }
  ]).run();
  editor.view.dom.ownerDocument.defaultView?.getSelection()?.collapseToEnd();
}
```

## Suggestion Dropdown UI

### No Results State
`/Users/fengzhao/multica/packages/views/editor/extensions/mention-suggestion.tsx:302-314`
```typescript
if (orderedItems.length === 0) {
  const isWaitingForServer = normalizedQuery !== "" && (isSearching || searchedQuery !== normalizedQuery);
  return (
    <div className="rounded-md border bg-popover p-2 text-xs text-muted-foreground shadow-md">
      {isWaitingForServer ? t(($) => $.mention.searching) : t(($) => $.mention.no_results)}
    </div>
  );
}
```

### Selection State
`/Users/fengzhao/multica/packages/views/editor/extensions/mention-suggestion.tsx:149`
```typescript
const [selectedKey, setSelectedKey] = useState<string | null>(null);
```

### User Suggestion Item
`/Users/fengzhao/multica/packages/views/editor/extensions/mention-suggestion.tsx:454-484`
```typescript
<button className={`flex w-full items-center gap-2.5 px-3 py-1.5 text-left text-xs transition-colors ${selected ? "bg-accent" : "hover:bg-accent/50"}`} onClick={onSelect}>
  <ActorAvatar actorType={item.type} actorId={item.id} size={20} showStatusDot />
  <span className="truncate font-medium">{item.label}</span>
  {item.type === "agent" && <Badge variant="outline" className="ml-auto">Agent</Badge>}
  {item.type === "squad" && <Badge variant="outline" className="ml-auto">Squad</Badge>}
</button>
```

### Issue Suggestion Item
`/Users/fengzhao/multica/packages/views/editor/extensions/mention-suggestion.tsx:389-422`
```typescript
const isClosed = item.status === "done" || item.status === "cancelled";
<button className={`flex w-full items-center gap-2.5 px-3 py-2 text-left text-xs transition-colors ${selected ? "bg-accent" : "hover:bg-accent/50"} ${isClosed ? "opacity-60" : ""}`} onClick={onSelect}>
  <span className="flex h-7 w-7 shrink-0 items-center justify-center">
    {item.status ? <StatusIcon status={item.status} className="h-3.5 w-3.5" /> : <ListTodo className="h-3.5 w-3.5 text-muted-foreground" />}
  </span>
  <span className="min-w-0 flex-1">
    <span className="flex min-w-0 items-center gap-2">
      <span className="shrink-0 font-medium text-muted-foreground">{item.label}</span>
      {item.description && <span className={`truncate text-foreground ${isClosed ? "line-through" : ""}`}>{item.description}</span>}
    </span>
  </span>
</button>
```

## Query Filtering
`/Users/fengzhao/multica/packages/views/editor/extensions/mention-suggestion.tsx:511-520`
```typescript
function matchesMentionQuery(item: MentionItem, query: string): boolean {
  const q = query.trim().toLowerCase();
  if (!q) return true;
  return (
    item.label.toLowerCase().includes(q) ||
    item.description?.toLowerCase().includes(q) === true ||
    matchesPinyin(item.label, q) ||
    (item.description ? matchesPinyin(item.description, q) : false)
  );
}
```

## Mention Rendering
`/Users/fengzhao/multica/packages/views/editor/extensions/mention-view.tsx:24-48`
```typescript
export function MentionView({ node }: NodeViewProps) {
  const { type, id, label } = node.attrs;
  if (type === "issue") return (<NodeViewWrapper as="span" className="inline"><IssueMention issueId={id} fallbackLabel={label} /></NodeViewWrapper>);
  if (type === "project") return (<NodeViewWrapper as="span" className="inline"><ProjectMention projectId={id} fallbackLabel={label} /></NodeViewWrapper>);
  return (<NodeViewWrapper as="span" className="inline"><span className="mention">@{label ?? id}</span></NodeViewWrapper>);
}
```

## Mobile Suggestion Bar

### Mobile Empty State
`/Users/fengzhao/multica/apps/mobile/components/issue/mention-suggestion-bar.tsx:240-248`
```typescript
if (item.kind === "empty") return (<View className="px-3 py-3"><Text className="text-xs text-muted-foreground">No matches.</Text></View>);
```

### Mobile Agent Item
`/Users/fengzhao/multica/apps/mobile/components/issue/mention-suggestion-bar.tsx:291-309`
```typescript
<Pressable onPress={() => onSelect({ type: "agent", id: item.agent.id, name: item.agent.name })} className="flex-row items-center gap-3 px-3 py-2 active:bg-secondary">
  <ActorAvatar type="agent" id={item.agent.id} size={28} showPresence />
  <Text className="flex-1 text-sm text-foreground">{item.agent.name}</Text>
  <Badge label="Agent" tone="brand" />
</Pressable>
```

## Keyboard Interaction
`/Users/fengzhao/multica/packages/views/editor/extensions/suggestion-popup.tsx:24-33`
```typescript
export function isPickerAcceptKey(event: KeyboardEvent): boolean {
  if (event.key === "Enter") return true;
  return (event.key === "Tab" && !event.shiftKey && !event.ctrlKey && !event.metaKey && !event.altKey);
}
```

`/Users/fengzhao/multica/packages/views/editor/extensions/mention-suggestion.tsx:274-300`
```typescript
useImperativeHandle(ref, () => ({
  onKeyDown: ({ event }) => {
    if (isImeComposing(event)) return false;
    if (event.key === "ArrowUp") {
      setSelectedKey(mentionItemKey(orderedItems[(selectedIndex + orderedItems.length - 1) % orderedItems.length]!));
      return true;
    }
    if (event.key === "ArrowDown") {
      setSelectedKey(mentionItemKey(orderedItems[(selectedIndex + 1) % orderedItems.length]!));
      return true;
    }
    if (isPickerAcceptKey(event)) {
      selectItem(orderedItems[selectedIndex]);
      return true;
    }
    return false;
  },
}));
```

## Popup Positioning
`/Users/fengzhao/multica/packages/views/editor/extensions/mention-suggestion.tsx:351-368`
```typescript
<div className={cn("flex flex-col overflow-y-auto overscroll-contain border bg-popover py-1", contextLayout ? "max-h-[420px] w-96 rounded-lg shadow-xl" : "max-h-[300px] w-72 rounded-md shadow-md")}>
  {groups.map((group) => (
    <div key={group.label}>
      <div className="px-3 py-2 text-[11px] font-semibold uppercase tracking-wide text-muted-foreground/80">{groupLabel(group.label)}</div>
      {renderRows(group)}
    </div>
  ))}
</div>
```

---
**Entry Count:** 16 code snippets from 6 files
**Dossier Location:** `/tmp/compound-engineering/ce-ideate/3a2656563c35/evidence-mention-creation-feedback.md`
