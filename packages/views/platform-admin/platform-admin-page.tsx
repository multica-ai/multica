"use client";

import { useState, type CSSProperties, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { configStore } from "@multica/core/config";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Switch } from "@multica/ui/components/ui/switch";
import { Tabs, TabsList, TabsContent, TabsTrigger } from "@multica/ui/components/ui/tabs";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { useT } from "../i18n";
import { DragStrip } from "../platform";

const settingsKey = ["platform-admin", "settings"];
const workspacesKey = ["platform-admin", "workspaces"];
const usersKey = ["platform-admin", "users"];
const membersKey = (id: string) => ["platform-admin", "members", id];
type Setting = Awaited<ReturnType<typeof api.getPlatformSettings>>[number];
type Workspace = Awaited<ReturnType<typeof api.createPlatformWorkspace>>;

export function PlatformAdminPage({ onBack }: { onBack: () => void }) {
  const { t } = useT("workspace");
  const isAdmin = useAuthStore((s) => s.user?.platform_admin === true);

  return (
    <div className="flex h-full flex-col overflow-hidden bg-background">
      <DragStrip />
      <header className="shrink-0 border-b border-border" style={{ WebkitAppRegion: "no-drag" } as CSSProperties}>
        <div className="mx-auto flex w-full max-w-5xl items-center justify-between gap-4 px-6 py-3">
          <h1 className="text-display-sm font-semibold">{t(($) => $.platform_admin.title)}</h1>
          <Button variant="ghost" onClick={onBack}>{t(($) => $.platform_admin.back)}</Button>
        </div>
      </header>
      <main className="min-h-0 flex-1 overflow-y-auto">
        {!isAdmin ? (
          <p className="mx-auto max-w-5xl px-6 py-8 text-body text-muted-foreground">
            {t(($) => $.platform_admin.forbidden)}
          </p>
        ) : (
          <Tabs defaultValue="workspaces" className="mx-auto w-full max-w-5xl gap-5 px-6 py-6">
            <TabsList>
              <TabsTrigger value="workspaces">{t(($) => $.platform_admin.workspaces)}</TabsTrigger>
              <TabsTrigger value="users">{t(($) => $.platform_admin.users)}</TabsTrigger>
              <TabsTrigger value="settings">{t(($) => $.platform_admin.settings)}</TabsTrigger>
            </TabsList>
            <TabsContent value="workspaces"><WorkspacesPanel /></TabsContent>
            <TabsContent value="users"><UsersPanel /></TabsContent>
            <TabsContent value="settings"><SettingsPanel /></TabsContent>
          </Tabs>
        )}
      </main>
    </div>
  );
}

function QueryStatus({ error, pending, retry }: { error: Error | null; pending: boolean; retry: () => void }) {
  const { t } = useT("workspace");
  if (error) {
    return (
      <div role="alert" className="flex items-center gap-3 py-3 text-body">
        <span>{error.message}</span>
        <Button variant="outline" size="sm" onClick={retry}>{t(($) => $.platform_admin.retry)}</Button>
      </div>
    );
  }
  return pending ? <p role="status" className="py-3 text-body text-muted-foreground">{t(($) => $.platform_admin.loading)}</p> : null;
}

function useDirectoryCursor() {
  const [search, setSearch] = useState("");
  const [cursors, setCursors] = useState([""]);
  return {
    search,
    setSearch: (value: string) => {
      setSearch(value);
      setCursors([""]);
    },
    after: cursors[cursors.length - 1]!,
    previous: cursors.length > 1 ? () => setCursors((pages) => pages.slice(0, -1)) : undefined,
    next: (cursor: string) => setCursors((pages) => [...pages, cursor]),
  };
}

function DirectoryPagination({ previous, next }: { previous?: () => void; next?: () => void }) {
  const { t } = useT("workspace");
  if (!previous && !next) return null;
  return (
    <div className="flex justify-end gap-2 pt-3">
      <Button variant="outline" size="sm" disabled={!previous} onClick={previous}>{t(($) => $.platform_admin.previous)}</Button>
      <Button variant="outline" size="sm" disabled={!next} onClick={next}>{t(($) => $.platform_admin.next)}</Button>
    </div>
  );
}

