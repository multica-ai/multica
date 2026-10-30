"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Link2, Plus, X } from "@multica/ui/icons";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { Input } from "@multica/ui/components/ui/input";
import {
  docTreeOptions,
  docVersionsOptions,
  folderAncestry,
  useLinkProjectDoc,
  useUnlinkProjectDoc,
} from "@multica/core/docs";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useActorName } from "@multica/core/workspace/hooks";
import { projectListOptions } from "@multica/core/projects/queries";
import type { Doc } from "@multica/core/types";
import { ActorAvatar } from "../../common/actor-avatar";
import { AppLink } from "../../navigation";
import { ProjectIcon } from "../../projects/components/project-icon";
import { useT, useTimeAgo } from "../../i18n";

function Property({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="grid grid-cols-[88px_minmax(0,1fr)] items-start gap-3 py-1.5 text-body">
      <span className="pt-0.5 text-caption text-muted-foreground">{label}</span>
      <div className="min-w-0">{children}</div>
    </div>
  );
}

function Section({ title, action, children }: { title: string; action?: React.ReactNode; children: React.ReactNode }) {
  return (
    <section className="border-b border-surface-border px-4 py-3 last:border-b-0">
      <div className="mb-1.5 flex h-6 items-center gap-2">
        <h3 className="flex-1 text-caption font-medium text-muted-foreground">{title}</h3>
        {action}
      </div>
      {children}
    </section>
  );
}

/** Properties and links of the open doc. */
export function DocSidePanel({ doc }: { doc: Doc }) {
  const { t } = useT("docs");
  const timeAgo = useTimeAgo();
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
  const { getActorName } = useActorName();
  const { data: tree } = useQuery(docTreeOptions(wsId));
  const { data: versions = [] } = useQuery(docVersionsOptions(wsId, doc.id));
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const link = useLinkProjectDoc();
  const unlink = useUnlinkProjectDoc();
  const [filter, setFilter] = useState("");

  const path = tree ? folderAncestry(tree.folders, doc.folder_id) : [];
  const editors = useMemo(() => {
    const seen = new Map<string, { type: "member" | "agent"; id: string }>();
    for (const v of versions) {
      const key = `${v.author_type}:${v.author_id}`;
      if (!seen.has(key)) seen.set(key, { type: v.author_type, id: v.author_id });
    }
    return [...seen.values()];
  }, [versions]);

  const linked = projects.filter((project) => doc.project_ids.includes(project.id));
  const linkable = projects.filter(
    (project) =>
      !doc.project_ids.includes(project.id) &&
      (!filter.trim() || project.title.toLowerCase().includes(filter.trim().toLowerCase())),
  );
  const onError = (error: unknown) => toast.error(error instanceof Error ? error.message : t(($) => $.errors.link));

  return (
    <div className="flex h-full flex-col overflow-y-auto">
      <Section title={t(($) => $.side.properties)}>
        <Property label={t(($) => $.side.location)}>
          <div className="flex flex-wrap items-center gap-1 text-body">
            <AppLink href={p.docs()} className="text-muted-foreground hover:text-foreground">
              {t(($) => $.kb)}
            </AppLink>
            {path.map((folder) => (
              <span key={folder.id} className="flex min-w-0 items-center gap-1">
                <span className="text-faint-foreground">/</span>
                <AppLink href={p.docFolder(folder.id)} className="truncate hover:underline">
                  {folder.title}
                </AppLink>
              </span>
            ))}
          </div>
        </Property>
        <Property label={t(($) => $.side.format)}>
          {doc.format === "html" ? t(($) => $.format.html) : t(($) => $.format.markdown)}
        </Property>
        <Property label={t(($) => $.side.created_by)}>
          <span className="flex items-center gap-1.5">
            <ActorAvatar actorType={doc.created_by_type} actorId={doc.created_by_id} size="xs" />
            <span className="truncate">{getActorName(doc.created_by_type, doc.created_by_id)}</span>
            <span className="shrink-0 text-muted-foreground">{timeAgo(doc.created_at)}</span>
          </span>
        </Property>
        <Property label={t(($) => $.side.edited_by)}>
          <span className="flex flex-wrap items-center gap-1">
            {editors.map((editor) => (
              <ActorAvatar
                key={`${editor.type}:${editor.id}`}
                actorType={editor.type}
                actorId={editor.id}
                size="xs"
                enableHoverCard
              />
            ))}
          </span>
        </Property>
        <Property label={t(($) => $.side.version)}>
          <span className="flex items-center gap-1.5">
            <span className="tabular-nums">{t(($) => $.version_number, { number: doc.version_count })}</span>
            <span className="text-faint-foreground">·</span>
            <AppLink href={p.docHistory(doc.id)} className="text-brand hover:underline">
              {t(($) => $.side.view_history)}
            </AppLink>
          </span>
        </Property>
      </Section>

      <Section
        title={t(($) => $.side.projects)}
        action={
          <DropdownMenu onOpenChange={(open) => !open && setFilter("")}>
            <DropdownMenuTrigger
              render={
                <button
                  type="button"
                  aria-label={t(($) => $.side.link_project)}
                  className="flex size-6 items-center justify-center rounded-md text-muted-foreground hover:bg-surface-hover hover:text-foreground"
                />
              }
            >
              <Plus className="size-3.5" />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-64">
              <div className="p-1">
                <Input
                  value={filter}
                  onChange={(event) => setFilter(event.target.value)}
                  onKeyDown={(event) => event.stopPropagation()}
                  placeholder={t(($) => $.side.search_projects)}
                  className="h-7"
                />
              </div>
              <div className="max-h-64 overflow-y-auto">
                {linkable.length === 0 ? (
                  <div className="px-2 py-2 text-caption text-muted-foreground">{t(($) => $.side.no_projects)}</div>
                ) : (
                  linkable.map((project) => (
                    <DropdownMenuItem
                      key={project.id}
                      onClick={() => link.mutate({ projectId: project.id, docId: doc.id }, { onError })}
                    >
                      <ProjectIcon project={project} />
                      <span className="truncate">{project.title}</span>
                    </DropdownMenuItem>
                  ))
                )}
              </div>
            </DropdownMenuContent>
          </DropdownMenu>
        }
      >
        {linked.length === 0 ? (
          <p className="flex items-center gap-1.5 py-1 text-caption text-faint-foreground">
            <Link2 className="size-3.5" />
            {t(($) => $.side.no_linked_projects)}
          </p>
        ) : (
          <ul className="space-y-px">
            {linked.map((project) => (
              <li key={project.id} className="group/link flex h-8 items-center gap-2 rounded-md px-1.5 hover:bg-surface-hover">
                <ProjectIcon project={project} />
                <AppLink href={p.projectDetail(project.id)} className="min-w-0 flex-1 truncate text-body">
                  {project.title}
                </AppLink>
                <button
                  type="button"
                  aria-label={t(($) => $.side.unlink_project, { title: project.title })}
                  onClick={() => unlink.mutate({ projectId: project.id, docId: doc.id }, { onError })}
                  className="flex size-5 items-center justify-center rounded text-muted-foreground opacity-0 hover:text-foreground group-hover/link:opacity-100 focus-visible:opacity-100"
                >
                  <X className="size-3.5" />
                </button>
              </li>
            ))}
          </ul>
        )}
      </Section>
    </div>
  );
}
