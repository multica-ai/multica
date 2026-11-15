"use client";

import { useState, type ReactNode } from "react";
import { ChevronDown } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { Tabs, TabsList, TabsTrigger } from "@multica/ui/components/ui/tabs";
import { TooltipHint } from "@multica/ui/components/common/tooltip-hint";
import { EmojiPicker } from "@multica/ui/components/common/emoji-picker";
import { Popover, PopoverContent, PopoverTrigger } from "@multica/ui/components/ui/popover";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import type { Project, UpdateProjectRequest } from "@multica/core/types";
import type { ProjectTab } from "@multica/core/projects/tab-store";
import { PageHeader } from "../../layout/page-header";
import { TitleEditor, ContentEditor } from "../../editor";
import { useT } from "../../i18n";
import { ProjectIcon } from "./project-icon";

const TAB_TRIGGER_CLASS = "h-full rounded-none px-2.5 group-data-horizontal/tabs:after:bottom-0";

/** Overview / Issues switch, sized to sit in a page header and underline at its bottom edge. */
export function ProjectTabs({
  tab,
  onTabChange,
  issueCount,
}: {
  tab: ProjectTab;
  onTabChange: (tab: ProjectTab) => void;
  issueCount: number;
}) {
  const { t } = useT("projects");
  return (
    <Tabs
      value={tab}
      onValueChange={(value) => onTabChange(value as ProjectTab)}
      className="shrink-0 self-stretch"
    >
      <TabsList variant="line" className="gap-0 p-0 group-data-horizontal/tabs:h-full">
        <TabsTrigger value="overview" className={TAB_TRIGGER_CLASS}>
          {t(($) => $.detail.tab_overview)}
        </TabsTrigger>
        <TabsTrigger value="issues" className={TAB_TRIGGER_CLASS}>
          {t(($) => $.detail.tab_issues)}
          <span className="tabular-nums text-muted-foreground">{issueCount}</span>
        </TabsTrigger>
      </TabsList>
    </Tabs>
  );
}

/** Header for the Overview tab: the same project menu as the Issues tab, without view controls. */
export function ProjectOverviewHeader({
  project,
  tabs,
  menu,
  trailing,
}: {
  project: Project;
  tabs: ReactNode;
  menu: ReactNode;
  trailing: ReactNode;
}) {
  return (
    <PageHeader>
      <div className="flex min-w-0 flex-1 items-center gap-1 self-stretch">
        <DropdownMenu>
          <h1 className="flex min-w-20">
            <DropdownMenuTrigger
              render={
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-7 min-w-0 max-w-64 shrink gap-2 px-2 text-body font-medium text-foreground"
                />
              }
            >
              <ProjectIcon project={project} size="sm" />
              <span className="min-w-0 truncate">{project.title}</span>
              <ChevronDown className="size-3 shrink-0 text-muted-foreground" />
            </DropdownMenuTrigger>
          </h1>
          <DropdownMenuContent align="start" className="w-auto min-w-52">
            {menu}
          </DropdownMenuContent>
        </DropdownMenu>
        {tabs}
      </div>
      <div className="flex shrink-0 items-center gap-1">{trailing}</div>
    </PageHeader>
  );
}

/**
 * The project as a document: icon, title and the full-width description that
 * agents receive as project context.
 */
export function ProjectOverviewBody({
  project,
  onUpdate,
}: {
  project: Project;
  onUpdate: (data: UpdateProjectRequest) => void;
}) {
  const { t } = useT("projects");
  const [iconPickerOpen, setIconPickerOpen] = useState(false);
  return (
    <div className="min-h-0 flex-1 overflow-y-auto">
      <div className="mx-auto w-full max-w-3xl px-8 pb-16 pt-10">
        <Popover open={iconPickerOpen} onOpenChange={setIconPickerOpen}>
          <TooltipHint label={t(($) => $.detail.icon_tooltip)}>
            <PopoverTrigger
              render={
                <button
                  type="button"
                  className="text-display-sm -ml-1 cursor-pointer rounded-lg p-1 transition-colors hover:bg-accent/60"
                  aria-label={t(($) => $.detail.icon_tooltip)}
                >
                  {project.icon || "📁"}
                </button>
              }
            />
          </TooltipHint>
          <PopoverContent align="start" className="w-auto p-0">
            <EmojiPicker
              onSelect={(emoji) => {
                onUpdate({ icon: emoji });
                setIconPickerOpen(false);
              }}
            />
          </PopoverContent>
        </Popover>
        <TitleEditor
          key={`title-${project.id}`}
          defaultValue={project.title}
          placeholder={t(($) => $.detail.title_placeholder)}
          className="mt-2 w-full text-title font-semibold leading-snug tracking-tight"
          onBlur={(value) => {
            const trimmed = value.trim();
            if (trimmed && trimmed !== project.title) onUpdate({ title: trimmed });
          }}
        />

        <section className="mt-8">
          <h2 className="mb-2 text-label font-medium">{t(($) => $.detail.section_description)}</h2>
          <ContentEditor
            key={project.id}
            value={project.description || ""}
            placeholder={t(($) => $.detail.description_placeholder)}
            onUpdate={(md) => onUpdate({ description: md || null })}
            debounceMs={1500}
          />
          <p className="mt-2 text-caption text-muted-foreground">
            {t(($) => $.detail.description_hint)}
          </p>
        </section>
      </div>
    </div>
  );
}
