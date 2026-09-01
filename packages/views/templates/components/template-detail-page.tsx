"use client";

import { useState } from "react";
import { ArrowLeft, Bot, LayoutTemplate, Loader2, ShieldAlert, Trash2, Users } from "lucide-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { useCurrentWorkspace, useWorkspacePaths } from "@multica/core/paths";
import { marketplaceTemplateDetailOptions, marketplaceTemplateKeys } from "@multica/core/templates";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent, CardHeader } from "@multica/ui/components/ui/card";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { CollectionPageHeader, CollectionPageState } from "../../layout/collection-page";
import { AppLink, useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { ApplyTemplateDialog } from "./templates-page";

export function TemplateDetailPage() {
  const { t } = useT("templates");
  const workspace = useCurrentWorkspace();
  const wsId = workspace?.id ?? "";
  const p = useWorkspacePaths();
  const { pathname, push } = useNavigation();
  const templateId = pathname.split("/").pop() ?? "";
  const queryClient = useQueryClient();
  const [applyOpen, setApplyOpen] = useState(false);
  const { data: template, isPending, isError } = useQuery(marketplaceTemplateDetailOptions(wsId, templateId));
  const remove = useMutation({
    mutationFn: () => api.deleteMarketplaceTemplate(templateId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: marketplaceTemplateKeys.all(wsId) });
      toast.success(t(($) => $.detail.delete_success));
      push(p.templates());
    },
  });

  if (isPending) {
    return <div className="space-y-4 p-6"><Skeleton className="h-12" /><Skeleton className="h-80" /></div>;
  }
  if (isError || !template) {
    return <CollectionPageState icon={LayoutTemplate} title={t(($) => $.page.empty_title)} description={t(($) => $.page.empty_description)} />;
  }

  const snapshot = template.snapshot;
  const TypeIcon = template.source_type === "squad" ? Users : Bot;
  const visibilityCopy = template.visibility === "public"
    ? t(($) => $.card.public)
    : template.visibility === "workspace"
      ? t(($) => $.card.workspace)
      : t(($) => $.card.private);

  return (
    <div className="flex h-full min-h-0 flex-col">
      <CollectionPageHeader
        icon={TypeIcon}
        title={template.name}
        description={template.description}
        actions={
          <div className="flex gap-2">
            {template.can_manage ? (
              <Button variant="outline" size="sm" disabled={remove.isPending} onClick={() => remove.mutate()}>
                {remove.isPending ? <Loader2 className="size-4 animate-spin" /> : <Trash2 className="size-4" />}
                {t(($) => $.detail.delete)}
              </Button>
            ) : null}
            <Button size="sm" onClick={() => setApplyOpen(true)}>{t(($) => $.card.use)}</Button>
          </div>
        }
      />
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto w-full max-w-5xl space-y-5 px-4 py-5 md:px-6">
          <AppLink href={p.templates()} className="inline-flex items-center gap-1.5 text-caption text-muted-foreground hover:text-foreground">
            <ArrowLeft className="size-3.5" />
            {t(($) => $.detail.back)}
          </AppLink>
          <div className="flex flex-wrap gap-2">
            <Badge variant="outline">{template.source_type === "squad" ? t(($) => $.card.squad) : t(($) => $.card.agent)}</Badge>
            <Badge variant="outline">{visibilityCopy}</Badge>
            {template.tags.map((tag) => <Badge key={tag} variant="secondary">{tag}</Badge>)}
          </div>
          <div className="rounded-lg border border-warning/30 bg-warning/5 p-3 text-caption text-muted-foreground">
            <div className="flex gap-2">
              <ShieldAlert className="mt-0.5 size-4 shrink-0 text-warning" />
              <span>{t(($) => $.apply.warning_body)}</span>
            </div>
          </div>
          <section className="space-y-3">
            <h2 className="text-title font-medium">{t(($) => $.detail.agents)} · {snapshot?.agents.length ?? 0}</h2>
            <div className="grid gap-3">
              {(snapshot?.agents ?? []).map((agent) => {
                const role = snapshot?.squad?.members.find((member) => member.agent_key === agent.key)?.role ?? "";
                const isLeader = snapshot?.squad?.leader_key === agent.key;
                return (
                  <Card key={agent.key}>
                    <CardHeader className="pb-2">
                      <div className="flex flex-wrap items-center gap-2">
                        <h3 className="text-body font-medium">{agent.name}</h3>
                        {isLeader ? <Badge>{t(($) => $.detail.leader)}</Badge> : null}
                        {role ? <Badge variant="outline">{role}</Badge> : null}
                      </div>
                      <p className="text-caption text-muted-foreground">{agent.description}</p>
                    </CardHeader>
                    <CardContent>
                      <details>
                        <summary className="cursor-pointer text-caption font-medium">{t(($) => $.detail.instructions)}</summary>
                        <pre className="mt-3 max-h-80 overflow-auto whitespace-pre-wrap rounded-lg bg-muted p-3 text-caption leading-relaxed">{agent.instructions}</pre>
                      </details>
                    </CardContent>
                  </Card>
                );
              })}
            </div>
          </section>
          <section className="space-y-3">
            <h2 className="text-title font-medium">{t(($) => $.detail.skills)} · {snapshot?.skills.length ?? 0}</h2>
            <div className="flex flex-wrap gap-2">
              {(snapshot?.skills ?? []).map((skill) => <Badge key={skill.key} variant="secondary">{skill.name}</Badge>)}
            </div>
          </section>
        </div>
      </div>
      <ApplyTemplateDialog templateId={applyOpen ? template.id : null} onOpenChange={setApplyOpen} wsId={wsId} />
    </div>
  );
}
