"use client";
import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useAuthStore } from "@multica/core/auth";
import { memberListOptions } from "@multica/core/workspace/queries";
import {
  graphCredentialsOptions,
  useSaveGraphCredential,
  useDeleteGraphCredential,
  useVerifyGraphAccess,
  useUpdateGraphAuth,
  type GraphCredential,
  type GraphAuthInput,
} from "@multica/core/workspace/codegraph";
import type { CodeGraphRepository } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { useT } from "../../i18n";
import { MoreHorizontal } from "@multica/ui/icons";

export function GitNotice({ children }: { children: React.ReactNode }) {
  return (
    <p className="rounded-lg border border-warning/40 bg-warning/10 p-3 text-body text-foreground break-words">
      {children}
    </p>
  );
}
export function useCanManageGit() {
  const wsId = useWorkspaceId();
  const userId = useAuthStore((state) => state.user?.id);
  const members = useQuery(memberListOptions(wsId));
  return (
    members.data?.some(
      (member) =>
        member.user_id === userId &&
        (member.role === "owner" || member.role === "admin"),
    ) === true
  );
}
export function GitCredentialManager() {
  const { t } = useT("layout");
  const wsId = useWorkspaceId();
  const allowed = useCanManageGit();
  const [open, setOpen] = useState(false);
  const [form, setForm] = useState(false);
  const [editing, setEditing] = useState<GraphCredential>();
  const [active, setActive] = useState<string>();
  const [deleting, setDeleting] = useState<GraphCredential>();
  const [name, setName] = useState("");
  const [service, setService] = useState("https://git.example.com");
  const [token, setToken] = useState("");
  const list = useQuery(graphCredentialsOptions(wsId, allowed && open));
  const save = useSaveGraphCredential(wsId);
  const remove = useDeleteGraphCredential(wsId);
  const busy = save.isPending || remove.isPending;
  const clear = () => {
    setEditing(undefined);
    setName("");
    setService("https://git.example.com");
    setToken("");
    setForm(false);
    setActive(undefined);
    setDeleting(undefined);
    save.reset();
    remove.reset();
  };
  const edit = (item?: GraphCredential) => {
    clear();
    setEditing(item);
    setName(item?.name ?? "");
    setService(item?.service_url ?? "https://git.example.com");
    setForm(true);
  };
  if (!allowed) return null;
  return (
    <>
      <Button variant="outline" onClick={() => setOpen(true)}>
        {t(($) => $.code_graph.git_auth.manage)}
      </Button>
      <Dialog
        open={open}
        onOpenChange={(value) => {
          if (!busy) {
            setOpen(value);
            if (!value) clear();
          }
        }}
      >
        <DialogContent
          className="flex h-[min(40rem,85dvh)] min-h-0 flex-col gap-0 overflow-hidden p-0 sm:max-w-3xl"
          aria-describedby={undefined}
        >
          <DialogHeader className="shrink-0 border-b px-6 py-5 pr-12">
            {form && (
              <Button
                variant="ghost"
                size="sm"
                className="mb-2 w-fit px-0 text-muted-foreground"
                disabled={busy}
                onClick={clear}
              >
                {t(($) => $.code_graph.git_auth.back)}
              </Button>
            )}
            <div className="flex items-center justify-between gap-4">
              <DialogTitle>
                {form
                  ? editing
                    ? t(($) => $.code_graph.git_auth.replace)
                    : t(($) => $.code_graph.git_auth.add)
                  : t(($) => $.code_graph.git_auth.manage)}
              </DialogTitle>
              {!form && (
                <Button size="sm" onClick={() => edit()}>
                  {t(($) => $.code_graph.git_auth.add)}
                </Button>
              )}
            </div>
          </DialogHeader>
          <div className="min-h-0 flex-1 overflow-y-auto px-6 py-5">
            {list.data && !list.data.configured && (
              <GitNotice>
                {t(($) => $.code_graph.git_auth.not_configured)}
              </GitNotice>
            )}
            {list.error && (
              <p role="alert" className="text-destructive">
                {list.error.message}
              </p>
            )}
            {!form ? (
              <div className="space-y-4">
                <p className="text-caption text-muted-foreground">
                  {t(($) => $.code_graph.git_auth.ownership)}
                </p>
                {list.isPending ? (
                  <p role="status">{t(($) => $.code_graph.loading)}</p>
                ) : list.data?.items.length === 0 ? (
                  <div className="rounded-lg border border-dashed p-8 text-center text-muted-foreground">
                    {t(($) => $.code_graph.git_auth.empty)}
                  </div>
                ) : (
                  <div className="divide-y rounded-lg border">
                    {list.data?.items.map((item) => (
                      <div key={item.id}>
                        <div className="flex items-center gap-4 px-4 py-4">
                          <div className="min-w-0 flex-1">
                            <p className="break-words font-medium">
                              {item.name}
                            </p>
                            <p className="mt-1 break-all text-caption text-muted-foreground">
                              {item.service_url}
                            </p>
                          </div>
                          <span className="shrink-0 text-caption text-muted-foreground">
                            {t(($) => $.code_graph.git_auth.used, {
                              count: item.repository_count,
                            })}
                          </span>
                          <Button
                            variant="ghost"
                            size="icon-sm"
                            aria-label={
                              t(($) => $.code_graph.actions) + " · " + item.name
                            }
                            aria-expanded={active === item.id}
                            onClick={() => {
                              setActive(
                                active === item.id ? undefined : item.id,
                              );
                              setDeleting(undefined);
                              remove.reset();
                            }}
                          >
                            <MoreHorizontal className="size-4" />
                          </Button>
                        </div>
                        {active === item.id && (
                          <div className="space-y-3 px-4 pb-4">
                            <div className="flex flex-wrap gap-2">
                              <Button
                                size="sm"
                                variant="outline"
                                disabled={busy}
                                onClick={() => edit(item)}
                              >
                                {t(($) => $.code_graph.git_auth.replace)}
                              </Button>
                              <Button
                                size="sm"
                                variant="ghost"
                                disabled={busy}
                                onClick={() => setDeleting(item)}
                              >
                                {t(($) => $.code_graph.git_auth.remove)}
                              </Button>
                            </div>
                            {deleting?.id === item.id && (
                              <>
                                <GitNotice>
                                  {item.repository_count > 0
                                    ? t(
                                        ($) =>
                                          $.code_graph.git_auth.delete_blocked,
                                        { count: item.repository_count },
                                      )
                                    : t(
                                        ($) =>
                                          $.code_graph.git_auth.delete_hint,
                                      )}
                                </GitNotice>
                                {item.repository_count === 0 && (
                                  <Button
                                    size="sm"
                                    variant="destructive"
                                    disabled={busy}
                                    onClick={() =>
                                      remove.mutate(item.id, {
                                        onSuccess: () => {
                                          setDeleting(undefined);
                                          setActive(undefined);
                                        },
                                      })
                                    }
                                  >
                                    {t(($) => $.code_graph.git_auth.remove)}
                                  </Button>
                                )}
                              </>
                            )}
                          </div>
                        )}
                      </div>
                    ))}
                  </div>
                )}
                {remove.error && (
                  <p role="alert" className="text-destructive">
                    {remove.error.message}
                  </p>
                )}
              </div>
            ) : (
              <div className="max-w-xl space-y-5">
                {editing ? (
                  <GitNotice>
                    {t(($) => $.code_graph.git_auth.rotation, {
                      count: editing.repository_count,
                    })}
                  </GitNotice>
                ) : (
                  <p className="text-body text-muted-foreground">
                    {t(($) => $.code_graph.git_auth.token_hint)}
                  </p>
                )}
                <label className="grid gap-2 text-body">
                  {t(($) => $.code_graph.git_auth.name)}
                  <Input
                    value={name}
                    maxLength={120}
                    onChange={(e) => setName(e.target.value)}
                  />
                </label>
                <label className="grid gap-2 text-body">
                  {t(($) => $.code_graph.git_auth.service)}
                  <Input
                    value={service}
                    disabled={!!editing}
                    placeholder="https://git.example.com"
                    onChange={(e) => setService(e.target.value)}
                  />
                </label>
                <label className="grid gap-2 text-body">
                  {t(($) => $.code_graph.git_auth.token_label)}
                  <Input
                    type="password"
                    autoComplete="new-password"
                    value={token}
                    onChange={(e) => setToken(e.target.value)}
                  />
                  <span className="text-caption text-muted-foreground">
                    {t(($) => $.code_graph.git_auth.saved_hint)}
                  </span>
                </label>
                {save.error && (
                  <p role="alert" className="text-destructive">
                    {save.error.message}
                  </p>
                )}
              </div>
            )}
          </div>
          <div className="flex shrink-0 flex-wrap items-center justify-between gap-3 border-t px-6 py-4">
            <p className="text-caption text-muted-foreground">
              {t(($) => $.code_graph.git_auth.secret_hint)}
            </p>
            <div className="flex gap-2">
              {form ? (
                <>
                  <Button variant="outline" disabled={busy} onClick={clear}>
                    {t(($) => $.code_graph.cancel)}
                  </Button>
                  <Button
                    disabled={
                      busy ||
                      !name.trim() ||
                      !token.trim() ||
                      !service.trim() ||
                      !list.data?.configured
                    }
                    onClick={() =>
                      save.mutate(
                        {
                          id: editing?.id,
                          name: name.trim(),
                          service_url: service.trim(),
                          token,
                        },
                        { onSuccess: clear },
                      )
                    }
                  >
                    {t(($) => $.code_graph.git_auth.save)}
                  </Button>
                </>
              ) : (
                <Button variant="outline" onClick={() => setOpen(false)}>
                  {t(($) => $.code_graph.git_auth.done)}
                </Button>
              )}
            </div>
          </div>
        </DialogContent>
      </Dialog>
    </>
  );
}

