"use client";

import { useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AuthorityClientError,
  authorityKeys,
  tagAuthorityClient,
  waitForAuthorityWorkspace,
  type AuthorityInvitation,
  type AuthorityMember,
  type AuthorityRole,
  type AuthorityWorkspace,
  type TagAuthorityClient,
} from "@multica/core/tag-authority";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Input } from "@multica/ui/components/ui/input";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import {
  Check,
  Clock,
  Copy,
  Crown,
  Link,
  Mail,
  Shield,
  User,
  UserMinus,
  Users,
  X,
} from "lucide-react";
import { nameToWorkspaceSlug, WORKSPACE_SLUG_REGEX } from "../workspace/slug";
import { useT } from "../i18n/use-t";

const ROLE_ICONS = { owner: Crown, admin: Shield, member: User } as const;

function randomCommandKey(prefix: string) {
  return `${prefix}:${crypto.randomUUID()}`;
}

async function copyText(value: string) {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(value);
      return true;
    }
    const textarea = document.createElement("textarea");
    textarea.value = value;
    textarea.setAttribute("readonly", "");
    textarea.style.position = "fixed";
    textarea.style.opacity = "0";
    document.body.appendChild(textarea);
    textarea.select();
    const copied = document.execCommand("copy");
    textarea.remove();
    return copied;
  } catch {
    return false;
  }
}

function PageShell({
  title,
  description,
  children,
}: {
  title: string;
  description: string;
  children: React.ReactNode;
}) {
  return (
    <main className="min-h-0 flex-1 overflow-y-auto bg-background px-4 py-6 text-foreground sm:px-6 lg:px-8">
      <div className="mx-auto flex w-full max-w-4xl flex-col gap-6">
        <header className="space-y-1">
          <h1 className="text-title-lg font-semibold">{title}</h1>
          <p className="text-body text-muted-foreground">{description}</p>
        </header>
        {children}
      </div>
    </main>
  );
}

export function AuthorityCreateWorkspacePage({
  client = tagAuthorityClient,
  onReady,
}: {
  client?: TagAuthorityClient;
  onReady: (workspace: AuthorityWorkspace) => void;
}) {
  const { t } = useT("tag-authority");
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [slugEdited, setSlugEdited] = useState(false);
  const [state, setState] = useState<
    "idle" | "creating" | "provisioning" | "error"
  >("idle");
  const [error, setError] = useState("");
  const idempotencyKey = useRef(randomCommandKey("workspace-create"));

  const updateName = (value: string) => {
    setName(value);
    if (!slugEdited) setSlug(nameToWorkspaceSlug(value));
  };

  const create = async () => {
    if (!name.trim() || !WORKSPACE_SLUG_REGEX.test(slug)) return;
    setState("creating");
    setError("");
    try {
      const created = await client.createWorkspace({
        name: name.trim(),
        slug,
        idempotencyKey: idempotencyKey.current,
      });
      setState("provisioning");
      const ready = await waitForAuthorityWorkspace(
        client,
        created.workspaceId,
      );
      onReady(ready);
    } catch (caught) {
      const code =
        caught instanceof AuthorityClientError ? caught.code : "request_failed";
      setError(
        code === "projection_pending"
          ? t(($) => $.create.errors.projection_pending)
          : code === "idempotency_conflict"
            ? t(($) => $.create.errors.idempotency_conflict)
            : code === "invalid_input"
              ? t(($) => $.create.errors.invalid_input)
              : t(($) => $.create.errors.fallback),
      );
      setState("error");
    }
  };

  const busy = state === "creating" || state === "provisioning";

  return (
    <PageShell
      title={t(($) => $.create.title)}
      description={t(($) => $.create.description)}
    >
      <Card className="w-full max-w-xl self-center">
        <CardContent className="space-y-5 py-6">
          <label className="block space-y-2 text-label font-medium">
            <span>{t(($) => $.create.name)}</span>
            <Input
              aria-label={t(($) => $.create.name)}
              value={name}
              disabled={busy}
              onChange={(event) => updateName(event.target.value)}
              autoComplete="organization"
            />
          </label>
          <label className="block space-y-2 text-label font-medium">
            <span>{t(($) => $.create.url)}</span>
            <Input
              aria-label={t(($) => $.create.url)}
              value={slug}
              disabled={busy}
              spellCheck={false}
              onChange={(event) => {
                setSlugEdited(true);
                setSlug(event.target.value.toLowerCase());
              }}
            />
            <span className="block text-caption font-normal text-muted-foreground">
              {t(($) => $.create.url_preview, {
                slug: slug || t(($) => $.create.url_fallback),
              })}
            </span>
          </label>

          {busy && (
            <div
              role="status"
              className="flex items-center gap-2 text-body text-muted-foreground"
            >
              <span
                aria-hidden="true"
                className="size-4 animate-spin rounded-full border-2 border-border border-t-foreground motion-reduce:animate-none"
              />
              {state === "creating"
                ? t(($) => $.create.creating)
                : t(($) => $.create.provisioning)}
            </div>
          )}
          {error && (
            <p role="alert" className="text-body text-destructive">
              {error}
            </p>
          )}

          <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
            {state === "error" && (
              <Button
                variant="outline"
                className="min-h-11"
                onClick={() => void create()}
              >
                {t(($) => $.create.retry)}
              </Button>
            )}
            <Button
              className="min-h-11"
              disabled={
                busy || !name.trim() || !WORKSPACE_SLUG_REGEX.test(slug)
              }
              onClick={() => void create()}
            >
              {t(($) => $.create.submit)}
            </Button>
          </div>
        </CardContent>
      </Card>
    </PageShell>
  );
}

