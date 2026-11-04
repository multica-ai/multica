"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Switch } from "@multica/ui/components/ui/switch";
import { Spinner } from "@multica/ui/components/ui/spinner";
import { ExternalLink } from "@multica/ui/icons";
import {
  knowledgeRepoSyncOptions,
  knowledgeSourcesOptions,
  useRevealKnowledgeOwnerPassword,
  useRunKnowledgeRepoSync,
  useSaveKnowledgeRepoSync,
  useTestKnowledgeModels,
  useSetKnowledgeVisibility,
  useUpdateKnowledgeBaseSettings,
  type KnowledgeBase,
  type KnowledgeOwnerLogin,
  type KnowledgeRepoSync,
  type KnowledgeRepoSyncRequest,
  type KnowledgeSource,
} from "@multica/core/knowledge";
import { useT } from "../i18n";
import { KnowledgeAccessEditor } from "./knowledge-access";

/**
 * The owner's and admins' side of the knowledge base: the data sources WeKnora
 * syncs, each base's restricted access and LLM switches, the model connection
 * test, and the owner's WeKnora password. Only owners and admins reach it.
 */
export function KnowledgeSetupPanel({
  wsId,
  knowledgeBases,
}: {
  wsId: string;
  knowledgeBases: KnowledgeBase[];
}) {
  return (
    <div className="flex flex-col gap-8">
      <KnowledgeSourcesSection wsId={wsId} />
      <KnowledgeRepoSyncSection wsId={wsId} />
      <KnowledgeBasesSection wsId={wsId} knowledgeBases={knowledgeBases} />
      <KnowledgeModelsSection />
      <KnowledgeOwnerLoginSection />
    </div>
  );
}

function SectionHeading({ title, description }: { title: string; description?: string }) {
  return (
    <div className="flex flex-col gap-1">
      <h3 className="text-sm font-medium">{title}</h3>
      {description ? <p className="text-xs text-muted-foreground">{description}</p> : null}
    </div>
  );
}

function KnowledgeSourcesSection({ wsId }: { wsId: string }) {
  const { t } = useT("knowledge");
  const sources = useQuery(knowledgeSourcesOptions(wsId));

  const statusLabel = (source: KnowledgeSource) => {
    switch (source.status) {
      case "active":
        return t(($) => $.sources.status.active);
      case "paused":
        return t(($) => $.sources.status.paused);
      case "error":
        return t(($) => $.sources.status.error);
      default:
        return t(($) => $.sources.status.unknown);
    }
  };

  return (
    <section className="flex flex-col gap-3">
      <SectionHeading title={t(($) => $.sources.title)} description={t(($) => $.sources.description)} />
      {sources.isPending ? (
        <Spinner />
      ) : sources.isError || !sources.data || sources.data.state === "unknown" ? (
        <p className="text-sm text-destructive">{t(($) => $.sources.load_error)}</p>
      ) : sources.data.state === "offline" ? (
        <p className="text-sm text-muted-foreground">{t(($) => $.sources.offline)}</p>
      ) : sources.data.sources.length === 0 ? (
        <p className="text-sm text-muted-foreground">{t(($) => $.sources.empty)}</p>
      ) : (
        <ul className="flex flex-col divide-y rounded-md border">
          {sources.data.sources.map((source) => (
            <li key={source.id} className="flex flex-col gap-1 px-3 py-2 text-sm">
              <div className="flex items-center gap-2">
                <span className="flex-1 truncate font-medium">{source.name}</span>
                <Badge variant="outline">{source.type}</Badge>
                <Badge variant={source.status === "error" ? "destructive" : "secondary"}>{statusLabel(source)}</Badge>
              </div>
              <span className="text-xs text-muted-foreground">
                {source.last_sync_at
                  ? t(($) => $.sources.last_sync, { when: new Date(source.last_sync_at).toLocaleString() })
                  : t(($) => $.sources.never_synced)}
                {" · "}
                {t(($) => $.sources.items, { count: source.total_items_synced })}
              </span>
              {source.error_message ? (
                <span className="text-xs text-destructive">{source.error_message}</span>
              ) : null}
            </li>
          ))}
        </ul>
      )}
      {sources.data?.ui_url ? (
        <div>
          <Button variant="outline" size="sm" render={<a href={sources.data.ui_url} target="_blank" rel="noreferrer" />}>
            <ExternalLink />
            {t(($) => $.sources.create)}
          </Button>
        </div>
      ) : null}
    </section>
  );
}

function toRepoSyncRequest(setting: KnowledgeRepoSync): KnowledgeRepoSyncRequest {
  return {
    enabled: setting.enabled,
    repo_url: setting.repo_url,
    branch: setting.branch,
    docs_path: setting.docs_path,
    knowledge_base_key: setting.knowledge_base_key,
  };
}

/**
 * The repo-docs sync (plan 10.12): a repository's docs directory kept in a
 * knowledge base. Saving does not clone; Sync now does, and so does the daily
 * job while the setting is on.
 */
