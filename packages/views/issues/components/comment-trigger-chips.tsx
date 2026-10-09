"use client";

import { useMemo, useState, type ReactElement } from "react";
import { ChevronDown, TriangleAlert, Users } from "lucide-react";
import type { CommentTriggerPreviewAgent, CommentTriggerOutcome } from "@multica/core/types";
import type { AgentRunState, RecipientAction } from "@multica/core/issues/run-steering";
import { useAgentPresenceDetail } from "@multica/core/agents";
import { mentionLabelsByTarget } from "@multica/core/issues/comment-trigger-outcomes";
import { useCurrentWorkspace } from "@multica/core/paths";
import { ActorAvatar as ActorAvatarBase } from "@multica/ui/components/common/actor-avatar";
import { AVATAR_SIZE_PX } from "@multica/ui/lib/avatar-size";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { Tooltip, TooltipContent, TooltipTrigger } from "@multica/ui/components/ui/tooltip";
import { cn } from "@multica/ui/lib/utils";
import { AgentStatusDot } from "../../common/actor-avatar";
import { useT } from "../../i18n";
import { blockedReasonLabel, blockedShortReasonLabel } from "../blocked-trigger-copy";
import type { RecipientEntry } from "../hooks/use-recipient-actions";

// Each recipient's chip says what Send will do to it, and its menu offers only
// what that recipient's current state allows: a running turn can take the
// message now, after the turn, or instead of it; an idle or queued agent can
// only start (or fold the message into its queued run) or be skipped.
// One recipient renders as a single chip. Several collapse to an overlapping
// avatar stack, mirroring WorkspaceAgentWorkingChip on the issues header, with
// one row and menu per agent in a click-opened Popover.
// The avatars render at the `xs` tier; the `+N` overflow chip and stack
// overlap reuse that tier's pixel diameter so the stack lines up exactly.
const AVATAR_SIZE = AVATAR_SIZE_PX.xs;
const MAX_STACK_HEADS = 4;

interface CommentTriggerChipsProps {
  recipients: RecipientEntry[];
  // Explicit @agent / @squad mentions that will NOT trigger if posted as-is
  // (MUL-4525 §2). Each renders as a named warning chip so the user sees WHICH
  // target won't run and why, not a silent no-op after sending.
  blocked?: CommentTriggerOutcome[];
  /** Whether the draft contains the structured @all member broadcast. */
  hasAllMembersMention?: boolean;
  // The draft markdown, used only to label each blocked target with the name the
  // user typed in its mention markup. The server omits blocked target names
  // (enumeration-safety); this is the user's own text, so it discloses nothing new.
  draftContent?: string;
  onActionChange: (agentId: string, action: RecipientAction) => void;
}

type IssuesT = ReturnType<typeof useT<"issues">>["t"];

function sourceLabel(source: string, t: IssuesT): string {
  switch (source) {
    case "issue_assignee":
      return t(($) => $.comment.trigger_source_issue_assignee);
    case "mention_agent":
      return t(($) => $.comment.trigger_source_mention_agent);
    case "mention_squad_leader":
      return t(($) => $.comment.trigger_source_mention_squad_leader);
    default:
      return t(($) => $.comment.trigger_source_unknown);
  }
}

// Presence is display metadata only — the trigger list itself is always the
// backend preview. Online-ish agents start right away; offline ones queue.
function useTriggerPresenceLine(agentId: string, t: IssuesT): string | null {
  const ws = useCurrentWorkspace();
  const detail = useAgentPresenceDetail(ws?.id, agentId);
  if (detail === "loading") return null;
  return detail.availability === "online" || detail.availability === "unstable"
    ? t(($) => $.comment.trigger_starts_now)
    : t(($) => $.comment.trigger_starts_when_online);
}

function stateLabel(state: AgentRunState, t: IssuesT): string | null {
  switch (state.kind) {
    case "running":
      return t(($) => $.comment.recipient_state_running);
    case "starting":
      return t(($) => $.comment.recipient_state_starting);
    case "queued":
      return t(($) => $.comment.recipient_state_queued);
    default:
      return null;
  }
}

