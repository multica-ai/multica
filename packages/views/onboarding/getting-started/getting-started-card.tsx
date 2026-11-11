"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Check, X } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useAuthStore } from "@multica/core/auth";
import { autopilotListOptions } from "@multica/core/autopilots/queries";
import { githubInstallationsOptions } from "@multica/core/github";
import {
  agentAssignedIssueCountOptions,
  useGettingStartedStore,
} from "@multica/core/onboarding";
import { useCurrentWorkspace, useWorkspacePaths } from "@multica/core/paths";
import {
  agentListOptions,
  invitationListOptions,
  memberListOptions,
} from "@multica/core/workspace/queries";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";
import { InviteTeammatesDialog } from "./invite-teammates-dialog";

type ItemId = "agent" | "task" | "invite" | "github" | "autopilot";

interface ChecklistItem {
  id: ItemId;
  done: boolean;
  title: string;
  body: string;
  action: string;
  href?: string;
}

/**
 * What to do after onboarding, at the foot of the Work panel. Only workspaces
 * this device onboarded into get one, and dismissing it is final — the
 * checklist is a nudge, not a gate. The first unfinished item is the one
 * expanded, so there is always exactly one obvious next thing.
 */
export function GettingStartedCard() {
  const workspace = useCurrentWorkspace();
  const state = useGettingStartedStore((s) =>
    workspace ? s.workspaces[workspace.id] : undefined,
  );
  if (!workspace || state !== "active") return null;
  return <Checklist wsId={workspace.id} />;
}

