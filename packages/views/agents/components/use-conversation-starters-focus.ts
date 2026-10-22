"use client";

import { useEffect, useRef, useState } from "react";
import { AGENT_FOCUS_CONVERSATION_STARTERS } from "@multica/core/paths";
import { useOptionalNavigation } from "../../navigation";

/** How long the deep-linked conversation-starters editor stays ringed. */
export const CONVERSATION_STARTERS_FOCUS_FLASH_MS = 1600;

/**
 * Arriving from "customize" in a chat's empty state: bring the starter
 * prompts into view and flash them, so the deep link lands ON the setting
 * rather than on a long page that happens to contain it. The param is
 * consumed on arrival, so a refresh never replays an animation nobody asked
 * for — but every fresh click must land, including a second one from a chat
 * window still open beside this page, and one aimed at a different agent.
 *
 * Navigation is read optionally: the editors that host this are leaves that
 * tests mount in isolation, and without an adapter there is nothing to focus.
 */
export function useConversationStartersFocus(agentId: string, enabled: boolean) {
  const navigation = useOptionalNavigation();
  const ref = useRef<HTMLDivElement | null>(null);
  const [focused, setFocused] = useState(false);
  // Bumped on every focus so a repeat restarts the flash window instead of
  // inheriting the previous one's remaining time.
  const [flashId, setFlashId] = useState(0);
  const handledForAgentRef = useRef<string | null>(null);

  useEffect(() => {
    if (!navigation || !enabled) return;

    if (navigation.searchParams.get("focus") !== AGENT_FOCUS_CONVERSATION_STARTERS) {
      // The param is gone: either this is an ordinary visit, or the `replace`
      // below has landed. Re-arm so the next click focuses again.
      handledForAgentRef.current = null;
      return;
    }
    // Guard against re-running before the stripped URL arrives — the adapter
    // object is not referentially stable, so this effect re-runs on renders
    // the `replace` below itself causes. Keyed by agent so a deep link aimed
    // at a DIFFERENT agent is never swallowed by this latch.
    if (handledForAgentRef.current === agentId) return;
    handledForAgentRef.current = agentId;

    // `block: "nearest"` deliberately: native scrollIntoView scrolls every
    // scrollable ancestor, which on desktop drags the shell itself (#3929).
    ref.current?.scrollIntoView?.({ block: "nearest" });
    setFocused(true);
    setFlashId((id) => id + 1);

    const params = new URLSearchParams(navigation.searchParams);
    params.delete("focus");
    const query = params.toString();
    navigation.replace(
      query ? `${navigation.pathname}?${query}` : navigation.pathname,
    );
  }, [agentId, navigation, enabled]);

  // The flash timer must not hang off the effect above: the adapter object is
  // unstable, so that effect's cleanup runs on the very next render and would
  // cancel the timer, leaving the ring on permanently. Keyed on the flash
  // itself, it also survives StrictMode's mount-unmount-mount replay, which
  // the latch above deliberately does not re-run.
  useEffect(() => {
    if (!focused) return;
    const timer = setTimeout(
      () => setFocused(false),
      CONVERSATION_STARTERS_FOCUS_FLASH_MS,
    );
    return () => clearTimeout(timer);
  }, [focused, flashId]);

  return { ref, focused };
}