function canManageRole(workspace: AuthorityWorkspace, role: AuthorityRole) {
  if (role === "owner") return workspace.capabilities.manageOwners;
  if (role === "admin") return workspace.capabilities.manageAdmins;
  return workspace.capabilities.manageMembers;
}

function RoleBadge({ role, label }: { role: AuthorityRole; label: string }) {
  const Icon = ROLE_ICONS[role];
  return (
    <Badge variant="secondary">
      <Icon className="size-3" />
      {label}
    </Badge>
  );
}

export function AuthorityMembersPage({
  workspace,
  currentUserId,
  buildJoinLinkUrl,
  client = tagAuthorityClient,
}: {
  workspace: AuthorityWorkspace;
  currentUserId: string;
  buildJoinLinkUrl: (token: string) => string;
  client?: TagAuthorityClient;
}) {
  const { t } = useT("tag-authority");
  const queryClient = useQueryClient();
  const [inviteEmail, setInviteEmail] = useState("");
  const [inviteRole, setInviteRole] = useState<"admin" | "member">("member");
  const [busy, setBusy] = useState<string | null>(null);
  const [message, setMessage] = useState("");
  const [confirmRemove, setConfirmRemove] = useState<AuthorityMember | null>(
    null,
  );
  const [createdLink, setCreatedLink] = useState<{
    id: string;
    token: string;
    expiresAt: Date;
  } | null>(null);

  const membersQuery = useQuery({
    queryKey: authorityKeys.members(workspace.id),
    queryFn: () => client.listMembers(workspace.id),
  });
  const invitationsQuery = useQuery({
    queryKey: authorityKeys.invitations(workspace.id),
    queryFn: () => client.listInvitations(workspace.id),
    enabled: workspace.capabilities.inviteMembers,
  });
  const members = membersQuery.data ?? [];
  const invitations = workspace.capabilities.inviteMembers
    ? (invitationsQuery.data ?? [])
    : [];
  const ownerCount = members.filter((member) => member.role === "owner").length;
  const roleLabel = (role: AuthorityRole) => {
    if (role === "owner") return t(($) => $.roles.owner);
    if (role === "admin") return t(($) => $.roles.admin);
    return t(($) => $.roles.member);
  };
  const errorCopy = (caught: unknown, fallback: string) =>
    authorityErrorCopy(caught, fallback, {
      unauthorized: t(($) => $.errors.unauthorized),
      email_not_verified: t(($) => $.errors.email_not_verified),
      wrong_account: t(($) => $.errors.wrong_account),
      expired: t(($) => $.errors.expired),
      not_pending: t(($) => $.errors.not_pending),
      not_active: t(($) => $.errors.not_active),
      invalid_token: t(($) => $.errors.invalid_token),
      last_owner: t(($) => $.errors.last_owner),
      stale_authority_version: t(($) => $.errors.stale_authority_version),
      restriction_sync_pending: t(($) => $.errors.restriction_sync_pending),
      projection_pending: t(($) => $.errors.projection_pending),
    });

  const refreshMembers = async () => {
    await Promise.all([
      queryClient.invalidateQueries({
        queryKey: authorityKeys.members(workspace.id),
      }),
      queryClient.invalidateQueries({ queryKey: authorityKeys.workspaces() }),
    ]);
  };
  const refreshInvitations = async () => {
    await queryClient.invalidateQueries({
      queryKey: authorityKeys.invitations(workspace.id),
    });
  };

  const issueInvite = async (targetEmail: string, role: "admin" | "member") => {
    setBusy(`invite:${targetEmail}`);
    setMessage("");
    try {
      await client.issueInvitation(workspace.id, { targetEmail, role });
      setInviteEmail("");
      setMessage(t(($) => $.members.messages.invite_pending));
      await refreshInvitations();
    } catch (caught) {
      setMessage(
        errorCopy(
          caught,
          t(($) => $.members.messages.invite_update_error),
        ),
      );
    } finally {
      setBusy(null);
    }
  };

  const invitationAction = async (
    invitation: AuthorityInvitation,
    action: "resend" | "revoke",
  ) => {
    setBusy(`${action}:${invitation.id}`);
    setMessage("");
    try {
      await client.actOnInvitation(invitation.id, action);
      setMessage(
        action === "resend"
          ? t(($) => $.members.messages.resend)
          : t(($) => $.members.messages.invite_revoked),
      );
      await refreshInvitations();
    } catch (caught) {
      setMessage(
        errorCopy(
          caught,
          t(($) => $.members.messages.invite_action_error),
        ),
      );
    } finally {
      setBusy(null);
    }
  };

  const changeMember = async (
    member: AuthorityMember,
    role: AuthorityRole,
    status: "active" | "removed",
  ) => {
    setBusy(`member:${member.userId}`);
    setMessage("");
    try {
      await client.changeMember(workspace.id, {
        targetUserId: member.userId,
        role,
        status,
        expectedAuthorityVersion: workspace.authorityVersion,
        idempotencyKey: randomCommandKey("membership-change"),
      });
      setMessage(
        status === "removed"
          ? t(($) => $.members.sync_pending)
          : t(($) => $.members.role_sync_pending),
      );
      await refreshMembers();
    } catch (caught) {
      if (
        caught instanceof AuthorityClientError &&
        caught.code === "restriction_sync_pending"
      ) {
        setMessage(
          status === "removed"
            ? t(($) => $.members.sync_pending)
            : t(($) => $.members.role_sync_pending),
        );
        await refreshMembers();
      } else {
        setMessage(
          errorCopy(
            caught,
            t(($) => $.members.messages.member_action_error),
          ),
        );
      }
    } finally {
      setBusy(null);
      setConfirmRemove(null);
    }
  };

  const createJoinLink = async () => {
    setBusy("join-link:create");
    setMessage("");
    try {
      const expiresAt = new Date(Date.now() + 7 * 24 * 60 * 60 * 1000);
      const result = await client.createJoinLink(workspace.id, {
        maxClaims: 10,
        expiresAt: expiresAt.toISOString(),
      });
      setCreatedLink({
        id: result.joinLink.id,
        token: result.token,
        expiresAt: result.joinLink.expiresAt,
      });
    } catch (caught) {
      setMessage(
        errorCopy(
          caught,
          t(($) => $.members.messages.join_create_error),
        ),
      );
    } finally {
      setBusy(null);
    }
  };

  const revokeJoinLink = async () => {
    if (!createdLink) return;
    setBusy("join-link:revoke");
    try {
      await client.revokeJoinLink(createdLink.id);
      setCreatedLink(null);
      setMessage(t(($) => $.members.messages.join_revoked));
    } catch (caught) {
      setMessage(
        errorCopy(
          caught,
          t(($) => $.members.messages.join_revoke_error),
        ),
      );
    } finally {
      setBusy(null);
    }
  };

  const joinUrl = createdLink ? buildJoinLinkUrl(createdLink.token) : "";

  return (
    <PageShell
      title={t(($) => $.members.title)}
      description={t(($) => $.members.description)}
    >
      {message && (
        <p
          role="status"
          aria-live="polite"
          className="rounded-md bg-muted px-4 py-3 text-body text-muted-foreground"
        >
          {message}
        </p>
      )}

      {workspace.capabilities.inviteMembers && (
        <section aria-labelledby="invite-member-title" className="space-y-3">
          <h2 id="invite-member-title" className="text-title font-semibold">
            {t(($) => $.members.invite_title)}
          </h2>
          <Card>
            <CardContent className="grid gap-3 py-5 sm:grid-cols-[minmax(0,1fr)_8rem_auto]">
              <Input
                aria-label={t(($) => $.members.invite_email)}
                type="email"
                value={inviteEmail}
                placeholder="person@example.com"
                onChange={(event) => setInviteEmail(event.target.value)}
              />
              <select
                aria-label={t(($) => $.members.invite_role)}
                className="min-h-11 rounded-md border border-input bg-background px-3 text-body"
                value={inviteRole}
                onChange={(event) =>
                  setInviteRole(event.target.value as "admin" | "member")
                }
              >
                <option value="member">{roleLabel("member")}</option>
                {workspace.capabilities.manageAdmins && (
                  <option value="admin">{roleLabel("admin")}</option>
                )}
              </select>
              <Button
                className="min-h-11"
                disabled={!inviteEmail.trim() || busy !== null}
                onClick={() => void issueInvite(inviteEmail.trim(), inviteRole)}
              >
                {t(($) => $.members.invite_submit)}
              </Button>
            </CardContent>
          </Card>
        </section>
      )}

      <section aria-labelledby="member-list-title" className="space-y-3">
        <h2 id="member-list-title" className="text-title font-semibold">
          {members.length > 0
            ? t(($) => $.members.list_count, { count: members.length })
            : t(($) => $.members.list_empty)}
        </h2>
        {membersQuery.isLoading ? (
          <p role="status" className="text-body text-muted-foreground">
            {t(($) => $.members.loading)}
          </p>
        ) : membersQuery.isError ? (
          <p role="alert" className="text-body text-destructive">
            {t(($) => $.members.load_error)}
          </p>
        ) : (
          <Card>
            <CardContent className="divide-y divide-border p-0">
              {members.map((member) => {
                const self = member.userId === currentUserId;
                const lastOwner = member.role === "owner" && ownerCount <= 1;
                const manageable = canManageRole(workspace, member.role);
                const allowRemove = self
                  ? workspace.capabilities.leaveWorkspace && !lastOwner
                  : manageable && !lastOwner;
                return (
                  <div
                    key={member.userId}
                    role="group"
                    aria-label={t(($) => $.members.member_label, {
                      name: member.name,
                    })}
                    className="flex min-h-16 flex-wrap items-center gap-3 px-4 py-3"
                  >
                    <div className="grid size-9 shrink-0 place-items-center rounded-full bg-muted text-label font-semibold">
                      {member.name.slice(0, 1).toUpperCase()}
                    </div>
                    <div className="min-w-0 flex-1">
                      <div className="truncate text-body font-medium">
                        <span>{member.name}</span>
                        {self ? ` (${t(($) => $.members.you)})` : ""}
                      </div>
                      <div className="text-caption text-muted-foreground">
                        {t(($) => $.members.generation, {
                          generation: member.membershipGeneration,
                        })}
                      </div>
                    </div>
                    {manageable && !self && (
                      <div className="flex flex-wrap gap-1">
                        {(["owner", "admin", "member"] as const).map((role) =>
                          role !== member.role &&
                          canManageRole(workspace, role) ? (
                            <Button
                              key={role}
                              variant="ghost"
                              size="sm"
                              disabled={
                                busy !== null || (lastOwner && role !== "owner")
                              }
                              aria-label={t(($) => $.members.change_role, {
                                name: member.name,
                                role: roleLabel(role),
                              })}
                              onClick={() =>
                                void changeMember(member, role, "active")
                              }
                            >
                              {roleLabel(role)}
                            </Button>
                          ) : null,
                        )}
                      </div>
                    )}
                    {allowRemove && (
                      <Button
                        variant="ghost"
                        size="sm"
                        disabled={busy !== null}
                        aria-label={
                          self
                            ? t(($) => $.members.leave_workspace)
                            : t(($) => $.members.remove_member, {
                                name: member.name,
                              })
                        }
                        onClick={() => setConfirmRemove(member)}
                      >
                        <UserMinus className="size-4" />
                        {self
                          ? t(($) => $.members.leave)
                          : t(($) => $.members.remove)}
                      </Button>
                    )}
                    <RoleBadge
                      role={member.role}
                      label={roleLabel(member.role)}
                    />
                  </div>
                );
              })}
            </CardContent>
          </Card>
        )}
      </section>

      {workspace.capabilities.inviteMembers && invitations.length > 0 && (
        <section
          aria-labelledby="pending-invitations-title"
          className="space-y-3"
        >
          <h2
            id="pending-invitations-title"
            className="text-title font-semibold"
          >
            {t(($) => $.members.pending_count, { count: invitations.length })}
          </h2>
          <Card>
            <CardContent className="divide-y divide-border p-0">
              {invitations.map((invitation) => {
                const manageable = canManageRole(workspace, invitation.role);
                return (
                  <div
                    key={invitation.id}
                    role="group"
                    aria-label={invitation.targetEmail}
                    className="flex min-h-16 flex-wrap items-center gap-3 px-4 py-3"
                  >
                    <div className="grid size-9 place-items-center rounded-full bg-muted">
                      <Mail className="size-4 text-muted-foreground" />
                    </div>
                    <div className="min-w-0 flex-1">
                      <div className="truncate text-body font-medium">
                        {invitation.targetEmail}
                      </div>
                      <div className="flex items-center gap-1 text-caption text-muted-foreground">
                        <Clock className="size-3" />
                        {t(($) => $.members.expires, {
                          date: invitation.expiresAt.toLocaleDateString(),
                        })}
                      </div>
                    </div>
                    {manageable && (
                      <Button
                        variant="ghost"
                        size="sm"
                        disabled={busy !== null}
                        aria-label={t(($) => $.members.resend_label)}
                        onClick={() =>
                          void invitationAction(invitation, "resend")
                        }
                      >
                        {t(($) => $.members.resend)}
                      </Button>
                    )}
                    {manageable && workspace.capabilities.manageAdmins && (
                      <Button
                        variant="ghost"
                        size="sm"
                        disabled={busy !== null}
                        aria-label={t(($) => $.members.change_invite_role, {
                          role: roleLabel(
                            invitation.role === "member" ? "admin" : "member",
                          ),
                        })}
                        onClick={() =>
                          void issueInvite(
                            invitation.targetEmail,
                            invitation.role === "member" ? "admin" : "member",
                          )
                        }
                      >
                        {invitation.role === "member"
                          ? t(($) => $.members.make_admin)
                          : t(($) => $.members.make_member)}
                      </Button>
                    )}
                    {manageable && (
                      <Button
                        variant="ghost"
                        size="sm"
                        disabled={busy !== null}
                        aria-label={t(($) => $.members.revoke_invite)}
                        onClick={() =>
                          void invitationAction(invitation, "revoke")
                        }
                      >
                        <X className="size-4" />
                        {t(($) => $.members.revoke)}
                      </Button>
                    )}
                    <RoleBadge
                      role={invitation.role}
                      label={roleLabel(invitation.role)}
                    />
                  </div>
                );
              })}
            </CardContent>
          </Card>
        </section>
      )}

      {workspace.capabilities.inviteMembers && (
        <section aria-labelledby="join-link-title" className="space-y-3">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div>
              <h2 id="join-link-title" className="text-title font-semibold">
                {t(($) => $.members.join_link)}
              </h2>
              <p className="text-body text-muted-foreground">
                {t(($) => $.members.join_link_rules)}
              </p>
            </div>
            <Button
              className="min-h-11"
              disabled={busy !== null || createdLink !== null}
              onClick={() => void createJoinLink()}
            >
              <Link className="size-4" />
              {t(($) => $.members.create_join_link)}
            </Button>
          </div>
          {createdLink && (
            <Card
              role="group"
              aria-label={t(($) => $.members.active_join_link)}
            >
              <CardContent className="flex flex-wrap items-center gap-3 py-4">
                <div className="min-w-0 flex-1">
                  <div className="text-body font-medium">
                    {t(($) => $.members.member_only)}
                  </div>
                  <div className="truncate font-mono text-caption text-muted-foreground">
                    {joinUrl}
                  </div>
                </div>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() =>
                    void copyText(joinUrl).then((copied) => {
                      setMessage(
                        copied
                          ? t(($) => $.members.copy_success)
                          : t(($) => $.members.copy_error),
                      );
                    })
                  }
                >
                  <Copy className="size-4" />
                  {t(($) => $.members.copy)}
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  aria-label={t(($) => $.members.revoke_join_link)}
                  disabled={busy !== null}
                  onClick={() => void revokeJoinLink()}
                >
                  {t(($) => $.members.revoke)}
                </Button>
              </CardContent>
            </Card>
          )}
        </section>
      )}

      <AlertDialog
        open={confirmRemove !== null}
        onOpenChange={(open) => !open && setConfirmRemove(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {confirmRemove?.userId === currentUserId
                ? t(($) => $.members.leave_title)
                : t(($) => $.members.remove_title, {
                    name: confirmRemove?.name ?? roleLabel("member"),
                  })}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.members.remove_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.members.cancel)}</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              aria-label={t(($) => $.members.confirm_remove)}
              disabled={busy !== null}
              onClick={() =>
                confirmRemove
                  ? void changeMember(
                      confirmRemove,
                      confirmRemove.role,
                      "removed",
                    )
                  : undefined
              }
            >
              {t(($) => $.members.confirm_remove)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </PageShell>
  );
}