export function GitAuthFields({
  value,
  onChange,
}: {
  value: GraphAuthInput;
  onChange: (value: GraphAuthInput) => void;
}) {
  const { t } = useT("layout");
  const wsId = useWorkspaceId();
  const allowed = useCanManageGit();
  const list = useQuery(graphCredentialsOptions(wsId, allowed));
  const verify = useVerifyGraphAccess(wsId);
  const resetVerification = verify.reset;
  useEffect(() => {
    resetVerification();
  }, [
    value.repo_url,
    value.branch,
    value.auth_mode,
    value.credential_id,
    resetVerification,
  ]);
  const change = (patch: Partial<GraphAuthInput>) => {
    verify.reset();
    onChange({ ...value, ...patch });
  };
  useEffect(() => {
    if (value.auth_mode !== "token" || value.credential_id) return;
    let matching: GraphCredential[] = [];
    try {
      const repo = new URL(value.repo_url);
      matching = (list.data?.items ?? []).filter((item) => {
        const service = new URL(item.service_url);
        return (
          service.origin === repo.origin &&
          (!service.pathname.replace(/\/$/, "") ||
            repo.pathname.startsWith(service.pathname.replace(/\/$/, "") + "/"))
        );
      });
    } catch {
      /* URL is still being entered. */
    }
    if (matching.length === 1)
      onChange({ ...value, credential_id: matching[0]!.id });
  }, [list.data, value, onChange]);
  return (
    <div className="grid gap-3">
      <GitNotice>{t(($) => $.code_graph.git_auth.auth_hint)}</GitNotice>
      <label className="grid gap-2">
        {t(($) => $.code_graph.git_auth.mode)}
        <select
          className="rounded-md border bg-background p-2"
          value={value.auth_mode}
          onChange={(e) =>
            change({
              auth_mode: e.target.value as "token" | "anonymous",
              credential_id: undefined,
            })
          }
        >
          <option value="token">
            {t(($) => $.code_graph.git_auth.token_mode)}
          </option>
          <option value="anonymous">
            {t(($) => $.code_graph.git_auth.anonymous)}
          </option>
        </select>
      </label>
      {value.auth_mode === "token" && (
        <label className="grid gap-2">
          {t(($) => $.code_graph.git_auth.credential)}
          <select
            className="rounded-md border bg-background p-2 min-w-0"
            value={value.credential_id ?? ""}
            onChange={(e) => change({ credential_id: e.target.value })}
          >
            <option value="">{t(($) => $.code_graph.git_auth.choose)}</option>
            {list.data?.items.map((item) => (
              <option key={item.id} value={item.id}>
                {item.name} · {item.service_url}
              </option>
            ))}
          </select>
        </label>
      )}
      {list.error && <p role="alert">{list.error.message}</p>}
      <GitNotice>{t(($) => $.code_graph.git_auth.verify_hint)}</GitNotice>
      <Button
        variant="outline"
        disabled={
          verify.isPending ||
          !value.repo_url ||
          !value.branch ||
          (value.auth_mode === "token" && !value.credential_id)
        }
        onClick={() => verify.mutate(value)}
      >
        {t(($) => $.code_graph.git_auth.verify)}
      </Button>
      {verify.data && (
        <p role="status">{t(($) => $.code_graph.git_auth.readable)}</p>
      )}
      {verify.error && (
        <p role="alert" className="text-destructive">
          {verify.error.message}
        </p>
      )}
    </div>
  );
}

