"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Activity, BookOpenText, FileCode, FileText, FolderPlus, Plus } from "@multica/ui/icons";
import { IconButton } from "@multica/ui/components/common/icon-button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { docTreeOptions } from "@multica/core/docs";
import { useCurrentWorkspace, useWorkspacePaths } from "@multica/core/paths";
import { skillListOptions } from "@multica/core/workspace/queries";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { DocTree, docRouteTarget } from "../../docs/components/doc-tree";
import { useDocCommands } from "../../docs/components/use-doc-commands";
import { CreateSkillDialog } from "../../skills/components/create-skill-dialog";
import { SkillPanelTree } from "../../skills/components/skill-panel-tree";
import { PanelBody, PanelCount, PanelHeader, PanelLink, PanelList, PanelSection } from "../panel-nav";

/** The knowledge base: docs in nested folders, and the workspace's skills. */
export function KbPanel() {
  const { t } = useT("docs");
  const { t: layoutT } = useT("layout");
  const p = useWorkspacePaths();
  const { pathname, push } = useNavigation();
  const wsId = useCurrentWorkspace()?.id ?? "";
  const { newDoc } = useDocCommands();
  const [naming, setNaming] = useState<{ parentId: string | null } | null>(null);
  const [creatingSkill, setCreatingSkill] = useState(false);

  const { data: tree } = useQuery({ ...docTreeOptions(wsId), enabled: !!wsId });
  const { data: skills = [] } = useQuery({ ...skillListOptions(wsId), enabled: !!wsId });
  const unread = tree?.docs.filter((d) => d.unread).length ?? 0;

  // New docs land beside what is open: inside the open folder, or next to the open doc.
  const { docId, folderId } = docRouteTarget(pathname, p.docs());
  const here = folderId ?? (docId ? (tree?.docs.find((d) => d.id === docId)?.folder_id ?? null) : null);

  return (
    <>
      <PanelHeader title={layoutT(($) => $.spaces.kb)}>
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <IconButton label={t(($) => $.actions.new)} tooltipSide="bottom" className="text-muted-foreground" />
            }
          >
            <Plus />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-auto min-w-48">
            <DropdownMenuItem onClick={() => newDoc("markdown", here)}>
              <FileText />
              {t(($) => $.actions.new_doc)}
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => newDoc("html", here)}>
              <FileCode />
              {t(($) => $.actions.new_html_doc)}
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => setNaming({ parentId: null })}>
              <FolderPlus />
              {t(($) => $.actions.new_folder)}
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem onClick={() => setCreatingSkill(true)}>
              <BookOpenText />
              {t(($) => $.actions.new_skill)}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </PanelHeader>
      <PanelBody>
        <PanelList className="mt-1">
          <PanelLink
            href={p.docs()}
            icon={<Activity />}
            label={t(($) => $.feed.title)}
            active={pathname === p.docs()}
            trailing={<PanelCount value={unread} className="text-brand" />}
          />
        </PanelList>

        <PanelSection
          label={t(($) => $.panel.docs)}
          action={
            <IconButton
              label={t(($) => $.actions.new_folder)}
              size="icon-xs"
              className="text-muted-foreground"
              onClick={() => setNaming({ parentId: null })}
            >
              <FolderPlus className="size-3.5" />
            </IconButton>
          }
        >
          {wsId && <DocTree wsId={wsId} naming={naming} onNamingChange={setNaming} />}
        </PanelSection>

        <PanelSection label={layoutT(($) => $.nav.skills)} action={<PanelCount value={skills.length} className="pr-2" />}>
          {wsId && <SkillPanelTree wsId={wsId} skills={skills} />}
          <PanelList className="mt-px">
            <PanelLink
              href={p.skills()}
              label={t(($) => $.panel.all_skills)}
              active={pathname === p.skills()}
              icon={<span className="size-4" aria-hidden />}
              className="text-caption"
            />
          </PanelList>
        </PanelSection>
      </PanelBody>
      {creatingSkill && (
        <CreateSkillDialog
          onClose={() => setCreatingSkill(false)}
          onCreated={(skill) => {
            setCreatingSkill(false);
            push(p.skillDetail(skill.id));
          }}
        />
      )}
    </>
  );
}
