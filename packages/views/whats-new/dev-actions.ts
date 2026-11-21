import { useAuthStore } from "@multica/core/auth";
import { useChatStore } from "@multica/core/chat";
import { useConfigStore } from "@multica/core/config";
import { useDevPromptStore, useSessionPromptStore } from "@multica/core/session-prompt";
import type { DevFeature, DevFeatureState } from "../dev-toolbar/actions";
import { WHATS_NEW_RELEASES } from "./releases";
import { useWhatsNew } from "./use-whats-new";
import { deckBlocker, deckCards, type DeckBlocker } from "./whats-new-deck";

const BLOCKER_LABEL: Record<DeckBlocker, string> = {
  dealt: "already dealt this session",
  survey: "source survey open or deciding",
  corner: "star card holds the corner",
  chat: "chat window open",
  busy: "an input has focus, a dialog is open, or the tab is hidden",
};

const day = (iso: string | null | undefined) => (iso ? iso.slice(0, 10) : "—");

export const whatsNewDevFeature: DevFeature = {
  id: "whats-new-deck",
  group: "Prompts",
  title: "What's new deck",
  useDevState: ({ wsId, user }): DevFeatureState => {
    const serverVersion = useConfigStore((s) => s.serverVersion);
    // `deckBlocker` reads these stores without subscribing; reading them here
    // keeps the status current as they change.
    const { corner } = useSessionPromptStore();
    useChatStore((s) => s.isOpen);
    const { visible, unread } = useWhatsNew();
    const cards = deckCards(unread).length;
    const blocker = deckBlocker();
    const status: NonNullable<DevFeatureState["status"]> =
      corner === "whats-new"
        ? { text: "on screen" }
        : !wsId
          ? { text: "no workspace open", tone: "warn" }
          : unread.length === 0
            ? { text: "nothing unread", tone: "warn" }
            : cards === 0
              ? { text: "no unread highlight has hero art", tone: "bad" }
              : blocker
                ? { text: `waiting · ${BLOCKER_LABEL[blocker]}`, tone: blocker === "dealt" ? "warn" : "normal" }
                : { text: "deals at the next check" };
    const seen = user?.whats_new_seen_version;
    return {
      status,
      rows: [
        { label: "Server version", value: serverVersion || "unknown · no cap" },
        {
          label: "Seen version",
          value: seen === undefined ? "server cannot store it" : (seen ?? "none · count since signup"),
          tone: seen === undefined ? "bad" : "normal",
        },
        { label: "Signed up", value: day(user?.created_at) },
        { label: "Visible", value: visible.map((r) => r.version).join(", ") || "—" },
        { label: "Unread", value: unread.map((r) => r.version).join(", ") || "—" },
        { label: "Cards to deal", value: String(cards) },
      ],
    };
  },
  actions: [
    {
      id: "show",
      title: "Show deck",
      description: "Deals every card with hero art, read or not",
      needs: "workspace",
      run: () => useDevPromptStore.getState().trigger("whats-new"),
    },
    {
      id: "check",
      title: "Run check now",
      description: "The real gates, without the start-up delay",
      needs: "workspace",
      run: () => useDevPromptStore.getState().trigger("whats-new-check"),
    },
    {
      id: "seen-version",
      title: "Set seen version",
      description: "This client only, until the next reload",
      needs: "user",
      choices: [
        { value: "", label: "none" },
        ...WHATS_NEW_RELEASES.map((r) => ({ value: r.version, label: r.version })),
      ],
      run: (_ctx, version) => {
        const current = useAuthStore.getState().user;
        if (current) useAuthStore.getState().setUser({ ...current, whats_new_seen_version: version || null });
      },
    },
  ],
};
