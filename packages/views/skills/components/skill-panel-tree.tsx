"use client";

import { useId, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { BookOpenText, ChevronRight, FileText, Folder, FolderOpen } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { useWorkspacePaths } from "@multica/core/paths";
import { skillDetailOptions } from "@multica/core/workspace/queries";
import type { SkillSummary } from "@multica/core/types";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { PANEL_ROW_CLASS, panelRowStateClass } from "../../layout/panel-nav";
import { TreeRow } from "../../layout/panels/sidebar-tree";
import { buildSkillFileTree, SKILL_MAIN_FILE, type SkillFileNode } from "../lib/file-tree";

const NESTED_LIST_CLASS = "mt-px ml-5 space-y-px border-l border-surface-border pl-1.5";

interface OpenFile {
  skillId: string;
  path: string;
}

/**
 * The knowledge base panel's skills: each skill expands into its files, so
 * the skill page only has to show the one that is open (`?file=`).
 */
export function SkillPanelTree({ wsId, skills }: { wsId: string; skills: SkillSummary[] }) {
  const p = useWorkspacePaths();
  const { pathname, searchParams } = useNavigation();
  // Explicit toggles; anything untouched follows the open file.
  const [toggled, setToggled] = useState<Record<string, boolean>>({});

  const open = useMemo<OpenFile | null>(() => {
    const skill = skills.find((s) => pathname === p.skillDetail(s.id));
    if (!skill) return null;
    return { skillId: skill.id, path: searchParams.get("file") || SKILL_MAIN_FILE };
  }, [skills, pathname, searchParams, p]);

  const isExpanded = (key: string, containsOpen: boolean) => toggled[key] ?? containsOpen;
  const setExpanded = (key: string, value: boolean) => setToggled((prev) => ({ ...prev, [key]: value }));

  return (
    <ul className="space-y-px">
      {skills.map((skill) => (
        <SkillNode
          key={skill.id}
          wsId={wsId}
          skill={skill}
          open={open?.skillId === skill.id ? open.path : null}
          isExpanded={isExpanded}
          setExpanded={setExpanded}
        />
      ))}
    </ul>
  );
}

interface NodeState {
  isExpanded: (key: string, containsOpen: boolean) => boolean;
  setExpanded: (key: string, value: boolean) => void;
}

function SkillNode({
  wsId,
  skill,
  open,
  isExpanded,
  setExpanded,
}: NodeState & { wsId: string; skill: SkillSummary; open: string | null }) {
  const { t } = useT("docs");
  const p = useWorkspacePaths();
  const listId = useId();
  const expanded = isExpanded(skill.id, open !== null);

  return (
    <li>
      <TreeRow
        href={p.skillDetail(skill.id)}
        label={skill.name}
        // Expanded, the file rows carry the selection instead.
        active={open !== null && !expanded}
        icon={<BookOpenText className="size-4 shrink-0" />}
        expanded={expanded}
        onExpandedChange={(value) => setExpanded(skill.id, value)}
        listId={listId}
        toggleLabel={t(($) => $.tree.toggle, { title: skill.name })}
      />
      {expanded && (
        <SkillFiles
          id={listId}
          wsId={wsId}
          skillId={skill.id}
          open={open}
          isExpanded={isExpanded}
          setExpanded={setExpanded}
        />
      )}
    </li>
  );
}

function SkillFiles({
  id,
  wsId,
  skillId,
  open,
  isExpanded,
  setExpanded,
}: NodeState & { id: string; wsId: string; skillId: string; open: string | null }) {
  const { data: skill, isPending } = useQuery(skillDetailOptions(wsId, skillId));
  const nodes = useMemo(
    () => buildSkillFileTree([SKILL_MAIN_FILE, ...(skill?.files ?? []).map((f) => f.path)]),
    [skill?.files],
  );

  if (isPending) {
    return (
      <ul id={id} className={NESTED_LIST_CLASS}>
        <li aria-hidden className="mx-3 my-2 h-3 w-24 animate-pulse rounded bg-muted" />
      </ul>
    );
  }
  return (
    <FileNodes
      id={id}
      skillId={skillId}
      nodes={nodes}
      open={open}
      isExpanded={isExpanded}
      setExpanded={setExpanded}
    />
  );
}

function FileNodes({
  id,
  skillId,
  nodes,
  open,
  isExpanded,
  setExpanded,
}: NodeState & { id?: string; skillId: string; nodes: SkillFileNode[]; open: string | null }) {
  const { t } = useT("docs");
  const p = useWorkspacePaths();

  return (
    <ul id={id} className={NESTED_LIST_CLASS}>
      {nodes.map((node) => {
        if (!node.isDirectory) {
          return (
            <li key={node.path}>
              <TreeRow
                href={node.path === SKILL_MAIN_FILE ? p.skillDetail(skillId) : p.skillFile(skillId, node.path)}
                label={node.name}
                active={open === node.path}
                icon={<FileText className="size-4 shrink-0" />}
              />
            </li>
          );
        }
        const key = `${skillId}:${node.path}`;
        const expanded = isExpanded(key, open?.startsWith(`${node.path}/`) ?? false);
        return (
          <li key={node.path}>
            <button
              type="button"
              aria-expanded={expanded}
              aria-label={t(($) => $.tree.toggle, { title: node.name })}
              onClick={() => setExpanded(key, !expanded)}
              className={cn(PANEL_ROW_CLASS, panelRowStateClass(false), "group/row")}
            >
              {/* Like TreeRow: the chevron takes the icon's place on hover. */}
              <span className="relative flex size-4 shrink-0 items-center justify-center">
                <span className="flex group-hover/row:opacity-0 group-focus-visible/row:opacity-0">
                  {expanded ? <FolderOpen className="size-4" /> : <Folder className="size-4" />}
                </span>
                <ChevronRight
                  aria-hidden
                  className={cn(
                    "absolute size-3.5 text-muted-foreground opacity-0 transition-transform duration-150 ease-out group-hover/row:opacity-100 group-focus-visible/row:opacity-100 motion-reduce:transition-none",
                    expanded && "rotate-90",
                  )}
                />
              </span>
              <span className="min-w-0 truncate">{node.name}</span>
            </button>
            {expanded && (
              <FileNodes
                skillId={skillId}
                nodes={node.children}
                open={open}
                isExpanded={isExpanded}
                setExpanded={setExpanded}
              />
            )}
          </li>
        );
      })}
    </ul>
  );
}
