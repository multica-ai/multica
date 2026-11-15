"use client";

import type { ReactNode } from "react";
import { useAuthStore } from "@multica/core/auth";
import { useConfigStore } from "@multica/core/config";
import { needsSourceBackfill, SOURCE_BACKFILL_MAX_DISMISSALS } from "@multica/core/onboarding";
import { useDevPromptStore, useSessionPromptStore } from "@multica/core/session-prompt";
import { STAR_PROMPT_MAX_SHOWINGS, starPromptDue } from "@multica/core/star-prompt";
import { cn } from "@multica/ui/lib/utils";
import {
  resetSourceBackfillDismissCount,
  useSourceBackfillDismissCount,
} from "../onboarding/source-backfill-dismiss";
import { WHATS_NEW_RELEASES } from "../whats-new/releases";
import { useWhatsNew } from "../whats-new/use-whats-new";
import { deckBlocker, deckCards, type DeckBlocker } from "../whats-new/whats-new-deck";
import type { MetricTone } from "./metrics";
import { PanelHeader, SmallButton, TONE_CLASS } from "./panels";

const BLOCKER_LABEL: Record<DeckBlocker, string> = {
  dealt: "already dealt this session",
  survey: "source survey open or deciding",
  corner: "star card holds the corner",
  chat: "chat window open",
  busy: "typing, a dialog open, or tab hidden",
};

function Section({ title, note, actions, children }: { title: string; note?: string; actions?: ReactNode; children: ReactNode }) {
  return (
    <div className="border-b border-border last:border-b-0">
      <div className="flex items-center gap-2 px-3 pt-2 pb-1">
        <span className="font-medium text-foreground">{title}</span>
        {note && <span className="text-micro text-muted-foreground">{note}</span>}
        <div className="ml-auto flex items-center gap-1">{actions}</div>
      </div>
      <div className="pb-2">{children}</div>
    </div>
  );
}

function Field({ label, tone = "normal", children }: { label: string; tone?: MetricTone; children: ReactNode }) {
  return (
    <div className="flex min-h-5 items-center gap-3 px-3">
      <span className="w-32 shrink-0 text-muted-foreground">{label}</span>
      <span className={cn("min-w-0 flex-1 truncate font-mono tabular-nums", TONE_CLASS[tone])}>{children}</span>
    </div>
  );
}

const yesNo = (value: boolean) => (value ? "yes" : "no");
const day = (iso: string | null | undefined) => (iso ? iso.slice(0, 10) : "—");

/**
 * Every gate behind the app's own prompts — the What's new deck, the GitHub
 * star card and the source survey — with buttons that show each one on demand.
 * `now` re-renders the panel so DOM-dependent gates stay current.
 */