type AuthorityErrorMessages = Record<
  | "unauthorized"
  | "email_not_verified"
  | "wrong_account"
  | "expired"
  | "not_pending"
  | "not_active"
  | "invalid_token"
  | "last_owner"
  | "stale_authority_version"
  | "restriction_sync_pending"
  | "projection_pending",
  string
>;

function authorityErrorCopy(
  caught: unknown,
  fallback: string,
  messages: AuthorityErrorMessages,
) {
  if (!(caught instanceof AuthorityClientError)) return fallback;
  switch (caught.code) {
    case "unauthorized":
      return messages.unauthorized;
    case "email_not_verified":
      return messages.email_not_verified;
    case "wrong_account":
      return messages.wrong_account;
    case "expired":
      return messages.expired;
    case "not_pending":
      return messages.not_pending;
    case "not_active":
      return messages.not_active;
    case "invalid_token":
      return messages.invalid_token;
    case "last_owner":
      return messages.last_owner;
    case "stale_authority_version":
      return messages.stale_authority_version;
    case "restriction_sync_pending":
      return messages.restriction_sync_pending;
    case "projection_pending":
      return messages.projection_pending;
    default:
      return fallback;
  }
}

function useTranslatedAuthorityErrors(): AuthorityErrorMessages {
  const { t } = useT("tag-authority");
  return {
    unauthorized: t(($) => $.errors.unauthorized),
    email_not_verified: t(($) => $.errors.email_not_verified),
    wrong_account: t(($) => $.errors.wrong_account),
    expired: t(($) => $.errors.expired),
    not_pending: t(($) => $.errors.not_pending),
    not_active: t(($) => $.errors.not_active),
    invalid_token: t(($) => $.errors.invalid_token),
    last_owner: t(($) => $.errors.last_owner),
    stale_authority_version: t(($) => $.errors.stale_authority_version),
    restriction_sync_pending: t(($) => $.errors.restriction_sync_pending),
    projection_pending: t(($) => $.errors.projection_pending),
  };
}