function KnowledgeRepoSyncSection({ wsId }: { wsId: string }) {
  const { t } = useT("knowledge");
  const setting = useQuery(knowledgeRepoSyncOptions(wsId));
  const save = useSaveKnowledgeRepoSync(wsId);
  const run = useRunKnowledgeRepoSync(wsId);
  // The form holds local edits until saved; it starts from the server's setting.
  const [draft, setDraft] = useState<KnowledgeRepoSyncRequest | null>(null);

  if (setting.isPending) {
    return null;
  }
  if (setting.isError || !setting.data) {
    return (
      <section className="flex flex-col gap-3">
        <SectionHeading title={t(($) => $.repo.title)} />
        <p className="text-sm text-destructive">{t(($) => $.repo.load_error)}</p>
      </section>
    );
  }
  const current = draft ?? toRepoSyncRequest(setting.data);
  const edit = (patch: Partial<KnowledgeRepoSyncRequest>) => setDraft({ ...current, ...patch });
  const fail = (error: Error) => toast.error(error.message || t(($) => $.repo.toast.failed));

  return (
    <section className="flex flex-col gap-3">
      <SectionHeading title={t(($) => $.repo.title)} description={t(($) => $.repo.description)} />
      <label className="flex items-center gap-2 text-sm">
        <Switch
          checked={current.enabled}
          disabled={save.isPending}
          aria-label={t(($) => $.repo.enabled)}
          onCheckedChange={(checked) => edit({ enabled: checked })}
        />
        {t(($) => $.repo.enabled)}
      </label>
      <div className="grid gap-3 sm:grid-cols-2">
        <div className="flex flex-col gap-1 sm:col-span-2">
          <Label htmlFor="repo-sync-url">{t(($) => $.repo.repo_url)}</Label>
          <Input
            id="repo-sync-url"
            type="url"
            value={current.repo_url}
            placeholder="https://github.com/acme/app.git"
            onChange={(e) => edit({ repo_url: e.target.value })}
          />
        </div>
        <div className="flex flex-col gap-1">
          <Label htmlFor="repo-sync-branch">{t(($) => $.repo.branch)}</Label>
          <Input id="repo-sync-branch" value={current.branch} onChange={(e) => edit({ branch: e.target.value })} />
        </div>
        <div className="flex flex-col gap-1">
          <Label htmlFor="repo-sync-path">{t(($) => $.repo.docs_path)}</Label>
          <Input id="repo-sync-path" value={current.docs_path} onChange={(e) => edit({ docs_path: e.target.value })} />
        </div>
        <div className="flex flex-col gap-1">
          <Label htmlFor="repo-sync-base">{t(($) => $.repo.base)}</Label>
          <Input
            id="repo-sync-base"
            value={current.knowledge_base_key}
            onChange={(e) => edit({ knowledge_base_key: e.target.value })}
          />
        </div>
      </div>
      <div className="flex flex-wrap items-center gap-2">
        <Button
          variant="outline"
          size="sm"
          disabled={save.isPending || draft === null}
          onClick={() => {
            if (!draft) return;
            save.mutate(draft, {
              onError: fail,
              onSuccess: () => {
                setDraft(null);
                toast.success(t(($) => $.repo.toast.saved));
              },
            });
          }}
        >
          {t(($) => $.repo.save)}
        </Button>
        <Button
          variant="outline"
          size="sm"
          disabled={run.isPending || save.isPending || setting.data.repo_url === "" || draft !== null}
          aria-busy={run.isPending}
          onClick={() =>
            run.mutate(undefined, {
              onError: fail,
              onSuccess: (result) => {
                if (result.last_error) {
                  toast.error(t(($) => $.repo.last_error));
                } else {
                  toast.success(t(($) => $.repo.toast.synced));
                }
              },
            })
          }
        >
          {run.isPending ? <Spinner /> : null}
          {t(($) => $.repo.run)}
        </Button>
        <span className="text-xs text-muted-foreground">
          {setting.data.last_synced_at
            ? t(($) => $.repo.last_sync, { when: new Date(setting.data.last_synced_at).toLocaleString() })
            : t(($) => $.repo.never_synced)}
        </span>
      </div>
      {setting.data.last_error ? (
        <p className="whitespace-pre-line text-xs text-destructive">{setting.data.last_error}</p>
      ) : null}
    </section>
  );
}

