"use client";

import { createContext, useContext, useMemo, type ReactNode } from "react";
import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import type { Node, Parent } from "unist";
import type { Element, Root, Text } from "hast";
import { ArrowRight } from "@multica/ui/icons";
import { HoverCard, HoverCardContent, HoverCardTrigger } from "@multica/ui/components/ui/hover-card";
import { cn } from "@multica/ui/lib/utils";
import type { BaselineChapter, BaselineRevision, IssueGraphNode } from "@multica/core/api";
import { useT } from "../../i18n";
import { useStatusLabel } from "../../issues/utils/status-label";
import {
  matchBaselineReferences,
  type BaselineReference,
  type BaselineSection,
} from "./engineering-baseline-utils";
import type { EngineeringStageSummary } from "./engineering-utils";

export type BaselineLens = "understand" | "manage";

export interface BaselineSectionRelations {
  relatedConcepts: BaselineSection[];
  backlinks: BaselineSection[];
  issues: IssueGraphNode[];
  latestRevision: BaselineRevision | null;
}

interface BaselineWikiContextValue {
  lens: BaselineLens;
  workspaceId: string;
  sections: readonly BaselineSection[];
  nodes: readonly IssueGraphNode[];
  stages: readonly EngineeringStageSummary[];
  relations: ReadonlyMap<string, BaselineSectionRelations>;
  referenceCounts: ReadonlyMap<string, number>;
  onNavigate: (reference: BaselineReference) => void;
}

const BaselineWikiContext = createContext<BaselineWikiContextValue | null>(null);
export const BaselineWikiProvider = BaselineWikiContext.Provider;

function useBaselineWiki(): BaselineWikiContextValue {
  const value = useContext(BaselineWikiContext);
  if (!value) throw new Error("BaselineMarkdown must be rendered inside BaselineWikiProvider");
  return value;
}

function referenceId(reference: BaselineReference): string {
  return `${reference.kind}:${reference.key}`;
}

function createReferencePlugin(options: {
  sections: readonly BaselineSection[];
  currentSectionKey: string;
  nodes: readonly IssueGraphNode[];
  stages: readonly EngineeringStageSummary[];
  conceptsOnly: boolean;
}) {
  return () => (tree: Root) => {
    const seenConcepts = options.conceptsOnly ? new Set<string>() : undefined;
    const transform = (parent: Parent) => {
      for (let index = 0; index < parent.children.length; index += 1) {
        const node = parent.children[index]!;
        const tagName = (node as unknown as Element).tagName;
        if (tagName === "code" || tagName === "pre" || tagName === "a" || tagName === "baseline-reference") continue;
        if (node.type === "text") {
          const value = (node as unknown as Text).value;
          const matches = matchBaselineReferences(value, {
            sections: options.sections,
            currentSectionKey: options.currentSectionKey,
            nodes: options.nodes,
            stages: options.stages,
            conceptsOnly: options.conceptsOnly,
            seenConcepts,
          });
          if (!matches.length) continue;
          const replacements: Node[] = [];
          let cursor = 0;
          for (const match of matches) {
            if (match.start > cursor) replacements.push({ type: "text", value: value.slice(cursor, match.start) } as unknown as Node);
            replacements.push({
              type: "element",
              tagName: "baseline-reference",
              properties: { referenceId: referenceId(match.reference) },
              children: [{ type: "text", value: value.slice(match.start, match.end) }],
            } as unknown as Node);
            cursor = match.end;
          }
          if (cursor < value.length) replacements.push({ type: "text", value: value.slice(cursor) } as unknown as Node);
          parent.children.splice(index, 1, ...replacements);
          index += replacements.length - 1;
        } else if (Array.isArray((node as Parent).children)) {
          transform(node as Parent);
        }
      }
    };
    transform(tree as unknown as Parent);
  };
}

