import { needsSourceBackfill, SOURCE_BACKFILL_MAX_DISMISSALS } from "@multica/core/onboarding";
import { useDevPromptStore, useSessionPromptStore } from "@multica/core/session-prompt";
import type { DevFeature } from "../dev-toolbar/actions";
import { resetSourceBackfillDismissCount, useSourceBackfillDismissCount } from "./source-backfill-dismiss";

export const sourceSurveyDevFeature: DevFeature = {
  id: "source-survey",
  group: "Prompts",
  title: "Source survey",
  useDevState: ({ user }) => {
    const pending = useSessionPromptStore((s) => s.surveyPending);
    const [dismissals] = useSourceBackfillDismissCount(user?.id);
    const owes = needsSourceBackfill(user, dismissals);
    return {
      status: pending ? { text: "open or deciding" } : owes ? { text: "owes an answer" } : { text: "settled" },
      rows: [{ label: "Dismissals", value: `${dismissals}/${SOURCE_BACKFILL_MAX_DISMISSALS}` }],
    };
  },
  actions: [
    {
      id: "open",
      title: "Open survey",
      needs: "workspace",
      caution: "Submitting or skipping saves your answer to the server",
      run: () => useDevPromptStore.getState().trigger("survey"),
    },
    {
      id: "reset-dismissals",
      title: "Reset dismissals",
      description: "This browser only",
      needs: "user",
      run: ({ user }) => user && resetSourceBackfillDismissCount(user.id),
    },
  ],
};
