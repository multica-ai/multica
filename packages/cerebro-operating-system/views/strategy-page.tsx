"use client";

import { useState, type KeyboardEvent } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  DndContext, DragOverlay, KeyboardSensor, PointerSensor, closestCorners, useDroppable, useSensor, useSensors,
  type CollisionDetection, type DragEndEvent, type DragStartEvent,
} from "@dnd-kit/core";
import {
  SortableContext, horizontalListSortingStrategy, sortableKeyboardCoordinates, useSortable, verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import { ChevronDown, ChevronUp, GripVertical, ListTodo, Plus, Trash2 } from "lucide-react";
import { DEFAULT_TERMINOLOGY } from "../core/api-schemas";
import {
  periodsOptions, rocksOptions, settingsOptions, useCreateConnection, useCreateVisionPlanItem,
  useCreateVisionPlanSection, useDeleteConnection, useDeleteVisionPlanItem, useDeleteVisionPlanSection,
  useSaveRock, useUpdateVisionPlanItem, useUpdateVisionPlanSection, visionPlanOptions,
} from "../core/queries";
import { moveItem, moveSection } from "../core/strategy-board";
import type { Rock, VisionPlanItem, VisionPlanItemInput, VisionPlanSection } from "../core/types";
import { SearchSelect, type SearchSelectOption } from "./search-select";
import { StrategyMap } from "./strategy-map";

const MARKETING_PARTS = ["Target market", "Differentiators", "Proven process", "Guarantee"];
const SECTION_PREFIX = "section-";
const ITEM_PREFIX = "item-";
const COLUMN_PREFIX = "column-";

const itemInput = (item: VisionPlanItem, overrides: Partial<VisionPlanItemInput> = {}): VisionPlanItemInput => ({
  section_id: item.section_id, title: item.title, description: item.description,
  part_label: item.part_label, owner_type: item.owner_type, owner_id: item.owner_id,
  position: item.position, state: item.state, ...overrides,
});

const sectionInputFrom = (section: VisionPlanSection, position: number) => ({
  name: section.name, section_type: section.section_type, position,
});

interface PlanItemProps {
  item: VisionPlanItem;
  itemIndex: number;
  siblingItems: VisionPlanItem[];
  section: VisionPlanSection;
  wsId: string;
  goals: Rock[];
  ownerOptions: SearchSelectOption[];
  currentPeriodId?: string;
  allowGoalConnections: boolean;
}

function PlanItem({ item, itemIndex, siblingItems, section, wsId, goals, ownerOptions, currentPeriodId, allowGoalConnections }: PlanItemProps) {
  const update = useUpdateVisionPlanItem(wsId);
  const remove = useDeleteVisionPlanItem(wsId);
  const connect = useCreateConnection(wsId);
  const disconnect = useDeleteConnection(wsId);
  const saveGoal = useSaveRock(wsId);
  const [title, setTitle] = useState(item.title);
  const [description, setDescription] = useState(item.description);
  const [partLabel, setPartLabel] = useState(item.part_label ?? "");
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: `${ITEM_PREFIX}${item.id}` });

  function save(overrides: Partial<VisionPlanItemInput> = {}) {
    if (!title.trim()) return;
    update.mutate({ id: item.id, input: itemInput(item, { title: title.trim(), description: description.trim(), part_label: partLabel.trim(), ...overrides }) });
  }

  function changeGoals(goalIds: string[]) {
    const existing = new Map(item.goal_connections.map((connection) => [connection.goal_id, connection.connection_id]));
    for (const goalId of goalIds) {
      if (!existing.has(goalId)) connect.mutate({ source_type: "strategy_item", source_id: item.id, target_type: "rock", target_id: goalId, relationship_type: "supports", provenance: "manual" });
    }
    for (const [goalId, connectionId] of existing) {
      if (!goalIds.includes(goalId)) disconnect.mutate(connectionId);
    }
  }

  function createLinkedGoal(query: string) {
    if (!currentPeriodId) return;
    saveGoal.mutate({ input: {
      title: query || title, description, period_id: currentPeriodId, confidence: 50,
      reported_health: "unset", project_ids: [], issue_ids: [], strategy_item_id: item.id,
    } });
  }

  function move(direction: -1 | 1) {
    const other = siblingItems[itemIndex + direction];
    if (!other) return;
    update.mutate({ id: item.id, input: itemInput(item, { title: title.trim(), description: description.trim(), part_label: partLabel.trim(), position: other.position }) });
    update.mutate({ id: other.id, input: itemInput(other, { position: item.position }) });
  }

  const selectedGoals = item.goal_connections.map((connection) => connection.goal_id);
  const connectedGoals = selectedGoals.map((goalId) => goals.find((goal) => goal.id === goalId)).filter((goal): goal is Rock => Boolean(goal));
  const style = { transform: CSS.Transform.toString(transform), transition };
  return (
    <div ref={setNodeRef} style={style} className={`group grid gap-2 rounded-lg border bg-background p-3 ${isDragging ? "opacity-50" : ""}`}>
      <div className="flex items-start gap-2">
        <button type="button" aria-label={`Reorder ${item.title}`} className="mt-0.5 grid size-8 shrink-0 cursor-grab touch-none place-items-center rounded text-muted-foreground opacity-40 hover:bg-muted group-hover:opacity-100" {...attributes} {...listeners}><GripVertical aria-hidden className="size-4" /></button>
        <div className="min-w-0 flex-1">
          {section.section_type === "structured" && (
            <input aria-label={`${item.title} part`} value={partLabel} onChange={(event) => setPartLabel(event.target.value)} onBlur={() => save()} placeholder="Part label" className="mb-1 w-full bg-transparent text-xs font-semibold uppercase tracking-wide text-muted-foreground outline-none" />
          )}
          <input aria-label={`${item.title} title`} value={title} onChange={(event) => setTitle(event.target.value)} onBlur={() => save()} className="w-full bg-transparent text-sm font-medium outline-none focus:ring-1 focus:ring-ring" />
          <textarea aria-label={`${item.title} description`} value={description} onChange={(event) => setDescription(event.target.value)} onBlur={() => save()} rows={description ? 2 : 1} placeholder="Add context…" className="mt-1 w-full resize-none bg-transparent text-xs leading-relaxed text-muted-foreground outline-none focus:ring-1 focus:ring-ring" />
        </div>
        <div className="flex shrink-0 items-start opacity-60 group-hover:opacity-100">
          <button type="button" aria-label={`Move ${item.title} up`} disabled={itemIndex === 0} onClick={() => move(-1)} className="grid size-8 place-items-center rounded text-muted-foreground hover:bg-muted disabled:opacity-30"><ChevronUp aria-hidden className="size-4" /></button>
          <button type="button" aria-label={`Move ${item.title} down`} disabled={itemIndex === siblingItems.length - 1} onClick={() => move(1)} className="grid size-8 place-items-center rounded text-muted-foreground hover:bg-muted disabled:opacity-30"><ChevronDown aria-hidden className="size-4" /></button>
          <button type="button" aria-label={`Delete ${item.title}`} onClick={() => remove.mutate(item.id)} className="grid size-8 place-items-center rounded text-muted-foreground hover:bg-muted hover:text-destructive"><Trash2 aria-hidden className="size-4" /></button>
        </div>
      </div>
      {section.section_type === "process" && (
        <SearchSelect compact label={`${item.title} owner`} options={ownerOptions} value={item.owner_id ? `${item.owner_type}:${item.owner_id}` : ""} onChange={(value) => {
          const [ownerType, ownerId] = value.split(":");
          save({ owner_type: ownerId ? ownerType as "member" | "agent" : undefined, owner_id: ownerId || undefined });
        }} clearLabel="No owner" placeholder="Process owner" />
      )}
      {allowGoalConnections && (
        <div className="grid gap-1.5">
          <SearchSelect compact multiple label={`${item.title} Goals`} options={goals.map((goal) => ({ value: goal.id, label: goal.title }))} values={selectedGoals} onValuesChange={changeGoals} placeholder="Connect Goals" actionLabel={currentPeriodId ? "Create linked Goal" : undefined} onAction={currentPeriodId ? createLinkedGoal : undefined} />
          {connectedGoals.length > 0 && (
            <ul aria-label={`${item.title} connected Goals`} className="flex flex-wrap gap-1.5">
              {connectedGoals.map((goal) => (
                <li key={goal.id}>
                  <button type="button" aria-label={`Disconnect ${goal.title}`} onClick={() => changeGoals(selectedGoals.filter((id) => id !== goal.id))} className="flex max-w-full items-center gap-1 rounded-full border bg-muted/40 px-2 py-0.5 text-xs">
                    <span className="truncate">{goal.title}</span>
                    <span className="inline-flex items-center gap-0.5 text-muted-foreground"><ListTodo aria-hidden className="size-3" />{goal.issue_count}</span>
                    <span aria-hidden className="text-muted-foreground">×</span>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </div>
  );
}

interface PlanSectionProps {
  section: VisionPlanSection;
  index: number;
  sections: VisionPlanSection[];
  wsId: string;
  goals: Rock[];
  ownerOptions: SearchSelectOption[];
  currentPeriodId?: string;
}

function PlanSection({ section, index, sections, wsId, goals, ownerOptions, currentPeriodId }: PlanSectionProps) {
  const createItem = useCreateVisionPlanItem(wsId);
  const updateSection = useUpdateVisionPlanSection(wsId);
  const deleteSection = useDeleteVisionPlanSection(wsId);
  const [name, setName] = useState(section.name);
  const [newItem, setNewItem] = useState("");
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: `${SECTION_PREFIX}${section.id}` });
  const { setNodeRef: setDropRef } = useDroppable({ id: `${COLUMN_PREFIX}${section.id}` });

  function addItem(title: string, partLabel?: string) {
    if (!title.trim()) return;
    createItem.mutate({ section_id: section.id, title: title.trim(), part_label: partLabel, position: section.items.length, state: "active" });
    setNewItem("");
  }

  function onNewItemKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (event.key === "Enter") { event.preventDefault(); addItem(newItem); }
  }

  function move(direction: -1 | 1) {
    const other = sections[index + direction];
    if (!other) return;
    updateSection.mutate({ id: section.id, input: sectionInputFrom(section, other.position) });
    updateSection.mutate({ id: other.id, input: sectionInputFrom(other, section.position) });
  }

  const missingParts = section.section_type === "structured" ? MARKETING_PARTS.filter((part) => !section.items.some((item) => item.part_label === part)) : [];
  const activeItems = section.items.filter((item) => item.state === "active").sort((a, b) => a.position - b.position);
  const allowGoalConnections = section.key === "one-year-plan";
  const style = { transform: CSS.Transform.toString(transform), transition };
  return (
    <section ref={setNodeRef} style={style} className={`flex w-full shrink-0 flex-col rounded-xl border bg-card p-4 shadow-sm md:w-80 ${isDragging ? "opacity-50" : ""}`}>
      <header className="flex items-center gap-1 border-b pb-3">
        <button type="button" aria-label={`Reorder ${section.name}`} className="grid size-8 shrink-0 cursor-grab touch-none place-items-center rounded text-muted-foreground hover:bg-muted" {...attributes} {...listeners}><GripVertical aria-hidden className="size-4" /></button>
        <input aria-label={`${section.name} section name`} value={name} onChange={(event) => setName(event.target.value)} onBlur={() => updateSection.mutate({ id: section.id, input: sectionInputFrom({ ...section, name: name.trim() || section.name }, section.position) })} className="min-w-0 flex-1 bg-transparent text-base font-semibold outline-none focus:ring-1 focus:ring-ring" />
        <button type="button" aria-label={`Move ${section.name} up`} disabled={index === 0} onClick={() => move(-1)} className="grid size-8 place-items-center rounded text-muted-foreground hover:bg-muted disabled:opacity-30"><ChevronUp aria-hidden className="size-4" /></button>
        <button type="button" aria-label={`Move ${section.name} down`} disabled={index === sections.length - 1} onClick={() => move(1)} className="grid size-8 place-items-center rounded text-muted-foreground hover:bg-muted disabled:opacity-30"><ChevronDown aria-hidden className="size-4" /></button>
        <button type="button" aria-label={`Delete ${section.name} section`} onClick={() => { if (window.confirm(`Delete ${section.name} and its items?`)) deleteSection.mutate(section.id); }} className="grid size-8 place-items-center rounded text-muted-foreground hover:bg-muted hover:text-destructive"><Trash2 aria-hidden className="size-4" /></button>
      </header>
      <div ref={setDropRef} className="mt-3 grid flex-1 content-start gap-2">
        <SortableContext items={activeItems.map((item) => `${ITEM_PREFIX}${item.id}`)} strategy={verticalListSortingStrategy}>
          {activeItems.map((item, itemIndex) => <PlanItem key={item.id} item={item} itemIndex={itemIndex} siblingItems={activeItems} section={section} wsId={wsId} goals={goals} ownerOptions={ownerOptions} currentPeriodId={currentPeriodId} allowGoalConnections={allowGoalConnections} />)}
        </SortableContext>
        {missingParts.length > 0 && <div className="flex flex-wrap gap-1.5">{missingParts.map((part) => <button key={part} type="button" onClick={() => addItem(part, part)} className="flex min-h-8 items-center gap-1 rounded-full border border-dashed px-3 text-xs text-muted-foreground hover:border-foreground hover:text-foreground"><Plus aria-hidden className="size-3.5" />{part}</button>)}</div>}
        <input aria-label={`Add item to ${section.name}`} value={newItem} onChange={(event) => setNewItem(event.target.value)} onKeyDown={onNewItemKeyDown} placeholder={section.section_type === "process" ? "+ Add process and press Enter" : "+ Add item and press Enter"} className="min-h-11 rounded-md border border-dashed bg-transparent px-3 text-sm text-muted-foreground outline-none focus:border-ring focus:text-foreground" />
      </div>
    </section>
  );
}

const boardCollision: CollisionDetection = (args) => {
  const activeId = String(args.active.id);
  const isSection = activeId.startsWith(SECTION_PREFIX);
  const droppableContainers = args.droppableContainers.filter((container) => {
    const id = String(container.id);
    return isSection ? id.startsWith(SECTION_PREFIX) : id.startsWith(ITEM_PREFIX) || id.startsWith(COLUMN_PREFIX);
  });
  return closestCorners({ ...args, droppableContainers });
};

export function StrategyPage() {
  const enabled = useFeatureFlag("cerebro_operating_system");
  const wsId = useWorkspaceId();
  const settings = useQuery(settingsOptions(wsId));
  const plan = useQuery(visionPlanOptions(wsId));
  const rocks = useQuery(rocksOptions(wsId));
  const periods = useQuery(periodsOptions(wsId));
  const members = useQuery(memberListOptions(wsId));
  const agents = useQuery(agentListOptions(wsId));
  const createSection = useCreateVisionPlanSection(wsId);
  const updateSection = useUpdateVisionPlanSection(wsId);
  const updateItem = useUpdateVisionPlanItem(wsId);
  const [mode, setMode] = useState<"overview" | "edit">("overview");
  const [focusedSectionId, setFocusedSectionId] = useState<string>();
  const [addingSection, setAddingSection] = useState(false);
  const [sectionName, setSectionName] = useState("");
  const [dragLabel, setDragLabel] = useState<string>();
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 6 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );
  if (!enabled) return null;

  const terminology = settings.data?.terminology ?? DEFAULT_TERMINOLOGY;
  const sections = [...(plan.data?.sections ?? [])].sort((a, b) => a.position - b.position);
  const goals = rocks.data?.rocks ?? [];
  const allItems = sections.flatMap((section) => section.items);
  const ownerOptions: SearchSelectOption[] = [
    ...(members.data ?? []).map((member) => ({ value: `member:${member.id}`, label: member.name, group: "Members" })),
    ...(agents.data ?? []).map((agent) => ({ value: `agent:${agent.id}`, label: agent.name, group: "Agents" })),
  ];

  function addSection() {
    if (!sectionName.trim()) return;
    createSection.mutate({ name: sectionName.trim(), section_type: "list", position: sections.length });
    setSectionName(""); setAddingSection(false);
  }

  function editSection(sectionId: string) {
    setFocusedSectionId(sectionId);
    setMode("edit");
    window.requestAnimationFrame(() => document.getElementById(`vision-section-${sectionId}`)?.scrollIntoView({ block: "start", inline: "start" }));
  }

  function openRock(rockId: string) {
    const target = new URL(window.location.href);
    target.pathname = target.pathname.replace(/\/strategy\/?$/, "/rocks");
    target.searchParams.set("rock", rockId);
    window.location.assign(target);
  }

  function itemTarget(overId: string): { columnId: string; beforeItemId?: string } | null {
    if (overId.startsWith(COLUMN_PREFIX)) return { columnId: overId.slice(COLUMN_PREFIX.length) };
    if (overId.startsWith(SECTION_PREFIX)) return { columnId: overId.slice(SECTION_PREFIX.length) };
    if (overId.startsWith(ITEM_PREFIX)) {
      const itemId = overId.slice(ITEM_PREFIX.length);
      const column = sections.find((section) => section.items.some((item) => item.id === itemId));
      if (column) return { columnId: column.id, beforeItemId: itemId };
    }
    return null;
  }

  function onDragStart(event: DragStartEvent) {
    const activeId = String(event.active.id);
    if (activeId.startsWith(SECTION_PREFIX)) setDragLabel(sections.find((section) => `${SECTION_PREFIX}${section.id}` === activeId)?.name);
    else if (activeId.startsWith(ITEM_PREFIX)) setDragLabel(allItems.find((item) => `${ITEM_PREFIX}${item.id}` === activeId)?.title);
  }

  function onDragEnd(event: DragEndEvent) {
    setDragLabel(undefined);
    const { active, over } = event;
    if (!over) return;
    const activeId = String(active.id);
    const overId = String(over.id);
    if (activeId === overId) return;

    if (activeId.startsWith(SECTION_PREFIX)) {
      if (!overId.startsWith(SECTION_PREFIX)) return;
      for (const change of moveSection(sections, activeId.slice(SECTION_PREFIX.length), overId.slice(SECTION_PREFIX.length))) {
        const section = sections.find((candidate) => candidate.id === change.id);
        if (section) updateSection.mutate({ id: section.id, input: sectionInputFrom(section, change.position) });
      }
      return;
    }

    if (activeId.startsWith(ITEM_PREFIX)) {
      const target = itemTarget(overId);
      if (!target) return;
      for (const change of moveItem(sections, activeId.slice(ITEM_PREFIX.length), target.columnId, target.beforeItemId)) {
        const item = allItems.find((candidate) => candidate.id === change.id);
        if (item) updateItem.mutate({ id: item.id, input: itemInput(item, { section_id: change.section_id, position: change.position }) });
      }
    }
  }

  return (
    <main className="h-full min-w-0 overflow-y-auto bg-muted/20">
      <div className="mx-auto flex w-full max-w-6xl min-w-0 flex-col gap-5 p-4 sm:p-6">
        <header className="flex flex-wrap items-end justify-between gap-4 border-b pb-5">
          <div><p className="text-xs font-semibold uppercase tracking-[0.18em] text-muted-foreground">Company direction</p><h1 className="mt-1 text-3xl font-semibold tracking-tight">{terminology.strategy_map}</h1><p className="mt-1 text-sm text-muted-foreground">Connect long-term direction to current {terminology.rocks.toLowerCase()} in one scan.</p></div>
          <div className="flex gap-2"><button type="button" aria-pressed={mode === "overview"} onClick={() => setMode("overview")} className="h-11 rounded-md border bg-background px-4 text-sm font-medium hover:bg-muted">Overview</button><button type="button" aria-pressed={mode === "edit"} onClick={() => setMode("edit")} className="h-11 rounded-md border bg-background px-4 text-sm font-medium hover:bg-muted">Edit plan</button>{mode === "edit" && <button type="button" aria-label="Add section" onClick={() => setAddingSection(true)} className="h-11 rounded-md border bg-background px-4 text-sm font-medium hover:bg-muted">+ Add section</button>}</div>
        </header>
        {mode === "edit" && addingSection && <div className="flex gap-2 rounded-xl border border-dashed bg-card p-3"><input autoFocus aria-label="New section name" value={sectionName} onChange={(event) => setSectionName(event.target.value)} onKeyDown={(event) => { if (event.key === "Enter") addSection(); }} placeholder="Section name" className="h-10 min-w-0 flex-1 rounded-md border bg-background px-3 text-sm" /><button type="button" onClick={addSection} className="h-10 rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground">Add</button><button type="button" onClick={() => setAddingSection(false)} className="h-10 rounded-md border px-4 text-sm">Cancel</button></div>}
        {mode === "edit" && <p className="text-xs text-muted-foreground">Drag the grip to reorder columns, or drag a card between columns to reshape the plan. One-Year Plan items connect to {terminology.rocks} and their issues.</p>}
        {plan.isLoading ? <p>Loading {terminology.vision_plan}…</p> : plan.isError ? <p role="alert">{terminology.vision_plan} could not be loaded</p> : mode === "overview" ? <StrategyMap sections={sections} rocks={goals} terminology={terminology} onEditSection={editSection} onOpenRock={openRock} /> : (
          <DndContext sensors={sensors} collisionDetection={boardCollision} onDragStart={onDragStart} onDragEnd={onDragEnd} onDragCancel={() => setDragLabel(undefined)}>
            <SortableContext items={sections.map((section) => `${SECTION_PREFIX}${section.id}`)} strategy={horizontalListSortingStrategy}>
              <div className="flex flex-col gap-4 md:flex-row md:items-start md:overflow-x-auto md:pb-2">
                {sections.map((section, index) => <div id={`vision-section-${section.id}`} key={section.id} className={`flex ${focusedSectionId === section.id ? "scroll-mt-4 rounded-xl ring-2 ring-primary/30" : "scroll-mt-4"}`}><PlanSection section={section} index={index} sections={sections} wsId={wsId} goals={goals} ownerOptions={ownerOptions} currentPeriodId={periods.data?.periods[0]?.id} /></div>)}
              </div>
            </SortableContext>
            <DragOverlay>{dragLabel ? <div className="rounded-lg border bg-card px-3 py-2 text-sm font-medium shadow-lg">{dragLabel}</div> : null}</DragOverlay>
          </DndContext>
        )}
        <footer className="rounded-xl border border-dashed bg-card px-5 py-4 text-sm text-muted-foreground"><strong className="text-foreground">One plan, shared context.</strong> Items in One-Year Plan can connect to {terminology.rocks}; the remaining sections stay focused on the written plan.</footer>
      </div>
    </main>
  );
}
