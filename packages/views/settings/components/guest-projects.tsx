"use client";

import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Plus, X } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { Label } from "@multica/ui/components/ui/label";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { projectListOptions } from "@multica/core/projects/queries";
import type { Project } from "@multica/core/types";
import { ProjectIcon } from "../../projects/components/project-icon";
import { useT } from "../../i18n";

/** Projects an admin can share with a guest: the live ones it can see. */
export function useShareableProjects(wsId: string): Project[] {
  const { data } = useQuery(projectListOptions(wsId));
  return (data ?? []).filter((project) => !project.archived_at);
}

/**
 * The projects a guest (MUL-9420) can see, as removable chips with an
 * "Add project…" menu. A guest needs at least one.
 */
export function GuestProjectsField({
  id,
  projects,
  value,
  onChange,
}: {
  id: string;
  projects: Project[];
  value: string[];
  onChange: (next: string[]) => void;
}) {
  const { t } = useT("settings");
  const byId = new Map(projects.map((project) => [project.id, project]));
  const selected = value.map((projectId) => byId.get(projectId)).filter((p): p is Project => !!p);
  const remaining = projects.filter((project) => !value.includes(project.id));

  return (
    <div className="space-y-1.5">
      <Label id={id}>{t(($) => $.members.guest_projects_label)}</Label>
      <div
        role="group"
        aria-labelledby={id}
        className="flex min-h-9 flex-wrap items-center gap-1.5 rounded-md border border-input px-2 py-1.5"
      >
        {selected.map((project) => (
          <span
            key={project.id}
            className="inline-flex h-6 items-center gap-1 rounded-md bg-muted pr-1 pl-1.5 text-caption font-medium"
          >
            <ProjectIcon project={project} />
            <span className="max-w-40 truncate">{project.title}</span>
            <button
              type="button"
              className="inline-flex size-4 items-center justify-center rounded-sm text-muted-foreground hover:bg-surface-hover hover:text-foreground"
              aria-label={t(($) => $.members.guest_projects_remove, { project: project.title })}
              onClick={() => onChange(value.filter((projectId) => projectId !== project.id))}
            >
              <X className="size-3" />
            </button>
          </span>
        ))}
        {remaining.length > 0 && (
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <button
                  type="button"
                  className="inline-flex h-6 items-center gap-1 rounded-md px-1.5 text-caption text-muted-foreground hover:bg-surface-hover hover:text-foreground"
                >
                  <Plus className="size-3" />
                  {t(($) => $.members.guest_projects_add)}
                </button>
              }
            />
            <DropdownMenuContent align="start" className="max-h-72 w-56 overflow-y-auto">
              {remaining.map((project) => (
                <DropdownMenuItem key={project.id} onClick={() => onChange([...value, project.id])}>
                  <ProjectIcon project={project} />
                  <span className="truncate">{project.title}</span>
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
        )}
      </div>
      <p className="text-caption text-muted-foreground">{t(($) => $.members.guest_projects_hint)}</p>
    </div>
  );
}

/**
 * Picks a guest's projects, either to change them or to turn a member into a
 * guest. The caller does the request; a failure keeps the dialog open.
 */
export function GuestProjectsDialog({
  open,
  title,
  description,
  confirmLabel,
  projects,
  initial,
  onOpenChange,
  onConfirm,
}: {
  open: boolean;
  title: string;
  description?: string;
  confirmLabel: string;
  projects: Project[];
  initial: string[];
  onOpenChange: (open: boolean) => void;
  onConfirm: (projectIds: string[]) => Promise<void>;
}) {
  const { t } = useT("settings");
  const [value, setValue] = useState(initial);
  const [saving, setSaving] = useState(false);
  useEffect(() => {
    if (open) setValue(initial);
    // eslint-disable-next-line react-hooks/exhaustive-deps -- reset on open only
  }, [open]);

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!saving) onOpenChange(next);
      }}
    >
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          {description ? <DialogDescription>{description}</DialogDescription> : null}
        </DialogHeader>
        <GuestProjectsField id="guest-projects-dialog" projects={projects} value={value} onChange={setValue} />
        <DialogFooter>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
            {t(($) => $.members.confirm_cancel)}
          </Button>
          <Button
            type="button"
            disabled={saving || value.length === 0}
            aria-busy={saving || undefined}
            onClick={async () => {
              setSaving(true);
              try {
                await onConfirm(value);
              } finally {
                setSaving(false);
              }
            }}
          >
            {confirmLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
