"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Building2, ChevronDown } from "lucide-react";
import { projectListOptions } from "@multica/core/projects/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { ProjectIcon } from "../../projects/components/project-icon";
import { PropertyPicker, PickerItem, PickerEmpty } from "./pickers/property-picker";
import { matchesPinyin } from "../../editor/extensions/pinyin-match";
import { useT } from "../../i18n";

export function IssueProjectScopePicker({ projectId, onChange }: {
  projectId: string | null;
  onChange: (projectId: string | null) => void;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { data: projects = [], isError, refetch } = useQuery(projectListOptions(wsId));
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const current = projects.find((project) => project.id === projectId);
  const workspaceLabel = t(($) => $.project_scope.workspace);
  const query = search.trim().toLowerCase();
  const filtered = projects.filter((project) => project.title.toLowerCase().includes(query) || matchesPinyin(project.title, query));
  const select = (id: string | null) => { onChange(id); setOpen(false); };
  return <PropertyPicker
    open={open}
    onOpenChange={setOpen}
    searchable
    searchPlaceholder={t(($) => $.project_scope.search)}
    onSearchChange={setSearch}
    width="w-64"
    triggerRender={<button type="button" aria-label={t(($) => $.project_scope.label)}
      className="inline-flex min-w-0 max-w-64 items-center gap-2 rounded-md px-2 py-1 text-body font-medium hover:bg-accent" />}
    trigger={<>
      {current ? <ProjectIcon project={current} size="sm" /> : <Building2 className="size-3.5 shrink-0" />}
      <span className="truncate">{current?.title ?? workspaceLabel}</span>
      <ChevronDown className="size-3 shrink-0 text-muted-foreground" />
    </>}
  >
    <PickerItem emptyValue selected={projectId === null} onClick={() => select(null)}>
      <Building2 className="size-3.5 shrink-0" />{workspaceLabel}
    </PickerItem>
    {filtered.map((project) => <PickerItem key={project.id} selected={project.id === projectId} onClick={() => select(project.id)}>
      <ProjectIcon project={project} size="sm" /><span className="truncate">{project.title}</span>
    </PickerItem>)}
    {isError ? <PickerItem selected={false} onClick={() => void refetch()}>{t(($) => $.project_scope.retry)}</PickerItem> : filtered.length === 0 && query ? <PickerEmpty /> : null}
  </PropertyPicker>;
}