function WorkspacesPanel() {
  const { t } = useT("workspace");
  const [selected, setSelected] = useState<Workspace | null>(null);
  const page = useDirectoryCursor();
  const query = useQuery({
    queryKey: [...workspacesKey, page.search, page.after],
    queryFn: ({ signal }) => api.listPlatformWorkspaces(page.search, page.after, signal),
  });

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <Input className="max-w-xs" type="search" aria-label={t(($) => $.platform_admin.search_workspaces)}
          placeholder={t(($) => $.platform_admin.search_workspaces)} value={page.search}
          onChange={(event) => page.setSearch(event.target.value)} />
        <CreateWorkspaceDialog onCreated={setSelected} />
      </div>
      <QueryStatus error={query.error} pending={query.isPending} retry={() => void query.refetch()} />
      {query.data && (
        <div className="grid items-start gap-4 md:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
          <div className="min-w-0">
            <ul className="divide-y divide-border rounded-lg border border-border">
              {query.data.items.length === 0 && (
                <li className="px-4 py-4 text-body text-muted-foreground">{t(($) => $.platform_admin.no_workspaces)}</li>
              )}
              {query.data.items.map((workspace) => (
                <li key={workspace.id}>
                  <button type="button" aria-pressed={selected?.id === workspace.id}
                    onClick={() => setSelected(workspace)}
                    className="flex w-full flex-col items-start gap-1 px-4 py-3 text-start hover:bg-surface-selected aria-pressed:bg-surface-selected">
                    <span className="max-w-full truncate text-body font-medium">{workspace.name}</span>
                    <span className="max-w-full truncate text-caption text-muted-foreground">/{workspace.slug}</span>
                  </button>
                </li>
              ))}
            </ul>
            <DirectoryPagination previous={page.previous} next={query.data.next ? () => page.next(query.data.next) : undefined} />
          </div>
          {selected && <WorkspaceMembers key={selected.id} workspace={selected} />}
        </div>
      )}
    </div>
  );
}

