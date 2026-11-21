import { useSessionPromptStore } from "@multica/core/session-prompt";
import type { DevFeature } from "./actions";

export const sessionPromptDevFeature: DevFeature = {
  id: "prompt-session",
  group: "Prompts",
  title: "Prompt session",
  useDevState: () => {
    const { prompted, deckDealt, corner } = useSessionPromptStore();
    return {
      status: { text: corner ? `${corner} holds the corner` : "corner empty" },
      rows: [
        { label: "Prompt budget", value: prompted ? "used by survey or star card" : "free", tone: prompted ? "warn" : "normal" },
        { label: "Deck dealt", value: deckDealt ? "yes" : "no" },
      ],
    };
  },
  actions: [
    {
      id: "reset",
      title: "Reset session",
      description: "Forgets what this session has shown, as a reload would",
      run: () => useSessionPromptStore.getState().reset(),
    },
  ],
};
