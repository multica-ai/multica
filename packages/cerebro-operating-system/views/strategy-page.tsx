"use client";

import { useState, type KeyboardEvent } from "react";
import { useQuery } from "@tanstack/react-query";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import { DEFAULT_TERMINOLOGY } from "../core/api-schemas";
import {
  periodsOptions, rocksOptions, settingsOptions, useCreateConnection, useCreateVisionPlanItem,
  useCreateVisionPlanSection, useDeleteConnection, useDeleteVisionPlanItem, useDeleteVisionPlanSection,
  useSaveRock, useUpdateVisionPlanItem, useUpdateVisionPlanSection, visionPlanOptions,
} from "../core/queries";
import type { Rock, VisionPlanItem, VisionPlanItemInput, VisionPlanSection, VisionPlanSectionType } from "../core/types";
import { SearchSelect, type SearchSelectOption } from "./search-select";

const MARKETING_PARTS = ["Target market", "Differentiators", "Proven process", "Guarantee"];

const itemInput = (item: VisionPlanItem, overrides: Partial<VisionPlanItemInput> = {}): VisionPlanItemInput => ({
  section_id: item.section_id, title: item.title, description: item.description,
  part_label: item.part_label, owner_type: item.owner_type, owner_id: item.owner_id,
  position: item.position, state: item.state, ...overrides,
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
  return (
    <div className="group grid gap-2 rounded-lg border bg-background p-3 md:grid-cols-[minmax(0,1fr)_minmax(13rem,0.45fr)_auto]">
      <div className="min-w-0">
        {section.section_type === "structured" && (
          <input aria-label={`${item.title} part`} value={partLabel} onChange={(event) => setPartLabel(event.target.value)} onBlur={() => save()} placeholder="Part label" className="mb-1 w-full bg-transparent text-xs font-semibold uppercase tracking-wide text-muted-foreground outline-none" />
        )}
        <input aria-label={`${item.title} title`} value={title} onChange={(event) => setTitle(event.target.value)} onBlur={() => save()} className="w-full bg-transparent text-sm font-medium outline-none focus:ring-1 focus:ring-ring" />
        <textarea aria-label={`${item.title} description`} value={description} onChange={(event) => setDescription(event.target.value)} onBlur={() => save()} rows={description ? 2 : 1} placeholder="Add context…" className="mt-1 w-full resize-none bg-transparent text-xs leading-relaxed text-muted-foreground outline-none focus:ring-1 focus:ring-ring" />
      </div>
      <div className="grid content-start gap-2">
        {section.section_type === "process" && (
          <SearchSelect compact label={`${item.title} owner`} options={ownerOptions} value={item.owner_id ? `${item.owner_type}:${item.owner_id}` : ""} onChange={(value) => {
            const [ownerType, ownerId] = value.split(":");
            save({ owner_type: ownerId ? ownerType as "member" | "agent" : undefined, owner_id: ownerId || undefined });
          }} clearLabel="No owner" placeholder="Process owner" />
        )}
        {allowGoalConnections && <SearchSelect compact multiple label={`${item.title} Goals`} options={goals.map((goal) => ({ value: goal.id, label: goal.title }))} values={selectedGoals} onValuesChange={changeGoals} placeholder="Connect Goals" actionLabel={currentPeriodId ? "Create linked Goal" : undefined} onAction={currentPeriodId ? createLinkedGoal : undefined} />}
      </div>
      <div className="flex items-start gap-1 opacity-60 group-hover:opacity-100">
        <button type="button" aria-label={`Move ${item.title} up`} disabled={itemIndex === 0} onClick={() => move(-1)} className="h-8 rounded px-1.5 text-muted-foreground hover:bg-muted disabled:opacity-30">↑</button>
        <button type="button" aria-label={`Move ${item.title} down`} disabled={itemIndex === siblingItems.length - 1} onClick={() => move(1)} className="h-8 rounded px-1.5 text-muted-foreground hover:bg-muted disabled:opacity-30">↓</button>
        <button type="button" aria-label={`Delete ${item.title}`} onClick={() => remove.mutate(item.id)} className="h-8 rounded px-2 text-xs text-muted-foreground hover:bg-muted hover:text-destructive">Delete</button>
      </div>
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

  function sectionInput(overrides: Partial<{ name: string; section_type: VisionPlanSectionType; position: number }> = {}) {
    return { name: name.trim() || section.name, section_type: section.section_type, position: section.position, ...overrides };
  }

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
    updateSection.mutate({ id: section.id, input: sectionInput({ position: other.position }) });
    updateSection.mutate({ id: other.id, input: { name: other.name, section_type: other.section_type, position: section.position } });
  }

  const missingParts = section.section_type === "structured" ? MARKETING_PARTS.filter((part) => !section.items.some((item) => item.part_label === part)) : [];
  const activeItems = section.items.filter((item) => item.state === "active");
  const allowGoalConnections = section.key === "one-year-plan";
  return (
    <section className="rounded-xl border bg-card p-4 shadow-sm">
      <header className="flex items-center gap-2 border-b pb-3">
        <span aria-hidden className="text-xs tabular-nums text-muted-foreground">{String(index + 1).padStart(2, "0")}</span>
        <input aria-label={`${section.name} section name`} value={name} onChange={(event) => setName(event.target.value)} onBlur={() => updateSection.mutate({ id: section.id, input: sectionInput() })} className="min-w-0 flex-1 bg-transparent text-base font-semibold outline-none focus:ring-1 focus:ring-ring" />
        <button type="button" aria-label={`Move ${section.name} up`} disabled={index === 0} onClick={() => move(-1)} className="h-8 rounded px-2 text-muted-foreground hover:bg-muted disabled:opacity-30">↑</button>
        <button type="button" aria-label={`Move ${section.name} down`} disabled={index === sections.length - 1} onClick={() => move(1)} className="h-8 rounded px-2 text-muted-foreground hover:bg-muted disabled:opacity-30">↓</button>
        <button type="button" aria-label={`Delete ${section.name} section`} onClick={() => { if (window.confirm(`Delete ${section.name} and its items?`)) deleteSection.mutate(section.id); }} className="h-8 rounded px-2 text-xs text-muted-foreground hover:bg-muted hover:text-destructive">Delete</button>
      </header>
      <div className="mt-3 grid gap-2">
        {activeItems.map((item, itemIndex) => <PlanItem key={item.id} item={item} itemIndex={itemIndex} siblingItems={activeItems} section={section} wsId={wsId} goals={goals} ownerOptions={ownerOptions} currentPeriodId={currentPeriodId} allowGoalConnections={allowGoalConnections} />)}
        {missingParts.length > 0 && <div className="flex flex-wrap gap-1.5">{missingParts.map((part) => <button key={part} type="button" onClick={() => addItem(part, part)} className="rounded-full border border-dashed px-2.5 py-1 text-xs text-muted-foreground hover:border-foreground hover:text-foreground">+ {part}</button>)}</div>}
        <input aria-label={`Add item to ${section.name}`} value={newItem} onChange={(event) => setNewItem(event.target.value)} onKeyDown={onNewItemKeyDown} placeholder={section.section_type === "process" ? "+ Add process and press Enter" : "+ Add item and press Enter"} className="h-9 rounded-md border border-dashed bg-transparent px-3 text-sm text-muted-foreground outline-none focus:border-ring focus:text-foreground" />
      </div>
    </section>
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
  const createSection = useCreateVisionPlanSection(wsId);
  const [addingSection, setAddingSection] = useState(false);
  const [sectionName, setSectionName] = useState("");
  if (!enabled) return null;

  const terminology = settings.data?.terminology ?? DEFAULT_TERMINOLOGY;
  const sections = [...(plan.data?.sections ?? [])].sort((a, b) => a.position - b.position);
  const goals = rocks.data?.rocks ?? [];
  const ownerOptions: SearchSelectOption[] = [
    ...(members.data ?? []).map((member) => ({ value: `member:${member.id}`, label: member.name, group: "Members" })),
    ...(agents.data ?? []).map((agent) => ({ value: `agent:${agent.id}`, label: agent.name, group: "Agents" })),
  ];

  function addSection() {
    if (!sectionName.trim()) return;
    createSection.mutate({ name: sectionName.trim(), section_type: "list", position: sections.length });
    setSectionName(""); setAddingSection(false);
  }

  return (
    <main className="h-full min-w-0 overflow-y-auto bg-muted/20">
      <div className="mx-auto flex w-full max-w-5xl min-w-0 flex-col gap-5 p-4 sm:p-6">
        <header className="flex flex-wrap items-end justify-between gap-4 border-b pb-5">
          <div><p className="text-xs font-semibold uppercase tracking-[0.18em] text-muted-foreground">Company direction</p><h1 className="mt-1 text-3xl font-semibold tracking-tight">{terminology.vision_plan}</h1><p className="mt-1 text-sm text-muted-foreground">Edit the plan where it lives. Connect direction to {terminology.rocks} as it becomes actionable.</p></div>
          <button type="button" aria-label="Add section" onClick={() => setAddingSection(true)} className="h-10 rounded-md border bg-background px-4 text-sm font-medium hover:bg-muted">+ Add section</button>
        </header>
        {addingSection && <div className="flex gap-2 rounded-xl border border-dashed bg-card p-3"><input autoFocus aria-label="New section name" value={sectionName} onChange={(event) => setSectionName(event.target.value)} onKeyDown={(event) => { if (event.key === "Enter") addSection(); }} placeholder="Section name" className="h-10 min-w-0 flex-1 rounded-md border bg-background px-3 text-sm" /><button type="button" onClick={addSection} className="h-10 rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground">Add</button><button type="button" onClick={() => setAddingSection(false)} className="h-10 rounded-md border px-4 text-sm">Cancel</button></div>}
        {plan.isLoading ? <p>Loading {terminology.vision_plan}…</p> : plan.isError ? <p role="alert">{terminology.vision_plan} could not be loaded</p> : <div className="grid gap-3">{sections.map((section, index) => <PlanSection key={section.id} section={section} index={index} sections={sections} wsId={wsId} goals={goals} ownerOptions={ownerOptions} currentPeriodId={periods.data?.periods[0]?.id} />)}</div>}
        <footer className="rounded-xl border border-dashed bg-card px-5 py-4 text-sm text-muted-foreground"><strong className="text-foreground">One plan, shared context.</strong> Items in One-Year Plan can connect to {terminology.rocks}; the remaining sections stay focused on the written plan.</footer>
      </div>
    </main>
  );
}
