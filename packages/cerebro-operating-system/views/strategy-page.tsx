"use client";

import { useLayoutEffect, useRef, useState, type ComponentProps, type KeyboardEvent } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  DndContext, DragOverlay, KeyboardSensor, PointerSensor, closestCorners, useDroppable, useSensor, useSensors,
  type CollisionDetection, type DragEndEvent, type DragStartEvent,
} from "@dnd-kit/core";
import {
  SortableContext, sortableKeyboardCoordinates, useSortable, verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueListOptions } from "@multica/core/issues/queries";
import { projectListOptions } from "@multica/core/projects";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import { ChevronDown, ChevronUp, FolderKanban, GripVertical, ListTodo, Plus, Trash2 } from "lucide-react";
import { DEFAULT_TERMINOLOGY } from "../core/api-schemas";
import {
  periodsOptions, rocksOptions, settingsOptions, useCreateConnection, useCreateVisionPlanItem,
  useCreateVisionPlanPage, useCreateVisionPlanSection, useDeleteConnection, useDeleteVisionPlanItem,
  useDeleteVisionPlanPage, useDeleteVisionPlanSection, useSaveRock, useUpdateVisionPlanItem,
  useUpdateVisionPlanPage, useUpdateVisionPlanSection, visionPlanOptions,
} from "../core/queries";
import { columnSections, moveItem, moveSection } from "../core/strategy-board";
import type { Rock, VisionPlanItem, VisionPlanItemInput, VisionPlanObjectLink, VisionPlanPage, VisionPlanSection } from "../core/types";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@multica/ui/components/ui/tabs";
import { SearchSelect, type SearchSelectOption } from "./search-select";
import {
  COLUMN_PREFIX, GoalsBlockBody, ITEM_PREFIX, ITEM_ZONE_PREFIX, PageBoard, SECTION_PREFIX, parseColumnId,
} from "./plan-board";

// The named blanks each V/TO block asks for, offered as one-click chips so the
// page fills in like the paper organiser instead of an empty list.
const WIREFRAME_PARTS: Record<string, string[]> = {
  "core-focus": ["Purpose/Cause/Passion", "Our Niche"],
  "marketing-strategy": ["Target Market / The List", "Three Uniques", "Proven Process", "Guarantee"],
  "three-year-picture": ["Future Date", "Revenue", "Profit", "Measurables"],
  "one-year-plan": ["Future Date", "Revenue", "Profit"],
};

const usesPartLabels = (section: VisionPlanSection) => section.section_type === "structured" || Boolean(WIREFRAME_PARTS[section.key]);

