import type { BaselineChapter, EngineeringConfig, EngineeringStage, IssueGraphNode } from "@multica/core/api";
import type { Agent, Member, Project, TimelineEntry } from "@multica/core/types";

export const ENGINEERING_CHAPTERS: readonly BaselineChapter[] = [
  "overview",
  "domain_rules",
  "architecture",
  "exclusions_and_risks",
];

export const STRIXNOVA_ENGINEERING_SKILLS = [
  "strixnova-discovery",
  "strixnova-architecture-and-slicing",
  "strixnova-tdd-and-debugging",
  "strixnova-deck",
] as const;

function decisionCard(content: string, language: "html" | "markdown"): boolean {
  return new RegExp(`^ {0,3}\`\`\`${language} title="决策卡[^"]*"`, "m").test(content);
}

export function selectEngineeringComment(
  entries: readonly TimelineEntry[],
  deciderIds: readonly string[] = [],
): TimelineEntry | null {
  const roots = entries
    .filter((entry) => entry.type === "comment" && !entry.parent_id && !entry.deleted_at && entry.content?.trim())
    .sort((a, b) => b.created_at.localeCompare(a.created_at) || b.id.localeCompare(a.id));
  const unresolved = roots.filter((entry) => !entry.resolved_at);
  const mentionsDecider = (entry: TimelineEntry) => deciderIds.some((id) => entry.content?.includes(`mention://member/${id}`));
  return unresolved.find((entry) => decisionCard(entry.content!, "html") || mentionsDecider(entry))
    ?? unresolved.find((entry) => decisionCard(entry.content!, "markdown"))
    ?? unresolved[0]
    ?? roots[0]
    ?? null;
}

export interface EngineeringPermissions {
  isAdmin: boolean;
  isDecider: boolean;
  deciderIds: string[];
}

export function engineeringPermissions(
  userId: string | undefined,
  members: readonly Member[],
  project: Project | undefined,
  config: EngineeringConfig,
): EngineeringPermissions {
  const adminIds = members.filter((member) => member.role === "owner" || member.role === "admin").map((member) => member.user_id);
  const deciderIds = new Set(adminIds);
  if (config.deciderMemberIds.length > 0) {
    for (const id of config.deciderMemberIds) deciderIds.add(id);
  } else if (project?.lead_type === "member" && project.lead_id) {
    deciderIds.add(project.lead_id);
  }
  return {
    isAdmin: !!userId && adminIds.includes(userId),
    isDecider: !!userId && deciderIds.has(userId),
    deciderIds: [...deciderIds].sort(),
  };
}

export type EngineeringStageStatus = "completed" | "review" | "current" | "locked";

export interface EngineeringStageSummary {
  stage: number;
  title: string;
  goal: string;
  status: EngineeringStageStatus;
  total: number;
  closed: number;
  blocked: number;
  inReview: number;
  inProgress: number;
  todo: number;
  tasks: IssueGraphNode[];
}

export interface EngineeringRoadmap {
  stages: EngineeringStageSummary[];
  outside: IssueGraphNode[];
}

export function isClosedEngineeringNode(node: IssueGraphNode): boolean {
  return node.statusCategory === "done" || node.statusCategory === "closed" || node.statusCategory === "cancelled";
}

export function buildEngineeringRoadmap(
  nodes: readonly IssueGraphNode[],
  configuredStages: readonly EngineeringStage[],
  currentStage: number,
): EngineeringRoadmap {
  const nodeById = new Map(nodes.map((node) => [node.id, node]));
  const tasksByStage = new Map<number, IssueGraphNode[]>();
  const outside: IssueGraphNode[] = [];
  for (const node of nodes) {
    if (node.stage !== null) {
      const parent = node.parentIssueId ? nodeById.get(node.parentIssueId) : undefined;
      if (parent && isClosedEngineeringNode(parent)) continue;
      const tasks = tasksByStage.get(node.stage) ?? [];
      tasks.push(node);
      tasksByStage.set(node.stage, tasks);
    } else if (!isClosedEngineeringNode(node)) {
      outside.push(node);
    }
  }

  const configuredByStage = new Map(configuredStages.map((stage) => [stage.stage, stage]));
  const stageNumbers = new Set<number>([...configuredByStage.keys(), ...tasksByStage.keys(), currentStage]);
  const stages = [...stageNumbers].sort((a, b) => a - b).map((stage) => {
    const configured = configuredByStage.get(stage);
    const tasks = tasksByStage.get(stage) ?? [];
    const closed = tasks.filter(isClosedEngineeringNode).length;
    const status: EngineeringStageStatus = stage < currentStage
      ? "completed"
      : stage > currentStage
        ? "locked"
        : tasks.length > 0 && closed === tasks.length ? "review" : "current";
    return {
      stage,
      title: configured?.title ?? "",
      goal: configured?.goal ?? "",
      status,
      total: tasks.length,
      closed,
      blocked: tasks.filter((node) => !isClosedEngineeringNode(node) && node.status === "blocked").length,
      inReview: tasks.filter((node) => !isClosedEngineeringNode(node) && node.status === "in_review").length,
      inProgress: tasks.filter((node) => !isClosedEngineeringNode(node) && node.status === "in_progress").length,
      todo: tasks.filter((node) => !isClosedEngineeringNode(node) && node.statusCategory === "unstarted").length,
      tasks,
    };
  });
  return { stages, outside };
}

export interface StrixnovaSkillBinding {
  agent: Agent;
  skills: string[];
}

export function strixnovaSkillBindings(agents: readonly Agent[]): StrixnovaSkillBinding[] {
  const coreSkills = new Set<string>(STRIXNOVA_ENGINEERING_SKILLS);
  return agents.flatMap((agent) => {
    if (agent.archived_at) return [];
    const skills = (agent.skills ?? [])
      .filter((skill) => skill.enabled !== false && coreSkills.has(skill.name))
      .map((skill) => skill.name)
      .sort((a, b) => STRIXNOVA_ENGINEERING_SKILLS.indexOf(a as typeof STRIXNOVA_ENGINEERING_SKILLS[number]) - STRIXNOVA_ENGINEERING_SKILLS.indexOf(b as typeof STRIXNOVA_ENGINEERING_SKILLS[number]));
    return skills.length ? [{ agent, skills }] : [];
  });
}
