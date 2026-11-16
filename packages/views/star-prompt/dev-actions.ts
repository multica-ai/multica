import { useDevPromptStore, useSessionPromptStore } from "@multica/core/session-prompt";
import { STAR_PROMPT_MAX_SHOWINGS, starPromptDue } from "@multica/core/star-prompt";
import type { DevFeature } from "../dev-toolbar/actions";

const day = (iso: string | null | undefined) => (iso ? iso.slice(0, 10) : "—");

export const starPromptDevFeature: DevFeature = {
  id: "star-card",
  group: "Prompts",
  title: "Star card",
  useDevState: ({ user }) => {
    const corner = useSessionPromptStore((s) => s.corner);
    const due = starPromptDue(user, Date.now());
    const showings =
      user?.star_prompt_shown_count === undefined
        ? "server cannot count them"
        : `${user.star_prompt_shown_count}/${STAR_PROMPT_MAX_SHOWINGS} · last ${day(user.star_prompt_last_shown_at)}`;
    return {
      status: corner === "star" ? { text: "on screen" } : due ? { text: "due" } : { text: "not due", tone: "warn" },
      rows: [
        { label: "Showings", value: showings },
        { label: "Starred", value: day(user?.star_prompt_starred_at) },
      ],
      note: "Shows when an agent's issue moves to done.",
    };
  },
  actions: [
    {
      id: "show",
      title: "Show star card",
      description: "Skips the server's showing count",
      needs: "workspace",
      run: () => useDevPromptStore.getState().trigger("star"),
    },
  ],
};
