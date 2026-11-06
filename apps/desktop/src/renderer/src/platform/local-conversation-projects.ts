import type { LocalFolderProject, LocalProviderId } from "@multica/core/local-conversation/protocol";
import type { LocalV2Summary } from "@multica/core/local-conversation/v2";

/** A local conversation as lists show it, from its v2 summary. */
export interface LocalSessionEntry {
  id: string;
  title: string;
  provider: LocalProviderId;
  cwd: string;
  pinned: boolean;
  archived: boolean;
  projectId: string | null;
  running: boolean;
  waitingForAnswer: boolean;
  backgroundTasks: number;
  preview: string;
  turnCount: number;
  updatedAt: string;
  side?: { sourceId: string; turnId?: string; quote?: string };
}

export function sessionEntry(summary: LocalV2Summary): LocalSessionEntry {
  return {
    id: summary.conversation_id, title: summary.title, provider: summary.provider === "codex" ? "codex" : "claude", cwd: summary.cwd,
    pinned: summary.pinned, archived: summary.archived, projectId: summary.project_id, running: summary.status === "running",
    waitingForAnswer: summary.status === "running" && summary.pending_interactions > 0, backgroundTasks: summary.background_tasks, preview: summary.last_message_preview,
    turnCount: summary.turn_count, updatedAt: summary.updated_at,
    ...(summary.side_source_id ? { side: { sourceId: summary.side_source_id, ...(summary.side_source_turn_id ? { turnId: summary.side_source_turn_id } : {}), ...(summary.side_quote ? { quote: summary.side_quote } : {}) } } : {}),
  };
}

export interface LocalConversationProjectGroup {
  id: string | null;
  project?: LocalFolderProject;
  sessions: LocalSessionEntry[];
}

function directoryKey(path: string): string { return path.replace(/[/\\]+$/, "") || path; }

/** The daemon reserves this identity for the scope's shared default directory. */
export function isDefaultFolderProject(project: LocalFolderProject): boolean {
  return /^unassigned:[0-9]+$/.test(project.id);
}

export function projectForDirectory(projects: LocalFolderProject[], cwd: string): LocalFolderProject | undefined {
  return projects.find((project) => directoryKey(project.path) === directoryKey(cwd));
}

/** Local folder projects follow execution directories; cloud task associations stay independent. */
export function groupLocalConversations(sessions: LocalSessionEntry[], projects: LocalFolderProject[], archived: boolean, search: string): LocalConversationProjectGroup[] {
  const defaultProject = projects.find(isDefaultFolderProject);
  const catalog = new Map(projects.map((project) => [directoryKey(project.path), project]));
  const groups = new Map<string | null, LocalConversationProjectGroup>();
  const query = search.toLocaleLowerCase().trim();
  for (const session of sessions) {
    if (session.archived !== archived) continue;
    const project = catalog.get(directoryKey(session.cwd)) ?? defaultProject;
    const id = project && !isDefaultFolderProject(project) ? project.id : null;
    if (query && ![session.title, session.preview, project?.title, project?.path].some((value) => value?.toLocaleLowerCase().includes(query))) continue;
    let group = groups.get(id);
    if (!group) { group = { id, project, sessions: [] }; groups.set(id, group); }
    group.sessions.push(session);
  }
  const result: LocalConversationProjectGroup[] = [];
  for (const project of projects) {
    if (isDefaultFolderProject(project)) continue;
    const group = groups.get(project.id);
    if (group) { result.push(group); groups.delete(project.id); }
    else if (!archived && !query) result.push({ id: project.id, project, sessions: [] });
  }
  const unassigned = groups.get(null);
  if (unassigned) result.push(unassigned);
  else if (!archived && !query && (result.length || defaultProject)) result.push({ id: null, ...(defaultProject ? { project: defaultProject } : {}), sessions: [] });
  return result;
}
