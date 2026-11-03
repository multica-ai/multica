"use client";

import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentMember } from "@multica/core/permissions";
import {
  canManageInitiatives,
  initiativeDetailOptions,
  type Pipeline,
  type PipelineStage,
} from "@multica/core/pipeline";
import type { Squad } from "@multica/core/types";
import { useWorkspacePaths } from "@multica/core/paths";
import { useT } from "../i18n";
import { AppLink } from "../navigation";
import { InitiativeActions } from "./initiative-actions";
import { InitiativeArtifacts } from "./initiative-artifacts";
import { InitiativeForce } from "./initiative-force";
import { InitiativeGates } from "./initiative-gates";
import { InitiativeThread } from "./initiative-thread";
import { InitiativeTimeline } from "./initiative-timeline";

/**
 * One initiative: the overview and exit criteria, the stage transitions the
 * caller may take, its artifacts for the current stage, the backing issue's
 * thread, the gate proposals, the stage timeline and, for an owner or admin, the
 * forced move. Reads the detail once; every write refetches it.
 */
export function InitiativePanel({
  initiativeId,
  pipeline,
  squads,
}: {
  initiativeId: string;
  pipeline: Pipeline | null;
  squads: readonly Squad[];
}) {
  const { t } = useT("pipeline");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const { role } = useCurrentMember(wsId);
  const canManage = canManageInitiatives(role);
  const detail = useQuery(initiativeDetailOptions(wsId, initiativeId));

  if (detail.isPending) {
    return <p className="px-4 py-6 text-body text-muted-foreground">{t(($) => $.panel.loading)}</p>;
  }
  const data = detail.data;
  if (!data || !data.initiative.id) {
    return (
      <p role="alert" className="px-4 py-6 text-body text-destructive">
        {t(($) => $.panel.unavailable)}
      </p>
    );
  }

  const { initiative, artifacts, events, gates } = data;
  const stage = pipeline?.stages.find((candidate) => candidate.key === initiative.stage_key) ?? null;
  const stageName = (key: string | null) =>
    (key ? pipeline?.stages.find((candidate) => candidate.key === key)?.name : null) || key || "";
  const owner = squads.find((squad) => squad.id === initiative.owner_squad_id)?.name ?? null;
  const currentArtifacts = artifacts.filter((artifact) => artifact.stage_key === initiative.stage_key);

  return (
    <div className="flex flex-col gap-6 px-4 pb-6">
      <section aria-labelledby="initiative-overview" className="flex flex-col gap-3">
        <h3 id="initiative-overview" className="text-caption font-medium text-muted-foreground">
          {t(($) => $.panel.overview)}
        </h3>
        <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-body">
          <dt className="text-muted-foreground">{t(($) => $.panel.stage)}</dt>
          <dd>{stage?.name || initiative.stage_key}</dd>
          <dt className="text-muted-foreground">{t(($) => $.panel.owner)}</dt>
          <dd>{owner ?? t(($) => $.card.no_owner)}</dd>
          <dt className="text-muted-foreground">{t(($) => $.panel.status)}</dt>
          <dd>{t(($) => $.status[initiative.status])}</dd>
          <dt className="text-muted-foreground">{t(($) => $.panel.days_in_stage)}</dt>
          <dd className="tabular-nums">{initiative.days_in_stage}</dd>
          <dt className="text-muted-foreground">{t(($) => $.panel.sla)}</dt>
          <dd className={initiative.sla_state === "stalled" ? "text-destructive" : undefined}>
            {initiative.sla_state === "stalled" ? t(($) => $.stage_sla.stalled) : t(($) => $.stage_sla.ok)}
          </dd>
        </dl>
        {initiative.thesis ? <p className="whitespace-pre-line text-body">{initiative.thesis}</p> : null}
        {stage ? <StageExit stage={stage} /> : null}
      </section>

      <InitiativeActions initiative={initiative} stages={pipeline?.stages ?? []} canManage={canManage} />

      <InitiativeArtifacts
        initiativeId={initiative.id}
        artifacts={currentArtifacts}
        canManage={canManage}
      />

      <InitiativeThread issueId={initiative.issue_id} />

      <InitiativeGates gates={gates} canManage={canManage} />

      <AppLink href={paths.issueDetail(initiative.issue_id)} className="text-body text-primary hover:underline">
        {t(($) => $.panel.open_issue)}
      </AppLink>

      <InitiativeTimeline events={events} stageName={stageName} />

      {canManage && pipeline ? (
        <InitiativeForce
          initiativeId={initiative.id}
          currentStage={initiative.stage_key}
          stages={pipeline.stages}
        />
      ) : null}
    </div>
  );
}

/** The exit rule of a stage: how it hands on, and the criteria it hands on by. */
function StageExit({ stage }: { stage: PipelineStage }) {
  const { t } = useT("pipeline");
  return (
    <div className="flex flex-col gap-1 rounded-lg bg-muted/50 px-3 py-2 text-caption">
      <span className="font-medium text-foreground">{t(($) => $.panel.exit_mode[stage.exit.mode])}</span>
      {stage.exit.criteria ? (
        <p className="whitespace-pre-line text-muted-foreground">{stage.exit.criteria}</p>
      ) : null}
    </div>
  );
}