function KnowledgeBasesSection({ wsId, knowledgeBases }: { wsId: string; knowledgeBases: KnowledgeBase[] }) {
  const { t } = useT("knowledge");
  const setVisibility = useSetKnowledgeVisibility(wsId);
  const settings = useUpdateKnowledgeBaseSettings(wsId);
  const [open, setOpen] = useState<string | null>(null);

  const fail = (error: Error) => toast.error(error.message || t(($) => $.bases.toast.failed));

  return (
    <section className="flex flex-col gap-3">
      <SectionHeading title={t(($) => $.bases.title)} description={t(($) => $.bases.description)} />
      <ul className="flex flex-col divide-y rounded-md border">
        {knowledgeBases.map((base) => {
          const restricted = base.visibility === "restricted";
          const expanded = open === base.key;
          return (
            <li key={base.key} className="flex flex-col gap-3 px-3 py-3 text-sm">
              <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
                <span className="flex-1 font-medium">{base.name}</span>
                <label className="flex items-center gap-2 text-xs">
                  <Switch
                    checked={restricted}
                    disabled={setVisibility.isPending}
                    aria-label={t(($) => $.bases.restricted)}
                    onCheckedChange={(checked) =>
                      setVisibility.mutate(
                        { key: base.key, visibility: checked ? "restricted" : "workspace" },
                        { onError: fail, onSuccess: () => setOpen(checked ? base.key : null) },
                      )
                    }
                  />
                  {t(($) => $.bases.restricted)}
                </label>
                <label className="flex items-center gap-2 text-xs">
                  <Switch
                    checked={base.wiki_enabled}
                    disabled={settings.isPending}
                    aria-label={t(($) => $.bases.wiki)}
                    onCheckedChange={(checked) =>
                      settings.mutate({ key: base.key, wiki_enabled: checked }, { onError: fail })
                    }
                  />
                  {t(($) => $.bases.wiki)}
                </label>
                <label className="flex items-center gap-2 text-xs">
                  <Switch
                    checked={base.summaries_enabled}
                    disabled={settings.isPending}
                    aria-label={t(($) => $.bases.summaries)}
                    onCheckedChange={(checked) =>
                      settings.mutate({ key: base.key, summaries_enabled: checked }, { onError: fail })
                    }
                  />
                  {t(($) => $.bases.summaries)}
                </label>
                {restricted ? (
                  <Button variant="outline" size="sm" onClick={() => setOpen(expanded ? null : base.key)} aria-expanded={expanded}>
                    {t(($) => $.bases.access)}
                  </Button>
                ) : null}
              </div>
              {restricted && expanded ? <KnowledgeAccessEditor wsId={wsId} baseKey={base.key} /> : null}
            </li>
          );
        })}
      </ul>
    </section>
  );
}

function KnowledgeModelsSection() {
  const { t } = useT("knowledge");
  const test = useTestKnowledgeModels();
  const result = test.data;

  return (
    <section className="flex flex-col gap-3">
      <SectionHeading title={t(($) => $.models.title)} description={t(($) => $.models.description)} />
      <div>
        <Button variant="outline" onClick={() => test.mutate(undefined, { onError: (e) => toast.error(e.message) })} disabled={test.isPending} aria-busy={test.isPending}>
          {t(($) => $.models.test)}
        </Button>
      </div>
      {result && !result.configured ? (
        <p className="text-sm text-muted-foreground">{t(($) => $.models.not_configured)}</p>
      ) : null}
      {result?.configured ? (
        <ul className="flex flex-col divide-y rounded-md border">
          {result.checks.map((check) => (
            <li key={check.role} className="flex flex-col gap-1 px-3 py-2 text-sm">
              <div className="flex items-center gap-2">
                <span className="flex-1 truncate">{check.name}</span>
                <Badge variant="outline">{t(($) => $.models.role[roleKey(check.role)])}</Badge>
                <Badge variant={check.ok ? "secondary" : "destructive"}>
                  {check.ok ? t(($) => $.models.ok) : t(($) => $.models.failed)}
                </Badge>
              </div>
              {!check.ok && check.error ? <span className="text-xs text-destructive">{check.error}</span> : null}
            </li>
          ))}
        </ul>
      ) : null}
    </section>
  );
}

function roleKey(role: string): "chat" | "rerank" | "vision" | "unknown" {
  return role === "chat" || role === "rerank" || role === "vision" ? role : "unknown";
}

function KnowledgeOwnerLoginSection() {
  const { t } = useT("knowledge");
  const reveal = useRevealKnowledgeOwnerPassword();
  const [login, setLogin] = useState<KnowledgeOwnerLogin | null>(null);

  const show = () =>
    reveal.mutate(undefined, {
      onSuccess: (value) => setLogin(value.password ? value : null),
      onError: (error) => toast.error(error.message || t(($) => $.owner_login.failed)),
    });

  return (
    <section className="flex flex-col gap-3">
      <SectionHeading title={t(($) => $.owner_login.title)} description={t(($) => $.owner_login.description)} />
      {login ? (
        <div className="flex flex-col gap-2 rounded-md border p-3 text-sm">
          <span>{t(($) => $.owner_login.email, { email: login.email })}</span>
          <code className="select-all rounded bg-muted px-2 py-1 font-mono text-xs">{login.password}</code>
          <span className="text-xs text-muted-foreground">{t(($) => $.owner_login.once)}</span>
          {login.ui_url ? (
            <div>
              <Button variant="outline" size="sm" render={<a href={login.ui_url} target="_blank" rel="noreferrer" />}>
                <ExternalLink />
                {t(($) => $.owner_login.open)}
              </Button>
            </div>
          ) : null}
        </div>
      ) : (
        <div>
          <Button variant="outline" onClick={show} disabled={reveal.isPending} aria-busy={reveal.isPending}>
            {t(($) => $.owner_login.show)}
          </Button>
        </div>
      )}
    </section>
  );
}
