import type {
  BaselineChapter,
  BaselineChange,
  BaselineRevision,
  IssueGraphNode,
} from "@multica/core/api";
import type { EngineeringStageSummary } from "./engineering-utils";

export interface BaselineSection {
  key: string;
  chapter: BaselineChapter;
  anchor: string;
  title: string;
  level: number;
  body: string;
  firstParagraph: string;
  why: string;
  example: string;
}

export interface BaselineChapterDocument {
  chapter: BaselineChapter;
  sections: BaselineSection[];
}

export type BaselineSectionChange = "same" | "added" | "modified" | "removed";

export interface BaselineDisplaySection extends BaselineSection {
  change: BaselineSectionChange;
  beforeBody: string;
}

export interface BaselineDisplayChapter {
  chapter: BaselineChapter;
  sections: BaselineDisplaySection[];
}

export type BaselineReference =
  | { kind: "concept"; key: string; label: string; section: BaselineSection }
  | { kind: "issue"; key: string; label: string; node: IssueGraphNode }
  | { kind: "stage"; key: string; label: string; stage: EngineeringStageSummary };

export interface BaselineReferenceMatch {
  start: number;
  end: number;
  reference: BaselineReference;
}

const HEADING = /^(#{1,3})[ \t]+(.+?)[ \t]*#*[ \t]*$/;
const FENCE = /^ {0,3}(`{3,}|~{3,})/;

function cleanTitle(title: string): string {
  return title.replace(/^\[([^\]]+)]\s*/, "").trim();
}

function sectionAnchor(title: string): string {
  return title.match(/^\[([^\]]+)]\s*/)?.[1]?.trim() ?? cleanTitle(title);
}

export function plainBaselineText(markdown: string): string {
  return markdown
    .replace(/\[\[([^\]|]+)(?:\|([^\]]+))?]]/g, (_match, id: string, label: string | undefined) => label ?? (id.startsWith("#") ? id.slice(1) : id.startsWith("stage:") ? `Stage ${id.slice(6)}` : id))
    .replace(/!\[([^\]]*)]\([^)]*\)/g, "$1")
    .replace(/\[([^\]]+)]\([^)]*\)/g, "$1")
    .replace(/<[^>]+>/g, "")
    .replace(/[*_~`>#-]/g, "")
    .replace(/\s+/g, " ")
    .trim();
}

function labeledParagraphs(body: string): { body: string; why: string; example: string } {
  const kept: string[] = [];
  let why = "";
  let example = "";
  for (const paragraph of body.split(/\n\s*\n/)) {
    const match = paragraph.match(/^\s*(?:[-*]\s+)?(?:\*\*)?(为什么这样规定|为什么|Why|例|示例|Example)(?:\*\*)?\s*[:：]\s*([\s\S]*)$/i);
    if (match) {
      const value = match[2]?.trim() ?? "";
      if (/^(为什么这样规定|为什么|Why)$/i.test(match[1] ?? "")) why = value;
      else example = value;
    } else if (paragraph.trim()) {
      kept.push(paragraph);
    }
  }
  return { body: kept.join("\n\n"), why, example };
}

export function parseBaselineChapter(chapter: BaselineChapter, markdown: string): BaselineChapterDocument {
  const sections: BaselineSection[] = [];
  const lines = markdown.replace(/\r\n?/g, "\n").split("\n");
  let current: { title: string; level: number; lines: string[] } = { title: "", level: 0, lines: [] };
  let fence = "";
  const usedKeys = new Map<string, number>();
  const flush = () => {
    const rawBody = current.lines.join("\n").trim();
    if (!current.title && !rawBody) return;
    const labeled = labeledParagraphs(rawBody);
    const anchor = current.title ? sectionAnchor(current.title) : "intro";
    const baseKey = `${chapter}:${anchor}`;
    const count = (usedKeys.get(baseKey) ?? 0) + 1;
    usedKeys.set(baseKey, count);
    sections.push({
      key: count === 1 ? baseKey : `${baseKey} (${count})`,
      chapter,
      anchor,
      title: cleanTitle(current.title),
      level: current.level,
      body: labeled.body,
      firstParagraph: plainBaselineText(labeled.body.split(/\n\s*\n/)[0] ?? ""),
      why: labeled.why,
      example: labeled.example,
    });
  };
  for (const line of lines) {
    const fenceMatch = line.match(FENCE);
    if (fenceMatch) {
      const marker = fenceMatch[1] ?? "";
      if (!fence) fence = marker[0] ?? "";
      else if (marker[0] === fence) fence = "";
      current.lines.push(line);
      continue;
    }
    const heading = !fence ? line.match(HEADING) : null;
    if (heading) {
      flush();
      current = { title: heading[2]?.trim() ?? "", level: heading[1]?.length ?? 0, lines: [] };
    } else {
      current.lines.push(line);
    }
  }
  flush();
  return { chapter, sections };
}

function changeMatches(section: BaselineSection, change: BaselineChange): boolean {
  return section.anchor === change.anchor || section.title === cleanTitle(change.title);
}

function changeBody(chapter: BaselineChapter, change: BaselineChange, side: "before" | "after"): string {
  const value = change[side];
  const parsed = parseBaselineChapter(chapter, value);
  if (parsed.sections.length === 1 && parsed.sections[0]?.title) return parsed.sections[0].body;
  return value.trim();
}

export function mergeBaselineSections(
  chapter: BaselineChapter,
  adoptedMarkdown: string,
  revision: BaselineRevision | null,
  compare: boolean,
): BaselineDisplaySection[] {
  const adopted = parseBaselineChapter(chapter, adoptedMarkdown).sections;
  if (!compare || !revision || revision.chapter !== chapter) {
    return adopted.map((section) => ({ ...section, change: "same" as const, beforeBody: "" }));
  }
  const candidate = parseBaselineChapter(chapter, revision.contentMarkdown).sections;
  const changes = [
    ...revision.anchorsDiff.added.map((change) => ({ change, kind: "added" as const })),
    ...revision.anchorsDiff.modified.map((change) => ({ change, kind: "modified" as const })),
  ];
  const result: BaselineDisplaySection[] = candidate.map((section) => {
    const matched = changes.find(({ change }) => changeMatches(section, change));
    return {
      ...section,
      change: matched?.kind ?? "same",
      beforeBody: matched?.kind === "modified" ? changeBody(chapter, matched.change, "before") : "",
    };
  });
  for (const change of revision.anchorsDiff.removed) {
    if (result.some((section) => changeMatches(section, change))) continue;
    const original = adopted.find((section) => changeMatches(section, change));
    const fallback = original ?? parseBaselineChapter(chapter, `### ${change.title}\n\n${change.before}`).sections[0];
    if (!fallback) continue;
    const removed: BaselineDisplaySection = { ...fallback, change: "removed", beforeBody: "" };
    const originalIndex = original ? adopted.indexOf(original) : -1;
    const next = originalIndex >= 0
      ? adopted.slice(originalIndex + 1).find((section) => result.some((item) => item.key === section.key))
      : undefined;
    const previous = originalIndex >= 0
      ? [...adopted.slice(0, originalIndex)].reverse().find((section) => result.some((item) => item.key === section.key))
      : undefined;
    const insertAt = next
      ? result.findIndex((section) => section.key === next.key)
      : previous ? result.findIndex((section) => section.key === previous.key) + 1 : result.length;
    result.splice(insertAt < 0 ? result.length : insertAt, 0, removed);
  }
  return result;
}

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

export function matchBaselineReferences(
  text: string,
  options: {
    sections: readonly BaselineSection[];
    currentSectionKey?: string;
    nodes?: readonly IssueGraphNode[];
    stages?: readonly EngineeringStageSummary[];
    conceptsOnly?: boolean;
    seenConcepts?: Set<string>;
  },
): BaselineReferenceMatch[] {
  const concepts = new Map<string, BaselineSection>();
  for (const section of options.sections) {
    if (section.key === options.currentSectionKey || !section.title || concepts.has(section.title)) continue;
    concepts.set(section.title, section);
  }
  const nodes = new Map((options.nodes ?? []).map((node) => [node.identifier, node]));
  const stages = new Map((options.stages ?? []).map((stage) => [stage.stage, stage]));
  const alternatives = [
    ...[...concepts.keys()].map((title) => ({ value: title, kind: "concept" as const })),
    ...(options.conceptsOnly ? [] : [
      ...[...nodes.keys()].map((identifier) => ({ value: identifier, kind: "issue" as const })),
      ...[...stages.keys()].flatMap((stage) => [
        { value: `Stage ${stage}`, kind: "stage" as const },
        { value: `第 ${stage} 阶段`, kind: "stage" as const },
        { value: `第${stage}阶段`, kind: "stage" as const },
      ]),
    ]),
  ].sort((a, b) => b.value.length - a.value.length);
  if (!alternatives.length) return [];
  const pattern = new RegExp(alternatives.map((item) => escapeRegExp(item.value)).join("|"), "gi");
  const matches: BaselineReferenceMatch[] = [];
  for (const match of text.matchAll(pattern)) {
    const start = match.index;
    const raw = match[0];
    const before = text[start - 1];
    const after = text[start + raw.length];
    if (before && /[A-Za-z0-9_]/.test(before) && /^[A-Za-z0-9_]/.test(raw)) continue;
    if (after && /[A-Za-z0-9_]/.test(after) && /[A-Za-z0-9_]$/.test(raw)) continue;
    const concept = concepts.get(raw);
    let reference: BaselineReference | undefined;
    if (concept) {
      if (options.seenConcepts?.has(concept.key)) continue;
      reference = { kind: "concept", key: concept.key, label: raw, section: concept };
      options.seenConcepts?.add(concept.key);
    } else if (!options.conceptsOnly) {
      const node = nodes.get(raw.toUpperCase());
      if (node) reference = { kind: "issue", key: node.identifier, label: raw, node };
      const stageNumber = raw.match(/\d+/)?.[0];
      const stage = stageNumber ? stages.get(Number(stageNumber)) : undefined;
      if (!reference && stage) reference = { kind: "stage", key: String(stage.stage), label: raw, stage };
    }
    if (reference) matches.push({ start, end: start + raw.length, reference });
  }
  return matches;
}

export function scanBaselineReferences(
  section: BaselineSection,
  sections: readonly BaselineSection[],
  nodes: readonly IssueGraphNode[],
  stages: readonly EngineeringStageSummary[],
): BaselineReference[] {
  const seen = new Set<string>();
  return matchBaselineReferences(section.body, { sections, currentSectionKey: section.key, nodes, stages, seenConcepts: seen })
    .map((match) => match.reference);
}

export function latestBaselineSectionRevision(
  section: BaselineSection,
  revisions: readonly BaselineRevision[],
): BaselineRevision | null {
  return revisions.find((revision) => [
    ...revision.anchorsDiff.added,
    ...revision.anchorsDiff.modified,
    ...revision.anchorsDiff.removed,
  ].some((change) => changeMatches(section, change))) ?? null;
}