function AcceptanceShell({
  icon,
  title,
  description,
  error,
  busy,
  checkingLabel,
  children,
}: {
  icon: React.ReactNode;
  title: string;
  description: string;
  error: string;
  busy: boolean;
  checkingLabel: string;
  children: React.ReactNode;
}) {
  return (
    <main className="grid min-h-svh place-items-center bg-background px-4 py-12 text-foreground">
      <Card className="w-full max-w-md">
        <CardContent className="flex flex-col items-center gap-5 py-8 text-center sm:py-12">
          <div className="grid size-14 place-items-center rounded-full bg-muted">
            {icon}
          </div>
          <div className="space-y-2">
            <h1 className="text-title-lg font-semibold">{title}</h1>
            <p className="text-body text-muted-foreground">{description}</p>
          </div>
          {busy && (
            <p role="status" className="text-body text-muted-foreground">
              {checkingLabel}
            </p>
          )}
          {error && (
            <p role="alert" className="text-body text-destructive">
              {error}
            </p>
          )}
          {children}
        </CardContent>
      </Card>
    </main>
  );
}

async function readyAfterGrant(
  client: TagAuthorityClient,
  workspaceId: string | undefined,
) {
  if (!workspaceId) throw new AuthorityClientError("invalid_response", 200);
  return await waitForAuthorityWorkspace(client, workspaceId);
}

