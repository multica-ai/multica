"use client";

import { useQuery } from "@tanstack/react-query";
import {
  orgChangePayloadOf,
  squadCharterOptions,
  type OrgChangeCharter,
  type OrgChangePayload,
  type Proposal,
} from "@multica/core/company";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentDetailOptions } from "@multica/core/workspace/queries";
import { DiffView } from "../../common/diff-view";
import { useT } from "../../i18n";

/** The organisation change types this client names. Any other type reads as the generic kind. */
const KNOWN_ORG_CHANGE_TYPES = [
  "instruction_change",
  "skill_change",
  "charter_change",
  "autonomy_change",
  "hire",
  "retire",
  "move",
  "merge_squads",
  "unretire",
  "unmerge",
] as const;
type KnownOrgChangeType = (typeof KNOWN_ORG_CHANGE_TYPES)[number];

function knownOrgChangeType(type: string): KnownOrgChangeType | null {
  return (KNOWN_ORG_CHANGE_TYPES as readonly string[]).includes(type) ? (type as KnownOrgChangeType) : null;
}

/**
 * What an organisation change asks the boss (spec 4.6). A pending instruction or
 * charter change shows the text it would replace as a diff against the current
 * version. Every other change, and any change already decided, shows its type,
 * its summary and its rationale, because the current version may have moved on.
 */
export function OrgChangeSummary({ proposal }: { proposal: Proposal }) {
  const payload = orgChangePayloadOf(proposal);
  const pending = proposal.state === "pending";

  if (pending && payload.type === "instruction_change" && payload.agent_id) {
    return <InstructionChangeDiff payload={payload} />;
  }
  if (pending && payload.type === "charter_change" && payload.squad_id && payload.charter) {
    return <CharterChangeDiff squadId={payload.squad_id} charter={payload.charter} rationale={payload.rationale} />;
  }
  return <OrgChangeText payload={payload} />;
}

function OrgChangeText({ payload }: { payload: OrgChangePayload }) {
  const { t } = useT("governance");
  const { t: tPipeline } = useT("pipeline");
  const known = knownOrgChangeType(payload.type);
  const heading = known
    ? t(($) => $.reviews_tab.change[known])
    : tPipeline(($) => $.kind.label.org_change);

  return (
    <section className="flex flex-col gap-1.5">
      <h4 className="text-caption font-medium text-muted-foreground">{heading}</h4>
      {payload.diff_summary ? <p className="text-body">{payload.diff_summary}</p> : null}
      {payload.rationale ? <p className="whitespace-pre-line text-body">{payload.rationale}</p> : null}
    </section>
  );
}

function InstructionChangeDiff({ payload }: { payload: OrgChangePayload }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const current = useQuery(agentDetailOptions(wsId, payload.agent_id));

  return (
    <section className="flex flex-col gap-1.5">
      <h4 className="text-caption font-medium text-muted-foreground">
        {t(($) => $.company.decision_change_instructions)}
      </h4>
      {payload.diff_summary ? <p className="text-body">{payload.diff_summary}</p> : null}
      {current.isPending ? (
        <p className="text-caption text-muted-foreground">{t(($) => $.company.charter_loading)}</p>
      ) : current.isError || !current.data ? (
        <p role="alert" className="text-caption text-destructive">
          {t(($) => $.company.decision_change_unreadable)}
        </p>
      ) : (
        <DiffView
          before={current.data.instructions ?? ""}
          after={payload.instructions}
          removedLabel={t(($) => $.company.decision_change_removed)}
          addedLabel={t(($) => $.company.decision_change_added)}
        />
      )}
      {payload.rationale ? <p className="whitespace-pre-line text-body">{payload.rationale}</p> : null}
    </section>
  );
}

function CharterChangeDiff({
  squadId,
  charter,
  rationale,
}: {
  squadId: string;
  charter: OrgChangeCharter;
  rationale: string;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const current = useQuery(squadCharterOptions(wsId, squadId));
  const labels: CharterLabels = {
    mission: t(($) => $.company.charter_mission),
    responsibilities: t(($) => $.company.charter_responsibilities),
    outputs: t(($) => $.company.charter_outputs),
    boundaries: t(($) => $.company.charter_boundaries),
    escalation: t(($) => $.company.charter_escalation),
    intake: t(($) => $.company.charter_intake),
    responseHours: t(($) => $.company.charter_hours_label),
    notSet: t(($) => $.company.charter_not_set),
  };

  return (
    <section className="flex flex-col gap-1.5">
      <h4 className="text-caption font-medium text-muted-foreground">
        {t(($) => $.company.decision_change_charter)}
      </h4>
      {current.isPending ? (
        <p className="text-caption text-muted-foreground">{t(($) => $.company.charter_loading)}</p>
      ) : current.isError || !current.data ? (
        <p role="alert" className="text-caption text-destructive">
          {t(($) => $.company.decision_change_unreadable)}
        </p>
      ) : (
        <DiffView
          before={charterText(current.data, labels)}
          after={charterText(charter, labels)}
          removedLabel={t(($) => $.company.decision_change_removed)}
          addedLabel={t(($) => $.company.decision_change_added)}
        />
      )}
      {rationale ? <p className="whitespace-pre-line text-body">{rationale}</p> : null}
    </section>
  );
}

type CharterLabels = {
  mission: string;
  responsibilities: string;
  outputs: string;
  boundaries: string;
  escalation: string;
  intake: string;
  responseHours: string;
  notSet: string;
};

/**
 * A charter as the lines a diff compares: one labelled block per field, in the
 * order the charter editor shows them. A field with no text reads as not set, so
 * an added or cleared field shows as a change rather than a gap.
 */
function charterText(charter: CharterFieldsForText, labels: CharterLabels): string {
  const block = (label: string, body: string[]) => [label, ...(body.length > 0 ? body : [labels.notSet]), ""];
  const text = (value: string) => (value.trim() ? value.split("\n") : []);
  return [
    ...block(labels.mission, text(charter.mission)),
    ...block(
      labels.responsibilities,
      charter.responsibilities.filter((item) => item.text.trim()).map((item) => `- ${item.text}`),
    ),
    ...block(
      labels.outputs,
      charter.outputs
        .filter((output) => output.key.trim())
        .map((output) => `- ${output.key}${output.kind ? ` (${output.kind})` : ""}`),
    ),
    ...block(labels.boundaries, text(charter.boundaries)),
    ...block(labels.escalation, text(charter.escalation)),
    ...block(labels.intake, text(charter.intake)),
    ...block(labels.responseHours, [String(charter.response_hours)]),
  ].join("\n");
}

type CharterFieldsForText = Pick<
  OrgChangeCharter,
  "mission" | "responsibilities" | "outputs" | "boundaries" | "escalation" | "intake" | "response_hours"
>;
