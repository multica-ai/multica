import type { RuntimeDevice, RuntimeModel } from "../types";
import {
  AGENT_CONVERSATION_STARTER_LABEL_MAX_LENGTH,
  AGENT_CONVERSATION_STARTER_MAX_LENGTH,
  AGENT_CONVERSATION_STARTERS_MAX,
} from "./constants";
import type { AgentDraft } from "./draft";

/**
 * Wire format between the Agent Creation Studio and the hidden builder agent.
 *
 * Two asymmetric directions:
 *   - Outbound, the composer's plain text is wrapped in a JSON envelope that
 *     also carries the current draft and the catalogs the builder may pick IDs
 *     from, so every turn re-states the full decision context.
 *   - Inbound, the builder appends one `<agent_draft>` JSON block to its natural
 *     language reply; the block updates the form and is stripped before the
 *     message is rendered.
 *
 * Both directions are parsed defensively: a CLI-backed model can emit slightly
 * malformed JSON, and a draft that fails to parse must degrade to "no form
 * update" rather than breaking the conversation.
 */
const BUILDER_INPUT_PREFIX = "MULTICA_AGENT_BUILDER_INPUT\n";

export interface BuilderDraftPayload {
  name?: unknown;
  description?: unknown;
  instructions?: unknown;
  conversation_starters?: unknown;
  model?: unknown;
  skill_ids?: unknown;
  permission_scope?: unknown;
  member_ids?: unknown;
}

export function parseBuilderDraft(content: string): BuilderDraftPayload | null {
  const match = content.match(/<agent_draft>([\s\S]*?)<\/agent_draft>/);
  if (!match?.[1]) return null;
  try {
    const value = JSON.parse(match[1]);
    return value && typeof value === "object"
      ? (value as BuilderDraftPayload)
      : null;
  } catch {
    // Some CLI-backed models emit literal newlines in the Markdown
    // instructions string even when asked for compact JSON. Repair only JSON
    // control characters that occur inside strings; object structure and all
    // other syntax still have to pass JSON.parse.
    try {
      const value = JSON.parse(escapeJsonStringControlCharacters(match[1]));
      return value && typeof value === "object"
        ? (value as BuilderDraftPayload)
        : null;
    } catch {
      return null;
    }
  }
}

function escapeJsonStringControlCharacters(value: string): string {
  let result = "";
  let inString = false;
  let escaped = false;

  for (const character of value) {
    if (!inString) {
      result += character;
      if (character === '"') inString = true;
      continue;
    }
    if (escaped) {
      result += character;
      escaped = false;
      continue;
    }
    if (character === "\\") {
      result += character;
      escaped = true;
      continue;
    }
    if (character === '"') {
      result += character;
      inString = false;
      continue;
    }
    if (character === "\n") {
      result += "\\n";
    } else if (character === "\r") {
      result += "\\r";
    } else if (character === "\t") {
      result += "\\t";
    } else {
      result += character;
    }
  }

  return result;
}

/**
 * Removes the structured block from a reply before a human reads it. The block
 * is machinery — it drives the configuration form — and the form is where its
 * effect is already visible, so the transcript keeps only the prose.
 *
 * Two patterns, not one. The closed form is what history holds. The unclosed
 * one is what streaming produces: the reply arrives token by token, so for the
 * seconds it takes to emit the JSON there is an opening tag and no closing tag,
 * which the closed-form pattern cannot match — the raw payload used to scroll
 * past the reader on every single turn.
 *
 * The leading `\s*` takes the whitespace the model left between its prose and
 * the block, so removing it does not strand a blank line.
 */
export function stripBuilderDraft(content: string): string {
  return content
    .replace(/\s*<agent_draft>[\s\S]*?<\/agent_draft>/g, "")
    .replace(/\s*<agent_draft>[\s\S]*$/, "")
    .trim();
}

export function encodeBuilderInput(
  request: string,
  draft: AgentDraft,
  skills: Array<{ id: string; name: string; description: string }>,
  members: Array<{ user_id: string; name: string }>,
  runtime: Pick<RuntimeDevice, "id" | "name" | "provider"> | null,
  models: RuntimeModel[] | null,
): string {
  return (
    BUILDER_INPUT_PREFIX +
    JSON.stringify(
      {
        user_request: request,
        current_draft: {
          name: draft.name,
          description: draft.description,
          instructions: draft.instructions,
          conversation_starters: draft.conversationStarters,
          model: draft.model,
          skill_ids: [...draft.skillIds],
          permission_scope: draft.permissionScope,
          member_ids: [...draft.memberIds],
        },
        selected_runtime: runtime
          ? {
              id: runtime.id,
              name: runtime.name,
              provider: runtime.provider,
            }
          : null,
        available_runtime_models:
          models === null
            ? null
            : models.map((model) => ({
                id: model.id,
                label: model.label,
                provider: model.provider,
              })),
        available_workspace_skills: skills.map((skill) => ({
          id: skill.id,
          name: skill.name,
          description: skill.description,
        })),
        available_workspace_members: members.map((member) => ({
          id: member.user_id,
          name: member.name,
        })),
      },
      null,
      2,
    )
  );
}