function actionLabel(action: RecipientAction, state: AgentRunState, t: IssuesT): string {
  switch (action) {
    case "steer":
      return t(($) => $.comment.recipient_steer);
    case "after_run":
      return t(($) => $.comment.recipient_after_run);
    case "restart":
      return t(($) => $.comment.recipient_restart);
    case "skip":
      return t(($) => $.comment.trigger_wont_trigger);
    default:
      return state.kind === "queued"
        ? t(($) => $.comment.recipient_join)
        : t(($) => $.comment.trigger_will_start);
  }
}

// Only choices whose consequence is not obvious from the label carry a line.
function actionDescription(action: RecipientAction, entry: RecipientEntry, presenceLine: string | null, t: IssuesT): string | null {
  const name = entry.agent.name;
  switch (action) {
    case "steer":
      return t(($) => $.comment.recipient_steer_desc, { name });
    case "after_run":
      return t(($) => $.comment.recipient_after_run_desc);
    case "restart":
      return t(($) => $.comment.recipient_restart_desc);
    case "start":
      return entry.state.kind === "queued" ? t(($) => $.comment.recipient_join_desc, { name }) : presenceLine;
    default:
      return null;
  }
}

function chipToneClass(action: RecipientAction): string {
  if (action === "steer") {
    return "border-brand/28 bg-brand/7 text-foreground hover:bg-brand/12 aria-expanded:bg-brand/12 dark:border-brand/45 dark:bg-brand/12 dark:hover:bg-brand/18";
  }
  if (action === "restart") {
    return "text-destructive hover:bg-destructive/10 hover:text-destructive aria-expanded:bg-destructive/10";
  }
  return "text-muted-foreground hover:bg-muted hover:text-foreground aria-expanded:bg-muted aria-expanded:text-foreground";
}

export function CommentTriggerChips({
  recipients,
  blocked = [],
  hasAllMembersMention = false,
  draftContent = "",
  onActionChange,
}: CommentTriggerChipsProps) {
  const { t } = useT("issues");
  // Blocked outcomes carry no name (enumeration-safety); recover the label the
  // user typed from their own draft so each chip can say which target it is.
  const blockedLabels = useMemo(() => mentionLabelsByTarget(draftContent), [draftContent]);

  // Loading and errors render nothing: the preview is an enhancement, and
  // any interim chrome here reads as composer noise.
  if (recipients.length === 0 && blocked.length === 0 && !hasAllMembersMention) return null;

  const allowed =
    recipients.length === 1 ? (
      <SingleRecipientChip entry={recipients[0]!} onActionChange={onActionChange} t={t} />
    ) : recipients.length > 1 ? (
      <MultiRecipientChip recipients={recipients} onActionChange={onActionChange} t={t} />
    ) : null;

  if (blocked.length === 0 && !hasAllMembersMention) return allowed;

  return (
    <div className="flex flex-wrap items-center gap-1.5">
      {hasAllMembersMention && (
        <span className="inline-flex h-6 min-w-0 max-w-full animate-in fade-in items-center gap-1.5 rounded-md px-1.5 text-micro font-medium text-muted-foreground">
          <Users className="size-3 shrink-0" />
          <span className="truncate">{t(($) => $.comment.all_members_notice)}</span>
        </span>
      )}
      {allowed}
      {blocked.map((outcome) => (
        <BlockedTriggerChip
          key={`${outcome.target_type}:${outcome.target_id}`}
          outcome={outcome}
          label={blockedLabels.get(`${outcome.target_type}:${outcome.target_id}`)}
          t={t}
        />
      ))}
    </div>
  );
}

// One blocked mention: named like an allowed chip ("Go"), but with an error
// indicator and a short reason ("Not found or no permission") instead of "will
// start", so a refused @mention reads as a clear, specific error rather than a
// vague count.
function BlockedTriggerChip({
  outcome,
  label,
  t,
}: {
  outcome: CommentTriggerOutcome;
  label?: string;
  t: IssuesT;
}) {
  const shortReason = blockedShortReasonLabel(outcome.reason_code, t);
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <span
            className="inline-flex h-6 min-w-0 max-w-full animate-in fade-in items-center gap-1.5 rounded-md px-1.5 text-micro font-medium text-destructive"
            aria-label={
              label
                ? t(($) => $.comment.trigger_blocked_chip_aria, { name: label, reason: shortReason })
                : shortReason
            }
          >
            <TriangleAlert className="size-3 shrink-0" />
            {label ? (
              <span className="inline-flex min-w-0 items-center gap-1">
                <span className="truncate">{label}</span>
                <span className="shrink-0">·</span>
                <span className="shrink-0">{shortReason}</span>
              </span>
            ) : (
              <span className="truncate">{shortReason}</span>
            )}
          </span>
        }
      />
      <TooltipContent side="top" className="max-w-72 text-caption">
        {blockedReasonLabel(outcome.reason_code, t)}
      </TooltipContent>
    </Tooltip>
  );
}