function CreateWorkspaceDialog({ onCreated }: { onCreated: (workspace: Workspace) => void }) {
  const { t } = useT("workspace");
  const queryClient = useQueryClient();
  const refreshMe = useAuthStore((s) => s.refreshMe);
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [owner, setOwner] = useState("");
  const create = useMutation({
    mutationFn: () => api.createPlatformWorkspace({ name, slug, owner_email: owner }),
    onSuccess: (workspace) => {
      void queryClient.invalidateQueries({ queryKey: workspacesKey });
      void queryClient.invalidateQueries({ queryKey: usersKey });
      void queryClient.invalidateQueries({ queryKey: workspaceKeys.list() });
      if (owner.trim().toLowerCase() === useAuthStore.getState().user?.email.toLowerCase()) {
        void refreshMe().catch(() => undefined);
      }
      if (workspace.id) onCreated(workspace);
      setOpen(false);
      setName("");
      setSlug("");
      setOwner("");
    },
  });
  const submit = (event: FormEvent) => {
    event.preventDefault();
    create.mutate();
  };
  return (
    <Dialog open={open} onOpenChange={(next) => {
      if (create.isPending) return;
      if (next) create.reset();
      setOpen(next);
    }}>
      <DialogTrigger render={<Button />}>{t(($) => $.platform_admin.create_workspace)}</DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t(($) => $.platform_admin.create_workspace)}</DialogTitle>
          <DialogDescription>{t(($) => $.platform_admin.owner_description)}</DialogDescription>
        </DialogHeader>
        <form onSubmit={submit} className="flex flex-col gap-4">
          <label className="flex flex-col gap-1.5 text-body">
            {t(($) => $.platform_admin.workspace_name)}
            <Input required value={name} onChange={(event) => setName(event.target.value)} />
          </label>
          <label className="flex flex-col gap-1.5 text-body">
            {t(($) => $.platform_admin.workspace_slug)}
            <Input required pattern="[a-z0-9]+(?:-[a-z0-9]+)*" value={slug} onChange={(event) => setSlug(event.target.value)} />
          </label>
          <label className="flex flex-col gap-1.5 text-body">
            {t(($) => $.platform_admin.owner_email)}
            <Input type="email" required value={owner} onChange={(event) => setOwner(event.target.value)} />
          </label>
          {create.error && <p role="alert" className="text-body text-destructive">{create.error.message}</p>}
          <DialogFooter>
            <Button type="submit" aria-busy={create.isPending} disabled={create.isPending}>{t(($) => $.platform_admin.create_workspace)}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function WorkspaceMembers({ workspace }: { workspace: Workspace }) {
  const { t } = useT("workspace");
  const queryClient = useQueryClient();
  const [email, setEmail] = useState("");
  const query = useQuery({
    queryKey: membersKey(workspace.id),
    queryFn: ({ signal }) => api.listPlatformWorkspaceMembers(workspace.id, signal),
  });
  const assign = useMutation({
    mutationFn: () => api.assignPlatformWorkspaceMember(workspace.id, email),
    onSuccess: () => {
      setEmail("");
      void queryClient.invalidateQueries({ queryKey: membersKey(workspace.id) });
      void queryClient.invalidateQueries({ queryKey: usersKey });
      void queryClient.invalidateQueries({ queryKey: workspaceKeys.list() });
    },
  });
  return (
    <section className="flex min-w-0 flex-col gap-3 rounded-lg border border-border p-4">
      <h2 className="break-words text-title font-semibold">{workspace.name}</h2>
      <h3 className="text-body font-medium">{t(($) => $.platform_admin.members)}</h3>
      <QueryStatus error={query.error} pending={query.isPending} retry={() => void query.refetch()} />
      <ul className="divide-y divide-border">
        {query.data?.map((member) => (
          <li key={member.id} className="flex items-center justify-between gap-3 py-2">
            <div className="min-w-0">
              <p className="truncate text-body">{member.user_name}</p>
              <p className="truncate text-caption text-muted-foreground">{member.user_email}</p>
            </div>
            <span className="text-caption text-muted-foreground">
              {member.role === "owner" ? t(($) => $.platform_admin.owner)
                : member.role === "admin" ? t(($) => $.platform_admin.workspace_admin)
                : member.role === "guest" ? t(($) => $.platform_admin.guest)
                : member.role === "member" ? t(($) => $.platform_admin.member) : member.role}
            </span>
          </li>
        ))}
      </ul>
      <form onSubmit={(event) => { event.preventDefault(); assign.mutate(); }} className="flex flex-wrap items-end gap-2">
        <label className="flex min-w-40 flex-1 flex-col gap-1.5 text-body">
          {t(($) => $.platform_admin.member_email)}
          <Input type="email" required value={email} onChange={(event) => setEmail(event.target.value)} />
        </label>
        <Button type="submit" aria-busy={assign.isPending} disabled={assign.isPending}>{t(($) => $.platform_admin.assign)}</Button>
      </form>
      {assign.error && <p role="alert" className="text-body text-destructive">{assign.error.message}</p>}
      {assign.isSuccess && <p role="status" className="text-caption text-muted-foreground">{t(($) => $.platform_admin.assigned)}</p>}
    </section>
  );
}

function UsersPanel() {
  const { t } = useT("workspace");
  const page = useDirectoryCursor();
  const query = useQuery({
    queryKey: [...usersKey, page.search, page.after],
    queryFn: ({ signal }) => api.listPlatformUsers(page.search, page.after, signal),
  });
  return (
    <div className="flex flex-col gap-3">
      <Input className="max-w-xs" type="search" aria-label={t(($) => $.platform_admin.search_users)}
        placeholder={t(($) => $.platform_admin.search_users)} value={page.search}
        onChange={(event) => page.setSearch(event.target.value)} />
      <QueryStatus error={query.error} pending={query.isPending} retry={() => void query.refetch()} />
      {query.data && (
        <>
          <ul className="divide-y divide-border rounded-lg border border-border">
            {query.data.items.length === 0 && <li className="px-4 py-3 text-body text-muted-foreground">{t(($) => $.platform_admin.no_users)}</li>}
            {query.data.items.map((user) => (
              <li key={user.id} className="flex items-center justify-between gap-3 px-4 py-3">
                <div className="min-w-0">
                  <p className="truncate text-body font-medium">{user.name}</p>
                  <p className="truncate text-caption text-muted-foreground">{user.email}</p>
                </div>
                <span className="text-end text-caption text-muted-foreground">
                  {user.has_workspace ? t(($) => $.platform_admin.assigned) : t(($) => $.platform_admin.unassigned)}
                </span>
              </li>
            ))}
          </ul>
          <DirectoryPagination previous={page.previous} next={query.data.next ? () => page.next(query.data.next) : undefined} />
        </>
      )}
    </div>
  );
}

const workspaceSettings = ["allow_workspace_creation", "workspace_creation_allowed_emails", "auto_create_workspace"];
const registrationSettings = ["allow_signup", "signup_allowed_emails", "signup_allowed_domains"];

function SettingsPanel() {
  const { t } = useT("workspace");
  const query = useQuery({ queryKey: settingsKey, queryFn: ({ signal }) => api.getPlatformSettings(signal) });
  const rows = query.data ?? [];
  const groups = [
    { title: t(($) => $.platform_admin.workspace_policy), rows: rows.filter((row) => workspaceSettings.includes(row.key)) },
    { title: t(($) => $.platform_admin.registration), rows: rows.filter((row) => registrationSettings.includes(row.key)) },
    { title: t(($) => $.platform_admin.features), rows: rows.filter((row) => !workspaceSettings.includes(row.key) && !registrationSettings.includes(row.key)) },
  ];
  return (
    <div className="flex flex-col gap-6">
      <QueryStatus error={query.error} pending={query.isPending} retry={() => void query.refetch()} />
      {groups.filter((group) => group.rows.length > 0).map((group) => (
        <section key={group.title}>
          <h2 className="mb-2 text-title font-semibold">{group.title}</h2>
          <ul className="divide-y divide-border rounded-lg border border-border">
            {group.rows.map((setting) => <SettingRow key={JSON.stringify(setting)} setting={setting} />)}
          </ul>
        </section>
      ))}
    </div>
  );
}

function SettingRow({ setting }: { setting: Setting }) {
  const { t } = useT("workspace");
  const queryClient = useQueryClient();
  const refreshMe = useAuthStore((s) => s.refreshMe);
  const [draft, setDraft] = useState(Array.isArray(setting.value) ? setting.value.join("\n") : "");
  const save = useMutation({
    mutationFn: (value: Setting["value"] | null) => api.updatePlatformSetting(setting.key, value),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: settingsKey });
      try {
        const config = await api.getConfig();
        const store = configStore.getState();
        store.setAuthConfig({
          allowSignup: config.allow_signup,
          googleClientId: config.google_client_id,
          workspaceCreationDisabled: config.workspace_creation_disabled,
          vcsIntegrationAvailable: config.vcs_integration_available,
        });
        store.setFeatureFlags(config.feature_flags);
        await refreshMe();
      } catch {
        // The saved decision is already authoritative on the server.
      }
    },
  });
  const labels = t(($) => $.platform_admin.keys, { returnObjects: true }) as Record<string, string>;
  const title = labels[setting.key] ?? setting.key;
  const source = setting.source === "platform" ? t(($) => $.platform_admin.override)
    : setting.source === "default" ? t(($) => $.platform_admin.default_value)
    : t(($) => $.platform_admin.deployment);
  const description = setting.key === "auto_create_workspace" ? t(($) => $.platform_admin.auto_description)
    : setting.key === "workspace_creation_allowed_emails" ? t(($) => $.platform_admin.allowlist_description)
    : setting.key === "allow_signup" ? t(($) => $.platform_admin.registration_description)
    : null;
  return (
    <li className="flex flex-col gap-2 p-4">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div className="min-w-0">
          <label htmlFor={setting.key} className="text-body font-medium">{title}</label>
          <p className="text-caption text-muted-foreground">{source}</p>
          {description && <p className="mt-1 text-caption text-muted-foreground">{description}</p>}
          {!setting.available && <p className="mt-1 text-caption text-muted-foreground">{t(($) => $.platform_admin.unavailable)}</p>}
        </div>
        <div className="flex items-center gap-2">
          {setting.source === "platform" && (
            <Button variant="ghost" size="sm" disabled={save.isPending} onClick={() => save.mutate(null)}>
              {t(($) => $.platform_admin.reset)}
            </Button>
          )}
          {typeof setting.value === "boolean" && (
            <Switch id={setting.key} checked={setting.value} disabled={save.isPending}
              onCheckedChange={(checked) => save.mutate(checked)} />
          )}
        </div>
      </div>
      {Array.isArray(setting.value) && (
        <form className="flex flex-col items-start gap-2" onSubmit={(event) => {
          event.preventDefault();
          save.mutate(draft.split(/[\n,]/).map((value) => value.trim()).filter(Boolean));
        }}>
          <Textarea id={setting.key} rows={2} value={draft} onChange={(event) => setDraft(event.target.value)} />
          <Button type="submit" variant="outline" size="sm" disabled={save.isPending || draft === setting.value.join("\n")}>
            {t(($) => $.platform_admin.save)}
          </Button>
        </form>
      )}
      {save.error && <p role="alert" className="text-body text-destructive">{save.error.message}</p>}
    </li>
  );
}
