"use client";

import type { ReactNode } from "react";
import { ArrowRight, AtSign, CircleCheck, ListPlus, Users } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useModalStore } from "@multica/core/modals";
import { useWorkspacePaths } from "@multica/core/paths";
import { runtimeDisplayLabel } from "@multica/core/runtimes";
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import {
  agentDetailOptions,
  squadListOptions,
} from "@multica/core/workspace/queries";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { cn } from "@multica/ui/lib/utils";
import { ActorAvatar } from "../../common/actor-avatar";
import { AppLink, useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { AgentCreateShell } from "./create-shell";

/**
 * Step 3 of agent creation. The agent exists; this screen only points at its
 * first uses, so leaving it at any point loses nothing.
 */
export function AgentCreatedPage({ agentId }: { agentId: string }) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const squadId = navigation.searchParams.get("squad");

  const { data: agent, isLoading } = useQuery(agentDetailOptions(wsId, agentId));
  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));
  const { data: squads = [] } = useQuery(squadListOptions(wsId));
  const runtime = runtimes.find((item) => item.id === agent?.runtime_id) ?? null;
  const squad = squadId ? squads.find((item) => item.id === squadId) ?? null : null;
  const name = agent?.name ?? "";

  return (
    <AgentCreateShell
      title={t(($) => $.creation_studio.title)}
      step={t(($) => $.creation_studio.steps.done)}
      stepIndex={3}
      closeIcon
      onBack={() => navigation.replace(paths.agentDetail(agentId))}
    >
      <main className="flex min-h-0 flex-1 flex-col overflow-y-auto px-5 py-12">
        <div className="m-auto w-full max-w-xl">
          <div className="flex flex-col items-center text-center">
            {isLoading || !agent ? (
              <Skeleton className="size-14 rounded-full" />
            ) : (
              <span className="relative">
                <ActorAvatar
                  actorType="agent"
                  actorId={agent.id}
                  name={agent.name}
                  avatarUrl={agent.avatar_url}
                  size="2xl"
                />
                <CircleCheck
                  aria-hidden="true"
                  className="absolute -bottom-1 -right-1 size-5 rounded-full bg-background text-success"
                />
              </span>
            )}
            <h2 className="mt-5 text-balance text-title font-semibold tracking-tight">
              {isLoading
                ? t(($) => $.creation_studio.done.loading)
                : t(($) => $.creation_studio.done.title, { name })}
            </h2>
            {runtime ? (
              <p className="mt-1.5 text-body text-muted-foreground">
                {t(($) => $.creation_studio.done.runs_on, {
                  runtime: runtimeDisplayLabel(runtime),
                })}
              </p>
            ) : null}
            {squad ? (
              <p className="mt-3 rounded-full bg-success/10 px-2.5 py-1 text-caption text-success">
                {t(($) => $.creation_studio.done.joined_squad, { squad: squad.name })}
              </p>
            ) : null}
          </div>

          <p className="mt-10 text-label font-medium text-muted-foreground">
            {t(($) => $.creation_studio.done.next)}
          </p>
          <div className="mt-2 space-y-2">
            <DoneAction
              icon={ListPlus}
              title={t(($) => $.creation_studio.done.assign_title)}
              primary
              disabled={!agent}
              onClick={() =>
                useModalStore.getState().open("create-issue", {
                  assignee_type: "agent",
                  assignee_id: agentId,
                })
              }
            />
            <DoneAction
              icon={AtSign}
              title={t(($) => $.creation_studio.done.mention_title)}
              href={paths.issues()}
            />
            <DoneAction
              icon={Users}
              title={
                squad
                  ? t(($) => $.creation_studio.done.open_squad_title, { squad: squad.name })
                  : t(($) => $.creation_studio.done.squad_title)
              }
              href={squad ? paths.squadDetail(squad.id) : paths.squads()}
            />
          </div>

          <div className="mt-8 flex items-center justify-center gap-4 text-label">
            <AppLink
              href={paths.agentDetail(agentId)}
              className="font-medium text-foreground underline-offset-4 hover:underline"
            >
              {t(($) => $.creation_studio.done.open_agent)}
            </AppLink>
            <span aria-hidden="true" className="text-muted-foreground">·</span>
            <AppLink
              href={paths.agents()}
              className="text-muted-foreground underline-offset-4 hover:text-foreground hover:underline"
            >
              {t(($) => $.creation_studio.done.back_to_list)}
            </AppLink>
          </div>
        </div>
      </main>
    </AgentCreateShell>
  );
}

function DoneAction({
  icon: Icon,
  title,
  primary = false,
  disabled = false,
  href,
  onClick,
}: {
  icon: typeof ListPlus;
  title: string;
  primary?: boolean;
  disabled?: boolean;
  href?: string;
  onClick?: () => void;
}) {
  const className = cn(
    "group flex w-full items-center gap-3 rounded-lg border bg-card px-4 py-3 text-left",
    "transition-colors hover:bg-accent/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
    "disabled:pointer-events-none disabled:opacity-50",
    primary && "border-primary/30 bg-primary/[0.03]",
  );
  const body: ReactNode = (
    <>
      <span
        className={cn(
          "flex size-9 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground",
          primary && "bg-primary/10 text-primary",
        )}
      >
        <Icon aria-hidden="true" className="size-4" />
      </span>
      <span className="min-w-0 flex-1 text-body font-medium">{title}</span>
      <ArrowRight
        aria-hidden="true"
        className="size-4 shrink-0 text-muted-foreground transition-transform group-hover:translate-x-0.5"
      />
    </>
  );
  if (href) {
    return (
      <AppLink href={href} className={className}>
        {body}
      </AppLink>
    );
  }
  return (
    <button type="button" className={className} disabled={disabled} onClick={onClick}>
      {body}
    </button>
  );
}
