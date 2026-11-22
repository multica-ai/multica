"use client";
import { useQueryClient } from "@tanstack/react-query";
import { projectKeys } from "@multica/core/projects/queries";
import { issueKeys } from "@multica/core/issues/queries";
import type { Issue, Project, ListProjectsResponse } from "@multica/core/types";
import { useEffect, useState } from "react";
import { registerProjectSourceHost, useProjectSourceChoice } from "@multica/core/projects/source-choice";
import { useUpdateProject } from "@multica/core/projects/mutations";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { Dialog, DialogContent, DialogTitle, DialogDescription } from "@multica/ui/components/ui/dialog";
import { Button } from "@multica/ui/components/ui/button";
import { useNavigation } from "../navigation";
import { useT } from "../i18n";
export function ProjectSourceChoiceModal() {
  const qc = useQueryClient();
  const current = useProjectSourceChoice(s => s.current);
  const update = useUpdateProject(), wsId = useWorkspaceId(), paths = useWorkspacePaths(), nav = useNavigation();
  const { t } = useT("projects");
  const [error, setError] = useState<string | null>(null), [busy, setBusy] = useState(false);
  useEffect(() => registerProjectSourceHost((path, raw, decisions) => {
    const body = raw as Record<string, unknown>, data = (body.updates ?? body) as Record<string, unknown>;
    if (data.suppress_run)
      return;
    const match = path.match(/^\/api\/issues\/([^/]+)(?:\/move)?$/);
    const ids = Array.isArray(body.issue_ids) ? body.issue_ids : match ? [match[1]] : [null];
    for (const id of ids) {
      const issue = typeof id === "string" ? qc.getQueryData<Issue>(issueKeys.detail(wsId, id)) : undefined;
      const assignee = data.assignee_type ?? (data.agent_id ? "agent" : data.squad_id ? "squad" : issue?.assignee_type);
      const status = data.status ?? issue?.status ?? "todo";
      if (assignee !== "agent" && assignee !== "squad")
        continue;
      if (status === "backlog" || status === "done" || status === "cancelled")
        continue;
      if (issue && data.assignee_id === undefined && data.status === undefined && data.project_id === undefined)
        continue;
      const parent = typeof data.parent_issue_id === "string" ? qc.getQueryData<Issue>(issueKeys.detail(wsId, data.parent_issue_id)) : undefined;
      const projectId = ("project_id" in data ? data.project_id : issue?.project_id) ?? parent?.project_id;
      if (typeof projectId !== "string" || decisions[projectId])
        continue;
      const project = qc.getQueryData<Project>(projectKeys.detail(wsId, projectId)) ?? qc.getQueryData<ListProjectsResponse>(projectKeys.list(wsId))?.projects.find(p => p.id === projectId);
      if (project && project.resource_count === 0 && project.source_intent !== "no_code")
        return project;
    }
    return undefined;
  }), [qc, wsId]);
  useEffect(() => { setError(null); setBusy(false); }, [current]);
  useEffect(() => { if (current && current.project.workspace_id !== wsId)
    current.finish("cancel"); }, [current, wsId]);
  if (!current)
    return null;
  const save = async () => {
    setBusy(true);
    setError(null);
    try {
      await update.mutateAsync({ id: current.project.id, source_intent: "no_code" });
      current.finish("saved");
    }
    catch (e) {
      setError(e instanceof Error ? e.message : t($ => $.source.save_failed));
      setBusy(false);
    }
  };
  return <Dialog open onOpenChange={open => { if (!open && !busy)
    current.finish("cancel"); }}>
 <DialogContent className="sm:max-w-lg">
  <DialogTitle>{t($ => $.source.title)}</DialogTitle>
  <DialogDescription>{current.project.title} — {t($ => $.source.description)}</DialogDescription>
  {error && <p role="alert" className="text-destructive">{error}</p>}
  <div className="flex flex-col gap-2">
  <Button disabled={busy} onClick={() => { current.finish("cancel"); nav.push(paths.projectDetail(current.project.id)); }}>{t($ => $.source.configure)}</Button>
  <Button variant="outline" disabled={busy} onClick={() => current.finish("once")}>{t($ => $.source.once)}</Button>
  <Button variant="outline" disabled={busy} onClick={() => void save()}>{t($ => $.source.no_code)}</Button>
  </div>
 </DialogContent>
 </Dialog>;
}