function RelatedConceptButton({ section }: { section: BaselineSection }) {
  const { onNavigate } = useBaselineWiki();
  return <button type="button" className="mr-2 inline text-left text-caption text-muted-foreground underline decoration-dotted underline-offset-2 hover:text-foreground" onClick={() => onNavigate({ kind: "concept", key: section.key, label: section.title, section })}>{section.title}</button>;
}

function ReferenceCard({ reference }: { reference: BaselineReference }) {
  const { t } = useT("engineering");
  const { lens, workspaceId, stages, relations, referenceCounts, onNavigate } = useBaselineWiki();
  const statusLabel = useStatusLabel(workspaceId);
  const open = <ButtonLink onClick={() => onNavigate(reference)}>{reference.kind === "concept" ? t(($) => $.wiki.goToSection) : t(($) => $.wiki.openInOverview)}</ButtonLink>;
  if (reference.kind === "concept") {
    const section = reference.section;
    const relation = relations.get(section.key);
    return (
      <div className="space-y-2" data-reference-card="concept">
        <div className="text-caption text-muted-foreground">{t(($) => $.chapters[section.chapter])}</div>
        <h3 className="font-medium">{section.title || t(($) => $.wiki.untitled)}</h3>
        <p className="line-clamp-4 text-caption text-muted-foreground">{section.firstParagraph}</p>
        {lens === "understand" ? (
          <>
            {section.why ? <div className="text-caption"><span className="mr-2 font-medium">{t(($) => $.wiki.whyShort)}</span><span className="text-muted-foreground">{section.why}</span></div> : null}
            {section.example ? <div className="text-caption"><span className="mr-2 font-medium">{t(($) => $.wiki.exampleShort)}</span><span className="text-muted-foreground">{section.example}</span></div> : null}
            {relation?.relatedConcepts.length ? <div><span className="mr-2 text-caption text-muted-foreground">{t(($) => $.wiki.relatedConcepts)}</span>{relation.relatedConcepts.slice(0, 4).map((item) => <RelatedConceptButton key={item.key} section={item} />)}</div> : null}
          </>
        ) : (
          <>
            {relation?.issues.length ? <ul className="space-y-1">{relation.issues.map((issue) => <li key={issue.id} className="flex justify-between gap-3 text-caption"><span className="font-mono">{issue.identifier}</span><span className="text-muted-foreground">{statusLabel(issue.status)}</span></li>)}</ul> : null}
            <div className="text-caption text-muted-foreground">{t(($) => $.wiki.referenceCount, { count: referenceCounts.get(referenceId(reference)) ?? 0 })}</div>
            {relation?.latestRevision ? <div className="text-caption text-muted-foreground">{t(($) => $.wiki.latestRevision, { number: relation.latestRevision.revisionNumber })}</div> : null}
          </>
        )}
        <div className="flex justify-end border-t pt-2">{open}</div>
      </div>
    );
  }
  if (reference.kind === "issue") {
    const issue = reference.node;
    const stage = stages.find((item) => item.stage === issue.stage);
    return (
      <div className="space-y-2" data-reference-card="issue">
        <div className="font-mono text-caption text-muted-foreground">{issue.identifier}</div>
        <h3 className="font-medium">{issue.title}</h3>
        <div className="text-caption">{statusLabel(issue.status)}</div>
        <div className="text-caption text-muted-foreground">{stage ? `${t(($) => $.stage.label, { number: stage.stage })} · ${stage.title || t(($) => $.stage.default, { number: stage.stage })}` : t(($) => $.wiki.noStage)}</div>
        <div className="text-caption text-muted-foreground">{t(($) => $.wiki.referenceCount, { count: referenceCounts.get(referenceId(reference)) ?? 0 })}</div>
        <div className="flex justify-end border-t pt-2">{open}</div>
      </div>
    );
  }
  const stage = reference.stage;
  return (
    <div className="space-y-2" data-reference-card="stage">
      <div className="text-caption text-muted-foreground">{t(($) => $.stage.label, { number: stage.stage })}</div>
      <h3 className="font-medium">{stage.title || t(($) => $.stage.default, { number: stage.stage })}</h3>
      {stage.goal ? <p className="line-clamp-3 text-caption text-muted-foreground">{stage.goal}</p> : null}
      <div className="h-1 overflow-hidden rounded-full bg-muted"><div className="h-full bg-foreground" style={{ width: `${stage.total ? Math.round(stage.closed / stage.total * 100) : 0}%` }} /></div>
      <div className="text-caption text-muted-foreground">{t(($) => $.stage.completedCount, { closed: stage.closed, total: stage.total })} · {t(($) => $.stage.status[stage.status])}</div>
      <div className="text-caption text-muted-foreground">{t(($) => $.wiki.referenceCount, { count: referenceCounts.get(referenceId(reference)) ?? 0 })}</div>
      <div className="flex justify-end border-t pt-2">{open}</div>
    </div>
  );
}

