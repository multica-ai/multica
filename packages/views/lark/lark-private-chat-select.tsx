"use client";

import { useEffect, useMemo, useState } from "react";
import { RefreshCw } from "@multica/ui/icons";
import { useConfirmLarkPrivateChats } from "@multica/core/lark";
import type { LarkPrivateChatCandidate } from "@multica/core/types";
import { Alert, AlertDescription } from "@multica/ui/components/ui/alert";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useLocale, useT } from "../i18n";
import {
  candidateDisplayName,
  candidateTimeMs,
  larkDiscoveryErrorKey,
  larkPrivateChatErrorKey,
  type LarkPrivateChatErrorKey,
} from "./discovery";
import { useLarkPrivateChatCandidates, useLarkTargetCapabilities } from "./use-lark-discovery";
import { ChatIdDisclosure, LarkSelectionChips, type LarkChatSelection } from "./picker-common";

/**
 * Private-chat authorization for the conversation-grant form (OL-76): the
 * direct chats people open with this bot are DISCOVERED (someone sends the
 * bot a message → the conversation appears here) and then explicitly
 * CONFIRMED by a human with the management role — no raw oc_ entry, no
 * auto-approval, no test messages. Confirmation goes through the OL-75
 * confirm endpoint, which atomically appends the selected candidates to the
 * saved grant; the returned grant is the authoritative saved state — the
 * mutation writes it into the installations cache and the form re-baselines
 * its draft from there, so a later full-list save can never overwrite it.
 *
 * Saved p2p chats are chips (removed individually, applied by the form's
 * full-list save) — including chats no longer present in the candidate list.
 * Raw oc_ typing remains as the caller-supplied `fallback` for servers
 * without candidate discovery; forbidden never falls back (OL-72 contract).
 */
