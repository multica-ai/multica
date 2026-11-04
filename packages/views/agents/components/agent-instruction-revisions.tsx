"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { ApiError } from "@multica/core/api";
import {
  agentInstructionRevisionsOptions,
  useRestoreAgentInstructionRevision,
  type InstructionRevision,
} from "@multica/core/agents";
import { useWorkspaceId } from "@multica/core/hooks";
import type { Agent } from "@multica/core/types";
import { useActorName } from "@multica/core/workspace/hooks";
import { Button } from "@multica/ui/components/ui/button";
import { ChevronDown, ChevronRight, RotateCcw } from "@multica/ui/icons";
import { ObjectSection } from "../../layout/object-page";
import { useT, useTimeAgo } from "../../i18n";

/**
 * The agent's instruction history (spec 4.6): every saved version, newest first,
 * with its source and author. A restore sets the instructions back to that
 * version, and the server records the restore as a new revision. Loaded only
 * once it is opened, so the agent page does not fetch history it is not showing.
 */
export function AgentInstructionRevisions({ agent, canEdit }: { agent: Agent; canEdit: boolean }) {
  const { t } = useT("agents");
  const [open, setOpen] = useState(false);

  return (
    <ObjectSection
      title={t(($) => $.instruction_revisions.title)}
      action={
        <button
          type="button"
          aria-expanded={open}
          onClick={() => setOpen((value) => !value)}
          className="flex h-7 items-center gap-1 rounded-sm px-1 text-caption font-medium text-muted-foreground hover:text-foreground"
        >
          {open ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" />}
          {open ? t(($) => $.instruction_revisions.hide) : t(($) => $.instruction_revisions.show)}
        </button>
      }
    >
      {open ? <RevisionList agent={agent} canEdit={canEdit} /> : null}
    </ObjectSection>
  );
}

function RevisionList({ agent, canEdit }: { agent: Agent; canEdit: boolean }) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const history = useQuery(agentInstructionRevisionsOptions(wsId, agent.id));

  if (history.isPending) {
    return <p className="text-caption text-muted-foreground">{t(($) => $.instruction_revisions.loading)}</p>;
  }
  if (history.isError) {
    return (
      <p role="alert" className="text-caption text-destructive">
        {t(($) => $.instruction_revisions.load_failed)}
      </p>
    );
  }
  const revisions = history.data;
  if (revisions.length === 0) {
    return <p className="text-caption text-muted-foreground">{t(($) => $.instruction_revisions.empty)}</p>;
  }

  return (
    <ol className="divide-y overflow-hidden rounded-lg border bg-card">
      {revisions.map((revision) => (
        <RevisionRow
          key={revision.id || revision.revision}
          agent={agent}
          revision={revision}
          canRestore={canEdit}
        />
      ))}
    </ol>
  );
}

function RevisionRow({
  agent,
  revision,
  canRestore,
}: {
  agent: Agent;
  revision: InstructionRevision;
  canRestore: boolean;
}) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const timeAgo = useTimeAgo();
  const { getActorName } = useActorName();
  const [showText, setShowText] = useState(false);
  const restore = useRestoreAgentInstructionRevision(wsId, agent.id);
  const isCurrent = revision.instructions === (agent.instructions ?? "");
  const restoring = restore.isPending && restore.variables === revision.revision;
  const sourceText: Record<InstructionRevision["source"], string> = {
    human: t(($) => $.instruction_revisions.source.human),
    proposal: t(($) => $.instruction_revisions.source.proposal),
    rollback: t(($) => $.instruction_revisions.source.rollback),
    unknown: t(($) => $.instruction_revisions.source.unknown),
  };

  const doRestore = () =>
    restore.mutate(revision.revision, {
      onSuccess: () => toast.success(t(($) => $.instruction_revisions.restored, { revision: revision.revision })),
      onError: (error) =>
        toast.error(
          error instanceof ApiError && error.status === 409
            ? t(($) => $.instruction_revisions.restore_unchanged)
            : t(($) => $.instruction_revisions.restore_failed),
        ),
    });

  // Only a member or an agent is named from the directory; anything else has no author to show.
  const author =
    (revision.actor_type === "member" || revision.actor_type === "agent") && revision.actor_id
      ? getActorName(revision.actor_type, revision.actor_id)
      : null;

  return (
    <li className="flex flex-col gap-2 px-3 py-2.5">
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
        <span className="text-body font-medium">
          {t(($) => $.instruction_revisions.revision_label, { revision: revision.revision })}
        </span>
        <span className="text-caption text-muted-foreground">{sourceText[revision.source]}</span>
        {author ? <span className="text-caption text-muted-foreground">{author}</span> : null}
        <span className="text-caption text-muted-foreground">{timeAgo(revision.created_at)}</span>
        {isCurrent ? (
          <span className="rounded-md bg-muted px-1.5 py-0.5 text-caption text-muted-foreground">
            {t(($) => $.instruction_revisions.current)}
          </span>
        ) : null}
        <div className="ml-auto flex items-center gap-1">
          <button
            type="button"
            aria-expanded={showText}
            onClick={() => setShowText((value) => !value)}
            className="rounded-sm px-1.5 py-0.5 text-caption font-medium text-muted-foreground hover:text-foreground"
          >
            {showText ? t(($) => $.instruction_revisions.hide_text) : t(($) => $.instruction_revisions.show_text)}
          </button>
          {canRestore && !isCurrent ? (
            <Button
              size="sm"
              variant="outline"
              disabled={restore.isPending}
              aria-busy={restoring || undefined}
              aria-label={t(($) => $.instruction_revisions.restore_named, { revision: revision.revision })}
              onClick={doRestore}
            >
              <RotateCcw className="size-3.5" />
              {t(($) => $.instruction_revisions.restore)}
            </Button>
          ) : null}
        </div>
      </div>
      {showText ? (
        <pre className="max-h-80 overflow-auto rounded-md border bg-muted/30 px-3 py-2 text-caption leading-6 whitespace-pre-wrap">
          {revision.instructions}
        </pre>
      ) : null}
    </li>
  );
}
