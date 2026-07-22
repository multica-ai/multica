import type {
  LoopBlockType,
  LoopChainBlock,
  LoopChainPhase,
  LoopChainSpec,
} from "../core/types";

// Step-type presentation metadata shared by the editor and its tests.
// `kind` drives the machine/human colour dot and badge; there is no server
// field for it — it is derived purely from the block type.
export type LoopBlockKind = "machine" | "human";

export interface BlockTypeMeta {
  value: LoopBlockType;
  label: string;
  kind: LoopBlockKind;
  hint: string;
}

export const BLOCK_TYPES: ReadonlyArray<BlockTypeMeta> = [
  { value: "session", label: "Session", kind: "machine", hint: "An agent runs a skill on the issue." },
  { value: "command", label: "Command", kind: "machine", hint: "A machine check that must pass." },
  { value: "review", label: "Review", kind: "human", hint: "An agent reviews the work." },
  { value: "human", label: "Human", kind: "human", hint: "Waits for a person to approve." },
  { value: "eval", label: "Eval", kind: "machine", hint: "The server scores and decides." },
];

const SLASH_SKILL_RE = /\[\/((?:[^\]\\]|\\.)+)\]\(slash:\/\/skill\/([^)]+)\)/g;

// ContentEditor stores slash choices as stable ids while Chain v2 stores
// human-readable names. Resolve them at edit time and ignore stale ids.
export function skillNamesFromPrompt(
  markdown: string,
  skills: ReadonlyArray<{ id: string; name: string }>,
): string[] {
  const namesById = new Map(skills.map((skill) => [skill.id, skill.name]));
  const result: string[] = [];
  const seen = new Set<string>();
  for (const match of markdown.matchAll(SLASH_SKILL_RE)) {
    const name = namesById.get(match[2] ?? "");
    if (!name || seen.has(name)) continue;
    seen.add(name);
    result.push(name);
  }
  return result;
}

export function applyCommandSelection(
  block: LoopChainBlock,
  command: { id: string; argv: string[] },
): LoopChainBlock {
  return { ...block, command_id: command.id, check: [...command.argv], expect: "exit_zero" };
}

export function blockMeta(type: LoopBlockType): BlockTypeMeta {
  return BLOCK_TYPES.find((item) => item.value === type) ?? BLOCK_TYPES[0]!;
}

// The issue statuses a chain may move an issue through, in board order. This
// mirrors loops.IssueStatuses on the server, which rejects anything else at
// save time — a status can no longer be typed by hand and silently park an
// issue in a status the board does not have.
export const ISSUE_STATUS_OPTIONS: ReadonlyArray<{ value: string; label: string }> = [
  { value: "backlog", label: "Backlog" },
  { value: "todo", label: "Todo" },
  { value: "in_progress", label: "In progress" },
  { value: "in_review", label: "In review" },
  { value: "blocked", label: "Blocked" },
  { value: "done", label: "Done" },
  { value: "cancelled", label: "Cancelled" },
];

// A saved status the list does not know is shown as-is rather than dropped, so
// opening an older recipe never rewrites what it already stores.
export function issueStatusLabel(status: string): string {
  return ISSUE_STATUS_OPTIONS.find((option) => option.value === status)?.label ?? status;
}

// One-line summary shown under a collapsed step card, so the user reads the
// chain without opening each step. Kept in plain language, never raw keys.
export function blockSummary(block: LoopChainBlock): string {
  switch (block.type) {
    case "session": {
      const parts: string[] = [];
      const skills = block.skills?.length ? block.skills : block.skill ? [block.skill] : [];
      if (skills.length) parts.push(skills.length === 1 ? `Skill ${skills[0]}` : `${skills.length} skills`);
      const agentCount = block.agents?.filter((agent) => agent.agent_id).length ?? 0;
      if (agentCount > 0) parts.push(agentCount === 1 ? "1 agent" : `${agentCount} agents`);
      return parts.length ? parts.join(" · ") : "An agent runs a skill on the issue";
    }
    case "command": {
      const command = (block.check ?? []).join(" ").trim();
      return command ? `${command} · must pass` : "A machine check that must pass";
    }
    case "review":
      return block.skills?.length ? `Reviews with ${block.skills.length} skills` : block.skill ? `Reviews with ${block.skill}` : "An agent reviews the work";
    case "human":
      return "Waits for a person to approve";
    case "eval":
      return block.eval_key ? `Server scores ${block.eval_key}` : "The server scores and decides";
    default:
      return "";
  }
}