export function LarkPrivateChatSelect({
  wsId,
  installationId,
  selected,
  onChange,
  max,
  otherCount,
  disabled,
  fallback,
  onBusyChange,
}: {
  wsId: string;
  installationId: string;
  selected: LarkChatSelection[];
  onChange: (next: LarkChatSelection[]) => void;
  /** Total conversation cap across groups and direct chats. */
  max: number;
  /** Conversations configured outside this list (the group multi-select). */
  otherCount: number;
  disabled?: boolean;
  fallback: React.ReactNode;
  /** Reports the confirm mutation's wait state so the parent form can hold
   * its full-list save/revoke until the confirmation has landed. */
  onBusyChange?: (busy: boolean) => void;
}) {
  const { t } = useT("lark");
  const locale = useLocale();
  const caps = useLarkTargetCapabilities(wsId, installationId);
  const supported = caps.data?.private_chat_candidates_supported === true;
  const list = useLarkPrivateChatCandidates(wsId, installationId, { enabled: supported });
  const confirm = useConfirmLarkPrivateChats(wsId, installationId);

  // Checkbox state is the candidate UUID set, kept local until the explicit
  // confirm (OL-75 contract: never auto-approve, never save on selection).
  const [checked, setChecked] = useState<ReadonlySet<string>>(new Set());
  const [confirmedNote, setConfirmedNote] = useState(false);

  const blocked = disabled === true || confirm.isPending;

  // Confirm and the form's full-list save/revoke are mutually exclusive
  // writes: a stale-draft PUT committed after a confirm would drop the
  // freshly authorized chats, so the parent shares this wait state.
  useEffect(() => {
    onBusyChange?.(confirm.isPending);
    return () => onBusyChange?.(false);
  }, [confirm.isPending, onBusyChange]);

  // A refreshed list may drop checked candidates (expired, evicted, or
  // authorized elsewhere): prune them so a dead UUID never occupies the cap
  // or gets submitted.
  useEffect(() => {
    setChecked((prev) => {
      if (prev.size === 0) return prev;
      const valid = new Set(
        list.items.filter((c) => c.authorization_status !== "authorized").map((c) => c.id),
      );
      let changed = false;
      const next = new Set<string>();
      for (const id of prev) {
        if (valid.has(id)) next.add(id);
        else changed = true;
      }
      return changed ? next : prev;
    });
  }, [list.items]);

  // Saved p2p chips resolve their names from the candidate list when the
  // draft only has the raw ID (e.g. a grant saved before this UI existed).
  const nameByChatId = useMemo(() => {
    const map = new Map<string, string>();
    for (const c of list.items) {
      const name = candidateDisplayName(c);
      if (name != null) map.set(c.chat_id, name);
    }
    return map;
  }, [list.items]);
  const chipsSelected = useMemo(
    () => selected.map((s) =>
      s.name.trim() !== "" ? s : { ...s, name: nameByChatId.get(s.chatId) ?? "" }),
    [selected, nameByChatId],
  );

  const chips = selected.length > 0 && (
    <LarkSelectionChips
      selected={chipsSelected}
      onRemove={(chatId) => onChange(selected.filter((s) => s.chatId !== chatId))}
      disabled={blocked}
    />
  );

  const errorText = (key: LarkPrivateChatErrorKey): string =>
    key === "limit_exceeded"
      ? t(($) => $.private.error.limit_exceeded, { max })
      : t(($) => $.private.error[key]);

  if (!supported) {
    // Forbidden is NOT a fallback case (OL-72 contract): explain the required
    // management role, never offer raw-ID entry as a bypass. Saved grants
    // stay visible (and removable) — they keep their existing save semantics.
    if (caps.isError && larkDiscoveryErrorKey(caps.error) === "forbidden") {
      return (
        <div className="space-y-2">
          {chips}
          <p className="text-caption text-muted-foreground">
            {t(($) => $.private.error.forbidden)}
          </p>
        </div>
      );
    }
    return (
      <div className="space-y-2">
        {chips}
        {caps.isPending ? (
          <p className="py-1 text-caption text-muted-foreground">
            {t(($) => $.private.loading)}
          </p>
        ) : (
          <>
            <p className="text-caption text-muted-foreground">
              {caps.isError
                ? errorText(larkPrivateChatErrorKey(caps.error))
                : t(($) => $.private.error.unsupported)}
            </p>
            {fallback}
          </>
        )}
      </div>
    );
  }

  const formatTime = (iso: string): string => {
    const ms = candidateTimeMs(iso);
    return ms == null
      ? t(($) => $.anchor.time_unknown)
      : new Date(ms).toLocaleString(locale);
  };

  const pending = list.items.filter((c) => c.authorization_status !== "authorized");
  const usedCount = selected.length + otherCount;
  const atCap = usedCount + checked.size >= max;
  const wouldExceed = usedCount + checked.size > max;
  const hasIdOnly = list.items.some((c) => candidateDisplayName(c) == null);

  const toggle = (candidate: LarkPrivateChatCandidate) => {
    setConfirmedNote(false);
    setChecked((prev) => {
      const next = new Set(prev);
      if (next.has(candidate.id)) next.delete(candidate.id);
      else if (usedCount + next.size < max) next.add(candidate.id);
      return next;
    });
  };

  async function confirmChecked() {
    if (checked.size === 0 || confirm.isPending) return;
    const ids = [...checked];
    try {
      await confirm.mutateAsync(ids);
      // The mutation's onSuccess has already written the returned grant into
      // the installations cache; the parent form re-baselines its draft from
      // that authoritative grant (OL-76 re-review — never hand-merge a stale
      // draft here). This component only releases the selection and confirms
      // the outcome.
      setChecked(new Set());
      setConfirmedNote(true);
    } catch (err) {
      if (larkPrivateChatErrorKey(err) === "candidate_unavailable") {
        // Stale selection: the contract prescribes refreshing the list; the
        // dead UUIDs must not be retried.
        setChecked(new Set());
        list.refresh();
      }
    }
  }

  return (
    <div className="space-y-2">
      {chips}
      <div className="space-y-2 rounded-md border p-2">
        <div className="flex items-start justify-between gap-2">
          <p className="text-caption text-muted-foreground">
            {t(($) => $.private.intro)}
          </p>
          <Button
            size="sm"
            variant="outline"
            className="shrink-0"
            onClick={() => {
              setConfirmedNote(false);
              list.refresh();
            }}
            disabled={blocked || list.isFetching}
          >
            <RefreshCw className={cn("h-3 w-3", list.isFetching && "animate-spin")} />
            {list.isFetching
              ? t(($) => $.private.refreshing)
              : t(($) => $.private.refresh)}
          </Button>
        </div>

        {list.errorKey != null && (
          <Alert variant="destructive">
            <AlertDescription className="flex items-center justify-between gap-2">
              <span>{errorText(list.errorKey)}</span>
              <Button size="sm" variant="outline" onClick={list.refresh} disabled={list.isFetching}>
                <RefreshCw className="h-3 w-3" />
                {t(($) => $.picker.retry)}
              </Button>
            </AlertDescription>
          </Alert>
        )}
        {confirm.isError && (
          <Alert variant="destructive">
            <AlertDescription>{errorText(larkPrivateChatErrorKey(confirm.error))}</AlertDescription>
          </Alert>
        )}
        {confirmedNote && !confirm.isError && (
          <p role="status" className="text-caption">
            {t(($) => $.private.confirmed)}
          </p>
        )}

        {list.isLoading ? (
          <p className="px-1 py-2 text-caption text-muted-foreground">
            {t(($) => $.private.loading)}
          </p>
        ) : list.errorKey == null && list.items.length === 0 ? (
          <div className="space-y-1 rounded-md border border-dashed px-3 py-3">
            <p className="text-caption font-medium">
              {t(($) => $.private.empty_title)}
            </p>
            <p className="text-caption text-muted-foreground">
              {t(($) => $.private.empty_steps)}
            </p>
          </div>
        ) : (
          list.items.length > 0 && (
            <>
              {hasIdOnly && (
                <p className="text-micro text-muted-foreground">
                  {t(($) => $.private.id_only_hint)}
                </p>
              )}
              <div
                role="group"
                aria-label={t(($) => $.private.list_label)}
                className="max-h-56 divide-y overflow-y-auto rounded-md border"
              >
                {list.items.map((candidate) => {
                  const name = candidateDisplayName(candidate);
                  const isAuthorized = candidate.authorization_status === "authorized";
                  const isChecked = checked.has(candidate.id);
                  const rowDisabled =
                    blocked || isAuthorized || (atCap && !isChecked);
                  const body = (
                    <>
                      <span className="flex items-center gap-1.5">
                        {name != null ? (
                          <span className="truncate text-body">{name}</span>
                        ) : (
                          <span className="truncate text-body text-muted-foreground">
                            {t(($) => $.private.name_unavailable)}
                          </span>
                        )}
                        <Badge
                          variant={isAuthorized ? "outline" : "secondary"}
                          className="shrink-0"
                        >
                          {isAuthorized
                            ? t(($) => $.private.authorized_badge)
                            : t(($) => $.private.pending_badge)}
                        </Badge>
                      </span>
                      <span className="block text-caption text-muted-foreground">
                        {t(($) => $.private.last_seen, { when: formatTime(candidate.last_seen_at) })}
                        {" · "}
                        {t(($) => $.private.expires, { when: formatTime(candidate.expires_at) })}
                      </span>
                      <span className="flex flex-wrap items-center gap-x-2">
                        <ChatIdDisclosure id={candidate.chat_id} />
                        {candidate.sender.id != null && candidate.sender.id !== "" && (
                          <ChatIdDisclosure
                            id={candidate.sender.id}
                            showLabel={t(($) => $.private.show_user_id)}
                            hideLabel={t(($) => $.private.hide_user_id)}
                          />
                        )}
                      </span>
                    </>
                  );
                  // Authorized rows are informational (revocation happens via
                  // the chips + the form's full-list save); pending rows are
                  // checkbox options committed only by the confirm action.
                  return isAuthorized ? (
                    <div key={candidate.id} className="flex w-full items-start gap-2.5 px-3 py-2">
                      <span className="min-w-0 flex-1">{body}</span>
                    </div>
                  ) : (
                    <label
                      key={candidate.id}
                      className={cn(
                        "flex w-full items-start gap-2.5 px-3 py-2 transition-colors",
                        rowDisabled ? "cursor-not-allowed opacity-60" : "cursor-pointer hover:bg-muted",
                        isChecked && "bg-muted/60 font-medium",
                      )}
                    >
                      <input
                        type="checkbox"
                        className="mt-1"
                        checked={isChecked}
                        disabled={rowDisabled}
                        onChange={() => toggle(candidate)}
                      />
                      <span className="min-w-0 flex-1">{body}</span>
                    </label>
                  );
                })}
              </div>
            </>
          )
        )}

        {pending.length > 0 && (
          <div className="space-y-1">
            {checked.size > 0 && (
              <p className="text-micro text-muted-foreground">
                {t(($) => $.private.confirm_scope)}
              </p>
            )}
            {atCap && (
              <p role="status" className="text-caption text-warning">
                {t(($) => $.picker.multi_max, { max })}
              </p>
            )}
            <div className="flex justify-end">
              <Button
                size="sm"
                onClick={() => void confirmChecked()}
                disabled={blocked || checked.size === 0 || wouldExceed}
              >
                {t(($) => $.private.confirm_selected, { count: checked.size })}
              </Button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