export function AuthorityInvitePage({
  token,
  client = tagAuthorityClient,
  onReady,
}: {
  token: string;
  client?: TagAuthorityClient;
  onReady: (workspace: AuthorityWorkspace) => void;
}) {
  const { t } = useT("tag-authority");
  const translatedErrors = useTranslatedAuthorityErrors();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [declined, setDeclined] = useState(false);

  const accept = async () => {
    setBusy(true);
    setError("");
    try {
      const accepted = await client.acceptInvitation(token);
      onReady(await readyAfterGrant(client, accepted.workspaceId));
    } catch (caught) {
      setError(
        authorityErrorCopy(
          caught,
          t(($) => $.accept.invite_accept_error),
          translatedErrors,
        ),
      );
    } finally {
      setBusy(false);
    }
  };

  const decline = async () => {
    setBusy(true);
    setError("");
    try {
      await client.declineInvitation(token);
      setDeclined(true);
    } catch (caught) {
      setError(
        authorityErrorCopy(
          caught,
          t(($) => $.accept.invite_decline_error),
          translatedErrors,
        ),
      );
    } finally {
      setBusy(false);
    }
  };

  return (
    <AcceptanceShell
      icon={
        declined ? <Check className="size-6" /> : <Users className="size-6" />
      }
      title={
        declined
          ? t(($) => $.accept.invite_declined)
          : t(($) => $.accept.join_title)
      }
      description={t(($) => $.accept.invite_description)}
      busy={busy}
      error={error}
      checkingLabel={t(($) => $.accept.checking)}
    >
      {!declined && (
        <div className="flex w-full flex-col-reverse gap-2 sm:flex-row">
          <Button
            variant="outline"
            className="min-h-11 flex-1"
            disabled={busy || !token}
            onClick={() => void decline()}
          >
            {t(($) => $.accept.decline)}
          </Button>
          <Button
            className="min-h-11 flex-1"
            disabled={busy || !token}
            onClick={() => void accept()}
          >
            {t(($) => $.accept.accept)}
          </Button>
        </div>
      )}
      <a
        className="inline-flex min-h-11 items-center text-body text-muted-foreground hover:text-foreground"
        href="/"
      >
        {t(($) => $.accept.return)}
      </a>
    </AcceptanceShell>
  );
}