function ButtonLink({ children, onClick }: { children: ReactNode; onClick: () => void }) {
  return <button type="button" className="inline-flex items-center gap-1 text-caption font-medium hover:underline" onClick={onClick}>{children}<ArrowRight className="size-3" aria-hidden="true" /></button>;
}

function BaselineReferenceLink({ reference, children }: { reference: BaselineReference; children: ReactNode }) {
  const { lens, onNavigate } = useBaselineWiki();
  const trigger = (
    <button
      type="button"
      className={cn("inline rounded-[2px] p-0 text-left font-inherit underline decoration-dotted underline-offset-4 hover:text-foreground hover:decoration-solid", reference.kind !== "concept" && "font-mono text-[0.92em]", lens === "understand" && "text-inherit")}
      onClick={() => onNavigate(reference)}
    >
      {children}
    </button>
  );
  return (
    <HoverCard>
      <HoverCardTrigger render={trigger} />
      <HoverCardContent className="w-80 max-w-[calc(100vw-2rem)] p-3">
        <ReferenceCard reference={reference} />
      </HoverCardContent>
    </HoverCard>
  );
}

export function BaselineMarkdown({ content, currentSection, chapter }: {
  content: string;
  currentSection: BaselineSection;
  chapter?: BaselineChapter;
}) {
  const { lens, sections, nodes, stages } = useBaselineWiki();
  const references = useMemo(() => {
    const map = new Map<string, BaselineReference>();
    for (const section of sections) {
      const reference: BaselineReference = { kind: "concept", key: section.key, label: section.title, section };
      map.set(referenceId(reference), reference);
    }
    for (const node of nodes) {
      const reference: BaselineReference = { kind: "issue", key: node.identifier, label: node.identifier, node };
      map.set(referenceId(reference), reference);
    }
    for (const stage of stages) {
      const reference: BaselineReference = { kind: "stage", key: String(stage.stage), label: `Stage ${stage.stage}`, stage };
      map.set(referenceId(reference), reference);
    }
    return map;
  }, [sections, nodes, stages]);
  const plugin = useMemo(() => createReferencePlugin({
    sections,
    currentSectionKey: currentSection.key,
    nodes,
    stages,
    conceptsOnly: lens === "understand",
  }), [sections, currentSection.key, nodes, stages, lens]);
  const components = useMemo(() => ({
    "baseline-reference": ({ referenceId: id, children }: { referenceId?: string; children?: ReactNode }) => {
      const reference = id ? references.get(id) : undefined;
      return reference ? <BaselineReferenceLink reference={reference}>{children}</BaselineReferenceLink> : <>{children}</>;
    },
  }) as unknown as Components, [references]);
  if (!content.trim()) return null;
  return (
    <div className="rich-text-editor readonly text-body leading-7 [&_li]:my-1 [&_p]:my-2 [&_strong]:font-medium" data-baseline-markdown={chapter ?? currentSection.chapter}>
      <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={[plugin]} components={components}>{content}</ReactMarkdown>
    </div>
  );
}