export function PromptsPanel({ wsId }: { wsId: string | null; now: number }) {
  const user = useAuthStore((s) => s.user);
  const serverVersion = useConfigStore((s) => s.serverVersion);
  const session = useSessionPromptStore();
  const trigger = useDevPromptStore((s) => s.trigger);
  const { visible, unread } = useWhatsNew();
  const [dismissCount] = useSourceBackfillDismissCount(user?.id);

  const cards = deckCards(unread).length;
  const blocker = deckBlocker();
  const deck: { text: string; tone: MetricTone } =
    session.corner === "whats-new"
      ? { text: "on screen", tone: "normal" }
      : !wsId
        ? { text: "no workspace open", tone: "warn" }
        : unread.length === 0
          ? { text: "nothing unread", tone: "warn" }
          : cards === 0
            ? { text: "no unread highlight has hero art", tone: "bad" }
            : blocker
              ? { text: `waiting: ${BLOCKER_LABEL[blocker]}`, tone: blocker === "dealt" ? "warn" : "normal" }
              : { text: "deals at the next check", tone: "normal" };

  const seenSupported = user?.whats_new_seen_version !== undefined;
  const setSeen = (version: string) => {
    const current = useAuthStore.getState().user;
    if (current) useAuthStore.getState().setUser({ ...current, whats_new_seen_version: version || null });
  };

  return (
    <>
      <PanelHeader title="Prompts" note="this app session">
        <SmallButton onClick={() => session.reset()}>Reset session</SmallButton>
      </PanelHeader>
      <Section title="Session">
        <Field label="Prompt budget" tone={session.prompted ? "warn" : "normal"}>
          {session.prompted ? "used by survey or star card" : "free"}
        </Field>
        <Field label="Corner">{session.corner ?? "empty"}</Field>
      </Section>
      <Section
        title="What's new deck"
        actions={
          <>
            <SmallButton onClick={() => trigger("whats-new-check")} disabled={!wsId}>
              Run check now
            </SmallButton>
            <SmallButton onClick={() => trigger("whats-new")} disabled={!wsId}>
              Show deck
            </SmallButton>
          </>
        }
      >
        <Field label="Status" tone={deck.tone}>
          {deck.text}
        </Field>
        <Field label="Server version">{serverVersion || "unknown · no cap"}</Field>
        <Field label="Seen version" tone={seenSupported ? "normal" : "bad"}>
          {seenSupported ? (
            <select
              aria-label="Seen version"
              value={user?.whats_new_seen_version ?? ""}
              onChange={(e) => setSeen(e.target.value)}
              className="h-5 rounded-sm border border-border bg-background px-1 font-mono text-micro"
            >
              <option value="">none · count since signup</option>
              {WHATS_NEW_RELEASES.map((r) => (
                <option key={r.version} value={r.version}>
                  {r.version}
                </option>
              ))}
            </select>
          ) : (
            "server cannot store it"
          )}
        </Field>
        <Field label="Signed up">{day(user?.created_at)}</Field>
        <Field label="Visible">{visible.map((r) => r.version).join(", ") || "—"}</Field>
        <Field label="Unread">{unread.map((r) => r.version).join(", ") || "—"}</Field>
        <Field label="Cards to deal">{String(cards)}</Field>
        <div className="px-3 pt-1 text-micro text-muted-foreground">
          Seen version changes stay on this client until the next reload. Show deck deals every card with hero art,
          read or not.
        </div>
      </Section>
      <Section title="Star card" actions={<SmallButton onClick={() => trigger("star")} disabled={!wsId}>Show star card</SmallButton>}>
        <Field label="Due" tone={starPromptDue(user, Date.now()) ? "normal" : "warn"}>
          {yesNo(starPromptDue(user, Date.now()))}
        </Field>
        <Field label="Showings">
          {user?.star_prompt_shown_count === undefined
            ? "server cannot count them"
            : `${user.star_prompt_shown_count}/${STAR_PROMPT_MAX_SHOWINGS} · last ${day(user.star_prompt_last_shown_at)}`}
        </Field>
        <Field label="Starred">{day(user?.star_prompt_starred_at)}</Field>
        <div className="px-3 pt-1 text-micro text-muted-foreground">
          Shows when an agent&apos;s issue moves to done. Show star card skips the server&apos;s showing count.
        </div>
      </Section>
      <Section
        title="Source survey"
        actions={
          <>
            <SmallButton onClick={() => user && resetSourceBackfillDismissCount(user.id)} disabled={dismissCount === 0}>
              Reset dismissals
            </SmallButton>
            <SmallButton onClick={() => trigger("survey")} disabled={!user || !wsId}>
              Open survey
            </SmallButton>
          </>
        }
      >
        <Field label="Owes an answer">{yesNo(needsSourceBackfill(user, dismissCount))}</Field>
        <Field label="Dismissals">{`${dismissCount}/${SOURCE_BACKFILL_MAX_DISMISSALS}`}</Field>
        <Field label="Open or deciding">{yesNo(session.surveyPending)}</Field>
        <div className="px-3 pt-1 text-micro text-muted-foreground">
          Submitting or skipping the survey saves your answer to the server.
        </div>
      </Section>
    </>
  );
}