export function decodeBuilderInput(content: string): string {
  if (!content.startsWith(BUILDER_INPUT_PREFIX)) return content;
  try {
    const parsed = JSON.parse(content.slice(BUILDER_INPUT_PREFIX.length)) as {
      user_request?: unknown;
    };
    return typeof parsed.user_request === "string"
      ? parsed.user_request
      : content;
  } catch {
    return content;
  }
}

export interface BuilderRestore {
  id: string;
  content: string;
}

/**
 * Chooses which cancelled prompt the builder composer should adopt (#5219).
 *
 * Two sources, never both: cancelling a task the daemon never started answers
 * synchronously (`cancelled_chat_message.restore_to_input`), while cancelling a
 * started-but-empty one defers the judgment and delivers the prompt later as a
 * durable chat_draft_restore row. The durable copy is the raw chat_message
 * content, i.e. still in the builder's encoded wire form, so it is decoded here
 * exactly as the synchronous path decodes its own.
 *
 * The session id is deliberately not carried over: the builder composer keys its
 * draft by `agent-builder:<id>`, so ChatInput's session guard would never match
 * a raw session id — and it does not need to, since this composer only ever
 * shows the builder session.
 */
export function pickBuilderRestore(
  synchronous: BuilderRestore | null,
  durable: { id: string; content: string } | null,
): BuilderRestore | null {
  if (synchronous) return synchronous;
  if (!durable) return null;
  return { id: durable.id, content: decodeBuilderInput(durable.content) };
}

export function mergeBuilderDraft(
  current: AgentDraft,
  payload: BuilderDraftPayload,
  validSkillIds: Set<string>,
  validMemberIds: Set<string>,
  validModelIds: ReadonlySet<string> | null,
): AgentDraft {
  const scope =
    payload.permission_scope === "workspace" ||
    payload.permission_scope === "members" ||
    payload.permission_scope === "private"
      ? payload.permission_scope
      : current.permissionScope;
  const skillIds = Array.isArray(payload.skill_ids)
    ? payload.skill_ids.filter(
        (id): id is string => typeof id === "string" && validSkillIds.has(id),
      )
    : [...current.skillIds];
  const memberIds = Array.isArray(payload.member_ids)
    ? payload.member_ids.filter(
        (id): id is string => typeof id === "string" && validMemberIds.has(id),
      )
    : [...current.memberIds];
  // The current value may be a deliberate custom entry from ModelDropdown,
  // so preserving it is always safe. Only catalog IDs may be introduced by
  // the builder; failed discovery therefore cannot turn into fail-open input.
  const model =
    typeof payload.model === "string" &&
    (payload.model === current.model ||
      (validModelIds !== null &&
        validModelIds.size > 0 &&
        (payload.model === "" || validModelIds.has(payload.model))))
      ? payload.model
      : current.model;

  const conversationStarters = Array.isArray(payload.conversation_starters)
    ? payload.conversation_starters
        .slice(0, AGENT_CONVERSATION_STARTERS_MAX)
        .filter(
          (item): item is { label: string; prompt: string } =>
            !!item &&
            typeof item === "object" &&
            typeof (item as { label?: unknown }).label === "string" &&
            typeof (item as { prompt?: unknown }).prompt === "string" &&
            (item as { label: string }).label.trim().length > 0 &&
            (item as { prompt: string }).prompt.trim().length > 0,
        )
        .map((item) => ({
          label: [...item.label.trim()]
            .slice(0, AGENT_CONVERSATION_STARTER_LABEL_MAX_LENGTH)
            .join(""),
          prompt: [...item.prompt.trim()]
            .slice(0, AGENT_CONVERSATION_STARTER_MAX_LENGTH)
            .join(""),
        }))
    : current.conversationStarters;

  return {
    ...current,
    name: typeof payload.name === "string" ? payload.name : current.name,
    description:
      typeof payload.description === "string"
        ? payload.description
        : current.description,
    instructions:
      typeof payload.instructions === "string"
        ? payload.instructions
        : current.instructions,
    conversationStarters,
    model,
    // The builder can move the model, which invalidates whatever thinking /
    // speed the user picked for the previous one. It never sets these two
    // itself, so an unchanged model simply preserves them.
    thinkingLevel: model === current.model ? current.thinkingLevel : "",
    serviceTier: model === current.model ? current.serviceTier : "",
    skillIds: new Set(skillIds),
    permissionScope: scope,
    memberIds: new Set(scope === "members" ? memberIds : []),
    teamIds: current.teamIds,
  };
}