export function totalSteps(chain: LoopChainSpec): number {
  return chain.phases.reduce((sum, phase) => sum + phase.blocks.length, 0);
}

interface BlockLocation {
  phaseIndex: number;
  blockIndex: number;
}

function locateBlock(phases: LoopChainPhase[], blockId: string): BlockLocation | null {
  for (let phaseIndex = 0; phaseIndex < phases.length; phaseIndex += 1) {
    const blockIndex = phases[phaseIndex]!.blocks.findIndex((block) => block.id === blockId);
    if (blockIndex > -1) return { phaseIndex, blockIndex };
  }
  return null;
}

// Pure reorder used by the drag handle. `above` places the dragged block just
// before the target, otherwise just after it. Blocks may move across phases,
// but a phase is never left empty — moving the last block out of a phase is a
// no-op, mirroring the "Delete step" control being disabled at one block.
export function reorderBlocks(
  phases: LoopChainPhase[],
  dragId: string,
  targetId: string,
  above: boolean,
): LoopChainPhase[] {
  if (dragId === targetId) return phases;
  const from = locateBlock(phases, dragId);
  const to = locateBlock(phases, targetId);
  if (!from || !to) return phases;
  if (from.phaseIndex !== to.phaseIndex && phases[from.phaseIndex]!.blocks.length <= 1) {
    return phases;
  }

  const next = phases.map((phase) => ({ ...phase, blocks: [...phase.blocks] }));
  const [moved] = next[from.phaseIndex]!.blocks.splice(from.blockIndex, 1);
  if (!moved) return phases;

  let insertIndex = to.blockIndex + (above ? 0 : 1);
  if (from.phaseIndex === to.phaseIndex && from.blockIndex < to.blockIndex) insertIndex -= 1;
  next[to.phaseIndex]!.blocks.splice(insertIndex, 0, moved);
  return next;
}

// Pure reorder for whole phases, the phase-level twin of reorderBlocks.
// `above` places the dragged phase just before the target, otherwise just
// after it. Unknown ids and a self-drop are no-ops, so a stale drag hint can
// never rewrite the chain.
export function reorderPhases(
  phases: LoopChainPhase[],
  dragId: string,
  targetId: string,
  above: boolean,
): LoopChainPhase[] {
  if (dragId === targetId) return phases;
  const from = phases.findIndex((phase) => phase.id === dragId);
  const to = phases.findIndex((phase) => phase.id === targetId);
  if (from < 0 || to < 0) return phases;

  const next = [...phases];
  const [moved] = next.splice(from, 1);
  if (!moved) return phases;

  let insertIndex = to + (above ? 0 : 1);
  if (from < to) insertIndex -= 1;
  next.splice(insertIndex, 0, moved);
  return next;
}

// Move a phase one slot up (-1) or down (+1) — the keyboard path on the phase
// drag handle, mirroring nudgeBlock so reordering never needs a pointer.
export function nudgePhase(
  phases: LoopChainPhase[],
  phaseId: string,
  offset: -1 | 1,
): LoopChainPhase[] {
  const from = phases.findIndex((phase) => phase.id === phaseId);
  if (from < 0) return phases;
  const target = from + offset;
  if (target < 0 || target >= phases.length) return phases;
  return reorderPhases(phases, phaseId, phases[target]!.id, offset < 0);
}

// Move a block one slot up (-1) or down (+1) within its phase — the keyboard
// path on the drag handle, so reordering stays reachable without a pointer.
export function nudgeBlock(
  phases: LoopChainPhase[],
  blockId: string,
  offset: -1 | 1,
): LoopChainPhase[] {
  const from = locateBlock(phases, blockId);
  if (!from) return phases;
  const target = from.blockIndex + offset;
  const blocks = phases[from.phaseIndex]!.blocks;
  if (target < 0 || target >= blocks.length) return phases;
  return reorderBlocks(phases, blockId, blocks[target]!.id, offset < 0);
}