export function AuthorityJoinPage({
  token,
  client = tagAuthorityClient,
  onReady,
}: {
  token: string;
  client?: TagAuthorityClient;
  onReady: (workspace: AuthorityWorkspace) => void;
}) {
  const { t } = useT("tag-authority");
  const translatedErrors = useTranslatedAuthorityErrors();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const claim = async () => {
    setBusy(true);
    setError("");
    try {
      const granted = await client.claimJoinLink(token);
      onReady(await readyAfterGrant(client, granted.workspaceId));
    } catch (caught) {
      setError(
        authorityErrorCopy(
          caught,
          t(($) => $.accept.join_error),
          translatedErrors,
        ),
      );
    } finally {
      setBusy(false);
    }
  };

  return (
    <AcceptanceShell
      icon={<Link className="size-6" />}
      title={t(($) => $.accept.join_title)}
      description={t(($) => $.accept.join_description)}
      busy={busy}
      error={error}
      checkingLabel={t(($) => $.accept.checking)}
    >
      <Button
        className="min-h-11 w-full"
        disabled={busy || !token}
        onClick={() => void claim()}
      >
        {t(($) => $.accept.join)}
      </Button>
      <a
        className="inline-flex min-h-11 items-center text-body text-muted-foreground hover:text-foreground"
        href="/"
      >
        {t(($) => $.accept.return)}
      </a>
    </AcceptanceShell>
  );
}