// A textarea that always grows to fit its content, so context text is never
// clipped or scrolled out of view (FIR-3589). No fixed row count, no scrollbar.
function AutoTextarea({ value, ...props }: ComponentProps<"textarea">) {
  const ref = useRef<HTMLTextAreaElement>(null);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${el.scrollHeight}px`;
  }, [value]);
  return <textarea ref={ref} value={value} rows={1} {...props} />;
}

const itemInput = (item: VisionPlanItem, overrides: Partial<VisionPlanItemInput> = {}): VisionPlanItemInput => ({
  section_id: item.section_id, title: item.title, description: item.description,
  part_label: item.part_label, owner_type: item.owner_type, owner_id: item.owner_id,
  position: item.position, state: item.state, ...overrides,
});

const sectionInputFrom = (section: VisionPlanSection, overrides: Partial<VisionPlanSection> = {}) => ({
  name: overrides.name ?? section.name,
  section_type: overrides.section_type ?? section.section_type,
  position: overrides.position ?? section.position,
  page_id: overrides.page_id ?? section.page_id,
  column_index: overrides.column_index ?? section.column_index,
});

const pageInputFrom = (page: VisionPlanPage, overrides: Partial<VisionPlanPage> = {}) => ({
  name: overrides.name ?? page.name,
  column_count: overrides.column_count ?? page.column_count,
  position: overrides.position ?? page.position,
});

// A link is addressed by "<target_type>:<target_id>" so Projects and Issues can
// share one picker without colliding on id.
const linkValue = (link: VisionPlanObjectLink) => `${link.target_type}:${link.target_id}`;
const linkLabel = (link: VisionPlanObjectLink) => (link.identifier ? `${link.identifier} · ${link.title}` : link.title || link.target_id);

interface PlanItemProps {
  item: VisionPlanItem;
  itemIndex: number;
  siblingItems: VisionPlanItem[];
  section: VisionPlanSection;
  wsId: string;
  goals: Rock[];
  ownerOptions: SearchSelectOption[];
  linkOptions: SearchSelectOption[];
  currentPeriodId?: string;
  allowGoalConnections: boolean;
}

function PlanItem({ item, itemIndex, siblingItems, section, wsId, goals, ownerOptions, linkOptions, currentPeriodId, allowGoalConnections }: PlanItemProps) {
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

  function changeLinks(values: string[]) {
    const existing = new Map(item.links.map((link) => [linkValue(link), link.connection_id]));
    for (const value of values) {
      if (existing.has(value)) continue;
      const [targetType, targetId] = value.split(":");
      if (!targetType || !targetId) continue;
      connect.mutate({ source_type: "strategy_item", source_id: item.id, target_type: targetType, target_id: targetId, relationship_type: "relates_to", provenance: "manual" });
    }
    for (const [value, connectionId] of existing) {
      if (!values.includes(value)) disconnect.mutate(connectionId);
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

  const selectedLinks = item.links.map(linkValue);
  const selectedGoals = item.goal_connections.map((connection) => connection.goal_id);
  const connectedGoals = selectedGoals.map((goalId) => goals.find((goal) => goal.id === goalId)).filter((goal): goal is Rock => Boolean(goal));
  const style = { transform: CSS.Transform.toString(transform), transition };
  return (
    <div ref={setNodeRef} style={style} className={`group grid gap-2 rounded-lg border bg-background p-3 ${isDragging ? "opacity-50" : ""}`}>
      <div className="flex items-start gap-2">
        <button type="button" aria-label={`Reorder ${item.title}`} className="mt-0.5 grid size-8 shrink-0 cursor-grab touch-none place-items-center rounded text-muted-foreground opacity-40 hover:bg-muted group-hover:opacity-100" {...attributes} {...listeners}><GripVertical aria-hidden className="size-4" /></button>
        <div className="min-w-0 flex-1">
          {usesPartLabels(section) && (
            <input aria-label={`${item.title} part`} value={partLabel} onChange={(event) => setPartLabel(event.target.value)} onBlur={() => save()} placeholder="Part label" className="mb-1 w-full bg-transparent text-xs font-semibold uppercase tracking-wide text-muted-foreground outline-none" />
          )}
          <input aria-label={`${item.title} title`} value={title} onChange={(event) => setTitle(event.target.value)} onBlur={() => save()} className="w-full bg-transparent text-sm font-medium outline-none focus:ring-1 focus:ring-ring" />
          <AutoTextarea aria-label={`${item.title} description`} value={description} onChange={(event) => setDescription(event.target.value)} onBlur={() => save()} placeholder="Add context…" className="mt-1 w-full resize-none overflow-hidden bg-transparent text-xs leading-relaxed text-muted-foreground outline-none focus:ring-1 focus:ring-ring" />
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
      <div className="grid gap-1.5">
        <SearchSelect compact multiple label={`${item.title} Projects and Issues`} options={linkOptions} values={selectedLinks} onValuesChange={changeLinks} placeholder="Connect Project or Issue" />
        {item.links.length > 0 && (
          <ul aria-label={`${item.title} connected Projects and Issues`} className="flex flex-wrap gap-1.5">
            {item.links.map((link) => (
              <li key={link.connection_id}>
                <button type="button" aria-label={`Disconnect ${linkLabel(link)}`} onClick={() => changeLinks(selectedLinks.filter((value) => value !== linkValue(link)))} className="flex max-w-full items-center gap-1 rounded-full border bg-muted/40 px-2 py-0.5 text-xs">
                  {link.target_type === "project" ? <FolderKanban aria-hidden className="size-3 shrink-0 text-muted-foreground" /> : <ListTodo aria-hidden className="size-3 shrink-0 text-muted-foreground" />}
                  <span className="truncate">{linkLabel(link)}</span>
                  <span aria-hidden className="text-muted-foreground">×</span>
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
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

interface SectionBodyProps {
  section: VisionPlanSection;
  wsId: string;
  goals: Rock[];
  rocksLabel: string;
  ownerOptions: SearchSelectOption[];
  linkOptions: SearchSelectOption[];
  currentPeriodId?: string;
  onOpenRock: (rockId: string) => void;
}

// The contents of one block: its cards, the named blanks it still wants, and the
// add row. A Goals block shows the current period's goals instead of items.
function SectionBody({ section, wsId, goals, rocksLabel, ownerOptions, linkOptions, currentPeriodId, onOpenRock }: SectionBodyProps) {
  const createItem = useCreateVisionPlanItem(wsId);
  const [newItem, setNewItem] = useState("");
  const { setNodeRef: setDropRef } = useDroppable({ id: `${ITEM_ZONE_PREFIX}${section.id}` });

  function addItem(title: string, partLabel?: string) {
    if (!title.trim()) return;
    createItem.mutate({ section_id: section.id, title: title.trim(), part_label: partLabel, position: section.items.length, state: "active" });
    setNewItem("");
  }

  function onNewItemKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (event.key === "Enter") { event.preventDefault(); addItem(newItem); }
  }

  if (section.section_type === "goals") {
    return <GoalsBlockBody rocks={goals} rocksLabel={rocksLabel} onOpenRock={onOpenRock} />;
  }

  const parts = WIREFRAME_PARTS[section.key] ?? (section.section_type === "structured" ? WIREFRAME_PARTS["marketing-strategy"] ?? [] : []);
  const missingParts = parts.filter((part) => !section.items.some((item) => item.part_label === part));
  const activeItems = section.items.filter((item) => item.state === "active").sort((a, b) => a.position - b.position);
  const allowGoalConnections = section.key === "one-year-plan";

  return (
    <div ref={setDropRef} className="grid flex-1 content-start gap-2">
      <SortableContext items={activeItems.map((item) => `${ITEM_PREFIX}${item.id}`)} strategy={verticalListSortingStrategy}>
        {activeItems.map((item, itemIndex) => <PlanItem key={item.id} item={item} itemIndex={itemIndex} siblingItems={activeItems} section={section} wsId={wsId} goals={goals} ownerOptions={ownerOptions} linkOptions={linkOptions} currentPeriodId={currentPeriodId} allowGoalConnections={allowGoalConnections} />)}
      </SortableContext>
      {missingParts.length > 0 && <div className="flex flex-wrap gap-1.5">{missingParts.map((part) => <button key={part} type="button" onClick={() => addItem(part, part)} className="flex min-h-8 items-center gap-1 rounded-full border border-dashed px-3 text-xs text-muted-foreground hover:border-foreground hover:text-foreground"><Plus aria-hidden className="size-3.5" />{part}</button>)}</div>}
      <input aria-label={`Add item to ${section.name}`} value={newItem} onChange={(event) => setNewItem(event.target.value)} onKeyDown={onNewItemKeyDown} placeholder={section.section_type === "process" ? "+ Add process and press Enter" : "+ Add item and press Enter"} className="min-h-11 rounded-md border border-dashed bg-transparent px-3 text-sm text-muted-foreground outline-none focus:border-ring focus:text-foreground" />
    </div>
  );
}

interface PlanSectionProps extends SectionBodyProps {
  index: number;
  siblings: VisionPlanSection[];
}

// One building block on a page: a titled card the workspace can rename, reorder
// inside its column, drag to another column or page, and delete.
function PlanSection({ section, index, siblings, ...body }: PlanSectionProps) {
  const updateSection = useUpdateVisionPlanSection(body.wsId);
  const deleteSection = useDeleteVisionPlanSection(body.wsId);
  const [name, setName] = useState(section.name);
  const [confirmingDelete, setConfirmingDelete] = useState(false);
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: `${SECTION_PREFIX}${section.id}` });

  function move(direction: -1 | 1) {
    const other = siblings[index + direction];
    if (!other) return;
    updateSection.mutate({ id: section.id, input: sectionInputFrom(section, { position: other.position }) });
    updateSection.mutate({ id: other.id, input: sectionInputFrom(other, { position: section.position }) });
  }

  const style = { transform: CSS.Transform.toString(transform), transition };
  return (
    <section ref={setNodeRef} style={style} aria-label={section.name} className={`flex min-w-0 flex-col rounded-xl border bg-card shadow-sm ${isDragging ? "opacity-50" : ""}`}>
      <header className="flex items-center gap-1 border-b bg-muted/60 px-2 py-2">
        <button type="button" aria-label={`Reorder ${section.name}`} className="grid size-8 shrink-0 cursor-grab touch-none place-items-center rounded text-muted-foreground hover:bg-muted" {...attributes} {...listeners}><GripVertical aria-hidden className="size-4" /></button>
        <input aria-label={`${section.name} block name`} value={name} onChange={(event) => setName(event.target.value)} onBlur={() => updateSection.mutate({ id: section.id, input: sectionInputFrom(section, { name: name.trim() || section.name }) })} className="min-w-0 flex-1 bg-transparent text-sm font-semibold uppercase tracking-wide outline-none focus:ring-1 focus:ring-ring" />
        <button type="button" aria-label={`Move ${section.name} up`} disabled={index === 0} onClick={() => move(-1)} className="grid size-8 place-items-center rounded text-muted-foreground hover:bg-muted disabled:opacity-30"><ChevronUp aria-hidden className="size-4" /></button>
        <button type="button" aria-label={`Move ${section.name} down`} disabled={index === siblings.length - 1} onClick={() => move(1)} className="grid size-8 place-items-center rounded text-muted-foreground hover:bg-muted disabled:opacity-30"><ChevronDown aria-hidden className="size-4" /></button>
        {confirmingDelete ? (
          <span className="flex items-center gap-1">
            <button type="button" aria-label={`Confirm delete ${section.name} block`} onClick={() => deleteSection.mutate(section.id)} className="h-8 rounded bg-destructive px-2 text-xs font-medium text-destructive-foreground">Delete</button>
            <button type="button" aria-label={`Cancel delete ${section.name} block`} onClick={() => setConfirmingDelete(false)} className="h-8 rounded border px-2 text-xs">Cancel</button>
          </span>
        ) : (
          <button type="button" aria-label={`Delete ${section.name} block`} onClick={() => setConfirmingDelete(true)} className="grid size-8 place-items-center rounded text-muted-foreground hover:bg-muted hover:text-destructive"><Trash2 aria-hidden className="size-4" /></button>
        )}
      </header>
      <div className="flex flex-1 flex-col p-3"><SectionBody section={section} {...body} /></div>
    </section>
  );
}

const boardCollision: CollisionDetection = (args) => {
  const activeId = String(args.active.id);
  const isSection = activeId.startsWith(SECTION_PREFIX);
  const droppableContainers = args.droppableContainers.filter((container) => {
    const id = String(container.id);
    return isSection
      ? id.startsWith(SECTION_PREFIX) || id.startsWith(COLUMN_PREFIX)
      : id.startsWith(ITEM_PREFIX) || id.startsWith(ITEM_ZONE_PREFIX);
  });
  return closestCorners({ ...args, droppableContainers });
};

// Adds a block to one column of the open page.
function AddBlock({ pageId, columnIndex, blockCount, wsId }: { pageId: string; columnIndex: number; blockCount: number; wsId: string }) {
  const createSection = useCreateVisionPlanSection(wsId);
  const [name, setName] = useState("");

  function add() {
    if (!name.trim()) return;
    createSection.mutate({ name: name.trim(), section_type: "list", position: blockCount, page_id: pageId, column_index: columnIndex });
    setName("");
  }

  return (
    <input
      aria-label={`Add block to column ${columnIndex + 1}`}
      value={name}
      onChange={(event) => setName(event.target.value)}
      onKeyDown={(event) => { if (event.key === "Enter") { event.preventDefault(); add(); } }}
      onBlur={add}
      placeholder="+ Add block and press Enter"
      className="min-h-11 rounded-xl border border-dashed bg-transparent px-3 text-sm text-muted-foreground outline-none focus:border-ring focus:text-foreground"
    />
  );
}

// Rename the open page, change how many columns it has, or delete it. The last
// page is never deletable, so there is always somewhere to put a block.
function PageToolbar({ page, pageCount, wsId }: { page: VisionPlanPage; pageCount: number; wsId: string }) {
  const updatePage = useUpdateVisionPlanPage(wsId);
  const deletePage = useDeleteVisionPlanPage(wsId);
  const [name, setName] = useState(page.name);
  const [confirmingDelete, setConfirmingDelete] = useState(false);

  return (
    <div className="flex flex-wrap items-center gap-2 rounded-xl border bg-card p-2">
      <input aria-label={`${page.name} page name`} value={name} onChange={(event) => setName(event.target.value)} onBlur={() => updatePage.mutate({ id: page.id, input: pageInputFrom(page, { name: name.trim() || page.name }) })} className="h-9 min-w-40 flex-1 rounded-md border bg-background px-3 text-sm font-medium" />
      <div role="group" aria-label={`${page.name} columns`} className="flex items-center gap-1">
        <span className="px-1 text-xs text-muted-foreground">Columns</span>
        {[1, 2, 3].map((count) => (
          <button key={count} type="button" aria-label={`${count} column layout`} aria-pressed={page.column_count === count} onClick={() => updatePage.mutate({ id: page.id, input: pageInputFrom(page, { column_count: count }) })} className={`size-9 rounded-md border text-sm ${page.column_count === count ? "border-foreground font-semibold" : "text-muted-foreground"}`}>{count}</button>
        ))}
      </div>
      {pageCount > 1 && (confirmingDelete ? (
        <span className="flex items-center gap-1">
          <button type="button" aria-label={`Confirm delete ${page.name} page`} onClick={() => deletePage.mutate(page.id)} className="h-9 rounded-md bg-destructive px-3 text-xs font-medium text-destructive-foreground">Delete page</button>
          <button type="button" aria-label={`Cancel delete ${page.name} page`} onClick={() => setConfirmingDelete(false)} className="h-9 rounded-md border px-3 text-xs">Cancel</button>
        </span>
      ) : (
        <button type="button" aria-label={`Delete ${page.name} page`} onClick={() => setConfirmingDelete(true)} className="grid size-9 place-items-center rounded-md border text-muted-foreground hover:text-destructive"><Trash2 aria-hidden className="size-4" /></button>
      ))}
    </div>
  );
}

export function StrategyPage() {
  const enabled = useFeatureFlag("cerebro_operating_system");
  const wsId = useWorkspaceId();
  const settings = useQuery(settingsOptions(wsId));
  const plan = useQuery(visionPlanOptions(wsId));
  const rocks = useQuery(rocksOptions(wsId));
  const periods = useQuery(periodsOptions(wsId));
  const members = useQuery(memberListOptions(wsId));
  const agents = useQuery(agentListOptions(wsId));
  const projects = useQuery(projectListOptions(wsId));
  const issues = useQuery(issueListOptions(wsId));
  const createPage = useCreateVisionPlanPage(wsId);
  const updateSection = useUpdateVisionPlanSection(wsId);
  const updateItem = useUpdateVisionPlanItem(wsId);
  const [addingPage, setAddingPage] = useState(false);
  const [pageName, setPageName] = useState("");
  const [openPageId, setOpenPageId] = useState<string>();
  const [dragLabel, setDragLabel] = useState<string>();
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 6 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );
  if (!enabled) return null;

  const terminology = settings.data?.terminology ?? DEFAULT_TERMINOLOGY;
  const pages = [...(plan.data?.pages ?? [])].sort((a, b) => a.position - b.position);
  const sections = plan.data?.sections ?? [];
  const goals = rocks.data?.rocks ?? [];
  const allItems = sections.flatMap((section) => section.items);
  const activePageId = openPageId && pages.some((page) => page.id === openPageId) ? openPageId : pages[0]?.id;
  const ownerOptions: SearchSelectOption[] = [
    ...(members.data ?? []).map((member) => ({ value: `member:${member.id}`, label: member.name, group: "Members" })),
    ...(agents.data ?? []).map((agent) => ({ value: `agent:${agent.id}`, label: agent.name, group: "Agents" })),
  ];
  const linkOptions: SearchSelectOption[] = [
    ...(projects.data ?? []).map((project) => ({ value: `project:${project.id}`, label: project.title, group: "Projects" })),
    ...(issues.data ?? []).map((issue) => ({ value: `issue:${issue.id}`, label: `${issue.identifier} · ${issue.title}`, group: "Issues" })),
  ];

  function addPage() {
    if (!pageName.trim()) return;
    createPage.mutate({ name: pageName.trim(), column_count: 3, position: pages.length });
    setPageName(""); setAddingPage(false);
  }

  function openRock(rockId: string) {
    const target = new URL(window.location.href);
    target.pathname = target.pathname.replace(/\/strategy\/?$/, "/rocks");
    target.searchParams.set("rock", rockId);
    window.location.assign(target);
  }

  function itemTarget(overId: string): { sectionId: string; beforeItemId?: string } | null {
    if (overId.startsWith(ITEM_ZONE_PREFIX)) return { sectionId: overId.slice(ITEM_ZONE_PREFIX.length) };
    if (overId.startsWith(ITEM_PREFIX)) {
      const itemId = overId.slice(ITEM_PREFIX.length);
      const owner = sections.find((section) => section.items.some((item) => item.id === itemId));
      if (owner) return { sectionId: owner.id, beforeItemId: itemId };
    }
    return null;
  }

  function blockTarget(overId: string): { pageId: string; columnIndex: number; beforeSectionId?: string } | null {
    const column = parseColumnId(overId);
    if (column) return column;
    if (overId.startsWith(SECTION_PREFIX)) {
      const sectionId = overId.slice(SECTION_PREFIX.length);
      const target = sections.find((section) => section.id === sectionId);
      if (target) return { pageId: target.page_id, columnIndex: target.column_index, beforeSectionId: sectionId };
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
      const target = blockTarget(overId);
      if (!target) return;
      for (const change of moveSection(sections, activeId.slice(SECTION_PREFIX.length), target.pageId, target.columnIndex, target.beforeSectionId)) {
        const section = sections.find((candidate) => candidate.id === change.id);
        if (section) updateSection.mutate({ id: section.id, input: sectionInputFrom(section, { page_id: change.page_id, column_index: change.column_index, position: change.position }) });
      }
      return;
    }

    if (activeId.startsWith(ITEM_PREFIX)) {
      const target = itemTarget(overId);
      if (!target) return;
      for (const change of moveItem(sections, activeId.slice(ITEM_PREFIX.length), target.sectionId, target.beforeItemId)) {
        const item = allItems.find((candidate) => candidate.id === change.id);
        if (item) updateItem.mutate({ id: item.id, input: itemInput(item, { section_id: change.section_id, position: change.position }) });
      }
    }
  }

  const currentPeriodId = periods.data?.periods[0]?.id;
  const bodyProps = { wsId, goals, rocksLabel: terminology.rocks, ownerOptions, linkOptions, currentPeriodId, onOpenRock: openRock };

  return (
    <main className="h-full min-w-0 overflow-y-auto bg-muted/20">
      <div className="flex w-full min-w-0 flex-col gap-5 p-4 sm:p-6">
        <header className="flex flex-wrap items-end justify-between gap-4 border-b pb-5">
          <div><p className="text-xs font-semibold uppercase tracking-[0.18em] text-muted-foreground">Vision/Traction Organizer</p><h1 className="mt-1 text-3xl font-semibold tracking-tight">{terminology.strategy_map}</h1><p className="mt-1 text-sm text-muted-foreground">Build each page from blocks, and connect long-term direction to current {terminology.rocks.toLowerCase()}.</p></div>
          <button type="button" aria-label="Add page" onClick={() => setAddingPage(true)} className="h-11 rounded-md border bg-background px-4 text-sm font-medium hover:bg-muted">+ Add page</button>
        </header>

        {addingPage && <div className="flex gap-2 rounded-xl border border-dashed bg-card p-3"><input autoFocus aria-label="New page name" value={pageName} onChange={(event) => setPageName(event.target.value)} onKeyDown={(event) => { if (event.key === "Enter") addPage(); }} placeholder="Page name" className="h-10 min-w-0 flex-1 rounded-md border bg-background px-3 text-sm" /><button type="button" onClick={addPage} className="h-10 rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground">Add</button><button type="button" onClick={() => setAddingPage(false)} className="h-10 rounded-md border px-4 text-sm">Cancel</button></div>}

        {plan.isLoading ? <p>Loading {terminology.vision_plan}…</p> : plan.isError ? <p role="alert">{terminology.vision_plan} could not be loaded</p> : pages.length === 0 ? <p className="text-sm text-muted-foreground">No pages yet. Add one to start building.</p> : (
          <DndContext sensors={sensors} collisionDetection={boardCollision} onDragStart={onDragStart} onDragEnd={onDragEnd} onDragCancel={() => setDragLabel(undefined)}>
            <Tabs value={activePageId} onValueChange={setOpenPageId} className="gap-4">
              <TabsList>
                {pages.map((page) => <TabsTrigger key={page.id} value={page.id}>{page.name}</TabsTrigger>)}
              </TabsList>
              {pages.map((page) => (
                <TabsContent key={page.id} value={page.id} className="grid gap-4">
                  <PageToolbar page={page} pageCount={pages.length} wsId={wsId} />
                  <PageBoard
                    page={page}
                    sections={sections}
                    renderSection={(section) => {
                      const siblings = columnSections(sections, page.id, section.column_index);
                      return <PlanSection key={section.id} section={section} index={siblings.findIndex((candidate) => candidate.id === section.id)} siblings={siblings} {...bodyProps} />;
                    }}
                    renderColumnFooter={(columnIndex) => <AddBlock key={`add-${columnIndex}`} pageId={page.id} columnIndex={columnIndex} blockCount={columnSections(sections, page.id, columnIndex).length} wsId={wsId} />}
                  />
                </TabsContent>
              ))}
            </Tabs>

            <DragOverlay dropAnimation={null}>{dragLabel ? <div className="rounded-lg border bg-card px-3 py-2 text-sm font-medium shadow-lg">{dragLabel}</div> : null}</DragOverlay>
          </DndContext>
        )}
      </div>
    </main>
  );
}