// The per-recipient choice menu. `trigger` is the element that opens it: the
// chip itself for a single recipient, a row button inside the stack popover.
function RecipientActionMenu({
  entry,
  onActionChange,
  trigger,
  t,
}: {
  entry: RecipientEntry;
  onActionChange: (agentId: string, action: RecipientAction) => void;
  trigger: ReactElement;
  t: IssuesT;
}) {
  const presenceLine = useTriggerPresenceLine(entry.agent.id, t);
  const state = stateLabel(entry.state, t);
  return (
    <DropdownMenu>
      <DropdownMenuTrigger render={trigger} />
      <DropdownMenuContent side="top" align="start" className="w-80">
        <div className="flex min-w-0 items-center gap-2 px-1.5 pt-1 pb-1.5 text-caption text-muted-foreground">
          <TriggerAgentAvatar agent={entry.agent} suppressed={false} />
          <span className="min-w-0 truncate font-medium text-foreground">{entry.agent.name}</span>
          <span className="shrink-0">{state ?? sourceLabel(entry.agent.source, t)}</span>
        </div>
        <DropdownMenuRadioGroup
          value={entry.action}
          onValueChange={(value) => onActionChange(entry.agent.id, value as RecipientAction)}
        >
          {entry.actions.map((action) => {
            const description = actionDescription(action, entry, presenceLine, t);
            return (
              <div key={action}>
                {action === "skip" && <DropdownMenuSeparator />}
                <DropdownMenuRadioItem value={action} className="items-start py-1.5">
                  <span className="flex min-w-0 flex-col gap-0.5">
                    <span>{actionLabel(action, entry.state, t)}</span>
                    {description && <span className="text-caption text-muted-foreground">{description}</span>}
                  </span>
                </DropdownMenuRadioItem>
              </div>
            );
          })}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function SingleRecipientChip({
  entry,
  onActionChange,
  t,
}: {
  entry: RecipientEntry;
  onActionChange: (agentId: string, action: RecipientAction) => void;
  t: IssuesT;
}) {
  const label = actionLabel(entry.action, entry.state, t);
  // The avatar carries "who"; the sentence carries only the outcome, so it
  // stays fixed-width and never truncates on long agent names.
  return (
    <RecipientActionMenu
      entry={entry}
      onActionChange={onActionChange}
      t={t}
      trigger={
        <button
          type="button"
          aria-label={t(($) => $.comment.trigger_chip_aria, { name: entry.agent.name, state: label })}
          className={cn(
            "inline-flex h-6 min-w-0 max-w-full animate-in fade-in cursor-pointer items-center gap-1.5 rounded-md border border-transparent px-1.5 text-micro font-medium transition-colors duration-200",
            chipToneClass(entry.action),
          )}
        >
          <TriggerAgentAvatar agent={entry.agent} suppressed={entry.action === "skip"} />
          <span className="truncate">{label}</span>
          <ChevronDown aria-hidden className="size-3 shrink-0 text-faint-foreground" />
        </button>
      }
    />
  );
}

function MultiRecipientChip({
  recipients,
  onActionChange,
  t,
}: {
  recipients: RecipientEntry[];
  onActionChange: (agentId: string, action: RecipientAction) => void;
  t: IssuesT;
}) {
  const [open, setOpen] = useState(false);
  const [tooltipHover, setTooltipHover] = useState(false);
  const activeCount = recipients.filter((r) => r.action !== "skip").length;
  const heads = recipients.slice(0, MAX_STACK_HEADS);
  const overflow = recipients.length - heads.length;
  // Mirror AgentAvatarStack: ~30% overlap reads as "stacked" without
  // obscuring the next avatar.
  const overlap = Math.round(AVATAR_SIZE * 0.3);
  const sentence =
    activeCount === 0
      ? t(($) => $.comment.trigger_none_will_trigger)
      : t(($) => $.comment.recipient_count, { count: activeCount });
  const tone = recipients.some((r) => r.action === "restart")
    ? "restart"
    : recipients.some((r) => r.action === "steer") ? "steer" : "start";

  const popoverTrigger = (
    <PopoverTrigger
      render={
        <button
          type="button"
          className={cn(
            "inline-flex h-6 min-w-0 max-w-full animate-in fade-in cursor-pointer items-center gap-1.5 rounded-md border border-transparent px-1.5 text-micro font-medium transition-colors duration-200",
            chipToneClass(tone),
          )}
        />
      }
    >
      <span className="inline-flex items-center">
        {heads.map((entry, i) => (
          <span
            key={entry.agent.id}
            style={{ marginLeft: i === 0 ? 0 : -overlap }}
            className="inline-flex rounded-full ring-2 ring-background"
          >
            <TriggerAgentAvatar agent={entry.agent} suppressed={entry.action === "skip"} showDot={false} />
          </span>
        ))}
        {overflow > 0 && (
          <span
            style={{
              marginLeft: -overlap,
              width: AVATAR_SIZE,
              height: AVATAR_SIZE,
              fontSize: Math.max(9, Math.round(AVATAR_SIZE * 0.45)),
            }}
            className="inline-flex items-center justify-center rounded-full bg-muted font-medium tabular-nums text-muted-foreground ring-2 ring-background"
          >
            +{overflow}
          </span>
        )}
      </span>
      <span className="truncate">{sentence}</span>
    </PopoverTrigger>
  );

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <Tooltip open={tooltipHover && !open} onOpenChange={setTooltipHover}>
        <TooltipTrigger render={popoverTrigger} />
        <TooltipContent side="top" className="text-caption">
          {t(($) => $.comment.trigger_click_to_manage)}
        </TooltipContent>
      </Tooltip>
      <PopoverContent align="start" className="w-80 p-2">
        <div className="px-1.5 pb-1 text-caption font-medium text-muted-foreground">
          {t(($) => $.comment.trigger_preview_title)}
        </div>
        <div className="flex flex-col">
          {recipients.map((entry) => {
            const label = actionLabel(entry.action, entry.state, t);
            const state = stateLabel(entry.state, t);
            return (
              <div key={entry.agent.id} className="flex min-w-0 items-center gap-2 rounded-md px-1.5 py-1">
                <TriggerAgentAvatar agent={entry.agent} suppressed={entry.action === "skip"} />
                <span className="min-w-0 flex-1 truncate text-caption">
                  {entry.agent.name}
                  {state && <span className="text-muted-foreground"> · {state}</span>}
                </span>
                <RecipientActionMenu
                  entry={entry}
                  onActionChange={onActionChange}
                  t={t}
                  trigger={
                    <button
                      type="button"
                      aria-label={t(($) => $.comment.trigger_chip_aria, { name: entry.agent.name, state: label })}
                      className={cn(
                        "inline-flex h-6 max-w-40 shrink-0 cursor-pointer items-center gap-1 rounded-md border border-transparent px-1.5 text-micro font-medium transition-colors",
                        chipToneClass(entry.action),
                      )}
                    >
                      <span className="truncate">{label}</span>
                      <ChevronDown aria-hidden className="size-3 shrink-0 text-faint-foreground" />
                    </button>
                  }
                />
              </div>
            );
          })}
        </div>
      </PopoverContent>
    </Popover>
  );
}

function TriggerAgentAvatar({
  agent,
  suppressed,
  showDot = true,
}: {
  agent: CommentTriggerPreviewAgent;
  suppressed: boolean;
  showDot?: boolean;
}) {
  return (
    <span
      className={cn(
        "relative inline-flex shrink-0",
        suppressed && "opacity-40 grayscale",
      )}
    >
      <ActorAvatarBase
        name={agent.name}
        initials=""
        avatarUrl={agent.avatar_url}
        isAgent
        size="xs"
      />
      {showDot && !suppressed && <AgentStatusDot agentId={agent.id} size="xs" />}
    </span>
  );
}