function Checklist({ wsId }: { wsId: string }) {
  const { t } = useT("onboarding");
  const p = useWorkspacePaths();
  const workspace = useCurrentWorkspace()!;
  const userId = useAuthStore((s) => s.user?.id);
  const dismiss = useGettingStartedStore((s) => s.dismiss);
  const [inviteOpen, setInviteOpen] = useState(false);

  const agents = useQuery(agentListOptions(wsId)).data;
  const assigned = useQuery(agentAssignedIssueCountOptions(wsId)).data;
  const members = useQuery(memberListOptions(wsId)).data;
  const me = members?.find((m) => m.user_id === userId);
  const canManage = me?.role === "owner" || me?.role === "admin";
  const invitations = useQuery({ ...invitationListOptions(wsId), enabled: canManage }).data;
  const github = useQuery(githubInstallationsOptions(wsId)).data;
  const autopilots = useQuery(autopilotListOptions(wsId)).data;

  const myAgents = (agents ?? []).filter((a) => !a.archived_at && !a.system_key);
  const alone = (members?.length ?? 0) <= 1;

  const items: ChecklistItem[] = [
    {
      id: "agent",
      done: myAgents.length > 0,
      title: myAgents[0]
        ? t(($) => $.getting_started.agent_done, { name: myAgents[0].name })
        : t(($) => $.getting_started.agent_title),
      body: t(($) => $.getting_started.agent_body),
      action: t(($) => $.getting_started.agent_action),
      href: p.newAgent(),
    },
    {
      id: "task",
      done: (assigned ?? 0) > 0,
      title: t(($) => $.getting_started.task_title),
      body: t(($) => $.getting_started.task_body),
      action: t(($) => $.getting_started.task_action),
      href: p.issues(),
    },
    ...(canManage
      ? [
          {
            id: "invite" as const,
            done: !alone || (invitations?.length ?? 0) > 0,
            title: t(($) => $.getting_started.invite_title),
            body: t(($) => $.getting_started.invite_body),
            action: t(($) => $.getting_started.invite_action),
          },
        ]
      : []),
    {
      id: "github",
      done: (github?.installations.length ?? 0) > 0,
      title: t(($) => $.getting_started.github_title),
      body: t(($) => $.getting_started.github_body),
      action: t(($) => $.getting_started.github_action),
      href: `${p.settings()}?tab=code`,
    },
    {
      id: "autopilot",
      done: (autopilots?.length ?? 0) > 0,
      title: t(($) => $.getting_started.autopilot_title),
      body: t(($) => $.getting_started.autopilot_body),
      action: t(($) => $.getting_started.autopilot_action),
      href: `${p.autopilots()}?create=1`,
    },
  ];

  const doneCount = items.filter((item) => item.done).length;
  const next = items.find((item) => !item.done);

  return (
    <section
      aria-label={t(($) => $.getting_started.title)}
      className="mx-2 mb-2 shrink-0 rounded-lg border bg-card p-3"
    >
      <div className="flex items-center gap-2">
        <span className="flex-1 text-label font-medium text-foreground">
          {t(($) => $.getting_started.title)}
        </span>
        <span className="text-caption text-muted-foreground">
          {t(($) => $.getting_started.count, { done: doneCount, total: items.length })}
        </span>
        <Button
          variant="ghost"
          size="icon-xs"
          className="-mr-1 text-muted-foreground"
          onClick={() => dismiss(wsId)}
          aria-label={t(($) => $.getting_started.dismiss)}
        >
          <X />
        </Button>
      </div>
      <div className="mt-2 h-1 overflow-hidden rounded-full bg-muted">
        <div
          className="h-full rounded-full bg-foreground transition-[width]"
          style={{ width: `${(doneCount / items.length) * 100}%` }}
        />
      </div>

      {next ? (
        <ul className="mt-2 flex flex-col">
          {items.map((item) => (
            <li key={item.id}>
              {item === next ? (
                <div className="flex gap-2 rounded-md bg-muted/60 px-2 py-2">
                  <Mark done={false} />
                  <div className="min-w-0 flex-1">
                    <div className="text-label font-medium text-foreground">{item.title}</div>
                    <p className="mt-0.5 text-caption text-muted-foreground">{item.body}</p>
                    {item.href ? (
                      <Button
                        variant="outline"
                        size="xs"
                        className="mt-2"
                        render={<AppLink href={item.href} />}
                        nativeButton={false}
                      >
                        {item.action}
                      </Button>
                    ) : (
                      <Button
                        variant="outline"
                        size="xs"
                        className="mt-2"
                        onClick={() => setInviteOpen(true)}
                      >
                        {item.action}
                      </Button>
                    )}
                  </div>
                </div>
              ) : (
                <ItemRow item={item} onInvite={() => setInviteOpen(true)} />
              )}
            </li>
          ))}
        </ul>
      ) : (
        <div className="mt-2 flex items-center justify-between gap-2">
          <p className="text-caption text-muted-foreground">{t(($) => $.getting_started.all_done)}</p>
          <Button variant="outline" size="xs" onClick={() => dismiss(wsId)}>
            {t(($) => $.getting_started.close)}
          </Button>
        </div>
      )}

      {canManage ? (
        <InviteTeammatesDialog
          workspace={workspace}
          offerRename={alone}
          open={inviteOpen}
          onOpenChange={setInviteOpen}
        />
      ) : null}
    </section>
  );
}

function ItemRow({ item, onInvite }: { item: ChecklistItem; onInvite: () => void }) {
  const className = cn(
    "flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-caption transition-colors hover:bg-muted/60",
    item.done ? "text-muted-foreground" : "text-foreground",
  );
  const content = (
    <>
      <Mark done={item.done} />
      <span className={cn("min-w-0 flex-1 truncate", item.done && "line-through")}>
        {item.title}
      </span>
    </>
  );
  if (item.done) {
    return <div className={cn(className, "hover:bg-transparent")}>{content}</div>;
  }
  return item.href ? (
    <AppLink href={item.href} className={className}>
      {content}
    </AppLink>
  ) : (
    <button type="button" onClick={onInvite} className={className}>
      {content}
    </button>
  );
}

function Mark({ done }: { done: boolean }) {
  return (
    <span
      aria-hidden
      className={cn(
        "mt-px flex size-4 shrink-0 items-center justify-center rounded-full border",
        done ? "border-foreground bg-foreground text-background" : "border-border",
      )}
    >
      {done ? <Check className="size-2.5" /> : null}
    </span>
  );
}