export function RepositoryGitAuth({ repo }: { repo: CodeGraphRepository }) {
  const { t } = useT("layout");
  const wsId = useWorkspaceId();
  const allowed = useCanManageGit();
  const list = useQuery(graphCredentialsOptions(wsId, allowed));
  const save = useUpdateGraphAuth(wsId);
  const [editing, setEditing] = useState(false);
  const [value, setValue] = useState<GraphAuthInput>({
    auth_mode: repo.auth_mode === "anonymous" ? "anonymous" : "token",
    credential_id: repo.credential_id,
    repo_url: repo.repo_url,
    branch: repo.branch,
  });
  if (!allowed) return null;
  const name = list.data?.items.find(
    (item) => item.id === repo.credential_id,
  )?.name;
  return (
    <section className="space-y-3 border-t pt-4">
      <h3 className="font-semibold">
        {t(($) => $.code_graph.git_auth.authentication)}
      </h3>
      <p className="break-words">
        {repo.auth_mode === "token"
          ? (name ?? t(($) => $.code_graph.git_auth.choose))
          : t(($) => $.code_graph.git_auth.anonymous)}
      </p>
      <p className="text-caption text-muted-foreground">
        {t(($) => $.code_graph.git_auth.sync_hint)}
      </p>
      <Button
        variant="outline"
        onClick={() => {
          if (!editing)
            setValue({
              auth_mode: repo.auth_mode === "anonymous" ? "anonymous" : "token",
              credential_id: repo.credential_id,
              repo_url: repo.repo_url,
              branch: repo.branch,
            });
          setEditing(!editing);
        }}
      >
        {t(($) => $.code_graph.git_auth.change)}
      </Button>
      {editing && (
        <>
          <GitNotice>{t(($) => $.code_graph.git_auth.binding_hint)}</GitNotice>
          <GitAuthFields value={value} onChange={setValue} />
          <Button
            disabled={
              save.isPending ||
              (value.auth_mode === "token" && !value.credential_id)
            }
            onClick={() =>
              save.mutate(
                { ...value, id: repo.id },
                { onSuccess: () => setEditing(false) },
              )
            }
          >
            {t(($) => $.code_graph.git_auth.save)}
          </Button>
          {save.error && (
            <p role="alert" className="text-destructive">
              {save.error.message}
            </p>
          )}
        </>
      )}
    </section>
  );
}
