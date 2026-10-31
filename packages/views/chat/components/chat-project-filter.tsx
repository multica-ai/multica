"use client";

import type { Project } from "@multica/core/types";
import { X } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@multica/ui/components/ui/select";
import { useT } from "../../i18n";

export function ChatProjectFilter({ projects, projectId, onChange }: {
  projects: Project[];
  projectId: string | null;
  onChange: (id: string | null) => void;
}) {
  const { t } = useT("chat");
  const items = [
    { value: "", label: t(($) => $.project_filter.all) },
    ...projects.map((project) => ({ value: project.id, label: project.title })),
  ];
  return (
    <div className="flex min-w-0 items-center gap-1 px-3 py-2">
      <Select items={items} value={projectId ?? ""} onValueChange={(value) => onChange(value || null)}>
        <SelectTrigger size="sm" aria-label={t(($) => $.project_filter.label)} className="min-w-0 flex-1">
          <SelectValue className="truncate" />
        </SelectTrigger>
        <SelectContent align="start">
          {items.map((item) => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}
        </SelectContent>
      </Select>
      {projectId && (
        <Button variant="ghost" size="icon-xs" aria-label={t(($) => $.project_filter.clear)} onClick={() => onChange(null)}><X /></Button>
      )}
    </div>
  );
}
