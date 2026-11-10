import { api } from "@multica/core/api";
import { buildCreateAgentRequest, EMPTY_AGENT_DRAFT } from "@multica/core/agents";
import { isReservedSlug } from "@multica/core/paths";
import { providerDisplayName } from "@multica/core/runtimes";
import type { Agent, AgentRuntime, Workspace } from "@multica/core/types";
import type { SupportedLocale } from "@multica/core/i18n";
import {
  WORKSPACE_SLUG_REGEX,
  isWorkspaceSlugConflict,
  nameToWorkspaceSlug,
} from "../workspace/slug";
import { buildLocalDirectoryResourceRef } from "../modals/create-project";

/** What the tool is called where people meet it, which is also the default
 *  name of the agent it becomes. The daemon's own display names are runtime
 *  labels ("Claude"), not product names. */
const TOOL_NAMES: Record<string, string> = {
  claude: "Claude Code",
  codex: "Codex",
  cursor: "Cursor",
  copilot: "GitHub Copilot",
  opencode: "OpenCode",
  gemini: "Gemini CLI",
};

export function toolDisplayName(provider: string): string {
  return TOOL_NAMES[provider] ?? providerDisplayName(provider);
}

/** The tools most people arrive with lead; the rest follow by name. The
 *  daemon reports its tools in no stable order. */
const TOOL_RANK = ["claude", "codex", "cursor", "copilot"];

export function compareTools(a: string, b: string): number {
  const rank = (p: string) => {
    const i = TOOL_RANK.indexOf(p);
    return i === -1 ? TOOL_RANK.length : i;
  };
  return rank(a) - rank(b) || toolDisplayName(a).localeCompare(toolDisplayName(b));
}

function firstName(name: string): string {
  return name.trim().split(/\s+/)[0] ?? "";
}

/**
 * Agent names are unique per workspace. A taken tool name gets the owner's
 * first name ("Claude Code · Jiayuan") — the teammate who took it first keeps
 * the plain name — and a counter only when that is taken too.
 */
export function uniqueAgentName(
  base: string,
  takenNames: Iterable<string>,
  ownerName: string,
): string {
  const taken = new Set(takenNames);
  if (!taken.has(base)) return base;
  const owner = firstName(ownerName);
  const personal = owner ? `${base} · ${owner}` : base;
  if (!taken.has(personal)) return personal;
  for (let n = 2; ; n += 1) {
    const candidate = `${personal} ${n}`;
    if (!taken.has(candidate)) return candidate;
  }
}

/** An agent this member already made from the same runtime — an earlier,
 *  interrupted run of the same flow. Reusing it keeps a retry from creating
 *  "Claude Code · Jiayuan" next to the "Claude Code" it already made. */
export function findReusableAgent(
  agents: Agent[],
  runtimeId: string,
  userId: string,
): Agent | undefined {
  return agents.find(
    (a) =>
      !a.archived_at &&
      !a.system_key &&
      a.runtime_id === runtimeId &&
      a.owner_id === userId,
  );
}

export async function ensureToolAgent({
  workspace,
  runtime,
  model,
  thinkingLevel,
  user,
  template,
}: {
  workspace: Pick<Workspace, "id" | "slug">;
  runtime: AgentRuntime;
  model: string;
  thinkingLevel: string;
  user: { id: string; name: string };
  template: "onboarding" | "invite_connect";
}): Promise<Agent> {
  const agents = await api.listAgents(
    { workspace_id: workspace.id, include_archived: true },
    workspace.slug,
  );
  const existing = findReusableAgent(agents, runtime.id, user.id);
  if (existing) return existing;
  const name = uniqueAgentName(
    toolDisplayName(runtime.provider),
    agents.map((a) => a.name),
    user.name,
  );
  return api.createAgent(
    buildCreateAgentRequest({
      draft: {
        ...EMPTY_AGENT_DRAFT,
        name,
        runtimeId: runtime.id,
        model,
        thinkingLevel,
      },
      runtimeId: runtime.id,
      runtimeProvider: runtime.provider,
      template,
    }),
    workspace.slug,
  );
}

