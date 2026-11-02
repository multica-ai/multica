"use client";

import { useState } from "react";
import { useQueries, useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { useChatStore } from "@multica/core/chat";
import { autopilotListOptions, useTriggerAutopilot } from "@multica/core/autopilots";
import { knowledgeBasesOptions, useCreateKnowledgeDocument } from "@multica/core/knowledge";
import {
  findSecretaryAutopilot,
  isBriefToday,
  officeSetupOptions,
  prefillSecretaryChat,
  proposalDetailOptions,
  splitBriefSections,
  type Brief,
  type BriefSection,
} from "@multica/core/office";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { squadListOptions } from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import { Dialog, DialogContent, DialogTitle } from "@multica/ui/components/ui/dialog";
import { Loader2, MessageSquare, Newspaper } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { isFloatingChatRouteSuppressed } from "../../chat/floating-chat-visibility";
import { ReadonlyContent } from "../../editor/readonly-content";
import { useT } from "../../i18n";
import { useNavigation } from "../../navigation";
import { OfficeBriefDelivery } from "./office-brief-delivery";
import { OfficeBriefFeedback } from "./office-brief-feedback";
import { OfficeProposalDecision } from "./office-proposal-decision";
import { OfficeProposalHistory, OfficeProposalRelated, OfficeProposalStatus } from "./office-proposal-details";

/** The knowledge base a brief is saved into. */
const DECISIONS_BASE = "decisions";

/**
 * The Secretary's latest brief, with its sections, the proposals it raises and
 * the actions on it. With no brief from today, the empty state offers Generate
 * now, which runs the Secretary's autopilot by hand. Earlier briefs are listed
 * beside it.
 */
export function OfficeBriefDialog({
  open,
  briefs,
  canManage,
  onClose,
}: {
  open: boolean;
  /** Newest first. */
  briefs: readonly Brief[];
  /** Owners and admins may decide proposals and save a brief to knowledge. */
  canManage: boolean;
  onClose: () => void;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [delivery, setDelivery] = useState(false);
  const setup = useQuery(officeSetupOptions(wsId));
  const autopilots = useQuery(autopilotListOptions(wsId));
  const trigger = useTriggerAutopilot();

  const latest = briefs[0] ?? null;
  const shown = briefs.find((brief) => brief.id === selectedId) ?? (latest && isBriefToday(latest) ? latest : null);
  const earlier = briefs.filter((brief) => brief.id !== shown?.id);
  const secretaryId = setup.data?.secretary_id ?? null;
  const autopilot = findSecretaryAutopilot(autopilots.data ?? [], secretaryId);

  const generateReason = !setup.isSuccess || !autopilots.isSuccess
    ? null
    : !secretaryId
      ? t(($) => $.office.brief_no_secretary)
      : !autopilot
        ? t(($) => $.office.brief_no_autopilot)
        : null;

  const generate = () => {
    if (!autopilot) return;
    trigger.mutate(autopilot.id, {
      onSuccess: () => toast.success(t(($) => $.office.brief_generating)),
      onError: () => toast.error(t(($) => $.office.brief_generate_failed)),
    });
  };

  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="flex max-h-[min(88dvh,48rem)] flex-col gap-0 overflow-hidden p-0 sm:max-w-3xl">
        <header className="flex shrink-0 items-center justify-between gap-3 border-b px-4 py-3 pr-12">
          <DialogTitle className="text-heading">{t(($) => $.office.brief_open)}</DialogTitle>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            aria-expanded={delivery}
            onClick={() => setDelivery((open) => !open)}
          >
            {t(($) => $.office.delivery_toggle)}
          </Button>
        </header>
        {delivery ? (
          <div className="shrink-0 border-b px-4 py-3">
            <OfficeBriefDelivery />
          </div>
        ) : null}
        <div className="flex min-h-0 flex-1 flex-col md:flex-row">
          <div className="min-h-0 flex-1 overflow-y-auto px-4 py-3">
            {shown ? (
              <BriefBody
                key={shown.id}
                brief={shown}
                canManage={canManage}
                secretaryId={secretaryId}
                onAsk={onClose}
              />
            ) : (
              <div className="flex flex-col items-center gap-3 py-10 text-center">
                <Newspaper className="size-8 text-muted-foreground" />
                <h3 className="text-heading">{t(($) => $.office.brief_empty_title)}</h3>
                <Button
                  type="button"
                  disabled={!autopilot || trigger.isPending}
                  aria-busy={trigger.isPending}
                  onClick={generate}
                >
                  {trigger.isPending ? <Loader2 className="size-4 animate-spin" /> : null}
                  {t(($) => $.office.brief_generate)}
                </Button>
                {generateReason ? (
                  <p className="text-caption text-muted-foreground">{generateReason}</p>
                ) : null}
              </div>
            )}
          </div>
          {earlier.length > 0 ? (
            <aside className="max-h-40 shrink-0 overflow-y-auto border-t p-2 md:max-h-none md:w-64 md:border-t-0 md:border-l">
              <h3 className="px-2 py-1 text-caption text-muted-foreground">{t(($) => $.office.brief_earlier)}</h3>
              <ul>
                {earlier.map((brief) => (
                  <li key={brief.id}>
                    <button
                      type="button"
                      onClick={() => setSelectedId(brief.id)}
                      className="flex w-full min-w-0 flex-col items-start gap-0.5 rounded-md px-2 py-1.5 text-left hover:bg-accent"
                    >
                      <span className="w-full truncate text-body">{brief.title}</span>
                      <span className="text-caption text-muted-foreground">
                        {formatBriefDate(brief.created_at)}
                      </span>
                    </button>
                  </li>
                ))}
              </ul>
            </aside>
          ) : null}
        </div>
      </DialogContent>
    </Dialog>
  );
}

function formatBriefDate(iso: string): string {
  const date = new Date(iso);
  return Number.isNaN(date.getTime()) ? "" : date.toLocaleDateString(undefined, { month: "short", day: "numeric" });
}

function BriefBody({
  brief,
  canManage,
  secretaryId,
  onAsk,
}: {
  brief: Brief;
  canManage: boolean;
  secretaryId: string | null;
  onAsk: () => void;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const navigation = useNavigation();
  const paths = useWorkspacePaths();
  const sections = splitBriefSections(brief.body);

  const proposals = useQueries({ queries: brief.proposal_ids.map((id) => proposalDetailOptions(wsId, id)) });
  const squads = useQuery(squadListOptions(wsId));
  const bases = useQuery(knowledgeBasesOptions(wsId));
  const saveToKnowledge = useCreateKnowledgeDocument(wsId);

  const knowledgeReady =
    !!bases.data?.offered &&
    bases.data.state === "ready" &&
    bases.data.knowledge_bases.some((base) => base.key === DECISIONS_BASE);

  const save = () =>
    saveToKnowledge.mutate(
      {
        kb: DECISIONS_BASE,
        title: brief.title,
        content: brief.body,
        source_type: "brief",
        source_id: brief.id,
        idempotency_key: `brief:${brief.id}`,
        publish: true,
      },
      {
        onSuccess: () => toast.success(t(($) => $.office.brief_saved)),
        onError: () => toast.error(t(($) => $.office.brief_save_failed)),
      },
    );

  // The chat window floats over the page; the dialog closes so it is not hidden under the overlay.
  const ask = (section: BriefSection) => {
    if (!secretaryId) return;
    prefillSecretaryChat(
      secretaryId,
      `${t(($) => $.office.brief_ask_intro, { section: section.heading })}\n\n${section.body}`,
    );
    const chat = useChatStore.getState();
    const chatPath = paths.chat();
    if (chat.floatingChatEnabled && !isFloatingChatRouteSuppressed(navigation.pathname, chatPath)) {
      chat.setOpen(true);
    } else {
      navigation.push(chatPath);
    }
    onAsk();
  };

  return (
    <article className="flex flex-col gap-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h3 className="text-heading">{brief.title}</h3>
          <p className="text-caption text-muted-foreground">{formatBriefDate(brief.created_at)}</p>
        </div>
        {canManage ? (
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={!knowledgeReady || saveToKnowledge.isPending}
            aria-busy={saveToKnowledge.isPending}
            onClick={save}
          >
            {t(($) => $.office.brief_save)}
          </Button>
        ) : null}
      </div>
      {canManage && bases.isSuccess && !knowledgeReady ? (
        <p className="text-caption text-muted-foreground">{t(($) => $.office.brief_save_unavailable)}</p>
      ) : null}

      {sections.length > 0 ? (
        sections.map((section) => (
          <section key={section.heading} className="flex flex-col gap-2">
            <div className="flex items-center justify-between gap-2">
              <h4 className="text-body font-medium">{section.heading}</h4>
              {secretaryId ? (
                <Button
                  type="button"
                  variant="ghost"
                  size="xs"
                  aria-label={t(($) => $.office.brief_ask_label, { section: section.heading })}
                  onClick={() => ask(section)}
                >
                  <MessageSquare className="size-3.5" />
                  {t(($) => $.office.brief_ask)}
                </Button>
              ) : null}
            </div>
            <ReadonlyContent content={section.body} />
          </section>
        ))
      ) : (
        <ReadonlyContent content={brief.body} />
      )}

      <OfficeBriefFeedback briefId={brief.id} canManage={canManage} />

      {proposals.length > 0 ? (
        <section className="flex flex-col gap-2">
          <h4 className="text-body font-medium">{t(($) => $.office.brief_proposals)}</h4>
          <ul className="flex flex-col gap-3">
            {proposals.map((query, index) => {
              const proposal = query.data;
              if (!proposal) return null;
              const squadName = squads.data?.find((squad) => squad.id === proposal.squad_id)?.name ?? null;
              const handoffName = squads.data?.find((squad) => squad.id === proposal.handoff_squad_id)?.name ?? null;
              return (
                <li key={brief.proposal_ids[index]} className={cn("rounded-lg border", proposal.state !== "pending" && "bg-muted/40")}>
                  <div className="flex flex-col gap-2 px-4 py-3">
                    <div className="flex flex-col gap-0.5">
                      <span className="text-caption text-muted-foreground">{proposal.issue_identifier}</span>
                      <span className="text-body font-medium">{proposal.title}</span>
                    </div>
                    <OfficeProposalStatus proposal={proposal} squadName={squadName} />
                    {proposal.state === "pending" && !canManage ? (
                      <span className="text-caption text-muted-foreground">{t(($) => $.office.squad_proposals)}</span>
                    ) : null}
                    <OfficeProposalRelated related={proposal.related} />
                    <OfficeProposalHistory proposal={proposal} />
                  </div>
                  <OfficeProposalDecision
                    proposal={proposal}
                    handoffName={handoffName}
                    canDecide={canManage}
                    className="px-4 pt-2 pb-3"
                  />
                </li>
              );
            })}
          </ul>
        </section>
      ) : null}
    </article>
  );
}