/** Issue titles are one line; a longer ask keeps its full text as the body. */
export function splitTaskText(text: string): { title: string; description?: string } {
  const trimmed = text.trim();
  const firstLine = trimmed.split("\n")[0]!.trim();
  const title = firstLine.length > 120 ? `${firstLine.slice(0, 119).trimEnd()}…` : firstLine;
  return title === trimmed ? { title } : { title, description: trimmed };
}

export interface FirstTaskFolder {
  path: string;
  label: string | null;
}

/**
 * The first task: an issue assigned to the new agent, inside a project that
 * points at the member's folder when they picked one. The folder runs in
 * place — worktree mode needs a git repo and a daemon that advertises it,
 * which is a choice for the project settings, not for a first run.
 */
export async function createFirstTask({
  workspace,
  agent,
  text,
  folder,
  daemonId,
}: {
  workspace: Pick<Workspace, "slug">;
  agent: Pick<Agent, "id">;
  text: string;
  folder: FirstTaskFolder | null;
  daemonId: string | null;
}): Promise<{ issueId: string }> {
  let projectId: string | undefined;
  if (folder && daemonId) {
    const project = await api.createProject(
      {
        title: folder.label || folder.path,
        resources: [
          {
            resource_type: "local_directory",
            resource_ref: buildLocalDirectoryResourceRef({
              localPath: folder.path,
              daemonId,
              label: folder.label,
              mode: "in_place",
            }),
          },
        ],
      },
      workspace.slug,
    );
    projectId = project.id;
  }
  const issue = await api.createIssue(
    {
      ...splitTaskText(text),
      status: "todo",
      assignee_type: "agent",
      assignee_id: agent.id,
      ...(projectId ? { project_id: projectId } : {}),
    },
    workspace.slug,
  );
  return { issueId: issue.id };
}

// Kana and Han only: Korean separates words with spaces.
const CJK_GAP = /([\u3040-\u30ff\u3400-\u9fff]) (?=[\u3040-\u30ff\u3400-\u9fff])/g;

/** The CJK templates put a space after the name, which is right next to a
 *  Latin name ("Jiayuan 的工作区") and wrong between two CJK characters
 *  ("张三的工作区"). */
export function tightenCjkSpacing(text: string): string {
  return text.replace(CJK_GAP, "$1");
}

const SLUG_SUFFIX_ALPHABET = "abcdefghijklmnopqrstuvwxyz0123456789";

function randomSuffix(random: () => number): string {
  let out = "";
  for (let i = 0; i < 4; i += 1) {
    out += SLUG_SUFFIX_ALPHABET[Math.floor(random() * SLUG_SUFFIX_ALPHABET.length)];
  }
  return out;
}

/** The slug the default workspace tries first, from the member's name, then
 *  their email's local part. */
export function defaultWorkspaceSlugBase(
  user: { name: string; email: string },
  locale?: SupportedLocale,
): string {
  const fromName = nameToWorkspaceSlug(user.name, locale);
  if (fromName) return fromName;
  return nameToWorkspaceSlug(user.email.split("@")[0] ?? "") || "workspace";
}

const DEFAULT_WORKSPACE_ATTEMPTS = 4;

/**
 * Creates the member's first workspace without asking them to name it. The
 * plain slug is tried first; taken or reserved slugs get a random suffix.
 */
export async function createDefaultWorkspace({
  name,
  slugBase,
  create,
  random = Math.random,
}: {
  name: string;
  slugBase: string;
  create: (input: { name: string; slug: string }) => Promise<Workspace>;
  random?: () => number;
}): Promise<Workspace> {
  const usable = (slug: string) =>
    WORKSPACE_SLUG_REGEX.test(slug) && !isReservedSlug(slug);
  let lastError: unknown;
  for (let attempt = 0; attempt < DEFAULT_WORKSPACE_ATTEMPTS; attempt += 1) {
    const slug =
      attempt === 0 && usable(slugBase)
        ? slugBase
        : `${slugBase}-${randomSuffix(random)}`;
    try {
      return await create({ name, slug });
    } catch (err) {
      if (!isWorkspaceSlugConflict(err)) throw err;
      lastError = err;
    }
  }
  throw lastError;
}
